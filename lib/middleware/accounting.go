package middleware

import (
	"sync"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/rs/zerolog"
	"plane.watch/lib/export"
	"plane.watch/lib/nats_io"
	"plane.watch/lib/tracker"
)

// Accounting keeps track of stats for feeders and sends updates back to ATC
// TODO: Put history of seen planes per feeder into click house so that the user can see which planes they track
// TODO: keep track of distance and altitude from LAT/LON so we can generate fancy graphs for each feeder

type (
	Accounting struct {
		natsServer *nats_io.Server

		handleQueue     chan *tracker.FrameSource
		exitQueueWaiter sync.WaitGroup

		stats map[string]feederStat

		atcUpdateQueue chan feederStat

		log zerolog.Logger
	}

	feederStat struct {
		frameCount    uint64
		lastSeen      time.Time
		lastAtcUpdate time.Time
	}

	AccountingOption func(*Accounting)
)

func WithNats(ns *nats_io.Server) AccountingOption {
	return func(accounting *Accounting) {
		accounting.natsServer = ns
	}
}

func NewAccounting(opts ...AccountingOption) *Accounting {
	a := &Accounting{}
	// set defaults...

	for _, opt := range opts {
		opt(a)
	}

	a.handleQueue = make(chan *tracker.FrameSource, 1000)
	a.atcUpdateQueue = make(chan feederStat, 1000)

	// config check
	if a.natsServer == nil {
		panic("You need to specify a NATS server")
	}

	go a.queueHandler()
	go a.atcUpdateQueueHandler()

	return a
}

func (a *Accounting) Handle(event *tracker.FrameEvent) tracker.Frame {
	a.handleQueue <- event.Source()

	return event.Frame()
}

func (a *Accounting) queueHandler() {
	a.exitQueueWaiter.Add(1)
	for item := range a.handleQueue {
		stat := a.stats[item.Tag]
		stat.frameCount++
		stat.lastSeen = time.Now()

		if stat.lastSeen.After(stat.lastAtcUpdate.Add(time.Minute)) {
			a.atcUpdateQueue <- stat
			stat.lastAtcUpdate = stat.lastSeen
		}
		a.stats[item.Tag] = stat
	}
	a.exitQueueWaiter.Done()
}

func (a *Accounting) atcUpdateQueueHandler() {
	a.exitQueueWaiter.Add(1)
	json := jsoniter.ConfigFastest
	for stat := range a.atcUpdateQueue {
		feederUpdates := []export.FeederUpdate{
			{
				ApiKey:   "",
				LastSeen: stat.lastSeen,
			},
		}
		data, err := json.Marshal(feederUpdates)
		if err != nil {
			a.log.Error().Err(err).Msg("failed to encode feeder update")
			continue
		}
		_, err = a.natsServer.Request(export.NatsApiFeederStatsUpdateV1, data, map[string]string{}, time.Second)
		if err != nil {
			a.log.Error().Err(err).Msg("failed to update feeder stats")
			return
		}
	}
	a.exitQueueWaiter.Done()
}

func (a *Accounting) String() string {
	return "Packet Accounting"
}

func (a *Accounting) HealthCheckName() string {
	return "Packet Accounting"
}

func (a *Accounting) HealthCheck() bool {
	// TODO: actually implement a health check
	return true
}

func (a *Accounting) Stop() {
	close(a.handleQueue)
	close(a.atcUpdateQueue)
	a.exitQueueWaiter.Wait()
}
