package mode_s

import (
	"fmt"
	"testing"
	"time"
)

func TestDecodeDF17BaroAlt1(t *testing.T) {
	frame, err := DecodeString("*8d7c4a08581fa28e6038b87a2e88;", time.Now())
	if nil != err {
		t.Error(err)
		t.FailNow()
	}

	if a, _ := frame.Altitude(); a != 5250 {
		t.Errorf("DF17 Baro Alt: Failed to decode altitude field correctly 5250 != %d", a)
	}

	sicao := fmt.Sprintf("%x", frame.Icao())
	if "7c4a08" != sicao {
		t.Errorf("Did not correctly decode the ICAO address: %s != 7c4a08", sicao)
	}
}

// Beast AVR MLAT Timestamp Format
func TestDecodeDF17BaroAlt2(t *testing.T) {
	frame, err := DecodeString("@000000EF31C08d8960c66055972f34137e0be0a2;", time.Now())
	if nil != err {
		t.Error(err)
		return
	}

	if a, _ := frame.Altitude(); a != 16025 {
		t.Errorf("DF17 Baro Alt: Failed to decode altitude field correctly 16025 != %d", a)
	}

	sicao := fmt.Sprintf("%x", frame.Icao())
	if "8960c6" != sicao {
		t.Errorf("Did not correctly decode the ICAO address: 8960c6 != %s", sicao)
	}
}

// mlat format test
func TestDecodeDF17BaroAlt3(t *testing.T) {
	frame, err := DecodeString("@000A237DD8708d7c6c9a583fa2c5422ad9e99abb;", time.Now())
	if nil != err {
		t.Error(err)
		return
	}

	if a, _ := frame.Altitude(); a != 11650 {
		t.Errorf("DF17 Baro Alt: Failed to decode altitude field correctly 16025 != %d", a)
	}

	sicao := fmt.Sprintf("%x", frame.Icao())
	if "7c6c9a" != sicao {
		t.Errorf("Did not correctly decode the ICAO address: 7c6c9a != %s", sicao)
	}
}

// airborne velocity
func TestDecodeDF17MT19ST1(t *testing.T) {
	frame, err := DecodeString("8D7C451C99C4182CA0A4164A8C70", time.Now())
	if nil != err {
		t.Error(err)
	}
	if nil == frame {
		t.Error("Failed to decode frame")
		t.FailNow()
	}

	if 17 != frame.DownLinkType() {
		t.Error("Strange, I swore that this was an ADS-B frame (type 17)")
	}

	if 5 != frame.ca {
		t.Errorf("Capability should be 5, got %d", frame.ca)
	}

	if "7C451C" != frame.IcaoStr() {
		t.Errorf("Failed to decode ICAO. expected 7C451C, got %s", frame.IcaoStr())
	}

	if 19 != frame.messageType || 1 != frame.messageSubType {
		t.Errorf("Expected ADS-B Frame 19, subtype 1. Got: %d:%d", frame.messageType, frame.messageSubType)
	}

	if 1 != frame.eastWestDirection {
		t.Errorf("Expected plane to be going west (1), but instead got: %d", frame.eastWestDirection)
	}
	if -23 != frame.eastWestVelocity {
		t.Errorf("Expected plane to be going west @ 23 (-23), got %d", frame.eastWestVelocity)
	}

	if 0 != frame.northSouthDirection {
		t.Errorf("Expected plane to be going north (0), but instead got: %d", frame.northSouthDirection)
	}
	if 356 != frame.northSouthVelocity {
		t.Errorf("Expected plane to be going north @ 356 (356), got %d", frame.northSouthVelocity)
	}
	if frame.superSonic {
		t.Errorf("Wow, this plane is going a lot faster than it should be! why is it thinking it is supersonic?")
	}
}

