package main

import (
	"context"
	"os"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
	"plane.watch/lib/dedupe"
	"plane.watch/lib/feederauth"
	"plane.watch/lib/logging"
	"plane.watch/lib/middleware"
	"plane.watch/lib/mlatbridge"
	"plane.watch/lib/monitoring"
	"plane.watch/lib/nats_io"
	"plane.watch/lib/setup"
	"plane.watch/lib/sink"
	"plane.watch/lib/tracker"
)

var (
	version                        = "dev"
	prometheusCounterFramesDecoded = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "pw_ingest",
		Name:      "num_decoded_frames",
		Help:      "The number of AVR frames decoded",
	})
	prometheusCounterFramesErrored = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "pw_ingest",
		Name:      "num_decode_errors",
		Help:      "The number of AVR frames decoded with errors",
	})
	prometheusCounterPlanesPurgedBeforeViable = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "pw_ingest",
		Name:      "num_planes_purged_before_viable",
		Help:      "The number aircraft that were purged before having received enough frames to be viable",
	})
	prometheusGaugeCurrentPlanes = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "pw_ingest",
		Name:      "current_tracked_planes_count",
		Help:      "The number of planes this instance is currently tracking",
	})
	prometheusOutputFrameDedupe = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "pw_ingest",
		Name:      "output_frame_dedupe_total",
		Help:      "The total number of deduped frames not output.",
	})
	prometheusStaleRejected = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "pw_ingest",
		Name:      "stale_updates_rejected_total",
		Help:      "Updates rejected because frame timestamp was older than stored value.",
	})
	prometheusAppVer = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "pw_ingest",
		Name:      "info",
		Help:      "Application Info/Metadata",
	}, []string{"version"})
)

func init() {
	prometheusAppVer.With(prometheus.Labels{"version": version}).Set(1)
}

func main() {
	app := cli.NewApp()

	app.Name = "Runway"
	app.Description = `This program acts as a server for multiple stunnel-based endpoints, ` +
		`authenticates the feeder based on API key (UUID) check against atc.plane.watch, ` +
		`routes data to feed-in containers.`
	app.Version = version

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Category: "Network",
			Name:     "listen-beast",
			Usage:    "Address and TCP port to listen on for BEAST connections",
			Value:    ":12345", // insert Spaceballs joke here
			EnvVars:  []string{"BC_LISTEN_BEAST", "LISTEN_BEAST"},
		},
		&cli.StringFlag{
			Category: "Network",
			Name:     "listen-mlat",
			Usage:    "Address and TCP port to listen on for MLAT connections",
			Value:    ":12346",
			EnvVars:  []string{"BC_LISTEN_MLAT", "LISTEN_MLAT"},
		},
		&cli.StringFlag{
			Category: "SSL/TLS",
			Name:     "cert",
			Usage:    "Server certificate PEM file name (x509)",
			Required: true,
			EnvVars:  []string{"BC_CERT_FILE", "CERT_FILE"},
		},
		&cli.StringFlag{
			Category: "SSL/TLS",
			Name:     "key",
			Usage:    "Server certificate private key PEM file name (x509)",
			Required: true,
			EnvVars:  []string{"BC_KEY_FILE", "KEY_FILE"},
		},
		&cli.IntFlag{
			Category: "ATC API",
			Name:     "atcupdatefreq",
			Usage:    "frequency (in minutes) for valid feeder updates from ATC",
			Value:    1,
			EnvVars:  []string{"ATC_UPDATE_FREQ"},
		},
		&cli.StringFlag{
			Category: "Network",
			Name:     setup.Tag,
			Usage:    "default tag name for feeders if they do not have one",
			Hidden:   true, // because the HandleSinkFlag expects this to be present, but we do not need it
			Value:    "runway",
		},
		&cli.BoolFlag{
			Category: "Network",
			Name:     "no-adsb-frame-dedupe",
			Usage:    "do no dedupe ADSB frames before processing them",
			Value:    false,
		},
	}

	setup.IncludeSinkFlags(app)
	logging.IncludeVerbosityFlags(app)
	monitoring.IncludeMonitoringFlags(app, 9602)

	app.Commands = []*cli.Command{
		{
			Name:   "daemon",
			Usage:  "Docker Daemon Mode",
			Action: runDaemon,
		},
	}

	app.Before = func(c *cli.Context) error {
		logging.SetLoggingLevel(c)
		return nil
	}

	if err := app.Run(os.Args); err != nil {
		log.Error().Err(err).Msg("Finishing with an error")
		os.Exit(1)
	}
}

