package main

import (
	"github.com/rs/zerolog/log"
	"os"
	"os/signal"
	"plane.watch/lib/export"
	"plane.watch/lib/monitoring"
	"plane.watch/lib/nats_io"
	"syscall"
	"time"
)

type (
	PwWsBroker struct {
		input source
		PwWsBrokerWeb
		exitChan chan bool
	}

	source interface {
		configure() error
		setProcessMessage(processMessage)
		consumeAll(chan bool)
		close()
		monitoring.HealthCheck
	}
	processMessage func(highLow string, loc *export.PlaneLocation)
)

func NewPlaneWatchWebSocketBroker(input source, natsRpc *nats_io.Server, httpAddr, cert, certKey string, serveTestWeb bool, sendTickDuration, snapshotMaxAge time.Duration) (*PwWsBroker, error) {

	return &PwWsBroker{
		input: input,
		PwWsBrokerWeb: PwWsBrokerWeb{
			natsRpc:          natsRpc,
			Addr:             httpAddr,
			ServeTest:        serveTestWeb,
			cert:             cert,
			certKey:          certKey,
			sendTickDuration: sendTickDuration,
			snapshotMaxAge:   snapshotMaxAge,
		},
		exitChan: make(chan bool),
	}, nil
}

func (b *PwWsBroker) Setup() error {
	if err := b.input.configure(); nil != err {
		return err
	}
	if err := b.configureWeb(); nil != err {
		return err
	}

	b.input.setProcessMessage(func(highLow string, loc *export.PlaneLocation) {
		prometheusIncomingMessages.WithLabelValues(highLow).Inc()
		tile := loc.TileLocation + highLow
		b.clients.SendLocationUpdate(highLow, tile, loc)
	})

	monitoring.AddHealthCheck(b.input)
	monitoring.AddHealthCheck(&b.PwWsBrokerWeb)
	return nil
}

func (b *PwWsBroker) Run() {
	go b.listenAndServe(b.exitChan)
	go b.input.consumeAll(b.exitChan)
}

func (b *PwWsBroker) Wait() {
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	select {
	case <-b.exitChan:
		log.Debug().Msg("We are exiting")
	case <-sc:
		log.Debug().Msg("Kill Signal Received")
	}
}

func (b *PwWsBroker) Close() {
	if err := b.httpServer.Close(); nil != err {
		log.Error().Err(err).Msg("Failed to close web server cleanly")
	}
	b.input.close()
}
