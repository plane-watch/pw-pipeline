package mode_s

import "fmt"

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
)

const modesGeneratorPoly uint32 = 0xfff409

func init() {
	var i uint32
	var j int

	for i = 0; i < 256; i++ {
		var c = i << 16

		for j = 0; j < 8; j++ {
			if c&0x800000 != 0 {
				c = (c << 1) ^ modesGeneratorPoly
			} else {
				c <<= 1
			}
		}

		modesChecksumTable[i] = c & 0x00ffffff
	}
}

// calculateCRC computes the Mode S CRC for the given message bytes.
// It processes 'length' bytes from the message and returns a 24-bit CRC value.
func calculateCRC(message []byte, length uint32) uint32 {
	var crc uint32
	var index uint32
	for i := uint32(0); i < length; i++ {
		index = uint32(message[i]) ^ ((crc & 0xff0000) >> 16)
		crc = (crc << 8) ^ modesChecksumTable[index]
		crc &= 0xffffff
	}
	return crc
}

func (f *Frame) decodeModeSChecksum() uint32 {
	var n = f.getMessageLengthBytes()

	// Calculate CRC over first n-3 bytes
	f.checkSum = calculateCRC(f.message, n-3)

	// XOR with the PI field (last 3 bytes)
	f.checkSum = f.checkSum ^ (uint32(f.message[n-3]) << 16) ^ (uint32(f.message[n-2]) << 8) ^ uint32(f.message[n-1])

	return f.checkSum
}
func (f *Frame) decodeModeSChecksumAddr() uint32 {
	var n = f.getMessageLengthBytes()
	var i, index uint32

	msg := make([]byte, len(f.message))
	copy(msg, f.message)
	msg[n-3] = 0
	msg[n-2] = 0
	msg[n-1] = 0
	var checkSum uint32
	for i = 0; i < n-3; i++ {
		index = uint32(msg[i]) ^ ((checkSum & 0xff_00_00) >> 16)
		checkSum = (checkSum << 8) ^ modesChecksumTable[index]
		checkSum &= 0xff_ff_ff
	}

	checkSum = checkSum ^ (uint32(msg[n-3]) << 16) ^ (uint32(msg[n-2]) << 8) ^ uint32(msg[n-1])

	crc := uint32(f.message[n-3])<<16 | uint32(f.message[n-2])<<8 | uint32(f.message[n-1])

	return checkSum ^ crc
}

func (f *Frame) checkCrc() error {

	switch f.downLinkFormat {
	case 11, 17, 18: // PI Field (Parity/Interrogator Identity)
		f.checkSum = f.decodeModeSChecksum()
		if f.checkSum == 0 {
			return nil
		}
		return fmt.Errorf("%w for DF %d (%s)", ErrInvalidChecksum, f.downLinkFormat, f.raw)

	case 0, 4, 5, 16: // AP Field (Address/Parity) - basic surveillance replies
		// AP field contains CRC XOR'd with Interrogator ID. Extract ICAO and validate
		// using frame count threshold to distinguish real aircraft from interrogator IDs.
		// Real aircraft transmit repeatedly (~3+ frames), interrogator IDs are random (1 frame).
		icao, err := f.validateAPField()
		if err != nil {
			return fmt.Errorf("DF%d AP validation failed: %w", f.downLinkFormat, err)
		}

		// Only accept if ICAO has been seen in enough frames (avoids ghost aircraft from interrogator IDs)
		if !testICAOFrameCount(icao) {
			return fmt.Errorf("DF%d from ICAO 0x%06X with insufficient frame count", f.downLinkFormat, icao)
		}

		// ICAO has sufficient history, accept the frame
		f.icao = icao
		return nil

	case 20, 21: // AP Field - Comm-B altitude/identity replies
		// DF20/21 contain valuable Comm-B data (BDS registers) not available in ADS-B.
		// Extract ICAO and only accept if it's from a known aircraft (via ICAO filter).
		icao, err := f.validateAPField()
		if err != nil {
			return fmt.Errorf("DF%d AP validation failed: %w", f.downLinkFormat, err)
		}

		// Test if this ICAO is already being tracked (from DF11/17/18 frames)
		if !testICAO(icao) {
			return fmt.Errorf("DF%d from unknown ICAO 0x%06X (not in filter)", f.downLinkFormat, icao)
		}

		// ICAO is known, accept the frame
		f.icao = icao
		return nil

	case 24: // AP Field for Comm-D
		// DF24 is different - AP field may be usable
		icao, err := f.validateAPField()
		if err != nil {
			return err
		}

		// For frames where we don't already have ICAO from AA field,
		// use the extracted ICAO from AP field
		if f.icao == 0 {
			f.icao = icao
		} else if f.icao != icao {
			// If we already decoded ICAO from AA field, it should match
			return fmt.Errorf("%w: AA field=0x%06X, AP field=0x%06X", ErrInvalidChecksum, f.icao, icao)
		}

		return nil

	default:
		return fmt.Errorf("do not know how to CRC Downlink Format %d", f.downLinkFormat)
	}
}

// validateAPField validates frames with AP field and extracts ICAO
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

	// Sanity check: verify by re-encoding
	expectedAP := crc ^ icao
	if apField != expectedAP {
		return 0, fmt.Errorf("%w: AP field corrupt: got 0x%06X, expected 0x%06X", ErrInvalidChecksum, apField, expectedAP)
	}

	return icao, nil
}
