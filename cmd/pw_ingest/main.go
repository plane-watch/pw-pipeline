package main

import (
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/renderer"
	"github.com/olekukonko/tablewriter/tw"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
	"plane.watch/lib/dedupe"
	"plane.watch/lib/example_finder"
	"plane.watch/lib/logging"
	"plane.watch/lib/middleware"
	"plane.watch/lib/monitoring"
	"plane.watch/lib/nats_io"
	"plane.watch/lib/setup"
	"plane.watch/lib/sink"
	"plane.watch/lib/tracker"
)

const (
	DedupeFilter       = "dedupe-filter"
	FilterLocationOnly = "locations-only"
	FilterIcao         = "icao"
	DecodeWorkerCount  = "decode-worker-count"
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

	app.Version = version
	app.Name = "Plane Watch Client"
	app.Usage = "Reads from dump1090 and sends it to https://plane.watch/"

	app.Description = `This program takes a stream of plane tracking info (beast/avr/sbs1), tracks the planes and ` +
		`outputs all sorts of interesting information to the configured sink, including decoded and tracked planes in JSON format.` +
		"\n\n" +
		`example: pw_ingest --fetch=beast://crawled.mapwithlove.com:3004 --sink=nats://localhost:4222 --tag="cool-stuff" --quiet simple`

	setup.IncludeSourceFlags(app)
	setup.IncludeSinkFlags(app)
	logging.IncludeVerbosityFlags(app)
	monitoring.IncludeMonitoringFlags(app, 9602)

	app.Commands = []*cli.Command{
		{
			Name:      "simple",
			Usage:     "Gather ADS-B data and sends it to the configured output. just a log of info",
			Action:    runSimple,
			ArgsUsage: "[app.log - A file name to output to or stdout if not specified]",
		},
		{
			Name:   "daemon",
			Usage:  "Docker Daemon Mode",
			Action: runDaemon,
		},
		{
			Name:   "filter",
			Usage:  "Find examples from input",
			Action: runDfFilter,
			Flags: []cli.Flag{
				&cli.StringSliceFlag{
					Name:  FilterIcao,
					Usage: "Plane ICAO to filter on. e,g, --icao=E48DF6 --icao=123ABC",
				},
				&cli.BoolFlag{
					Name:  FilterLocationOnly,
					Usage: "Filter location updates only",
				},
			},
		},
	}
	app.Flags = append(
		app.Flags,
		&cli.BoolFlag{
			Name:    DedupeFilter,
			Usage:   "Include the usage of the ADSB Message Deduplication Filter. Useful for combo feeds",
			EnvVars: []string{"DEDUPE"},
		},
		&cli.IntFlag{
			Name:  DecodeWorkerCount,
			Usage: "The number of tracker workers we spawn to handle the incoming traffic",
			Value: 1,
		},
		&cli.BoolFlag{
			Name:  "show-stats",
			Usage: "When pw-ingest quits, show some stats about the processed plane data, handy when looking at files",
		},
	)

	app.Before = func(c *cli.Context) error {
		logging.SetLoggingLevel(c)
		return nil
	}

	if err := app.Run(os.Args); nil != err {
		log.Error().Err(err).Msg("Finishing with an error")
		os.Exit(1)
	}
}