// runDaemon does not have pretty cli output (just JSON from logging)
func runDaemon(c *cli.Context) error {
	monitoring.RunWebServer(c)

	trackerOpts := make([]tracker.Option, 0)
	trackerOpts = append(
		trackerOpts,
		tracker.WithPrometheusCounters(
			prometheusGaugeCurrentPlanes,
			prometheusCounterFramesDecoded,
			prometheusCounterFramesErrored,
			prometheusCounterPlanesPurgedBeforeViable,
			prometheusStaleRejected,
		),
		tracker.WithDecodeWorkerCount(1), // only need a single decoder per source
	)
	trk := tracker.NewTracker(trackerOpts...)
	sinkDest, err := setup.HandleSinkFlag(c, "runway")
	if err != nil {
		return err
	}
	trk.SetSink(sinkDest)

	// middleware order matters

	// add in the frame accounting middleware before we deduplicate the incoming streams
	if sinkType, ok := sinkDest.(*sink.Sink); ok {
		if ns, ok := sinkType.Server().(*nats_io.Server); ok {
			trk.AddMiddleware(middleware.NewAccounting(middleware.WithNats(ns)))
		}
	}

	// no need to process the same ADSB from the same plane more than once
	if !c.Bool("no-adsb-frame-dedupe") {
		println("Include ADSB Dedupe")
		trk.AddMiddleware(dedupe.NewFilter(dedupe.WithDedupeCounter(prometheusOutputFrameDedupe)))
	}

	// allow our ingest tap to see what is going on
	if sinkType, ok := sinkDest.(*sink.Sink); ok {
		if ns, ok := sinkType.Server().(*nats_io.Server); ok {
			trk.AddMiddleware(middleware.NewIngestTap(ns))
		}
	}

	// start feeder cache system
	feederAuthenticator, err := feederauth.New(
		feederauth.WithLogger(log.With().Logger()),
		feederauth.WithNatsURL(c.String("sink")),
	)
	defer func() {
		errFeeder := feederAuthenticator.Close()
		if errFeeder != nil {
			log.Error().Err(errFeeder).Msg("error closing feederAuthenticator")
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wg := sync.WaitGroup{}

	// BEAST Listener
	wg.Go(func() {
		defer cancel()
		_, err := ListenForIncomingPlaneWatchBeast(
			ctx,
			WithListenHostPort(c.String("listen-beast")),
			WithTLSCertificate(c.String("cert"), c.String("key")),
			WithTracker(trk),
			WithNatsURL(c.String("sink")),
			WithFeederAuthenticator(feederAuthenticator),
		)
		if err != nil {
			log.Error().Err(err).Msg("failed to listen for beast")
		}
	})

	// MLAT Listener
	wg.Go(func() {
		defer cancel()
		_, err := mlatbridge.ListenForIncomingPlaneWatchMLAT(
			ctx,
			mlatbridge.WithListenHostPort(c.String("listen-mlat")),
			mlatbridge.WithTLSCertificate(c.String("cert"), c.String("key")),
			mlatbridge.WithNatsURL(c.String("sink")),
			mlatbridge.WithFeederAuthenticator(feederAuthenticator),
		)
		if err != nil {
			log.Error().Err(err).Msg("failed to listen for mlat")
		}
	})

	go trk.StopOnCancel(func() {
		cancel()
	})
	wg.Wait()
	trk.Wait()

	return err
}
