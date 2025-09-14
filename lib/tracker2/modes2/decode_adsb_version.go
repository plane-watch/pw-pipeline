package modes2

import "strconv"

func (do ADSBVersion) String() string {
	switch do {
	case DO260:
		return "ADSB v0 (original)"
	case DO260A:
		return "ADSB v1 (DO260A)"
	case DO260B:
		return "ADSB v2 (DO260B)"
	default:
		return "unknown v" + strconv.FormatInt(int64(do), 10)
	}
}

// DO260ATargetInfo Target Fields

func (t DO260ATargetInfo) VerticalDataSource() byte { return byte(t & 0x00_01_80_00_00_00_00_00 >> 47) } // 2 bits
func (t DO260ATargetInfo) TargetAltType() byte      { return byte(t & 0x00_00_40_00_00_00_00_00 >> 46) } // 1 bit
func (t DO260ATargetInfo) TargetAltCap() byte       { return byte(t & 0x00_00_18_00_00_00_00_00 >> 43) } // 2 bit
func (t DO260ATargetInfo) VerticalMode() byte       { return byte(t & 0x00_00_06_00_00_00_00_00 >> 41) } // 2 bit
func (t DO260ATargetInfo) TargetAltitude() int      { return int(t & 0x00_00_01_FF_80_00_00_00 >> 31) }  // 10 bit
func (t DO260ATargetInfo) HorizontalData() byte     { return byte(t & 0x00_00_00_00_60_00_00_00 >> 29) } // 2 bit
func (t DO260ATargetInfo) TargetHeading() int       { return int(t & 0x00_00_00_00_1F_F0_00_00 >> 20) }  // 9 bit
func (t DO260ATargetInfo) TargetHeadingSign() byte  { return byte(t & 0x00_00_00_00_00_08_00_00 >> 19) } // 1 bit
func (t DO260ATargetInfo) HorizontalMode() byte     { return byte(t & 0x00_00_00_00_00_06_00_00 >> 17) } // 2 bit

// NACp Navigation Accuracy Category — Position (NACP)
func (t DO260ATargetInfo) NACp() byte { return byte(t & 0x00_00_00_00_00_01_E0_00 >> 13) } // 4 bit

// NICbaro Navigation Integrity Category — Baro (NICBARO)
func (t DO260ATargetInfo) NICbaro() byte { return byte(t & 0x00_00_00_00_00_00_10_00 >> 12) } // 1 bit

// SIL Surveillance Integrity Level
func (t DO260ATargetInfo) SIL() byte { return byte(t & 0x00_00_00_00_00_00_0C_00 >> 10) } // 2 bit

// CapModeCodes Capability / Mode Codes
func (t DO260ATargetInfo) CapModeCodes() byte { return byte(t & 0x00_00_00_00_00_00_00_18 >> 3) } // 2 bit
func (t DO260ATargetInfo) Emergency() byte    { return byte(t & 0x00_00_00_00_00_00_00_07) }      // 3 bit

// DO260BTargetInfo Fields

func (t DO260BTargetInfo) SILSupplement() byte     { return byte(t & 0x00_01_00_00_00_00_00_00 >> 48) } // 1 bit
func (t DO260BTargetInfo) SelectedAltType() byte   { return byte(t & 0x00_00_80_00_00_00_00_00 >> 47) } // 1 bit
func (t DO260BTargetInfo) SelectedAltitude() int   { return int(t & 0x00_00_7F_F0_00_00_00_00 >> 36) }  //nolint:gosec
func (t DO260BTargetInfo) BarometricPressure() int { return int(t & 0x00_00_00_0F_F8_00_00_00 >> 27) }  //nolint:gosec
func (t DO260BTargetInfo) Status() byte            { return byte(t & 0x00_00_00_00_04_00_00_00 >> 26) }
func (t DO260BTargetInfo) Sign() byte              { return byte(t & 0x00_00_00_00_02_00_00_00 >> 25) }
func (t DO260BTargetInfo) SelectedHeading() byte   { return byte(t & 0x00_00_00_00_01_FE_00_00 >> 17) } // 8 bit

// NACp Navigation Accuracy Category — Position (NACP)
func (t DO260BTargetInfo) NACp() byte { return byte(t & 0x00_00_00_00_00_01_E0_00 >> 13) } // 4 bit

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
