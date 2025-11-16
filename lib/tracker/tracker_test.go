package tracker

import (
	"flag"
	"fmt"
	"strconv"
	"testing"
	"time"

	"plane.watch/lib/tracker/beast"

	"github.com/rs/zerolog"
	"plane.watch/lib/tracker/mode_s"
)

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Verbose() {
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	}
	m.Run()
}

func TestNLFunc(t *testing.T) {
	for i, f := range NLTable {
		if r := getNumLongitudeZone(f - 0.01); i != r {
			t.Errorf("NL Table Fail: Expected %0.2f to yield %d, got %d", f, i, r)
		}
	}
}

func TestCprDecode(t *testing.T) {
	type testDataType struct {
		evenRlatCheck1 string
		evenRlonCheck1 string
		evenRlat       string
		evenRlon       string
		oddRlat        string
		oddRlon        string
		evenLat        float64
		evenLon        float64
		oddLat         float64
		oddLon         float64
	}
	testData := []testDataType{
		// odd *8d7c4516581f76e48d95e8ab20ca; even *8d7c4516581f6288f83ade534ae1;
		{evenLat: 83068, evenLon: 15070, oddLat: 94790, oddLon: 103912, oddRlat: "-32.197483", oddRlon: "+116.028629", evenRlat: "-32.197449", evenRlon: "+116.027820"},

		// odd *8d7c4516580f06fc6d8f25d8669d; even *8d7c4516580df2a168340b32212a;
		{evenLat: 86196, evenLon: 13323, oddLat: 97846, oddLon: 102181, oddRlat: "-32.055219", oddRlon: "+115.931602", evenRlat: "-32.054260", evenRlon: "+115.931854"},

		// test data from cprtest.c from mutability dump1090
		{evenLat: 80536, evenLon: 9432, oddLat: 61720, oddLon: 9192, evenRlat: "+51.686646", evenRlon: "+0.700156", oddRlat: "+51.686763", oddRlon: "+0.701294"},
	}
	airDlat0 := "+6.000000"
	airDlat1 := "+6.101695"
	trk := NewTracker()

	for i, d := range testData {
		plane := trk.GetPlane(11234)

		_ = plane.setCprOddLocation(d.oddLat, d.oddLon, time.Now())
		time.Sleep(2 * time.Microsecond)
		_ = plane.setCprEvenLocation(d.evenLat, d.evenLon, time.Now())
		loc, err := plane.cprLocation.decodeGlobalAir()
		if err != nil {
			t.Error(err)
		}

		lat := fmt.Sprintf("%+0.6f", loc.latitude)
		lon := fmt.Sprintf("%+0.6f", loc.longitude)

		if lat != d.oddRlat {
			t.Errorf("Plane latitude is wrong for packet %d: should be %s, was %s", i, d.oddRlat, lat)
		}
		if lon != d.oddRlon {
			t.Errorf("Plane latitude is wrong for packet %d: should be %s, was %s", i, d.oddRlon, lon)
		}

		if airDlat0 != fmt.Sprintf("%+0.6f", plane.cprLocation.airDLat0) {
			t.Error("AirDlat0 is wrong")
		}
		if airDlat1 != fmt.Sprintf("%+0.6f", plane.cprLocation.airDLat1) {
			t.Error("AirDlat1 is wrong")
		}

		_ = plane.setCprEvenLocation(d.evenLat, d.evenLon, time.Now())
		time.Sleep(2 * time.Microsecond)
		_ = plane.setCprOddLocation(d.oddLat, d.oddLon, time.Now())
		loc, err = plane.cprLocation.decodeGlobalAir()
		if err != nil {
			t.Error(err)
		}

		lat = fmt.Sprintf("%+0.6f", loc.latitude)
		lon = fmt.Sprintf("%+0.6f", loc.longitude)

		if lat != d.evenRlat {
			t.Errorf("Plane latitude is wrong for packet %d: should be %s, was %s", i, d.evenRlat, lat)
		}
		if lon != d.evenRlon {
			t.Errorf("Plane latitude is wrong for packet %d: should be %s, was %s", i, d.evenRlon, lon)
		}

		if airDlat0 != fmt.Sprintf("%+0.6f", plane.cprLocation.airDLat0) {
			t.Error("AirDlat0 is wrong")
		}
		if airDlat1 != fmt.Sprintf("%+0.6f", plane.cprLocation.airDLat1) {
			t.Error("AirDlat1 is wrong")
		}

	}
}

