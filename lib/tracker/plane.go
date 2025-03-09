package tracker

import (
	"fmt"
	"math"
	"os"
	"plane.watch/lib/tile_grid"
	"plane.watch/lib/tracker/mode_s"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	max17Bits = 131071
)

type (
	headingInfo []heading
	heading     struct {
		label string
		from  float64
		to    float64
	}
	// PlaneLocation stores where we think a plane is currently at. It is am amalgamation of all the tracking info
	// we receive.
	PlaneLocation struct {
		onGroundTs        time.Time
		cprDecodedTs      time.Time
		headingTs         time.Time
		velocityTs        time.Time
		altitudeTs        time.Time
		verticalRateTs    time.Time
		gridTileLocation  string
		altitudeUnits     string
		heading           float64
		velocity          float64
		verticalRate      int
		distanceTravelled float64
		durationTravelled float64
		longitude         float64
		latitude          float64
		mu                sync.Mutex
		altitude          int32
		onGround          bool
		TrackFinished     bool
		hasLatLon         bool
		hasHeading        bool
		hasVelocity       bool
		hasVerticalRate   bool
	}

	flight struct {
		flightStatusTs time.Time
		identifier     string
		status         string
		statusId       byte
	}

	airframe struct {
		width        *float32
		length       *float32
		registration *string
		category     string
		categoryType string
	}

	Plane struct {
		flight          flight
		airframe        airframe
		specialTs       time.Time
		lastSeen        time.Time
		squawkTs        time.Time
		trackedSince    time.Time
		signalLevel     *float64
		location        *PlaneLocation
		special         map[string]string
		tracker         *Tracker
		icao            string
		recentFrames    lossyFrameList
		locationHistory []*PlaneLocation
		cprLocation     CprLocation
		msgCount        uint64
		rwLock          sync.RWMutex
		squawk          uint32
		icaoIdentifier  uint32
	}

	PlaneIterator func(p *Plane) bool

	DistanceTravelled struct {
		metres   float64
		duration float64
	}
)

var (
	MaxLocationHistory = 1000
	headingLookup      = headingInfo{
		{from: 348.75, to: 360, label: "N"},
		{from: 0, to: 11.25, label: "N"},
		{from: 11.25, to: 33.75, label: "NNE"},
		{from: 33.75, to: 56.25, label: "NE"},
		{from: 56.25, to: 78.75, label: "ENE"},
		{from: 78.75, to: 101.25, label: "E"},
		{from: 101.25, to: 123.75, label: "ESE"},
		{from: 123.75, to: 146.25, label: "SE"},
		{from: 146.25, to: 168.75, label: "SSE"},
		{from: 168.75, to: 191.25, label: "S"},
		{from: 191.25, to: 213.75, label: "SSW"},
		{from: 213.75, to: 236.25, label: "SW"},
		{from: 236.25, to: 258.75, label: "WSW"},
		{from: 258.75, to: 281.25, label: "W"},
		{from: 281.25, to: 303.75, label: "WNW"},
		{from: 303.75, to: 326.25, label: "NW"},
		{from: 326.25, to: 348.75, label: "NNW"},
	}
	colourOutput = haveTty()
)

func newPlane(icao uint32) *Plane {
	p := &Plane{
		location: &PlaneLocation{},
		special:  map[string]string{},
	}
	p.setIcaoIdentifier(icao)
	p.resetLocationHistory()
	p.zeroCpr()
	p.recentFrames = newLossyFrameList(20)
	p.trackedSince = time.Now()
	return p
}

func (p *Plane) addFrame(f *mode_s.Frame) {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	p.recentFrames.Push(f)
}

// TrackedSince tells us when we started tracking this plane (on this run, not historical)
func (p *Plane) TrackedSince() time.Time {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.trackedSince
}

