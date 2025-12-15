package main

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
	"plane.watch/lib/export"
	"plane.watch/lib/middleware"
	"plane.watch/lib/nats_io"
	"plane.watch/lib/randstr"
	"plane.watch/lib/sink"
	"plane.watch/lib/ws_protocol"
)

const (
	tapActionAdd    = "add"
	tapActionRemove = "remove"
)

type (
	tapHeaders map[string]string

	PlaneWatchTapper struct {
		natsSvr       *nats_io.Server
		wsLow, wsHigh *ws_protocol.WsClient

		exitChannels map[string]chan bool
		taps         []tapHeaders

		logger zerolog.Logger

		queueLocations             string
		queueLocationsEnriched     string
		queueLocationsEnrichedLow  string
		queueLocationsEnrichedHigh string

		wsHandlersLow  []*wsFilterFunc
		wsHandlersHigh []*wsFilterFunc
		mu             sync.Mutex
	}

	wsFilterFunc struct {
		icao, feederTag string
		speed           string
		filter          wsHandler
	}

	IngestTapHandler func(frameType, tag string, data []byte)
	wsHandler        func(location *export.PlaneLocation)

	Option func(*PlaneWatchTapper)
)

func NewPlaneWatchTapper(opts ...Option) *PlaneWatchTapper {
	pw := &PlaneWatchTapper{
		exitChannels:               make(map[string]chan bool),
		taps:                       make([]tapHeaders, 0),
		logger:                     zerolog.Logger{},
		queueLocations:             sink.QueueLocationUpdates,
		queueLocationsEnriched:     "location-updates-enriched",
		queueLocationsEnrichedLow:  "location-updates-enriched-reduced",
		queueLocationsEnrichedHigh: "location-updates-enriched-merged",

		wsHandlersLow:  make([]*wsFilterFunc, 0),
		wsHandlersHigh: make([]*wsFilterFunc, 0),
	}

	for _, opt := range opts {
		opt(pw)
	}

	return pw
}

func WithLogger(logger zerolog.Logger) Option {
	return func(tapper *PlaneWatchTapper) {
		tapper.logger = logger
	}
}

func (pw *PlaneWatchTapper) Connect(natsServer, wsServer string) error {
	var err error
	pw.natsSvr, err = nats_io.NewServer(
		nats_io.WithConnections(true, true),
		nats_io.WithServer(natsServer, "ingest-nats-tap"),
	)
	if nil != err {
		return err
	}

	pw.wsLow = ws_protocol.NewClient(
		ws_protocol.WithSourceURL(wsServer),
		ws_protocol.WithResponseHandler(pw.wsHandler(&pw.wsHandlersLow)),
		ws_protocol.WithLogger(pw.logger.With().Str("ws", "low").Logger()),
	)
	if err = pw.wsLow.Connect(); err != nil {
		return err
	}
	pw.wsHigh = ws_protocol.NewClient(
		ws_protocol.WithSourceURL(wsServer),
		ws_protocol.WithResponseHandler(pw.wsHandler(&pw.wsHandlersHigh)),
		ws_protocol.WithLogger(pw.logger.With().Str("ws", "high").Logger()),
	)
	return pw.wsHigh.Connect()
}

func (pw *PlaneWatchTapper) Disconnect() {
	// request things stop sending
	for _, tapName := range pw.taps {
		pw.RemoveIncomingTap(tapName)
	}

	// drains the incoming queues before closing all the things
	pw.natsSvr.Close()
	if err := pw.wsLow.Disconnect(); err != nil {
		pw.logger.Error().Err(err).Msg("Did not disconnect from websocket, low")
	}
	if err := pw.wsHigh.Disconnect(); err != nil {
		pw.logger.Error().Err(err).Msg("Did not disconnect from websocket, high")
	}

	// simple sequential disconnect
	for _, exitChan := range pw.exitChannels {
		exitChan <- true
	}
}

func (pw *PlaneWatchTapper) IncomingDataTap(icao, feederKey string, writer IngestTapHandler) error {
	tapSubject := "ingest-natsTap-" + randstr.RandString(20)
	ch, err := pw.natsSvr.Subscribe(tapSubject)
	if err != nil {
		return fmt.Errorf("failed to open Nats tap: %w", err)
	}
	pw.exitChannels[tapSubject] = make(chan bool)

	go func(ch chan *nats.Msg, exitChan chan bool) {
		for {
			select {
			case msg := <-ch:
				writer(msg.Header.Get("type"), msg.Header.Get("tag"), msg.Data)
			case <-exitChan:
				return
			}
		}
	}(ch, pw.exitChannels[tapSubject])

	headers := tapHeaders{
		"action":  tapActionAdd,
		"api-key": feederKey, // the ingest feeder we wish to target
		"icao":    icao,      // the aircraft we wish to look at
		"subject": tapSubject,
	}

	response, errRq := pw.natsSvr.Request(middleware.NatsAPIv1PwIngestTap, []byte{}, headers, time.Second)
	if nil != errRq {
		pw.logger.Error().Err(err).Msg("Failed to request natsTap, are there any pw_ingest's?")
		return errRq
	}
	pw.logger.Debug().Str("response", string(response)).Msg("request response")
	pw.taps = append(pw.taps, headers)

	return nil
}

