package tracker

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"plane.watch/lib/tile_grid"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"plane.watch/lib/dedupe/forgetfulmap"
	"plane.watch/lib/tracker/mode_s"
	"plane.watch/lib/tracker/sbs1"
)

type (
	Tracker struct {
		log   zerolog.Logger
		stats struct {
			currentPlanes prometheus.Gauge
			decodedFrames prometheus.Counter
		}
		startTime           time.Time
		sink                Sink
		planeList           *forgetfulmap.ForgetfulSyncMap
		producers           []Producer
		middlewares         []Middleware
		producerWaiter      sync.WaitGroup
		eventsWaiter        sync.WaitGroup
		decodingQueueWaiter sync.WaitGroup
		middlewareWaiter    sync.WaitGroup
		decodeWorkerCount   int
		pruneAfter          time.Duration
		pruneTick           time.Duration
		finishDone          bool

		muProducers sync.RWMutex
	}

	dummySink struct {
	}
)

func (d dummySink) OnEvent(event Event) {
}

func (d dummySink) Stop() {
}

func (d dummySink) HealthCheckName() string {
	return "Dummy Sink"
}

func (d dummySink) HealthCheck() bool {
	return true
}

// NewTracker creates a new tracker with which we can populate with plane tracking data
func NewTracker(opts ...Option) *Tracker {
	t := &Tracker{
		producers:         []Producer{},
		middlewares:       []Middleware{},
		decodeWorkerCount: 5,
		pruneTick:         10 * time.Second,
		pruneAfter:        5 * time.Minute,
		sink:              dummySink{},
		startTime:         time.Now(),

		log: log.With().Str("Section", "Tracker").Logger(),
	}

	for _, opt := range opts {
		opt(t)
	}

	t.planeList = forgetfulmap.NewForgetfulSyncMap(
		forgetfulmap.WithSweepInterval(t.pruneTick),
		forgetfulmap.WithPreEvictionAction(func(key, value interface{}) {
			if nil != t.stats.currentPlanes {
				t.stats.currentPlanes.Dec()
			}

			if plane, ok := value.(*Plane); ok {
				// now send an event
				t.sink.OnEvent(newPlaneActionEvent(plane, false, true))
			}
			if t.log.Trace().Enabled() {
				t.log.Trace().
					Str("ICAO", fmt.Sprintf("%06X", key)).
					Msg("Plane is being evicted")
			}
		}),
		forgetfulmap.WithForgettableAction(func(key, value any, added time.Time) bool {
			result := true
			if plane, ok := value.(*Plane); ok {
				oldest := time.Now().Add(-t.pruneAfter)
				// remove the plane from the list if it is older than our oldest allowable
				result = plane.LastSeen().Before(oldest)
			}
			if t.log.Trace().Enabled() {
				t.log.Trace().
					Bool("Removing?", result).
					Str("ICAO", fmt.Sprintf("%06X", key)).
					Msg("Should plane be forgotten?")
			}
			// remove anything that is not a *export.PlaneLocation
			return result
		}),
	)

	return t
}

func (t *Tracker) numPlanes() int {
	return int(t.planeList.Len())
}

func (t *Tracker) GetPlane(icao uint32) *Plane {
	plane, ok := t.planeList.Load(icao)
	if ok {
		return plane.(*Plane)
	}
	if t.log.Trace().Enabled() {
		t.log.Trace().
			Str("ICAO", fmt.Sprintf("%06X", icao)).
			Msg("Plane has made an appearance")
	}
	if nil != t.stats.currentPlanes {
		t.stats.currentPlanes.Inc()
	}

	p := newPlane(icao)
	p.tracker = t
	t.planeList.Store(icao, p)
	return p
}

func (t *Tracker) EachPlane(pi PlaneIterator) {
	t.planeList.Range(func(key, value interface{}) bool {
		return pi(value.(*Plane))
	})
}

