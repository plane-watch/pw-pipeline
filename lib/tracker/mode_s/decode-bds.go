package mode_s

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/rs/zerolog/log"
)

// BDS type string constants for external use
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
	BdsMetHazardReport     = "4.5"
	BdsEhsTrackTurnReport  = "5.0"
	BdsEhsPosRepCoarse     = "5.1"
	BdsEhsPosRepFine       = "5.2"
	BdsEhsAirRefStateVec   = "5.3"
	BdsEhsHeadingSpeed     = "6.0"
)

// bdsDecoder is a function that scores and optionally decodes a BDS message
// Returns a score (higher is better match), or 0 if message doesn't match this format
type bdsDecoder func(f *Frame, mb []byte, store bool) int

// Scoring constants for BDS decoder confidence levels
const (
	// Full match scores - all bits validated
	scoreFullMatch = 56 // All 56 bits of MB field validated

	// Field presence scores (bits 1-N validated)
	scoreFieldPresent13Bit = 13 // 13-bit field validated (altitude, etc)
	scoreFieldPresent11Bit = 11 // 11-bit field validated (angles, speeds)
	scoreFieldPresent10Bit = 10 // 10-bit field validated (rates, etc)
	scoreFieldPresent4Bit  = 4  // 4-bit field validated
	scoreFieldPresent3Bit  = 3  // 3-bit field validated
	scoreFieldPresent1Bit  = 1  // 1-bit field validated

	// Penalty scores for inconsistent data
	scorePenaltyInconsistent = 6 // Physics/cross-validation failure
	scorePenaltyUnlikely     = 4 // Unlikely value (e.g. altitude not multiple of 500ft)
	scorePenaltyRare         = 2 // Rare capability bit set
)

