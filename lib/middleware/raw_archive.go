package middleware

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
	"plane.watch/lib/randstr"
	"plane.watch/lib/rawarchive"
	"plane.watch/lib/tile_grid"
	"plane.watch/lib/tracker"
	"plane.watch/lib/tracker/beast"
	"plane.watch/lib/tracker/mode_s"
	"plane.watch/lib/tracker/sbs1"
)

const (
	// RawArchiveDefaultInterval is how often a batch is published when nothing else is set.
	RawArchiveDefaultInterval = 5 * time.Second
	// RawArchiveDefaultMaxBytes is the body size at which a batch is published early.
	RawArchiveDefaultMaxBytes = 512 * 1024
	// RawArchiveMinMaxBytes is the smallest size cap accepted.
	RawArchiveMinMaxBytes = 1024

	rawArchiveQueueLen      = 1000 // handoff only; the batcher drains it continuously
	rawArchiveErrorLogEvery = 10 * time.Second
	rawArchiveBeastMaxLen   = 23 // esc, type, 6 byte timestamp, signal, 14 byte Mode S
)

type (
	// RawArchive publishes every frame that passes through it, batched into one NATS
	// message per interval, in the rawarchive format. It is the opt-in source for a
	// raw-frame archive and does nothing unless registered.
	//
	// Handle copies the frame and returns at once; the frame path never blocks and
	// takes no lock beyond the queue channel. A full queue drops frames from the
	// archive, counted and reported, and leaves the live path alone.
	RawArchive struct {
		pub       rawPublisher
		subject   string
		instance  string
		interval  time.Duration
		maxBytes  int
		now       func() time.Time
		start     time.Time // first interval, fixed before the batcher starts
		queue     chan rawItem
		stop      chan struct{}
		done      chan struct{}
		stopOnce  sync.Once
		stopped   atomic.Bool
		dropped   atomic.Uint64 // since the last message
		processed atomic.Uint64 // items the batcher has taken from the queue; tests wait on it
		lastError atomic.Int64  // unix nanos of the last publish error logged

		droppedCounter   prometheus.Counter
		publishedCounter prometheus.Counter
		recordsCounter   prometheus.Counter
	}

	// RawArchiveOption configures a RawArchive at construction.
	RawArchiveOption func(*RawArchive)

	rawPublisher interface {
		PublishMsg(m *nats.Msg) error
	}

	// rawItem is a frame copied out of the decode path. Beast frames are pooled and
	// released as soon as Handle returns, so their bytes live in the item.
	rawItem struct {
		at   time.Time
		typ  uint8
		tile uint8
		n    uint8
		raw  [rawArchiveBeastMaxLen]byte
		body []byte // avr and sbs1, which are not pooled
	}
)

// WithRawArchiveInterval sets how often a batch is published. It must divide a minute.
func WithRawArchiveInterval(d time.Duration) RawArchiveOption {
	return func(r *RawArchive) { r.interval = d }
}

// WithRawArchiveMaxBytes sets the body size at which a batch is published before its interval ends.
func WithRawArchiveMaxBytes(n int) RawArchiveOption {
	return func(r *RawArchive) { r.maxBytes = n }
}

// WithRawArchiveInstance names this process in the batch headers. The default is the
// hostname plus a random suffix, so a restart is visible to the consumer.
func WithRawArchiveInstance(name string) RawArchiveOption {
	return func(r *RawArchive) { r.instance = name }
}

// WithRawArchiveCounters exports frames dropped from the queue, messages published and
// records published. Any may be nil.
func WithRawArchiveCounters(dropped, published, records prometheus.Counter) RawArchiveOption {
	return func(r *RawArchive) {
		r.droppedCounter = dropped
		r.publishedCounter = published
		r.recordsCounter = records
	}
}

func withRawArchiveClock(now func() time.Time) RawArchiveOption {
	return func(r *RawArchive) { r.now = now }
}

func withRawArchiveQueueLen(n int) RawArchiveOption {
	return func(r *RawArchive) { r.queue = make(chan rawItem, n) }
}

