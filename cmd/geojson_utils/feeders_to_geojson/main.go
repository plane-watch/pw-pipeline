package main

import (
	"fmt"
	"os"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/paulmach/orb"
	"github.com/paulmach/orb/geojson"
	"github.com/urfave/cli/v2"
	"plane.watch/lib/export"
	"plane.watch/lib/nats_io"
	"plane.watch/lib/setup"
)

const version = "0.0.1"

func main() {
	app := cli.NewApp()

	app.Name = "FeedersToGeoJSON"
	app.Description = `This program outputs GeoJSON containing all feeders to stdout`
	app.Version = version

	app.Flags = []cli.Flag{}

	app.Action = runApp

	setup.IncludeSinkFlags(app)

	err := app.Run(os.Args)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runApp(c *cli.Context) error {

	// connect to nats
	natsServer, err := nats_io.NewServer(
		nats_io.WithConnections(false, true),
		nats_io.WithServer(c.String(setup.Sink), "feeders-to-geojson"),
	)
	if err != nil {
		return fmt.Errorf("failed to setup nats connection: %w", err)
	}

	// get feeders via nats
	ret, err := natsServer.Request(export.NatsApiFeederListV1, nil, map[string]string{}, time.Second)
	if err != nil {
		return fmt.Errorf("failed to fetch feeder list from atc api: %w", err)
	}
	json := jsoniter.ConfigFastest
	feeders := make(export.Feeders, 0, 1000)
	err = json.Unmarshal(ret, &feeders)
	if err != nil {
		return fmt.Errorf("failed to unmarshal feeder list: %w", err)
	}

	// prepare geojson feature collection
	fc := geojson.NewFeatureCollection()

	// add each feeder as geojson feature
	for _, feeder := range feeders {
		feat := geojson.NewFeature(orb.Point{*feeder.Longitude, *feeder.Latitude})
		feat.Properties["User"] = feeder.User
		feat.Properties["Id"] = feeder.Id
		feat.Properties["Label"] = feeder.Label
		feat.Properties["FeederCode"] = feeder.FeederCode
		fc.Append(feat)
	}

	jb, err := fc.MarshalJSON()
	if err != nil {
		return fmt.Errorf("failed to marshal GeoJSON: %w", err)
	}

	fmt.Println(string(jb))
	return nil
}