func TestTracking(t *testing.T) {
	frames := []string{
		"*8D40621D58C382D690C8AC2863A7;",
		"*8D40621D58C386435CC412692AD6;",
	}
	trk := performTrackingTest(frames, t)

	plane := trk.GetPlane(4219421)
	if alt := plane.Altitude(); alt != 38000 {
		t.Errorf("Plane should be at 38000 feet, was %d", alt)
	}

	lat := "+52.2572021484375"
	lon := "+3.9193725585938"
	if lon != fmt.Sprintf("%+03.13f", plane.Lon()) {
		t.Errorf("longitude Calculation was incorrect: expected %s, got %+0.13f", lon, plane.Lon())
	}
	if lat != fmt.Sprintf("%+03.13f", plane.Lat()) {
		t.Errorf("latitude Calculation was incorrect: expected %s, got %+0.13f", lat, plane.Lat())
	}
}

func TestTracking2(t *testing.T) {
	zerolog.SetGlobalLevel(zerolog.PanicLevel)
	mode_s.DisableICAOChecking()
	frames := []string{
		"*8D7C7DAA99146D0980080D6131A1;",
		"*5D7C7DAACD3CE9;",
		"*0005050870B303;",
		"*8D7C7DAA99146C0980040D2A616F;",
		"*8D7C7DAAF80020060049B06CA244;",
		"*8D7C7DAA582886FA618B21ADB377;",
		"*5D7C7DAACD3CE9;",
		"*8D7C7DAA5828829F322FE81F6DD1;",
		"*8D7C7DAA99146C0980040D2A616F;",
		"*8D7C7DAA99146C0980040D2A616F;",
		"*8D7C7DAA99146C0960080D47BBB9;",
		"*8D7C7DAA582886FA778B115D2F89;",
		"*000005084A3646;",
		"*000005084A3646;",
		"*28000A00307264;",
		"*8D7C7DAA99146A09280C0D91E947;",
		"*8D7C7DAA9914690920080DC2621D;",
		"*8D7C7DAA9914690928040DE49A15;",
		"*8D7C7DAA210DA1E0820820472D63;",
		"*5D7C7DAACD3CE9;",
		"*8D7C7DAA582886FB218A9AFB0420;",
		"*5D7C7DAACD3CE9;",
		"*8D7C7DAA5828829FF42F5E556B2D;",
		"*8D7C7DAA9914680920080DC168D3;",
		"*000005084A3646;",
		"*5D7C7DAACD3CE9;",
		"*8D7C7DAA582886FB318A8FD96CD7;",
		"*8D7C7DAA9914670900080D9576E0;",
		"*000005084A3646;",
	}
	performTrackingTest(frames, t).Finish()

}

func performTrackingTest(frames []string, t *testing.T) *Tracker {
	trk := NewTracker()
	for _, msg := range frames {
		frame, err := mode_s.DecodeString(msg, time.Now())
		if nil != err {
			t.Errorf("failed to decode (%s): %s", msg, err)
		}
		trk.GetPlane(frame.Icao()).HandleModeSFrame(frame, nil)
	}
	return trk
}