func (p *Plane) HandleModeSFrame(frame *mode_s.Frame, source *FrameSource) {
	if nil == frame {
		return
	}
	icao := frame.Icao()
	if icao == 0 {
		return
	}
	var planeFormat string
	var hasChanged bool
	var refLat, refLon *float64
	checkVelocity := true

	if source != nil {
		refLat = source.RefLat
		refLon = source.RefLon
		checkVelocity = source.VelocityCheck
	}

	p.setLastSeen(frame.TimeStamp())
	p.incMsgCount()
	isDebug := p.tracker.log.Debug().Enabled()

	if isDebug {
		p.addFrame(frame)
	}

	debugMessage := func(sfmt string, a ...interface{}) {
		if isDebug {
			planeFormat = fmt.Sprintf("DF%02d - \033[0;97mPlane (\033[38;5;118m%s %-8s\033[0;97m)", frame.DownLinkType(), p.IcaoIdentifierStr(), p.FlightNumber())
			p.tracker.log.Debug().Msgf(planeFormat+sfmt, a...)
		}
	}

	hasChanged = p.setRegistration(frame.DecodeAuIcaoRegistration()) || hasChanged

	if log.Trace().Enabled() {
		log.Trace().
			Str("frame", frame.String()).
			Str("icao", frame.IcaoStr()).
			Str("Downlink Type", "DF"+strconv.Itoa(int(frame.DownLinkType()))).
			Int("Downlink Format", int(frame.DownLinkType())).
			Str("DF17 Msg Type", frame.MessageTypeString()).
			Bytes("RAW", frame.Raw()).
			Send()
	}

	// if there is no tile location for this plane and we have a refLat/refLon - let's assume it is in the same tile
	// as the receiver. This will be "fixed" for aircraft sending lat/lon within a few frames if it is different.
	// this means that all the aircraft that do not send locations, will at least have a chance of showing up.
	if p.GridTileLocation() == "" && nil != refLat && nil != refLon {
		p.location.SetTileGrid(tile_grid.LookupTile(*refLat, *refLon))
	}

	// determine what to do with our given frame
	switch frame.DownLinkType() {
	case 0:
		// grab the altitude
		if frame.AltitudeValid() {
			alt, _ := frame.Altitude()
			hasChanged = p.setAltitude(alt, frame.AltitudeUnits(), frame.TimeStamp()) || hasChanged
		}
		if frame.VerticalStatusValid() {
			hasChanged = p.setGroundStatus(frame.MustOnGround(), frame.TimeStamp()) || hasChanged
		}
		debugMessage(" is at %d %s \033[0m", p.Altitude(), p.AltitudeUnits())

	case 1, 2, 3:
		if frame.VerticalStatusValid() {
			hasChanged = p.setGroundStatus(frame.MustOnGround(), frame.TimeStamp()) || hasChanged
		}
		if frame.Alert() {
			hasChanged = p.setSpecial("alert", "Alert", frame.TimeStamp()) || hasChanged
		}
	case 6, 7, 8, 9, 10, 12, 13, 14, 15, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31:
		debugMessage(" \033[38;5;52mIgnoring Mode S Frame: %d (%s)\033[0m\n", frame.DownLinkType(), frame.DownLinkFormat())
	case 11:
		if frame.VerticalStatusValid() {
			hasChanged = p.setGroundStatus(frame.MustOnGround(), frame.TimeStamp()) || hasChanged
		}
	case 4, 5:
		if frame.VerticalStatusValid() {
			hasChanged = p.setGroundStatus(frame.MustOnGround(), frame.TimeStamp()) || hasChanged
		}
		if frame.Alert() {
			hasChanged = p.setSpecial("alert", "Alert", frame.TimeStamp()) || hasChanged
		}
		if frame.AltitudeValid() {
			alt, _ := frame.Altitude()
			hasChanged = p.setAltitude(alt, frame.AltitudeUnits(), frame.TimeStamp()) || hasChanged
		}
		hasChanged = p.setFlightStatus(frame.FlightStatus(), frame.FlightStatusString(), frame.TimeStamp()) || hasChanged

		if frame.DownLinkType() == 5 { // || 21 == frame.DownLinkType()
			hasChanged = p.setSquawkIdentity(frame.SquawkIdentity(), frame.TimeStamp()) || hasChanged
		}

		debugMessage(" is at %d %s and flight status is: %s. \033[2mMode S Frame: %d \033[0m",
			p.Altitude(), p.AltitudeUnits(), p.FlightStatus(), frame.DownLinkType())
	case 16:
		if frame.AltitudeValid() {
			alt, _ := frame.Altitude()
			hasChanged = p.setAltitude(alt, frame.AltitudeUnits(), frame.TimeStamp()) || hasChanged
		}
		if frame.VerticalStatusValid() {
			hasChanged = p.setGroundStatus(frame.MustOnGround(), frame.TimeStamp()) || hasChanged
		}

	case 17, 18, 19: // ADS-B
		// I am using the text version because it is easier to program with.
		// if performance is an issue, change over to byte comparing
		messageType := frame.MessageTypeString()
		switch messageType {
		case mode_s.DF17FrameIdCat: // "Aircraft Identification and Category"
			{
				hasChanged = p.setFlightNumber(frame.FlightNumber()) || hasChanged
				if frame.ValidCategory() {
					hasChanged = p.setAirFrameCategory(frame.Category()) || hasChanged
					hasChanged = p.setAirFrameCategoryType(frame.CategoryType()) || hasChanged
				}
				break
			}
		case mode_s.DF17FrameSurfacePos: // "Surface Position"
			{
				if frame.HeadingValid() {
					hasChanged = p.setHeading(frame.MustHeading(), frame.TimeStamp()) || hasChanged
				}
				if frame.VelocityValid() {
					hasChanged = p.setVelocity(frame.MustVelocity(), time.Now()) || hasChanged
				}
				if !p.OnGround() {
					p.zeroCpr()
				}
				hasChanged = p.setGroundStatus(true, frame.TimeStamp()) || hasChanged

				if frame.IsEven() {
					_ = p.setCprEvenLocation(float64(frame.Latitude()), float64(frame.Longitude()), frame.TimeStamp())
				} else {
					_ = p.setCprOddLocation(float64(frame.Latitude()), float64(frame.Longitude()), frame.TimeStamp())
				}
				if err := p.decodeCprFilledRefLatLon(refLat, refLon, checkVelocity); nil != err {
					debugMessage("%s", err)
				} else {
					hasChanged = true
				}

				debugMessage(" is on the ground and has heading %s and is travelling at %0.2f knots\033[0m", p.HeadingStr(), p.Velocity())
				break
			}
		case mode_s.DF17FrameAirPositionBarometric, mode_s.DF17FrameAirPositionGnss: // "Airborne Position (with Barometric altitude)"
			if p.OnGround() {
				// this is valid since we should only get this type of message when we are off the ground
				p.zeroCpr()
			}
			hasChanged = p.setGroundStatus(false, frame.TimeStamp()) || hasChanged

			if frame.IsEven() {
				_ = p.setCprEvenLocation(float64(frame.Latitude()), float64(frame.Longitude()), frame.TimeStamp())
			} else {
				_ = p.setCprOddLocation(float64(frame.Latitude()), float64(frame.Longitude()), frame.TimeStamp())
			}

			altitude, _ := frame.Altitude()
			hasChanged = p.setAltitude(altitude, frame.AltitudeUnits(), frame.TimeStamp()) || hasChanged
			if err := p.decodeCpr(0, 0, checkVelocity); nil != err {
				debugMessage("%s", err)
			} else {
				hasChanged = true
			}

			if dt := p.DistanceTravelled(); dt.Valid() {
				debugMessage(" travelled %0.2f metres %0.2f seconds", dt.metres, dt.duration)
			}

			if frame.HasSurveillanceStatus() {
				hasChanged = p.setSpecial("surveillance", frame.SurveillanceStatus(), frame.TimeStamp()) || hasChanged
			} else {
				hasChanged = p.setSpecial("surveillance", "", frame.TimeStamp()) || hasChanged
			}

		case mode_s.DF17FrameAirVelocity: // "Airborne velocity"
			hasChanged = p.setGroundStatus(false, frame.TimeStamp()) || hasChanged

			if frame.HeadingValid() {
				hasChanged = p.setHeading(frame.MustHeading(), frame.TimeStamp()) || hasChanged
			}
			if frame.VelocityValid() {
				hasChanged = p.setVelocity(frame.MustVelocity(), frame.TimeStamp()) || hasChanged
			}
			if frame.VerticalRateValid() {
				hasChanged = p.setVerticalRate(frame.MustVerticalRate(), frame.TimeStamp()) || hasChanged
			}

			if p.tracker.log.Debug().Enabled() {
				headingStr := "unknown heading"
				if p.HasHeading() {
					headingStr = fmt.Sprintf("heading %0.2f", p.Heading())
				}
				debugMessage(" has %s and is travelling at %0.2f knots\033[0m", headingStr, p.Velocity())
			}

		case mode_s.DF17FrameTestMessage:
			debugMessage("\033[2m Ignoring: DF%d %s\033[0m", frame.DownLinkType(), messageType)
		case mode_s.DF17FrameTestMessageSquawk:
			{
				if frame.SquawkIdentity() > 0 {
					hasChanged = p.setSquawkIdentity(frame.SquawkIdentity(), frame.TimeStamp()) || hasChanged
				}
			}
		case mode_s.DF17FrameSurfaceSystemStatus:
			hasChanged = p.setGroundStatus(true, frame.TimeStamp()) || hasChanged
			debugMessage("\033[2m Ignoring: DF%d %s\033[0m", frame.DownLinkType(), messageType)

		case mode_s.DF17FrameEmergencyPriority:
			{
				debugMessage("\033[2m %s\033[0m", messageType)
				if frame.Alert() {
					hasChanged = p.setSpecial("special", frame.Special(), frame.TimeStamp()) || hasChanged
					hasChanged = p.setSpecial("emergency", frame.Emergency(), frame.TimeStamp()) || hasChanged
				}
				hasChanged = p.setSquawkIdentity(frame.SquawkIdentity(), frame.TimeStamp()) || hasChanged
			}
		case mode_s.DF17FrameTcasRA:
			{
				debugMessage("\033[2m Ignoring: DF%d %s\033[0m", frame.DownLinkType(), messageType)
			}
		case mode_s.DF17FrameTargetStateStatus:
			{
				debugMessage("\033[2m Ignoring: DF%d %s\033[0m", frame.DownLinkType(), messageType)
			}
		case mode_s.DF17FrameAircraftOperational:
			{
				if frame.VerticalStatusValid() {
					hasChanged = p.setGroundStatus(frame.MustOnGround(), frame.TimeStamp()) || hasChanged
				}
				hasChanged = p.setAirFrameWidthLength(frame.GetAirplaneLengthWidth()) || hasChanged
			}
		}

	case 20, 21:
		switch frame.BdsMessageType() {
		case mode_s.BdsElsDataLinkCap: // 1.0
			hasChanged = p.setSquawkIdentity(frame.SquawkIdentity(), frame.TimeStamp()) || hasChanged
		case mode_s.BdsElsGicbCap: // 1.7
			if frame.AltitudeValid() {
				hasChanged = p.setAltitude(frame.MustAltitude(), frame.AltitudeUnits(), frame.TimeStamp()) || hasChanged
			}
		case mode_s.BdsElsAircraftIdent: // 2.0
			hasChanged = p.setFlightNumber(frame.FlightNumber()) || hasChanged
		default:
			// let's see if we can decode more BDS info
			// TODO: Decode Other BDS frames
		}
	}

	if p.location.HasTileGrid() && nil != refLat && nil != refLon {
		// do not have a grid tile for this plane, let's assume it is in same tile as the receiver
		p.location.SetTileGrid(tile_grid.LookupTile(*refLat, *refLon))
		hasChanged = p.location.TileGrid() != "" || hasChanged
	}

	// we only transmit events if we have a few frames from this plane to prevent junk data
	if hasChanged && p.MsgCount() > 3 {
		p.tracker.sink.OnEvent(NewPlaneLocationEvent(p))
	}
}

func (p *Plane) HandleSbs1Frame(frame *sbs1.Frame) {
	var hasChanged bool
	p.setLastSeen(frame.TimeStamp())
	p.incMsgCount()
	if frame.HasPosition {
		if err := p.addLatLong(frame.Lat, frame.Lon, frame.Received, true); nil != err {
			p.tracker.log.Warn().Err(err).Send()
		}

		hasChanged = true
		p.tracker.log.Debug().Msgf("Plane %s is at %0.4f, %0.4f", frame.IcaoStr(), frame.Lat, frame.Lon)
	}

	if hasChanged {
		p.tracker.sink.OnEvent(NewPlaneLocationEvent(p))
	}
}