func (pw *PlaneWatchTapper) RemoveIncomingTap(tap tapHeaders) {
	tap["action"] = tapActionRemove
	response, errRq := pw.natsSvr.Request(middleware.NatsAPIv1PwIngestTap, []byte{}, tap, time.Second)
	if nil != errRq {
		pw.logger.Error().Err(errRq).Msg("Failed to stop natsTap")
		return
	}
	pw.logger.Debug().Str("response", string(response)).Msg("request response")
}

func (pw *PlaneWatchTapper) natsTap(icao, feederKey string, subject string, callback func(*export.PlaneLocation)) error {
	listenCh, err := pw.natsSvr.Subscribe(subject)
	if nil != err {
		return err
	}
	tapSubject := subject + "-" + randstr.RandString(20)

	pw.exitChannels[tapSubject] = make(chan bool)

	go func(ch chan *nats.Msg, exitChan chan bool) {
		for {
			select {
			case msg := <-ch:
				var planeLocation export.PlaneLocation
				err = json.Unmarshal(msg.Data, &planeLocation)
				if nil != err {
					pw.logger.Error().Err(err)
					continue
				}
				if icao != "" && icao != planeLocation.Icao {
					continue
				}
				if feederKey != "" && feederKey != planeLocation.SourceTag {
					continue
				}
				callback(&planeLocation)
			case <-exitChan:
				return
			}
		}
	}(listenCh, pw.exitChannels[tapSubject])

	return nil
}

func (pw *PlaneWatchTapper) AfterIngestTap(icao, feederKey string, callback func(*export.PlaneLocation)) error {
	return pw.natsTap(icao, feederKey, pw.queueLocations, callback)
}

func (pw *PlaneWatchTapper) AfterEnrichmentTap(icao, feederKey string, callback func(*export.PlaneLocation)) error {
	return pw.natsTap(icao, feederKey, pw.queueLocationsEnriched, callback)
}

func (pw *PlaneWatchTapper) AfterRouterLowTap(icao, feederKey string, callback func(*export.PlaneLocation)) error {
	return pw.natsTap(icao, feederKey, pw.queueLocationsEnrichedLow, callback)
}

func (pw *PlaneWatchTapper) AfterRouterHighTap(icao, feederKey string, callback func(*export.PlaneLocation)) error {
	return pw.natsTap(icao, feederKey, pw.queueLocationsEnrichedHigh, callback)
}

func (pw *PlaneWatchTapper) WebSocketTapLow(icao, feederKey string, callback func(*export.PlaneLocation)) error {
	pw.addWsFilterFunc(icao, feederKey, "low", callback)
	return pw.wsLow.SubscribeAllLow()
}
func (pw *PlaneWatchTapper) WebSocketTapHigh(icao, feederKey string, callback func(*export.PlaneLocation)) error {
	pw.addWsFilterFunc(icao, feederKey, "high", callback)
	return pw.wsHigh.SubscribeAllHigh()
}

func (pw *PlaneWatchTapper) addWsFilterFunc(icao, feederKey, speed string, filter wsHandler) {
	pw.mu.Lock()
	defer pw.mu.Unlock()
	if speed == "low" {
		pw.wsHandlersLow = append(pw.wsHandlersLow, &wsFilterFunc{
			icao:      icao,
			feederTag: feederKey,
			speed:     speed,
			filter:    filter,
		})
	} else {
		pw.wsHandlersHigh = append(pw.wsHandlersHigh, &wsFilterFunc{
			icao:      icao,
			feederTag: feederKey,
			filter:    filter,
		})
	}
}

// wsHandler runs through all the handlers we have and calls maybeSend for each plane location
func (pw *PlaneWatchTapper) wsHandler(handlers *[]*wsFilterFunc) func(r *ws_protocol.WsResponse) {
	return func(r *ws_protocol.WsResponse) {
		pw.mu.Lock()
		defer pw.mu.Unlock()
		for _, ff := range *handlers {
			pw.maybeSend(ff, r.Location)
			if r.Locations != nil {
				for _, loc := range r.Locations {
					pw.maybeSend(ff, loc)
				}
			}
		}
	}
}

// maybeSend takes into account the filter and calls the user provided handler if it matches
func (pw *PlaneWatchTapper) maybeSend(ff *wsFilterFunc, loc *export.PlaneLocation) {
	if nil == loc {
		return
	}
	if ff.icao != "" && ff.icao != loc.Icao {
		return
	}
	if ff.feederTag != "" {
		if _, ok := loc.SourceTags[ff.feederTag]; !ok {
			return
		}
	}

	ff.filter(loc)
}
