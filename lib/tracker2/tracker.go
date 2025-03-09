package tracker2

import (
	"fmt"
	"plane.watch/lib/tracker2/beast"
	"plane.watch/lib/tracker2/modes2"
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
		f, err := beast.Decode(msg)
		if err != nil {
			continue
		}

		switch f.DF {
		case modes2.DF00ShortAirToAir:
			df0, _ := f.DecodeDF0()
			fmt.Printf("Aircraft [icao] %06X, onGround=%t altitude=%d\n", df0.ICAO, df0.OnGround, df0.Altitude)
		case modes2.DF04SurveillanceAltitudeReply:
		case modes2.DF05SurveillanceIdentReply:
		case modes2.DF11ModeSAllCallReply:
		case modes2.DF16LongAirToAir:
		case modes2.DF17ADSBExtendedSquitter:
		case modes2.DF18ADSBSupplementary:
		case modes2.DF19ADSBMilitary:
		case modes2.DF20CommB:
		case modes2.DF21CommB:
		case modes2.DF22Military:
		case modes2.DF24CommD:
		}
	}
}

func (pt *PlaneTracker) AddAvrSource()  {}
func (pt *PlaneTracker) AddSbs1Source() {}