// Makes sure that we get a location update only when we need one
// The logic we want:
//
//	Only add something to history if was previously valid and it has now changed
//	example:
//	 first frame has Alt, no history
//	 second frame has half a location, no history
//	 third frame has other half location, no history (alt and location are now valid)
//	 forth frame has same alt, no history
//	 fifth frame has new alt, 1 history with old alt and location)
//	 six frame has heading, 1 history
//
// Things that change that give us a history
//
//	Lat, Long, Alt, GroundStatus, Heading
func TestTrackingLocationHistory(t *testing.T) {
	tests := []struct {
		name         string
		frame        string
		numLocations int
	}{
		// ground position does not trigger location history, only lat/lon does
		{name: "DF17/MT31/ST00 Airborne Status Frame", frame: "8D7C4A0CF80300030049B8BA7984", numLocations: 0},
		{name: "DF17/MT31/ST00 Airborne Status Frame", frame: "8D7C4A0CF80300030049B8BA7984", numLocations: 0},

		{name: "DF17/MT31/ST01 Ground Status Frame", frame: "8C7C4A0CF9004103834938E42BD4", numLocations: 0},
		{name: "DF17/MT31/ST01 Ground Status Frame", frame: "8C7C4A0CF9004103834938E42BD4", numLocations: 0},

		{name: "DF17/MT11/Odd", frame: "8D7C75285841B71C2FB174E7746B", numLocations: 0},
		{name: "DF17/MT11/Even", frame: "8D7C75285841C2C178571CF5234E", numLocations: 1},
	}
	trk := NewTracker()
	// our second test should have our plane in the air, so we can put it on the ground
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame, err := mode_s.DecodeString(tt.frame, time.Now())
			if nil != err {
				t.Error(err)
				return
			}
			if nil == frame {
				t.Errorf("nil frame from avr frame %s", tt.frame)
				return
			}
			plane := trk.GetPlane(frame.Icao())
			plane.HandleModeSFrame(frame, nil)
			numHistory := len(plane.locationHistory)
			if tt.numLocations != numHistory {
				t.Errorf("Expected plane to have %d history items, actually has %d", tt.numLocations, numHistory)
			}
		})
	}
	p := trk.GetPlane(0x7C7528)
	if nil == p {
		t.Errorf("Failed to get our plane")
	}
	if !p.HasLocation() {
		t.Errorf("Did not set location correctly")
	}
}

func TestPlane_HasLocation(t *testing.T) {
	trk := NewTracker()
	p := trk.GetPlane(0x010101)
	err := p.addLatLong(0.01, 0.02, time.Now(), true)
	if nil != err {
		t.Errorf("Got error when adding lat/lon: %s", err)
	}
	if !p.HasLocation() {
		t.Error("Did not correctly set plane location has updated flag")
	}
	if len(p.locationHistory) != 1 {
		t.Errorf("Expected plane history to have 1 item. have %d", len(p.locationHistory))
	}
}

func TestPlane_HasHeading(t *testing.T) {
	trk := NewTracker()
	p := trk.GetPlane(0x010101)
	if p.HasLocation() {
		t.Error("Did not expect to have a heading")
	}

	changed := p.setHeading(99, time.Now())
	if !changed {
		t.Error("Expected that setting our heading got a change")
	}

	if !p.HasHeading() {
		t.Error("Did not correctly set has heading")
	}
}

func TestPlane_HasVerticalRate(t *testing.T) {
	trk := NewTracker()
	p := trk.GetPlane(0x010101)
	if p.HasVerticalRate() {
		t.Error("Did not expect to have a vertical rate")
	}

	changed := p.setVerticalRate(99, time.Now())
	if !changed {
		t.Error("Expected that setting our vertical rate got a change")
	}

	if !p.HasVerticalRate() {
		t.Error("Did not correctly set has vertical rate")
	}
}

func TestPlane_HasVelocity(t *testing.T) {
	trk := NewTracker()
	p := trk.GetPlane(0x010101)
	if p.HasVelocity() {
		t.Error("Did not expect to have a velocity")
	}

	changed := p.setVelocity(99, time.Now())
	if !changed {
		t.Error("Expected that setting our velocity got a change")
	}

	if !p.HasVelocity() {
		t.Error("Did not correctly set velocity")
	}
}

