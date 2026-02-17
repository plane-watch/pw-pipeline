package mode_s

import (
	"errors"
	"fmt"
	"github.com/rs/zerolog/log"
)

const (
	BdsESAirbornePosition  = "0.5"
	BdsESSurfacePosition   = "0.6"
	BdsESStatus            = "0.7"
	BdsESIdCat             = "0.8"
	BdsESAirVelocity       = "0.9"
	BdsElsDataLinkCap      = "1.0"
	BdsElsGicbCap          = "1.7"
	BdsElsAircraftIdent    = "2.0"
	BdsElsAcasRA           = "3.0"
	BdsEhsSelVertIntent    = "4.0"
	BdsEhsAircraftIntent   = "4.3"
	BdsMetRoutineAirReport = "4.4"
	BdsMetHazartReport     = "4.5"
	BdsEhsTrackTurnReport  = "5.0"
	BdsEhsPosRepCourse     = "5.1"
	BdsEhsPosRepFine       = "5.2"
	BdsEhsAirRefStateVec   = "5.3"
	BdsEhsHeadingSpeed     = "6.0"
)

type bds struct {
}

var (
	ErrUnknownCommBMessage  = errors.New("unable to infer Comm-B message type")
	ErrCommBIncorrectLength = errors.New("Comm-B must be exactly 7 bytes")
	bdsFields               = map[string]string{
		// ADSB Service
		"0.5": "Extended Squitter Airborne Position",
		"0.6": "Extended Squitter Surface Position",
		"0.7": "Extended Squitter status",
		"0.8": "Extended Squitter Identification and Category",
		"0.9": "Extended Squitter Airborne velocity Information",
		"1.0": "Data Link Capability Report",         // ELS Service
		"1.7": "Common usage GICB capability report", // ELS Service
		"2.0": "Aircraft Identity",                   // ELS Service
		"3.0": "ACAS active resolution advisory",     // ELS Service
		"4.0": "Selected vertical intention",         // EHS Service
		"4.3": "Aircraft Intention",
		"4.4": "Meteorological Routine Air Report", // Meteorological Service
		"4.5": "Meteorological Hazard Report",      // Meteorological Service
		"5.0": "Track and Turn Report",             // EHS Service
		"5.1": "Position Report Coarse",
		"5.2": "Position Report Fine",
		"5.3": "Air-referenced State Vector",
		"6.0": "heading and Speed Report", // EHS Service
	}
)

func (f *Frame) DescribeBds() string {
	key := f.BdsMessageType()
	s, ok := bdsFields[key]
	if !ok {
		return key + ": Unknown"
	}
	return key + ": " + s
}

func (f *Frame) BdsMessageType() string {
	return fmt.Sprintf("%d.%d", f.bdsMajor, f.bdsMinor)
}

// Decodes an MB Field
func (f *Frame) decodeCommB() error {
	var err error
	f.bdsMajor, f.bdsMinor, err = inferCommBMessageType(f.message[4:11])
	if nil != err {
		// log the error?
		if !errors.Is(err, ErrUnknownCommBMessage) {
			log.Error().Err(err).Send()
		}
	}

	switch f.BdsMessageType() {
	case BdsElsDataLinkCap: // 1.0
		// decode capability
	case BdsElsGicbCap: // 1.7
		// decode GICB
	case BdsElsAircraftIdent: // 2.0
		f.decodeFlightNumber()
	}

	// things get a lot murkier from here on in!
	// we should attempt to decode each BDS frame, in turn.
	// if we cannot decode it as a given frame (lots of error checking) fall through to the next type

	// BDS 4,0 - BDS status bits = 1, 14, 26, 37, 48
	// BDS 4,3 - BDS status bits = 1, 13, 26. bits 43-56 are 0's
	// BDS 4,4 - BDS status bits = 5, 24, 35, 47, 50
	// BDS 4,5 - BDS status bits = 1, 4, 7, 10, 13, 16, 27, 39. 52-56 are 0's
	// BDS 5,0 - BDS status bits = 1, 12, 24, 35, 46
	// BDS 5,1 - BDS status bits = 1
	// BDS 5,2 - BDS status bits = 1
	// BDS 5,3 - BDS status bits = 1, 13, 24, 34, 47
	// BDS 6,0 - BDS status bits = 1, 13, 24, 35, 46

	return nil
}

// inferCommBMessageType uses some fancy guesswork to determine the type of response we have.
// pass in the Comm-B message bytes (MB Field)
// decoding based on this https://mode-s.org/decode/content/mode-s/9-inference.html
func inferCommBMessageType(mb []byte) (byte, byte, error) {
	if len(mb) != 7 {
		return 0, 0, ErrCommBIncorrectLength
	}

	// BDS == Comm-B Data Selector

	// Starting with ELS Detection

	// BDS 1,0 - Data Link Capability Report
	// 00 01 00 00 == 0x10  && X0 00 00 XX
	if mb[0] == 0b0001_0000 && mb[1]&0b0111_1100 == 0 {
		return 1, 0, nil
	}

	// BDS 1,7 - Common usage GICB capability report
	// bit 7 == 1, bits 29-56 zeros
	// Detection: BDS Code && Reserved Bits
	if mb[0]&0x2 == 0x2 && ((0 == mb[3]&0xF) && (0 == mb[4]+mb[5]+mb[6])) {
		return 1, 7, nil
	}

	// BDS 2, 0
	// bits 1-8 ==
	// Detection: BDS Code && Callsign
	if mb[0] == 0b0010_0000 {
		// bits 9-56 are call sign, should not contain any ? chars from aisCharset
		if callsign := decodeFlightNumber(mb[1:7]); len(callsign) > 0 {
			return 2, 0, nil
		}
	}

	// BDS 3, 0
	// Detection: BDS Code && Threat Type && ACAS
	var bits uint64
	// get bits 16-22 as the LSB
	bits = ((uint64(mb[1]) << 8) | uint64(mb[2])) >> 2
	if mb[0] == 0b0011_0000 && mb[3]&0b0000_1100 != 0b0000_1100 && bits&0b0111_1111 < 48 {
		return 3, 0, nil
	}

	// Now onto EHS Detection
	// TODO: implement EHS

	// and lastly onto Meteorological Detection
	// TODO: Implement MRAR and MHR

	return 0, 0, ErrUnknownCommBMessage
}
