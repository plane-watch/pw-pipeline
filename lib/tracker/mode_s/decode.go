package mode_s

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	modesLongMsgBytes  = 14
	modesShortMsgBytes = 7
	modesLongMsgBits   = modesLongMsgBytes * 8
	modesShortMsgBits  = modesShortMsgBytes * 8
)

type (
	ReceivedFrame struct {
		Frame string
		Time  time.Time
	}
)

var (
	ErrNoOp                      = errors.New("frame is NoOp")
	ErrInvalidIcao               = errors.New("invalid ICAO address")
	ErrInvalidChecksum           = errors.New("invalid checksum")
	ErrReplyInformation          = errors.New("reply information is set to value that is not assigned")
	ErrInvalidFlightStatus       = errors.New("invalid flight status")
	ErrUnassignedDownloadRequest = errors.New("unassigned downlink request")
	ErrSquawkInvalid             = errors.New("invalid squawk identity")
	ErrUnknownD18Capability      = errors.New("unknown DF18 Capability")
)

func DecodeString(rawFrame string, t time.Time) (*Frame, error) {
	frame := NewFrame(rawFrame, t)
	if nil == frame {
		return nil, errors.New("unable to parse frame")
	}
	err := frame.Decode()
	if nil != err {
		return frame, err
	}
	return frame, nil
}

func NewFrame(rawFrame string, t time.Time) *Frame {
	f := Frame{
		decodeLock: &sync.Mutex{},
		full:       rawFrame,
		timeStamp:  t,
	}

	return &f
}

func NewFrameFromBytes(beastTicks uint64, message []byte, t time.Time) Frame {
	return Frame{
		decodeLock: &sync.Mutex{},
		full:       "",
		mode:       "MLAT",
		beastTicks: beastTicks,
		timeStamp:  t,
		message:    message,
		fromBytes:  true,
	}
}

// Decode will decode the Mode-S frame, if not already decoded
func (f *Frame) Decode() error {
	if nil == f {
		return nil
	}
	f.decodeLock.Lock()
	defer f.decodeLock.Unlock()
	if f.hasDecoded {
		return nil
	}
	if !f.fromBytes {
		if err := f.parseIntoRaw(); nil != err {
			return err
		}

		if f.isNoOp() {
			return ErrNoOp
		}
	}

	err := f.parse()
	if nil == err {
		// successful decode
		f.hasDecoded = true
	}
	return err
}

func (f *Frame) parse() error {
	var err error

	f.decodeDownLinkFormat()

	// now see if the message we got matches up with the DF format we decoded
	if int(f.getMessageLengthBytes()) != len(f.message) {
		return fmt.Errorf("cannot parse AVR Frame (DF%d) (%X). Incorrect length %d != %d", f.downLinkFormat, f.message, f.getMessageLengthBytes(), len(f.message))
	}

	err = f.checkCrc()
	if nil != err {
		return err
	}

	// decode the specific DF type
	switch f.downLinkFormat {
	case 0: // Airborne position, baro altitude only
		err = errors.Join(
			f.decodeICAO(),
			f.decodeVerticalStatus(),
			f.decodeCrossLinkCapability(),
			f.decodeSensitivityLevel(),
			f.decodeReplyInformation(),
			f.decode13bitAltitudeCode(),
		)
	case 4:
		err = errors.Join(
			f.decodeICAO(),
			f.decodeFlightStatus(),
			f.decodeDownLinkRequest(),
			f.decodeUtilityMessage(),
			f.decode13bitAltitudeCode(),
		)
	case 5: // DF_5
		err = errors.Join(
			f.decodeICAO(),
			f.decodeFlightStatus(),
			f.decodeDownLinkRequest(),
			f.decodeUtilityMessage(),
			f.decodeSquawkIdentity(2, 3), // gillham encoded squawk
		)
	case 11: // DF_11
		err = errors.Join(
			f.decodeICAO(),
			f.decodeCapability(),
		)
	case 16: // DF_16
		err = errors.Join(
			f.decodeICAO(),
			f.decodeVerticalStatus(),
			f.decode13bitAltitudeCode(),
			f.decodeReplyInformation(),
			f.decodeSensitivityLevel(),
		)
	case 17: // DF_17
		err = errors.Join(
			f.decodeICAO(),
			f.decodeCapability(),
			f.decodeAdsb(),
		)
	case 18: // DF_18
		switch f.ca {
		case 0:
			err = errors.Join(
				f.decodeCapability(), // control field
				f.decodeICAO(),
				f.decodeAdsb(),
			)
		default:
			err = ErrUnknownD18Capability
		}
	case 20: // DF_20
		err = errors.Join(
			f.decodeICAO(),
			f.decodeFlightStatus(),
			f.decode13bitAltitudeCode(),
			f.decodeCommB(),
		)
	case 21: // DF_21
		err = errors.Join(
			f.decodeICAO(),
			f.decodeFlightStatus(),
			f.decodeSquawkIdentity(2, 3), // gillham encoded squawk
			f.decodeCommB(),
		)
	}

	return err
}

