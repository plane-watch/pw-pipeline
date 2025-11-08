package mode_s

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestAPFieldValidation tests AP field CRC validation (DF 0, 4, 5, 16, 20, 21, 24)
func TestAPFieldValidation(t *testing.T) {
	tests := []struct {
		name        string
		frame       string
		expectedDF  byte
		expectedICAO string
	}{
		{
			name:        "DF0 - Short air-air surveillance",
			frame:       "*00050319AB8C22;",
			expectedDF:  0,
			expectedICAO: "7C7B5A",
		},
		{
			name:        "DF4 - Surveillance altitude reply",
			frame:       "*210000992F8C48;",
			expectedDF:  4,
			expectedICAO: "7C7539",
		},
		{
			name:        "DF5 - Surveillance identity reply",
			frame:       "28001B1F2181F6;",
			expectedDF:  5,
			expectedICAO: "7C1B28",
		},
		{
			name:        "DF16 - Long air-air surveillance",
			frame:       "8061902258822EFC8B9486FDA3BF",
			expectedDF:  16,
			expectedICAO: "7C431F",
		},
		{
			name:        "DF20 - Comm-B altitude reply",
			frame:       "A000033610020A80F00000270BAA;",
			expectedDF:  20,
			expectedICAO: "7C1666",
		},
		{
			name:        "DF21 - Comm-B identity reply",
			frame:       "A80011892058F6B9C38DA09C6D38",
			expectedDF:  21,
			expectedICAO: "7C1BE8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := DecodeString(tt.frame, time.Now())
			if err != nil {
				t.Fatalf("Failed to decode frame: %v", err)
			}
			if f == nil {
				t.Fatal("Frame is nil")
			}

			if f.downLinkFormat != tt.expectedDF {
				t.Errorf("Wrong DF: got %d, want %d", f.downLinkFormat, tt.expectedDF)
			}

			if got := f.IcaoStr(); got != tt.expectedICAO {
				t.Errorf("ICAO mismatch: got %s, want %s", got, tt.expectedICAO)
			}
		})
	}
}

// TestPIFieldValidation tests PI field CRC validation (DF 11, 17, 18)
func TestPIFieldValidation(t *testing.T) {
	tests := []struct {
		name        string
		frame       string
		expectedDF  byte
		expectedICAO string
	}{
		{
			name:        "DF17 - Extended squitter",
			frame:       "*8D4840D6202CC371C32CE0576098;",
			expectedDF:  17,
			expectedICAO: "4840D6",
		},
		{
			name:        "DF17 - Airborne position",
			frame:       "*8D75804B580FF2CF7E9BA6F701D0;",
			expectedDF:  17,
			expectedICAO: "75804B",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := DecodeString(tt.frame, time.Now())
			if err != nil {
				t.Fatalf("Failed to decode frame: %v", err)
			}
			if f == nil {
				t.Fatal("Frame is nil")
			}

			if f.downLinkFormat != tt.expectedDF {
				t.Errorf("Wrong DF: got %d, want %d", f.downLinkFormat, tt.expectedDF)
			}

			if got := f.IcaoStr(); got != tt.expectedICAO {
				t.Errorf("ICAO mismatch: got %s, want %s", got, tt.expectedICAO)
			}
		})
	}
}

// TestCorruptedFrames tests that corrupted frames are rejected
func TestCorruptedFrames(t *testing.T) {
	validFrames := []string{
		"*8D4840D6202CC371C32CE0576098;", // DF17
		// Note: AP fields (DF0, 4) can't detect corruption - they just extract different ICAO
		// Only PI fields (DF11, 17, 18) can detect bit errors
	}

	for _, validFrame := range validFrames {
		t.Run(validFrame, func(t *testing.T) {
			// First verify the valid frame works
			f, err := DecodeString(validFrame, time.Now())
			if err != nil {
				t.Fatalf("Valid frame failed: %v", err)
			}

			t.Logf("Original: %X, DF=%d, ICAO=%s", f.message, f.downLinkFormat, f.IcaoStr())

			// Now corrupt it and verify it fails
			corrupted := CorruptCRC(f.message)
			corruptedHex := strings.ToUpper(fmt.Sprintf("%X", corrupted))

			t.Logf("Corrupted: %s", corruptedHex)

			fc, err := DecodeString(corruptedHex, time.Now())
			t.Logf("Decode error: %v", err)

			if err == nil {
				t.Errorf("Corrupted frame was accepted! Frame: %s", corruptedHex)
				if fc != nil {
					t.Logf("Corrupted frame ICAO: %s, checkSum: 0x%06X", fc.IcaoStr(), fc.checkSum)
				}
			}
		})
	}
}

