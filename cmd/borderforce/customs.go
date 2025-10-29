package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

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
		log: log.With().Str("Section", "IncomingBeast").Logger(),
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
		nats_io.WithServer(manifest.natsURL, "borderforce-atc-client"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to setup nats connection: %w", err)
	}

	// now let's start listening for connections!

	manifest.listener, err = stunnel.New(
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

	// TODO(mikenye): This causes a panic if the client reconnects.
	//                `panic: duplicate metrics collector registration attempted`
	//                Replacing "promauto.NewCounter" with "prometheus.NewCounter" stops panic,
	//                but I'm unsure if this is the correct fix.
	//prometheusInputBeastFrames := promauto.NewCounter(prometheus.CounterOpts{
	//	Namespace: "border-force",
	//	Subsystem: "beast",
	//	Name:      "input-total",
	//	Help:      "The total number of beast frames processed.",
	//	ConstLabels: map[string]string{
	//		"feeder_id": strconv.FormatInt(int64(feeder.Id), 10),
	//	},
	//})

	// TODO: handle stats updates to ATC
	// TODO: jam stats into clicks for received packets per second (needs to be done before dedupe)
	// TODO: Jam distance/heading info into clicks so we can produce a coverage map
	p := producer.New(
		producer.WithConnection(conn),
		producer.WithType(producer.Beast),
		producer.WithOriginName(feeder.ApiKey.String()),
		producer.WithReferenceLatLon(feeder.Latitude, feeder.Longitude),
		producer.WithSourceTag(feeder.FeederCode),
		// TODO(mikenye): re-enable when panic fixed
		//producer.WithPrometheusCounters(nil, prometheusInputBeastFrames, nil),
		producer.WithPoisonPill(
			func() bool {
				if !m.feeders.IsValid(apiKey) {
					log.Warn().Msg("feeder api key no longer valid")
					return true // take poison pill
				}
				return false
			},
			time.Second*5,
		),
		producer.WithCleanUpTasks(
			func() error {
				m.feeders.SetDisconnected(apiKey, feedercache.BEAST)
				return nil
			},
		),
	)

	m.trk.AddProducer(p)

	return nil
}
