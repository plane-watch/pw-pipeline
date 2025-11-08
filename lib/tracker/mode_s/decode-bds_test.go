package mode_s

import (
	"testing"
)

// Test message constants - valid Comm-B messages for testing
var (
	// validBDS20Callsign is a valid BDS 2,0 message with a proper call sign
	validBDS20CallSign = []byte{0b0010_0000, 0b0100_1100, 0b1001_0000, 0b0111_0010, 0b1100_1011, 0b0100_1000, 0b0010_0000}

	// validBDS10 is a valid BDS 1,0 Data Link Capability Report
	validBDS10 = []byte{0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}

	// emptyResponse is an all-zeros Comm-B response
	emptyResponse = []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}

	// invalidBDS20 has invalid BDS code for BDS 2,0
	invalidBDS20 = []byte{0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}

	// invalidCallSign has BDS 2,0 code but invalid cal sign characters
	invalidCallSign = []byte{0x20, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
)

// Test helper to create a Frame with MB field for Comm-B testing
func createFrameWithMB(mb []byte) *Frame {
	// Create a minimal DF20 frame (14 bytes)
	// DF20 = 0b10100 (bits 0-4) = 0xA0
	message := make([]byte, 14)
	message[0] = 0xA0 // DF20
	// Copy MB field (bytes 4-10)
	copy(message[4:11], mb)

	return &Frame{
		message:        message,
		downLinkFormat: 20,
		dr:             0, // No downlink request
		um:             0, // No utility message
	}
}

