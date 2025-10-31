package main

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
	"plane.watch/lib/export"
	"plane.watch/lib/logging"
	"plane.watch/lib/nats_io"
	"plane.watch/lib/producer"
	"plane.watch/lib/setup"
	"plane.watch/lib/stunnel"
)

var (
	version = "dev"

	//go:embed full-feed.beast
	beastData []byte
)

func main() {
	app := cli.NewApp()

	app.Name = "Runway"
	app.Description = `This program acts as a server for multiple stunnel-based endpoints, ` +
		`authenticates the feeder based on API key (UUID) check against atc.plane.watch, ` +
		`routes data to feed-in containers.`
	app.Version = version

	app.Flags = []cli.Flag{
		&cli.IntFlag{
			Name:     "feeders",
			Category: "Stress Testing",
			Usage:    "the number of feeders to simulate",
			Required: true,
		},
		&cli.DurationFlag{
			Name:     "duration",
			Category: "Stress Testing",
			Usage:    "how long to run stress test",
			Value:    time.Second * 30,
		},
		&cli.StringFlag{
			Name:     "beastout",
			Category: "Stress Testing",
			Usage:    "plane.watch endpoint for BEAST data",
			Value:    "feed.push.plane.watch:22345", // beta-env by default
			Required: true,
		},
		&cli.DurationFlag{
			Name:     "ifgmin",
			Category: "Stress Testing",
			Usage:    "Per-feeder minimum inter-frame Gap",
			Value:    time.Millisecond * 5,
		},
		&cli.DurationFlag{
			Name:     "ifgmax",
			Category: "Stress Testing",
			Usage:    "Per-feeder maximum inter-frame Gap",
			Value:    time.Millisecond * 50,
		},
	}

	setup.IncludeSinkFlags(app)
	logging.IncludeVerbosityFlags(app)

	app.Before = func(c *cli.Context) error {
		logging.SetLoggingLevel(c)
		return nil
	}

	app.Action = func(c *cli.Context) error {
		return runStress(c)
	}

	if err := app.Run(os.Args); err != nil {
		log.Error().Err(err).Msg("Finishing with an error")
		os.Exit(1)
	}
}

func runStress(c *cli.Context) error {

	// connect to NATS
	log.Info().Msg("connecting to nats")
	natsServer, err := nats_io.NewServer(
		nats_io.WithConnections(false, true),
		nats_io.WithServer(c.String("sink"), "stress-atc-client"),
	)
	if err != nil {
		return fmt.Errorf("failed to setup nats connection: %w", err)
	}

	// get list of feeders (for valid API keys)
	ret, err := natsServer.Request(export.NatsApiFeederListV1, nil, map[string]string{}, time.Second)
	if err != nil {
		return fmt.Errorf("failed to fetch feeder list from atc api: %w", err)
	}
	json := jsoniter.ConfigFastest
	feeders := make(export.Feeders, 0, 1000)
	err = json.Unmarshal(ret, &feeders)
	if err != nil {
		return fmt.Errorf("failed to decode feeder list: %w", err)
	}
	log.Info().Msgf("got %d feeders", len(feeders))

	wg := sync.WaitGroup{}
	ctx, cancel := context.WithCancel(context.Background())

	// determine max workers
	maxWorkers := min(len(feeders), c.Int("feeders"))
	if maxWorkers < c.Int("feeders") {
		log.Warn().Msgf("number of available feeder API keys is less than --feeders: %d", maxWorkers)
	}

	// spawn workers
	log.Info().Msgf("spawning %d workers", maxWorkers)
	for i := 0; i < maxWorkers; i++ {
		wg.Go(func() {

			// connect
			D, err := stunnel.NewDialler(
				stunnel.WithTimeout(time.Second),
				stunnel.WithSni(feeders[i].ApiKey.String()),
				stunnel.WithAddress(c.String("beastout")),
				stunnel.WithInsecure(),
			)
			if err != nil {
				// handle error by killing all workers and bailing out
				log.Error().Err(err).Msg("Failed to create stunnel dialer")
				cancel()
				return
			}
			conn, err := D.Dial()
			if err != nil {
				// handle error by killing all workers and bailing out
				log.Error().Err(err).Msg("Failed to connect to remote host")
				cancel()
				return
			}
			defer func() {
				_ = conn.Close()
			}()

			// send beast traffic forever (until context closure
			for {

				// check for context closure
				select {
				case <-ctx.Done():
					return
				default:
				}

				// scan through beast data, sending frames
				S := bufio.NewScanner(bytes.NewReader(beastData))
				S.Split(producer.ScanBeast)
				for S.Scan() && S.Err() == nil {

					// check for context closure
					select {
					case <-ctx.Done():
						return
					default:
					}

					// write frame to connection
					msg := bytes.Clone(S.Bytes())
					_, err := conn.Write(msg)
					if err != nil {
						// handle error by killing all workers and bailing out
						log.Error().Err(err).Msg("error sending data")
						cancel()
						return
					}

					// fake inter packet gap between 5 and 100ms
					ipg := rand.Int63n(c.Duration("ifgmax").Milliseconds())
					ipg += c.Duration("ifgmin").Milliseconds()
					time.Sleep(time.Duration(ipg) * time.Millisecond)
				}
			}
		})
	}
	log.Info().Msgf("workers spawned, running for %s", c.Duration("duration").String())

	t := time.NewTicker(c.Duration("duration"))
	select {
	case <-t.C:
		log.Info().Msg("stress test finished successfully")
		cancel()
	case <-ctx.Done():
		log.Info().Msg("one or more workers errored")
	}

	cancel()
	wg.Wait()

	return nil
}
