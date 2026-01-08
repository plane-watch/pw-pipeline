package mode_s

import (
	"errors"
	"fmt"
)

// CRC Validation Framework for Mode S
//
// Mode S uses two types of CRC fields:
//
// 1. PI Field (Parity/Interrogator Identity) - DF 11, 17, 18
//    - Last 3 bytes contain pure CRC
//    - Validation: CRC of entire message should equal 0
//
// 2. AP Field (Address/Parity) - DF 0, 4, 5, 16, 20, 21, 24
//    - Last 3 bytes contain CRC ⊕ ICAO Address
//    - Validation: Extract ICAO and verify consistency
//
// Reference: ICAO Annex 10, Volume IV

var (
	modesChecksumTable [256]uint32
	doIcaoCheck        bool = true

	ErrUnreliableICAOInsufficientFrameCount = errors.New("insufficient frames from plane to trust DF00 data")
	ErrUnreliableICAOUnableToDetermine      = errors.New("unable to determine icao from data")
	ErrUnreliableICAONotPreviouslySeen      = errors.New("not trusting frame from unseen aircraft")
	ErrUnreliableICAODidNotMatch            = errors.New("crc icao did not match previously decoded icao")
)

// modesGeneratorPoly is the Mode S generator polynomial for CRC-24.
// This polynomial is defined in the Mode S specification (ICAO Annex 10, Volume IV).
const modesGeneratorPoly uint32 = 0xfff409

// init generates a 256-entry lookup table for fast CRC calculation.
// The table is precomputed for each possible byte value (0-255).
// Each entry contains the CRC result of processing that byte with the Mode S polynomial.
// This allows the CRC calculation to process one byte at a time using table lookups
// instead of bit-by-bit polynomial division, significantly improving performance.
func init() {
	var i uint32
	var j int

	for i = 0; i < 256; i++ {
		// Position byte value in the high bits (treating it as the next input byte)
		var c = i << 16

		// Process 8 bits using polynomial division
		for j = 0; j < 8; j++ {
			if c&0x800000 != 0 {
				// If high bit is set, shift and XOR with polynomial
				c = (c << 1) ^ modesGeneratorPoly
			} else {
				// Otherwise just shift
				c <<= 1
			}
		}

		// Store 24-bit result
		modesChecksumTable[i] = c & 0x00ffffff
	}
}

// DisableICAOChecking disables the ICAO validation checks that filter frames
// based on whether we've seen the aircraft before. This is useful for testing
// or debugging, but should be enabled in production to filter out false positives
// from interrogator IDs being misidentified as aircraft ICAOs.
func DisableICAOChecking() {
	doIcaoCheck = false
}

// calculateCRC computes the Mode S CRC-24 for the given message bytes.
//
// This implements table-based CRC calculation using the precomputed lookup table.
// The algorithm processes one byte at a time:
//  1. XOR the next input byte with the high byte of current CRC
//  2. Use result as index into lookup table
//  3. Shift CRC left 8 bits and XOR with table value
//  4. Mask to 24 bits
//
// This is mathematically equivalent to polynomial division but much faster.
func calculateCRC(message []byte, length uint32) uint32 {
	var crc uint32
	var index uint32
	for i := uint32(0); i < length; i++ {
		// Combine input byte with high byte of running CRC
		index = uint32(message[i]) ^ ((crc & 0xff0000) >> 16)
		// Shift CRC and XOR with precomputed table entry
		crc = (crc << 8) ^ modesChecksumTable[index]
		// Keep only 24 bits
		crc &= 0xffffff
	}
	return crc
}

// decodeModeSChecksum validates PI field frames (DF 11, 17, 18).
//
// For PI field frames, the Mode S specification requires that the CRC
// computed over the entire message (including the PI field) equals zero.
// This is achieved by the transmitter encoding the PI field as:
//
//	PI = CRC(message_data)
//
// To validate:
//  1. Compute CRC over message data (first n-3 bytes)
//  2. XOR with PI field (last 3 bytes)
//  3. Result should be 0 for valid frames
//
// Returns the validation result (0 = valid, non-zero = invalid/corrupted).
func (f *Frame) decodeModeSChecksum() uint32 {
	var n = f.getMessageLengthBytes()

	// Calculate CRC over message data (excluding PI field)
	f.checkSum = calculateCRC(f.message, n-3)

	// XOR with the PI field (last 3 bytes)
	// This completes the validation: CRC(data) ⊕ PI should equal 0
	f.checkSum = f.checkSum ^ (uint32(f.message[n-3]) << 16) ^ (uint32(f.message[n-2]) << 8) ^ uint32(f.message[n-1])

	return f.checkSum
}

