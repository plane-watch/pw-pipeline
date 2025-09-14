package main

import (
	"context"
	"os"
	"os/signal"
	"plane.watch/lib/clickhouse"
	"sync"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
	"plane.watch/lib/dedupe/forgetfulmap"
	"plane.watch/lib/monitoring"

	"plane.watch/lib/logging"
)

// queue suffixes for a low (only significant) and high (every message) tile queues
const (
	qSuffixLow  = "_low"
	qSuffixHigh = "_high"
)

type (
	pwRouter struct {
		syncSamples *forgetfulmap.ForgetfulSyncMap

		haveSourceSinkConnection bool

		incomingMessages chan []byte

		nats *natsIoRouter
	}
)

var (
	version          = "dev"
	updatesProcessed = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "pw_router",
		Name:      "updates_processed_total",
		Help:      "The total number of messages processed.",
	})
	updatesSignificant = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "pw_router",
		Name:      "updates_significant_total",
		Help:      "The total number of messages determined to be significant.",
	})
	updatesInsignificant = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "pw_router",
		Name:      "updates_insignificant_total",
		Help:      "The total number of messages determined to be insignificant.",
	})
	updatesIgnored = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "pw_router",
		Name:      "updates_ignored_total",
		Help:      "The total number of messages determined to be insignificant and thus ignored.",
	})
	updatesPublished = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "pw_router",
		Name:      "updates_published_total",
		Help:      "The total number of messages published to the output queue.",
	})
	updatesError = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "pw_router",
		Name:      "updates_error_total",
		Help:      "The total number of messages that could not be processed due to an error.",
	})
	cacheEntries = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "pw_router",
		Name:      "cache_planes_count",
		Help:      "The number of planes in the reducer cache.",
	})
	cacheEvictions = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "pw_router",
		Name:      "cache_eviction_total",
		Help:      "The number of cache evictions made from the cache.",
	})
	prometheusAppVer = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "pw_router",
		Name:      "info",
		Help:      "Application Info/Metadata",
	}, []string{"version"})
)

func init() {
	prometheusAppVer.With(prometheus.Labels{"version": version}).Set(1)
}

func main() {
	app := cli.NewApp()
	zerolog.SetGlobalLevel(zerolog.InfoLevel)

	app.Version = version
	app.Name = "Plane Watch Router (pw_router)"
	app.Usage = "Reads location updates from AMQP and publishes only significant updates."

	app.Description = `This program takes a stream of plane tracking data (location updates) from a message bus  ` +
		`and filters messages and only returns significant changes for each aircraft.` +
		"\n\n" +
		`example: ./pw_router --nats="nats://guest:guest@localhost:4222" --source-route-key=location-updates --num-workers=8 --prom-metrics-port=9601`

	app.Commands = cli.Commands{
		{
			Name:        "daemon",
			Description: "For prod, Logging is JSON formatted",
			Action:      runDaemon,
		},
		{
			Name:        "cli",
			Description: "Runs in your terminal with human readable output",
			Action:      runCli,
		},
	}

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:  "nats",
			Usage: "Nats.io URL for fetching and publishing updates.",
			//Value:   "nats://guest:guest@nats:4222/",
			EnvVars: []string{"NATS"},
		},
		&cli.StringFlag{
			Name:  "clickhouse",
			Usage: "Save our location updates to clickhouse, clickhouse://user:pass@host:port/database",
			//Value:   "clickhouse://user:pass@host:port/database",
			EnvVars: []string{"CLICKHOUSE"},
		},
		&cli.StringFlag{
			Name:    "source-route-key",
			Usage:   "Name of the routing key to read location updates from.",
			Value:   "location-updates-enriched",
			EnvVars: []string{"SOURCE_ROUTE_KEY"},
		},
		&cli.StringFlag{
			Name:    "destination-route-key",
			Usage:   "Name of the routing key to publish significant updates to. (low)",
			Value:   "location-updates-enriched-reduced",
			EnvVars: []string{"DEST_ROUTE_KEY"},
		},
		&cli.StringFlag{
			Name:    "destination-route-key-merged",
			Usage:   "Name of the routing key to publish merged updates to. (high)",
			Value:   "location-updates-enriched-merged",
			EnvVars: []string{"DEST_ROUTE_KEY"},
		},
		&cli.IntFlag{
			Name:    "num-workers",
			Usage:   "Number of workers to process updates.",
			Value:   10,
			EnvVars: []string{"NUM_WORKERS"},
		},
		&cli.BoolFlag{
			Name:    "spread-updates",
			Usage:   "publish location updates to their respective tileXX_high and tileXX_low routing keys as well.",
			EnvVars: []string{"SPREAD"},
		},
		&cli.IntFlag{
			Name:    "update-age",
			Usage:   "seconds to keep an update before aging it out of the cache.",
			Value:   300,
			EnvVars: []string{"UPDATE_AGE"},
		},
		&cli.IntFlag{
			Name:    "update-age-sweep-interval",
			Usage:   "Seconds between cache age sweeps.",
			Value:   30,
			EnvVars: []string{"UPDATE_SWEEP"},
		},
	}
	logging.IncludeVerbosityFlags(app)
	monitoring.IncludeMonitoringFlags(app, 9601)
	app.InvalidFlagAccessHandler = func(c *cli.Context, s string) {
		log.Fatal().Str("Unknown Flag", s).Msg("Invalid CLI Flag used. Please Fix.")
	}
	app.Before = func(c *cli.Context) error {
		logging.SetLoggingLevel(c)
		return nil
	}

	if err := app.Run(os.Args); nil != err {
		log.Error().Err(err).Send()
	}
}

