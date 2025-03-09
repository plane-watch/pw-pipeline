// package modes2
package modes2

import (
	"fmt"
	"reflect"
	"testing"
)

func TestAvrFrame_DownlinkFormat(t *testing.T) {
	type fields struct {
		a uint64
		b uint64
	}
	tests := []struct {
		name   string
		fields fields
		want   byte
	}{
		{name: "DF0", fields: fields{a: uint64(0) << 59}, want: 0},
		{name: "DF1", fields: fields{a: uint64(1) << 59}, want: 1},
		{name: "DF2", fields: fields{a: uint64(2) << 59}, want: 2},
		{name: "DF3", fields: fields{a: uint64(3) << 59}, want: 3},
		{name: "DF4", fields: fields{a: uint64(4) << 59}, want: 4},
		{name: "DF5", fields: fields{a: uint64(5) << 59}, want: 5},
		{name: "DF6", fields: fields{a: uint64(6) << 59}, want: 6},
		{name: "DF7", fields: fields{a: uint64(7) << 59}, want: 7},
		{name: "DF8", fields: fields{a: uint64(8) << 59}, want: 8},
		{name: "DF9", fields: fields{a: uint64(9) << 59}, want: 9},
		{name: "DF10", fields: fields{a: uint64(10) << 59}, want: 10},
		{name: "DF11", fields: fields{a: uint64(11) << 59}, want: 11},
		{name: "DF12", fields: fields{a: uint64(12) << 59}, want: 12},
		{name: "DF13", fields: fields{a: uint64(13) << 59}, want: 13},
		{name: "DF14", fields: fields{a: uint64(14) << 59}, want: 14},
		{name: "DF15", fields: fields{a: uint64(15) << 59}, want: 15},
		{name: "DF16", fields: fields{a: uint64(16) << 59}, want: 16},
		{name: "DF17", fields: fields{a: uint64(17) << 59}, want: 17},
		{name: "DF18", fields: fields{a: uint64(18) << 59}, want: 18},
		{name: "DF19", fields: fields{a: uint64(19) << 59}, want: 19},
		{name: "DF20", fields: fields{a: uint64(20) << 59}, want: 20},
		{name: "DF21", fields: fields{a: uint64(21) << 59}, want: 21},
		{name: "DF22", fields: fields{a: uint64(22) << 59}, want: 22},
		{name: "DF23", fields: fields{a: uint64(23) << 59}, want: 23},

		{name: "DF24", fields: fields{a: uint64(24) << 59}, want: 24},
		{name: "DF24", fields: fields{a: uint64(25) << 59}, want: 24},
		{name: "DF24", fields: fields{a: uint64(26) << 59}, want: 24},
		{name: "DF24", fields: fields{a: uint64(27) << 59}, want: 24},
		{name: "DF24", fields: fields{a: uint64(28) << 59}, want: 24},
		{name: "DF24", fields: fields{a: uint64(29) << 59}, want: 24},
		{name: "DF24", fields: fields{a: uint64(30) << 59}, want: 24},
		{name: "DF24", fields: fields{a: uint64(31) << 59}, want: 24},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := AvrFrame{
				a: tt.fields.a,
				b: tt.fields.b,
			}
			if got := a.DownlinkFormat(); got != tt.want {
				t.Errorf("DownlinkFormat() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFromBytes112(t *testing.T) {
	type args struct {
		buf []byte
	}
	tests := []struct {
		name    string
		args    args
		want    AvrFrame
		wantStr string
		wantDF  byte
	}{
		{
			name: "DF17 (04/0) Aircraft Identification and Category",
			args: args{
				buf: []byte{0x8D, 0x76, 0xCE, 0x88, 0x20, 0x4C, 0x90, 0x72, 0xCB, 0x48, 0x20, 0x9A, 0x50, 0x4D},
			},
			want: AvrFrame{
				a:        0b10001101_01110110_11001110_10001000_00100000_01001100_10010000_01110010,
				b:        0b11001011_01001000_00100000_10011010_01010000_01001101_00000000_00000000,
				len:      14,
				checkSum: 0x9a504d,
				DF:       17,
			},
			wantStr: "8D76CE88204C9072CB48209A504D",
			wantDF:  17,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			avr := FromBytes112(tt.args.buf)
			if !reflect.DeepEqual(avr, tt.want) {
				t.Errorf("FromBytes112() = \na:%064b, b:%064b,\nwant\na:%064b, b:%064b", avr.a, avr.b, tt.want.a, tt.want.b)
				t.Errorf("\ngot: %#v != want: %#v", avr, tt.want)
			}
			if avr.DownlinkFormat() != tt.wantDF {
				t.Errorf("AvrFrame.DownlinkFormat() = %d, want %d", avr.DownlinkFormat(), tt.wantDF)
			}
			if avr.String() != tt.wantStr {
				t.Errorf("AvrFrame.String() = %s, want %s", avr.String(), tt.wantStr)
			}
		})
	}
}

func TestFromBytes56(t *testing.T) {
	type args struct {
		buf []byte
	}
	tests := []struct {
		name    string
		args    args
		want    AvrFrame
		wantStr string
		wantDF  byte
	}{
		{
			name: "DF0",
			args: args{
				buf: []byte{0x00, 0x05, 0x03, 0x19, 0xAB, 0x8C, 0x22},
			},
			want: AvrFrame{
				a:        0b00000000_00000101_00000011_00011001_10101011_10001100_00100010_00000000,
				b:        0,
				len:      7,
				checkSum: 0xd7f778,
				DF:       0,
			},
			wantStr: "00050319AB8C22",
			wantDF:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			avr := FromBytes56(tt.args.buf)
			if !reflect.DeepEqual(avr, tt.want) {
				t.Errorf("FromBytes56() = \na:%064b, b:%064b,\nwant\na:%064b, b:%064b", avr.a, avr.b, tt.want.a, tt.want.b)
				t.Errorf("\ngot: %#v != want: %#v", avr, tt.want)
			}
			if avr.DownlinkFormat() != tt.wantDF {
				t.Errorf("AvrFrame.DownlinkFormat() = %d, want %d", avr.DownlinkFormat(), tt.wantDF)
			}
			if avr.String() != tt.wantStr {
				t.Errorf("AvrFrame.String() = %s, want %s", avr.String(), tt.wantStr)
			}
		})
	}
}

func TestAvrFrame_ICAO(t *testing.T) {
	tests := []struct {
		df     byte
		input  []byte
		want   string
		decode func([]byte) AvrFrame
	}{
		{
			df:    0,
			input: []byte{0x00, 0x05, 0x03, 0x19, 0xAB, 0x8C, 0x22},
			want:  "7C7B5A",
		},
		{
			df:    4,
			input: []byte{0x21, 0x00, 0x00, 0x99, 0x2F, 0x8C, 0x48},
			want:  "7C7539",
		},
		{
			df:    5,
			input: []byte{0x28, 0x00, 0x1B, 0x1F, 0x21, 0x81, 0xF6},
			want:  "7C1B28",
		},
		{
			df:    11,
			input: []byte{0x5F, 0x7C, 0x7B, 0x5A, 0xBB, 0x4F, 0x87},
			want:  "7C7B5A",
		},
		{
			df:    16,
			input: []byte{0x80, 0x61, 0x90, 0x22, 0x58, 0x82, 0x2E, 0xFC /*b2*/, 0x8B, 0x94, 0x86, 0xFD, 0xA3, 0xBF},
			want:  "7C431F",
		},
		{
			df:    17,
			input: []byte{0x8D, 0x7C, 0x77, 0x94, 0x78, 0x65, 0x87, 0x83, 0xF6, 0xEF, 0x13, 0x89, 0xB3, 0x9E},
			want:  "7C7794",
		},
		{
			df:    18,
			input: []byte{0x90, 0x7C, 0xF7, 0xC6, 0xC1, 0x04, 0x00, 0x00, 0x00, 0x20, 0x04, 0x6E, 0xFB, 0xAC},
			want:  "7CF7C6",
		},
		{
			df:    20,
			input: []byte{0xA0, 0x00, 0x03, 0x36, 0x10, 0x02, 0x0A, 0x80, 0xF0, 0x00, 0x00, 0x27, 0x0B, 0xAA},
			want:  "7C1666",
		},
		{
			df:    21,
			input: []byte{0xA8, 0x00, 0x11, 0x89, 0x20, 0x58, 0xF6, 0xB9, 0xC3, 0x8D, 0xA0, 0x9C, 0x6D, 0x38},
			want:  "7C1BE8",
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("DF%d", tt.df), func(t *testing.T) {
			var frame AvrFrame
			if len(tt.input) == 7 {
				frame = FromBytes56(tt.input)
			} else {
				frame = FromBytes112(tt.input)
			}
			icao := frame.ICAO()

			if got := fmt.Sprintf("%X", icao); got != tt.want {
				t.Errorf("AvrFrame.ICAO(%d) = %v, want %v", tt.df, got, tt.want)
			}
		})
	}
}