func (p *Plane) LocationUpdatedAt() time.Time {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.location.cprDecodedTs
}
func (p *Plane) AltitudeUpdatedAt() time.Time {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.location.altitudeTs
}
func (p *Plane) VelocityUpdatedAt() time.Time {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.location.velocityTs
}
func (p *Plane) HeadingUpdatedAt() time.Time {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.location.headingTs
}
func (p *Plane) OnGroundUpdatedAt() time.Time {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.location.onGroundTs
}
func (p *Plane) VerticalRateUpdatedAt() time.Time {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.location.verticalRateTs
}
func (p *Plane) FlightStatusUpdatedAt() time.Time {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.flight.flightStatusTs
}
func (p *Plane) SpecialUpdatedAt() time.Time {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.specialTs
}
func (p *Plane) SquawkUpdatedAt() time.Time {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.squawkTs
}

// LastSeen is when we last received a message from this Plane
func (p *Plane) LastSeen() time.Time {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.lastSeen
}

// setLastSeen sets the last seen timestamp
func (p *Plane) setLastSeen(lastSeen time.Time) {
	p.rwLock.Lock()
	defer p.rwLock.Unlock()
	p.lastSeen = lastSeen
}

// MsgCount is the number of messages we have received from this plane while we have been tracking it
func (p *Plane) MsgCount() uint64 {
	return atomic.LoadUint64(&p.msgCount)
}

// incMsgCount increments our message count by 1
func (p *Plane) incMsgCount() {
	atomic.AddUint64(&p.msgCount, 1)
}

// IcaoIdentifier returns the ICAO identifier this plane is using
func (p *Plane) IcaoIdentifier() uint32 {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.icaoIdentifier
}

// IcaoIdentifierStr returns a pretty printed ICAO identifier, fit for human consumption
func (p *Plane) IcaoIdentifierStr() string {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.icao
}

// setIcaoIdentifier sets the tracking identifier for this Plane
func (p *Plane) setIcaoIdentifier(icaoIdentifier uint32) {
	p.rwLock.Lock()
	defer p.rwLock.Unlock()
	p.icaoIdentifier = icaoIdentifier
	p.icao = fmt.Sprintf("%06X", icaoIdentifier)
}

// resetLocationHistory Zeros out the tracking history for this aircraft
func (p *Plane) resetLocationHistory() {
	p.rwLock.Lock()
	defer p.rwLock.Unlock()
	p.locationHistory = make([]*PlaneLocation, 0)
}

// setSpecial allows us to set any special status this plane is transmitting
func (p *Plane) setSpecial(what, status string, ts time.Time) bool {
	p.rwLock.Lock()
	defer p.rwLock.Unlock()
	hasChanged := p.special[what] != status
	p.special[what] = status
	p.specialTs = ts
	return hasChanged
}

// Special returns any special status for this aircraft
func (p *Plane) Special() string {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	var ret string
	for _, v := range p.special {
		ret = ret + v + " "
	}
	return strings.TrimSpace(ret)
}

// setSignalLevel sets the receivers signal level for this frame
func (p *Plane) setSignalLevel(rssi float64) bool {
	if math.IsNaN(rssi) || math.IsInf(rssi, 0) {
		return false
	}

	p.rwLock.Lock()
	defer p.rwLock.Unlock()
	hasChanged := true
	if nil != p.signalLevel {
		hasChanged = *p.signalLevel != rssi
	}
	p.signalLevel = &rssi
	return hasChanged
}

// SignalLevel gives the RSSI value from the last frame processed for this plane
func (p *Plane) SignalLevel() *float64 {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.signalLevel
}

// SignalLevelStr gives the RSSI value from the last frame processed for this plane
func (p *Plane) SignalLevelStr() string {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	if nil == p.signalLevel {
		return ""
	}
	return fmt.Sprintf("%0.1f", *p.signalLevel)
}

func haveTty() bool {
	fi, err := os.Stdout.Stat()
	if nil != err {
		return false
	}

	if fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}

	return true
}

