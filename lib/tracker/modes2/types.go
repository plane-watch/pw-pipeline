package main

type (
	DF0 struct {
		ICAO                int // 24 bit ICAO aircraft code
		Altitude            int
		AltUnit             AltitudeUnit
		ReplyInformation    byte
		SensitivityLevel    byte
		CrosslinkCapability bool
		OnGround            bool
	}

	DF4 struct {
		ICAO            int // 24 bit ICAO aircraft code
		Altitude        int
		AltUnit         AltitudeUnit
		FlightStatus    byte
		DownlinkRequest byte
		UtilityRequest  byte
		OnGround        bool
		Emergency       bool
	}

	DF5 struct {
		ICAO            int // 24 bit ICAO aircraft code
		Squawk          int16
		FlightStatus    byte
		DownlinkRequest byte
		UtilityRequest  byte
		OnGround        bool
		Emergency       bool
	}

	DF11 struct {
		ICAO       int // 24 bit ICAO aircraft code
		Capability byte
	}
	DF16 struct {
		ICAO             int // 24 bit ICAO aircraft code
		Altitude         int
		CommVMsg         uint64 // 56bits air-to-air message
		AltUnit          AltitudeUnit
		OnGround         bool
		TCASSensitivity  byte
		ReplyInformation byte // RI
	}

	DFADSB struct {
		FlightNumber      string
		Heading, Velocity float64

		ICAO     int // 24 bit ICAO aircraft code
		Altitude int

		CprLat, CprLon int
		CprOddEven     byte

		AltUnit AltitudeUnit

		MessageType    byte
		MessageSubType byte

		SurveillanceStatus, NicSupplementB byte

		CategoryType, CategorySubType byte
		CategoryValid                 bool

		Military, Interrogatable bool

		OnGround, ValidVertical     bool
		ValidHeading, ValidVelocity bool
		ValidAltitude               bool

		UTCTimeSync bool
	}

	DF19 struct {
		ICAO int // 24 bit ICAO aircraft code
	}
	DF20 struct {
		ICAO int // 24 bit ICAO aircraft code
	}
	DF21 struct {
		ICAO int // 24 bit ICAO aircraft code
	}
	DF24 struct {
		ICAO int // 24 bit ICAO aircraft code
	}
)