func TestCorrectCprDecodeSouthAmerica(t *testing.T) {
	type pair struct {
		odd, even string
		lat, lon  string
		icao      uint32
	}

	icao, _ := strconv.ParseUint("E065D3", 16, 32)

	samples := []pair{
		{
			odd:  "8DE065D358C38797B4F57E1A56F2",
			even: "8DE065D358C3833F06A8657B6B41",
			lat:  "-31.12995",
			lon:  "-54.14777",
			icao: uint32(icao),
		},
	}
	trk := NewTracker()
	for _, sample := range samples {
		t.Run(fmt.Sprintf("decode_%s_%s", sample.odd, sample.even), func(tt *testing.T) {
			oddFrame, err := mode_s.DecodeString(sample.odd, time.Now())
			if nil != err || oddFrame == nil {
				tt.Error(err)
				return
			}
			if oddFrame.IsEven() {
				tt.Error("Odd Frame was Even")
			}
			evenFrame, err := mode_s.DecodeString(sample.even, time.Now())
			if nil != err {
				tt.Error(err)
			}
			if !evenFrame.IsEven() {
				tt.Error("Even frame was Odd")
			}
			p := trk.GetPlane(sample.icao)
			if err = p.setCprEvenLocation(float64(evenFrame.Latitude()), float64(evenFrame.Longitude()), evenFrame.TimeStamp()); nil != err {
				tt.Error(err)
			}
			if err = p.setCprOddLocation(float64(oddFrame.Latitude()), float64(oddFrame.Longitude()), oddFrame.TimeStamp()); nil != err {
				tt.Error(err)
			}

			loc, err := p.cprLocation.decodeGlobalAir()
			if nil != err {
				tt.Error(err)
			}

			if loc.onGround {
				t.Error("Decoding air position resulted in aircraft being on ground")
			}

			if fmt.Sprintf("%0.5f", loc.latitude) != sample.lat {
				t.Errorf("Got incorrect Latitude: expecting: %s != got: %0.5f", sample.lat, loc.latitude)
			}
			if fmt.Sprintf("%0.5f", loc.longitude) != sample.lon {
				t.Errorf("Got incorrect Longitude: expecting: %s != got: %0.5f", sample.lon, loc.longitude)
			}
		})
	}
}

func TestPlaneListGetsEvicted(t *testing.T) {
	const planeid = 1234
	const squawk = 9999

	// Ensure that Tracker sets up the ForgetfulSyncMap correctly to evict planes from its cache
	tkr := NewTracker(WithPruneTiming(time.Millisecond, time.Millisecond))
	p := tkr.GetPlane(planeid)

	p.squawk = squawk

	time.Sleep(100 * time.Millisecond)

	p = tkr.GetPlane(planeid)
	if p.squawk == squawk {
		t.Errorf("Tracker's forgetfulmap did not correctly evict plane")
	}
}

// TestFarApartLocationUpdatesFail is testing how we decode a planes location with only a few frames far apart in time
// we should not get a location
func TestFarApartLocationUpdatesFail(t *testing.T) {
	md := func(f *mode_s.Frame, err error) *mode_s.Frame {
		if nil != err {
			panic(err)
		}
		return f
	}
	// more than 10s apart
	frames := []*mode_s.Frame{
		md(mode_s.DecodeString("8D4CC54C58D3012E5A42EC86E201", time.Unix(1654054750, 540447277))),
		md(mode_s.DecodeString("8D4CC54C58D304E49BF688F07265", time.Unix(1654054764, 563149779))),
		md(mode_s.DecodeString("8D4CC54C58D3012D1E44DD9DB4C3", time.Unix(1654054761, 392075155))),
		md(mode_s.DecodeString("8D4CC54C58D304DFD3FE0680A0AE", time.Unix(1654054797, 461184199))),
	}

	// make sure our frame timestamps are correct
	expectedUnixNano := []int64{
		1654054750540447277,
		1654054764563149779,
		1654054761392075155,
		1654054797461184199,
	}
	for i := 0; i < 4; i++ {
		if expectedUnixNano[i] != frames[i].TimeStamp().UnixNano() {
			t.Errorf("Incorrect unix timestamp for frame %d. Expected %d != %d", i, expectedUnixNano[i], frames[i].TimeStamp().UnixNano())
		}
	}
	source := &FrameSource{VelocityCheck: true}
	tkr := NewTracker()
	p := tkr.GetPlane(0x4CC54C)

	for i := 0; i < 4; i++ {
		p.HandleModeSFrame(frames[i], source)

		if p.location.hasLatLon {
			t.Error("Should not have decoded lat/lon")
		}
	}
}