// String gives us a nicely printable ANSI escaped string
func (p *Plane) String() string {
	var id, alt, position, direction, special, strength string

	white := "\033[0;97m"
	lime := "\033[38;5;118m"
	orange := "\033[38;5;226m"
	blue := "\033[38;5;122m"
	red := "\033[38;5;160m"

	if colourOutput {
		c := lime
		v := p.IcaoIdentifierStr()
		if nil != p.Registration() {
			c = orange
			v = *p.Registration()
		}
		id = fmt.Sprintf("%sPlane (%s%s %-8s%s)", white, c, v, p.FlightNumber(), white)
	} else {
		id = fmt.Sprintf("Plane (%s %-8s)", p.IcaoIdentifierStr(), p.FlightNumber())
	}

	if p.OnGround() {
		position += " is on the ground."
	} else if p.Altitude() > 0 {
		if colourOutput {
			alt = fmt.Sprintf(" %s%d%s %s,", orange, p.Altitude(), white, p.AltitudeUnits())
		} else {
			alt = fmt.Sprintf(" %d %s,", p.Altitude(), p.AltitudeUnits())
		}
	}

	if p.HasLocation() {
		if colourOutput {
			position += fmt.Sprintf(" %s%+03.13f%s, %s%+03.13f%s,", blue, p.Lat(), white, blue, p.Lon(), white)
		} else {
			position += fmt.Sprintf(" %+03.13f, %+03.13f,", p.Lat(), p.Lon())
		}
	}

	if p.HasHeading() {
		if colourOutput {
			direction += fmt.Sprintf(" heading %s%0.2f%s, speed %s%0.2f%s knots", orange, p.Heading(), white, orange, p.Velocity(), white)
		} else {
			direction += fmt.Sprintf(" heading %0.2f, speed %0.2f knots", p.Heading(), p.Velocity())
		}
	}

	if p.Special() != "" {
		if colourOutput {
			special = " " + red + p.Special() + white + ", "
		} else {
			special = " " + p.Special() + ", "
		}
	}

	ret := id + alt + position + direction + special + strength
	if colourOutput {
		return ret + "\033[0m"
	}
	return ret
}

// setAltitude puts our plane in the sky
func (p *Plane) setAltitude(altitude int32, altitudeUnits string, ts time.Time) bool {
	p.rwLock.Lock()
	defer p.rwLock.Unlock()
	// set the current altitude
	var hasChanged bool
	if p.location.altitude != altitude {
		p.location.altitude = altitude
		hasChanged = true
	}
	if p.location.altitudeUnits != altitudeUnits {
		hasChanged = true
		p.location.altitudeUnits = altitudeUnits
	}
	p.location.altitudeTs = ts
	return hasChanged
}

// Altitude is the planes altitude in AltitudeUnits units
func (p *Plane) Altitude() int32 {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	// set the current altitude
	return p.location.altitude
}

// HasAltitude is true when we know the planes height
func (p *Plane) HasAltitude() bool {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	// set the current altitude
	return !p.location.altitudeTs.IsZero()
}

// AltitudeUnits how we are measuring altitude (feet / metres)
func (p *Plane) AltitudeUnits() string {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	// set the current altitude
	return p.location.altitudeUnits
}

// setGroundStatus puts our plane on the ground (or not). Use carefully, planes do not like being put on
// the ground suddenly.
func (p *Plane) setGroundStatus(onGround bool, ts time.Time) bool {
	defer func() {
		if onGround {
			p.setVerticalRate(0, ts)
		}
	}()
	p.rwLock.Lock()
	defer p.rwLock.Unlock()
	hasChanged := p.location.onGround != onGround
	p.location.onGround = onGround
	p.location.onGroundTs = ts
	return hasChanged
}

// OnGround tells us where the plane thinks it is (In the sky or on the ground)
func (p *Plane) OnGround() bool {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.location.onGround
}

// HasOnGround is set true if we know for sure if we are on the ground or in the air
func (p *Plane) HasOnGround() bool {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return !p.location.onGroundTs.IsZero()
}

// setFlightStatus sets the flight status of the aircraft, the string is one from mode_s.flightStatusTable
func (p *Plane) setFlightStatus(statusId byte, statusString string, ts time.Time) bool {
	p.rwLock.Lock()
	defer p.rwLock.Unlock()

	hasChanged := p.flight.statusId != statusId || p.flight.status != statusString

	p.flight.statusId = statusId
	p.flight.status = statusString
	p.flight.flightStatusTs = ts
	return hasChanged
}

