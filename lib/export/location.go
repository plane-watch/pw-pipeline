package export

import (
	"strings"
	"sync"

	jsoniter "github.com/json-iterator/go"
	"github.com/rs/zerolog/log"
	"plane.watch/lib/tracker"
)

func NewPlaneLocation(plane *tracker.Plane, isNew, isRemoved bool, source string) PlaneLocation {
	callSign := strings.TrimSpace(plane.FlightNumber())
	pl := PlaneLocation{
		New:             isNew,
		Removed:         isRemoved,
		Icao:            plane.IcaoIdentifierStr(),
		Lat:             plane.Lat(),
		Lon:             plane.Lon(),
		Heading:         plane.Heading(),
		Altitude:        int(plane.Altitude()),
		VerticalRate:    plane.VerticalRate(),
		AltitudeUnits:   plane.AltitudeUnits(),
		Velocity:        plane.Velocity(),
		CallSign:        &callSign,
		FlightStatus:    plane.FlightStatus(),
		OnGround:        plane.OnGround(),
		Airframe:        plane.AirFrame(),
		AirframeType:    plane.AirFrameType(),
		Squawk:          plane.SquawkIdentityStr(),
		Special:         plane.Special(),
		AircraftWidth:   plane.AirFrameWidth(),
		AircraftLength:  plane.AirFrameLength(),
		Registration:    plane.Registration(),
		HasAltitude:     plane.HasAltitude(),
		HasLocation:     plane.HasLocation(),
		HasHeading:      plane.HasHeading(),
		HasVerticalRate: plane.HasVerticalRate(),
		HasVelocity:     plane.HasVelocity(),
		HasFlightStatus: plane.HasFlightStatus(),
		HasOnGround:     plane.HasOnGround(),
		SourceTag:       source,
		TileLocation:    plane.GridTileLocation(),
		LastMsg:         plane.LastSeen().UTC(),
		TrackedSince:    plane.TrackedSince().UTC(),
		SignalRssi:      plane.SignalLevel(),
		Updates: Updates{
			Location:     plane.LocationUpdatedAt().UTC(),
			Altitude:     plane.AltitudeUpdatedAt().UTC(),
			Velocity:     plane.VelocityUpdatedAt().UTC(),
			Heading:      plane.HeadingUpdatedAt().UTC(),
			VerticalRate: plane.VerticalRateUpdatedAt().UTC(),
			OnGround:     plane.OnGroundUpdatedAt().UTC(),
			FlightStatus: plane.FlightStatusUpdatedAt().UTC(),
			Special:      plane.SpecialUpdatedAt().UTC(),
			Squawk:       plane.SquawkUpdatedAt().UTC(),
			Nav:          plane.NavUpdatedAt().UTC(),
			EHS:          plane.EhsUpdatedAt().UTC(),
		},
		sourceTagsMutex: &sync.Mutex{},
	}
	if nav, ok := plane.Nav(); ok {
		pl.NavAltitudeMCP = nav.SelectedAltitudeMCP
		pl.NavAltitudeFMS = nav.SelectedAltitudeFMS
		pl.NavQNH = nav.QNH
		pl.NavHeading = nav.SelectedHeading
		if nav.Modes != nil {
			pl.NavModes = nav.Modes.Strings()
		}
	}
	if ehs, ok := plane.EHS(); ok {
		pl.IAS = ehs.IAS
		pl.TAS = ehs.TAS
		pl.Mach = ehs.Mach
		pl.Roll = ehs.Roll
		pl.MagHeading = ehs.MagneticHeading
		pl.TrackRate = ehs.TrackRate
		pl.BaroRate = ehs.BaroVerticalRate
		pl.GeomRate = ehs.InertialVerticalRate
	}
	return pl
}

func (pl *PlaneLocation) ToJSONBytes() ([]byte, error) {
	json := jsoniter.ConfigFastest

	pl.sourceTagsMutex.Lock()
	defer pl.sourceTagsMutex.Unlock()

	jsonBuf, err := json.Marshal(pl)
	if nil != err {
		log.Error().Err(err).Msg("could not create json bytes for sending")
		return nil, err
	} else {
		return jsonBuf, nil
	}
}
