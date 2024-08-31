package main

import (
	"bytes"
	"errors"
)

var (
	aisCharset = "@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_ !\"#$%&'()*+,-./0123456789:;<=>?"
)

func (a AvrFrame) DecodeADSB() (DFADSB, error) {
	ret := DFADSB{
		ICAO:           a.ICAO(),
		MessageType:    byte((a.a & 0x00_00_00_00_F8_00_00_00) >> 27),
		MessageSubType: byte((a.a & 0x00_00_00_00_07_00_00_00) >> 24),
		Military:       a.df == 19,
		Interrogatable: a.df == 17,
	}

	switch ret.MessageType {
	case 1, 2, 3, 4:
		/* Aircraft Identification and Category */
		ret.MessageSubType = 0
		ret.FlightNumber = decodeFlightNumber(
			//          0  1  2  3  4  5  6  7
			byte(a.a&0x00_00_00_00_00_FC_00_00>>18), // 11111100
			byte(a.a&0x00_00_00_00_00_03_F0_00>>12), // 00000011_11110000
			byte(a.a&0x00_00_00_00_00_00_0F_C0>>6),  //          00001111_11000000
			byte(a.a&0x00_00_00_00_00_00_00_3F),     //                   00111111
			byte(a.b&0xFC_00_00_00_00_00_00_00>>58),
			byte(a.b&0x03_F0_00_00_00_00_00_00>>52),
			byte(a.b&0x00_0F_C0_00_00_00_00_00>>46),
			byte(a.b&0x00_00_3F_00_00_00_00_00>>40),
		)
		ret.CategoryType = 4 - ret.MessageType
		ret.CategorySubType = byte(a.a & 0x00_00_00_00_07_00_00_00 >> 24)
		ret.CategoryValid = true

	case 5, 6, 7, 8:
		// surface position
		ret.MessageSubType = 0
		ret.OnGround = true
		ret.ValidVertical = true

		// Decode the Surface Movement Field
		ret.Velocity, ret.ValidVelocity = calcSurfaceSpeed(a.a & 0x00_00_00_00_07_F0_00_00 >> 20)

		// Decode the Heading Field -  if the 4th bit is set, the heading is valid
		if a.a&0x00_00_00_00_00_08_00_00 != 0 {
			ret.Heading = float64(a.a&0x00_00_00_00_00_07_F0_00>>12) * 360.0 / 128.0
			ret.ValidHeading = true
		}

		// decode one half of this planes lat/lon
		ret.CprOddEven = byte(a.a & 0x00_00_00_00_00_00_04_00 >> 10)                                   // 1 or 0
		ret.CprLat = int((a.a & 0x00_00_00_00_00_00_03_FF << 7) | (a.b&0xFE_00_00_00_00_00_00_00)>>57) //nolint:gosec
		ret.CprLon = int(a.b & 0x01_FF_FF_00_00_00_00_00 >> 40)                                        //nolint:gosec

	case 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 20, 21, 22:
		/* Airborne position Message */
		ret.MessageSubType = 0
		isGnssAlt := ret.MessageType >= 20
		ret.UTCTimeSync = a.a&0x00_00_00_00_00_00_08_00>>11 == 1
		ret.OnGround = false
		ret.ValidVertical = true
		ret.SurveillanceStatus = byte(a.a & 0x00_00_00_00_06_00_00_00 >> 25)
		ret.NicSupplementB = byte(a.a & 0x00_00_00_00_01_00_00_00 >> 24)

		altitudeField := int(a.a & 0x00_00_00_00_00_FF_F0_00 >> 12) //nolint:gosec
		if isGnssAlt {
			ret.Altitude = altitudeField
			ret.AltUnit = AltitudeUnitMetre
		} else {
			ret.Altitude, ret.AltUnit = decodeAC12Field(altitudeField)
		}
		ret.ValidAltitude = ret.Altitude != 0

		// decode one half of this planes lat/lon
		ret.CprOddEven = byte(a.a & 0x00_00_00_00_00_00_04_00 >> 10)                                   // 1 or 0
		ret.CprLat = int((a.a & 0x00_00_00_00_00_00_03_FF << 7) | (a.b&0xFE_00_00_00_00_00_00_00)>>57) //nolint:gosec
		ret.CprLon = int(a.b & 0x01_FF_FF_00_00_00_00_00 >> 40)                                        //nolint:gosec

	case 19:
	case 23:
	case 24:
	case 25, 26:
	case 27:
	case 28:
	case 29:
	case 30:
	case 31:

	}

	return ret, nil
}

var busted = []byte("@@@@@@@@")

func decodeFlightNumber(b0, b1, b2, b3, b4, b5, b6, b7 byte) string {
	callsign := []byte("        ")
	callsign[0] = aisCharset[b0]
	callsign[1] = aisCharset[b1]
	callsign[2] = aisCharset[b2]
	callsign[3] = aisCharset[b3]
	callsign[4] = aisCharset[b4]
	callsign[5] = aisCharset[b5]
	callsign[6] = aisCharset[b6]
	callsign[7] = aisCharset[b7]

	// because planes have sent us things like A90004A0200000000000007D8DB4
	// we need
	if bytes.Equal(callsign, busted) {
		callsign = nil
	}

	return string(callsign) // 1 8 byte alloc - yay escape analysis
}

