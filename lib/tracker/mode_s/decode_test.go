package mode_s

import (
	//"fmt"
	"testing"
	"time"
)

func TestDecodeString_Odd_Frame_Length(t *testing.T) {
	_, err := DecodeString("M00", time.Now())

	if nil == err {
		t.Error("Failed to detect Odd Frame Length")
	}
}
func TestDecodeString_DF17_EVEN_LAT(t *testing.T) {
	var timeStamp = time.Now()

	frame, err := DecodeString("8D75804B580FF2CF7E9BA6F701D0", time.Now())

	if nil != err {
		t.Error("There was an error decoding the frame!", err)
		return
	}

	if frame.mode != "NORMAL" {
		t.Errorf("Exported mode NORMAL, Got: %s", frame.mode)
	}

	if frame.timeStamp.Before(timeStamp) {
		t.Error("Expected a timestamp that was after the test started, got something else")
	}

	if frame.downLinkFormat != 17 {
		t.Errorf("Downlink format not correct. expected 17, got %d", frame.downLinkFormat)
	}

	if frame.icao != 7700555 {
		// 0x75804B
		t.Errorf("Failed to decode ICAO address correctly, expected 7700555, got: %d", frame.icao)
	}

	if frame.messageType != 11 {
		t.Errorf("Expected DF Message 11 (type: %d) but got type %d", frame.MessageType(), frame.messageType)
	}

	if frame.messageSubType != 0 {
		t.Errorf("Got an Incorrect DF17 sub type")
	}

	if frame.timeFlag != 0 {
		t.Errorf("Expected time flag to not be be UTC")
	}

	if frame.cprFlagOddEven != 0 {
		t.Errorf("Expected the F Flag to be EVEN (0) - was odd instead")
	}

	if frame.altitude != 2175 {
		t.Errorf("Incorrect altitude! expected 2175 - got: %d", frame.altitude)
	}

	if frame.rawLatitude != 92095 {
		t.Errorf("Incorrectly decoded the RAW latitude for this frame. expected 92095, got %d", frame.rawLatitude)
	}
	if frame.rawLongitude != 39846 {
		t.Errorf("Incorrectly decoded the RAW latitude for this frame. expected 39846, got %d", frame.rawLongitude)
	}

}

func TestDecodeString_DF17_ODD_LAT(t *testing.T) {
	var timeStamp = time.Now()

	frame, err := DecodeString("8D75804B580FF6B283EB7A157117", time.Now())

	if nil != err {
		t.Error("There was an error decoding the frame!", err)
		return
	}

	if frame.mode != "NORMAL" {
		t.Errorf("Exported mode NORMAL, Got: %s", frame.mode)
	}

	if frame.timeStamp.Before(timeStamp) {
		t.Error("Expected a timestamp that was after the test started, got something else")
	}

	if frame.downLinkFormat != 17 {
		t.Errorf("Downlink format not correct. expected 17, got %d", frame.downLinkFormat)
	}

	if frame.icao != 7700555 {
		// 0x75804B
		t.Errorf("Failed to decode ICAO address correctly, expected 7700555, got: %d", frame.icao)
	}

	if frame.messageType != 11 {
		t.Errorf("Expected DF Message 11 (type: %d) but got type %d", frame.MessageType(), frame.messageType)
	}

	if frame.messageSubType != 0 {
		t.Errorf("Got an Incorrect DF17 sub type")
	}

	if frame.timeFlag != 0 {
		t.Errorf("Expected time flag to not be be UTC")
	}

	if frame.cprFlagOddEven != 1 {
		t.Errorf("Expected the F Flag to be ODD (1) - was even instead")
	}

	if frame.altitude != 2175 {
		t.Errorf("Incorrect altitude! expected 2175 - got: %d", frame.altitude)
	}

	if frame.rawLatitude != 88385 {
		t.Errorf("Incorrectly decoded the RAW latitude for this frame. expected 92095, got %d", frame.rawLatitude)
	}
	if frame.rawLongitude != 125818 {
		t.Errorf("Incorrectly decoded the RAW latitude for this frame. expected 39846, got %d", frame.rawLongitude)
	}
}

func BenchmarkDecodeDF17Msg11(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = DecodeString("8D75804B580FF6B283EB7A157117", time.Now())
	}
}

//type tIcaoMessage struct {
//	msg, expectedIcao, df string
//}

