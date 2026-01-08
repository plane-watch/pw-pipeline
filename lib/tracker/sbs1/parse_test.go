package sbs1

import (
	"fmt"
	"testing"
	"time"
)

func TestIcaoStringToInt(t *testing.T) {
	sut := "7C1BE8"
	expected := uint32(8133608)
	icaoAddr, err := icaoStringToInt(sut)
	if err != nil {
		t.Error(err)
	}
	if icaoAddr != expected {
		t.Errorf("Expected %s to decode to %d, but got %d", sut, expected, icaoAddr)
	}
}

func TestKeepAliveNoOp(t *testing.T) {
	trimableStrings := []string{
		"",
		"\n",
		"\r",
		"\r\n",
		" ",
		" \n",
		"  ",
		"  \n",
		"   ",
		"   \n",
		"    ",
		"    \n",
		"\n\n",
	}

	for _, s := range trimableStrings {
		f := NewFrame(s)
		if nil != f.Parse() {
			t.Errorf("Should not have gotten an error for a newline")
		}
	}
}

func TestBadInputErrors(t *testing.T) {
	testStrings := []string{
		",",
		",,,,,,,,",
		",,,,,,,,,,,,,,,,,,,,",
	}

	for i, s := range testStrings {
		t.Run(fmt.Sprintf("bad string %d", i), func(tt *testing.T) {
			f := NewFrame(s)
			if nil == f.Parse() {
				t.Errorf("we should have errored for bad string %d - %s", i, s)
			}
		})
	}
}

func TestNewFrame(t *testing.T) {
	type args struct {
		sbsString string
	}
	tests := []struct {
		name string
		args args
		want *Frame
	}{
		{
			name: "MSG1",
			args: args{sbsString: "MSG,3,49,2425,7503DF,748059,2016/06/03,08:00:00.489,2016/06/03,08:00:00.568,,2650,,,-32.09214,115.92078,,,0,0,0,0"},
			want: &Frame{
				MsgType:      "MSG",
				original:     "MSG,3,49,2425,7503DF,748059,2016/06/03,08:00:00.489,2016/06/03,08:00:00.568,,2650,,,-32.09214,115.92078,,,0,0,0,0",
				icaoStr:      "7503DF",
				IcaoInt:      0x7503DF,
				Received:     time.Date(2016, 06, 03, 8, 00, 00, 489000000, time.UTC),
				CallSign:     "",
				Altitude:     2650,
				GroundSpeed:  0,
				Track:        0,
				Lat:          -32.09214,
				Lon:          115.92078,
				VerticalRate: 0,
				Squawk:       "",
				Alert:        "0",
				Emergency:    "0",
				OnGround:     false,
				HasPosition:  true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewFrame(tt.args.sbsString)
			if err := got.Parse(); err != nil {
				t.Error("Should not have errored", err)
			}
			if got.MsgType != tt.want.MsgType {
				t.Errorf("wrong value for MsgType: expected %s got %s", tt.want.MsgType, got.MsgType)
			}
			if got.original != tt.want.original {
				t.Errorf("wrong value for original: expected %s got %s", tt.want.original, got.original)
			}
			if got.icaoStr != tt.want.icaoStr {
				t.Errorf("wrong value for icaoStr: expected %s got %s", tt.want.icaoStr, got.icaoStr)
			}
			if got.IcaoInt != tt.want.IcaoInt {
				t.Errorf("wrong value for IcaoInt: expected %d got %d", tt.want.IcaoInt, got.IcaoInt)
			}
			if got.Received != tt.want.Received {
				t.Errorf("wrong value for Received: expected %s got %s", tt.want.Received, got.Received)
			}
			if got.CallSign != tt.want.CallSign {
				t.Errorf("wrong value for CallSign: expected %s got %s", tt.want.CallSign, got.CallSign)
			}
			if got.Altitude != tt.want.Altitude {
				t.Errorf("wrong value for Altitude: expected %d got %d", tt.want.Altitude, got.Altitude)
			}
			if got.GroundSpeed != tt.want.GroundSpeed {
				t.Errorf("wrong value for GroundSpeed: expected %d got %d", tt.want.GroundSpeed, got.GroundSpeed)
			}
			if got.Track != tt.want.Track {
				t.Errorf("wrong value for Track: expected %0f got %f", tt.want.Track, got.Track)
			}
			if fmt.Sprintf("%0.6f", got.Lat) != fmt.Sprintf("%0.6f", tt.want.Lat) {
				t.Errorf("wrong value for Lat: expected %0.6f got %0.6f", tt.want.Lat, got.Lat)
			}
			if got.Lon != tt.want.Lon {
				t.Errorf("wrong value for Lon: expected %f got %f", tt.want.Lon, got.Lon)
			}
			if got.VerticalRate != tt.want.VerticalRate {
				t.Errorf("wrong value for VerticalRate: expected %d got %d", tt.want.VerticalRate, got.VerticalRate)
			}
			if got.Squawk != tt.want.Squawk {
				t.Errorf("wrong value for Squawk: expected %s got %s", tt.want.Squawk, got.Squawk)
			}
			if got.Alert != tt.want.Alert {
				t.Errorf("wrong value for Alert: expected %s got %s", tt.want.Alert, got.Alert)
			}
			if got.Emergency != tt.want.Emergency {
				t.Errorf("wrong value for Emergency: expected %s got %s", tt.want.Emergency, got.Emergency)
			}
			if got.OnGround != tt.want.OnGround {
				t.Errorf("wrong value for OnGround: expected %t got %t", tt.want.OnGround, got.OnGround)
			}
			if got.HasPosition != tt.want.HasPosition {
				t.Errorf("wrong value for HasPosition: expected %t got %t", tt.want.HasPosition, got.HasPosition)
			}
		})
	}
}

func Test_icaoStringToInt(t *testing.T) {
	type args struct {
		icao string
	}
	tests := []struct {
		name    string
		args    args
		want    uint32
		wantErr bool
	}{
		{
			name:    "moo",
			args:    args{icao: "moo"},
			want:    0,
			wantErr: true,
		},
		{
			name:    "blank",
			args:    args{icao: ""},
			want:    0,
			wantErr: true,
		},
		{
			name:    "A",
			args:    args{icao: "A"},
			want:    0,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := icaoStringToInt(tt.args.icao)
			t.Logf("Err: %s", err)
			if (err != nil) != tt.wantErr {
				t.Errorf("icaoStringToInt() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("icaoStringToInt() got = %v, want %v", got, tt.want)
			}
		})
	}
}
