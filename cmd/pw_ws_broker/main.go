package main

import (
	"errors"
	"os"
	"plane.watch/lib/nats_io"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
	"plane.watch/lib/logging"
	"plane.watch/lib/monitoring"
)

var (
	version = "dev"

	prometheusNumClients = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "pw_ws_broker",
		Name:      "num_clients",
		Help:      "The current number of websocket clients we are currently serving",
	})
	prometheusIncomingMessages = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "pw_ws_broker",
			Name:      "incoming_messages",
			Help:      "The number of messages from the queue",
		},
		[]string{"rate"},
	)
	prometheusKnownPlanes = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "pw_ws_broker",
		Name:      "known_planes",
		Help:      "The number of planes we know about",
	})
	prometheusMessagesSent = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "pw_ws_broker",
		Name:      "messages_sent",
		Help:      "The number of messages sent to clients over websockets",
	})
	prometheusMessagesSize = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "pw_ws_broker",
		Name:      "messages_size",
		Help:      "the raw size of messages sent (before compression)",
	})
	prometheusSubscriptions = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "pw_ws_broker",
			Name:      "subscriptions",
			Help:      "which tiles people are subscribed to",
		},
		[]string{"tile"},
	)
	prometheusAppVer = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "pw_ws_broker",
		Name:      "info",
		Help:      "Application Info/Metadata",
	}, []string{"version"})
)

func init() {
	prometheusAppVer.With(prometheus.Labels{"version": version}).Set(1)
}

func main() {
	app := cli.NewApp()
	app.Version = version
	app.Name = "Plane.Watch WebSocket Broker (pw_ws_broker)"
	app.Usage = "Websocket Broker"
	app.Description = "Acts as a go between external display elements our the data pipeline"
	app.Authors = []*cli.Author{
		{
			Name:  "Jason Playne",
			Email: "jason@jasonplayne.com",
		},
	}

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
			Name:    "nats",
			Usage:   "Nats.io URL for fetching and publishing updates. nats://guest:guest@host:4222/",
			EnvVars: []string{"NATS"},
		},
		&cli.StringFlag{
			Name:  "clickhouse",
			Usage: "Save our location updates to clickhouse, clickhouse://user:pass@host:port/database",
			//Value:   "clickhouse://user:pass@host:port/database",
			EnvVars: []string{"CLICKHOUSE"},
		},
		&cli.StringFlag{
			Name:    "route-key-low",
			Usage:   "The routing key that has only the significant flight update events",
			Value:   "location-updates-enriched-reduced",
			EnvVars: []string{"ROUTE_KEY_LOW"},
		},
		&cli.StringFlag{
			Name:    "route-key-high",
			Usage:   "The routing key that has all of the flight update events",
			Value:   "location-updates-enriched-merged",
			EnvVars: []string{"ROUTE_KEY_HIGH"},
		},
		&cli.StringFlag{
			Name:    "http-addr",
			Usage:   "What the HTTP server listens on",
			Value:   ":80",
			EnvVars: []string{"HTTP_ADDR"},
		},
		&cli.StringFlag{
			Name:    "tls-cert",
			Usage:   "The path to a PEM encoded TLS Full Chain Certificate (cert+intermediate+ca)",
			Value:   "",
			EnvVars: []string{"TLS_CERT"},
		},
		&cli.StringFlag{
			Name:    "tls-cert-key",
			Usage:   "The path to a PEM encoded TLS Certificate Key",
			Value:   "",
			EnvVars: []string{"TLS_CERT_KEY"},
		},

		&cli.BoolFlag{
			Name:    "serve-test-web",
			Usage:   "Serve up a test website for websocket testing",
			EnvVars: []string{"TEST_WEB"},
		},
		&cli.DurationFlag{
			Name:    "send-tick",
			Usage:   "When > 0, how long to collect messages before sending them in one batch",
			EnvVars: []string{"SEND_TICK"},
			Value:   500 * time.Millisecond,
		},
	}

	logging.IncludeVerbosityFlags(app)
	monitoring.IncludeMonitoringFlags(app, 9603)

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
	cert := c.String("tls-cert")
	certKey := c.String("tls-cert-key")
	if ("" != cert || "" != certKey) && ("" == cert || "" == certKey) {
		return errors.New("please provide both certificate and key")
	}
	if ":80" == c.String("http-addr") && "" != cert {
		return c.Set("http-addr", ":443")
	}

	monitoring.RunWebServer(c)

	nats := c.String("nats")
	lowRoute := c.String("route-key-low")
	highRoute := c.String("route-key-high")

	hasNats := "" != nats

	isValid := true
	if !hasNats {
		log.Info().Msg("Please provide nats connection details. (--nats)")
		isValid = false
	}
	if "" == lowRoute {
		log.Info().Msg("Please provide the routing key for significant updates. (--route-key-low)")
		isValid = false
	}
	if "" == highRoute {
		log.Info().Msg("Please provide the routing key for all updates. (--route-key-high)")
		isValid = false
	}
	if !isValid {
		return errors.New("invalid configuration. You need nats, route-key-low and, route-key-high configured")
	}

	clickHouseUrl := c.String("clickhouse")
	if "" == clickHouseUrl {
		return errors.New("clickhouse URL must be specified")
	}

	var err error
	GlobalClickHouseData, err = NewClickHouseData(clickHouseUrl)
	if nil != err {
		return err
	}

	var natsServerRpc *nats_io.Server

	var input source
	input, err = NewPwWsBrokerNats(nats, lowRoute, highRoute)
	natsServerRpc, _ = nats_io.NewServer(nats_io.WithServer(nats, "pw_ws_broker+rpc"))
	if nil != err {
		return err
	}

	broker, err := NewPlaneWatchWebSocketBroker(
		input,
		natsServerRpc,
		c.String("http-addr"),
		c.String("tls-cert"),
		c.String("tls-cert-key"),
		c.Bool("serve-test-web"),
		c.Duration("send-tick"),
	)
	if nil != err {
		return err
	}
	defer broker.Close()

	if err = broker.Setup(); nil != err {
		return err
	}
	go broker.Run()

	broker.Wait()

	return nil
}
