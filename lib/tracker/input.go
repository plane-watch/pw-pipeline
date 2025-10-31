package tracker

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/exp/slices"
	"plane.watch/lib/monitoring"
	"plane.watch/lib/tracker/beast"
	"plane.watch/lib/tracker/mode_s"
	"plane.watch/lib/tracker/sbs1"
)

type (
	// Option allows us to configure our new Tracker as we need it
	Option func(*Tracker)

	EventMaker interface {
		Stopper
		Listen() chan FrameEvent
	}
	EventListener interface {
		OnEvent(Event)
	}
	Stopper interface {
		Stop()
	}
	// Frame is our general object for a tracking update, AVR, SBS1, Modes Beast Binary
	Frame interface {
		Icao() uint32
		IcaoStr() string
		Decode() error
		TimeStamp() time.Time
		Raw() []byte
	}

	// A Producer can listen for or generate Frames, it provides the output via a channel that the handler can then
	// process further.
	// A Producer can send *FrameEvent events
	Producer interface {
		EventMaker
		Source() *FrameSource
		fmt.Stringer
		monitoring.HealthCheck
	}

	// Sink is something that takes the output from our producers and trackers
	Sink interface {
		EventListener
		Stopper
		monitoring.HealthCheck
	}

	// Middleware has a chance to modify a frame before we send it to the plane Tracker
	Middleware interface {
		fmt.Stringer
		monitoring.HealthCheck
		Handle(*FrameEvent) Frame
	}
)

func WithDecodeWorkerCount(numDecodeWorkers int) Option {
	return func(t *Tracker) {
		t.decodeWorkerCount = numDecodeWorkers
	}
}

func WithPruneTiming(pruneTick, pruneAfter time.Duration) Option {
	return func(t *Tracker) {
		t.pruneTick = pruneTick
		t.pruneAfter = pruneAfter
	}
}
func WithPrometheusCounters(currentPlanes prometheus.Gauge, decodedFrames prometheus.Counter) Option {
	return func(t *Tracker) {
		t.stats.currentPlanes = currentPlanes
		t.stats.decodedFrames = decodedFrames
	}
}

// Finish begins the ending of the tracking by closing our decoding queue
func (t *Tracker) Finish() {
	if t.finishDone {
		return
	}

	t.finishDone = true
	t.log.Debug().Str("func", "Finish()").Msg("Starting...")

	// stop all producers
	t.muProducers.Lock()
	stopFuncs := make([]func(), 0, len(t.producers))
	for _, p := range t.producers {
		t.log.Debug().Str("func", "Finish()").Str("producer", p.String()).Msg("Stopping Producer")
		stopFuncs = append(stopFuncs, p.Stop)
	}
	t.muProducers.Unlock()
	for _, f := range stopFuncs {
		f()
	}

	t.log.Debug().Str("func", "Finish()").Msg("Closing Decoding Queue")
	t.planeList.Stop()
	t.log.Debug().Str("func", "Finish()").Msg("done...")
}

// AddProducer wires up a Producer to start feeding data into the tracker
func (t *Tracker) AddProducer(p Producer) {
	if p == nil {
		return
	}

	monitoring.AddHealthCheck(p)

	t.muProducers.Lock()
	defer t.muProducers.Unlock()

	t.log.Debug().Str("producer", p.String()).Msg("Adding producer")
	t.producers = append(t.producers, p)

	doneChan := make(chan bool)
	inFlight := t.decodeWorkerCount

	t.producerWaiter.Go(func() {
		for range doneChan {
			inFlight--
			if inFlight == 0 {
				break
			}
		}
		close(doneChan)
		t.removeProducer(p)
		monitoring.RemoveHealthCheck(p)
	})
	for i := 0; i < t.decodeWorkerCount; i++ {
		go t.decodeQueue(p.Listen(), doneChan)
	}
	t.log.Info().
		Int("num workers", t.decodeWorkerCount).
		Str("source", p.String()).
		Msg("Just added a producer")
}

