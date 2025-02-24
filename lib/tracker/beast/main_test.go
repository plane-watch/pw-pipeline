package beast

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"
)

var (
	beastModeAc     = []byte{0x1A, 0x31, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	beastModeSShort = []byte{0x1a, 0x32, 0x22, 0x1b, 0x54, 0xf0, 0x81, 0x2b, 0x26, 0x5d, 0x7c, 0x49, 0xf8, 0x28, 0xe9, 0x43}
	beastModeSLong  = []byte{0x1a, 0x33, 0x22, 0x1b, 0x54, 0xac, 0xc2, 0xe9, 0x28, 0x8d, 0x7c, 0x49, 0xf8, 0x58, 0x41, 0xd2, 0x6c, 0xca, 0x39, 0x33, 0xe4, 0x1e, 0xcf}
)

func TestNewBeastMsgModeAC(t *testing.T) {
	f, err := NewFrame(beastModeAc, false)

	if nil != err {
		t.Error("Did not get a beast message")
		return
	}

	if 0x31 != f.msgType {
		t.Error("Incorrect msg type")
	}
}

func TestNewBeastMsgModeSShort(t *testing.T) {
	f, err := NewFrame(beastModeSShort, false)

	if nil != err {
		t.Error("Did not get a beast message")
		return
	}

	if !bytes.Equal(beastModeSShort, f.raw) {
		t.Errorf(
			"Failed to copy the short beast message correctly (%s != %s)",
			fmt.Sprintf("%02X", beastModeSShort),
			fmt.Sprintf("%02X", f.raw),
		)
	}

	if f.msgType != 0x32 {
		t.Error("Incorrect msg type")
	}

	// check time stamp
	if len(f.mlatTimestamp) != 6 {
		t.Errorf("Incorrect timestamp len. expected 6, got %d", len(f.mlatTimestamp))
	}
	// check signal level - should be 0xBF
	if f.signalLevel != 38 {
		t.Errorf("Did not get the signal level correctly. expected 93: got %d", f.signalLevel)
	}
	// make sure we decode into a mode_s.Frame
	if len(f.body) != 7 {
		t.Errorf("Incorrect body len. expected 7, got %d", len(f.body))
	}
}

func TestNewBeastMsgModeSLong(t *testing.T) {
	f, err := NewFrame(beastModeSLong, false)

	if nil != err {
		t.Errorf("Did not get a beast message: %s", err)
		return
	}

	if !bytes.Equal(beastModeSLong, f.raw) {
		t.Error("Failed to copy the long beast message correctly")
	}

	if f.msgType != 0x33 {
		t.Error("Incorrect msg type")
	}

	// check time stamp
	if len(f.mlatTimestamp) != 6 {
		t.Errorf("Incorrect timestamp len. expected 6, got %d", len(f.mlatTimestamp))
	}
	// check signal level - should be 0xBF
	if f.signalLevel != 40 {
		t.Errorf("Did not get the signal level correctly. expected 93: got %d", f.signalLevel)
	}
	// make sure we decode into a mode_s.Frame
	if len(f.body) != 14 {
		t.Errorf("Incorrect body len. expected 7, got %d", len(f.body))
	}
}

func Test_newBeastMsg(t *testing.T) {
	type args struct {
		rawBytes []byte
	}
	tests := []struct {
		name string
		args args
		want *Frame
	}{
		{name: "empty", args: args{rawBytes: []byte{}}, want: nil},
		{name: "1", args: args{rawBytes: []byte{0}}, want: nil},
		{name: "2", args: args{rawBytes: []byte{0, 0}}, want: nil},
		{name: "3", args: args{rawBytes: []byte{0, 0, 0}}, want: nil},
		{name: "4", args: args{rawBytes: []byte{0, 0, 0, 0}}, want: nil},
		{name: "5", args: args{rawBytes: []byte{0, 0, 0, 0, 0}}, want: nil},
		{name: "6", args: args{rawBytes: []byte{0, 0, 0, 0, 0, 0}}, want: nil},
		{name: "7", args: args{rawBytes: []byte{0, 0, 0, 0, 0, 0, 0}}, want: nil},
		{name: "8", args: args{rawBytes: []byte{0, 0, 0, 0, 0, 0, 0, 0}}, want: nil},
		{name: "9", args: args{rawBytes: []byte{0, 0, 0, 0, 0, 0, 0, 0, 0}}, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewFrame(tt.args.rawBytes, false)
			if nil == err {
				t.Error("expected bad decode")
			}
		})
	}
}