// FlightStatus gives us the flight status of this aircraft
func (p *Plane) FlightStatus() string {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.flight.status
}

// HasFlightStatus indicates if we have a flight status
func (p *Plane) HasFlightStatus() bool {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return !p.flight.flightStatusTs.IsZero()
}

// FlightNumber is the planes self identifier for the route it is flying. e.g. QF1, SPTR644
func (p *Plane) FlightNumber() string {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.flight.identifier
}

// Registration belongs to the plane. e.g. VH-OWO
func (p *Plane) Registration() *string {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.airframe.registration
}

// setFlightNumber is the flights identifier/number
func (p *Plane) setFlightNumber(flightIdentifier string) bool {
	if flightIdentifier == "" {
		return false
	}
	p.rwLock.Lock()
	defer p.rwLock.Unlock()
	hasChanged := p.flight.identifier != flightIdentifier
	p.flight.identifier = flightIdentifier
	return hasChanged
}

// setRegistration sets our flights call sign
func (p *Plane) setRegistration(reg *string, err error) bool {
	if nil != err {
		return false
	}
	p.rwLock.Lock()
	defer p.rwLock.Unlock()
	hasChanged := p.airframe.registration != reg
	p.airframe.registration = reg
	return hasChanged
}

// setSquawkIdentity Sets the planes squawk. A squawk is set by the pilots for various reasons (including flight control)
func (p *Plane) setSquawkIdentity(ident uint32, ts time.Time) bool {
	p.rwLock.Lock()
	defer p.rwLock.Unlock()
	hasChanged := p.squawk != ident
	p.squawk = ident
	p.squawkTs = ts
	return hasChanged
}

// SquawkIdentity the integer version of the squawk
func (p *Plane) SquawkIdentity() uint32 {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.squawk
}

// SquawkIdentityStr is the string version of SquawkIdentity
func (p *Plane) SquawkIdentityStr() string {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return strconv.FormatUint(uint64(p.squawk), 10)
}

// setAirFrameCategory is the type of airframe for this aircraft
func (p *Plane) setAirFrameCategory(category string) bool {
	p.rwLock.Lock()
	defer p.rwLock.Unlock()
	hasChanged := p.airframe.category != category
	p.airframe.category = category
	return hasChanged
}

func (p *Plane) AirFrame() string {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.airframe.category
}

// setAirFrameCategoryType is the type of airframe for this aircraft
func (p *Plane) setAirFrameCategoryType(categoryType string) bool {
	p.rwLock.Lock()
	defer p.rwLock.Unlock()
	hasChanged := p.airframe.categoryType != categoryType
	p.airframe.categoryType = categoryType
	return hasChanged
}

func (p *Plane) AirFrameType() string {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.airframe.categoryType
}

// setAirFrameWidthLength
func (p *Plane) setAirFrameWidthLength(w, l *float32, err error) bool {
	if nil != err {
		return false
	}
	p.rwLock.Lock()
	defer p.rwLock.Unlock()
	hasChanged := p.airframe.width != w || p.airframe.length != l
	p.airframe.width = w
	p.airframe.length = l
	return hasChanged
}

func (p *Plane) AirFrameWidth() *float32 {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.airframe.width
}

func (p *Plane) AirFrameLength() *float32 {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.airframe.length
}

// setHeading gives our plane some direction in life
func (p *Plane) setHeading(heading float64, ts time.Time) bool {
	p.rwLock.Lock()
	defer p.rwLock.Unlock()
	// set the current altitude
	hasChanged := p.location.heading != heading || !p.location.hasHeading

	p.location.heading = heading
	p.location.hasHeading = true
	p.location.headingTs = ts
	return hasChanged
}

// Heading tells us which way the plane is currently facing
func (p *Plane) Heading() float64 {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	// set the current altitude
	return p.location.heading
}

