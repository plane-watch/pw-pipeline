package setup

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
	"plane.watch/lib/producer"
	"plane.watch/lib/tracker"
)

const (
	Fetch  = "fetch"
	Listen = "listen"
	File   = "file"
	RefLat = "ref-lat"
	RefLon = "ref-lon"
	Tag    = "tag"
	Adsc   = "ads-c"
)

var (
	prometheusInputBeastFrames prometheus.Counter
	prometheusInputAvrFrames   prometheus.Counter
	prometheusInputSbs1Frames  prometheus.Counter
)

func IncludeSourceFlags(app *cli.App) {
	sourceFlags := []cli.Flag{
		&cli.StringSliceFlag{
			Name:    Fetch,
			Usage:   "The Source in URL Form. [avr|beast|sbs1]://host:port?tag=MYTAG&refLat=-31.0&refLon=115.0",
			EnvVars: []string{"SOURCE"},
		},
		&cli.StringSliceFlag{
			Name:    Listen,
			Usage:   "The Source in URL Form. [avr|beast|sbs1]://host:port?tag=MYTAG&refLat=-31.0&refLon=115.0",
			EnvVars: []string{"LISTEN"},
		},
		&cli.StringSliceFlag{
			Name:    File,
			Usage:   "The Source in URL Form. [avr|beast|sbs1]:///path/to/file?tag=MYTAG&refLat=-31.0&refLon=115.0&delay=no",
			EnvVars: []string{"FILE"},
		},

		&cli.Float64Flag{
			Name:    RefLat,
			Usage:   "The reference latitude for decoding messages. Needs to be within 45nm of where the messages are generated.",
			EnvVars: []string{"REF_LAT", "LAT"},
		},
		&cli.Float64Flag{
			Name:    RefLon,
			Usage:   "The reference longitude for decoding messages. Needs to be within 45nm of where the messages are generated.",
			EnvVars: []string{"REF_LON", "LONG"},
		},

		&cli.StringFlag{
			Name:    Tag,
			Usage:   "A value that is included in the payloads output to the Sinks. Useful for knowing where something came from",
			EnvVars: []string{"TAG"},
		},
		&cli.BoolFlag{
			Name:    Adsc,
			Usage:   "Used for ADS-C feeds which arrive in SBS1 format and get updates roughly ever half hour",
			EnvVars: []string{"ASD_C"},
		},
	}

	app.Flags = append(app.Flags, sourceFlags...)
}

func HandleSourceFlags(c *cli.Context) ([]tracker.Producer, error) {
	refLat := c.Float64(RefLat)
	refLon := c.Float64(RefLon)
	defaultTag := c.String(Tag)
	isAdsc := c.Bool(Adsc)

	out := make([]tracker.Producer, 0)

	for _, fetchUrl := range c.StringSlice(Fetch) {
		log.Debug().Str("fetch-url", fetchUrl).Msg("With Fetch")
		p, err := handleSource(fetchUrl, defaultTag, refLat, refLon, false, isAdsc)
		if nil != err {
			log.Error().Err(err).Str("url", fetchUrl).Str("what", "fetch").Msg("Failed setup source")
			return nil, err
		} else {
			out = append(out, p)
		}
	}
	for _, listenUrl := range c.StringSlice(Listen) {
		log.Debug().Str("listen-url", listenUrl).Msg("With Listen")
		p, err := handleSource(listenUrl, defaultTag, refLat, refLon, true, isAdsc)
		if nil != err {
			log.Error().Err(err).Str("url", listenUrl).Str("what", "listen").Msg("Failed setup listen")
			return nil, err
		} else {
			out = append(out, p)
		}
	}
	for _, fileUrl := range c.StringSlice(File) {
		log.Debug().Str("file-url", fileUrl).Msg("With File")
		p, err := handleFileSource(fileUrl, defaultTag, refLat, refLon)
		if nil != err {
			log.Error().Err(err).Str("url", fileUrl).Msgf("Failed to understand URL: %s", err)
			return nil, err
		} else {
			out = append(out, p)
		}
	}

	return out, nil
}

func getRef(parsedUrl *url.URL, what string, defaultRef float64) float64 {
	if nil == parsedUrl {
		return 0
	}
	if parsedUrl.Query().Has(what) {
		f, err := strconv.ParseFloat(parsedUrl.Query().Get(what), 64)
		if nil == err {
			return f
		}
		log.Error().Err(err).Str("query_param", what).Msg("Could not determine reference value")
	}
	return defaultRef
}

