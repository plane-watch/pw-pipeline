package tracker

import (
	"fmt"
	"plane.watch/lib/tracker/mode_s"
	"testing"
	"time"
)

func TestCprDecodeSurfacePosition(t *testing.T) {

	type (
		locationTestTable struct {
			refLat, refLon         float64
			evenCprLat, evenCprLon float64
			oddCprLat, oddCprLon   float64

			evenErrCount       int
			evenRLat, evenRLon float64
			oddErrCount        int
			oddRLat, oddRLon   float64
		}
	)

	var testData = []locationTestTable{
		{52.00, -180.00, 105730, 9259, 29693, 8997, 0, 52.209984, 0.176601 - 180.0, 0, 52.209976, 0.176507 - 180.0},
		{52.00, -140.00, 105730, 9259, 29693, 8997, 0, 52.209984, 0.176601 - 180.0, 0, 52.209976, 0.176507 - 180.0},
		{52.00, -130.00, 105730, 9259, 29693, 8997, 0, 52.209984, 0.176601 - 90.0, 0, 52.209976, 0.176507 - 90.0},
		{52.00, -50.00, 105730, 9259, 29693, 8997, 0, 52.209984, 0.176601 - 90.0, 0, 52.209976, 0.176507 - 90.0},
		{52.00, -40.00, 105730, 9259, 29693, 8997, 0, 52.209984, 0.176601, 0, 52.209976, 0.176507},
		{52.00, -10.00, 105730, 9259, 29693, 8997, 0, 52.209984, 0.176601, 0, 52.209976, 0.176507},
		{52.00, 0.00, 105730, 9259, 29693, 8997, 0, 52.209984, 0.176601, 0, 52.209976, 0.176507},
		{52.00, 10.00, 105730, 9259, 29693, 8997, 0, 52.209984, 0.176601, 0, 52.209976, 0.176507},
		{52.00, 40.00, 105730, 9259, 29693, 8997, 0, 52.209984, 0.176601, 0, 52.209976, 0.176507},
		{52.00, 50.00, 105730, 9259, 29693, 8997, 0, 52.209984, 0.176601 + 90.0, 0, 52.209976, 0.176507 + 90.0},
		{52.00, 130.00, 105730, 9259, 29693, 8997, 0, 52.209984, 0.176601 + 90.0, 0, 52.209976, 0.176507 + 90.0},
		{52.00, 140.00, 105730, 9259, 29693, 8997, 0, 52.209984, 0.176601 - 180.0, 0, 52.209976, 0.176507 - 180.0},
		{52.00, 180.00, 105730, 9259, 29693, 8997, 0, 52.209984, 0.176601 - 180.0, 0, 52.209976, 0.176507 - 180.0},

		// latitude quadrants (but only 2). The decoded longitude also changes because the cell size changes with latitude
		{90.00, 0.00, 105730, 9259, 29693, 8997, 0, 52.209984, 0.176601, 0, 52.209976, 0.176507},
		{52.00, 0.00, 105730, 9259, 29693, 8997, 0, 52.209984, 0.176601, 0, 52.209976, 0.176507},
		{8.00, 0.00, 105730, 9259, 29693, 8997, 0, 52.209984, 0.176601, 0, 52.209976, 0.176507},
		{7.00, 0.00, 105730, 9259, 29693, 8997, 0, 52.209984 - 90.0, 0.135269, 0, 52.209976 - 90.0, 0.134299},
		{-52.00, 0.00, 105730, 9259, 29693, 8997, 0, 52.209984 - 90.0, 0.135269, 0, 52.209976 - 90.0, 0.134299},
		{-90.00, 0.00, 105730, 9259, 29693, 8997, 0, 52.209984 - 90.0, 0.135269, 0, 52.209976 - 90.0, 0.134299},

		// poles/equator cases
		{-46.00, -180.00, 0, 0, 0, 0, 0, -90.0, -180.000000, 0, -90.0, -180.0}, // south pole
		{-44.00, -180.00, 0, 0, 0, 0, 0, 0.0, -180.000000, 0, 0.0, -180.0},     // equator
		{44.00, -180.00, 0, 0, 0, 0, 0, 0.0, -180.000000, 0, 0.0, -180.0},      // equator
		{46.00, -180.00, 0, 0, 0, 0, 0, 90.0, -180.000000, 0, 90.0, -180.0},    // north pole
	}

	var loc *PlaneLocation
	var err error
	var expectedLat, expectedLon, actualLat, actualLon string

	for i, test := range testData {
		t.Run("Test Surface CPR", func(t *testing.T) {
			cpr := CprLocation{}
			cpr.refLat = test.refLat
			cpr.refLon = test.refLon
			evenTime := time.Now().Add(-time.Second)
			oddTime := evenTime.Add(time.Second)
			if oddTime.Before(evenTime) {
				t.Error("something wrong with time")
				return
			}
			if err = cpr.SetEvenLocation(test.evenCprLat, test.evenCprLon, evenTime); nil != err {
				t.Error(err)
			}
			if err = cpr.SetOddLocation(test.oddCprLat, test.oddCprLon, oddTime); nil != err {
				t.Error(err)
			}
			loc, err = cpr.decode(true)
			if nil == loc {
				t.Error("Failed to decode our location")
				return
			}
			if nil != err && test.evenErrCount == 0 {
				t.Error(err.Error())
			}
			if !cpr.evenDecode {
				t.Error("Expected Even Decode")
			}

			expectedLat = fmt.Sprintf("%0.6f", test.evenRLat)
			expectedLon = fmt.Sprintf("%0.6f", test.evenRLon)
			actualLat = fmt.Sprintf("%0.6f", loc.latitude)
			actualLon = fmt.Sprintf("%0.6f", loc.longitude)
			if expectedLat != actualLat {
				t.Errorf("Even latitude Expected %s, got %s for test %d", expectedLat, actualLat, i)
			}
			if expectedLon != actualLon {
				t.Errorf("Even longitude Expected %s, got %s for test %d", expectedLon, actualLon, i)
			}
			cpr.zero(false)

			// and now test the reverse
			evenTime = time.Now().Add(time.Second)
			oddTime = evenTime.Add(-time.Second)
			if evenTime.Before(oddTime) {
				t.Error("something wrong with time")
				return
			}
			if err = cpr.SetOddLocation(test.oddCprLat, test.oddCprLon, oddTime); nil != err {
				t.Error(err)
			}
			if err = cpr.SetEvenLocation(test.evenCprLat, test.evenCprLon, evenTime); nil != err {
				t.Error(err)
			}
			loc, err = cpr.decodeSurface(test.refLat, test.refLon)
			if nil == loc {
				t.Error("Failed to decode our location")
				return
			}
			if nil != err && test.oddErrCount == 0 {
				t.Error(err.Error())
			}
			if !cpr.oddDecode {
				t.Error("Expected Odd Decode")
			}

			expectedLat = fmt.Sprintf("%0.6f", test.oddRLat)
			expectedLon = fmt.Sprintf("%0.6f", test.oddRLon)
			actualLat = fmt.Sprintf("%0.6f", loc.latitude)
			actualLon = fmt.Sprintf("%0.6f", loc.longitude)

			if expectedLat != actualLat {
				t.Errorf("Odd latitude Expected %s, got %s for test %d", expectedLat, actualLat, i)
			}
			if expectedLon != actualLon {
				t.Errorf("Odd longitude Expected %s, got %s for test %d", expectedLon, actualLon, i)
			}
		})
	}
}

