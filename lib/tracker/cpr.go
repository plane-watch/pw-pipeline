package tracker

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"
)

// CprLocation - meanings: 0 is even frame, 1 is odd frame
type CprLocation struct {
	time0              time.Time
	time1              time.Time
	airDLat1           float64
	globalSurfaceRange float64
	oddLon             float64
	oddLat             float64
	evenLat            float64
	rlat0              float64
	rlat1              float64
	airDLat0           float64
	evenLon            float64
	refLon             float64
	refLat             float64
	rwLock             sync.RWMutex
	latitudeIndex      int32
	nl0                int32
	nl1                int32
	evenDecode         bool
	oddDecode          bool
	evenFrame          bool
	oddFrame           bool
}

var NLTable = map[int32]float64{
	59: 10.47047130,
	58: 14.82817437,
	57: 18.18626357,
	56: 21.02939493,
	55: 23.54504487,
	54: 25.82924707,
	53: 27.93898710,
	52: 29.91135686,
	51: 31.77209708,
	50: 33.53993436,
	49: 35.22899598,
	48: 36.85025108,
	47: 38.41241892,
	46: 39.92256684,
	45: 41.38651832,
	44: 42.80914012,
	43: 44.19454951,
	42: 45.54626723,
	41: 46.86733252,
	40: 48.16039128,
	39: 49.42776439,
	38: 50.67150166,
	37: 51.89342469,
	36: 53.09516153,
	35: 54.27817472,
	34: 55.44378444,
	33: 56.59318756,
	32: 57.72747354,
	31: 58.84763776,
	30: 59.95459277,
	29: 61.04917774,
	28: 62.13216659,
	27: 63.20427479,
	26: 64.26616523,
	25: 65.31845310,
	24: 66.36171008,
	23: 67.39646774,
	22: 68.42322022,
	21: 69.44242631,
	20: 70.45451075,
	19: 71.45986473,
	18: 72.45884545,
	17: 73.45177442,
	16: 74.43893416,
	15: 75.42056257,
	14: 76.39684391,
	13: 77.36789461,
	12: 78.33374083,
	11: 79.29428225,
	10: 80.24923213,
	9:  81.19801349,
	8:  82.13956981,
	7:  83.07199445,
	6:  83.99173563,
	5:  84.89166191,
	4:  85.75541621,
	3:  86.53536998,
	2:  87.00000000,
}

func (cpr *CprLocation) canDecode() bool {
	cpr.rwLock.RLock()
	defer cpr.rwLock.RUnlock()
	return cpr.oddFrame && cpr.evenFrame
}

func (cpr *CprLocation) zero(lock bool) {
	if lock {
		cpr.rwLock.Lock()
		defer cpr.rwLock.Unlock()
	}
	cpr.evenLat = 0
	cpr.evenLon = 0
	cpr.oddLat = 0
	cpr.oddLon = 0
	cpr.rlat0 = 0
	cpr.rlat1 = 0
	cpr.time0 = time.Unix(0, 0)
	cpr.time1 = time.Unix(0, 0)
	cpr.evenFrame = false
	cpr.oddFrame = false
}

func (cpr *CprLocation) SetEvenLocation(lat, lon float64, t time.Time) error {
	// cpr locations are 17 bits long, if we get a value outside of this then we have a problem
	if lat > max17Bits || lat < 0 || lon > max17Bits || lon < 0 {
		return fmt.Errorf("CPR Raw Lat/Lon can be a max of %d, got %0.4f,%0.4f", max17Bits, lat, lon)
	}
	cpr.rwLock.Lock()
	defer cpr.rwLock.Unlock()

	cpr.evenLat = lat
	cpr.evenLon = lon
	cpr.time0 = t
	cpr.evenFrame = true
	return nil
}

