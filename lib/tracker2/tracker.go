package tracker2

import (
	"sync"
	"time"
)

type (
	PlaneTracker struct {
		emitFrequency time.Duration

		planes sync.Map
	}

	planeCmd interface {
		perform(plane)
	}

	plane struct {
		icao uint32

		emitFrequency time.Duration
		cmd           chan planeCmd
	}

	Option func(tracker *PlaneTracker)
)

func New(opts ...Option) *PlaneTracker {
	pt := &PlaneTracker{
		emitFrequency: time.Second,
	}

	for _, opt := range opts {
		opt(pt)
	}

	return pt
}

func WithEmitFrequency(d time.Duration) Option {
	return func(tracker *PlaneTracker) {
		tracker.emitFrequency = d
	}
}

func (pt *PlaneTracker) getICAO(icao uint32) *plane {
	p := &plane{icao: icao}
	ret, loaded := pt.planes.LoadOrStore(icao, p)
	if !loaded {
		// init our plane
		p.cmd = make(chan planeCmd, 10)
	}

	return ret.(*plane)
}

func (pt *PlaneTracker) AddBeastSourceChannel(msgs chan []byte) {
	for msg := range msgs {

	}
}

func (pt *PlaneTracker) AddAvrSource()  {}
func (pt *PlaneTracker) AddSbs1Source() {}