func handleSource(urlSource, defaultTag string, defaultRefLat, defaultRefLon float64, listen, isAdsc bool) (tracker.Producer, error) {
	parsedUrl, err := url.Parse(urlSource)
	if err != nil {
		return nil, err
	}

	prometheusInputBeastFrames = promauto.NewCounter(prometheus.CounterOpts{
		Name: "pw_ingest_input_beast_total",
		Help: "The total number of beast frames processed.",
	})
	prometheusInputAvrFrames = promauto.NewCounter(prometheus.CounterOpts{
		Name: "pw_ingest_input_avr_total",
		Help: "The total number of AVR frames processed.",
	})
	prometheusInputSbs1Frames = promauto.NewCounter(prometheus.CounterOpts{
		Name: "pw_ingest_input_sbs1_total",
		Help: "The total number of SBS1 frames processed.",
	})

	producerOpts := make([]producer.Option, 3)
	producerOpts[0] = producer.WithSourceTag(getTag(parsedUrl, defaultTag))

	switch strings.ToLower(parsedUrl.Scheme) {
	case "avr":
		producerOpts[1] = producer.WithType(producer.Avr)
	case "beast":
		producerOpts[1] = producer.WithType(producer.Beast)
	case "sbs1":
		producerOpts[1] = producer.WithType(producer.Sbs1)
	default:
		return nil, fmt.Errorf("unknown scheme: %s, expected one of [avr|beast|sbs1]", parsedUrl.Scheme)
	}
	producerOpts[2] = producer.WithPrometheusCounters(prometheusInputAvrFrames, prometheusInputBeastFrames, prometheusInputSbs1Frames)

	refLat := getRef(parsedUrl, "refLat", defaultRefLat)
	refLon := getRef(parsedUrl, "refLon", defaultRefLon)

	if refLat != 0 && refLon != 0 {
		producerOpts = append(producerOpts, producer.WithReferenceLatLon(&refLat, &refLon))
	} else {
		log.Error().
			Float64("ref-lat", refLat).
			Float64("ref-lon", refLon).
			Msg("Do not have a reference lat/lon - will not decode surface position frames")
	}

	if listen {
		producerOpts = append(producerOpts, producer.WithListener(parsedUrl.Hostname(), parsedUrl.Port()))
	} else {
		producerOpts = append(producerOpts, producer.WithFetcher(parsedUrl.Hostname(), parsedUrl.Port()))
	}

	if isAdsc {
		producerOpts = append(producerOpts, producer.WithKeepAliveRepeater())
	}

	return producer.New(producerOpts...), nil
}

func handleFileSource(urlFile, defaultTag string, defaultRefLat, defaultRefLon float64) (tracker.Producer, error) {
	parsedUrl, err := url.Parse(urlFile)
	if nil != err {
		return nil, err
	}
	producerOpts := make([]producer.Option, 1)
	switch strings.ToLower(parsedUrl.Scheme) {
	case "avr":
		producerOpts[0] = producer.WithType(producer.Avr)
	case "beast":
		producerOpts[0] = producer.WithType(producer.Beast)
		delay := false
		if parsedUrl.Query().Has("delay") {
			switch strings.ToLower(parsedUrl.Query().Get("delay")) {
			case "", "no", "false", "0":
				delay = false
			default:
				delay = true
			}
		}
		producerOpts = append(producerOpts, producer.WithBeastDelay(delay))
	case "sbs1":
		producerOpts[0] = producer.WithType(producer.Sbs1)
	default:
		return nil, fmt.Errorf("unknown file Type: %s", parsedUrl.Scheme)
	}
	refLat := getRef(parsedUrl, "refLat", defaultRefLat)
	refLon := getRef(parsedUrl, "refLon", defaultRefLon)
	if refLat != 0 && refLon != 0 {
		producerOpts = append(producerOpts, producer.WithReferenceLatLon(&refLat, &refLon))
	}

	producerOpts = append(
		producerOpts,
		producer.WithSourceTag(getTag(parsedUrl, defaultTag)),
		producer.WithFiles([]string{parsedUrl.Path}),
	)

	return producer.New(producerOpts...), nil
}
