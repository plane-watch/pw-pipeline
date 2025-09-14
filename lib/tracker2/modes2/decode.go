package modes2

import (
	"errors"
)

// DecodeDF0 decodes Downlink Format 0 ((0) Short air-air surveillance (TCAS)) messages
func (a AvrFrame) DecodeDF0() (DF0, error) {
	if a.a == 0 {
		// valid DF0 frame, but not exactly useful
		return DF0{}, errors.New("blank DF00 msg")
	}

	altitude, unit, err := a.decode13bitAltitudeCode()

	ret := DF0{
		ICAO:                a.ICAO(),
		Altitude:            altitude,
		AltUnit:             unit,
		SensitivityLevel:    byte((a.a & 0x00_E0_00_00_00_00_00_00) >> 53),
		ReplyInformation:    byte((a.a & 0x00_07_80_00_00_00_00_00) >> 47),
		CrosslinkCapability: a.a&0x02_00_00_00_00_00_00_00 > 0,
		OnGround:            a.a&0x04_00_00_00_00_00_00_00 > 0,
	}

	if ret.OnGround && ret.Altitude > 16_000 {
		// world highest alt airport is 4411m or 14,472ft - allowing for some altimeter shenanigans,
		// anything in here is an invalid frame
		return ret, ErrImpossibleOnGroundAltitude
	}

	return ret, err
}

// DecodeDF4 decodes Downlink Format 4 (Roll Call Reply - altitude (~100ft accuracy)) messages
func (a AvrFrame) DecodeDF4() (DF4, error) {
	altitude, unit, err := a.decode13bitAltitudeCode()

	ret := DF4{
		ICAO:            a.ICAO(),
		Altitude:        altitude,
		AltUnit:         unit,
		FlightStatus:    byte((a.a & 0x07_00_00_00_00_00_00_00) >> 56), // 3 bits
		DownlinkRequest: byte((a.a & 0x00_F8_00_00_00_00_00_00) >> 51), // 5 bits
		UtilityRequest:  byte((a.a & 0x00_07_E0_00_00_00_00_00) >> 45), // 6 bits,
	}
	if !isFlightStatusValid(ret.FlightStatus) {
		return ret, ErrInvalidFlightStatus
	}
	ret.OnGround = isFlightStatusOnGround(ret.FlightStatus)
	ret.Emergency = isFlightStatusAlert(ret.FlightStatus)

	if ret.OnGround && ret.Altitude > 16_000 {
		// world highest alt airport is 4411m or 14,472ft - allowing for some altimeter shenanigans,
		// anything in here is an invalid frame
		return ret, ErrImpossibleOnGroundAltitude
	}

	return ret, err
}

// DecodeDF5 decodes Downlink Format 5 (Roll Call Reply - squawk) messages
func (a AvrFrame) DecodeDF5() (DF5, error) {
	ret := DF5{
		ICAO:            a.ICAO(),
		FlightStatus:    byte((a.a & 0x07_00_00_00_00_00_00_00) >> 56), // 3 bits
		DownlinkRequest: byte((a.a & 0x00_F8_00_00_00_00_00_00) >> 51), // 5 bits
		UtilityRequest:  byte((a.a & 0x00_03_E0_00_00_00_00_00) >> 45), // 6 bits
	}
	if !isFlightStatusValid(ret.FlightStatus) {
		return ret, ErrInvalidFlightStatus
	}
	ret.OnGround = isFlightStatusOnGround(ret.FlightStatus)
	ret.Emergency = isFlightStatusAlert(ret.FlightStatus)

	var err error
	ret.Squawk, err = decodeID13Field(int16(a.a & 0x00_00_1F_FF_00_00_00_00 >> 32))
	return ret, err
}

// DecodeDF11 decodes Downlink Format 11 (All-Call reply containing aircraft address) messages
func (a AvrFrame) DecodeDF11() (DF11, error) {
	// make sure our Checksum matches our CRC
	if a.messageCrc() != a.checkSum {
		return DF11{}, ErrChecksumFailed
	}

	ret := DF11{
		ICAO:       a.ICAO(),
		Capability: byte(a.a & 0x07_00_00_00_00_00_00_00 >> 56),
	}

	return ret, nil
}

func (a AvrFrame) DecodeDF16() (DF16, error) {
	altitude, unit, err := a.decode13bitAltitudeCode()

	ret := DF16{
		ICAO:             a.ICAO(),
		Altitude:         altitude,
		CommVMsg:         ((a.a << 24) & 0x00_FF_FF_FF_FF_00_00_00) | (a.b >> 40),
		AltUnit:          unit,
		OnGround:         a.a&0x04_00_00_00_00_00_00_00 > 0,
		TCASSensitivity:  byte(a.a & 0x00_E0_00_00_00_00_00_00 >> 53),
		ReplyInformation: byte(a.a & 0x00_07_80_00_00_00_00_00 >> 47),
	}

	if ret.OnGround && ret.Altitude > 16_000 {
		// world highest alt airport is 4411m or 14,472ft - allowing for some altimeter shenanigans,
		// anything in here is an invalid frame
		return ret, ErrImpossibleOnGroundAltitude
	}

	return ret, err
}