// HeadingStr gives a nice printable version of the heading, including compass heading
func (p *Plane) HeadingStr() string {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	if !p.location.hasHeading {
		return "?"
	}
	return fmt.Sprintf("%s (%0.2f)", headingLookup.getCompassLabel(p.location.heading), p.location.heading)
}

// HasHeading let's us know if this plane has found it's way in life and knows where it is heading
func (p *Plane) HasHeading() bool {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	// set the current altitude
	return p.location.hasHeading
}

// setVelocity allows us to set the speed the plane is heading
func (p *Plane) setVelocity(velocity float64, ts time.Time) bool {
	p.rwLock.Lock()
	defer p.rwLock.Unlock()
	// set the current altitude
	hasChanged := !p.location.hasVelocity || p.location.velocity != velocity

	p.location.hasVelocity = true
	p.location.velocity = velocity
	p.location.velocityTs = ts
	return hasChanged
}

// Velocity is how fast the plane is going in it's Heading
func (p *Plane) Velocity() float64 {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	// set the current altitude
	return p.location.velocity
}
func (p *Plane) VelocityStr() string {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	// set the current altitude
	if p.location.velocity == 0 {
		return "?"
	}
	return fmt.Sprintf("%0.2f knots", p.location.velocity)
}

// DistanceTravelled Tells us how far we have tracked this plane
func (p *Plane) DistanceTravelled() DistanceTravelled {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return DistanceTravelled{
		metres:   p.location.distanceTravelled,
		duration: p.location.durationTravelled,
	}
}

// setVerticalRate shows us how fast the plane is going up and down and uuupp aaannndd doooowwn
func (p *Plane) setVerticalRate(rate int, ts time.Time) bool {
	p.rwLock.Lock()
	defer p.rwLock.Unlock()
	hasChanged := !p.location.hasVerticalRate || p.location.verticalRate != rate
	p.location.hasVerticalRate = true
	p.location.verticalRate = rate
	p.location.verticalRateTs = ts
	return hasChanged
}

// VerticalRate tells us how fast the plane is going up and down
func (p *Plane) VerticalRate() int {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.location.verticalRate
}

// HasVerticalRate tells us if the plane has reported its vertical rate
func (p *Plane) HasVerticalRate() bool {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.location.hasVerticalRate
}

// HasVelocity tells us if the plane has reported its Velocity
func (p *Plane) HasVelocity() bool {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.location.hasVelocity
}

// HasLocation tells us if we have a latitude/longitude decoded
func (p *Plane) HasLocation() bool {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.location.hasLatLon
}

// Lat tells use the planes last reported latitude
func (p *Plane) Lat() float64 {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.location.latitude
}
func (p *Plane) Lon() float64 {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.location.longitude
}

func (p *Plane) decodeCprFilledRefLatLon(refLat, refLon *float64, velocityCheck bool) error {
	if nil == refLat || nil == refLon {
		// let's see if we can use a past plane location for this decode
		// all we need for our reference lat/lon is a location within 45 nautical miles
		for _, loc := range p.locationHistory {
			// assume our aircraft is travelling < mach 4 and that it will not cover > 45mn in 1 minute
			if nil != loc && loc.hasLatLon && loc.cprDecodedTs.After(time.Now().Add(-time.Minute)) {
				lat := loc.latitude
				refLat = &lat
				lon := loc.longitude
				refLon = &lon
				break
			}
		}
	}
	if nil != refLat && nil != refLon {
		if err := p.decodeCpr(*refLat, *refLon, velocityCheck); nil != err {
			return err
		}
	}
	return nil
}

