package modes2

const (
	AltitudeUnitFeet  AltitudeUnit = 0
	AltitudeUnitMetre AltitudeUnit = 1

	VerticalRateSourceGNSS       VerticalRateSource = 0
	VerticalRateSourceBarometric VerticalRateSource = 1

	DO260  ADSBVersion = 0
	DO260A ADSBVersion = 1
	DO260B ADSBVersion = 2

	DF00ShortAirToAir             = 0
	DF04SurveillanceAltitudeReply = 4
	DF05SurveillanceIdentReply    = 5
	DF11ModeSAllCallReply         = 11
	DF16LongAirToAir              = 16
	DF17ADSBExtendedSquitter      = 17
	DF18ADSBSupplementary         = 18
	DF19ADSBMilitary              = 19
	DF20CommB                     = 20
	DF21CommB                     = 21
	DF22Military                  = 22
	DF24CommD                     = 24
)

type (
	AltitudeUnit       byte
	VerticalRateSource byte
	ADSBVersion        byte

	DO260ATargetInfo uint64
	DO260BTargetInfo uint64

	OperationalStatusInfo uint64

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
		FlightNumber                    string
		Heading, Velocity, VerticalRate float64

		TargetD0260A    DO260ATargetInfo
		TargetD0260B    DO260BTargetInfo
		OperationalInfo OperationalStatusInfo

		ICAO                 int // 24 bit ICAO aircraft code
		Altitude             int
		HeightAboveEllipsoid int // Delta above Ellipsoid
		Squawk               uint32

		CprLat, CprLon int
		CprOddEven     byte

		AltUnit     AltitudeUnit
		ADSBVersion ADSBVersion

		MessageType    byte
		MessageSubType byte

		Emergency byte

		SurveillanceStatus, NicSupplementB byte
		NavigationalAccuracyV              byte // NACv

		VerticalRateSource VerticalRateSource

		CategoryType, CategorySubType byte
		CategoryValid                 bool

		Military, Interrogatable bool

		IntentChange                     bool // aircraft wants to change altitude
		IFRCapable                       bool // ADSBv1 only, Instrument Flight Rules Capable
		SuperSonic                       bool // plane go zoom zoom
		OnGround, ValidVertical          bool
		ValidHeading, ValidSquawk        bool
		ValidVelocity, TrueAirSpeed      bool
		ValidAltitude, ValidVerticalRate bool
		ValidHAE, ValidNacV              bool
		ValidEmergency, ValidADSBVersion bool

		UTCTimeSync bool
	}
	DF18 struct {
		ICAO int // 24 bit ICAO aircraft code
	}

	// DF20 CommB Reply
	// DF/20/21 are same as DF/4/5 with a
	// 56 bit data field
	DF20 struct {
		ICAO            int // 24 bit ICAO aircraft code
		Altitude        int
		AltUnit         AltitudeUnit
		FlightStatus    byte
		DownlinkRequest byte
		UtilityRequest  byte
		OnGround        bool
		Emergency       bool
		BdsRegisters    uint64
	}
	// DF21 CommB Reply
	// DF/20/21 are same as DF/4/5 with a
	// 56 bit data field
	DF21 struct {
		ICAO            int // 24 bit ICAO aircraft code
		Squawk          int16
		FlightStatus    byte
		DownlinkRequest byte
		UtilityRequest  byte
		OnGround        bool
		Emergency       bool
		BdsRegisters    uint64
	}

	DF24 struct {
		ICAO int // 24 bit ICAO aircraft code
	}
)
