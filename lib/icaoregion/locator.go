// Package icaoregion provides functionality to determine which ICAO air navigation
// region a given geographic coordinate belongs to.
//
// The package embeds a GeoJSON dataset of ICAO Flight Information Regions (FIRs)
// obtained from the open-source AeroGeoJSON project (https://github.com/dkozickis/AeroGeoJSON).
// Each FIR feature in this dataset includes a "region" property corresponding to one of
// ICAO’s nine air navigation regions as defined in ICAO Doc 7030 and associated regional
// air navigation plans.
//
// # Overview
//
// The Locator type provides a read-only spatial index of all FIR boundaries and allows
// concurrent queries from multiple goroutines without additional synchronisation.
//
// A Locator is created with NewLocator, which parses the embedded FIR GeoJSON and prepares
// it for spatial queries:
//
//	l, err := icaoregion.NewLocator()
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// Once constructed, the Locator can be queried using either RegionOfLatLon or RegionOfPoint
// to determine which ICAO region a geographic point lies within:
//
//	r := l.RegionOfLatLon(-31.9505, 115.8575)
//	fmt.Println(r) // Output: APAC (for Perth, Australia)
//
// # Regions
//
// The Region type represents the ICAO air navigation regions:
//
//   - AFI  – Africa–Indian Ocean
//   - ASIA – Asia
//   - CAR  – Caribbean
//   - EUR  – Europe
//   - MID  – Middle East
//   - NAM  – North America
//   - NAT  – North Atlantic
//   - SAM  – South America
//
// If a point does not fall within any defined region, RegionOfLatLon returns Unknown.
//
// # Concurrency
//
// A Locator is immutable after construction. It is safe for concurrent use by multiple
// goroutines without additional locking, as no internal state is modified during queries.
//
// # References
//
//   - ICAO Doc 7030 – Regional Supplementary Procedures
//   - ICAO Regions overview: https://skybrary.aero/articles/icao-regions
//   - AeroGeoJSON FIR dataset: https://github.com/dkozickis/AeroGeoJSON
package icaoregion

import (
	_ "embed"

	"github.com/paulmach/orb"
	"github.com/paulmach/orb/geojson"
	"github.com/paulmach/orb/planar"
)

// firGeoJason is obtained from: https://github.com/dkozickis/AeroGeoJSON
//
//go:embed fir.geojson
var firGeoJson []byte

func lookupRegion(r string) Region {
	switch r {
	case "AFI":
		return AFI
	case "ASIA":
		return ASIA
	case "CAR":
		return CAR
	case "EUR":
		return EUR
	case "MID":
		return MID
	case "NAM":
		return NAM
	case "NAT":
		return NAT
	case "SAM":
		return SAM
	default:
		return Unknown
	}
}

// Locator is used to look up ICAO Regions based on lat/lon
type Locator struct {
	fc *geojson.FeatureCollection
}

// NewLocator returns a new instance of Locator
func NewLocator() (*Locator, error) {
	// Parse as FeatureCollection
	fc, err := geojson.UnmarshalFeatureCollection(firGeoJson)
	if err != nil {
		return nil, err
	}
	return &Locator{fc: fc}, nil
}

// RegionOfLatLon returns the ICAO region of a point defined by lat, lon.
func (l *Locator) RegionOfLatLon(lat, lon float64) Region {
	return l.RegionOfPoint(orb.Point{lon, lat})
}

// RegionOfPoint returns the ICAO region of point p
func (l *Locator) RegionOfPoint(p orb.Point) Region {
	for _, f := range l.fc.Features {
		switch geom := f.Geometry.(type) {
		case orb.Polygon:
			if planar.PolygonContains(geom, p) {
				return lookupRegion(f.Properties.MustString("region"))
			}
		case orb.MultiPolygon:
			if planar.MultiPolygonContains(geom, p) {
				return lookupRegion(f.Properties.MustString("region"))
			}
		}
	}
	return Unknown
}
