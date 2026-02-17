package dedupe

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
	"plane.watch/lib/dedupe/forgetfulmap"
	"plane.watch/lib/tracker"
	"plane.watch/lib/tracker/beast"
	"plane.watch/lib/tracker/mode_s"
	"plane.watch/lib/tracker/sbs1"
)

/**
This package provides a way to deduplicate mode_s messages.

Consider a message a duplicate if we have seen it in the last minute
*/

type (
	// rawFrameKey is a compact, comparable copy of a Mode-S payload.
	// Mode-S messages are fixed-length at 7 or 14 bytes.
	rawFrameKey struct {
		n uint8
		b [14]byte
	}

	Option func(*Filter)
	Filter struct {
		events chan tracker.Event
		list   *forgetfulmap.ForgetfulSyncMap

		dedupeCounter prometheus.Counter
	}
)

func makeRawFrameKey(raw []byte) rawFrameKey {
	var key rawFrameKey
	key.n = uint8(len(raw)) // mode-s payloads are max 14 bytes
	copy(key.b[:], raw)
	return key
}

func (f *Filter) Stop() {
	f.list.Stop()
}

func WithDedupeCounter(dedupeCounter prometheus.Counter) Option {
	return func(f *Filter) {
		f.dedupeCounter = dedupeCounter
	}
}

func NewFilter(opts ...Option) *Filter {
	f := Filter{
		list: forgetfulmap.NewForgetfulSyncMap(forgetfulmap.WithOldAgeAfter(time.Second * 9)),
	}

	for _, opt := range opts {
		opt(&f)
	}

	return &f
}
func (f *Filter) HealthCheckName() string {
	return "Dedupe Filter"
}

func (f *Filter) HealthCheck() bool {
	log.Info().
		Str("what", "Dedupe Middleware").
		Int32("Num Entries", f.list.Len()).
		Msg("Health Check")

	return true
}

func (f *Filter) Handle(fe *tracker.FrameEvent) tracker.Frame {
	if nil == fe {
		return nil
	}
	frame := fe.Frame()
	if nil == frame {
		return nil
	}
	var key any

	// We use raw bytes of the squitter body as the key.
	// We de-dupe across both beast and mode_s, using only the Mode-S payload
	// which excludes variable data such as timestamps, rssi, etc.
	switch ft := (frame).(type) {
	case *beast.Frame:
		key = makeRawFrameKey(ft.AvrRaw())
	case *mode_s.Frame:
		key = makeRawFrameKey(ft.Raw())
	case *sbs1.Frame:
		// todo: investigate better dedupe detection for sbs1
		key = string(ft.Raw())
	default:
		return frame
	}
	if f.list.HasKey(key) {
		return nil
	}
	f.list.AddKey(key)

	// we have a deduped frame, do send it to the dedupe queue
	if nil != f.dedupeCounter {
		f.dedupeCounter.Inc()
	}
	return frame
}
func (f *Filter) String() string {
	return "Dedupe"
}
