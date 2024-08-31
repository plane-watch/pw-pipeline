package main

import (
	"reflect"
	"testing"
)

func TestAvrFrame_DecodeDF0(t *testing.T) {
	tests := []struct {
		name    string
		fields  []byte
		want    DF0
		wantErr bool
	}{
		{
			name:   "In Air",
			fields: []byte{0x00, 0x05, 0x03, 0x19, 0xAB, 0x8C, 0x22},
			want: DF0{
				ICAO:                0x7C7B5A,
				Altitude:            4025,
				AltUnit:             AltitudeUnitFeet,
				ReplyInformation:    0b1010,
				SensitivityLevel:    0,
				CrosslinkCapability: false,
				OnGround:            false,
			},
			wantErr: false,
		},
		{
			name:   "In Air Crosslink",
			fields: []byte{0x02, 0x81, 0x83, 0x35, 0xD9, 0xDA, 0x51},
			want: DF0{
				ICAO:                0x7C1BE8,
				Altitude:            4325,
				AltUnit:             AltitudeUnitFeet,
				ReplyInformation:    0b0011,
				SensitivityLevel:    4,
				CrosslinkCapability: true,
				OnGround:            false,
			},
			wantErr: false,
		},
		{
			name:   "On Ground Crosslink",
			fields: []byte{0x06, 0x04, 0x90, 0x00, 0xEA, 0x15, 0x6D},
			want: DF0{
				ICAO:                0x3C83A7,
				Altitude:            -800,
				AltUnit:             AltitudeUnitFeet,
				ReplyInformation:    0b1001,
				SensitivityLevel:    0,
				CrosslinkCapability: true,
				OnGround:            true,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := FromBytes56(tt.fields)
			got, err := a.DecodeDF0()
			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeDF0() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DecodeDF0() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAvrFrame_DecodeDF4(t *testing.T) {
	tests := []struct {
		name    string
		fields  []byte
		want    DF4
		wantErr bool
	}{
		{
			name:   "In Air",
			fields: []byte{0x20, 0x00, 0x06, 0xA2, 0xDE, 0x8B, 0x1C},
			want: DF4{
				ICAO:            0x7C1B28,
				Altitude:        10_000,
				AltUnit:         AltitudeUnitFeet,
				FlightStatus:    0,
				DownlinkRequest: 0,
				UtilityRequest:  0,
				OnGround:        false,
				Emergency:       false,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := FromBytes56(tt.fields)
			if a.df != 4 {
				t.Errorf("Incorrect DF type, got=%d, want=%d", a.df, 4)
				return
			}
			got, err := a.DecodeDF4()
			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeDF4(0x%X) error = %v, wantErr %v", tt.fields, err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DecodeDF4(0x%X) got = %v, want %v", tt.fields, got, tt.want)
			}
		})
	}
}

func TestAvrFrame_DecodeDF5(t *testing.T) {
	tests := []struct {
		name    string
		fields  []byte
		want    DF5
		wantErr bool
	}{
		{
			name:   "In Air",
			fields: []byte{0x28, 0x00, 0x1B, 0x1F, 0x21, 0x81, 0xF6},
			want: DF5{
				ICAO:            0x7C1B28,
				Squawk:          3657,
				FlightStatus:    0,
				DownlinkRequest: 0,
				UtilityRequest:  0,
				OnGround:        false,
				Emergency:       false,
			},
			wantErr: false,
		},
		{
			name:   "On Ground",
			fields: []byte{0x29, 0x00, 0x1B, 0x3A, 0xF4, 0x7E, 0x76},
			want: DF5{
				ICAO:            0x7C1474,
				Squawk:          3751,
				FlightStatus:    1,
				DownlinkRequest: 0,
				UtilityRequest:  0,
				OnGround:        true,
				Emergency:       false,
			},
			wantErr: false,
		},
		{
			name:   "Max Squawk",
			fields: []byte{0x29, 0x00, 0x1F, 0xBF, 0x74, 0x15, 0x5A},
			want: DF5{
				ICAO:            0x3C83A7,
				Squawk:          7777,
				FlightStatus:    1,
				DownlinkRequest: 0,
				UtilityRequest:  0,
				OnGround:        true,
				Emergency:       false,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := FromBytes56(tt.fields)
			if a.df != 5 {
				t.Errorf("Incorrect DF type, got=%d, want=%d", a.df, 5)
				return
			}

			got, err := a.DecodeDF5()
			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeDF5() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DecodeDF5() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func BenchmarkAvrFrame_DecodeDF0(b *testing.B) {
	for i := 0; i < b.N; i++ {
		f := FromBytes56([]byte{0x00, 0x05, 0x03, 0x19, 0xAB, 0x8C, 0x22})
		df0, err := f.DecodeDF0()
		if err != nil {
			b.Error(err)
		}
		if df0.OnGround {
			b.Error("on ground?")
		}
	}
}

func TestAvrFrame_DecodeDF11(t *testing.T) {
	tests := []struct {
		name    string
		fields  []byte
		want    DF11
		wantErr bool
	}{
		{
			name:   "DF11 Valid",
			fields: []byte{0x5F, 0x7C, 0x7B, 0x5A, 0xBB, 0x4F, 0x87},
			want: DF11{
				ICAO:       0x7C7B5A,
				Capability: 7,
			},
			wantErr: false,
		},
		{
			name:    "DF11 failing checksum",
			fields:  []byte{0x5F, 0x7C, 0x7B, 0x5A, 0xBB, 0x4F, 0x88},
			want:    DF11{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := FromBytes56(tt.fields)
			if a.df != 11 {
				t.Errorf("Incorrect DF type, got=%d, want=%d", a.df, 11)
				return
			}
			got, err := a.DecodeDF11()
			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeDF11() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DecodeDF11() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAvrFrame_DecodeDF16(t *testing.T) {
	tests := []struct {
		name    string
		fields  []byte
		want    DF16
		wantErr bool
	}{
		{
			name:   "Airborne",
			fields: []byte{0x80, 0x61, 0x94, 0x20, 0x58, 0xA2, 0x0A, 0xA1, 0x0C, 0x3A, 0x1E, 0x6E, 0xE7, 0xCD},
			want: DF16{
				ICAO:             0x7C431F,
				Altitude:         2400,
				CommVMsg:         0b00000000_01011000_10100010_00001010_10100001_00001100_00111010_00011110,
				AltUnit:          AltitudeUnitFeet,
				OnGround:         false,
				TCASSensitivity:  0b011,
				ReplyInformation: 0b0011,
			},
			wantErr: false,
		},
		{
			name:   "On Ground",
			fields: []byte{0x80, 0x61, 0x94, 0x20, 0x58, 0xA2, 0x0A, 0xA1, 0x0C, 0x3A, 0x1E, 0x6E, 0xE7, 0xCD},
			want: DF16{
				ICAO:             0x7C431F,
				Altitude:         2400,
				CommVMsg:         0b00000000_01011000_10100010_00001010_10100001_00001100_00111010_00011110,
				AltUnit:          AltitudeUnitFeet,
				OnGround:         false,
				TCASSensitivity:  0b011,
				ReplyInformation: 0b0011,
			},
			wantErr: false,
		},
		{
			name:   "Invalid On Ground",
			fields: []byte{0x84, 0x83, 0x4D, 0xAB, 0x4D, 0x94, 0x0F, 0x66, 0x52, 0x1E, 0x0E, 0x9A, 0xD8, 0xDE},
			want: DF16{
				ICAO:             0x3D71F3,
				Altitude:         35900,
				CommVMsg:         0b00000000_01001101_10010100_00001111_01100110_01010010_00011110_00001110,
				AltUnit:          AltitudeUnitFeet,
				OnGround:         true,
				TCASSensitivity:  0b100,
				ReplyInformation: 0b0110,
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := FromBytes112(tt.fields)
			if a.df != 16 {
				t.Errorf("Incorrect DF type, got=%d, want=%d", a.df, 16)
				return
			}
			got, err := a.DecodeDF16()
			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeDF16() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DecodeDF16() got = %v, want %v", got, tt.want)
			}
		})
	}
}