// addLatLong Adds a Lat/Long pair to our location tracking and sets it as the current plane location
func (p *Plane) addLatLong(lat, lon float64, ts time.Time, velocityCheck bool) (warn error) {
	if lat < -95.0 || lat > 95 || lon < -180 || lon > 180 {
		return fmt.Errorf("cannot add invalid coordinates {%0.6f, %0.6f}", lat, lon)
	}
	p.rwLock.Lock()
	defer p.rwLock.Unlock()

	var travelledDistance float64
	var durationTravelled float64
	numHistoryItems := len(p.locationHistory)
	// determine speed?
	doVelocityCheck := velocityCheck && numHistoryItems > 0 && p.location.latitude != 0 && p.location.longitude != 0
	if doVelocityCheck {
		referenceTime := p.locationHistory[numHistoryItems-1].cprDecodedTs
		if !referenceTime.IsZero() && referenceTime.Before(ts) {
			durationTravelled = float64(ts.Sub(referenceTime)) / float64(time.Second)
			if durationTravelled == 0.0 {
				durationTravelled = 1
			}
			acceptableMaxDistance := (1 + durationTravelled) * 686 // mach2 in metres/second seems fast enough...

			travelledDistance = distance(lat, lon, p.location.latitude, p.location.longitude)

			if travelledDistance > acceptableMaxDistance {
				warn = fmt.Errorf(" the distance (%0.2fm) between {%0.4f,%0.4f} and {%0.4f,%0.4f} is too great for %s to travel in %0.2f seconds. Discarding", travelledDistance, lat, lon, p.location.latitude, p.location.longitude, p.icao, durationTravelled)
				p.location.TrackFinished = true

				// debug log the recently received list of frames
				ourCalculatedVelocityMpS := travelledDistance / (durationTravelled / 1.0)
				reportedVelocityMpS := p.location.velocity / 1.944
				p.tracker.log.Error().
					Str("ICAO", p.icao).
					Float64("Distance", travelledDistance).
					Float64("Duration", durationTravelled).
					Float64("Max Acceptable Distance", acceptableMaxDistance).
					Float64("Reported Velocity m/s", reportedVelocityMpS).
					Float64("Calculated Velocity m/s", ourCalculatedVelocityMpS).
					Floats64("Prev Lat/Lon", []float64{p.location.latitude, p.location.longitude}).
					Floats64("This Lat/Lon", []float64{lat, lon}).
					Msg("A Frame Too Far")

				if p.tracker.log.Debug().Enabled() {
					var lastTS int64
					p.recentFrames.Range(func(f *mode_s.Frame) bool {
						if lastTS == 0 {
							lastTS = f.TimeStamp().UnixNano()
						}
						p.tracker.log.Error().
							Str("ICAO", f.IcaoStr()).
							Time("received", f.TimeStamp()).
							Int64("unix nano", f.TimeStamp().UnixNano()).
							Str("Frame", f.RawString()).
							Int64("Time Diff ms", (lastTS-f.TimeStamp().UnixNano())/1e6).
							Msg("Frames Leading to Broken Track")
						lastTS = f.TimeStamp().UnixNano()
						return true
					})
				}
				return
			}
		}
	}

	if MaxLocationHistory > 0 && numHistoryItems >= MaxLocationHistory {
		p.locationHistory = p.locationHistory[1:]
	}
	p.location.latitude = lat
	p.location.longitude = lon
	p.location.hasLatLon = true
	p.location.cprDecodedTs = ts

	needsLookup := true
	if !p.location.HasTileGrid() {
		if tile_grid.InGridLocation(lat, lon, p.location.TileGrid()) {
			needsLookup = false
		}
	}
	if needsLookup {
		p.location.SetTileGrid(tile_grid.LookupTile(lat, lon))
	}
	p.locationHistory = append(p.locationHistory, p.location.Copy())
	return
}

func (p *Plane) GridTileLocation() string {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.location.gridTileLocation
}

// zeroCpr is called once we have successfully decoded our CPR pair
func (p *Plane) zeroCpr() {
	p.cprLocation.zero(true)
}

// setCprEvenLocation sets our Even CPR location for LAT/LON decoding
func (p *Plane) setCprEvenLocation(lat, lon float64, t time.Time) error {
	return p.cprLocation.SetEvenLocation(lat, lon, t)
}

// setCprOddLocation sets our Even CPR location for LAT/LON decoding
func (p *Plane) setCprOddLocation(lat, lon float64, t time.Time) error {
	return p.cprLocation.SetOddLocation(lat, lon, t)
}