func commonSetup(c *cli.Context) (*tracker.Tracker, error) {
	monitoring.RunWebServer(c)

	producers, err := setup.HandleSourceFlags(c)
	if nil != err {
		return nil, err
	}

	trackerOpts := make([]tracker.Option, 0)
	trackerOpts = append(
		trackerOpts,
		tracker.WithPrometheusCounters(
			prometheusGaugeCurrentPlanes,
			prometheusCounterFramesDecoded,
			prometheusCounterFramesErrored,
			prometheusCounterPlanesPurgedBeforeViable,
		),
		tracker.WithDecodeWorkerCount(c.Int(DecodeWorkerCount)),
	)
	trk := tracker.NewTracker(trackerOpts...)

	if c.Bool(DedupeFilter) {
		trk.AddMiddleware(dedupe.NewFilter(dedupe.WithDedupeCounter(prometheusOutputFrameDedupe)))
		// trk.AddMiddleware(dedupe.NewFilterBTree(dedupe.WithDedupeCounterBTree(prometheusOutputFrameDedupe), dedupe.WithBtreeDegree(16)))
	}
	sinkDest, err := setup.HandleSinkFlag(c, "pw_ingest")
	if nil != err {
		return nil, err
	}
	trk.SetSink(sinkDest)

	if sinkType, ok := sinkDest.(*sink.Sink); ok {
		if ns, ok := sinkType.Server().(*nats_io.Server); ok {
			trk.AddMiddleware(middleware.NewIngestTap(ns))
		}
	}

	for _, p := range producers {
		trk.AddProducer(p)
	}

	return trk, nil
}

func runSimple(c *cli.Context) error {
	logging.ConfigureForCli()

	trk, err := commonSetup(c)

	if nil != err {
		return err
	}

	go trk.StopOnCancel()
	trk.Wait()

	if c.Bool("show-stats") {
		showPlaneStats(trk)
	}

	return nil
}

func showPlaneStats(trk *tracker.Tracker) {
	tbl := tablewriter.NewTable(
		os.Stdout,
		tablewriter.WithRenderer(
			renderer.NewBlueprint(
				tw.Rendition{
					Borders: tw.BorderNone,
					Symbols: tw.NewSymbols(tw.StyleASCII),
					Settings: tw.Settings{
						Separators: tw.Separators{
							ShowHeader:     tw.On,
							ShowFooter:     tw.Off,
							BetweenRows:    tw.Off,
							BetweenColumns: tw.On,
						},
						Lines: tw.Lines{
							ShowTop:        tw.On,
							ShowBottom:     tw.Off,
							ShowHeaderLine: tw.On,
							ShowFooterLine: tw.Off,
						},
						CompactMode: tw.Off,
					},
					Streaming: false,
				})),
	)
	rows := make([][]string, 0)
	headers := []string{"ICAO", "Squawk", "Reg", "Flight Number", "Type", "MSG Count"}
	trk.EachPlane(func(p *tracker.Plane) bool {
		rows = append(rows, []string{
			p.IcaoIdentifierStr(),
			p.SquawkIdentityStr(),
			unPtr(p.Registration()),
			p.FlightNumber(),
			p.AirFrame(),
			strconv.FormatUint(p.MsgCount(), 10),
		})

		return true
	})

	slices.SortFunc(rows, func(a, b []string) int {
		return strings.Compare(a[0], b[0])
	})

	tbl.Header(headers)
	for _, row := range rows {
		_ = tbl.Append(row)
	}
	_ = tbl.Render()
}

func unPtr[T any](pt *T) T {
	var def T
	if pt == nil {
		return def
	}
	return *pt
}

// runDfFilter is a special mode for hunting down DF examples from live inputs
func runDfFilter(c *cli.Context) error {
	logging.ConfigureForCli()

	trk, err := commonSetup(c)
	if nil != err {
		return err
	}

	var filterOpts []example_finder.Option
	if c.Bool(FilterLocationOnly) {
		filterOpts = append(filterOpts, example_finder.WithDF17MessageTypeLocation())
	} else {
		filterOpts = append(filterOpts, example_finder.WithDownlinkFormatType(17))
	}
	for _, icao := range c.StringSlice(FilterIcao) {
		filterOpts = append(filterOpts, example_finder.WithPlaneIcaoStr(icao))
	}
	trk.AddMiddleware(example_finder.NewFilter(filterOpts...))

	trk.Wait()
	return nil
}

// runDaemon does not have pretty cli output (just JSON from logging)
func runDaemon(c *cli.Context) error {
	trk, err := commonSetup(c)
	if nil != err {
		return err
	}

	go trk.StopOnCancel()
	trk.Wait()
	return nil
}
