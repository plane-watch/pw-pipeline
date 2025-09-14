package main

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog/log"
	sqldblogger "github.com/simukti/sqldb-logger"
	"github.com/simukti/sqldb-logger/logadapter/zerologadapter"
	"github.com/urfave/cli/v2"
	"net"
	"net/url"
	"os"
	"plane.watch/lib/logging"
	"plane.watch/lib/monitoring"
	"plane.watch/lib/nats_io"
	"strings"
	"time"
)

var (
	version = "dev"
	db      *sqlx.DB

	ErrUnsupportedResponse = `{"error":"Unsupported Request","Type":"%s"}`
	ErrRequestFailed       = `{"error":"Something went wrong with the request","Type":"%s"}`

	prometheusCounterSearch = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "pw_atc_api",
		Name:      "search_count",
		Help:      "The number of searches handled",
	})

	prometheusCounterSearchSummary = promauto.NewSummary(prometheus.SummaryOpts{
		Namespace: "pw_atc_api",
		Name:      "search_summary",
		Help:      "A Summary of the search times in milliseconds",
	})

	prometheusCounterEnrich = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "pw_atc_api",
		Name:      "enrich_count",
		Help:      "The number of enrichments handled",
	})

	prometheusCounterEnrichSummary = promauto.NewSummary(prometheus.SummaryOpts{
		Namespace: "pw_atc_api",
		Name:      "enrich_summary",
		Help:      "A Summary of the enrich times in milliseconds",
	})

	prometheusCounterFeeder = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "pw_atc_api",
		Name:      "feeder_count",
		Help:      "The number of requests for feeder information and feeder updates",
	})

	prometheusCounterFeederSummary = promauto.NewSummary(prometheus.SummaryOpts{
		Namespace: "pw_atc_api",
		Name:      "feeder_summary",
		Help:      "A Summary of the feeder request times in milliseconds",
	})

	prometheusAppVer = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "pw_atc_api",
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
	app.Name = "Plane Watch ATC API Server"
	app.Usage = "Listens on NATS bus for requests and responds to them"

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
		// support database URL and individual parts
		&cli.StringFlag{
			Name:    "database",
			Usage:   "Database URL",
			EnvVars: []string{"DATABASE_URL"},
		},

		&cli.StringFlag{
			Name:    "db-host",
			Usage:   "Database Host e.g. postgres://user@pass:localhost:5432/db_name?sslmode=disable&schema=public",
			EnvVars: []string{"DATABASE_HOST"},
		},
		&cli.StringFlag{
			Name:    "db-port",
			Usage:   "Database Port",
			Value:   "5432",
			EnvVars: []string{"DATABASE_PORT"},
		},
		&cli.StringFlag{
			Name:    "db-user",
			Usage:   "Database User",
			EnvVars: []string{"DATABASE_USER"},
		},
		&cli.StringFlag{
			Name:    "db-pass",
			Usage:   "Database Pass",
			EnvVars: []string{"DATABASE_PASSWORD"},
		},
		&cli.StringFlag{
			Name:    "db-name",
			Usage:   "Database Name",
			EnvVars: []string{"DATABASE_NAME"},
		},
		&cli.IntFlag{
			Name:    "num-workers",
			Usage:   "How many workers to spin up",
			Value:   8,
			EnvVars: []string{"NUM_WORKERS"},
		},
	}
	app.Before = func(c *cli.Context) error {
		logging.SetLoggingLevel(c)

		if "" == c.String("database") {
			dbUrl := url.URL{
				Scheme:   "postgres",
				User:     url.UserPassword(c.String("db-user"), c.String("db-pass")),
				Host:     net.JoinHostPort(c.String("db-host"), c.String("db-port")),
				Path:     c.String("db-name"),
				RawQuery: "sslmode=disable&schema=public",
			}

			return c.Set("database", dbUrl.String())
		}

		return nil
	}
	logging.IncludeVerbosityFlags(app)
	monitoring.IncludeMonitoringFlags(app, 9602)

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

func connectDatabase(c *cli.Context) error {
	databaseUrl := c.String("database")
	dbLog := log.With().Str("section", "database").Logger()
	logAdapter := zerologadapter.New(dbLog)

	urlParts, err := url.Parse(databaseUrl)
	if nil != err {
		return err
	}

	pass, _ := urlParts.User.Password()
	schema := urlParts.Query().Get("schema")
	if "" == schema {
		schema = "public"
	}
	sslMode := urlParts.Query().Get("sslMode")
	if "" == sslMode {
		sslMode = "disable"
	}
	dbPort := urlParts.Port()
	if "" == dbPort {
		dbPort = "5432"
	}

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s search_path=%s sslmode=%s",
		urlParts.Hostname(),
		dbPort,
		urlParts.User.Username(),
		pass,
		strings.Trim(urlParts.Path, "/"),
		schema,
		sslMode,
	)
	dbLog.Debug().
		Str("host", urlParts.Hostname()).
		Str("port", dbPort).
		Str("user", urlParts.User.Username()).
		Str("database", strings.Trim(urlParts.Path, "/")).
		Str("schema", schema).
		Str("sslmode", sslMode).
		Msg("Database Connection Info")

	var sqlDb *sql.DB

	connectAttempts := 0
	for sqlDb == nil {
		connectAttempts++
		dbLog.Debug().Int("attempt", connectAttempts).Msg("db connect...")
		sqlDb, err = sql.Open("postgres", dsn)
		if nil != err {
			dbLog.Error().Err(err).Int("attempt", connectAttempts).Msg("failed connect, sleeping and trying again")
			time.Sleep(time.Second * time.Duration(connectAttempts))
		}
		if err = sqlDb.Ping(); nil != err {
			dbLog.Error().Err(err).Int("attempt", connectAttempts).Msg("failed ping, sleeping and trying again")
			time.Sleep(time.Second * time.Duration(connectAttempts))
		}
	}
	if dbLog.Debug().Enabled() {
		sqlDb = sqldblogger.OpenDriver(dsn, sqlDb.Driver(), logAdapter)
	}
	db = sqlx.NewDb(sqlDb, "postgres")

	if nil != err {
		return err
	}
	if nil == db {
		return errors.New("unable to connect to the database")
	}
	db.DB.SetMaxOpenConns(50) // because samfty said so
	db.DB.SetMaxIdleConns(10)

	t1 := time.Now()
	defer func() {
		dbLog.Debug().
			Str("database", "ping").
			Dur("ping time ms", time.Since(t1)).
			Send()
	}()

	return db.Ping()
}

func run(c *cli.Context) error {
	log.Info().Msg("Starting up")
	monitoring.RunWebServer(c)
	if err := connectDatabase(c); nil != err {
		log.Error().Err(err).Msg("Failed to connect to database")
		return err
	}

	server, err := nats_io.NewServer(
		nats_io.WithServer(c.String("nats"), "pw_atc_api"),
		nats_io.WithConnections(false, true),
	)
	if nil != err {
		return err
	}

	numWorkers := c.Int("num-workers")
	for i := 0; i < numWorkers; i++ {
		go newSearchApi(i).configure(server).listen()
		go newEnrichmentApi(i).configure(server).listen()
		go newFeederApi(i).configure(server).listen()
	}

	hc := health{}
	monitoring.AddHealthCheck(&hc)

	select {}
}