// decodeCpr decodes the CPR Even and Odd frames and gets our Plane position
func (p *Plane) decodeCpr(refLat, refLon float64, velocityCheck bool) error {
	p.cprLocation.refLat = refLat
	p.cprLocation.refLon = refLon
	loc, err := p.cprLocation.decode(p.OnGround())
	if nil != err || loc == nil {
		return err
	}

	return p.addLatLong(loc.latitude, loc.longitude, loc.cprDecodedTs, velocityCheck)
}

// LocationHistory returns the track history of the Plane
func (p *Plane) LocationHistory() []*PlaneLocation {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.locationHistory
}

// Distance function returns the distance (in meters) between two points of
//
//	a given longitude and latitude relatively accurately (using a spherical
//	approximation of the Earth) through the Haversin Distance Formula for
//	great arc distance on a sphere with accuracy for small distances
//
// point coordinates are supplied in degrees and converted into rad. in the func
//
// distance returned is METERS!!!!!!
// http://en.wikipedia.org/wiki/Haversine_formula
func distance(lat1, lon1, lat2, lon2 float64) float64 {
	// convert to radians
	// must cast radius as float to multiply later
	var la1, lo1, la2, lo2, r float64
	la1 = lat1 * math.Pi / 180
	lo1 = lon1 * math.Pi / 180
	la2 = lat2 * math.Pi / 180
	lo2 = lon2 * math.Pi / 180

	r = 6378100 // Earth radius in METERS

	// calculate
	h := hsin(la2-la1) + math.Cos(la1)*math.Cos(la2)*hsin(lo2-lo1)

	return 2 * r * math.Asin(math.Sqrt(h))
}

// Valid let's us know if we have some data
func (dt *DistanceTravelled) Valid() bool {
	return dt.metres > 0 && dt.duration > 0
}

// Metres returns how far we have gone
func (dt *DistanceTravelled) Metres() float64 {
	return dt.metres
}

// Duration is how long we have been going
func (dt *DistanceTravelled) Duration() float64 {
	return dt.duration
}

// getCompassLabel turns a 0-360 degree compass reading into a nice human readable label N/S/E/W etc
func (hi headingInfo) getCompassLabel(heading float64) string {
	for _, h := range hi {
		if heading >= h.from && heading <= h.to {
			return h.label
		}
	}
	return "?"
}

// Copy let's us duplicate a plane location
func (pl *PlaneLocation) Copy() *PlaneLocation {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	return &PlaneLocation{
		latitude:          pl.latitude,
		longitude:         pl.longitude,
		altitude:          pl.altitude,
		hasVerticalRate:   pl.hasVerticalRate,
		verticalRate:      pl.verticalRate,
		altitudeUnits:     pl.altitudeUnits,
		heading:           pl.heading,
		velocity:          pl.velocity,
		cprDecodedTs:      pl.cprDecodedTs,
		altitudeTs:        pl.altitudeTs,
		headingTs:         pl.headingTs,
		velocityTs:        pl.velocityTs,
		onGroundTs:        pl.onGroundTs,
		verticalRateTs:    pl.verticalRateTs,
		onGround:          pl.onGround,
		hasHeading:        pl.hasHeading,
		hasLatLon:         pl.hasLatLon,
		distanceTravelled: pl.distanceTravelled,
		durationTravelled: pl.durationTravelled,
		TrackFinished:     pl.TrackFinished,
	}
}

// Lat returns the Locations current LAT
func (pl *PlaneLocation) Lat() float64 {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	return pl.latitude
}

// Lon returns the Locations current LON
func (pl *PlaneLocation) Lon() float64 {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	return pl.longitude
}

func (pl *PlaneLocation) HasTileGrid() bool {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	return pl.gridTileLocation != ""
}

func (pl *PlaneLocation) SetTileGrid(tile string) {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	pl.gridTileLocation = tile
}

func (pl *PlaneLocation) TileGrid() string {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	return pl.gridTileLocation
}
