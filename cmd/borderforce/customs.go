package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"plane.watch/lib/export"
	"plane.watch/lib/nats_io"
	"plane.watch/lib/producer"
	"plane.watch/lib/stunnel"
	"plane.watch/lib/tracker"
)

type (
	Manifest struct {
		hostPort string

		certPath, keyPath string

		trk *tracker.Tracker
		log zerolog.Logger

		listener *stunnel.Listener

		feeders   map[string]export.Feeder
		muFeeders sync.Mutex

		feederFetchTicker *time.Ticker

		natsServer *nats_io.Server
		natsURL    string
	}

	Option func(manifest *Manifest)
)

var (
	MissingOption = errors.New("option is required")
)

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

	// setup our nats connection, get our initial feeder list and start the refresher
	manifest.natsServer, err = nats_io.NewServer(
		nats_io.WithConnections(false, true),
		nats_io.WithServer(manifest.natsURL, "borderforce-atc-client"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to setup nats connection: %w", err)
	}

	// and fetch our initial feeder list - this fails if nats is not up and running
	if err = manifest.fetchFeeders(); err != nil {
		return nil, fmt.Errorf("failed to fetch feeders: %w", err)
	}

	// and now refresh our api keyed feeder list every 5 minutes
	// TODO: kick feeders with API Keys that were once valid but no longer so!
	manifest.feederFetchTicker = time.NewTicker(5 * time.Minute)
	go func() {
		if err = manifest.fetchFeeders(); err != nil {
			manifest.log.Error().Err(err).Msg("failed to update feeder api list")
		}
	}()

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

	go func() {
		err = manifest.listener.Listen(ctx)
		if err != nil {
			manifest.log.Error().Err(err).Msg("failed to listen for plane.watch beast")
		}
	}()

	return manifest, nil
}

func (m *Manifest) authenticator(apiKey string) (bool, error) {
	m.muFeeders.Lock()
	defer m.muFeeders.Unlock()
	if _, ok := m.feeders[apiKey]; ok {
		return true, nil
	}
	return false, nil
}

func (m *Manifest) handler(conn net.Conn, apiKey string) error {
	m.muFeeders.Lock()
	feeder, ok := m.feeders[apiKey]
	m.muFeeders.Unlock()

	if !ok {
		return fmt.Errorf("failed to get feeder info for authorised feeder key")
	}

	prometheusInputBeastFrames := promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "border-force",
		Subsystem: "beast",
		Name:      "input-total",
		Help:      "The total number of beast frames processed.",
		ConstLabels: map[string]string{
			"feeder_id": strconv.FormatInt(int64(feeder.Id), 10),
		},
	})

	// TODO: handle stats updates to ATC
	// TODO: jam stats into clicks for received packets per second (needs to be done before dedupe)
	// TODO: Jam distance/heading info into clicks so we can produce a coverage map
	p := producer.New(
		producer.WithConnection(conn),
		producer.WithType(producer.Beast),
		producer.WithOriginName(feeder.Label),
		producer.WithReferenceLatLon(feeder.Latitude, feeder.Longitude),
		producer.WithSourceTag(feeder.FeederCode),
		producer.WithPrometheusCounters(nil, prometheusInputBeastFrames, nil))

	m.trk.AddProducer(p)

	return nil
}

func (m *Manifest) fetchFeeders() error {
	emptyMap := map[string]string{}
	ret, err := m.natsServer.Request(export.NatsApiFeederListV1, nil, emptyMap, time.Second)
	if err != nil {
		return fmt.Errorf("failed to fetch feeder list data from atc api: %w", err)
	}
	json := jsoniter.ConfigFastest

	feeders := make(export.Feeders, 0, 1000)

	err = json.Unmarshal(ret, &feeders)
	if err != nil {
		return fmt.Errorf("failed to decode feeder list: %w", err)
	}

	m.muFeeders.Lock()
	defer m.muFeeders.Unlock()
	m.log.Info().
		Int("prev-feeder-count", len(m.feeders)).
		Int("new-feeder-count", len(m.feeders)).
		Msg("Updating Feeders")

	m.feeders = make(map[string]export.Feeder, len(feeders))
	for _, feeder := range feeders {
		m.feeders[feeder.ApiKey.String()] = feeder
	}

	return nil
}