func TestBeastAvrTimestampDecode112BitModeS(t *testing.T) {
	// info taken from https://wiki.jetvision.de/wiki/Mode-S_Beast:Data_Output_Formats#:~:text=The%20Mode%2DS%20Beast%20supports,time%20and%20signal%20level%20information

	raw := "@016CE3671AA88D00199A8BB80030A80006520316;"
	t1 := time.Now()
	frame, err := DecodeString(raw, t1)
	if nil != err || nil == frame {
		t.Errorf("Failed to decode frame: %s", err)
		return
	}

	if raw != frame.full {
		t.Errorf("Failed to set the correct full frame. want: %s, got: %s", raw, frame.full)
	}
	if "MLAT" != frame.mode {
		t.Errorf("Failed to identify frame as Beast AVR")
	}
}

func Test_calcSurfaceSpeed(t *testing.T) {
	type args struct {
		value uint64
	}
	tests := []struct {
		name  string
		args  args
		want  float64
		want1 bool
	}{
		{name: "not avail", args: args{value: 0}, want: 0.0, want1: false},

		{name: "stopped", args: args{value: 1}, want: 0.0, want1: true},

		{name: "slowest", args: args{value: 2}, want: 0.125, want1: true},
		{name: "slowest", args: args{value: 3}, want: 0.25, want1: true},
		{name: "slowest", args: args{value: 4}, want: 0.375, want1: true},
		{name: "slowest", args: args{value: 5}, want: 0.5, want1: true},
		{name: "slowest", args: args{value: 6}, want: 0.625, want1: true},
		{name: "slowest", args: args{value: 7}, want: 0.75, want1: true},
		{name: "slowest", args: args{value: 8}, want: 0.875, want1: true},

		{name: "slow", args: args{value: 9}, want: 1, want1: true},
		{name: "slow", args: args{value: 10}, want: 1.25, want1: true},
		{name: "slow", args: args{value: 11}, want: 1.5, want1: true},
		{name: "slow", args: args{value: 12}, want: 1.75, want1: true},

		{name: "crawling", args: args{value: 13}, want: 2, want1: true},
		{name: "crawling", args: args{value: 14}, want: 2.5, want1: true},
		{name: "crawling", args: args{value: 15}, want: 3, want1: true},
		{name: "crawling", args: args{value: 16}, want: 3.5, want1: true},
		{name: "crawling", args: args{value: 17}, want: 4, want1: true},
		{name: "crawling", args: args{value: 18}, want: 4.5, want1: true},
		{name: "crawling", args: args{value: 19}, want: 5, want1: true},
		{name: "crawling", args: args{value: 20}, want: 5.5, want1: true},
		{name: "crawling", args: args{value: 21}, want: 6, want1: true},
		{name: "crawling", args: args{value: 22}, want: 6.5, want1: true},
		{name: "crawling", args: args{value: 23}, want: 7, want1: true},
		{name: "crawling", args: args{value: 24}, want: 7.5, want1: true},
		{name: "crawling", args: args{value: 25}, want: 8, want1: true},
		{name: "crawling", args: args{value: 26}, want: 8.5, want1: true},
		{name: "crawling", args: args{value: 27}, want: 9, want1: true},
		{name: "crawling", args: args{value: 28}, want: 9.5, want1: true},
		{name: "crawling", args: args{value: 29}, want: 10, want1: true},
		{name: "crawling", args: args{value: 30}, want: 10.5, want1: true},
		{name: "crawling", args: args{value: 31}, want: 11, want1: true},
		{name: "crawling", args: args{value: 32}, want: 11.5, want1: true},
		{name: "crawling", args: args{value: 33}, want: 12, want1: true},
		{name: "crawling", args: args{value: 34}, want: 12.5, want1: true},
		{name: "crawling", args: args{value: 35}, want: 13, want1: true},
		{name: "crawling", args: args{value: 36}, want: 13.5, want1: true},
		{name: "crawling", args: args{value: 37}, want: 14, want1: true},
		{name: "crawling", args: args{value: 38}, want: 14.5, want1: true},

		{name: "going", args: args{value: 39}, want: 15, want1: true},
		{name: "going", args: args{value: 40}, want: 16, want1: true},
		{name: "going", args: args{value: 41}, want: 17, want1: true},
		{name: "going", args: args{value: 42}, want: 18, want1: true},
		{name: "going", args: args{value: 43}, want: 19, want1: true},
		{name: "going", args: args{value: 44}, want: 20, want1: true},
		{name: "going", args: args{value: 45}, want: 21, want1: true},
		{name: "going", args: args{value: 46}, want: 22, want1: true},
		{name: "going", args: args{value: 47}, want: 23, want1: true},
		{name: "going", args: args{value: 48}, want: 24, want1: true},
		{name: "going", args: args{value: 49}, want: 25, want1: true},
		{name: "going", args: args{value: 50}, want: 26, want1: true},
		{name: "going", args: args{value: 51}, want: 27, want1: true},
		{name: "going", args: args{value: 52}, want: 28, want1: true},
		{name: "going", args: args{value: 53}, want: 29, want1: true},
		{name: "going", args: args{value: 54}, want: 30, want1: true},
		{name: "going", args: args{value: 55}, want: 31, want1: true},
		{name: "going", args: args{value: 56}, want: 32, want1: true},
		{name: "going", args: args{value: 57}, want: 33, want1: true},
		// .. skip a few
		{name: "going", args: args{value: 91}, want: 67, want1: true},
		{name: "going", args: args{value: 92}, want: 68, want1: true},
		{name: "going", args: args{value: 93}, want: 69, want1: true},

		{name: "moving", args: args{value: 94}, want: 70, want1: true},
		{name: "moving", args: args{value: 95}, want: 72, want1: true},
		{name: "moving", args: args{value: 96}, want: 74, want1: true},
		{name: "moving", args: args{value: 97}, want: 76, want1: true},
		{name: "moving", args: args{value: 98}, want: 78, want1: true},
		{name: "moving", args: args{value: 99}, want: 80, want1: true},
		{name: "moving", args: args{value: 100}, want: 82, want1: true},
		{name: "moving", args: args{value: 101}, want: 84, want1: true},
		{name: "moving", args: args{value: 102}, want: 86, want1: true},
		{name: "moving", args: args{value: 103}, want: 88, want1: true},
		{name: "moving", args: args{value: 104}, want: 90, want1: true},
		{name: "moving", args: args{value: 105}, want: 92, want1: true},
		{name: "moving", args: args{value: 106}, want: 94, want1: true},
		{name: "moving", args: args{value: 107}, want: 96, want1: true},
		{name: "moving", args: args{value: 108}, want: 98, want1: true},

		{name: "outtahere", args: args{value: 109}, want: 100, want1: true},
		{name: "outtahere", args: args{value: 110}, want: 105, want1: true},
		{name: "outtahere", args: args{value: 111}, want: 110, want1: true},
		{name: "outtahere", args: args{value: 112}, want: 115, want1: true},
		{name: "outtahere", args: args{value: 113}, want: 120, want1: true},
		{name: "outtahere", args: args{value: 114}, want: 125, want1: true},
		{name: "outtahere", args: args{value: 115}, want: 130, want1: true},
		{name: "outtahere", args: args{value: 116}, want: 135, want1: true},
		{name: "outtahere", args: args{value: 117}, want: 140, want1: true},
		{name: "outtahere", args: args{value: 118}, want: 145, want1: true},
		{name: "outtahere", args: args{value: 119}, want: 150, want1: true},
		{name: "outtahere", args: args{value: 120}, want: 155, want1: true},
		{name: "outtahere", args: args{value: 121}, want: 160, want1: true},
		{name: "outtahere", args: args{value: 122}, want: 165, want1: true},
		{name: "outtahere", args: args{value: 123}, want: 170, want1: true},

		{name: "gone", args: args{value: 124}, want: 175, want1: true},

		{name: "reserved", args: args{value: 125}, want: 0, want1: false},
		{name: "reserved", args: args{value: 126}, want: 0, want1: false},
		{name: "reserved", args: args{value: 127}, want: 0, want1: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := calcSurfaceSpeed(tt.args.value)
			if got != tt.want {
				t.Errorf("calcSurfaceSpeed() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("calcSurfaceSpeed() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func TestAltitudeDecode(t *testing.T) {
	frame, err := DecodeString("*8D7C7DAA582886FB218A9AFB0420;", time.Now())
	if nil != err {
		t.Error(err)
	}
	if nil == frame {
		t.FailNow()
	}
	a, err := frame.Altitude()
	if nil != err {
		t.Error(err)
	}
	if 600 != a {
		t.Errorf("Expected an altitude of 600 feet, got %d", a)
	}
}

// frame type decode test

func TestDecodeDF17MT28(t *testing.T) {
	tests := []struct {
		name      string
		frame     string
		emergency bool
		cap       int
	}{
		{name: fmt.Sprintf("DF17/MT28/ST01 %s", DF17FrameEmergencyPriority), frame: "8C7C4A0CE104BC0000000069DE1A", cap: 4},
		{name: fmt.Sprintf("DF17/MT28/ST01 %s", DF17FrameEmergencyPriority), frame: "8D7C4A0CE101950000000095FC54", cap: 5},
		{name: fmt.Sprintf("DF17/MT28/ST01 %s", DF17FrameEmergencyPriority), frame: "8D7C4A0CE104BC0000000031AF62", cap: 5},
		{name: fmt.Sprintf("DF17/MT28/ST01 %s", DF17FrameEmergencyPriority), frame: "8F7C4A0CE104BC00000000814D92", cap: 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame, err := DecodeString(tt.frame, time.Now())
			if frame == nil || nil != err {
				t.Error(err, "failed to decode")
				return
			}
			if 17 != frame.DownLinkType() {
				t.Error("Should have been DF17")
			}
			if 28 != frame.MessageType() {
				t.Error("Should have been Message Type 28")
			}
			if 1 != frame.MessageSubType() {
				t.Error("Should have been Message Type 28")
			}

			if "7C4A0C" != frame.IcaoStr() {
				t.Errorf("Invalid ICAO. 7C4A0C != %s", frame.IcaoStr())
			}

			// todo: determine more tests
		})
	}
}

func TestDecodeDF17MT29(t *testing.T) {
	tests := []struct {
		name     string
		frame    string
		onGround bool
	}{
		{name: fmt.Sprintf("DF17/MT29/ST02 %s", DF17FrameTargetStateStatus), frame: "8D7C4A0CEA0000000000005D4CDC", onGround: false},
		{name: fmt.Sprintf("DF17/MT29/ST02 %s", DF17FrameTargetStateStatus), frame: "8D7C4A0CEA00085FBD3F04D4F47E", onGround: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame, err := DecodeString(tt.frame, time.Now())
			if frame == nil || nil != err {
				t.Error(err, "failed to decode")
				return
			}
			if 17 != frame.DownLinkType() {
				t.Error("Should have been DF17")
			}
			if 29 != frame.MessageType() {
				t.Error("Should have been Message Type 29")
			}
			if "7C4A0C" != frame.IcaoStr() {
				t.Errorf("Invalid ICAO. 7C4A0C != %s", frame.IcaoStr())
			}

			// todo: determine more tests
		})
	}
}

func TestDecodeDF17MT31(t *testing.T) {
	tests := []struct {
		name     string
		frame    string
		onGround bool
	}{
		{name: fmt.Sprintf("DF17/MT31/ST00 Airborne %s", DF17FrameAircraftOperational), frame: "8D7C4A0CF80300030049B8BA7984", onGround: false},
		{name: fmt.Sprintf("DF17/MT31/ST01 Ground %s", DF17FrameAircraftOperational), frame: "8C7C4A0CF9004103834938E42BD4", onGround: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame, err := DecodeString(tt.frame, time.Now())
			if frame == nil || nil != err {
				t.Error(err, "failed to decode")
				return
			}
			if 17 != frame.DownLinkType() {
				t.Error("Should have been DF17")
			}
			if 31 != frame.MessageType() {
				t.Error("Should have been Message Type 31")
			}
			if "7C4A0C" != frame.IcaoStr() {
				t.Errorf("Invalid ICAO. 7C4A0C != %s", frame.IcaoStr())
			}
			if !frame.VerticalStatusValid() {
				t.Error("Failed to decode vertical status")
			}
			if frame.onGround != tt.onGround {
				t.Errorf("Ground Status should be %t, but was %t", tt.onGround, frame.onGround)
			}

			// todo: determine more tests
		})
	}
}
