# Plane.Watch Pipeline

This repo contains several tools used to decode, understand and process ADSB information.

We typically develop against the latest version of Golang.

## Info
 The `pw_ingest`, `pw_router` and `pw_ws_broker` commands are connected together with a message queue. We currently
support:
* Redis PubSub
* Nats.io

## Components

### Commands
You can find commands in the `cmd/` directory

#### Filtering and Finding
There are some small programs to help find examples of ADSB messages

* df_example_finder
* recorder

#### Displaying

##### website_decode

You can find it running at http://jasonplayne.com:8080/. Throw in your ADSB message and it'll show you what it can about
the message.

##### ingest_tap

used to view stuff going on in the nats pipeline

#### Processing

* pw_ingest
* pw_router
* pw_ws_broker
* pw_atc_api

These three components are used to take incoming ADSB messages (beast, avr, sbs1) decode them, turn them into plane
tracking json blobs and make them available via websocket to a website.

### Libraries

Reusable bits!

#### Decoding and Tracking

* tile_grid
* tracker
* tracker/beast
* tracker/mode_s
* tracker/sbs1
* export

These libs form the basis of the whole decoding part

#### Helpers

The other libs in the `lib/` folder are common shared parts of the larger whole.

## Further Reading

Some Links for More Information around ADSB

* http://airmetar.main.jp/radio/ADS-B%20Decoding%20Guide.pdf
* https://mode-s.org/decode/book-the_1090mhz_riddle-junzi_sun.pdf
* https://pypi.org/project/pyModeS/
* https://mode-s.org/decode/content/mode-s/6-els.html
* https://www.eurocontrol.int/sites/default/files/content/documents/nm/asterix/archives/asterix-cat062-system-track-data-part9-v1.10-122009.pdf

## Building

### Development

    make

That's it. It runs the tests and builds the binaries and puts them into `bin/`

If you want to build a specific binary

    go build plane.watch/cmd/pw_ingest

or you can run it with

    go run plane.watch/cmd/pw_ingest

### Building Docker Containers

    docker build -t plane.watch/pw_ws_broker:latest -f docker/pw_ws_broker/Dockerfile .
    docker build -t plane.watch/pw_router:latest -f docker/pw_router/Dockerfile .
    docker build -t plane.watch/pw_ingest:latest -f docker/pw_ingest/Dockerfile .