//func TestIcaoDecode(t *testing.T) {
//	valid := []tIcaoMessage{
//		{"@00141275CC0A000014a0a20605;", "000000", "DF0"}, // make sure we get a 0 value if we do not know this plane yet
//		{"@0014191B25325d7c2f75b6b2cb;", "7c2f75", "DF11"}, // DF11 has an ICAO address field that we remember
//		{"@00141275CC0A000014a0a20605;", "000000", "DF0"},
//		{"@0014195109D8200010a21a4a41;", "000000", "DF4"},
//		{"@001417E8B99E28000037a2a6f7;", "000000", "DF5"},
//		{"@000000EF31C08d8960c66055972f34137e0be0a2;", "8960c6", "DF17"},
//		{"*8D76AA735893E7E3F1FC2A112A9D;", "76aa73", "DF17"},
//		{"@001419270A26a00010a22028e4a0820820da95b3;", "000000", "DF20"},
//		{"@00141A451EE6a80000372028e4a0820820905e2c;", "000000", "DF21"},
//	}
//
//	for _, sut := range valid {
//		t.Log("------------------------------")
//		t.Log("Testing Code: ", sut.msg, sut.df, sut.expectedIcao)
//		frame, err := DecodeString(sut.msg, time.Now())
//		if nil != err {
//			t.Error("Fail", err)
//		}
//		decodedIcao := fmt.Sprintf("%06x", frame.icao())
//		if sut.expectedIcao != decodedIcao {
//			t.Errorf("%s: Bad ICAO Decode: expected %s != %s actual", sut.df, sut.expectedIcao, decodedIcao)
//		}
//	}
//}

func TestCprDecode(t *testing.T) {
	type testDataType struct {
		raw              string
		icoa             string
		isEven           bool
		alt              int32
		raw_lat, raw_lon int
	}
	testData := []testDataType{
		{raw: "*8d7c4516581f76e48d95e8ab20ca;", icoa: "7c4516", isEven: false, alt: 5175, raw_lat: 94790, raw_lon: 103912},
		{raw: "*8d7c4516581f6288f83ade534ae1;", icoa: "7c4516", isEven: true, alt: 5150, raw_lat: 83068, raw_lon: 15070},

		{raw: "*8d7c4516580f06fc6d8f25d8669d;", icoa: "7c4516", isEven: false, alt: 1800, raw_lat: 97846, raw_lon: 102181},
		{raw: "*8d7c4516580df2a168340b32212a;", icoa: "7c4516", isEven: true, alt: 1775, raw_lat: 86196, raw_lon: 13323},
	}

	for i, d := range testData {
		frame, err := DecodeString(d.raw, time.Now())
		if nil != err {
			t.Error(err)
		}
		if nil == frame {
			t.FailNow()
		}
		if frame.IsEven() != d.isEven {
			t.Errorf("Failed to decode %d DF17/11 CPR Even/Odd. Should be %t, but is %t", i, d.isEven, frame.IsEven())
		}
		if frame.altitude != d.alt {
			t.Errorf("Failed to decode %d DF17/11 Alt. Should be %d, but is %d", i, d.alt, frame.altitude)
		}
		if frame.rawLatitude != d.raw_lat {
			t.Errorf("Failed to decode %d DF17/11 CPR Lat. Should be %d, but is %d", i, d.raw_lat, frame.rawLatitude)
		}
		if frame.rawLongitude != d.raw_lon {
			t.Errorf("Failed to decode %d DF17/11 CPR Lat. Should be %d, but is %d", i, d.raw_lon, frame.rawLongitude)
		}
	}
}

func TestCrcDecode(t *testing.T) {
	_, err := DecodeString("*8D76AA735893E7E3F1FC2A112A9D;", time.Now())

	if nil != err {
		t.Error(err)
	}
}

func TestBadFuzz(t *testing.T) {
	messages := []string{
		"@00000000000010",
		"88000000300000",
		"@00000000000 \n",
	}
	var err error
	var f *Frame
	for _, msg := range messages {
		f, err = DecodeString(msg, time.Now())
		if nil == err {
			t.Errorf("Bad input %s was valid", msg)
			if nil != f {
				t.Error(f.String())
			}
		}
	}
}

