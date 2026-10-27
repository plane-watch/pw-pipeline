package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
	"plane.watch/lib/logging"
	"plane.watch/lib/monitoring"
	"plane.watch/lib/rawarchive"
)

var version = "dev"

const (
	flagNats          = "nats"
	flagSubject       = "subject"
	flagBatchInterval = "batch-interval"
	flagGrace         = "seal-grace"
	flagRestartDelay  = "restart-delay"
	flagDedupeWindow  = "dedupe-window"
	flagTiles         = "tiles"
	flagQueueLen      = "queue-length"
	flagS3Endpoint    = "s3-endpoint"
	flagS3Region      = "s3-region"
	flagS3Bucket      = "s3-bucket"
	flagS3AccessKey   = "s3-access-key-id"
	flagS3SecretKey   = "s3-secret-access-key"
	flagS3PathStyle   = "s3-path-style"
	flagFirstDay      = "first-day"
)

func init() {
	prometheusAppVer.With(prometheus.Labels{"version": version}).Set(1)
}

func main() {
	app := cli.NewApp()
	zerolog.SetGlobalLevel(zerolog.InfoLevel)

	app.Version = version
	app.Name = "Plane Watch Raw Archiver (pw_archiver)"
	app.Usage = "Writes the raw frames the ingest instances accept to an object store, one object per minute."
	app.Description = `Subscribes to the raw archive batches that runway and pw_ingest publish when run with ` +
		`--raw-archive-subject, groups every frame by minute and map tile, de-duplicates copies heard by ` +
		`several instances, and writes one zstd object per UTC minute to an S3 bucket (Cloudflare R2 included). ` +
		`Run exactly one archiver per network.` +
		"\n\n" +
		`example: ./pw_archiver --nats=nats://localhost:4222 --s3-endpoint=https://<account>.r2.cloudflarestorage.com ` +
		`--s3-bucket=pw-raw-archive --s3-access-key-id=... --s3-secret-access-key=... daemon`

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
		{
			Name:        "tiles",
			Description: "Prints the tile grid description (meta/tiles.json) this build archives with",
			Action:      runTiles,
		},
	}

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    flagNats,
			Usage:   "NATS URL the ingest instances publish batches on",
			EnvVars: []string{"NATS"},
		},
		&cli.StringFlag{
			Name:    flagSubject,
			Usage:   "subject the batches are published on (the ingest --raw-archive-subject)",
			Value:   rawarchive.DefaultSubject,
			EnvVars: []string{"SUBJECT"},
		},
		&cli.DurationFlag{
			Name:    flagBatchInterval,
			Usage:   "the ingest instances' --raw-archive-interval; sets the seal time and the expected batches per minute",
			Value:   5 * time.Second,
			EnvVars: []string{"BATCH_INTERVAL"},
		},
		&cli.DurationFlag{
			Name:    flagGrace,
			Usage:   "added to the seal time for clock skew and lag",
			Value:   5 * time.Second,
			EnvVars: []string{"SEAL_GRACE"},
		},
		&cli.DurationFlag{
			Name:    flagRestartDelay,
			Usage:   "extra delay before writing the first, partial minute after a start, so an archiver still running writes its complete copy first",
			Value:   15 * time.Second,
			EnvVars: []string{"RESTART_DELAY"},
		},
		&cli.DurationFlag{
			Name:    flagDedupeWindow,
			Usage:   "identical messages this close together are one record",
			Value:   2 * time.Second,
			EnvVars: []string{"DEDUPE_WINDOW"},
		},
		&cli.StringFlag{
			Name:    flagTiles,
			Usage:   "comma separated tiles to archive, e.g. 35,36 (empty: all)",
			EnvVars: []string{"TILES"},
		},
		&cli.IntFlag{
			Name:    flagQueueLen,
			Usage:   "batches buffered between the NATS client and the archiver; beyond it batches are dropped and the minute marked incomplete",
			Value:   16384,
			EnvVars: []string{"QUEUE_LENGTH"},
		},
		&cli.StringFlag{
			Name:     flagS3Endpoint,
			Category: "Object store",
			Usage:    "S3 endpoint, e.g. https://<account>.r2.cloudflarestorage.com",
			EnvVars:  []string{"S3_ENDPOINT"},
		},
		&cli.StringFlag{
			Name:     flagS3Region,
			Category: "Object store",
			Usage:    "S3 region (R2: auto)",
			Value:    "auto",
			EnvVars:  []string{"S3_REGION"},
		},
		&cli.StringFlag{
			Name:     flagS3Bucket,
			Category: "Object store",
			Usage:    "bucket holding raw/v1/... and meta/tiles.json; keep it for the archive alone",
			EnvVars:  []string{"S3_BUCKET"},
		},
		&cli.StringFlag{
			Name:     flagS3AccessKey,
			Category: "Object store",
			Usage:    "access key id of a token that can write the bucket",
			EnvVars:  []string{"S3_ACCESS_KEY_ID", "AWS_ACCESS_KEY_ID"},
		},
		&cli.StringFlag{
			Name:     flagS3SecretKey,
			Category: "Object store",
			Usage:    "its secret access key",
			EnvVars:  []string{"S3_SECRET_ACCESS_KEY", "AWS_SECRET_ACCESS_KEY"},
		},
		&cli.BoolFlag{
			Name:     flagS3PathStyle,
			Category: "Object store",
			Usage:    "address the bucket in the path (MinIO) instead of the host",
			EnvVars:  []string{"S3_PATH_STYLE"},
		},
		&cli.StringFlag{
			Name:    flagFirstDay,
			Usage:   "first day archived, YYYYMMDD, written into meta/tiles.json when it is first created",
			EnvVars: []string{"FIRST_DAY"},
		},
	}
	logging.IncludeVerbosityFlags(app)
	monitoring.IncludeMonitoringFlags(app, 9607)
	app.InvalidFlagAccessHandler = func(c *cli.Context, s string) {
		log.Fatal().Str("Unknown Flag", s).Msg("Invalid CLI Flag used. Please Fix.")
	}
	app.Before = func(c *cli.Context) error {
		logging.SetLoggingLevel(c)
		return nil
	}

	if err := app.Run(os.Args); nil != err {
		log.Error().Err(err).Send()
		os.Exit(1)
	}
}