func decodeSurfaceFrame(t *testing.T, avr string) *mode_s.Frame {
	frame := mode_s.NewFrame(avr, time.Now())
	if nil == frame {
		t.Errorf("Failed to decode frame %s", avr)
	}
	err := frame.Decode()
	if nil != err {
		t.Errorf("decoding of frame %s failed, %s", avr, err)
	}
	if onGround, err := frame.OnGround(); nil != err || !onGround {
		t.Errorf("Plane should be on ground. onground: %t, err:%s", onGround, err)
	}

	return frame
}

func TestCprDecodeFailsSurfaceInAir(t *testing.T) {
	cpr := CprLocation{
		oddFrame:  true,
		evenFrame: true,
	}

	_, err := cpr.decode(true)
	if nil == err {
		t.Error("Expected a surface decode to fail with no reflat/reflon")
	}
}

func TestCprDecodeExample(t *testing.T) {
	// example taken from https://mode-s.org/decode/content/ads-b/4-surface-position.html
	avr := []string{
		"*8C4841753AAB238733C8CD4020B1;",
		"*8C4841753A8A35323FAEBDAC702D;",
	}
	refLat := 51.990
	refLon := 4.375

	frame0 := decodeSurfaceFrame(t, avr[0])
	frame1 := decodeSurfaceFrame(t, avr[1])
	cpr := CprLocation{}

	if !frame0.IsEven() {
		t.Error("frame 0 needs to be even")
	}
	if frame1.IsEven() {
		t.Error("frame 1 needs to be odd")
	}
	if err := cpr.SetEvenLocation(float64(frame0.Latitude()), float64(frame0.Longitude()), frame0.TimeStamp()); nil != err {
		t.Error(err)
	}
	if err := cpr.SetOddLocation(float64(frame1.Latitude()), float64(frame1.Longitude()), frame1.TimeStamp()); nil != err {
		t.Error(err)
	}
	if cpr.evenLat != 115609 {
		t.Errorf("incorrect value decoded for CPR Event Lat")
	}
	if cpr.evenLon != 116941 {
		t.Errorf("incorrect value decoded for CPR Event Lon")
	}
	if cpr.oddLat != 39199 {
		t.Errorf("incorrect value decoded for CPR Odd Lat")
	}
	if cpr.oddLon != 110269 {
		t.Errorf("incorrect value decoded for CPR Odd Lon")
	}

	// step 1: make sure we calculate J correctly
	cpr.globalSurfaceRange = 90.0
	cpr.computeLatitudeIndex()
	if cpr.latitudeIndex != 34 {
		t.Errorf("Incorrect Latitude Index (j) calculated. j - want 34, got: %d", cpr.latitudeIndex)
	}

	// step 2: let's decode the even and odd latitudes
	cpr.computeAirDLatRLat()
	if cpr.rlat0 != 52.323040008544920 { // even
		t.Errorf("Incorrect value computed for rlat0. got: %0.15f", cpr.rlat0)
	}
	if cpr.rlat1 != 52.320607072215964 { // odd
		t.Errorf("Incorrect value computed for rlat0. got: %0.15f", cpr.rlat1)
	}

	// make sure the longitudinal zone for both even and odd lats are the same
	if err := cpr.computeLongitudeZone(); nil != err {
		t.Error(err)
	}

	// make sure our twiddling works
	if err := cpr.surfacePosQuadrantTwiddle(refLat); nil != err {
		t.Error(err)
	}

	loc, err := cpr.computeLatLon()
	if nil != err {
		t.Error(err)
		return
	}
	if nil == loc {
		t.Error("Failed to decode position")
		return
	}
	if !cpr.evenDecode {
		t.Error("Expected to use even frame decoding")
	}

	// the example says to use the other (52.320) value, but that doesn't jive with the working tests above
	// it does however work if they got the odd/even the wrong way around
	if 52.323040008544922 != loc.latitude {
		t.Errorf("Failed to decode latitude properly. got %0.15f", loc.latitude)
	}

	// now we do it all again, using the method for this and make sure we get the same answers

	loc1, err := cpr.decodeSurface(refLat, refLon)
	if nil != err {
		t.Error(err)
	}
	if !loc1.onGround {
		t.Error("Plane should have been on the ground")
	}
}