// todo(mikenye): fix this error
//  /app/lib/tracker/input.go:148 is actually 179 below
//runway-runway-1  | panic: runtime error: slice bounds out of range [110:1:]
//runway-runway-1  |
//runway-runway-1  | goroutine 3533 [running]:
//runway-runway-1  | slices.Delete[...](...)
//runway-runway-1  |      /usr/local/go/src/slices/slices.go:223
//runway-runway-1  | golang.org/x/exp/slices.Delete[...](...)
//runway-runway-1  |      /go/pkg/mod/golang.org/x/exp@v0.0.0-20250911091902-df9299821621/slices/slices.go:111
//runway-runway-1  | plane.watch/lib/tracker.(*Tracker).removeProducer(0xc000294580, {0xcc6e88, 0xc000d00b00})
//runway-runway-1  |      /app/lib/tracker/input.go:148 +0x314
//runway-runway-1  | plane.watch/lib/tracker.(*Tracker).AddProducer.func1()
//runway-runway-1  |      /app/lib/tracker/input.go:127 +0x8d
//runway-runway-1  | sync.(*WaitGroup).Go.func1()
//runway-runway-1  |      /usr/local/go/src/sync/waitgroup.go:239 +0x4a
//runway-runway-1  | created by sync.(*WaitGroup).Go in goroutine 504
//runway-runway-1  |      /usr/local/go/src/sync/waitgroup.go:237 +0x73
//runway-runway-1 exited with code 0
//runway-runway-1  | slices.Delete[...](...)
//runway-runway-1  |      /usr/local/go/src/slices/slices.go:223
//runway-runway-1  | golang.org/x/exp/slices.Delete[...](...)
//runway-runway-1  |      /go/pkg/mod/golang.org/x/exp@v0.0.0-20250911091902-df9299821621/slices/slices.go:111
//runway-runway-1  | plane.watch/lib/tracker.(*Tracker).removeProducer(0xc000294580, {0xcc6e88, 0xc000d00b00})
//runway-runway-1  |      /app/lib/tracker/input.go:148 +0x314
//runway-runway-1  | plane.watch/lib/tracker.(*Tracker).AddProducer.func1()
//runway-runway-1  |      /app/lib/tracker/input.go:127 +0x8d
//runway-runway-1  | sync.(*WaitGroup).Go.func1()
//runway-runway-1  |      /usr/local/go/src/sync/waitgroup.go:239 +0x4a
//runway-runway-1  | created by sync.(*WaitGroup).Go in goroutine 504
//runway-runway-1  |      /usr/local/go/src/sync/waitgroup.go:237 +0x73

func (t *Tracker) removeProducer(toRemove Producer) {
	t.log.Debug().Str("func", "removeProducer()").Msg("Removing producer")
	if toRemove == nil {
		return
	}
	t.muProducers.Lock()
	defer t.muProducers.Unlock()
	for idx, p := range t.producers {
		if toRemove.String() == p.String() {
			t.producers = slices.Delete(t.producers, idx, 1)
			return
		}
	}
}

// AddMiddleware wires up a Middleware which each message will go through before being added to the tracker
func (t *Tracker) AddMiddleware(m Middleware) {
	if m == nil {
		return
	}
	monitoring.AddHealthCheck(m)
	t.log.Debug().Str("name", m.String()).Msg("Adding middleware")
	t.middlewares = append(t.middlewares, m)

	t.log.Debug().Msg("Just added a middleware")
}

// SetSink wires up a Sink in the tracker. Whenever an event happens it gets sent to each Sink
func (t *Tracker) SetSink(s Sink) {
	if s == nil {
		return
	}
	t.log.Debug().Str("name", s.HealthCheckName()).Msg("Set Sink")
	t.sink = s
	monitoring.AddHealthCheck(s)
}

