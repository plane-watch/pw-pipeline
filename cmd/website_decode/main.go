package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
	"plane.watch/lib/export"
	"plane.watch/lib/logging"
	"plane.watch/lib/monitoring"
	"plane.watch/lib/tracker"
	"plane.watch/lib/tracker/mode_s"
)

// this is a website where you put in one or more Mode S frames and they are decoded
// in a way that is informational

func main() {
	app := cli.NewApp()
	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "listen-http",
			Aliases: []string{"port"},
			Value:   ":8080",
			Usage:   "Port to run the website on (http)",
		},
		&cli.StringFlag{
			Name:  "listen-https",
			Value: ":8443",
			Usage: "Port to run the website on (https)",
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
	}
	logging.IncludeVerbosityFlags(app)
	monitoring.IncludeMonitoringFlags(app, 9605)

	app.Before = func(c *cli.Context) error {
		logging.SetLoggingLevel(c)
		return nil
	}

	app.Action = runHttpServer
	logging.ConfigureForCli()

	if err := app.Run(os.Args); nil != err {
		log.Error().Err(err).Send()
	}
}

func runHttpServer(c *cli.Context) error {
	cert := c.String("tls-cert")
	certKey := c.String("tls-cert-key")
	if ("" != cert || "" != certKey) && ("" == cert || "" == certKey) {
		return errors.New("please provide both certificate and key")
	}
	monitoring.RunWebServer(c)
	var htdocsPath string
	var err error
	var files fs.FS
	if c.NArg() == 0 {
		log.Info().Msg("Using our embedded filesystem")
		files, err = fs.Sub(embeddedHtdocs, "htdocs")
		if nil != err {
			panic(err)
		}
	} else {
		htdocsPath = path.Clean(c.Args().First())
		log.Info().Str("path", htdocsPath).Msg("Serving HTTP Docs from")
		files = os.DirFS(htdocsPath)
	}
	logging.SetLoggingLevel(c)

	http.Handle("/", http.FileServer(http.FS(files)))

	http.HandleFunc("/decode", func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if r := recover(); r != nil {
				_, _ = fmt.Fprintln(w, "<pre>Failed big time...")
				_, _ = fmt.Fprintln(w, r)
				log.Error().
					Stack().
					Err(errors.New("failed to handle request")).
					Interface("recover", r)
			}
		}()
		switch r.URL.Path {
		case "/decode":
			pt := tracker.NewTracker()
			var submittedPackets string
			_ = r.ParseForm()

			refLat := getAsFloat(r.FormValue("refLat"))
			refLon := getAsFloat(r.FormValue("refLon"))
			submittedPackets = r.FormValue("packet")
			if "" == submittedPackets {
				_, _ = fmt.Fprintln(w, "No Packet Provided")
				return
			}
			packets := strings.Split(submittedPackets, ";")
			icaoList := make(map[uint32]uint32)
			for _, packet := range packets {
				packet = strings.TrimSpace(packet)
				if "" == packet {
					continue
				}
				log.Debug().Str("frame", packet).Msg("Decoding Frame")
				frame, err := mode_s.DecodeString(packet, time.Now())
				if err != nil {
					_, _ = fmt.Fprintf(
						w,
						"\n<span class='text-danger'>Decoding Failure.\n%s</span>\n\n",
						html.EscapeString(err.Error()),
					)
					//return
				}
				if nil == frame {
					_, _ = fmt.Fprintln(w, "Not an AVR Frame", html.EscapeString(err.Error()))
					return
				}
				source := tracker.FrameSource{RefLat: refLat, RefLon: refLon}
				pt.GetPlane(frame.Icao()).HandleModeSFrame(frame, &source)
				icaoList[frame.Icao()] = frame.Icao()
				frame.Describe(w)
			}

			for _, icao := range icaoList {
				_, _ = fmt.Fprintln(w, "")
				plane := pt.GetPlane(icao)
				pl := export.NewPlaneLocation(plane, true, false, "")

				encoded, _ := json.MarshalIndent(pl, "", "  ")

				_, _ = fmt.Fprint(w, string(encoded))
			}

			pt = nil
		default:
			http.NotFound(w, r)
			_, _ = fmt.Fprintln(w, "<br/>\n"+r.RequestURI)
		}

	})

	exitChan := make(chan bool)
	go listenHttp(exitChan, c.String("listen-http"))
	if "" != cert {
		go listenHttps(exitChan, cert, certKey, c.String("listen-https"))
	}
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	select {
	case <-exitChan:
		log.Error().Msg("There as an error and we are exiting")
	case <-sc:
		log.Debug().Msg("Kill Signal Received")
	}

	return nil
}

func listenHttp(exitChan chan bool, port string) {
	log.Info().Msgf("Decode Listening on %s...", port)
	if err := http.ListenAndServe(port, nil); nil != err {
		if http.ErrServerClosed != err {
			log.Error().Err(err).Send()
		}
	}
	exitChan <- true
}

func listenHttps(exitChan chan bool, cert, certKey, port string) {
	log.Info().Msgf("Decode Listening on %s...", port)
	if err := http.ListenAndServeTLS(port, cert, certKey, nil); nil != err {
		if http.ErrServerClosed != err {
			log.Error().Err(err).Send()
		}
	}
	exitChan <- true
}

func getAsFloat(in string) *float64 {
	f, err := strconv.ParseFloat(in, 64)
	if nil == err {
		return &f
	}
	return nil
}
