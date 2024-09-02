package main

import (
	"errors"
)

var (
	ErrInvalidAltitude            = errors.New("invalid altitude decoded")
	ErrInvalidFlightStatus        = errors.New("invalid flight status")
	ErrChecksumFailed             = errors.New("invalid checksum")
	ErrImpossibleOnGroundAltitude = errors.New("too high to be on the ground")
)

// DownlinkFormat Determines the Downlink format of the AVR Frame
func (a AvrFrame) DownlinkFormat() byte {
	// DF24 is a little different. if the first two bits of the message are set, it is a DF24 message
	if a.a&0xc0_00_00_00_00_00_00_00 == 0xc0_00_00_00_00_00_00_00 {
		return 24
	}
	// get the down link format (DF) - first 5 bits
	return byte(a.a >> 59)
}

func (a AvrFrame) ChecksumValid() bool {
	switch a.df {
	case 0, 4, 5, 16, 20, 21:
		// attempt to get the ICAO from the AP Field
		// AP is CRC overlaid with the ICAO

		// return icao == (a.checkSum ^ a.messageCrc())
		return false
	case 11, 17, 18:
		return a.checkSum^a.messageCrc() == 0
	default:
		return false
	}
}

// messageCrc is the last 3 bytes (24 bits) of the payload
func (a AvrFrame) messageCrc() uint64 {
	if a.len == 14 {
		return a.b >> 16 & 0x00FFFFFF
	}
	return a.a >> 8 & 0x00FFFFFF
}

// Checksum is the calculated checksum only, to get the ICAO you need to xor the message CRC
func (a AvrFrame) Checksum() uint64 {
	var b, index uint64

	var checkSum uint64
	maxShift := -1
	if a.len == 7 {
		maxShift = 24
	}
	for shift := 56; shift > maxShift; shift -= 8 {
		b = (a.a >> shift) & 0xFF // get the byte we want to deal with
		index = b ^ ((checkSum & 0xff_00_00) >> 16)
		checkSum = ((checkSum << 8) ^ modesChecksumTable[index]) & 0xff_ff_ff
	}
	if a.len == 14 {
		for shift := 56; shift > 32; shift -= 8 {
			b = (a.b >> shift) & 0xFF // get the byte we want to deal with
			index = b ^ ((checkSum & 0xff_00_00) >> 16)
			checkSum = ((checkSum << 8) ^ modesChecksumTable[index]) & 0xff_ff_ff
		}
	}

	return checkSum
}

// ICAO gets the ICAO address from the message
func (a AvrFrame) ICAO() int {
	switch a.df {
	case 0, 4, 5, 16, 20, 21:
		// attempt to get the ICAO from the AP Field
		// AP is CRC overlaid with the ICAO

		return int(a.checkSum ^ a.messageCrc()) //nolint:gosec
	case 11, 17, 18:
		return int(a.a >> 32 & 0x00_FF_FF_FF) //nolint:gosec
	default:
		return 0
	}
}

// bits 20-32 are the altitude
// the 1 bits are AC13 field
// 00000000 00000000 00011111 1M1Q1111 00000000
func (a AvrFrame) decode13bitAltitudeCode() (int, AltitudeUnit, error) {
	// ac := uint32(f.message[2]&0x1F)<<8 | uint32(f.message[3])
	ac := a.a & 0x00_00_1F_FF_00_00_00_00 >> 32

	// altitude type, M Bit
	acM := ac&0x40 == 0x40 // bit 26 of message. 0 == feet, 1 = metres
	// resolution Q bit
	acQ := ac&0x10 == 0x10 // bit 28 of message. 1 = 25 ft encoding, 0 = Gillham Mode C encoding

	// make sure all the bits are good
	var unit AltitudeUnit
	var altitude int
	var err error
	switch {
	case !acM && acQ:
		// 25 ft increments
		unit = AltitudeUnitFeet
		/* `n` is the 11 bit integer resulting from the removal of bit Q and M */
		var n = int(((ac & 0x1F80) >> 2) | ((ac & 0x0020) >> 1) | (ac & 0x000F))
		/* The final altitude is due to the resulting number multiplied by 25, minus 1000. */
		altitude = (n * 25) - 1000

	case !acM && !acQ:
		// altitude reported in feet, 100ft increments
		unit = AltitudeUnitFeet
		altitude = modeAToModeC(decodeAC13Field(int32(ac)))
		if altitude < -12 {
			altitude = 0
			err = ErrInvalidAltitude
		}
		altitude *= 100

	case acM:
		// we are dealing with metres
		unit = AltitudeUnitMetre
		altitude = 0
		err = errors.New("TODO: implement decoding of altitude in metres")
		// TODO: Implement decoding Metres
	}

	return altitude, unit, err
}

