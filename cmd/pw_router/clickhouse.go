package main

import (
	"strconv"
	"time"

	"github.com/paulmach/orb"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"plane.watch/lib/clickhouse"
	"plane.watch/lib/export"
)

type (
	DataStream struct {
		low, high chan *export.PlaneLocation
		chs       *clickhouse.Server
		log       zerolog.Logger
	}

	chRow struct {
		Icao            string
		LatLon          orb.Point
		Lat             float64
		Lon             float64
		Heading         float64
		Velocity        float64
		Altitude        int32
		VerticalRate    int32
		AltitudeUnits   string
		CallSign        string
		FlightStatus    string
		OnGround        bool
		Airframe        string
		AirframeType    string
		HasLocation     bool
		HasHeading      bool
		HasVerticalRate bool
		HasVelocity     bool
		SourceTags      map[string]uint32
		Squawk          uint32
		Special         string
		TrackedSince    time.Time
		LastMsg         time.Time
		FlagCode        string
		Operator        string
		RegisteredOwner string
		Registration    string
		RouteCode       string
		Serial          string
		TileLocation    string
		TypeCode        string
	}
)

func NewDataStreams(chs *clickhouse.Server) *DataStream {
	ds := &DataStream{
		low:  make(chan *export.PlaneLocation, 1000),
		high: make(chan *export.PlaneLocation, 2000),
		chs:  chs,
		log:  log.With().Str("section", "ch data stream").Logger(),
	}
	go ds.handleQueue(ds.low, "location_updates_low")
	go ds.handleQueue(ds.high, "location_updates_high")

	return ds
}

func (ds *DataStream) AddLow(frame *export.PlaneLocation) {
	if "repeat" != frame.SourceTag {
		ds.low <- frame
	}
}

func (ds *DataStream) AddHigh(frame *export.PlaneLocation) {
	if "repeat" != frame.SourceTag {
		ds.high <- frame
	}
}

// handleQueue single threadedly accumulates and sends data to clickhouse for the given queue/table
func (ds *DataStream) handleQueue(q chan *export.PlaneLocation, table string) {
	ticker := time.NewTicker(time.Second)
	maxNumItems := 50_000
	updates := make([]any, maxNumItems)
	updateId := 0
	unPtr := func(s *string) string {
		if nil == s {
			return ""
		}
		return *s
	}
	send := func() {
		ds.log.Debug().Int("num", updateId).Msg("Sending Batch To Clickhouse")
		if err := ds.chs.Inserts(table, updates, updateId); nil != err {
			ds.log.Err(err).Msg("Did not save location update to clickhouse")
		}
		updateId = 0
	}
	for {
		select {
		case <-ticker.C:
			send()
		case loc := <-q:
			squawk, _ := strconv.ParseUint(loc.Squawk, 10, 32)
			updates[updateId] = &chRow{
				Icao:            loc.Icao,
				LatLon:          orb.Point{loc.Lat, loc.Lon},
				Heading:         loc.Heading,
				Velocity:        loc.Velocity,
				Altitude:        int32(loc.Altitude),
				VerticalRate:    int32(loc.VerticalRate),
				AltitudeUnits:   loc.AltitudeUnits,
				CallSign:        unPtr(loc.CallSign),
				FlightStatus:    loc.FlightStatus,
				OnGround:        loc.OnGround,
				Airframe:        loc.Airframe,
				AirframeType:    loc.AirframeType,
				HasLocation:     loc.HasLocation,
				HasHeading:      loc.HasHeading,
				HasVerticalRate: loc.HasVerticalRate,
				HasVelocity:     loc.HasVelocity,
				SourceTags:      loc.PrepareSourceTags(),
				Squawk:          uint32(squawk),
				Special:         loc.Special,
				TrackedSince:    loc.TrackedSince.UTC(),
				LastMsg:         loc.LastMsg.UTC(),
				FlagCode:        unPtr(loc.FlagCode),
				Operator:        unPtr(loc.Operator),
				RegisteredOwner: unPtr(loc.RegisteredOwner),
				Registration:    unPtr(loc.Registration),
				RouteCode:       unPtr(loc.RouteCode),
				Serial:          unPtr(loc.Serial),
				TileLocation:    loc.TileLocation,
				TypeCode:        unPtr(loc.TypeCode),
			}

			updateId++
			if updateId >= maxNumItems-1 {
				send()
			}
		}
	}
}