func TestBadLocationUpdateRejected(t *testing.T) {
	md := func(f *mode_s.Frame, err error) *mode_s.Frame {
		if nil != err {
			panic(err)
		}
		return f
	}
	frames := []*mode_s.Frame{
		// good decode
		md(mode_s.DecodeString("8D4CA813589186EF638487A3F9F7", time.Unix(1654071089, 590443635))),
		md(mode_s.DecodeString("8D4CA813589183871D80EEE6F328", time.Unix(1654071089, 993928591))),
		// busted lat/lon
		md(mode_s.DecodeString("8D4CA813589186EFA98497B6EF5A", time.Unix(1654071090, 498070277))),
		md(mode_s.DecodeString("8D4CA813589183F7CCA0F55734EA", time.Unix(1654071090, 997511392))),
	}

	tkr := NewTracker()
	p := tkr.GetPlane(0x4CA813)

	for i := 0; i < 4; i++ {
		p.HandleModeSFrame(frames[i], nil)
	}
	if !p.location.hasLatLon {
		t.Error("Should have decoded lat/lon")
	}

	// make sure we did not accept the bad location of
	//  "Lat": 89.90261271848516,
	//  "Lon": -86.77276611328125,

	if p.location.latitude != 53.290813898636124 {
		t.Error("Wrong Latitude")
	}

	if p.location.longitude != -2.553432688993553 {
		t.Error("Wrong Longitude")
	}

	if len(p.locationHistory) != 1 {
		t.Errorf("Incorrect history, expected: 1, got: %d", len(p.locationHistory))
	}
}

type testProducer struct {
	e      chan FrameEvent
	source *FrameSource
	frames []beast.Frame
	idx    int
}

func (tp *testProducer) Source() *FrameSource {
	return tp.source
}

