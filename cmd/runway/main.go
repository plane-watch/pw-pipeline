package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
	"golang.org/x/sync/errgroup"
	"plane.watch/lib/dedupe"
	"plane.watch/lib/feedercache"
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
		tracker.WithPrometheusCounters(prometheusGaugeCurrentPlanes, prometheusCounterFramesDecoded),
		tracker.WithDecodeWorkerCount(1), // only need a single decoder per source
	)
	trk := tracker.NewTracker(trackerOpts...)
	trk.AddMiddleware(dedupe.NewFilter(dedupe.WithDedupeCounter(prometheusOutputFrameDedupe)))
	sinkDest, err := setup.HandleSinkFlagWithoutTag(c, "runway")
	if err != nil {
		return err
	}
	trk.SetSink(sinkDest)

	if sinkType, ok := sinkDest.(*sink.Sink); ok {
		if ns, ok := sinkType.Server().(*nats_io.Server); ok {
			trk.AddMiddleware(middleware.NewIngestTap(ns))
		}
	}

	// start feeder cache system
	feeders, err := feedercache.New(
		feedercache.WithLogger(log.Logger),
		feedercache.WithNatsURL(c.String("sink")),
	)
	defer func() {
		err := feeders.Close()
		if err != nil {
			log.Error().Err(err).Msg("error closing feeders")
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wg := errgroup.Group{}

	// BEAST Listener
	wg.Go(func() error {
		defer cancel()
		for { // loop forever
			//todo(mikenye): implement a way to exit loop when app is quit (eg: SIGTERM/SIGINT) or similar.
			//  consider adding a "WithCancel" or "WithContext" for this
			_, err := ListenForIncomingPlaneWatchBeast(
				ctx,
				WithListenHostPort(c.String("listen-beast")),
				WithTLSCertificate(c.String("cert"), c.String("key")),
				WithTracker(trk),
				WithNatsURL(c.String("sink")),
				WithFeederCache(feeders),
			)
			if err != nil {
				return fmt.Errorf("failed to listen for beast: %w", err)
			}
			feeders.Reset(feedercache.BEAST)
			time.Sleep(time.Second * 10) // back-off between loops
		}
		return nil
	})

	// MLAT Listener
	wg.Go(func() error {
		defer cancel()
		for {
			//todo(mikenye): implement a way to exit loop when app is quit (eg: SIGTERM/SIGINT) or similar.
			//  consider adding a "WithCancel" or "WithContext" for this
			_, err := mlatbridge.ListenForIncomingPlaneWatchMLAT(
				ctx,
				mlatbridge.WithListenHostPort(c.String("listen-mlat")),
				mlatbridge.WithTLSCertificate(c.String("cert"), c.String("key")),
				mlatbridge.WithNatsURL(c.String("sink")),
				mlatbridge.WithFeederCache(feeders),
			)
			if err != nil {
				return fmt.Errorf("failed to listen for mlat: %w", err)
			}
			feeders.Reset(feedercache.MLAT)
			time.Sleep(time.Second * 10) // back-off between loops
		}
		return nil
	})

	err = wg.Wait()

	go trk.StopOnCancel()
	trk.Wait()

	return err
}