func (cpr *CprLocation) SetOddLocation(lat, lon float64, t time.Time) error {
	// cpr locations are 17 bits long, if we get a value outside of this then we have a problem
	if lat > max17Bits || lat < 0 || lon > max17Bits || lon < 0 {
		return fmt.Errorf("CPR Raw Lat/Lon can be a max of %d, got %0.4f,%0.4f", max17Bits, lat, lon)
	}
	cpr.rwLock.Lock()
	defer cpr.rwLock.Unlock()
	// only set the odd frame after the even frame is set
	// if !p.cprLocation.evenFrame {
	// 	return
	// }

	cpr.oddLat = lat
	cpr.oddLon = lon
	cpr.time1 = t
	cpr.oddFrame = true
	return nil
}

func (cpr *CprLocation) decode(onGround bool) (*PlaneLocation, error) {
	if !cpr.canDecode() {
		return nil, nil
	}
	// now guard the entire decode process
	cpr.rwLock.Lock()
	defer cpr.rwLock.Unlock()

	// attempt to decode the CPR LAT/LON
	var loc *PlaneLocation
	var err error

	if onGround {
		if cpr.refLat == 0 && cpr.refLon == 0 {
			return nil, errors.New("unable to decode surface position without reference lat/lon")
		}
		loc, err = cpr.decodeSurface(cpr.refLat, cpr.refLon)
	} else {
		loc, err = cpr.decodeGlobalAir()
	}
	cpr.zero(false)
	return loc, err

}

// computeLatitudeIndex computes `j` in the decode algorithm
func (cpr *CprLocation) computeLatitudeIndex() {
	cpr.latitudeIndex = int32(math.Floor((((59 * cpr.evenLat) - (60 * cpr.oddLat)) / 131072) + 0.5))
	// log.Printf("J = %d", cpr.latitudeIndex)
}

func (cpr *CprLocation) computeAirDLatRLat() {
	cpr.airDLat0 = cpr.globalSurfaceRange / 60.0
	cpr.airDLat1 = cpr.globalSurfaceRange / 59.0
	cpr.rlat0 = cpr.airDLat0 * (cprModFunction(cpr.latitudeIndex, 60) + (cpr.evenLat / 131072))
	cpr.rlat1 = cpr.airDLat1 * (cprModFunction(cpr.latitudeIndex, 59) + (cpr.oddLat / 131072))
	// log.Printf("j=%d rlat0=%0.6f rlat1=%0.6f", cpr.latitudeIndex, cpr.rlat0, cpr.rlat1)
}

func (cpr *CprLocation) computeLongitudeZone() error {
	cpr.nl0 = getNumLongitudeZone(cpr.rlat0)
	cpr.nl1 = getNumLongitudeZone(cpr.rlat1)

	if cpr.nl0 != cpr.nl1 {
		return fmt.Errorf("incorrect NL calculation %d!=%d (for lat/lon %0.13f / %0.13f)", cpr.nl0, cpr.nl1, cpr.rlat0, cpr.rlat1)
	}
	// log.Printf("nl0: %d, nl1: %d", cpr.nl0, cpr.nl1)
	return nil
}

// checkFrameTiming makes sure that the difference in receive time between 2 lat/lon frames is < 1 second
// they are emitted twice a second (2hz). This means one half of the lat/lon is tx every 0.5s
func (cpr *CprLocation) checkFrameTiming() error {
	d := cpr.time1.Sub(cpr.time0)
	tt := math.Abs(float64(d.Milliseconds()))
	if tt > 10_000 { // allow for some network jitter + time of flight jitter
		return errors.New("unable to decode this CPR Pair. they are too far apart in time")
	}
	return nil
}