func runDaemon(c *cli.Context) error {
	return run(c)
}

func runCli(c *cli.Context) error {
	logging.ConfigureForCli()
	return run(c)
}

func run(c *cli.Context) error {
	// setup and start the prom exporter
	monitoring.RunWebServer(c)

	var err error
	// connect to the message queue, create ourselves 2 queues
	router := pwRouter{
		syncSamples: forgetfulmap.NewForgetfulSyncMap(
			forgetfulmap.WithSweepIntervalSeconds(c.Int("update-age-sweep-interval")),
			forgetfulmap.WithOldAgeAfterSeconds(c.Int("update-age")),
			forgetfulmap.WithPreEvictionAction(func(key, value any) {
				cacheEvictions.Inc()
				cacheEntries.Dec()
				log.Debug().Msgf("Evicting cache entry Icao: %s", key)
			}),
		),
	}

	defer router.syncSamples.Stop()

	router.incomingMessages = make(chan []byte, 1000)

	router.nats = newNatsIoRouter(c.String("nats"))
	if nil == router.nats {
		cli.ShowAppHelpAndExit(c, 1)
	}

	incomingSubject := c.String("source-route-key")
	if err = router.nats.connect(); nil != err {
		return err
	}
	if err = router.nats.listen(incomingSubject, router.incomingMessages); nil != err {
		return err
	}
	monitoring.AddHealthCheck(router.nats)

	var ds *DataStream
	if chURL := c.String("clickhouse"); chURL != "" {
		chs, err := clickhouse.New(chURL)
		if nil != err {
			return err
		}
		ds = NewDataStreams(chs)
	}

	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	chSignal := make(chan os.Signal, 1)
	signal.Notify(chSignal, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-chSignal // wait for our cancel signal
		log.Info().Str("signal", sig.String()).Msg("Shutting Down")
		router.nats.close()
		// and then close all the things
		cancel()
	}()
	monitoring.AddHealthCheck(router)

	numWorkers := c.Int("num-workers")
	destRouteKeyLow := c.String("destination-route-key")
	destRouteKeyMerged := c.String("destination-route-key-merged")
	spreadUpdates := c.Bool("spread-updates")

	log.Info().Msgf("Starting with %d workers...", numWorkers)
	for i := 0; i < numWorkers; i++ {
		wkr := worker{
			router:             &router,
			destRoutingKeyLow:  destRouteKeyLow,
			destRoutingKeyHigh: destRouteKeyMerged,
			spreadUpdates:      spreadUpdates,
			ds:                 ds,
		}
		wg.Add(1)
		go func() {
			wkr.run(ctx, router.incomingMessages)
			wg.Done()
		}()
	}

	wg.Wait()

	return nil
}

func (p pwRouter) HealthCheckName() string {
	return "pw_router"
}

func (p pwRouter) HealthCheck() bool {
	// let's do a chan checks

	l := len(p.incomingMessages)
	c := cap(p.incomingMessages)
	percent := (float32(l) / float32(c)) * 100
	log.Info().
		Int("Num Messages Waiting", l).
		Int("Queue Capacity", c).
		Float32("Percent Used", percent).
		Msg("Incoming Message Queue")

	return percent < 80
}