func (f *Frame) parseIntoRaw() error {
	if len(f.raw) > 0 {
		// prevent double parse
		return nil
	}
	encodedFrame := strings.TrimFunc(f.full, func(r rune) bool {
		return unicode.IsSpace(r) || r == ';'
	})

	// let's ensure that we have some correct data...
	if encodedFrame == "" {
		return errors.New("cannot decode empty string")
	}

	if len(encodedFrame) < 14 {
		return fmt.Errorf("frame (%s) too short to be a Mode S frame", f.full)
	}

	// determine what type of frame we are dealing with
	if encodedFrame[0] == '@' {
		// Beast Timestamp+AVR format
		f.mode = "MLAT"
	} else {
		f.mode = "NORMAL"
	}

	// ensure we have a timestamp
	frameStart := 0
	if f.mode == "MLAT" {
		frameStart = 13
		// try and use the provided timestamp
		f.beastTimeStamp = encodedFrame[1:12]
		if err := f.parseBeastTimeStamp(); nil != err {
			return err
		}
	} else if encodedFrame[0:1] == "*" {
		frameStart = 1
	}
	f.raw = encodedFrame[frameStart:]

	if len(f.raw) == 0 {
		return errors.New("failed to decode message raw")
	}
	return f.parseRawToMessage()
}

func (f *Frame) decodeDownLinkFormat() {
	// DF24 is a little different. if the first two bits of the message are set, it is a DF24 message
	if f.message[0]&0xc0 == 0xc0 {
		f.downLinkFormat = 24
	} else {
		// get the down link format (DF) - first 5 bits
		f.downLinkFormat = f.message[0] >> 3
	}
}

func (f *Frame) parseRadarcapeTimeStamp() {
	// The same 48bites are used in GPS format (from radarcape)
	//   18 bit second of day, 30bit nanosecond
	// TODO: Decode Radarcape Ticks
}

func (f *Frame) parseBeastTimeStamp() error {
	if f.beastTimeStamp == "" || "00000000000" == f.beastTimeStamp {
		return nil
	}
	// MLAT timestamps from Beast AVR are dependent on when the device started ( 500ns intervals / 12mhz)
	// calculated from power on.
	// 48 bits = 2.81474976711e+14
	// max: 2,000,000 seconds
	// Wrinkle: The same 48bites are used in GPS format (from radarcape)
	//   18 bit second of day, 30bit nanosecond
	var err error
	f.beastTicks, err = strconv.ParseUint(f.beastTimeStamp, 16, 64)
	if err != nil {
		return fmt.Errorf("failed to decode beast avr timestamp: %s", err)
	}
	f.beastTicksNs = f.beastTicks * 500
	return nil
}

// BeastTicksNs returns a time.Duration timestamp for this frame
func (f *Frame) BeastTicksNs() time.Duration {
	return time.Duration(f.beastTicksNs)
}

func (f *Frame) TimeStamp() time.Time {
	return f.timeStamp
}

func (f *Frame) SetTimeStamp(t time.Time) {
	f.timeStamp = t
}

// call after frame.raw is set. does the preparing
func (f *Frame) parseRawToMessage() error {
	if nil != f.message {
		// prevent overwriting if called twice
		return nil
	}
	frameLen := len(f.raw)

	// cheap bitwise even number check!
	if (frameLen & 1) != 0 {
		return fmt.Errorf("frame is an odd length (%d), cannot decode unless length is even", frameLen)
	}

	messageLen := frameLen / 2

	if !(messageLen == modesShortMsgBytes || messageLen == modesLongMsgBytes) {
		return fmt.Errorf("frame is incorrect length. %d != 7 or 14", messageLen)
	}

	f.message = make([]byte, messageLen)
	// the rest of the frame is encoded in 2 char hex values

	index := 0
	for i := 0; i < len(f.raw); i += 2 {
		pair := f.raw[i : i+2]
		myInt, err := strconv.ParseUint(pair, 16, 8)

		if err != nil {
			return err
		}
		f.message[index] = byte(myInt)
		index++
	}
	if len(f.message) == 0 {
		return errors.New("failed to decode message in bytes")
	}
	return nil
}

