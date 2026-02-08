package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog/log"
	"plane.watch/lib/nats_io"
)

type (
	natsIoRouter struct {
		natsUrl  string
		n        *nats_io.Server
		doneChan chan bool
	}
)

func newNatsIoRouter(natsUrl string) *natsIoRouter {
	if "" == natsUrl {
		return nil
	}
	nr := &natsIoRouter{
		natsUrl:  natsUrl,
		doneChan: make(chan bool),
	}

	return nr
}

func (nr *natsIoRouter) connect() error {
	var err error

	nr.n, err = nats_io.NewServer(nats_io.WithServer(nr.natsUrl, "pw_router"))
	if nil != err {
		log.Error().
			Err(err).
			Str("MQ", "nats.io").
			Msg("Unable to determine configuration from URL")
		return err
	}
	nr.n.DroppedCounter(promauto.NewCounter(prometheus.CounterOpts{
		Name: "pw_router_nats_dropped_message_err_count",
		Help: "The total number slow consumer dropped message errors.",
	}))
	return err
}

func (nr *natsIoRouter) listen(subject string, incomingMessages chan []byte) error {
	ch, err := nr.n.Subscribe(subject)
	if nil != err {
		return err
	}

	go func() {
		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					log.Error().Msg("failed to get message from nats.io nicely")
					return
				}

				incomingMessages <- msg.Data
			case <-nr.doneChan:
				go func() {
					nr.n.Close()
					log.Debug().Msg("MQ Drained")
				}()
				return
			}
		}
	}()
	return nil
}

func (nr *natsIoRouter) publish(subject string, msg []byte) error {
	err := nr.n.Publish(subject, msg)
	if nil != err {
		log.Warn().Err(err).Str("mq", "nats").Msg("Failed to send update")
		return err
	}
	return nil
}

func (nr *natsIoRouter) close() {
	nr.doneChan <- true
}
func (nr *natsIoRouter) HealthCheckName() string {
	return "Nats"
}

func (nr *natsIoRouter) HealthCheck() bool {
	if nil == nr.n {
		return false
	}
	return nr.n.HealthCheck()
}