func (cpr *CprLocation) computeLatLon() (*PlaneLocation, error) {
	var loc PlaneLocation
	if cpr.time1.Before(cpr.time0) {
		cpr.oddDecode = true
		cpr.evenDecode = false
		// log.Println("Odd Decode")
		// this assumes we are using the odd packet to decode
		/* Compute ni and the longitude index 'm' */
		ni := cprNFunction(cpr.rlat1, 1)
		// log.Printf("	ni = %d", ni)
		m := math.Floor((((cpr.evenLon * float64(cpr.nl1-1)) - (cpr.oddLon * float64(cpr.nl1))) / 131072.0) + 0.5)
		// log.Printf("	m = %0.2f", m)

		loc.longitude = cpr.dlonFunction(cpr.rlat1, 1) * (cprModFunction(int32(m), ni) + (cpr.oddLon / 131072))
		loc.latitude = cpr.rlat1
		loc.cprDecodedTS = cpr.time1
	} else {
		// do even decode
		cpr.oddDecode = false
		cpr.evenDecode = true
		// log.Println("Even Decode")
		ni := cprNFunction(cpr.rlat0, 0)
		// log.Printf("	ni = %d", ni)
		m := math.Floor((((cpr.evenLon * float64(cpr.nl0-1)) - (cpr.oddLon * float64(cpr.nl0))) / 131072.0) + 0.5)
		// log.Printf("	m = %0.2f", m)
		loc.longitude = cpr.dlonFunction(cpr.rlat0, 0) * (cprModFunction(int32(m), ni) + cpr.evenLon/131072)
		loc.latitude = cpr.rlat0
		loc.cprDecodedTS = cpr.time0
	}
	// log.Printf("\tlat = %0.6f, lon = %0.6f\n", loc.latitude, loc.longitude)
	return &loc, nil
}

func (cpr *CprLocation) surfaceLongitudeTwiddle(refLon float64, loc *PlaneLocation) {

	// Pick the quadrant that's closest to the reference location -
	// this is not necessarily the same quadrant that contains the
	// reference location. Unlike the latitude case, all four
	// quadrants are valid.

	// if reflon is more than 45 degrees away, move some multiple of 90 degrees towards it
	loc.longitude += math.Floor((refLon-loc.longitude+45.0)/90.0) * 90.0 // this might move us outside (-180..+180), we fix this below

	loc.longitude -= math.Floor((loc.longitude+180.0)/360.0) * 360.0
}

func (cpr *CprLocation) normaliseLatLon(loc *PlaneLocation) error {
	if loc.longitude > 180.0 {
		loc.longitude -= 360.0
	}
	// log.Printf("post normalise rlat = %0.6f, rlon = %0.6f\n", loc.latitude, loc.longitude);

	if loc.latitude < -90 || loc.latitude > 90 {
		return fmt.Errorf("failed to decode CPR Lat %0.13f is out of range", loc.latitude)
	}

	return nil
}

func (cpr *CprLocation) decodeSurface(refLat, refLon float64) (*PlaneLocation, error) {
	var err error
	cpr.globalSurfaceRange = 90.0

	if refLat == 0 && refLon == 0 {
		return nil, errors.New("invalid Reference location")
	}

	// basic check - make sure we have both frames
	if !(cpr.oddFrame && cpr.evenFrame) {
		var s string
		if cpr.oddFrame {
			s = "Have Odd Frame"
		} else {
			s = "Have Even Frame"
		}
		return nil, fmt.Errorf("need both odd and even frames before decoding, %s", s)
	}

	// Compute the latitude Index "j"
	cpr.computeLatitudeIndex()

	// sets up our odd and even lat decodes (rlat0 and rlat1)
	cpr.computeAirDLatRLat()

	if err = cpr.surfacePosQuadrantTwiddle(refLat); nil != err {
		return nil, err
	}

	if err = cpr.computeLongitudeZone(); nil != err {
		return nil, err
	}

	if err = cpr.checkFrameTiming(); nil != err {
		return nil, err
	}

	locRet, err := cpr.computeLatLon()
	if nil != err {
		return nil, err
	}
	cpr.surfaceLongitudeTwiddle(refLon, locRet)
	if err = cpr.normaliseLatLon(locRet); nil != err {
		return nil, err
	}
	locRet.onGround = true
	return locRet, err
}

