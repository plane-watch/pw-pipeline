package main

import (
	"bytes"
	"errors"
	"math"
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
		/* Airborne velocity Message */
		ret.OnGround = false
		ret.ValidVertical = true

		ret.IntentChange = a.a&0x00_00_00_00_00_80_00_00 > 0 // (f.message[5] & 0x80) >> 7
		ret.IFRCapable = a.a&0x00_00_00_00_00_40_00_00 > 0   // (f.message[5] & 0x40) >> 6
		ret.ValidNacV = true
		ret.NavigationalAccuracyV = byte(a.a & 0x00_00_00_00_00_38_00_00 >> 19) // (f.message[5] & 0x38) >> 3

		switch ret.MessageSubType {
		case 1, 2:
			// heading type is ground track
			ret.Velocity, ret.SuperSonic, ret.ValidVelocity, ret.Heading, ret.ValidHeading = calcAirborneSpeedMT12(
				ret.MessageSubType,
				byte(a.a&0x00_00_00_00_00_04_00_00>>18),
				byte(a.a&0x00_00_00_00_00_00_00_80>>7),
				int(a.a&0x00_00_00_00_00_03_FF_00>>8), //nolint:gosec
				int(a.a&0x00_00_00_00_00_00_00_7F<<3|a.b&0xE0_00_00_00_00_00_00_00>>61),
			)
		case 3, 4:
			// heading type is magnetic or true
			ret.Velocity, ret.SuperSonic, ret.ValidVelocity, ret.Heading, ret.ValidHeading = calcAirborneSpeedMT34(
				ret.MessageSubType,
				int(a.a&0x00_00_00_00_00_00_00_7F<<3|a.b&0xE0_00_00_00_00_00_00_00>>61), //nolint:gosec
				a.a&0x00_00_00_00_00_04_00_00 != 0,
				float64(a.a&0x00_00_00_00_00_03_F8_00>>11),
			)

			ret.TrueAirSpeed = a.a&0x00_00_00_00_00_00_00_80 > 0 // false == indicated air speed
		}

		ret.VerticalRate, ret.VerticalRateSource, ret.ValidVertical = calcAirborneVerticalRate(
			byte(a.b&0x10_00_00_00_00_00_00_00>>60), // Vertical Rate Source
			byte(a.b&0x08_00_00_00_00_00_00_00>>59), // Vertical Rate Direction (sign +/-)
			int(a.b&0x07_FC_00_00_00_00_00_00>>50),  //nolint:gosec,gosec Vertical rate amount
		)

		ret.HeightAboveEllipsoid, ret.ValidHAE = calcHAE(
			byte(a.b&0x00_00_80_00_00_00_00_00>>47), // HAE Direction bit
			byte(a.b&0x00_00_7F_00_00_00_00_00>>40), // HAE Value, rest of byte 10
		)

	case 23:
		switch ret.MessageSubType {
		case 0: // test message
		case 1, 2, 3, 4, 5, 6: // Reserved
		case 7: // Allocated for national use
			// TEST MESSAGE with  squawk - decode it!
			ret.Squawk = decodeSquawkIdentity(
				uint32(a.a&0x00_00_00_00_00_FF_00_00>>16), //nolint:gosec
				uint32(a.a&0x00_00_00_00_00_00_FF_00>>8),  //nolint:gosec
			)
			ret.ValidSquawk = true
		}

	case 24:
	// Surface System Status Messages
	// NoOp
	// subType=1 is for Multilateration System Status (Allocated for national use)
	// this is a per system manufacturer message
	case 25, 26:
		// RESERVED
	case 27:
	case 28:
		// Aircraft Status
		switch ret.MessageSubType {
		case 0: // reserved
		case 1: // Emergency/priority status (§B.2.3.8)
			// EMERGENCY (or priority), EMERGENCY, THERE'S AN EMERGENCY GOING ON
			// ME bits 9,10,11 contain the emergency code (1-8 are TC/SUB) then EID bits (this)
			// ME starts at byte 5 (TC/SUB), EID is first 3 bits of byte 6
			ret.Emergency = byte(a.a & 0x00_00_00_00_00_E0_00_00 >> 13)
			ret.ValidEmergency = ret.Emergency > 0

			ret.Squawk = decodeSquawkIdentity(
				uint32(a.a&0x00_00_00_00_00_FF_00_00>>16), //nolint:gosec
				uint32(a.a&0x00_00_00_00_00_00_FF_00>>8),  //nolint:gosec
			)
			ret.ValidSquawk = true

			// can get the Mode A Address too
			// mode_a_code = (short) (msg[2]|((msg[1]&0x1F)<<8));
		case 2:
		// ACAS RA broadcast
		case 3, 4, 5, 6, 7: // RESERVED
		}
	case 29:
		// only 2 bits of message subtype
		ret.MessageSubType = byte((a.a & 0x00_00_00_00_06_00_00_00) >> 25)
		ret.ValidADSBVersion = true

		// Aircraft Target Status
		switch ret.MessageSubType {
		case 0:
			ret.ADSBVersion = DO260A
			// DO-260A
			ret.TargetD0260A = DO260ATargetInfo(a.a&0x00_00_00_00_FF_FF_FF_FF<<24 | a.b&0xFF_FF_FF_00_00_00_00_00>>40)

			if a.a&0x00_00_00_00_40_00_00_00 == 0 { // backwards compat bit set, we can proceed
				// target state and status v1

				ret.Emergency = ret.TargetD0260A.Emergency()
				ret.ValidEmergency = ret.Emergency > 0
			}
		case 1:
			// target state and status v2
			ret.ADSBVersion = DO260B
			ret.TargetD0260B = DO260BTargetInfo(a.a&0x00_00_00_00_FF_FF_FF_FF<<24 | a.b&0xFF_FF_FF_00_00_00_00_00>>40)
		case 2:
			// ?? Unknown
		}
	case 30:
		//  Aircraft Operational Coordination
	case 31:
		// Operational Status
		switch ret.MessageSubType {
		case 1, 2:
			ret.OperationalInfo = OperationalStatusInfo(a.a&0x00_00_00_00_FF_FF_FF_FF<<24 | a.b&0xFF_FF_FF_00_00_00_00_00>>40)
		}
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
	n = modeAToModeC(decodeAC13Field(int32(n))) //nolint:gosec
	if n < -12 {
		n = 0
	}
	return 100 * n, AltitudeUnitFeet
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

func calcAirborneSpeedMT12(
	messageSubType byte,
	eastWestDirection, northSouthDirection byte,
	eastWestVelocity, northSouthVelocity int,
) (velocity float64, superSonic, validVelocity bool, heading float64, validHeading bool) {
	if messageSubType != 1 && messageSubType != 2 {
		return
	}
	// speed over Ground Message
	/* Compute velocity and angle from the two speed components. */

	if messageSubType == 2 {
		// supersonic - unit is 4 knots
		eastWestVelocity <<= 2
		northSouthVelocity <<= 2
		superSonic = true
	}

	velocity = math.Sqrt(float64((northSouthVelocity * northSouthVelocity) + (eastWestVelocity * eastWestVelocity)))
	validVelocity = true

	if velocity != 0 {
		eastWestVelocity--
		northSouthVelocity--
		if eastWestDirection != 0 {
			// GO WEST! (0=east, 1=west)
			eastWestVelocity *= -1
		}
		if northSouthDirection != 0 {
			// Going Down South! (0=north, 1=south)
			northSouthVelocity *= -1
		}
		heading = math.Atan2(float64(eastWestVelocity), float64(northSouthVelocity))
		/* Convert to degrees. */
		heading = heading * 360 / (math.Pi * 2)
		/* We don't want negative values but a 0-360 scale. */
		if heading < 0 {
			heading += 360
		}
		validHeading = true
	} else {
		heading = 0
	}

	// limit precision to 2 places
	heading = math.Round(heading*100) / 100
	velocity = math.Round(velocity*100) / 100

	return
}

func calcAirborneSpeedMT34(
	messageSubType byte,
	airspeed int,
	hasHeading bool,
	headingField float64,
) (velocity float64, superSonic, validVelocity bool, heading float64, validHeading bool) {
	// Air Speed -- ground speed not available

	if messageSubType != 3 && messageSubType != 4 {
		return
	}

	if airspeed > 0 {
		airspeed--
		if messageSubType == 4 {
			// If (supersonic) unit is 4 kts
			superSonic = true
			airspeed <<= 2
		}
		velocity = float64(airspeed)
		validVelocity = true
		velocity = math.Round(velocity*100) / 100
	}

	if hasHeading {
		heading = (360.0 / 128.0) * headingField
		validHeading = true
		heading = math.Round(heading*100) / 100
	}

	return
}

func calcAirborneVerticalRate(
	verticalRateSource, verticalRateSign byte,
	verticalRateIn int,
) (verticalRate float64, vrs VerticalRateSource, validVerticalRate bool) {
	vrs = VerticalRateSource(verticalRateSource)

	// var verticalRateSign = int((f.message[8] & 0x8) >> 3)
	verticalRate = float64(verticalRateIn)
	if verticalRate != 0 {
		verticalRate--
		if verticalRateSign != 0 {
			verticalRate = 0 - verticalRate
		}
		verticalRate *= 64
		validVerticalRate = true
	}
	return
}

func calcHAE(haeDirection byte, haeValue byte) (heightAboveEllipsoidDelta int, validHAE bool) {
	if haeDirection+haeValue > 0 {
		validHAE = true
		var multiplier = -25
		if haeDirection == 0 {
			multiplier = 25
		}
		heightAboveEllipsoidDelta = multiplier * int(haeValue-1)
	}
	return
}

// decodeSquawkIdentity takes the index of the 2 bytes needed to decode our identity
// we require the identity to be in the last 5 bits of the first byte and all of the second byte
// these bits should contain the identity 0b0001_1111, 0b1111_1111
func decodeSquawkIdentity(msg2, msg3 uint32) uint32 {
	var a, b, c, d uint32

	/* In the squawk (identity) field bits are interleaved like that
	* (message bit 20 to bit 32 - 1 based):
	*
	* C1-A1-C2-A2-C4-A4-ZERO-B1-D1-B2-D2-B4-D4
	*
	* So every group of three bits A, B, C, D represent an integer
	* from 0 to 7.
	*
	* The actual meaning is just 4 octal numbers, but we convert it
	* into a base ten number that happens to represent the four
	* octal numbers.
	*
	* For more info: http://en.wikipedia.org/wiki/Gillham_code */
	a = ((msg3 & 0x80) >> 5) | ((msg2 & 0x02) >> 0) | ((msg2 & 0x08) >> 3)
	b = ((msg3 & 0x02) << 1) | ((msg3 & 0x08) >> 2) | ((msg3 & 0x20) >> 5)
	c = ((msg2 & 0x01) << 2) | ((msg2 & 0x04) >> 1) | ((msg2 & 0x10) >> 4)
	d = ((msg3 & 0x01) << 2) | ((msg3 & 0x04) >> 1) | ((msg3 & 0x10) >> 4)
	return a*1000 + b*100 + c*10 + d
}
