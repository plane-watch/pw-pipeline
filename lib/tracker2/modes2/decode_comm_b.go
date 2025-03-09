package modes2

// Perf from tests performed
// goos: linux
// goarch: amd64
// pkg: plane.watch/lib/tracker/modes2
// cpu: AMD Ryzen 9 7900X 12-Core Processor
//
// as normal full fat structs
// BenchmarkAvrFrame_DecodeDF20-24         26791585                44.79 ns/op            0 B/op          0 allocs/op
// BenchmarkAvrFrame_DecodeDF21-24         25729011                46.14 ns/op            0 B/op          0 allocs/op
//
//
// embedded DF4/DF5 before uint64(BdsRegisters)
// BenchmarkAvrFrame_DecodeDF20-24         20315286                58.54 ns/op            0 B/op          0 allocs/op
// BenchmarkAvrFrame_DecodeDF21-24         19732910                60.51 ns/op            0 B/op          0 allocs/op
//
// embedded uint64(BdsRegisters) before DF4/DF5
// BenchmarkAvrFrame_DecodeDF20-24         21369097                55.84 ns/op            0 B/op          0 allocs/op
// BenchmarkAvrFrame_DecodeDF21-24         21192322                56.71 ns/op            0 B/op          0 allocs/op

// DecodeDF20 is almost a copy/paste of DecodeDF4 - but for perf reasons it has its own logic
// in benchmarking it's about 10ns/op faster to do it this way (~44ns -> ~55 ns if we use struct embedding)
func (a AvrFrame) DecodeDF20() (DF20, error) {
	altitude, unit, err := a.decode13bitAltitudeCode()

	ret := DF20{
		ICAO:            a.ICAO(),
		Altitude:        altitude,
		AltUnit:         unit,
		FlightStatus:    byte((a.a & 0x07_00_00_00_00_00_00_00) >> 56), // 3 bits
		DownlinkRequest: byte((a.a & 0x00_F8_00_00_00_00_00_00) >> 51), // 5 bits
		UtilityRequest:  byte((a.a & 0x00_07_E0_00_00_00_00_00) >> 45), // 6 bits,
		BdsRegisters:    a.a&0x00_00_00_00_FF_FF_FF_FF<<24 | (a.b & 0xFF_FF_FF_00_00_00_00_00 >> 40),
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

// DecodeDF21 is almost a copy/paste of DecodeDF5 - but for perf reasons it has its own logic
// in benchmarking it's about 10ns/op faster to do it this way (~46ns -> ~56 ns if we use struct embedding)
func (a AvrFrame) DecodeDF21() (DF21, error) {
	ret := DF21{
		ICAO:            a.ICAO(),
		FlightStatus:    byte((a.a & 0x07_00_00_00_00_00_00_00) >> 56), // 3 bits
		DownlinkRequest: byte((a.a & 0x00_F8_00_00_00_00_00_00) >> 51), // 5 bits
		UtilityRequest:  byte((a.a & 0x00_03_E0_00_00_00_00_00) >> 45), // 6 bits
		BdsRegisters:    a.a&0x00_00_00_00_FF_FF_FF_FF<<24 | (a.b & 0xFF_FF_FF_00_00_00_00_00 >> 40),
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