// Stop attempts to stop all the things, mid-flight. Use this if you have something else waiting for things to finish
// use this if you are listening to remote sources
func (t *Tracker) Stop() {
	t.log.Debug().Str("func", "Stop()").Msg("Starting...")
	t.Finish()
	t.log.Debug().Str("func", "Stop()").Msg("Finish() completed")
	t.producerWaiter.Wait()
	t.log.Debug().Str("func", "Stop()").Msg("producers finished")
	t.decodingQueueWaiter.Wait()
	t.log.Debug().Str("func", "Stop()").Msg("decoding queue done")
	t.eventsWaiter.Wait()
	t.log.Debug().Str("func", "Stop()").Msg("events done")
	t.middlewareWaiter.Wait()
	t.log.Debug().Str("func", "Stop()").Msg("middleware done")
}

// StopOnCancel listens for SigInt etc. and gracefully stops
func (t *Tracker) StopOnCancel() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	isStopping := false
	exitChan := make(chan bool, 3)
	for {
		select {
		case sig := <-ch:
			t.log.Info().Str("Signal", sig.String()).Msg("Received Interrupt, stopping")
			if !isStopping {
				isStopping = true
				go func() {
					t.Stop()
					exitChan <- true
					t.log.Info().Msg("Done Stopping")
				}()
				go func() {
					time.Sleep(time.Second * 5)
					exitChan <- true
					t.log.Info().Msg("Timeout after 5 seconds, force stopping")
					os.Exit(1)
				}()
			} else {
				t.log.Info().Str("Signal", sig.String()).Msg("Second Interrupt, forcing exit")
				os.Exit(1)
			}
		case <-exitChan:
			return
		}
	}
}

// Wait waits for all producers to stop producing input and then returns
// use this method if you are processing a file
func (t *Tracker) Wait() {
	t.producerWaiter.Wait()
	t.log.Debug().Msg("Producers finished")
	time.Sleep(time.Millisecond * 50)
	t.Finish()
	t.log.Debug().Msg("Finish() done")
	t.decodingQueueWaiter.Wait()
	t.log.Debug().Msg("Decoding waiter done")
	t.eventsWaiter.Wait()
	t.log.Debug().Msg("events waiter done")
}

func (t *Tracker) decodeQueue(decodingQueue chan FrameEvent, done chan bool) {
	t.decodingQueueWaiter.Add(1)
	for frameEvent := range decodingQueue {
		if nil != t.stats.decodedFrames {
			t.stats.decodedFrames.Inc()
		}

		// frame is of type interface Frame
		frame := frameEvent.Frame()
		err := frame.Decode()
		if nil != err {
			if !errors.Is(mode_s.ErrNoOp, err) {
				// the decode operation failed to produce valid output, and we tell someone about it
				t.log.Error().Err(err).Str("Tag", frameEvent.Source().Tag).Send()
			}
			continue
		}

		for _, m := range t.middlewares {
			frame = m.Handle(&frameEvent)
			if nil == frame {
				break
			}
		}
		if frame == nil || frame.Icao() == 0 {
			// invalid frame || unable to determine planes ICAO
			continue
		}
		plane := t.GetPlane(frame.Icao())

		// TODO: have each plane object have it's own input channel
		// as it will be the only thing changing the plane, we can eliminate a lot of locking

		switch typeFrame := frame.(type) {
		case *beast.Frame:
			plane.HandleModeSFrame(typeFrame.AvrFrame(), frameEvent.Source())
			plane.setSignalLevel(typeFrame.SignalRssi())
			beast.Release(typeFrame)
		case *mode_s.Frame:
			plane.HandleModeSFrame(typeFrame, frameEvent.Source())
		case *sbs1.Frame:
			plane.HandleSbs1Frame(typeFrame)
		default:
			t.log.Error().Str("Tag", frameEvent.Source().Tag).Msg("unknown frame type, cannot track")
		}
	}
	t.decodingQueueWaiter.Done()
	t.log.Debug().Msg("decodeQueue() is done")
	done <- true
}
