package setup

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/urfave/cli/v2"
	"plane.watch/lib/middleware"
	"plane.watch/lib/nats_io"
	"plane.watch/lib/sink"
	"plane.watch/lib/tracker"
)

const (
	RawArchiveSubject  = "raw-archive-subject"
	RawArchiveInterval = "raw-archive-interval"
	RawArchiveMaxBytes = "raw-archive-max-bytes"
	RawArchiveInstance = "raw-archive-instance"
)

var (
	prometheusRawArchiveDropped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "pw_raw_archive_dropped_frames_total",
		Help: "Frames the raw archive batcher dropped because its queue was full.",
	})
	prometheusRawArchivePublished = promauto.NewCounter(prometheus.CounterOpts{
		Name: "pw_raw_archive_batches_total",
		Help: "Raw archive batches published to NATS.",
	})
	prometheusRawArchiveRecords = promauto.NewCounter(prometheus.CounterOpts{
		Name: "pw_raw_archive_records_total",
		Help: "Frames published in raw archive batches.",
	})
)

// IncludeRawArchiveFlags adds the raw archive batcher's flags. The batcher is off
// unless a subject is given.
func IncludeRawArchiveFlags(app *cli.App) {
	app.Flags = append(app.Flags,
		&cli.StringFlag{
			Category: "Raw Archive",
			Name:     RawArchiveSubject,
			Usage: "publish every accepted frame, batched, on this NATS subject (empty: off). " +
				"Needs a nats:// sink. Proposal uses " + "v1.pw-ingest.raw",
			EnvVars: []string{"PW_RAW_ARCHIVE_SUBJECT", "RAW_ARCHIVE_SUBJECT"},
		},
		&cli.DurationFlag{
			Category: "Raw Archive",
			Name:     RawArchiveInterval,
			Usage:    "how often a batch is published; must divide one minute",
			Value:    middleware.RawArchiveDefaultInterval,
			EnvVars:  []string{"PW_RAW_ARCHIVE_INTERVAL", "RAW_ARCHIVE_INTERVAL"},
		},
		&cli.IntFlag{
			Category: "Raw Archive",
			Name:     RawArchiveMaxBytes,
			Usage:    "publish a batch early once its body reaches this many bytes (keeps under the NATS payload limit)",
			Value:    middleware.RawArchiveDefaultMaxBytes,
			EnvVars:  []string{"PW_RAW_ARCHIVE_MAX_BYTES", "RAW_ARCHIVE_MAX_BYTES"},
		},
		&cli.StringFlag{
			Category: "Raw Archive",
			Name:     RawArchiveInstance,
			Usage:    "instance name in batch headers (default: hostname plus a random suffix)",
			EnvVars:  []string{"PW_RAW_ARCHIVE_INSTANCE", "RAW_ARCHIVE_INSTANCE"},
		},
	)
}

// HandleRawArchiveFlags registers the raw archive batcher on trk when a subject is
// configured. It must be called after the de-duplication middleware is added, so the
// batcher sees each frame once, and needs the sink to be NATS.
func HandleRawArchiveFlags(c *cli.Context, trk *tracker.Tracker, sinkDest tracker.Sink) error {
	subject := c.String(RawArchiveSubject)
	if subject == "" {
		return nil
	}
	natsSink, ok := sinkDest.(*sink.Sink)
	if !ok {
		return errors.New("raw archive needs a nats:// sink")
	}
	ns, ok := natsSink.Server().(*nats_io.Server)
	if !ok {
		return errors.New("raw archive needs a nats:// sink")
	}
	opts := []middleware.RawArchiveOption{
		middleware.WithRawArchiveInterval(c.Duration(RawArchiveInterval)),
		middleware.WithRawArchiveMaxBytes(c.Int(RawArchiveMaxBytes)),
		middleware.WithRawArchiveCounters(prometheusRawArchiveDropped, prometheusRawArchivePublished, prometheusRawArchiveRecords),
	}
	if name := c.String(RawArchiveInstance); name != "" {
		opts = append(opts, middleware.WithRawArchiveInstance(name))
	}
	ra, err := middleware.NewRawArchive(ns, subject, opts...)
	if err != nil {
		return err
	}
	trk.AddMiddleware(ra)
	return nil
}
