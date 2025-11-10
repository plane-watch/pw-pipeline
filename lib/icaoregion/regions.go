package icaoregion

// Region represents one of ICAO’s nine air navigation regions,
// as defined in ICAO Doc 7030 and related regional air navigation plans.
//
// See: https://skybrary.aero/articles/icao-regions
//
//go:generate stringer -type=Region
type Region uint8

const (
	// Unknown indicates that the ICAO region could not be determined.
	Unknown Region = iota

	// AFI represents the Africa-Indian Ocean Region.
	// It includes states within continental Africa and surrounding oceanic areas.
	AFI

	// ASIA represents the Asia Region.
	// It covers the Asian mainland and parts of Southeast and Central Asia not assigned to other ICAO regions.
	ASIA

	// CAR represents the Caribbean Region.
	// It covers the island states and territories of the Caribbean Sea.
	CAR

	// EUR represents the European Region.
	// It includes all European states and parts of adjacent areas under the ICAO EUR/NAT office.
	EUR

	// MID represents the Middle East Region.
	// It covers states in the Arabian Peninsula, Levant, and parts of North Africa and Western Asia.
	MID

	// NAM represents the North American Region.
	// It includes Canada, the United States, Mexico, and adjacent airspace.
	NAM

	// NAT represents the North Atlantic Region.
	// It covers the transatlantic oceanic airspace between North America and Europe.
	NAT

	// SAM represents the South American Region.
	// It includes all South American states and their adjacent oceanic airspace.
	SAM
)
