package main

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
	"plane.watch/lib/beastscanner"
	"plane.watch/lib/logging"
	"plane.watch/lib/tracker/beast"
)

var (
	version = "dev"
)

func main() {

	app := cli.NewApp()

	app.Name = "mlat-client"
	app.Description = `plane watch mlat client`
	app.Version = version

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Category: "Network",
			Name:     "beasthost",
			Usage:    "Host to dial for BEAST data",
			Value:    "127.0.0.1",
			EnvVars:  []string{"BEASTHOST"},
		},
		&cli.StringFlag{
			Category: "Network",
			Name:     "beastport",
			Usage:    "Port for BEASTHOST",
			Value:    "30005",
			EnvVars:  []string{"BEASTPORT"},
		},
	}

	app.Action = runMLATClient

	logging.IncludeVerbosityFlags(app)

	app.Before = func(c *cli.Context) error {
		logging.SetLoggingLevel(c)
		return nil
	}

	if err := app.Run(os.Args); err != nil {
		log.Error().Err(err).Msg("Finishing with an error")
		os.Exit(1)
	}

}

func runMLATClient(c *cli.Context) error {

	// open connection to beast provider
	addr := fmt.Sprintf("%s:%s", c.String("beasthost"), c.String("beastport"))
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = conn.Close()
	}()

	// run beast scanner
	_, err = beastscanner.Run(
		beastscanner.WithBEASTConnection(conn),
		beastscanner.WithEscapeCollapsing(false),
		beastscanner.WithFrameHandler(func(frame []byte) error {
			f, err := beast.NewFrame(frame, false)
			if err != nil {
				return fmt.Errorf("could not parse frame: %w", err)
			}

			// todo(mikenye): currently just dump frames to stdout.
			//  - Idea is to use ark ecs to track vessels.
			//  - For vessels without a position, use MLAT.
			//  - For vessels with a position, use as reference.
			fmt.Println(f.String())

			return nil
		}),
	)

	return nil
}