func (cpr *CprLocation) surfacePosQuadrantTwiddle(refLat float64) error {
	// Pick the quadrant that's closest to the reference location -
	// this is not necessarily the same quadrant that contains the
	// reference location.
	//
	// There are also only two valid quadrants: -90..0 and 0..90;
	// no correct message would try to encoding a latitude in the
	// ranges -180..-90 and 90..180.
	//
	// If the computed latitude is more than 45 degrees north of
	// the reference latitude (using the northern hemisphere
	// solution), then the southern hemisphere solution will be
	// closer to the reference latitude.
	//
	// e.g. reflat=0, rlat=44, use rlat=44
	//      reflat=0, rlat=46, use rlat=46-90 = -44
	//      reflat=40, rlat=84, use rlat=84
	//      reflat=40, rlat=86, use rlat=86-90 = -4
	//      reflat=-40, rlat=4, use rlat=4
	//      reflat=-40, rlat=6, use rlat=6-90 = -84

	// As a special case, -90, 0 and +90 all encode to zero, so
	// there's a little extra work to do there.

	if cpr.rlat0 == 0 {
		if refLat < -45 {
			cpr.rlat0 = -90
		} else if refLat > 45 {
			cpr.rlat0 = 90
		}
	} else if (cpr.rlat0 - refLat) > 45 {
		cpr.rlat0 -= 90
	}

	if cpr.rlat1 == 0 {
		if refLat < -45 {
			cpr.rlat1 = -90
		} else if refLat > 45 {
			cpr.rlat1 = 90
		}
	} else if (cpr.rlat1 - refLat) > 45 {
		cpr.rlat1 -= 90
	}

	// Check to see that the latitude is in range: -90 .. +90
	if cpr.rlat0 < -90 || cpr.rlat0 > 90 || cpr.rlat1 < -90 || cpr.rlat1 > 90 {
		return errors.New("Failed to decode CPR. Lat out of bounds")
	}
	return nil
}

func (cpr *CprLocation) decodeGlobalAir() (*PlaneLocation, error) {
	var err error

	// basic check - make sure we have both frames
	if !(cpr.oddFrame && cpr.evenFrame) {
		var s string
		if cpr.oddFrame {
			s = "Have Odd Frame"
		} else {
			s = "Have Even Frame"
		}
		return nil, fmt.Errorf("need both odd and even frames before decoding, %s", s)
	}
	cpr.globalSurfaceRange = 360.0

	// 1. Compute the latitude index (J):
	cpr.computeLatitudeIndex()

	// 2. Compute the values of rlat0 and rlat1:
	cpr.computeAirDLatRLat()

	// Note: Southern hemisphere values are 270° to 360°. Subtract 360°.
	if cpr.rlat0 >= 270 {
		cpr.rlat0 -= 360
	}
	if cpr.rlat1 >= 270 {
		cpr.rlat1 -= 360
	}

	if err = cpr.computeLongitudeZone(); nil != err {
		return nil, err
	}

	if err = cpr.checkFrameTiming(); nil != err {
		return nil, err
	}

	locRet, err := cpr.computeLatLon()
	if nil != err {
		return nil, err
	}

	if err = cpr.normaliseLatLon(locRet); nil != err {
		return nil, err
	}
	locRet.onGround = false
	return locRet, nil
}

// NL - The NL function uses the precomputed table from 1090-WP-9-14
func getNumLongitudeZone(lat float64) int32 {
	if lat < 0 {
		lat = math.Abs(lat)
	}

	for i := int32(59); i >= 2; i-- {
		// cannot use range, it does not guarantee order
		if lat < NLTable[i] {
			return i
		}
	}
	return 1
}

func cprNFunction(lat float64, isOdd int32) int32 {
	nl := getNumLongitudeZone(lat) - isOdd
	if nl < 1 {
		nl = 1
	}
	return nl
}

func (cpr *CprLocation) dlonFunction(lat float64, isOdd int32) float64 {
	// log.Printf("DLON = %0.1f / (n) %d, %0.2f", cpr.globalSurfaceRange, cprNFunction(lat, isOdd), cpr.globalSurfaceRange/float64(cprNFunction(lat, isOdd)))
	return cpr.globalSurfaceRange / float64(cprNFunction(lat, isOdd))
}

/* Always positive MOD operation, used for CPR decoding. */
func cprModFunction(a, b int32) float64 {
	res := math.Mod(float64(a), float64(b))
	if res < 0 {
		res += float64(b)
	}
	// return math.Floor(res)
	// log.Printf("Mod(%d, %d)=%0.2f", a, b, res)
	return res
}

// haversin(θ) function
func hsin(theta float64) float64 {
	return math.Pow(math.Sin(theta/2), 2)
}