func newTestProducer() *testProducer {
	messages := map[string][]byte{
		"DF00_MT00_ST00": {0x1A, 0x32, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xE1, 0x98, 0x38, 0x5F, 0x1A, 0x9D},
		"DF04_MT00_ST00": {0x1A, 0x32, 0x80, 0x61, 0xEA, 0xEA, 0x5D, 0xB0, 0x14, 0x20, 0x00, 0x17, 0x30, 0xE3, 0x07, 0x9D},
		"DF05_MT00_ST00": {0x1A, 0x32, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x28, 0x00, 0x09, 0xA3, 0xE0, 0x29, 0x52},
		"DF11_MT00_ST00": {0x1A, 0x32, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x5D, 0x48, 0xC2, 0x34, 0x18, 0x27, 0x15},
		"DF16_MT00_ST00": {0x1A, 0x33, 0x08, 0x39, 0xD4, 0x35, 0x7A, 0x17, 0x63, 0x80, 0xE1, 0x99, 0x98, 0x60, 0xCD, 0x81, 0x03, 0x4E, 0x5E, 0xAC, 0x22, 0x14, 0x15},
		"DF17_MT00_ST00": {0x1A, 0x33, 0x11, 0x92, 0x20, 0x74, 0x7B, 0xCD, 0x35, 0x8F, 0x4B, 0xAA, 0x74, 0x00, 0x53, 0x20, 0x00, 0x00, 0x00, 0x00, 0x72, 0x10, 0x75},
		"DF17_MT02_ST00": {0x1A, 0x33, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x8C, 0x49, 0xF0, 0x88, 0x12, 0xCB, 0x2C, 0xF7, 0x18, 0x61, 0x86, 0x01, 0xFD, 0x07},
		"DF17_MT03_ST00": {0x1A, 0x33, 0x14, 0x93, 0xFF, 0x2D, 0xD3, 0xC7, 0x62, 0x8D, 0x48, 0xFC, 0x83, 0x1C, 0x4D, 0x04, 0xCD, 0x14, 0x48, 0x20, 0x72, 0x37, 0xC3},
		"DF17_MT04_ST00": {0x1A, 0x33, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x8D, 0x4B, 0x8D, 0xEE, 0x23, 0x0C, 0x12, 0x78, 0xC3, 0x4C, 0x20, 0x40, 0x2C, 0xA1},
		"DF17_MT07_ST00": {0x1A, 0x33, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x8C, 0x40, 0x62, 0x50, 0x38, 0x1F, 0x57, 0x66, 0x9D, 0xBA, 0xF8, 0x7C, 0xB4, 0xB2},
		"DF17_MT11_ST00": {0x1A, 0x33, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x8D, 0x47, 0x1F, 0x89, 0x58, 0xC3, 0x81, 0x90, 0xF9, 0x47, 0x04, 0xB6, 0x51, 0xAA},
		"DF17_MT12_ST00": {0x1A, 0x33, 0x02, 0xC9, 0x2F, 0x30, 0x87, 0xD4, 0x20, 0x8D, 0x3C, 0x49, 0xEE, 0x60, 0xB5, 0x11, 0x10, 0x2C, 0xA2, 0x0E, 0x48, 0x92, 0x85},
		"DF17_MT13_ST00": {0x1A, 0x33, 0x21, 0x91, 0xCE, 0x58, 0xB3, 0x9E, 0x33, 0x8D, 0x48, 0xFC, 0x83, 0x68, 0x53, 0x41, 0x3C, 0xA6, 0x5D, 0x76, 0x0A, 0xE9, 0xCD},
		"DF17_MT18_ST00": {0x1A, 0x33, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x8D, 0x15, 0x1E, 0x73, 0x90, 0xBF, 0x04, 0x6E, 0xCA, 0xA0, 0xB4, 0xCF, 0x29, 0xD4},
		"DF17_MT19_ST01": {0x1A, 0x33, 0x80, 0x61, 0xEA, 0xEA, 0xE7, 0xA0, 0x09, 0x8D, 0x48, 0x58, 0x76, 0x99, 0x11, 0xC8, 0x84, 0xD0, 0xA8, 0x81, 0x32, 0x41, 0xFA},
		"DF17_MT19_ST03": {0x1A, 0x33, 0x11, 0x92, 0x1A, 0xCC, 0x70, 0xE3, 0x2C, 0x8F, 0x74, 0x80, 0x26, 0x9B, 0x04, 0xEC, 0x20, 0x98, 0x0C, 0x00, 0x68, 0x49, 0xB1},
		"DF17_MT23_ST07": {0x1A, 0x33, 0x29, 0x46, 0x08, 0x8E, 0x03, 0xF2, 0x2C, 0x8D, 0x7C, 0x7A, 0xF8, 0xBF, 0x40, 0x40, 0x00, 0x00, 0x00, 0x00, 0xDD, 0x9B, 0x89},
		"DF17_MT28_ST01": {0x1A, 0x33, 0x1A, 0x1D, 0xBC, 0x48, 0x44, 0x7F, 0x18, 0x8D, 0x06, 0xA1, 0x46, 0xE1, 0x1E, 0x18, 0x00, 0x00, 0x00, 0x00, 0xA6, 0xB3, 0xC4},
		"DF17_MT29_ST02": {0x1A, 0x33, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x8F, 0x4B, 0xA8, 0x90, 0xEA, 0x4C, 0x48, 0x64, 0x01, 0x1C, 0x08, 0x33, 0x67, 0xFE},
		"DF17_MT31_ST00": {0x1A, 0x33, 0x11, 0x92, 0x19, 0xF6, 0x33, 0xEA, 0x60, 0x8D, 0x68, 0x32, 0x73, 0xF8, 0x21, 0x00, 0x02, 0x00, 0x49, 0xB8, 0xF0, 0xA2, 0xAE},
		"DF17_MT31_ST01": {0x1A, 0x33, 0x0B, 0xB9, 0xB4, 0x5B, 0xC7, 0xAE, 0x28, 0x8C, 0x40, 0x62, 0x50, 0xF9, 0x00, 0x26, 0x03, 0x83, 0x49, 0x38, 0xF6, 0xB2, 0x79},
		"DF18_MT00_ST00": {0x1A, 0x33, 0x00, 0xD0, 0x11, 0xB0, 0xCA, 0x83, 0xD0, 0x91, 0x20, 0x10, 0x2A, 0xC1, 0x05, 0x0D, 0x37, 0xBD, 0x83, 0xF0, 0x5E, 0x9E, 0x53},
		"DF18_MT02_ST00": {0x1A, 0x33, 0x01, 0x96, 0xAA, 0xD1, 0x09, 0xDF, 0xB4, 0x90, 0xC1, 0xE1, 0xA7, 0x13, 0x65, 0x64, 0x94, 0x63, 0x38, 0x20, 0x5C, 0xEC, 0xCC},
		"DF18_MT05_ST00": {0x1A, 0x33, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x90, 0x49, 0xF0, 0xE2, 0x28, 0x00, 0x01, 0x9E, 0x76, 0x0B, 0xF4, 0xE2, 0x0F, 0x1D},
		"DF18_MT06_ST00": {0x1A, 0x33, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x90, 0x49, 0xF0, 0x85, 0x30, 0x00, 0x01, 0x99, 0x8A, 0x09, 0xCF, 0x43, 0x56, 0x31},
		"DF18_MT07_ST00": {0x1A, 0x33, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x90, 0xC1, 0xE5, 0xA6, 0x39, 0x9E, 0x45, 0x03, 0x2D, 0xFB, 0x75, 0x5F, 0x23, 0x04},
		"DF18_MT08_ST00": {0x1A, 0x33, 0x00, 0xD0, 0x13, 0xAA, 0xB9, 0x9E, 0x35, 0x90, 0x11, 0x2C, 0xCC, 0x40, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xE3, 0xB0, 0xB8},
		"DF18_MT24_ST01": {0x1A, 0x33, 0x00, 0xD0, 0x11, 0xAC, 0xDE, 0xDF, 0x36, 0x90, 0x11, 0x2C, 0xCC, 0xC1, 0xAB, 0x01, 0xE0, 0x19, 0xEB, 0x71, 0x64, 0x7B, 0xD5},
		"DF18_MT31_ST01": {0x1A, 0x33, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x90, 0xC1, 0xE5, 0xE1, 0xF9, 0x02, 0x00, 0x00, 0x00, 0x3B, 0x20, 0xD6, 0x57, 0xC1},
		"DF20_MT00_ST00": {0x1A, 0x33, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xA0, 0x00, 0x17, 0xB1, 0xB1, 0x29, 0xFB, 0x30, 0xE0, 0x04, 0x00, 0x2D, 0x88, 0xFB},
		"DF21_MT00_ST00": {0x1A, 0x33, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xA8, 0x00, 0x08, 0x00, 0x99, 0x6C, 0x09, 0xF0, 0xA8, 0x00, 0x00, 0xC8, 0xCE, 0x43},
		"DF24_MT00_ST00": {0x1A, 0x33, 0x04, 0x92, 0xE3, 0x82, 0x04, 0x84, 0x1E, 0xC5, 0x53, 0x2D, 0x86, 0x50, 0xF3, 0x51, 0x5B, 0x29, 0xBE, 0x13, 0x0D, 0xBA, 0xAD},
	}
	tp := testProducer{
		frames: make([]beast.Frame, 0, len(messages)),
		idx:    0,
		e:      make(chan FrameEvent),
		source: &FrameSource{
			OriginIdentifier: "test",
			Name:             "test",
			Tag:              "test",
			RefLat:           nil,
			RefLon:           nil,
		},
	}
	for k := range messages {
		frame, _ := beast.NewFrame(messages[k], false)
		tp.frames = append(tp.frames, *frame)
	}
	return &tp
}

func (tp *testProducer) Stop() {
	close(tp.e)
}

func (tp *testProducer) Listen() chan FrameEvent {
	return tp.e
}

func (tp *testProducer) String() string {
	return "Test Producer"
}

func (tp *testProducer) HealthCheckName() string {
	return "Test Producer"
}

func (tp *testProducer) HealthCheck() bool {
	return true
}

func (tp *testProducer) addMsg() {
	if tp.idx >= len(tp.frames) {
		tp.idx = 0
	}
	tp.e <- FrameEvent{
		frame:  &tp.frames[tp.idx],
		source: tp.source,
	}
	tp.idx++
}

func BenchmarkTracker_AddFrame(b *testing.B) {
	b.StopTimer()
	tracker := NewTracker(WithDecodeWorkerCount(1))
	tp := newTestProducer()
	tracker.AddProducer(tp)

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		tp.addMsg()
	}
}
