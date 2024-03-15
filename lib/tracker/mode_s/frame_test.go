package mode_s

import (
	"testing"
)

func TestIsNoop(t *testing.T) {
	frames := []*Frame{
		{raw: ""},
		{raw: "0"},
		{raw: "@00000"},
		{raw: "*0"},
		{raw: "*0000"},
	}
	for _, f := range frames {
		if !f.isNoOp() {
			t.Errorf("Failed to detect NoOp frame: %s", f.raw)
		}
	}
}

func TestIsNotNoop(t *testing.T) {
	frames := []*Frame{
		{raw: "10"},
		{raw: "123"},
		{raw: "@123;"},
		{raw: "*3"},
		{raw: "*023"},
		{raw: "*00001"},
	}
	for _, f := range frames {
		f.full = "*" + f.raw + ";"
		if f.isNoOp() {
			t.Errorf("Failed detect non NoOp frame as NoOp: %s", f.raw)
		}
	}
}

func TestFrame_isNoOp(t *testing.T) {
	type fields struct {
		full string
	}
	type variation struct {
		name, start, end string
	}
	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{name: "noop", fields: fields{full: ""}, want: true},
		{name: "noop", fields: fields{full: "0"}, want: true},
		{name: "noop", fields: fields{full: "00"}, want: true},
		{name: "noop", fields: fields{full: "000"}, want: true},
		{name: "noop", fields: fields{full: "0000"}, want: true},
		{name: "noop", fields: fields{full: "00000"}, want: true},
		{name: "noop", fields: fields{full: "000000"}, want: true},
		{name: "noop", fields: fields{full: "0000000"}, want: true},
		{name: "noop", fields: fields{full: "00000000"}, want: true},
		{name: "noop", fields: fields{full: "000000000"}, want: true},
		{name: "noop", fields: fields{full: "0000000000"}, want: true},
		{name: "noop", fields: fields{full: "00000000000"}, want: true},
		{name: "noop", fields: fields{full: "000000000000"}, want: true},
		{name: "noop", fields: fields{full: "0000000000000"}, want: true},
		{name: "noop", fields: fields{full: "00000000000000"}, want: true},
		{name: "noop", fields: fields{full: "000000000000000"}, want: true},
		{name: "noop", fields: fields{full: "0000000000000000"}, want: true},
		{name: "noop", fields: fields{full: "00000000000000000"}, want: true},
		{name: "noop", fields: fields{full: "000000000000000000"}, want: true},
		{name: "noop", fields: fields{full: "0000000000000000000"}, want: true},
		{name: "noop", fields: fields{full: "00000000000000000000"}, want: true},
		{name: "noop", fields: fields{full: "000000000000000000000"}, want: true},
		{name: "noop", fields: fields{full: "0000000000000000000000"}, want: true},
		{name: "noop", fields: fields{full: "00000000000000000000000"}, want: true},
		{name: "noop", fields: fields{full: "000000000000000000000000"}, want: true},
		{name: "noop", fields: fields{full: "0000000000000000000000000"}, want: true},
		{name: "noop", fields: fields{full: "00000000000000000000000000"}, want: true},
		{name: "noop", fields: fields{full: "000000000000000000000000000"}, want: true},
		{name: "noop", fields: fields{full: "0000000000000000000000000000"}, want: true},
		{name: "bad", fields: fields{full: "1"}, want: false},
		{name: "bad", fields: fields{full: "12"}, want: false},
		{name: "bad", fields: fields{full: "123"}, want: false},
		{name: "bad", fields: fields{full: "1234"}, want: false},
		{name: "bad", fields: fields{full: "12345"}, want: false},
		{name: "bad", fields: fields{full: "123456"}, want: false},
		{name: "bad", fields: fields{full: "1234567"}, want: false},
		{name: "bad", fields: fields{full: "12345678"}, want: false},
		{name: "bad", fields: fields{full: "123456789"}, want: false},
		{name: "bad", fields: fields{full: "1234567890"}, want: false},
		{name: "bad", fields: fields{full: "12345678901"}, want: false},
		{name: "bad", fields: fields{full: "123456789012"}, want: false},
		{name: "bad", fields: fields{full: "1234567890123"}, want: false},
		{name: "bad", fields: fields{full: "12345678901234"}, want: false},
		{name: "bad", fields: fields{full: "123456789012345"}, want: false},
		{name: "bad", fields: fields{full: "1234567890123456"}, want: false},
		{name: "bad", fields: fields{full: "12345678901234567"}, want: false},
	}
	variations := []variation{
		{name: "empty/", start: "", end: ""},
		{name: "star+semicolon/", start: "*", end: ";"},
	}
	for _, v := range variations {
		for _, tt := range tests {
			t.Run(v.name+tt.name, func(t *testing.T) {
				f := &Frame{
					full: v.start + tt.fields.full + v.end,
				}

				if got := f.isNoOp(); got != tt.want {
					t.Errorf("for `%s` isNoOp() = %v, want %v", f.full, got, tt.want)
				}
			})
		}
	}

	// test nil
	var f *Frame
	if !f.isNoOp() {
		t.Errorf("nil frames should NoOp")
	}
}

func TestFrame_VerticalRate(t *testing.T) {
	var f *Frame
	if f.VerticalRateValid() {
		t.Errorf("valid vertical rate on nil frame")
	}
	f = &Frame{
		verticalRate:      1,
		validVerticalRate: false,
	}
	if f.VerticalRateValid() {
		t.Errorf("valid vertical rate when not set")
	}
	v, err := f.VerticalRate()
	if nil == err {
		t.Errorf("did not get an error when I should have")
	}
	if 0 != v {
		t.Errorf("Got invalid value for invalid vertical rate. expected 0, got :%d", v)
	}

	f.validVerticalRate = true

	v, err = f.VerticalRate()
	if nil != err {
		t.Errorf("got an error when I should have not")
	}
	if 1 != v {
		t.Errorf("Got wrong value for vertical rate")
	}
}

func TestFrame_DecodeAuIcaoRegistration(t *testing.T) {
	icao := uint32(0x7C0000)
	//end := uint32(0x7C822D)
	charset := "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	for char1 := 0; char1 < 36; char1++ {
		for char2 := 0; char2 < 36; char2++ {
			for char3 := 0; char3 < 36; char3++ {
				expected := "VH-" + string(charset[char1]) + string(charset[char2]) + string(charset[char3])
				f := Frame{icao: icao}

				s, err := f.DecodeAuIcaoRegistration()
				if icao > 0x7C822D {
					// expect an error
					if nil == err {
						t.Errorf("Should not have decoded icao %X", icao)
					}
				} else {
					if nil != err {
						t.Error(err)
					}
					if *s != expected {
						t.Errorf("Did not decode correctly. Expected %s, got: %s", expected, *s)
					}
				}
				icao++
			}
		}
	}

}