func TestDecodeFailsOnBadData(t *testing.T) {
	cpr := CprLocation{}
	_ = cpr.SetEvenLocation(1, 2, time.Now())
	_ = cpr.SetOddLocation(888888, 888888, time.Now())

	location, err := cpr.decodeGlobalAir()

	if nil == err {
		t.Errorf("Failed to Fail! we should not be able to decode incomprehensible CPR locations")
	}

	if nil != location {
		t.Errorf("Failed to Fail! we should not be able to decode incomprehensible CPR locations")
	}
}

func TestDecodeFailsOnNoOddLoc(t *testing.T) {
	trk := NewTracker()
	plane := trk.GetPlane(1235)
	if err := plane.setCprEvenLocation(92095, 39846, time.Now()); nil != err {
		t.Error(err)
	}

	location, err := plane.cprLocation.decodeGlobalAir()

	if nil == err {
		t.Errorf("Failed to Fail! we should not be able to decode When there is no odd CPR location")
	}

	if nil != location {
		t.Error("Failed to Fail! we should not be able to decode When there is no odd CPR location")
	}
}

func TestDecodeFailsOnNoEvenLoc(t *testing.T) {
	cpr := CprLocation{}
	if err := cpr.SetOddLocation(88385, 125818, time.Now()); nil != err {
		t.Error(err)
	}

	location, err := cpr.decodeGlobalAir()
	if nil == err {
		t.Errorf("Failed to Fail! we should not be able to decode When there is no even CPR location")
	}
	if nil != location {
		t.Error("Failed to Fail! we should not be able to decode When there is no even CPR location")
	}
}