func TestFrame_SignalRssi(t *testing.T) {
	tests := []struct {
		name string
		args []byte
		want string
	}{
		{name: "AC", args: beastModeAc, want: "-Inf"},
		{name: "Long", args: beastModeSShort, want: "15.8"},
		{name: "Short", args: beastModeSLong, want: "16.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			beastMsg, _ := NewFrame(tt.args, false)
			if got := fmt.Sprintf("%0.1f", beastMsg.SignalRssi()); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("newBeastMsg() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewFrame(t *testing.T) {
	frame, err := NewFrame(beastModeSShort, false)
	if nil != err {
		t.Error(err)
	}
	if err = frame.Decode(); nil != err {
		t.Error(err)
	}

	if !frame.hasDecoded {
		t.Error("Should have decoded")
	}

	if "7C49F8" != frame.IcaoStr() {
		t.Errorf("%0X failed to decode frame properly: %s is not correct", beastModeSShort, frame.IcaoStr())
	}

	if "7C49F8" != frame.AvrFrame().IcaoStr() {
		t.Errorf("AVR failed to decode frame properly: %s is not correct", frame.IcaoStr())
	}
}

var (
	messages = map[string][]byte{
		"DF00_MT00_ST00": {0x1A, 0x32, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xE1, 0x98, 0x38, 0x5F, 0x1A, 0x9D},
		"DF04_MT00_ST00": {0x1A, 0x32, 0x80, 0x61, 0xEA, 0xEA, 0x5D, 0xB0, 0x14, 0x20, 0x00, 0x17, 0x30, 0xE3, 0x07, 0x9D},
		"DF05_MT00_ST00": {0x1A, 0x32, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x28, 0x00, 0x09, 0xA3, 0xE0, 0x29, 0x52},
		"DF11_MT00_ST00": {0x1A, 0x32, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x5D, 0x48, 0xC2, 0x34, 0x18, 0x27, 0x15},
		"DF16_MT00_ST00": {0x1A, 0x33, 0x08, 0x39, 0xD4, 0x35, 0x7A, 0x17, 0x63, 0x80, 0xE1, 0x99, 0x98, 0x60, 0xCD, 0x81, 0x03, 0x4E, 0x5E, 0xAC, 0x22, 0x14, 0x15},
		"DF17_MT00_ST00": {0x1A, 0x33, 0x11, 0x92, 0x20, 0x74, 0x7B, 0xCD, 0x35, 0x8F, 0x4B, 0xAA, 0x74, 0x00, 0x53, 0x20, 0x00, 0x00, 0x00, 0x00, 0x72, 0x10, 0x75},
		"DF17_MT02_ST00": {0x1A, 0x33, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x8C, 0x49, 0xF0, 0x88, 0x12, 0xCB, 0x2C, 0xF7, 0x18, 0x61, 0x86, 0x01, 0xFD, 0x07},
		"DF17_MT03_ST00": {0x1A, 0x33, 0x14, 0x93, 0xFF, 0x2D, 0xD3, 0xC7, 0x62, 0x8D, 0x48, 0xFC, 0x83, 0x1C, 0x4D, 0x04, 0xCD, 0x14, 0x48, 0x20, 0x72, 0x37, 0xC3},
		"DF17_MT04_ST00": {0x1A, 0x33, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x8D, 0x4B, 0x8D, 0xEE, 0x23, 0x0C, 0x12, 0x78, 0xC3, 0x4C, 0x20, 0x40, 0x2C, 0xA1},
		"DF17_MT07_ST00": {0x1A, 0x33, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x8C, 0x40, 0x62, 0x50, 0x38, 0x1F, 0x57, 0x66, 0x9D, 0xBA, 0xF8, 0x7C, 0xB4, 0xB2},
		"DF17_MT11_ST00": {0x1A, 0x33, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x8D, 0x47, 0x1F, 0x89, 0x58, 0xC3, 0x81, 0x90, 0xF9, 0x47, 0x04, 0xB6, 0x51, 0xAA},
		"DF17_MT12_ST00": {0x1A, 0x33, 0x02, 0xC9, 0x2F, 0x30, 0x87, 0xD4, 0x20, 0x8D, 0x3C, 0x49, 0xEE, 0x60, 0xB5, 0x11, 0x10, 0x2C, 0xA2, 0x0E, 0x48, 0x92, 0x85},
		"DF17_MT13_ST00": {0x1A, 0x33, 0x21, 0x91, 0xCE, 0x58, 0xB3, 0x9E, 0x33, 0x8D, 0x48, 0xFC, 0x83, 0x68, 0x53, 0x41, 0x3C, 0xA6, 0x5D, 0x76, 0x0A, 0xE9, 0xCD},
		"DF17_MT18_ST00": {0x1A, 0x33, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x8D, 0x15, 0x1E, 0x73, 0x90, 0xBF, 0x04, 0x6E, 0xCA, 0xA0, 0xB4, 0xCF, 0x29, 0xD4},
		"DF17_MT19_ST01": {0x1A, 0x33, 0x80, 0x61, 0xEA, 0xEA, 0xE7, 0xA0, 0x09, 0x8D, 0x48, 0x58, 0x76, 0x99, 0x11, 0xC8, 0x84, 0xD0, 0xA8, 0x81, 0x32, 0x41, 0xFA},
		"DF17_MT19_ST03": {0x1A, 0x33, 0x11, 0x92, 0x1A, 0xCC, 0x70, 0xE3, 0x2C, 0x8F, 0x74, 0x80, 0x26, 0x9B, 0x04, 0xEC, 0x20, 0x98, 0x0C, 0x00, 0x68, 0x49, 0xB1},
		"DF17_MT23_ST07": {0x1A, 0x33, 0x29, 0x46, 0x08, 0x8E, 0x03, 0xF2, 0x2C, 0x8D, 0x7C, 0x7A, 0xF8, 0xBF, 0x40, 0x40, 0x00, 0x00, 0x00, 0x00, 0xDD, 0x9B, 0x89},
		"DF17_MT28_ST01": {0x1A, 0x33, 0x1A, 0x1D, 0xBC, 0x48, 0x44, 0x7F, 0x18, 0x8D, 0x06, 0xA1, 0x46, 0xE1, 0x1E, 0x18, 0x00, 0x00, 0x00, 0x00, 0xA6, 0xB3, 0xC4},
		"DF17_MT29_ST02": {0x1A, 0x33, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x8F, 0x4B, 0xA8, 0x90, 0xEA, 0x4C, 0x48, 0x64, 0x01, 0x1C, 0x08, 0x33, 0x67, 0xFE},
		"DF17_MT31_ST00": {0x1A, 0x33, 0x11, 0x92, 0x19, 0xF6, 0x33, 0xEA, 0x60, 0x8D, 0x68, 0x32, 0x73, 0xF8, 0x21, 0x00, 0x02, 0x00, 0x49, 0xB8, 0xF0, 0xA2, 0xAE},
		"DF17_MT31_ST01": {0x1A, 0x33, 0x0B, 0xB9, 0xB4, 0x5B, 0xC7, 0xAE, 0x28, 0x8C, 0x40, 0x62, 0x50, 0xF9, 0x00, 0x26, 0x03, 0x83, 0x49, 0x38, 0xF6, 0xB2, 0x79},
		"DF18_MT00_ST00": {0x1A, 0x33, 0x00, 0xD0, 0x11, 0xB0, 0xCA, 0x83, 0xD0, 0x91, 0x20, 0x10, 0x2A, 0xC1, 0x05, 0x0D, 0x37, 0xBD, 0x83, 0xF0, 0x5E, 0x9E, 0x53},
		"DF18_MT02_ST00": {0x1A, 0x33, 0x01, 0x96, 0xAA, 0xD1, 0x09, 0xDF, 0xB4, 0x90, 0xC1, 0xE1, 0xA7, 0x13, 0x65, 0x64, 0x94, 0x63, 0x38, 0x20, 0x5C, 0xEC, 0xCC},
		"DF18_MT05_ST00": {0x1A, 0x33, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x90, 0x49, 0xF0, 0xE2, 0x28, 0x00, 0x01, 0x9E, 0x76, 0x0B, 0xF4, 0xE2, 0x0F, 0x1D},
		"DF18_MT06_ST00": {0x1A, 0x33, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x90, 0x49, 0xF0, 0x85, 0x30, 0x00, 0x01, 0x99, 0x8A, 0x09, 0xCF, 0x43, 0x56, 0x31},
		"DF18_MT07_ST00": {0x1A, 0x33, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x90, 0xC1, 0xE5, 0xA6, 0x39, 0x9E, 0x45, 0x03, 0x2D, 0xFB, 0x75, 0x5F, 0x23, 0x04},
		"DF18_MT08_ST00": {0x1A, 0x33, 0x00, 0xD0, 0x13, 0xAA, 0xB9, 0x9E, 0x35, 0x90, 0x11, 0x2C, 0xCC, 0x40, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xE3, 0xB0, 0xB8},
		"DF18_MT24_ST01": {0x1A, 0x33, 0x00, 0xD0, 0x11, 0xAC, 0xDE, 0xDF, 0x36, 0x90, 0x11, 0x2C, 0xCC, 0xC1, 0xAB, 0x01, 0xE0, 0x19, 0xEB, 0x71, 0x64, 0x7B, 0xD5},
		"DF18_MT31_ST01": {0x1A, 0x33, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x90, 0xC1, 0xE5, 0xE1, 0xF9, 0x02, 0x00, 0x00, 0x00, 0x3B, 0x20, 0xD6, 0x57, 0xC1},
		"DF20_MT00_ST00": {0x1A, 0x33, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xA0, 0x00, 0x17, 0xB1, 0xB1, 0x29, 0xFB, 0x30, 0xE0, 0x04, 0x00, 0x2D, 0x88, 0xFB},
		"DF21_MT00_ST00": {0x1A, 0x33, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xA8, 0x00, 0x08, 0x00, 0x99, 0x6C, 0x09, 0xF0, 0xA8, 0x00, 0x00, 0xC8, 0xCE, 0x43},
		"DF24_MT00_ST00": {0x1A, 0x33, 0x04, 0x92, 0xE3, 0x82, 0x04, 0x84, 0x1E, 0xC5, 0x53, 0x2D, 0x86, 0x50, 0xF3, 0x51, 0x5B, 0x29, 0xBE, 0x13, 0x0D, 0xBA, 0xAD},
	}
	keys = []string{`DF00_MT00_ST00`, `DF04_MT00_ST00`, `DF05_MT00_ST00`, `DF11_MT00_ST00`, `DF16_MT00_ST00`, `DF17_MT00_ST00`, `DF17_MT02_ST00`, `DF17_MT03_ST00`, `DF17_MT04_ST00`, `DF17_MT07_ST00`, `DF17_MT11_ST00`, `DF17_MT12_ST00`, `DF17_MT13_ST00`, `DF17_MT18_ST00`, `DF17_MT19_ST01`, `DF17_MT19_ST03`, `DF17_MT23_ST07`, `DF17_MT28_ST01`, `DF17_MT29_ST02`, `DF17_MT31_ST00`, `DF17_MT31_ST01`, `DF18_MT00_ST00`, `DF18_MT02_ST00`, `DF18_MT05_ST00`, `DF18_MT06_ST00`, `DF18_MT07_ST00`, `DF18_MT08_ST00`, `DF18_MT24_ST01`, `DF18_MT31_ST01`, `DF20_MT00_ST00`, `DF21_MT00_ST00`, `DF24_MT00_ST00`}
)

func BenchmarkNewFrame2Only(b *testing.B) {
	for _, name := range keys {
		arg := messages[name]
		b.Run(name, func(bb *testing.B) {
			for n := 0; n < bb.N; n++ {
				if _, err := NewFrame(arg, false); nil != err {
					bb.Error(err)
				}
			}
		})
	}
}

func BenchmarkNewFrameAndDecode(b *testing.B) {
	for _, name := range keys {
		arg := messages[name]
		b.Run(name, func(bb *testing.B) {
			var frame *Frame
			var err error
			for n := 0; n < bb.N; n++ {
				frame, err = NewFrame(arg, false)
				if nil != err {
					b.Error(err)
				}
				if err = frame.Decode(); nil != err {
					bb.Error(err)
				}
			}
		})
	}
}

func BenchmarkNewFrameAndDecodePool(b *testing.B) {
	UsePoolAllocator = true
	for _, name := range keys {
		arg := messages[name]
		b.Run(name, func(bb *testing.B) {
			var frame *Frame
			var err error
			for n := 0; n < bb.N; n++ {
				frame, err = NewFrame(arg, false)
				if nil != err {
					b.Error(err)
				}
				if err = frame.Decode(); nil != err {
					bb.Error(err)
				}
				Release(frame)
			}
		})
	}
}
