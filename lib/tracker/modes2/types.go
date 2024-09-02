package main

const (
	AltitudeUnitFeet  AltitudeUnit = 0
	AltitudeUnitMetre AltitudeUnit = 1

	VerticalRateSourceGNSS       VerticalRateSource = 0
	VerticalRateSourceBarometric VerticalRateSource = 1

	DO260  ADSBVersion = 0
	DO260A ADSBVersion = 1
	DO260B ADSBVersion = 2
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

// DO260ATargetInfo Target Fields

func (t DO260ATargetInfo) VerticalDataSource() byte { return byte(t & 0x00_01_80_00_00_00_00_00 >> 47) }
func (t DO260ATargetInfo) TargetAltType() byte      { return byte(t & 0x00_00_40_00_00_00_00_00 >> 46) }
func (t DO260ATargetInfo) TargetAltCap() byte       { return byte(t & 0x00_00_18_00_00_00_00_00 >> 43) }
func (t DO260ATargetInfo) VerticalMode() byte       { return byte(t & 0x00_00_06_00_00_00_00_00 >> 41) }
func (t DO260ATargetInfo) TargetAltitude() byte     { return byte(t & 0x00_00_01_FF_80_00_00_00 >> 31) }
func (t DO260ATargetInfo) HorizontalData() byte     { return byte(t & 0x00_00_00_00_60_00_00_00 >> 29) }
func (t DO260ATargetInfo) TargetHeading() byte      { return byte(t & 0x00_00_00_00_1F_F0_00_00 >> 20) }
func (t DO260ATargetInfo) TargetHeadingSign() byte  { return byte(t & 0x00_00_00_00_00_08_00_00 >> 19) }
func (t DO260ATargetInfo) HorizontalMode() byte     { return byte(t & 0x00_00_00_00_00_06_00_00 >> 17) }

// NACp Navigation Accuracy Category — Position (NACP)
func (t DO260ATargetInfo) NACp() byte { return byte(t & 0x00_00_00_00_00_01_E0_00 >> 13) }

// NICbaro Navigation Integrity Category — Baro (NICBARO)
func (t DO260ATargetInfo) NICbaro() byte { return byte(t & 0x00_00_00_00_00_00_10_00 >> 12) }

// SIL Surveillance Integrity Level
func (t DO260ATargetInfo) SIL() byte { return byte(t & 0x00_00_00_00_00_00_0C_00 >> 10) }

// CapModeCodes Capability / Mode Codes
func (t DO260ATargetInfo) CapModeCodes() byte { return byte(t & 0x00_00_00_00_00_00_00_18 >> 3) }
func (t DO260ATargetInfo) Emergency() byte    { return byte(t & 0x00_00_00_00_00_00_00_07) }

// DO260BTargetInfo Fields

func (t DO260BTargetInfo) SILSupplement() byte     { return byte(t & 0x00_01_00_00_00_00_00_00 >> 48) }
func (t DO260BTargetInfo) SelectedAltType() byte   { return byte(t & 0x00_00_80_00_00_00_00_00 >> 47) }
func (t DO260BTargetInfo) SelectedAltitude() int   { return int(t & 0x00_00_7F_F0_00_00_00_00 >> 36) } //nolint:gosec
func (t DO260BTargetInfo) BarometricPressure() int { return int(t & 0x00_00_00_0F_F8_00_00_00 >> 27) } //nolint:gosec
func (t DO260BTargetInfo) Status() byte            { return byte(t & 0x00_00_00_00_04_00_00_00 >> 26) }
func (t DO260BTargetInfo) Sign() byte              { return byte(t & 0x00_00_00_00_02_00_00_00 >> 25) }
func (t DO260BTargetInfo) SelectedHeading() byte   { return byte(t & 0x00_00_00_00_01_FE_00_00 >> 17) }

// NACp Navigation Accuracy Category — Position (NACP)
func (t DO260BTargetInfo) NACp() byte { return byte(t & 0x00_00_00_00_00_01_E0_00 >> 13) }

// NICbaro Navigation Integrity Category — Baro (NICBARO)
func (t DO260BTargetInfo) NICbaro() byte { return byte(t & 0x00_00_00_00_00_00_10_00 >> 12) }

// SIL Surveillance Integrity Level
func (t DO260BTargetInfo) SIL() byte { return byte(t & 0x00_00_00_00_00_00_0C_00 >> 10) }

func (t DO260BTargetInfo) MCPFPUStatus() byte        { return byte(t & 0x00_00_00_00_00_00_02_00 >> 9) }
func (t DO260BTargetInfo) AutoPilotEngaged() bool    { return t&0x00_00_00_00_00_00_01_00 > 0 }
func (t DO260BTargetInfo) APVNavMode() bool          { return t&0x00_00_00_00_00_00_00_80 > 0 }
func (t DO260BTargetInfo) APAltitudeHoldMode() bool  { return t&0x00_00_00_00_00_00_00_40 > 0 }
func (t DO260BTargetInfo) ApproachMode() bool        { return t&0x00_00_00_00_00_00_00_10 > 0 }
func (t DO260BTargetInfo) TcasAcasOperational() bool { return t&0x00_00_00_00_00_00_00_08 > 0 }
func (t DO260BTargetInfo) APLNavMode() bool          { return t&0x00_00_00_00_00_00_00_04 > 0 }

// TargetAltitude is the calculated altitude the pilots have selected (not the raw value)
func (t DO260BTargetInfo) TargetAltitude() int {
	return (t.SelectedAltitude() - 1) * 32
}

// QNH signifies the atmospheric pressure adjusted to mean sea level. it is the calculated value of the BarometricPressure field
func (t DO260BTargetInfo) QNH() float64 {
	return 800.0 + float64(t.BarometricPressure()-1)*0.8
}

// TargetHeading is the calculated value from the SelectedHeading field
func (t DO260BTargetInfo) TargetHeading() float64 {
	return float64(t.SelectedHeading()) * 180.0 / 256.0
}

// // OperationalStatusInfo

// Version gets the type of into we are dealing with
func (t OperationalStatusInfo) Version() byte { return byte(t & 0x00_00_00_00_00_00_E0_00 >> 13) }