func calcSurfaceSpeed(value uint64) (gSpeed float64, validVelocity bool) {
	if (value > 0) && (value < 125) {
		validVelocity = true

		switch {
		case value > 123:
			gSpeed = 175 // > 175kt
		case value > 108: // 109-123 - 5 kt steps
			gSpeed = float64((value-109)*5) + 100.0
		case value > 93: // 94 - 108 | 70kt - <100kt
			gSpeed = float64((value-94)*2) + 70
		case value > 38: // 39 - 93 | 15kt - <70kt | 1kt step:
			gSpeed = float64(value-39) + 15
		case value > 12: // 13-38 |  2 kt - <15kt | 0.5 kt steps:
			gSpeed = float64(value-13)*0.5 + 2
		case value > 8: // 9-12 | 1kt - < 2kt | 0.25 kt steps:
			gSpeed = float64(value-9)*0.25 + 1
		case value > 1:
			gSpeed = float64(value-1) * 0.125
		default:
			gSpeed = 0
		}
	}
	return
}

func decodeAC12Field(aC12Field int) (int, AltitudeUnit) {
	acQ := (aC12Field & 0x10) == 0x10
	var n int

	if acQ {
		// log.Printf(format, "Q Bit Set", strconv.FormatInt(int64(AC12Field), 2))
		// / N is the 11 bit integer resulting from the removal of bit Q at bit 4
		n = ((aC12Field & 0x0FE0) >> 1) | (aC12Field & 0x000F)
		// The final altitude is the resulting number multiplied by 25, minus 1000.

		return (n * 25) - 1000, AltitudeUnitFeet
	}
	// Make N a 13 bit Gillham coded altitude by inserting M=0 at bit 6
	n = ((aC12Field & 0x0FC0) << 1) | (aC12Field & 0x003F)
	// log.Printf(format, "Q Bit Clear", strconv.FormatInt(int64(n), 2))
	n = modeAToModeC(decodeGillhamCodedAltitude(n))
	if n < -12 {
		n = 0
	}
	return 100 * n, AltitudeUnitFeet
}

func decodeGillhamCodedAltitude(ID13Field int) int32 {
	var hexGillham int32
	// log.Printf(format, "Decoding ID13 Field", strconv.FormatInt(int64(ID13Field), 2))

	if 0 < (ID13Field & 0x1000) {
		hexGillham |= 0x0010
	} // Bit 12 = C1
	if 0 < (ID13Field & 0x0800) {
		hexGillham |= 0x1000
	} // Bit 11 = A1
	if 0 < (ID13Field & 0x0400) {
		hexGillham |= 0x0020
	} // Bit 10 = C2
	if 0 < (ID13Field & 0x0200) {
		hexGillham |= 0x2000
	} // Bit  9 = A2
	if 0 < (ID13Field & 0x0100) {
		hexGillham |= 0x0040
	} // Bit  8 = C4
	if 0 < (ID13Field & 0x0080) {
		hexGillham |= 0x4000
	} // Bit  7 = A4
	// if (ID13Field & 0x0040) {hexGillham |= 0x0800;} // Bit  6 = X  or M
	if 0 < (ID13Field & 0x0020) {
		hexGillham |= 0x0100
	} // Bit  5 = B1
	if 0 < (ID13Field & 0x0010) {
		hexGillham |= 0x0001
	} // Bit  4 = D1 or Q
	if 0 < (ID13Field & 0x0008) {
		hexGillham |= 0x0200
	} // Bit  3 = B2
	if 0 < (ID13Field & 0x0004) {
		hexGillham |= 0x0002
	} // Bit  2 = D2
	if 0 < (ID13Field & 0x0002) {
		hexGillham |= 0x0400
	} // Bit  1 = B4
	if 0 < (ID13Field & 0x0001) {
		hexGillham |= 0x0004
	} // Bit  0 = D4
	// log.Printf(format, "Decoded ID13 Field", strconv.FormatInt(int64(hexGillham), 2))

	return hexGillham
}

// ContainmentRadiusLimit calculates the horizontal containment radius limit in meters.
// Set NIC supplement A from Operational status Message for better precision.
// Otherwise, we'll be pessimistic.
// Note: For ADS-B versions < 2, this is inaccurate for NIC class 6, since there was
// no NIC supplement B in earlier versions.
func (f DFADSB) ContainmentRadiusLimit(nicSupplA bool) (float64, error) {
	var radius float64
	var err error
	if !f.Interrogatable {
		return radius, errors.New("ContainmentRadiusLimit Only valid for ADS-B Airborne Position Messages")
	}
	switch f.MessageType {
	case 0, 18, 22:
		err = errors.New("unknown containment radius")
	case 9, 20:
		radius = 7.5
	case 10, 21:
		radius = 25
	case 11:
		if nicSupplA {
			radius = 75
		} else {
			radius = 185.2
		}
	case 12:
		radius = 370.4
	case 13:
		switch {
		case f.NicSupplementB == 0:
			radius = 926
		case nicSupplA:
			radius = 1111.2
		default:
			radius = 555.6
		}
	case 14:
		radius = 1852
	case 15:
		radius = 3704
	case 16:
		if nicSupplA {
			radius = 7408
		} else {
			radius = 14816
		}
	case 17:
		radius = 37040
	default:
		radius = 0
	}

	return radius, err
}

func (f DFADSB) NavigationIntegrityCategory(nicSupplA bool) (byte, error) {
	var nic byte
	var err error
	if f.Interrogatable {
		return nic, errors.New("ContainmentRadiusLimit Only valid for ADS-B Airborne Position Messages")
	}
	switch f.MessageType {
	case 0, 18, 22:
		err = errors.New("unknown navigation integrity category")
	case 9, 20:
		nic = 11
	case 10:
	case 21:
		nic = 10
	case 11:
		if nicSupplA {
			nic = 9
		} else {
			nic = 8
		}
	case 12:
		nic = 7
	case 13:
		nic = 6
	case 14:
		nic = 5
	case 15:
		nic = 4
	case 16:
		if nicSupplA {
			nic = 3
		} else {
			nic = 2
		}
	case 17:
		nic = 1
	default:
		nic = 0
	}

	return nic, err
}
