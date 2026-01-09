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
	Option func(*Filter)
	Filter struct {
		events chan tracker.Event
		list   *forgetfulmap.ForgetfulSyncMap

		dedupeCounter prometheus.Counter
	}
)

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
	var key string

	// We use the raw string representation of the squitter body
	// This means that we de-dupe across both beast and mode_s, additionally removing
	// variable data such as timestamps, rssi, etc.
	switch ft := (frame).(type) {
	case *beast.Frame:
		key = ft.RawString()
	case *mode_s.Frame:
		key = ft.RawString()
	case *sbs1.Frame:
		// todo: investigate better dedupe detection for sbs1
		key = string(ft.Raw())
	default:
	}
	if f.list.HasKeyStr(key) {
		return nil
	}
	f.list.AddKeyStr(key)

	// we have a deduped frame, do send it to the dedupe queue
	if nil != f.dedupeCounter {
		f.dedupeCounter.Inc()
	}
	return frame
}
func (f *Filter) String() string {
	return "Dedupe"
}