func runDaemon(c *cli.Context) error {
	return run(c)
}

func runCli(c *cli.Context) error {
	logging.ConfigureForCli()
	return run(c)
}

func runTiles(c *cli.Context) error {
	b, err := buildTilesJSON(c.String(flagFirstDay))
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, string(b))
	return err
}

// natsHealth reports the connection in /status.
type natsHealth struct{ nc *nats.Conn }

func (n natsHealth) HealthCheckName() string { return "Nats" }
func (n natsHealth) HealthCheck() bool       { return n.nc.IsConnected() }

func run(c *cli.Context) error {
	monitoring.RunWebServer(c)

	if c.String(flagNats) == "" {
		return errors.New("--nats is required")
	}
	tiles, err := parseTileList(c.String(flagTiles))
	if err != nil {
		return err
	}
	if !rawarchive.ValidInterval(c.Duration(flagBatchInterval)) {
		return fmt.Errorf("--%s must divide one minute", flagBatchInterval)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	store, err := NewS3Store(ctx, S3Config{
		Endpoint:  c.String(flagS3Endpoint),
		Region:    c.String(flagS3Region),
		Bucket:    c.String(flagS3Bucket),
		AccessKey: c.String(flagS3AccessKey),
		SecretKey: c.String(flagS3SecretKey),
		PathStyle: c.Bool(flagS3PathStyle),
	})
	if err != nil {
		return err
	}

	// The grid description goes in once; a later build with a new grid version
	// would append to it, which is a change to make by hand.
	tilesJSON, err := buildTilesJSON(c.String(flagFirstDay))
	if err != nil {
		return err
	}
	created, err := store.Put(ctx, tilesKey, tilesJSON, "application/json")
	if err != nil {
		return fmt.Errorf("writing %s: %w", tilesKey, err)
	}
	log.Info().Str("key", tilesKey).Bool("created", created).Msg("tile grid description")

	nc, err := nats.Connect(c.String(flagNats),
		nats.Name("pw_archiver"),
		nats.MaxReconnects(-1),
		nats.ErrorHandler(func(_ *nats.Conn, sub *nats.Subscription, err error) {
			if errors.Is(err, nats.ErrSlowConsumer) {
				log.Warn().Err(err).Msg("NATS slow consumer: batches dropped")
				return
			}
			log.Error().Err(err).Msg("NATS error")
		}),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			log.Warn().Err(err).Msg("NATS disconnected")
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Info().Str("url", nc.ConnectedUrl()).Msg("NATS reconnected")
		}),
	)
	if err != nil {
		return fmt.Errorf("nats: %w", err)
	}
	defer nc.Close()
	monitoring.AddHealthCheck(natsHealth{nc})

	msgs := make(chan *nats.Msg, c.Int(flagQueueLen))
	sub, err := nc.ChanSubscribe(c.String(flagSubject), msgs)
	if err != nil {
		return fmt.Errorf("nats subscribe: %w", err)
	}
	log.Info().Str("subject", c.String(flagSubject)).Msg("subscribed")

	arch := New(Config{
		BatchInterval: c.Duration(flagBatchInterval),
		Grace:         c.Duration(flagGrace),
		RestartDelay:  c.Duration(flagRestartDelay),
		DedupeWindow:  c.Duration(flagDedupeWindow),
		Tiles:         tiles,
		GridVersion:   gridVersion,
		Build:         buildID(version),
		SubDropped: func() int64 {
			n, _ := sub.Dropped()
			return int64(n)
		},
		Log: log.With().Str("section", "archiver").Logger(),
	}, store)
	monitoring.AddHealthCheck(arch)
	log.Info().Str("tiles", tileListString(tiles)).Dur("batchInterval", c.Duration(flagBatchInterval)).
		Str("version", version).Msg("archiving")

	handled := make(chan struct{})
	go func() {
		defer close(handled)
		for m := range msgs {
			arch.Handle(m)
		}
	}()

	arch.Run(ctx)

	_ = sub.Unsubscribe()
	close(msgs)
	<-handled
	return nil
}

func tileListString(tiles map[uint8]bool) string {
	if tiles == nil {
		return "all"
	}
	parts := make([]string, 0, len(tiles))
	for t := range tiles {
		parts = append(parts, strconv.Itoa(int(t)))
	}
	return strings.Join(parts, ",")
}

// buildID is the short git hash as a number, for the partition header: from the
// build's VCS information when present, else from a version string that is a hash.
func buildID(version string) uint32 {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				if v, ok := hexPrefix(s.Value); ok {
					return v
				}
			}
		}
	}
	v, _ := hexPrefix(strings.TrimPrefix(version, "v"))
	return v
}

func hexPrefix(s string) (uint32, bool) {
	if len(s) < 8 {
		return 0, false
	}
	v, err := strconv.ParseUint(s[:8], 16, 32)
	if err != nil {
		return 0, false
	}
	return uint32(v), true
}