// make sure we can decode a full byte without it causing a problem
func TestDecodeFlightStatusErr(t *testing.T) {

	// make sure values 0-255 work, we only need 0-7
	for i := 0; i < 256; i++ {
		f := Frame{
			message: []byte{byte(i), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		}
		f.decodeFlightStatus()
	}
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
func TestFrame_decodeFlightStatus(t *testing.T) {
	type fields struct {
		fs    byte
		alert bool
		// spi?
		onGround            bool
		validVerticalStatus bool
		special             string
		message             []byte
	}
	tests := []struct {
		name   string
		fields fields
	}{
		{
			name: "000: no alert, no SPI, aircraft is airborne",
			fields: fields{
				fs:                  0,
				alert:               false,
				onGround:            false,
				validVerticalStatus: true,
				special:             "",
				message:             []byte{0},
			},
		},
		{
			name: "001: no alert, no SPI, aircraft is on-ground",
			fields: fields{
				fs:                  1,
				alert:               false,
				onGround:            true,
				validVerticalStatus: true,
				special:             "",
				message:             []byte{1},
			},
		},
		{
			name: "010: alert, no SPI, aircraft is airborne",
			fields: fields{
				fs:                  2,
				alert:               true,
				onGround:            false,
				validVerticalStatus: true,
				special:             "",
				message:             []byte{2},
			},
		},
		{
			name: "011: alert, no SPI, aircraft is on-ground",
			fields: fields{
				fs:                  3,
				alert:               true,
				onGround:            true,
				validVerticalStatus: true,
				special:             "",
				message:             []byte{3},
			},
		},
		{
			name: "100: alert, SPI, aircraft is airborne or on-ground",
			fields: fields{
				fs:                  4,
				alert:               true,
				onGround:            false,
				validVerticalStatus: false,
				special:             flightStatusTable[4],
				message:             []byte{4},
			},
		},
		{
			name: "101: no alert, SPI, aircraft is airborne or on-ground",
			fields: fields{
				fs:                  5,
				alert:               false,
				onGround:            false,
				validVerticalStatus: false,
				special:             flightStatusTable[5],
				message:             []byte{5},
			},
		},
		{
			name: "110: reserved",
			fields: fields{
				fs:                  6,
				alert:               false,
				onGround:            false,
				validVerticalStatus: false,
				special:             "",
				message:             []byte{6},
			},
		},
		{
			name: "111: not assigned",
			fields: fields{
				fs:                  7,
				alert:               false,
				onGround:            false,
				validVerticalStatus: false,
				special:             "",
				message:             []byte{7},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Frame{
				message: tt.fields.message,
			}
			f.decodeFlightStatus()

			if f.fs != tt.fields.fs {
				t.Errorf("Expected flight status field %d, got %d", tt.fields.fs, f.fs)
			}
			if f.alert != tt.fields.alert {
				t.Errorf("Expected alert field %t, got %t", tt.fields.alert, f.alert)
			}
			if f.onGround != tt.fields.onGround {
				t.Errorf("Expected onGround field %t, got %t", tt.fields.onGround, f.onGround)
			}
			if f.validVerticalStatus != tt.fields.validVerticalStatus {
				t.Errorf("Expected validVerticalStatus field %t, got %t", tt.fields.validVerticalStatus, f.validVerticalStatus)
			}
			if f.special != tt.fields.special {
				t.Errorf("Expected special field `%s`, got `%s`", tt.fields.special, f.special)
			}

			if _, ok := flightStatusTable[f.fs]; !ok {
				t.Errorf("Expected to have a flight status lookup for value %d", f.fs)
			}
		})
	}
}

func TestFrame_decodeDownLinkFormat(t *testing.T) {
	type fields struct {
		message        []byte
		downLinkFormat byte
	}
	tests := []struct {
		name   string
		fields fields
	}{
		{name: "DF0", fields: fields{message: []byte{0}, downLinkFormat: 0}},
		{name: "DF1", fields: fields{message: []byte{1 << 3}, downLinkFormat: 1}},
		{name: "DF2", fields: fields{message: []byte{2 << 3}, downLinkFormat: 2}},
		{name: "DF3", fields: fields{message: []byte{3 << 3}, downLinkFormat: 3}},
		{name: "DF4", fields: fields{message: []byte{4 << 3}, downLinkFormat: 4}},
		{name: "DF5", fields: fields{message: []byte{5 << 3}, downLinkFormat: 5}},
		{name: "DF6", fields: fields{message: []byte{6 << 3}, downLinkFormat: 6}},
		{name: "DF7", fields: fields{message: []byte{7 << 3}, downLinkFormat: 7}},
		{name: "DF8", fields: fields{message: []byte{8 << 3}, downLinkFormat: 8}},
		{name: "DF9", fields: fields{message: []byte{9 << 3}, downLinkFormat: 9}},

		{name: "DF10", fields: fields{message: []byte{10 << 3}, downLinkFormat: 10}},
		{name: "DF11", fields: fields{message: []byte{11 << 3}, downLinkFormat: 11}},
		{name: "DF12", fields: fields{message: []byte{12 << 3}, downLinkFormat: 12}},
		{name: "DF13", fields: fields{message: []byte{13 << 3}, downLinkFormat: 13}},
		{name: "DF14", fields: fields{message: []byte{14 << 3}, downLinkFormat: 14}},
		{name: "DF15", fields: fields{message: []byte{15 << 3}, downLinkFormat: 15}},
		{name: "DF16", fields: fields{message: []byte{16 << 3}, downLinkFormat: 16}},
		{name: "DF17", fields: fields{message: []byte{17 << 3}, downLinkFormat: 17}},
		{name: "DF18", fields: fields{message: []byte{18 << 3}, downLinkFormat: 18}},
		{name: "DF19", fields: fields{message: []byte{19 << 3}, downLinkFormat: 19}},

		{name: "DF20", fields: fields{message: []byte{20 << 3}, downLinkFormat: 20}},
		{name: "DF21", fields: fields{message: []byte{21 << 3}, downLinkFormat: 21}},
		{name: "DF22", fields: fields{message: []byte{22 << 3}, downLinkFormat: 22}},
		{name: "DF23", fields: fields{message: []byte{23 << 3}, downLinkFormat: 23}},
		{name: "DF24", fields: fields{message: []byte{24 << 3}, downLinkFormat: 24}},
		{name: "DF24", fields: fields{message: []byte{25 << 3}, downLinkFormat: 24}},
		{name: "DF24", fields: fields{message: []byte{26 << 3}, downLinkFormat: 24}},
		{name: "DF24", fields: fields{message: []byte{27 << 3}, downLinkFormat: 24}},
		{name: "DF24", fields: fields{message: []byte{28 << 3}, downLinkFormat: 24}},
		{name: "DF24", fields: fields{message: []byte{29 << 3}, downLinkFormat: 24}},

		{name: "DF24", fields: fields{message: []byte{30 << 3}, downLinkFormat: 24}},
		{name: "DF24", fields: fields{message: []byte{31 << 3}, downLinkFormat: 24}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Frame{
				message: tt.fields.message,
			}
			f.decodeDownLinkFormat()
			if tt.fields.downLinkFormat != f.downLinkFormat {
				t.Errorf(
					"Expected 0x%X to decode DF %d, got DF %d",
					tt.fields.message[0],
					tt.fields.downLinkFormat,
					f.downLinkFormat,
				)
			}
		})
	}
}

func TestFrame_decode13bitAltitudeCode(t *testing.T) {
	tests := map[string]int32{
		// DF17
		"8D7C6D9E582142CDA64A44211B82": 5500, // Q=true
		"8D7C6D9E5821C2CEB04B35256969": 5700, // Q=true
		"8D7C6C48580C228548352C857006": 5800, // Q=false

		// DF20
		"A0001416C759B9263E97D798A8DD": 31150, // M=false, Q=true
		"A0001910CC3661B0A80000284CC8": 39000, // M=false, Q=true
		"A0000182001807144000006ACF72": 5800,  // M=false, Q=false

		// DF0
		"02E61411056201": 31025, // M=false, Q=true
		"02001428F2C704": 1100,  // M=false, Q=false

		// DF4
		"20001CB0861890": 45000, // M=false, Q=true
		"20010D8340800E": 37900, // M=false, Q=true
	}

	for avr, expectedAlt := range tests {
		t.Run(avr, func(tt *testing.T) {
			frame := NewFrame(avr, time.Now())
			if err := frame.Decode(); err != nil {
				tt.Error(err)
				return
			}

			if expectedAlt != frame.altitude {
				tt.Errorf("Expected altitude of %d, got %d (M:%t Q:%t)", expectedAlt, frame.altitude, frame.acM, frame.acQ)
			}
		})
	}
}

func BenchmarkFrame_DecodeDF0(b *testing.B) {
	t := time.Now()
	var ok bool
	for i := 0; i < b.N; i++ {
		f := NewFrameFromBytes(0, []byte{0x00, 0x05, 0x03, 0x19, 0xAB, 0x8C, 0x22}, t)
		err := f.Decode()
		if err != nil {
			b.Error(err)
		}
		if ok, err = f.OnGround(); ok || err != nil {
			b.Error("on ground?", ok, err)
		}
	}
}
