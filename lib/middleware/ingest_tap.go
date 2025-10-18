package middleware

import (
	"strconv"
	"sync"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
	"plane.watch/lib/nats_io"
	"plane.watch/lib/randstr"
	"plane.watch/lib/tracker"
	"plane.watch/lib/tracker/beast"
	"plane.watch/lib/tracker/mode_s"
	"plane.watch/lib/tracker/sbs1"
)

const NatsAPIv1PwIngestTap = "v1.pw-ingest.tap" //nolint:gosec

type (
	IngestTap struct {
		head, tail *condition
		queue      chan *tracker.FrameEvent
		queueWg    sync.WaitGroup
		natsQueue  string
		natsServer *nats_io.Server
		sub        *nats.Subscription
	}

	condition struct {
		nextItem *condition
		prevItem *condition
		queue    string
		apiKey   string
		icao     uint32
	}
)

func NewIngestTap(natsServer *nats_io.Server) tracker.Middleware {
	tap := &IngestTap{
		head:       nil,
		tail:       nil,
		queue:      make(chan *tracker.FrameEvent, 100),
		natsServer: natsServer,
	}
	for i := 0; i < 8; i++ {
		tap.queueWg.Add(1)
		go tap.processQueue()
	}
	var err error
	tap.natsQueue = "pw-ingest-tap-" + randstr.RandString(20)
	tap.sub, err = tap.natsServer.SubscribeReply(NatsAPIv1PwIngestTap, tap.natsQueue, tap.requestHandler)
	if err != nil {
		return nil
	}
	log.Info().Msg("Including Ingest Tap")
	return tap
}

func (tap *IngestTap) Close() {
	close(tap.queue)
	tap.queueWg.Wait()
	err := tap.sub.Unsubscribe()
	if err != nil {
		log.Error().Err(err).Msg("Failed to unsubscribe from nats")
	}
}

func (tap *IngestTap) processQueue() {
	for frame := range tap.queue {
		for c := tap.head; c != nil; c = c.nextItem {
			if c.match(frame) {
				tap.send(c.queue, frame)
			}
		}
	}
	tap.queueWg.Done()
}

func (tap *IngestTap) requestHandler(msg *nats.Msg) {
	if nil == msg {
		return
	}
	action := msg.Header.Get("action")
	apiKey := msg.Header.Get("api-key")
	icao := msg.Header.Get("icao") // formatted like 7C1234
	returnSubject := msg.Header.Get("subject")
	log.Info().
		Str("action", action).
		Str("apiKey", apiKey).
		Str("icao", icao).
		Str("returnSubject", returnSubject).
		Msg("Network Tap Request")

	var err error
	var uIcao uint64

	if icao != "" {
		uIcao, err = strconv.ParseUint(icao, 16, 32)
		if nil != err {
			log.Error().Err(err).Str("icao", icao).Msg("Failed to convert ICAO string into a uint")
		}
	}

	c := &condition{
		queue:  returnSubject,
		apiKey: apiKey,
		icao:   uint32(uIcao),
	}

	switch action {
	case "add":
		tap.append(c)
	case "remove":
		tap.remove(c)
	}
	_ = msg.Respond([]byte("Ack"))
}

func (tap *IngestTap) String() string {
	return "Ingest Tap"
}

func (tap *IngestTap) HealthCheckName() string {
	return "Ingest Tap"
}

func (tap *IngestTap) HealthCheck() bool {
	return true
}

func (tap *IngestTap) Handle(frame *tracker.FrameEvent) tracker.Frame {
	if tap.head != nil {
		tap.queue <- frame
	}
	return frame.Frame()
}

func (tap *IngestTap) send(subject string, fe *tracker.FrameEvent) {
	headers := make(map[string]string)
	headers["tag"] = fe.Source().Tag

	var msg []byte
	switch tFrame := fe.Frame().(type) {
	case *beast.Frame:
		headers["type"] = "beast"
		msg = tFrame.Raw()
	case *mode_s.Frame:
		headers["type"] = "avr"
		msg = tFrame.Raw()
	case *sbs1.Frame:
		headers["type"] = "sbs1"
		msg = tFrame.Raw()
	default:
		log.Error().Msg("Unknown frame type, cannot send")
		return
	}

	if err := tap.natsServer.PublishWithHeaders(subject, msg, headers); nil != err {
		log.Error().Err(err).Msg("Failed to send tap data")
	}
}

func (tap *IngestTap) append(c *condition) {
	if nil == tap.head {
		tap.head = c
		tap.tail = c
		return
	}
	c.prevItem = tap.tail
	tap.tail.nextItem = c
	tap.tail = c
}

func (tap *IngestTap) remove(c *condition) {
	if nil == c {
		return
	}
	for cond := tap.head; nil != cond; cond = cond.nextItem {
		if cond.queue == c.queue && cond.apiKey == c.apiKey && cond.icao == c.icao {
			// removal required
			if tap.head == cond {
				tap.head = cond.nextItem
			}
			if tap.tail == cond {
				tap.tail = cond.prevItem
			}
			if nil != cond.prevItem {
				cond.prevItem.nextItem = cond.nextItem
			}
			if nil != cond.nextItem {
				cond.nextItem.prevItem = cond.prevItem
			}
			// and finally unlink it
			cond.nextItem = nil
			cond.prevItem = nil
		}
	}
}

func (c *condition) match(fe *tracker.FrameEvent) bool {
	if nil == fe {
		return false
	}
	isMatchAPIKey := true
	if c.apiKey != "" {
		source := fe.Source()
		if nil == source {
			return false
		}
		isMatchAPIKey = source.Tag == c.apiKey
	}

	isMatchIcao := true
	if c.icao != 0 {
		frame := fe.Frame()
		if nil == frame {
			return false
		}
		isMatchIcao = frame.Icao() == c.icao
	}

	return isMatchAPIKey && isMatchIcao
}