// decodeAC13Field decodes the altitude (not the squawk)
func decodeAC13Field(ID13Field int32) int32 {
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

// decodeID13Field decodes our Squawk or altitude code, if the zero bit is set we error
//
//	In the squawk (identity) field bits are interleaved like this
//	(message bit 20 to bit 32 - 1 based):
//
//	C1-A1-C2-A2-C4-A4-ZERO-B1-D1-B2-D2-B4-D4
//	               ^ byte
//	So every group of three bits A, B, C, D represent an integer
//	from 0 to 7.
//
//	      0
//	0010100001011
//	     ^ byte
//
//	The actual meaning is just 4 octal numbers, but we convert it
//	into a base ten number that happens to represent the four
//	octal numbers.
//
//	For more info: http://en.wikipedia.org/wiki/Gillham_code */
func decodeID13Field(id13 int16) (int16, error) {
	if id13&0x40 != 0 {
		return 0, errors.New("invalid squawk (zero bit set)")
	}
	var a, b, c, d int16
	a = ((id13 & 0x00_80) >> 5) /*A4*/ | ((id13 & 0x02_00) >> 8) /*A2*/ | ((id13 & 0x08_00) >> 11) /*A1*/
	b = ((id13 & 0x00_02) << 1) /*B4*/ | ((id13 & 0x00_08) >> 2) /*B2*/ | ((id13 & 0x00_20) >> 5)  /*B1*/
	c = ((id13 & 0x01_00) >> 6) /*C4*/ | ((id13 & 0x04_00) >> 9) /*C2*/ | ((id13 & 0x10_00) >> 12) /*C1*/
	d = ((id13 & 0x00_01) << 2) /*D4*/ | ((id13 & 0x00_04) >> 1) /*D2*/ | ((id13 & 0x00_10) >> 4)  /*D1*/

	return a*1000 + b*100 + c*10 + d, nil
}

// Mode A to Mode C Height/altitude
func modeAToModeC(modeA int32) int {
	var OneHundreds, FiveHundreds int
	// log.Printf(format, "Mode A -> C", strconv.FormatInt(int64(modeA), 2))
	// log.Printf(format, "Mask 1", strconv.FormatInt(int64(0x0FFF8889), 2))
	// log.Printf(format, "Mask 2", strconv.FormatInt(int64(0x000000F0), 2))

	if (modeA&0x0FFF8889) > 0 || ((modeA & 0x000000F0) == 0) {
		// check zero bits are zero, D1 set is illegal || C1,,C4 cannot be Zero
		return -9999
	}

	if (modeA & 0x0010) > 0 {
		OneHundreds ^= 0x007
	} // C1
	if (modeA & 0x0020) > 0 {
		OneHundreds ^= 0x003
	} // C2
	if (modeA & 0x0040) > 0 {
		OneHundreds ^= 0x001
	} // C4

	// Remove 7s from OneHundreds (Make 7->5, snd 5->7).
	if (OneHundreds & 5) == 5 {
		OneHundreds ^= 2
	}

	// Check for invalid codes, only 1 to 5 are valid
	if OneHundreds > 5 {
		return -9999
	}

	// if (modeA & 0x0001) {FiveHundreds ^= 0x1FF;} // D1 never used for altitude
	if (modeA & 0x0002) > 0 {
		FiveHundreds ^= 0x0FF
	} // D2
	if (modeA & 0x0004) > 0 {
		FiveHundreds ^= 0x07F
	} // D4

	if (modeA & 0x1000) > 0 {
		FiveHundreds ^= 0x03F
	} // A1
	if (modeA & 0x2000) > 0 {
		FiveHundreds ^= 0x01F
	} // A2
	if (modeA & 0x4000) > 0 {
		FiveHundreds ^= 0x00F
	} // A4

	if (modeA & 0x0100) > 0 {
		FiveHundreds ^= 0x007
	} // B1
	if (modeA & 0x0200) > 0 {
		FiveHundreds ^= 0x003
	} // B2
	if (modeA & 0x0400) > 0 {
		FiveHundreds ^= 0x001
	} // B4

	// Correct order of OneHundreds.
	if (FiveHundreds & 1) > 0 {
		OneHundreds = 6 - OneHundreds
	}

	result := (FiveHundreds * 5) + OneHundreds - 13
	// log.Printf(format, "Converted", strconv.FormatInt(int64(result), 2))
	return result
}

// Flight status (FS): 3 bits, shows status of alert, special position pulse (SPI, in Mode A only) and aircraft status (airborne or on-ground). The field is interpreted as:
//
//	000: no alert, no SPI, aircraft is airborne
//	001: no alert, no SPI, aircraft is on-ground
//	010: alert, no SPI, aircraft is airborne
//	011: alert, no SPI, aircraft is on-ground
//	100: alert, SPI, aircraft is airborne or on-ground
//	101: no alert, SPI, aircraft is airborne or on-ground
//	110: reserved
//	111: not assigned

// isFlightStatusOnGround determines if the current flight status is airborne or not
func isFlightStatusOnGround(fs byte) bool {
	return fs == 1 || fs == 3
}

// isFlightStatusAlert determines if we are alerting
func isFlightStatusAlert(fs byte) bool {
	return fs == 0b010 || fs == 0b011 || fs == 0b100
}

// isFlightStatusValid determines if we have a valid value for our flight status
func isFlightStatusValid(fs byte) bool {
	return fs < 0b110
}