// TestMLATFrameValidation tests that MLAT frames are validated
func TestMLATFrameValidation(t *testing.T) {
	tests := []struct {
		name        string
		frame       string
		expectedICAO string
		shouldPass  bool
	}{
		{
			name:        "Valid MLAT DF17",
			frame:       "@000000EF31C08D4840D6202CC371C32CE0576098;",
			expectedICAO: "4840D6",
			shouldPass:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := DecodeString(tt.frame, time.Now())

			if tt.shouldPass {
				if err != nil {
					t.Errorf("Valid MLAT frame rejected: %v", err)
				}
				if f != nil && f.IcaoStr() != tt.expectedICAO {
					t.Errorf("ICAO mismatch: got %s, want %s", f.IcaoStr(), tt.expectedICAO)
				}
			} else {
				if err == nil {
					t.Errorf("Invalid MLAT frame was accepted")
				}
			}
		})
	}
}

// TestComputePIField tests the PI field computation utility
func TestComputePIField(t *testing.T) {
	// Test with a known DF17 frame
	// DF17, CA=5, ICAO=4840D6, then message data
	message := []byte{
		0x8D, 0x48, 0x40, 0xD6, // DF17, CA, ICAO
		0x20, 0x2C, 0xC3, 0x71, 0xC3, 0x2C, 0xE0, // Message data
		0x00, 0x00, 0x00, // Zero CRC to be computed
	}

	result := ComputePIField(message)

	// Decode and verify
	f := NewFrameFromBytes(0, result, time.Now())
	err := f.Decode()
	if err != nil {
		t.Errorf("Generated frame failed validation: %v", err)
	}

	// The computed frame should pass CRC check
	if f.checkSum != 0 {
		t.Errorf("CRC check failed: checksum = 0x%06X (should be 0)", f.checkSum)
	}
}

// TestComputeAPField tests the AP field computation utility
func TestComputeAPField(t *testing.T) {
	icao := uint32(0x7C7B5A)

	// DF0 frame without AP field
	message := []byte{
		0x00, 0x05, 0x03, 0x19, // DF0 data
		0x00, 0x00, 0x00, // Zero AP to be computed
	}

	result := ComputeAPField(message, icao)

	// Decode and verify
	f := NewFrameFromBytes(0, result, time.Now())
	err := f.Decode()
	if err != nil {
		t.Errorf("Generated frame failed validation: %v", err)
	}

	if f.icao != icao {
		t.Errorf("ICAO mismatch: got 0x%06X, want 0x%06X", f.icao, icao)
	}
}

// TestCRCRoundTrip tests generating and validating frames
func TestCRCRoundTrip(t *testing.T) {
	t.Run("PI Field Round Trip", func(t *testing.T) {
		// Create a DF17 frame
		message := []byte{
			0x8D, 0xAB, 0xCD, 0xEF, // DF17, CA, ICAO
			0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Message data
			0x00, 0x00, 0x00, // CRC to be computed
		}

		withCRC := ComputePIField(message)

		// Decode and verify
		f := NewFrameFromBytes(0, withCRC, time.Now())
		if err := f.Decode(); err != nil {
			t.Errorf("Round trip failed: %v", err)
		}

		if f.icao != 0xABCDEF {
			t.Errorf("ICAO mismatch: got 0x%06X, want 0xABCDEF", f.icao)
		}
	})

	t.Run("AP Field Round Trip", func(t *testing.T) {
		icao := uint32(0x123456)
		message := []byte{
			0x20, 0x00, 0x00, 0x00, // DF4 data
			0x00, 0x00, 0x00, // AP to be computed
		}

		withAP := ComputeAPField(message, icao)

		// Decode and verify
		f := NewFrameFromBytes(0, withAP, time.Now())
		if err := f.Decode(); err != nil {
			t.Errorf("Round trip failed: %v", err)
		}

		if f.icao != icao {
			t.Errorf("ICAO mismatch: got 0x%06X, want 0x%06X", f.icao, icao)
		}
	})
}

// Benchmark CRC validation performance
func BenchmarkPIFieldValidation(b *testing.B) {
	frame := "*8D4840D6202CC371C32CE0576098;"
	f, _ := DecodeString(frame, time.Now())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = f.checkCrc()
	}
}

func BenchmarkAPFieldValidation(b *testing.B) {
	frame := "*00050319AB8C22;"
	f, _ := DecodeString(frame, time.Now())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = f.checkCrc()
	}
}