// decodeModeSChecksumAddr computes the Mode S CRC for a frame,
// then XORs it with the Address/Parity (AP) field to recover the ICAO address.
// The recovered ICAO is returned.
func (f *Frame) decodeModeSChecksumAddr() uint32 {
	var n = f.getMessageLengthBytes()

	// Calculate CRC over the message data (excluding AP field)
	crc := calculateCRC(f.message, n-3)

	// Extract AP field from message (last 3 bytes)
	apField := uint32(f.message[n-3])<<16 | uint32(f.message[n-2])<<8 | uint32(f.message[n-1])

	// ICAO = CRC ⊕ AP
	return crc ^ apField
}

func (f *Frame) CRC() uint32 {
	return f.checkSum
}

// checkCrc validates the CRC/parity field and extracts ICAO addresses from Mode S frames.
//
// Mode S uses two different parity field types:
//
// PI Field (DF 11, 17, 18): Contains pure CRC for message integrity validation.
//   - These are ADS-B messages with reliable ICAO addresses in the message payload.
//   - Validation: Compute CRC of entire message; result should be 0.
//
// AP Field (DF 0, 4, 5, 16, 20, 21, 24): Contains CRC ⊕ Address for both integrity and addressing.
//   - The "address" may be aircraft ICAO or interrogator ID depending on context.
//   - Challenge: Distinguishing aircraft ICAO from interrogator ID requires context-specific logic.
//   - Each DF type requires different validation strategies (see individual cases below).
func (f *Frame) checkCrc() error {

	switch f.downLinkFormat {
	case 11, 17, 18: // PI Field (Parity/Interrogator Identity)
		// DF11: All-Call Reply (mode S only roll call)
		// DF17: Extended Squitter (ADS-B)
		// DF18: Extended Squitter/Non-Transponder (ADS-B, TIS-B, ADS-R)
		//
		// These frames contain the aircraft ICAO in the message payload (AA field),
		// and use a pure CRC in the PI field for message integrity.
		// Validation is straightforward: CRC of entire message should equal 0.
		f.checkSum = f.decodeModeSChecksum()
		if f.checkSum == 0 {
			return nil
		}
		return fmt.Errorf("%w for DF %d (%s)", ErrInvalidChecksum, f.downLinkFormat, f.raw)

	case 0, 16: // AP Field (Address/Parity) - ACAS/TCAS surveillance replies
		// DF0: Short Air-Air Surveillance (ACAS)
		// DF16: Long Air-Air Surveillance (ACAS)
		//
		// CHALLENGE: These frames are responses to air-to-air interrogations (TCAS).
		// The AP field contains CRC ⊕ Interrogator_ID, NOT the aircraft ICAO.
		// However, the extracted value looks like a valid ICAO address.
		//
		// ATTEMPTED SOLUTION: Tried to find bit patterns that distinguish aircraft ICAOs
		// from interrogator IDs, but no reliable patterns were found.
		//
		// CURRENT SOLUTION: Use frame count as a heuristic:
		// - Real aircraft transmit DF0/16 frames repeatedly over time
		// - Interrogator IDs appear random and don't recur with significant frequency
		// - Only accept if we've seen sufficient frames with this "ICAO"
		icao, err := f.validateAPField()
		if err != nil {
			return fmt.Errorf("DF%d AP validation failed: %w", f.downLinkFormat, err)
		}

		if doIcaoCheck && !hasSufficientExistingMessages(icao) {
			return ErrUnreliableICAOInsufficientFrameCount
		}

		f.icao = icao
		return nil

	case 4, 5: // AP Field (Address/Parity) - surveillance altitude/identity replies
		// DF4: Surveillance Altitude Reply
		// DF5: Surveillance Identity Reply
		//
		// These are responses to ground interrogations (Mode S or Mode A/C radar).
		// The AP field interpretation depends on the interrogation type:
		//
		// STRATEGY: Use the UM (Utility Message) field to determine AP field content:
		// - UM=0: Broadcast interrogation → AP = CRC ⊕ Aircraft_ICAO
		// - UM≠0: Selective interrogation → AP = CRC ⊕ Interrogator_ID
		//
		// UM field structure (6 bits):
		//   - IIS (4 bits): Interrogator Identifier Subfield
		//   - IDS (2 bits): Interrogator Identifier Designator
		//   - When we see all zeros (UM=0): No specific interrogator = broadcast
		icao, err := f.validateAPField()
		if err != nil {
			return fmt.Errorf("DF%d AP validation failed: %w", f.downLinkFormat, err)
		}

		// Extract UM field (bits 13-18):
		// Byte 0: [DF(5) FS(3)]
		// Byte 1: [DR(5) UM(3)]  <- bits 13-15 of message
		// Byte 2: [UM(3) AC(5)]  <- bits 16-18 of message
		um := ((uint16(f.message[1]) & 0x07) << 3) | (uint16(f.message[2]) >> 5)

		if um == 0 {
			// Broadcast interrogation: AP field contains aircraft ICAO

			// Let's make sure this isn't the first message we've seen-- to ensure we don't get false positives.
			// um == 0 is a solid test like >99.9% of the time, but I've seen tiny outliers, so let's throw one frame
			// away to be sure.
			if doIcaoCheck && !hasAnyExistingMessages(icao) {
				return ErrUnreliableICAONotPreviouslySeen
			}

			f.icao = icao
			return nil
		}

		// Selective interrogation (UM≠0): AP likely contains interrogator ID
		// Use frame count threshold as fallback (same strategy as DF0/16)
		if doIcaoCheck && !hasSufficientExistingMessages(icao) {
			return ErrUnreliableICAOUnableToDetermine
		}

		f.icao = icao
		return nil

	case 20, 21: // AP Field - Comm-B altitude/identity replies
		// DF20: Comm-B Altitude Reply
		// DF21: Comm-B Identity Reply
		//
		// These frames contain Comm-B data (BDS registers) with aircraft information
		// not available in ADS-B (e.g., selected altitude, meteorological data, etc.).
		//
		// STRATEGY: Only accept frames from aircraft we've already seen via DF11/17/18.
		// This ensures we don't process interrogator IDs or spurious addresses.
		// The AP field likely contains interrogator ID, but if the extracted ICAO
		// matches a known aircraft, we can trust the Comm-B data payload.
		icao, err := f.validateAPField()
		if err != nil {
			return ErrUnreliableICAOUnableToDetermine
		}

		// Reject if ICAO hasn't been seen in reliable frames (DF11/17/18)
		if doIcaoCheck && !hasAnyExistingMessages(icao) {
			return ErrUnreliableICAONotPreviouslySeen
		}

		f.icao = icao
		return nil

	case 24: // AP Field - Comm-D (Extended Length Message)
		// DF24: Comm-D (ELM) - Extended Length Message
		//
		// DF24 frames can contain the aircraft ICAO in an AA (Address Announced) field
		// within the message payload. If present, we use that for validation.
		// Otherwise, we extract from the AP field and validate against known aircraft.
		icao, err := f.validateAPField()
		if err != nil {
			return err
		}

		if f.icao == 0 {
			// No ICAO in AA field, use AP field extraction
			// Only accept if this ICAO is from a known aircraft
			if doIcaoCheck && !hasAnyExistingMessages(icao) {
				return ErrUnreliableICAONotPreviouslySeen
			}

			f.icao = icao
		} else if f.icao != icao {
			// ICAO from AA field should match ICAO from AP field
			// If they don't match, the frame is likely corrupted or malformed
			return ErrUnreliableICAODidNotMatch
		}

		return nil
	default:
		return fmt.Errorf("do not know how to CRC Downlink Format %d", f.downLinkFormat)
	}
}

// validateAPField validates frames with AP field and extracts ICAO.
// For AP field messages, the last 3 bytes contain: AP = CRC ⊕ ICAO
// Therefore: ICAO = CRC ⊕ AP
func (f *Frame) validateAPField() (uint32, error) {
	var n = f.getMessageLengthBytes()

	// Create message copy with AP field zeroed
	msg := make([]byte, len(f.message))
	copy(msg, f.message)
	msg[n-3] = 0
	msg[n-2] = 0
	msg[n-1] = 0

	// Calculate CRC of message with zeroed AP field
	crc := calculateCRC(msg, n-3)

	// Extract AP field from original message
	apField := uint32(f.message[n-3])<<16 |
		uint32(f.message[n-2])<<8 |
		uint32(f.message[n-1])

	// ICAO = CRC ⊕ AP
	icao := crc ^ apField

	return icao, nil
}