func TestCprDecode2(t *testing.T) {
	type testDataType struct {
		receivedAt time.Time
		raw        string
		icoa       string
		lat        string
		lon        string
		alt        int32
		isEven     bool
	}
	testData := [][]testDataType{
		{
			{raw: "*8d7c4516581f76e48d95e8ab20ca;", icoa: "7c4516", isEven: false, alt: 5175, lat: "+0.000000", lon: "+0.000000", receivedAt: time.Now()},
			{raw: "*8d7c4516581f6288f83ade534ae1;", icoa: "7c4516", isEven: true, alt: 5150, lat: "-32.197483", lon: "+116.028629", receivedAt: time.Now().Add(time.Millisecond)},
		},
		{
			{raw: "*8d7c4516580f06fc6d8f25d8669d;", icoa: "7c4516", isEven: false, alt: 1800, lat: "+0.000000", lon: "+0.000000", receivedAt: time.Now()},
			{raw: "*8d7c4516580df2a168340b32212a;", icoa: "7c4516", isEven: true, alt: 1775, lat: "-32.055219", lon: "+115.931602", receivedAt: time.Now().Add(time.Millisecond)},
		},
	}
	for _, test := range testData {
		trk := NewTracker()
		for i, d := range test {
			frame, err := mode_s.DecodeString(d.raw, d.receivedAt)
			if nil != err {
				t.Error(err)
			}
			plane := trk.GetPlane(frame.Icao())
			plane.HandleModeSFrame(frame, nil)

			if nil == plane {
				t.Errorf("Plane data should have been updated")
				continue
			}
			if plane.Altitude() != d.alt {
				t.Errorf("Plane altitude is wrong for packet %d: should be %d, was %d", i, d.alt, plane.Altitude())
			}

			lat := fmt.Sprintf("%+0.6f", plane.Lat())
			lon := fmt.Sprintf("%+0.6f", plane.Lon())

			if lat != d.lat {
				t.Errorf("Plane latitude is wrong for packet %d: should be %s was %s", i, d.lat, lat)
			}
			if lon != d.lon {
				t.Errorf("Plane latitude is wrong for packet %d: should be %s was %s", i, d.lon, lon)
			}
		}
	}

}
