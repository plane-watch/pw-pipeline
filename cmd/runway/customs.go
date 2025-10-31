package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"plane.watch/lib/feedercache"
	"plane.watch/lib/nats_io"
	"plane.watch/lib/producer"
	"plane.watch/lib/stunnel"
	"plane.watch/lib/tracker"
)

type (
	Manifest struct {

		// hostPort contains the IP and port to listen on, in the same format as the address argument to net.Listen
		hostPort string

		// certPath contains the file to use for the server certificate
		certPath string

		// keyPath contains the file to use for the server certificate's private key
		keyPath string

		trk      *tracker.Tracker
		log      zerolog.Logger
		listener *stunnel.Listener

		natsServer *nats_io.Server
		natsURL    string

		feeders *feedercache.FeederCache
	}

	Option func(manifest *Manifest)
)

var (
	MissingOption = errors.New("option is required")

	prometheusConnectedBeastFeeders = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "runway",
		Subsystem: "beast",
		Name:      "feeders-connected",
		Help:      "The total number of beast feeders connected.",
	})
)

func WithFeederCache(feeders *feedercache.FeederCache) Option {
	return func(manifest *Manifest) {
		manifest.feeders = feeders
	}
}

func WithListenHostPort(listen string) Option {
	return func(manifest *Manifest) {
		manifest.hostPort = listen
	}
}

func WithTLSCertificate(cert, key string) Option {
	return func(manifest *Manifest) {
		manifest.certPath = cert
		manifest.keyPath = key
	}
}

func WithTracker(trk *tracker.Tracker) Option {
	return func(manifest *Manifest) {
		manifest.trk = trk
	}
}

func WithNatsURL(natsURL string) Option {
	return func(manifest *Manifest) {
		manifest.natsURL = natsURL
	}
}

func ListenForIncomingPlaneWatchBeast(ctx context.Context, opts ...Option) (*Manifest, error) {
	// listen for incoming connection, validate them and then accept incoming beast
	var err error

	// create our Manifest and apply our options to it
	manifest := &Manifest{
		log: log.With().Str("listener", "beast").Logger(),
	}
	for _, opt := range opts {
		opt(manifest)
	}

	// let's do some sanity checking...
	if manifest.trk == nil {
		return nil, fmt.Errorf("%w: You need to configure the *tracker.Tracker", MissingOption)
	}
	if manifest.natsURL == "" {
		return nil, fmt.Errorf("%w: Please specify the Nats URL (sink)", MissingOption)
	}
	if manifest.feeders == nil {
		return nil, fmt.Errorf("%w: You need to configure the *feederauth.FeederCache", MissingOption)
	}

	// setup our nats connection
	manifest.natsServer, err = nats_io.NewServer(
		nats_io.WithConnections(false, true),
		nats_io.WithServer(manifest.natsURL, "runway-atc-client-BEAST"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to setup nats connection: %w", err)
	}

	// now let's start listening for connections!

	manifest.listener, err = stunnel.NewListener(
		stunnel.WithHostPort(manifest.hostPort),
		stunnel.WithTLSCertificate(manifest.certPath, manifest.keyPath),
		stunnel.WithConnectionHandler(manifest.handler),
		stunnel.WithAuthenticator(manifest.authenticator),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to setup stunnel listener: %w", err)
	}

	err = manifest.listener.Listen(ctx)
	if err != nil {
		manifest.log.Error().Err(err).Msg("failed to listen for plane.watch beast")
	}

	return manifest, nil
}

func (m *Manifest) authenticator(apiKey string) (bool, error) {
	return m.feeders.Authenticate(apiKey, feedercache.BEAST)
}

func (m *Manifest) handler(conn net.Conn, apiKey string) error {

	feeder, err := m.feeders.Get(apiKey)
	if err != nil {
		return fmt.Errorf("failed to get feeder for %s: %w", apiKey, err)
	}
	m.feeders.SetConnected(apiKey, feedercache.BEAST)

	// register prom metrics
	prometheusInputBeastFrames := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "runway",
		Subsystem: "beast",
		Name:      "input-total",
		Help:      "The total number of beast frames processed.",
		ConstLabels: map[string]string{
			"feeder_id":    strconv.FormatInt(int64(feeder.Id), 10),
			"feeder_label": feeder.Label,
			"feeder_user":  feeder.User,
		},
	})
	err = prometheus.Register(prometheusInputBeastFrames)
	if err != nil {
		return fmt.Errorf("failed to register prometheus counter: %w", err)
	}
	prometheusConnectedBeastFeeders.Inc()

	// TODO: handle stats updates to ATC
	// TODO: jam stats into clicks for received packets per second (needs to be done before dedupe)
	// TODO: Jam distance/heading info into clicks so we can produce a coverage map
	p := producer.New(
		producer.WithConnection(conn),
		producer.WithType(producer.Beast),
		producer.WithOriginName(feeder.ApiKey.String()),
		producer.WithReferenceLatLon(feeder.Latitude, feeder.Longitude),
		producer.WithSourceTag(feeder.FeederCode),
		producer.WithPrometheusCounters(nil, prometheusInputBeastFrames, nil),
		producer.WithPoisonPill(
			func() bool {
				if !m.feeders.IsValid(apiKey) {
					m.log.Warn().Msg("feeder api key no longer valid")
					return true // take poison pill
				}
				return false
			},
			time.Second*5,
		),
		producer.WithCleanUpTasks(
			// set feeder disconnected
			func() error {
				m.feeders.SetDisconnected(apiKey, feedercache.BEAST)
				return nil
			},
			// unregister prom metrics
			func() error {
				_ = prometheus.Unregister(prometheusInputBeastFrames)
				prometheusConnectedBeastFeeders.Dec()
				return nil
			},
		),
	)

	m.trk.AddProducer(p)

	return nil
}