func (f *Frame) decodeCapability() error {
	f.ca = f.message[0] & 7

	switch f.ca {
	case 4:
		f.validVerticalStatus = true
		f.onGround = true
	case 5:
		f.validVerticalStatus = true
		f.onGround = false
	default:
	}

	return nil
}
func (f *Frame) decodeCrossLinkCapability() error {
	f.cc = f.message[0] & 0x2 >> 1

	return nil
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
func (f *Frame) decodeFlightStatus() error {
	// first 5 bits are the downlink format
	// bits 5,6,7 are the flight status
	// https://mode-s.org/decode/content/mode-s/3-surveillance.html
	f.fs = f.message[0] & 0x7
	switch f.fs {
	case 0, 2:
		f.validVerticalStatus = true
		f.onGround = false
	case 1, 3:
		f.validVerticalStatus = true
		f.onGround = true
	case 4, 5:
		// special pos
		f.validVerticalStatus = false
		f.onGround = false // assume in the air
		if f.fs <= 7 {
			f.special += flightStatusTable[f.fs]
		} else {
			f.special += fmt.Sprintf("Unknown Flight Status: %d", f.fs)
		}
	case 6, 7:
		return ErrInvalidFlightStatus

	}
	if f.fs == 2 || f.fs == 3 || f.fs == 4 {
		// ALERT!
		f.alert = true
	}

	return nil
}

// VS == Vertical status
func (f *Frame) decodeVerticalStatus() error {
	f.vs = f.message[0] & 4 >> 2
	f.onGround = f.vs != 0
	f.validVerticalStatus = true
	return nil
}

// bits 13,14,15 and 16 make up the RI field
func (f *Frame) decodeReplyInformation() error {
	f.ri = (f.message[1]&7)<<1 | (f.message[2]&0x80)>>7
	if f.ri == 1 || f.ri == 5 || f.ri == 6 || f.ri == 7 || f.ri == 15 {
		return ErrReplyInformation
	}
	return nil
}
func (f *Frame) decodeSensitivityLevel() error {
	f.sl = (f.message[1] & 0xe0) >> 5
	return nil
}

// decodeDownLinkRequest is 5 bits
func (f *Frame) decodeDownLinkRequest() error {
	f.dr = (f.message[1] & 0xf8) >> 3

	// value 8 -- 14 is not assigned
	if f.dr >= 8 && f.dr <= 14 {
		return ErrUnassignedDownloadRequest
	}

	return nil
}

// decodeUtilityMessage decodes our utility message info
// Utility message (UM): 6 bits, contains transponder communication status information.
//
//	IIS: The first 4 bits of UM indicate the interrogator identifier code.
//
//	IDS: The last 2 bits indicate the type of reservation made by the interrogator.
//	    00: no information
//	    01: IIS contains Comm-B interrogator identifier code
//	    10: IIS contains Comm-C interrogator identifier code
//	    11: IIS contains Comm-D interrogator identifier code
func (f *Frame) decodeUtilityMessage() error {
	// 0000_0111 << 3 | 1110_0000 >> 5
	f.um = (f.message[1]&0x7)<<3 | (f.message[2]&0xe0)>>5

	return nil
}

// Determines the ICAO address from bytes 2,3 and 4
func (f *Frame) decodeICAO() error {
	switch f.downLinkFormat {
	case 0, 4, 5, 16, 20, 21:
		// attempt to get the ICAO from the AP Field
		// AP is CRC overlaid with the ICAO
		f.icao = f.decodeModeSChecksumAddr()

	case 1, 2, 3, 6, 7, 8, 9, 10, 12, 13, 14, 15, 19, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31:
		f.icao = 0
		return ErrInvalidIcao
	case 11, 17, 18:
		a := uint32(f.message[1])
		b := uint32(f.message[2])
		c := uint32(f.message[3])
		f.icao = a<<16 | b<<8 | c
	}

	return nil
}

// decodeSquawkIdentity takes the 2 bytes needed to decode our identity
// we require the identity to be in the last 5 bits of the first byte and all of the second byte
// these bits should contain the identity 0b0001_1111, 0b1111_1111
func (f *Frame) decodeSquawkIdentity(byte1, byte2 int) error {
	var a, b, c, d uint32
	var msg2, msg3 uint32

	msg2 = uint32(f.message[byte1])
	msg3 = uint32(f.message[byte2])

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

	if byte2&0b0100_0000 == 1 {
		return ErrSquawkInvalid
	}

	a = ((msg3 & 0x80) >> 5) | ((msg2 & 0x02) >> 0) | ((msg2 & 0x08) >> 3)
	b = ((msg3 & 0x02) << 1) | ((msg3 & 0x08) >> 2) | ((msg3 & 0x20) >> 5)
	c = ((msg2 & 0x01) << 2) | ((msg2 & 0x04) >> 1) | ((msg2 & 0x10) >> 4)
	d = ((msg3 & 0x01) << 2) | ((msg3 & 0x04) >> 1) | ((msg3 & 0x10) >> 4)
	f.identity = a*1000 + b*100 + c*10 + d

	return nil
}

// bits 20-32 are the altitude
// the 1 bits are AC13 field
// 00000000 00000000 00011111 1M1Q1111 00000000
func (f *Frame) decode13bitAltitudeCode() error {
	f.ac = uint32(f.message[2]&0x1F)<<8 | uint32(f.message[3])

	// altitude type, M Bit
	f.acM = f.ac&0x40 == 0x40 // bit 26 of message. 0 == feet, 1 = metres
	// resolution Q bit
	f.acQ = f.ac&0x10 == 0x10 // bit 28 of message. 1 = 25 ft encoding, 0 = Gillham Mode C encoding

	// make sure all the bits are good

	switch {
	case !f.acM && f.acQ:
		// 25 ft increments
		f.unit = modesUnitFeet
		/* `n` is the 11 bit integer resulting from the removal of bit Q and M */
		var n = int32(((f.ac & 0x1F80) >> 2) | ((f.ac & 0x0020) >> 1) | (f.ac & 0x000F))
		/* The final altitude is due to the resulting number multiplied by 25, minus 1000. */
		f.altitude = (n * 25) - 1000
		f.validAltitude = true

	case !f.acM && !f.acQ:
		// altitude reported in feet, 100ft increments
		f.unit = modesUnitFeet
		f.altitude = modeAToModeC(decodeID13Field(int32(f.ac)))
		f.validAltitude = f.altitude >= -12
		if !f.validAltitude {
			f.altitude = 0
		}
		f.altitude *= 100

	case f.acM:
		// we are dealing with metres
		f.unit = modesUnitMetres
		f.validAltitude = false
		//TODO: Implement decoding Metres
	}

	return nil
}

func (f *Frame) getMessageLengthBits() uint32 {
	if f.downLinkFormat&0x10 != 0 {
		if len(f.message) == 14 {
			return modesShortMsgBits
		}
		return modesLongMsgBits
	}
	return modesShortMsgBits
}

func (f *Frame) getMessageLengthBytes() uint32 {
	if f.downLinkFormat&0x10 != 0 {
		return modesLongMsgBytes
	}
	return modesShortMsgBytes
}

func (f *Frame) decodeFlightNumber() {
	f.flight = decodeFlightNumber(f.message[5:11])
}

func decodeFlightNumber(b []byte) []byte {
	if len(b) != 6 {
		panic(fmt.Sprintf("attempting to decode a flight number/callsign with too many bytes (%d)", len(b)))
	}

	// Decode into a stack buffer first; only allocate when returning a valid value.
	var callsign [8]byte
	callsign[0] = aisCharset[b[0]>>2] // 6 bits
	callsign[1] = aisCharset[((b[0]&3)<<4)|(b[1]>>4)]
	callsign[2] = aisCharset[((b[1]&15)<<2)|(b[2]>>6)]
	callsign[3] = aisCharset[b[2]&63]
	callsign[4] = aisCharset[b[3]>>2]
	callsign[5] = aisCharset[((b[3]&3)<<4)|(b[4]>>4)]
	callsign[6] = aisCharset[((b[4]&15)<<2)|(b[5]>>6)]
	callsign[7] = aisCharset[b[5]&63]

	// Validate callsign characters - matches readsb validation logic
	// Valid characters: A-Z, -./ (ASCII 45-47), 0-9, space, @
	// Reference: readsb mode_s.c:824-840
	callsignValid := true
	for i := 0; i < len(callsign); i++ {
		c := callsign[i]
		if (c >= 'A' && c <= 'Z') ||
			(c >= '-' && c <= '9') || // ASCII 45-57: -./ and 0-9
			c == ' ' ||
			c == '@' {
			// valid character
		} else {
			// Invalid character - reject entire callsign
			callsignValid = false
			break
		}
	}

	if !callsignValid {
		return nil
	}

	// Reject common invalid patterns, because occasionally misconfigured aircraft send bogus call signs.
	// For example-- we've seen things like: A90004A0200000000000007D8DB4 which is basically all nulls.
	if callsign == [8]byte{'@', '@', '@', '@', '@', '@', '@', '@'} ||
		callsign == [8]byte{' ', ' ', ' ', ' ', ' ', ' ', ' ', ' '} ||
		callsign == [8]byte{'-', '-', '-', '-', '-', '-', '-', '-'} {
		return nil
	}

	decoded := make([]byte, len(callsign))
	copy(decoded, callsign[:])
	return decoded
}