var (
	ErrUnknownCommBMessage  = errors.New("unable to infer Comm-B message type")
	ErrCommBIncorrectLength = errors.New("Comm-B must be exactly 7 bytes")
	bdsFields               = map[string]string{
		// ADSB Service (Extended Squitter - DF17/18)
		BdsESAirbornePosition: "Extended Squitter Airborne Position",
		BdsESSurfacePosition:  "Extended Squitter Surface Position",
		BdsESStatus:           "Extended Squitter Status",
		BdsESIdCat:            "Extended Squitter Identification and Category",
		BdsESAirVelocity:      "Extended Squitter Airborne Velocity Information",
		// ELS Service (Elementary Surveillance - DF20/21)
		BdsElsDataLinkCap:   "Data Link Capability Report",
		BdsElsGicbCap:       "Common Usage GICB Capability Report",
		BdsElsAircraftIdent: "Aircraft Identity",
		BdsElsAcasRA:        "ACAS Active Resolution Advisory",
		// EHS Service (Enhanced Surveillance - DF20/21)
		BdsEhsSelVertIntent:   "Selected Vertical Intention",
		BdsEhsAircraftIntent:  "Aircraft Intention",
		BdsEhsTrackTurnReport: "Track and Turn Report",
		BdsEhsPosRepCoarse:    "Position Report Coarse",
		BdsEhsPosRepFine:      "Position Report Fine",
		BdsEhsAirRefStateVec:  "Air-Referenced State Vector",
		BdsEhsHeadingSpeed:    "Heading and Speed Report",
		// Meteorological Service
		BdsMetRoutineAirReport: "Meteorological Routine Air Report",
		BdsMetHazardReport:     "Meteorological Hazard Report",
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

// Decodes an MB Field using scoring system
// Based on readsb's comm_b.c implementation
func (f *Frame) decodeCommB() error {
	mb := f.message[4:11] // MB field is 7 bytes in DF20/21

	if len(mb) != 7 {
		return ErrCommBIncorrectLength
	}

	// If DR or UM are set, this message is _probably_ noise
	// DR = Downlink Request (bits 8-12 in DF20)
	// UM = Utility Message (bits 8-12 in DF20, bits 13-18 in DF21)
	// Also skip anything that had error bits corrected
	// Reference: readsb comm_b.c:55-60
	if f.dr != 0 || f.um != 0 {
		return nil // Skip Comm-B decoding
	}

	// List of all BDS decoders - they will compete via scoring
	decoders := []bdsDecoder{
		decodeEmptyResponse,
		decodeBDS10,
		decodeBDS17,
		decodeBDS20,
		decodeBDS30,
		decodeBDS40,
		decodeBDS50,
		decodeBDS60,
	}

	// Find the best matching decoder
	bestScore := 0
	var bestDecoder bdsDecoder
	ambiguous := false

	for _, decoder := range decoders {
		score := decoder(f, mb, false) // Trial run - don't store yet
		if score > bestScore {
			bestScore = score
			bestDecoder = decoder
			ambiguous = false
		} else if score == bestScore && score > 0 {
			ambiguous = true // Multiple decoders have same score
		}
	}

	// Decode with the best decoder if found and unambiguous
	if bestDecoder != nil {
		if ambiguous {
			// Multiple decoders matched - can't reliably decode
			log.Debug().Msg("Comm-B message is ambiguous")
			return nil
		}
		// Actually decode and store the data
		bestDecoder(f, mb, true)
	}

	return nil
}

// inferCommBMessageType is a legacy BDS inference function that uses pattern matching.
// NOTE: This function is DEPRECATED for production use. The scoring-based decodeCommB()
// function should be used instead as it includes EHS decoders (BDS 4.0, 5.0, 6.0) and
// physics-based validation. This function is retained for testing and validation purposes.
//
// Pass in the Comm-B message bytes (MB Field)
// Decoding based on https://mode-s.org/decode/content/mode-s/9-inference.html
// Reference: Original implementation prior to competitive scoring system
func inferCommBMessageType(mb []byte) (byte, byte, error) {
	if len(mb) != 7 {
		return 0, 0, ErrCommBIncorrectLength
	}

	// BDS == Comm-B Data Selector

	// ELS (Elementary Surveillance) Detection

	// BDS 1,0 - Data Link Capability Report
	// Pattern: 0x10 with reserved bits clear
	if mb[0] == 0b0001_0000 && mb[1]&0b0111_1100 == 0 {
		return 1, 0, nil
	}

	// BDS 1,7 - Common usage GICB capability report
	// bit 7 == 1, bits 29-56 zeros
	// Detection: BDS Code && Reserved Bits
	if mb[0]&0x2 == 0x2 && ((0 == mb[3]&0xF) && (0 == mb[4]+mb[5]+mb[6])) {
		return 1, 7, nil
	}

	// BDS 2,0 - Aircraft Identification
	// Detection: BDS Code && Valid Callsign
	if mb[0] == 0b0010_0000 {
		// bits 9-56 are call sign, should not contain any ? chars from aisCharset
		callsign := string(decodeFlightNumber(mb[1:7]))
		if callsign != "" && !strings.Contains(callsign, "?") {
			return 2, 0, nil
		}
	}

	// BDS 3,0 - ACAS Resolution Advisory
	// Detection: BDS Code && Threat Type && ACAS
	var bits uint64
	// get bits 16-22 as the LSB
	bits = ((uint64(mb[1]) << 8) | uint64(mb[2])) >> 2
	if mb[0] == 0b0011_0000 && mb[3]&0b0000_1100 != 0b0000_1100 && bits&0b0111_1111 < 48 {
		return 3, 0, nil
	}

	// EHS (Enhanced Surveillance) and Meteorological formats are NOT implemented
	// in this legacy function. Use the scoring-based decoders instead:
	// - BDS 4.0: decodeBDS40() (Selected Vertical Intention)
	// - BDS 5.0: decodeBDS50() (Track and Turn Report)
	// - BDS 6.0: decodeBDS60() (Heading and Speed Report)

	return 0, 0, ErrUnknownCommBMessage
}

// getBit extracts a single bit from a byte array using 1-based ICAO bit indexing.
//
// The ICAO specification uses 1-based bit numbering where bit 1 is the MSB of byte 0.
// This differs from typical programming conventions which use 0-based indexing.
//
// Parameters:
//   - data: byte array to extract from
//   - bitIndex: 1-based bit position (1 = MSB of first byte)
//
// Returns 0 if bitIndex is out of bounds.
//
// Example: For data = [0b10101010], getBit(data, 1) returns 1 (MSB)
func getBit(data []byte, bitIndex int) uint {
	byteIndex := (bitIndex - 1) / 8
	bitOffset := 7 - ((bitIndex - 1) % 8)
	if byteIndex >= len(data) {
		return 0
	}
	return uint((data[byteIndex] >> bitOffset) & 1)
}

// getBits extracts multiple consecutive bits from a byte array using 1-based ICAO bit indexing.
//
// The ICAO specification uses 1-based bit numbering where bit 1 is the MSB of byte 0.
// Both startBit and endBit are inclusive.
//
// Parameters:
//   - data: byte array to extract from
//   - startBit: 1-based starting bit position (inclusive)
//   - endBit: 1-based ending bit position (inclusive)
//
// Returns a uint64 containing the extracted bits in the least significant positions.
//
// Example: For data = [0xFF, 0x00], getBits(data, 1, 8) returns 0xFF
func getBits(data []byte, startBit, endBit int) uint64 {
	var result uint64
	for i := startBit; i <= endBit; i++ {
		result = (result << 1) | uint64(getBit(data, i))
	}
	return result
}

// decodeEmptyResponse checks if the Comm-B message is all zeros
// Reference: readsb comm_b.c:88-100
func decodeEmptyResponse(f *Frame, mb []byte, store bool) int {
	for i := 0; i < 7; i++ {
		if mb[i] != 0 {
			return 0
		}
	}

	if store {
		f.bdsMajor = 0
		f.bdsMinor = 0
	}

	return scoreFullMatch // All 56 bits are zero - high confidence
}

// decodeBDS10 decodes BDS 1,0 - Data Link Capability Report
// Reference: readsb comm_b.c:104-124
func decodeBDS10(f *Frame, mb []byte, store bool) int {
	// BDS identifier must be 0x10
	if mb[0] != 0x10 {
		return 0
	}

	// Reserved bits 10-14 must be zero
	if getBits(mb, 10, 14) != 0 {
		return 0
	}

	if store {
		f.bdsMajor = 1
		f.bdsMinor = 0
		// Could decode capability bits here if needed
	}

	return scoreFullMatch
}

// decodeBDS17 decodes BDS 1,7 - Common usage GICB capability report
// Reference: readsb comm_b.c:128-206
func decodeBDS17(f *Frame, mb []byte, store bool) int {
	// Reserved bits 25-56 must be zero
	if getBits(mb, 25, 56) != 0 {
		return 0
	}

	score := 0

	// BDS 2,0 (aircraft identification) is on almost everything
	if getBit(mb, 7) == 1 {
		score += scoreFieldPresent1Bit
	} else {
		score -= scorePenaltyRare
	}

	// Unlikely bits - these are rare capabilities
	if getBit(mb, 10) == 1 { // 4,1 next waypoint identifier
		score -= scorePenaltyRare
	}
	if getBit(mb, 11) == 1 { // 4,2 next waypoint position
		score -= scorePenaltyRare
	}

	if score < 0 {
		return 0
	}

	if store {
		f.bdsMajor = 1
		f.bdsMinor = 7
	}

	return score
}

// decodeBDS20 decodes BDS 2,0 - Aircraft Identification
// Reference: readsb comm_b.c:209-248
func decodeBDS20(f *Frame, mb []byte, store bool) int {
	// BDS identifier must be 0x20
	if mb[0] != 0x20 {
		return 0
	}

	// Decode callsign and validate
	callsign := decodeFlightNumber(mb[1:7])
	if callsign == nil {
		return 0
	}

	// Check for invalid characters
	callsignStr := string(callsign)
	if strings.Contains(callsignStr, "?") || strings.Contains(callsignStr, "@") {
		return 0
	}

	if store {
		f.bdsMajor = 2
		f.bdsMinor = 0
		f.flight = callsign
	}

	return scoreFullMatch
}

// decodeBDS30 decodes BDS 3,0 - ACAS Active Resolution Advisory
// Reference: readsb comm_b.c:333-348
func decodeBDS30(f *Frame, mb []byte, store bool) int {
	// BDS identifier must be 0x30
	if mb[0] != 0x30 {
		return 0
	}

	// Threat type indicator (bits 27-28) - both bits set is invalid
	tti := getBits(mb, 27, 28)
	if tti == 3 {
		return 0
	}

	// AR active (bits 16-22) - ICAO address, must be < 48 for validity
	arActive := getBits(mb, 16, 22)
	if arActive >= 48 {
		return 0
	}

	if store {
		f.bdsMajor = 3
		f.bdsMinor = 0
		// Could decode ACAS RA details here
	}

	return scoreFullMatch
}

// decodeBDS40 decodes BDS 4,0 - Selected Vertical Intention
// Reference: readsb comm_b.c:352-514
func decodeBDS40(f *Frame, mb []byte, store bool) int {
	mcpValid := getBit(mb, 1)
	mcpRaw := getBits(mb, 2, 13)
	fmsValid := getBit(mb, 14)
	fmsRaw := getBits(mb, 15, 26)
	baroValid := getBit(mb, 27)
	baroRaw := getBits(mb, 28, 39)
	reserved1 := getBits(mb, 40, 47)
	modeValid := getBit(mb, 48)
	modeRaw := getBits(mb, 49, 51)
	reserved2 := getBits(mb, 52, 53)
	sourceValid := getBit(mb, 54)
	sourceRaw := getBits(mb, 55, 56)

	// At least one field must be valid
	if mcpValid == 0 && fmsValid == 0 && baroValid == 0 && modeValid == 0 && sourceValid == 0 {
		return 0
	}

	score := 0

	// Validate MCP altitude
	var mcpAlt uint64
	if mcpValid == 1 && mcpRaw != 0 {
		mcpAlt = mcpRaw * 16
		if mcpAlt >= 1000 && mcpAlt <= 50000 {
			score += scoreFieldPresent13Bit
		} else {
			return 0 // Implausible altitude
		}
	} else if mcpValid == 0 && mcpRaw == 0 {
		score += scoreFieldPresent1Bit
	} else {
		return 0
	}

	// Validate FMS altitude
	var fmsAlt uint64
	if fmsValid == 1 && fmsRaw != 0 {
		fmsAlt = fmsRaw * 16
		if fmsAlt >= 1000 && fmsAlt <= 50000 {
			score += scoreFieldPresent13Bit
		} else {
			return 0 // Implausible altitude
		}
	} else if fmsValid == 0 && fmsRaw == 0 {
		score += scoreFieldPresent1Bit
	} else {
		return 0
	}

	// Validate barometric setting
	var baroSetting float64
	if baroValid == 1 && baroRaw != 0 {
		baroSetting = 800 + float64(baroRaw)*0.1
		if baroSetting >= 900 && baroSetting <= 1100 {
			score += scoreFieldPresent13Bit
		} else {
			return 0 // Implausible pressure
		}
	} else if baroValid == 0 && baroRaw == 0 {
		score += scoreFieldPresent1Bit
	} else {
		return 0
	}

	// Reserved bits must be zero
	if reserved1 != 0 || reserved2 != 0 {
		return 0
	}

	// Validate mode field
	if modeValid == 1 {
		score += scoreFieldPresent4Bit
	} else if modeValid == 0 && modeRaw == 0 {
		score += scoreFieldPresent1Bit
	} else {
		return 0
	}

	// Validate source field
	if sourceValid == 1 {
		score += scoreFieldPresent3Bit
	} else if sourceValid == 0 && sourceRaw == 0 {
		score += scoreFieldPresent1Bit
	} else {
		return 0
	}

	// Penalty for inconsistent MCP/FMS altitudes
	if mcpValid == 1 && fmsValid == 1 && mcpAlt != fmsAlt {
		score -= scorePenaltyUnlikely
	}

	// MCP altitude should be a multiple of 500 feet
	if mcpValid == 1 {
		remainder := mcpAlt % 500
		if !(remainder < 16 || remainder > 484) {
			score -= scorePenaltyUnlikely
		}
	}

	// FMS altitude should be a multiple of 500 feet
	if fmsValid == 1 {
		remainder := fmsAlt % 500
		if !(remainder < 16 || remainder > 484) {
			score -= scorePenaltyUnlikely
		}
	}

	if store {
		f.bdsMajor = 4
		f.bdsMinor = 0
		// Store decoded values (would need to add fields to Frame struct)
	}

	return score
}

// decodeBDS50 decodes BDS 5,0 - Track and Turn Report
// Reference: readsb comm_b.c:518-672
func decodeBDS50(f *Frame, mb []byte, store bool) int {
	rollValid := getBit(mb, 1)
	rollSign := getBit(mb, 2)
	rollRaw := getBits(mb, 3, 11)

	trackValid := getBit(mb, 12)
	trackSign := getBit(mb, 13)
	trackRaw := getBits(mb, 14, 23)

	gsValid := getBit(mb, 24)
	gsRaw := getBits(mb, 25, 34)

	trackRateValid := getBit(mb, 35)
	trackRateSign := getBit(mb, 36)
	trackRateRaw := getBits(mb, 37, 45)

	tasValid := getBit(mb, 46)
	tasRaw := getBits(mb, 47, 56)

	// All status bits must be set for BDS 5,0
	if rollValid == 0 || trackValid == 0 || gsValid == 0 || tasValid == 0 {
		return 0
	}

	score := 0

	// Validate roll angle (-90 to +90 degrees)
	roll := float64(rollRaw) * 45.0 / 256.0
	if rollSign == 1 {
		roll -= 90.0
	}
	if roll >= -40 && roll < 40 {
		score += scoreFieldPresent11Bit
	} else {
		return 0 // Implausible roll angle
	}

	// Validate track angle (0-360 degrees)
	track := float64(trackRaw) * 90.0 / 512.0
	if trackSign == 1 {
		track += 180.0
	}
	score += scoreFieldPresent11Bit

	// Validate ground speed (0-700 knots typical)
	gs := float64(gsRaw) * 2
	if gs >= 50 && gs <= 700 {
		score += scoreFieldPresent11Bit
	} else {
		return 0
	}

	// Validate track rate
	var trackRate float64
	if trackRateValid == 1 {
		trackRate = float64(trackRateRaw) * 8.0 / 256.0
		if trackRateSign == 1 {
			trackRate -= 16.0
		}
		if trackRate >= -10 && trackRate <= 10 {
			score += scoreFieldPresent10Bit
		} else {
			return 0
		}
	}

	// Validate true airspeed (50-700 knots typical)
	tas := float64(tasRaw) * 2
	if tas >= 50 && tas <= 700 {
		score += scoreFieldPresent11Bit
	} else {
		return 0
	}

	// Cross-validate: theoretical turn rate vs actual track rate
	// turn_rate = (68625 * tan(roll)) / (TAS * 20 * π)
	if trackRateValid == 1 && tas > 0 {
		theoreticalTurnRate := 68625 * math.Tan(roll*math.Pi/180.0) / (tas * 20 * math.Pi)
		delta := math.Abs(theoreticalTurnRate - trackRate)
		if delta > 2.0 {
			score -= scorePenaltyInconsistent
		}
	}

	if store {
		f.bdsMajor = 5
		f.bdsMinor = 0
		// Store decoded values (would need to add fields to Frame struct)
	}

	return score
}

// decodeBDS60 decodes BDS 6,0 - Heading and Speed Report
// Reference: readsb comm_b.c:676-824
func decodeBDS60(f *Frame, mb []byte, store bool) int {
	hdgValid := getBit(mb, 1)
	hdgSign := getBit(mb, 2)
	hdgRaw := getBits(mb, 3, 12)

	iasValid := getBit(mb, 13)
	iasRaw := getBits(mb, 14, 23)

	machValid := getBit(mb, 24)
	machRaw := getBits(mb, 25, 34)

	barRateValid := getBit(mb, 35)
	barRateSign := getBit(mb, 36)
	barRateRaw := getBits(mb, 37, 45)

	inertRateValid := getBit(mb, 46)
	inertRateSign := getBit(mb, 47)
	inertRateRaw := getBits(mb, 48, 56)

	// All status bits must be set for BDS 6,0
	if hdgValid == 0 || iasValid == 0 || machValid == 0 || barRateValid == 0 || inertRateValid == 0 {
		return 0
	}

	score := 0

	// Validate magnetic heading (0-360 degrees)
	hdg := float64(hdgRaw) * 90.0 / 512.0
	if hdgSign == 1 {
		hdg += 180.0
	}
	score += scoreFieldPresent11Bit

	// Validate indicated airspeed (50-700 knots typical)
	ias := float64(iasRaw)
	if ias >= 50 && ias <= 700 {
		score += scoreFieldPresent11Bit
	} else {
		return 0
	}

	// Validate Mach number (0.1 to 1.0 typical)
	mach := float64(machRaw) * 2.048 / 512.0
	if mach >= 0.1 && mach <= 1.0 {
		score += scoreFieldPresent11Bit
	} else {
		return 0
	}

	// Validate barometric altitude rate (-6000 to +6000 ft/min typical)
	barRate := float64(barRateRaw) * 32
	if barRateSign == 1 {
		barRate = -barRate
	}
	if math.Abs(barRate) <= 6000 {
		score += scoreFieldPresent10Bit
	} else {
		return 0
	}

	// Validate inertial vertical rate (-6000 to +6000 ft/min typical)
	inertRate := float64(inertRateRaw) * 32
	if inertRateSign == 1 {
		inertRate = -inertRate
	}
	if math.Abs(inertRate) <= 6000 {
		score += scoreFieldPresent10Bit
	} else {
		return 0
	}

	// Cross-validate: IAS and Mach should be consistent
	// At typical cruise altitude, Mach * 600 ≈ IAS
	// This is a rough check
	if ias > 0 && mach > 0 {
		expectedIAS := mach * 600
		delta := math.Abs(ias - expectedIAS)
		if delta > 150 {
			score -= scorePenaltyInconsistent
		}
	}

	if store {
		f.bdsMajor = 6
		f.bdsMinor = 0
		// Store decoded values (would need to add fields to Frame struct)
	}

	return score
}