// NewRawArchive returns the middleware publishing on subject through pub, and starts
// its batcher. It returns an error for an interval that does not divide a minute or a
// size cap under RawArchiveMinMaxBytes.
func NewRawArchive(pub rawPublisher, subject string, opts ...RawArchiveOption) (*RawArchive, error) {
	if nil == pub {
		return nil, errors.New("raw archive: no publisher")
	}
	if subject == "" {
		return nil, errors.New("raw archive: no subject")
	}
	r := &RawArchive{
		pub:      pub,
		subject:  subject,
		interval: RawArchiveDefaultInterval,
		maxBytes: RawArchiveDefaultMaxBytes,
		now:      time.Now,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	for _, opt := range opts {
		opt(r)
	}
	if nil == r.queue {
		r.queue = make(chan rawItem, rawArchiveQueueLen)
	}
	if !rawarchive.ValidInterval(r.interval) {
		return nil, fmt.Errorf("raw archive: interval %v must be whole milliseconds dividing one minute", r.interval)
	}
	if r.maxBytes < RawArchiveMinMaxBytes {
		return nil, fmt.Errorf("raw archive: max bytes %d is below %d", r.maxBytes, RawArchiveMinMaxBytes)
	}
	if r.instance == "" {
		host, err := os.Hostname()
		if err != nil || host == "" {
			host = "ingest"
		}
		r.instance = host + "-" + randstr.RandString(6)
	}
	r.start = rawarchive.IntervalStart(r.now(), r.interval)
	go r.run()
	log.Info().
		Str("subject", r.subject).
		Str("instance", r.instance).
		Dur("interval", r.interval).
		Int("maxBytes", r.maxBytes).
		Msg("Including Raw Archive")
	return r, nil
}

// Instance is the name this process puts in its batch headers.
func (r *RawArchive) Instance() string { return r.instance }

// String names the middleware.
func (r *RawArchive) String() string { return "Raw Archive" }

// HealthCheckName names the middleware in health checks.
func (r *RawArchive) HealthCheckName() string { return "Raw Archive" }

// HealthCheck always reports healthy: the batcher drops rather than stalls.
func (r *RawArchive) HealthCheck() bool { return true }

// Handle copies the frame into the queue and returns it unchanged.
func (r *RawArchive) Handle(fe *tracker.FrameEvent) tracker.Frame {
	if nil == fe {
		return nil
	}
	frame := fe.Frame()
	if r.stopped.Load() {
		// Nothing drains the queue after Stop; the frame goes on to the tracker only.
		return frame
	}
	item, ok := r.newItem(frame, fe.Source())
	if !ok {
		return frame
	}
	select {
	case r.queue <- item:
	default:
		r.dropped.Add(1)
		if nil != r.droppedCounter {
			r.droppedCounter.Inc()
		}
	}
	return frame
}

func (r *RawArchive) newItem(frame tracker.Frame, source *tracker.FrameSource) (rawItem, bool) {
	item := rawItem{at: r.now(), tile: rawarchive.TileUnknown}
	switch f := frame.(type) {
	case *beast.Frame:
		raw := f.Raw()
		if len(raw) > len(item.raw) {
			return item, false
		}
		item.n = uint8(copy(item.raw[:], raw))
		item.typ = rawarchive.TypeBeast
		item.tile = sourceTile(source)
	case *mode_s.Frame:
		item.body = f.Raw()
		item.typ = rawarchive.TypeAVR
		item.tile = sourceTile(source)
	case *sbs1.Frame:
		// An SBS line carries the aircraft's position; the consumer tiles it from that.
		item.body = f.Raw()
		item.typ = rawarchive.TypeSBS1
	default:
		return item, false
	}
	return item, true
}

// sourceTile is the tile of the feeder's reference position, or TileUnknown.
func sourceTile(source *tracker.FrameSource) uint8 {
	if nil == source || nil == source.RefLat || nil == source.RefLon {
		return rawarchive.TileUnknown
	}
	return rawarchive.TileNumber(tile_grid.LookupTile(*source.RefLat, *source.RefLon))
}

func (item *rawItem) data() []byte {
	if item.typ == rawarchive.TypeBeast {
		return item.raw[:item.n]
	}
	return item.body
}

// Stop publishes what is queued and buffered, then returns. The tracker stops its
// middleware while the producers are still winding down, so Handle may be called
// again afterwards; those frames are not archived.
func (r *RawArchive) Stop() {
	r.stopOnce.Do(func() {
		r.stopped.Store(true)
		close(r.stop)
	})
	<-r.done
}

// run is the batcher: it drains the queue into the current interval's buffer and
// publishes at each aligned interval boundary, or earlier when the buffer reaches the
// size cap. Intervals divide the minute and start on whole multiples of themselves
// past it, so no batch ever straddles a minute.
func (r *RawArchive) run() {
	defer close(r.done)
	enc := rawarchive.NewEncoder(r.maxBytes + 1024)
	start := r.start
	timer := time.NewTimer(r.untilEnd(start))
	defer timer.Stop()

	add := func(item rawItem) {
		if !item.at.Before(start.Add(r.interval)) {
			// The frame belongs to a later interval than the one open: close this one first.
			r.publish(enc, start)
			start = rawarchive.IntervalStart(item.at, r.interval)
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(r.untilEnd(start))
		}
		var offset uint16
		if d := item.at.Sub(start); d > 0 {
			ms := d / time.Millisecond
			if ms > rawarchive.MaxOffset {
				ms = rawarchive.MaxOffset
			}
			offset = uint16(ms)
		}
		if err := enc.Append(item.typ, item.tile, offset, item.data()); err != nil {
			r.dropped.Add(1)
			if nil != r.droppedCounter {
				r.droppedCounter.Inc()
			}
			return
		}
		if enc.Len() >= r.maxBytes {
			r.publish(enc, start)
		}
	}

	for {
		select {
		case item := <-r.queue:
			add(item)
			r.processed.Add(1)
		case <-timer.C:
			// Frames already queued were stamped before the boundary: file them in the
			// interval that is closing rather than at offset 0 of the next one.
			r.drain(add)
			if !r.now().Before(start.Add(r.interval)) {
				r.publish(enc, start)
				start = rawarchive.IntervalStart(r.now(), r.interval)
			}
			timer.Reset(r.untilEnd(start))
		case <-r.stop:
			r.drain(add)
			r.publish(enc, start)
			return
		}
	}
}

// drain passes everything in the queue to add without waiting for more.
func (r *RawArchive) drain(add func(rawItem)) {
	for {
		select {
		case item := <-r.queue:
			add(item)
			r.processed.Add(1)
		default:
			return
		}
	}
}

func (r *RawArchive) untilEnd(start time.Time) time.Duration {
	d := start.Add(r.interval).Sub(r.now())
	if d < 0 {
		return 0
	}
	return d
}

// publish sends the buffer as one message and empties it. An empty buffer is sent too:
// it is the heartbeat. A batch that fails to publish is lost; its records, and the
// drop count it carried, are reported as dropped in the next message that gets through.
func (r *RawArchive) publish(enc *rawarchive.Encoder, start time.Time) {
	msg := nats.NewMsg(r.subject)
	dropped := r.dropped.Swap(0)
	rawarchive.SetHeader(msg, rawarchive.Header{
		Instance: r.instance,
		Start:    start,
		Dropped:  dropped,
	})
	msg.Data = enc.Bytes()
	records := enc.Records()
	// nats.go encodes the message before PublishMsg returns, so the buffer can be reused.
	if err := r.pub.PublishMsg(msg); err != nil {
		r.dropped.Add(dropped + uint64(records))
		if nil != r.droppedCounter && records > 0 {
			r.droppedCounter.Add(float64(records))
		}
		now := time.Now().UnixNano()
		last := r.lastError.Load()
		if now-last >= int64(rawArchiveErrorLogEvery) && r.lastError.CompareAndSwap(last, now) {
			log.Error().Err(err).Str("subject", r.subject).Msg("Raw Archive: failed to publish batch")
		}
	} else {
		if nil != r.publishedCounter {
			r.publishedCounter.Inc()
		}
		if nil != r.recordsCounter && records > 0 {
			r.recordsCounter.Add(float64(records))
		}
	}
	enc.Reset()
}