func Test_inferCommBMessageType(t *testing.T) {
	type args struct {
		mb []byte
	}
	// Logic for the args. If we need to specify bits for detection they are in 0b binary notation. 0's and 0xFF's are junk data
	tests := []struct {
		name    string
		args    args
		want    byte
		want1   byte
		wantErr bool
	}{
		{
			name:    "Correct Length",
			args:    args{mb: []byte{}},
			want:    0,
			want1:   0,
			wantErr: true,
		},
		{
			name:    "Infer BDS 1.0",
			args:    args{mb: []byte{0b0001_0000, 0b1000_0011, 0, 0xFF, 0, 0xFF, 0}},
			want:    1,
			want1:   0,
			wantErr: false,
		},
		{
			name:    "Infer BDS 1.7",
			args:    args{mb: []byte{0b0000_0010, 0xFF, 0xFF, 0b1111_0000, 0b0, 0b0, 0b0}},
			want:    1,
			want1:   7,
			wantErr: false,
		},
		{
			name:    "Infer BDS 2.0",
			args:    args{mb: []byte{0b0010_0000, 0b0100_1100, 0b1001_0000, 0b0111_0010, 0b1100_1011, 0b0100_1000, 0b0010_0000}},
			want:    2,
			want1:   0,
			wantErr: false,
		},
		{
			name:    "Infer BDS 3.0 1",
			args:    args{mb: []byte{0b0011_0000, 0b1111_1110, 0b0011_1100, 0b0000_1000, 0xFF, 0xFF, 0xFF}},
			want:    3,
			want1:   0,
			wantErr: false,
		},
		{
			name:    "Infer BDS 3.0 2",
			args:    args{mb: []byte{0b0011_0000, 0b1111_1110, 0b0100_1100, 0b0000_0100, 0xFF, 0xFF, 0xFF}},
			want:    3,
			want1:   0,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1, err := inferCommBMessageType(tt.args.mb)
			if (err != nil) != tt.wantErr {
				t.Errorf("inferCommBMessageType() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("inferCommBMessageType() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("inferCommBMessageType() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

// TestGetBitHelper tests the getBit helper function
func TestGetBitHelper(t *testing.T) {
	data := []byte{0b10101010, 0b11001100, 0b11110000}

	tests := []struct {
		bitIndex int
		expected uint
	}{
		{1, 1}, // First bit of first byte
		{2, 0},
		{3, 1},
		{4, 0},
		{5, 1},
		{6, 0},
		{7, 1},
		{8, 0},
		{9, 1}, // First bit of second byte
		{10, 1},
		{11, 0},
		{12, 0},
		{17, 1}, // First bit of third byte
		{24, 0}, // Last bit of third byte
		{25, 0}, // Out of bounds
	}

	for _, tt := range tests {
		result := getBit(data, tt.bitIndex)
		if result != tt.expected {
			t.Errorf("getBit(%d) = %d, want %d", tt.bitIndex, result, tt.expected)
		}
	}
}

// TestGetBitsHelper tests the getBits helper function
func TestGetBitsHelper(t *testing.T) {
	data := []byte{0b10101010, 0b11001100, 0b11110000}

	tests := []struct {
		startBit int
		endBit   int
		expected uint64
	}{
		{1, 8, 0b10101010},          // Full first byte
		{1, 4, 0b1010},              // First 4 bits
		{5, 8, 0b1010},              // Last 4 bits of first byte
		{1, 16, 0b1010101011001100}, // First two bytes
		{9, 16, 0b11001100},         // Full second byte
		{1, 3, 0b101},               // First 3 bits
	}

	for _, tt := range tests {
		result := getBits(data, tt.startBit, tt.endBit)
		if result != tt.expected {
			t.Errorf("getBits(%d, %d) = %064b, want %064b",
				tt.startBit, tt.endBit, result, tt.expected)
		}
	}
}

// TestDecodeEmptyResponse tests the empty response handler
func TestDecodeEmptyResponse(t *testing.T) {
	tests := []struct {
		name     string
		mb       []byte
		expected int
	}{
		{
			name:     "All zeros",
			mb:       []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			expected: 56,
		},
		{
			name:     "One non-zero byte",
			mb:       []byte{0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00},
			expected: 0,
		},
		{
			name:     "All ones",
			mb:       []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := createFrameWithMB(tt.mb)
			score := decodeEmptyResponse(f, tt.mb, false)
			if score != tt.expected {
				t.Errorf("decodeEmptyResponse() = %d, want %d", score, tt.expected)
			}
		})
	}
}

// TestDecodeBDS10 tests BDS 1,0 decoder
func TestDecodeBDS10(t *testing.T) {
	tests := []struct {
		name     string
		mb       []byte
		expected int
	}{
		{
			name:     "Valid BDS 1,0",
			mb:       []byte{0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			expected: 56,
		},
		{
			name:     "Invalid BDS code",
			mb:       []byte{0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			expected: 0,
		},
		{
			name:     "Reserved bits not zero",
			mb:       []byte{0x10, 0b00100000, 0x00, 0x00, 0x00, 0x00, 0x00},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := createFrameWithMB(tt.mb)
			score := decodeBDS10(f, tt.mb, false)
			if score != tt.expected {
				t.Errorf("decodeBDS10() = %d, want %d", score, tt.expected)
			}

			// Test store mode if valid
			if score > 0 {
				decodeBDS10(f, tt.mb, true)
				if f.bdsMajor != 1 || f.bdsMinor != 0 {
					t.Errorf("BDS type not stored correctly: %d.%d", f.bdsMajor, f.bdsMinor)
				}
			}
		})
	}
}

// TestDecodeBDS20 tests BDS 2,0 decoder
func TestDecodeBDS20(t *testing.T) {
	tests := []struct {
		name     string
		mb       []byte
		expected int
	}{
		{
			name:     "Valid Call Sign",
			mb:       validBDS20CallSign,
			expected: 56,
		},
		{
			name:     "Invalid BDS code",
			mb:       invalidBDS20,
			expected: 0,
		},
		{
			name:     "Call sign with invalid characters",
			mb:       invalidCallSign,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := createFrameWithMB(tt.mb)
			score := decodeBDS20(f, tt.mb, false)
			if score != tt.expected {
				t.Errorf("decodeBDS20() = %d, want %d", score, tt.expected)
			}

			// Test store mode if valid
			if score > 0 {
				decodeBDS20(f, tt.mb, true)
				if f.bdsMajor != 2 || f.bdsMinor != 0 {
					t.Errorf("BDS type not stored correctly: %d.%d", f.bdsMajor, f.bdsMinor)
				}
			}
		})
	}
}

// TestCommBScoringSystem tests the overall scoring framework
func TestCommBScoringSystem(t *testing.T) {
	tests := []struct {
		name         string
		mb           []byte
		expectedBDS  string
		shouldDecode bool
	}{
		{
			name:         "Clear BDS 2,0 winner",
			mb:           validBDS20CallSign,
			expectedBDS:  "2.0",
			shouldDecode: true,
		},
		{
			name:         "Clear BDS 1,0 winner",
			mb:           validBDS10,
			expectedBDS:  "1.0",
			shouldDecode: true,
		},
		{
			name:         "Empty response",
			mb:           emptyResponse,
			expectedBDS:  "0.0",
			shouldDecode: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := createFrameWithMB(tt.mb)
			err := f.decodeCommB()
			if err != nil {
				t.Fatalf("decodeCommB() error = %v", err)
			}

			if tt.shouldDecode {
				bdsType := f.BdsMessageType()
				if bdsType != tt.expectedBDS {
					t.Errorf("BDS type = %s, want %s", bdsType, tt.expectedBDS)
				}
			}
		})
	}
}

// TestDRUMFiltering tests that DR/UM field filtering works
func TestDRUMFiltering(t *testing.T) {
	mb := validBDS20CallSign

	tests := []struct {
		name         string
		dr           byte
		um           byte
		shouldDecode bool
	}{
		{
			name:         "DR=0, UM=0 - should decode",
			dr:           0,
			um:           0,
			shouldDecode: true,
		},
		{
			name:         "DR≠0 - should skip",
			dr:           1,
			um:           0,
			shouldDecode: false,
		},
		{
			name:         "UM≠0 - should skip",
			dr:           0,
			um:           1,
			shouldDecode: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := createFrameWithMB(mb)
			f.dr = tt.dr
			f.um = tt.um

			err := f.decodeCommB()
			if err != nil {
				t.Fatalf("decodeCommB() error = %v", err)
			}

			decoded := f.bdsMajor != 0 || f.bdsMinor != 0
			if decoded != tt.shouldDecode {
				t.Errorf("Message decoded = %v, want %v (DR=%d, UM=%d)",
					decoded, tt.shouldDecode, tt.dr, tt.um)
			}
		})
	}
}
