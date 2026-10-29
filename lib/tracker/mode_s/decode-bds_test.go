package mode_s

import (
	"encoding/hex"
	"math"
	"strings"
	"testing"
	"time"
)

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

// Comm-B register support, one row per register the decoder knows the name
// of (bdsFields), in order. Each row says what the decoder does with a reply
// carrying that register:
//
//   - decoded:     the register is inferred and its fields are read
//   - unsupported: the decoder never infers it; the reply comes out unknown
//
// Real replies were heard near Perth on 2026-10-22 and their values checked
// against pyModeS 3.6. Synthetic MB fields are built from the ICAO Doc 9871
// layouts with typical values; pyModeS infers no register from them either.
// BDS 0,5 to 0,9 are extended squitters, never Comm-B replies, so they have
// no row.
func TestCommBRegisters(t *testing.T) {
	tests := []struct {
		bds     string
		support string // decoded, recognised, unsupported
		msg     string // full DF20/21 frame, or "" to build one from mb
		mb      string // 7-byte MB field, synthetic rows
		check   func(t *testing.T, f *Frame)
	}{
		{bds: BdsElsDataLinkCap, support: "decoded", msg: "a00001b310020a80e000005de699",
			check: func(t *testing.T, f *Frame) {
				got := f.DataLinkCapability()
				want := &DataLinkCapability{
					OverlayCommand: true, SubnetworkVersion: 5, SpecificServices: true,
					AircraftIdent: true, Squitter: true, SurveillanceIdent: true,
				}
				if got == nil || *got != *want {
					t.Errorf("data link capability\n got %+v\nwant %+v", got, want)
				}
			}},
		{bds: BdsElsGicbCap, support: "decoded", msg: "a00003b3fe01e10000000096530c",
			check: func(t *testing.T, f *Frame) {
				g := f.GicbCapability()
				want := []string{"0.5", "0.6", "0.7", "0.8", "0.9", "0.A", "2.0", "5.0", "5.1", "5.2", "5.3", "6.0"}
				if g == nil || strings.Join(g.Supported, " ") != strings.Join(want, " ") {
					t.Errorf("supported registers %v, want %v", g, want)
				}
				if g.Has(BdsEhsSelVertIntent) || !g.Has(BdsEhsHeadingSpeed) {
					t.Errorf("Has: 4,0 %v 6,0 %v", g.Has(BdsEhsSelVertIntent), g.Has(BdsEhsHeadingSpeed))
				}
			}},
		{bds: BdsElsAircraftIdent, support: "decoded", msg: "a8000489200d4cb0ca08207b5f82",
			check: func(t *testing.T, f *Frame) {
				if strings.TrimSpace(f.FlightNumber()) != "CT202" {
					t.Errorf("callsign %q, want CT202", f.FlightNumber())
				}
			}},
		// The two fields from Test_inferCommBMessageType: a threat given by
		// altitude, range and bearing, and one given by ICAO address.
		{bds: BdsElsAcasRA, support: "decoded", mb: "30fe3c08ffffff",
			check: func(t *testing.T, f *Frame) {
				ra := f.AcasRA()
				if ra == nil || !ra.Active || !ra.Corrective || !ra.DownwardSense || !ra.Positive || ra.NoBelow || ra.Terminated || ra.MultipleThreats {
					t.Fatalf("RA %+v", ra)
				}
				if ra.ThreatType != 2 || ra.ThreatAltFt != nil {
					t.Errorf("threat type %d, altitude %v (code 0x0400 is not a valid Gillham code)", ra.ThreatType, ra.ThreatAltFt)
				}
				near(t, "threat range", ra.ThreatRangeNm, 12.6, 1e-9)
				if ra.ThreatBearing != nil {
					t.Errorf("threat bearing %v from code 63, want not available", *ra.ThreatBearing)
				}
			}},
		{bds: BdsElsAcasRA, support: "decoded", mb: "30fe4c04ffffff",
			check: func(t *testing.T, f *Frame) {
				ra := f.AcasRA()
				if ra == nil || ra.ThreatType != 1 || ra.ThreatIcao != 0x3FFFFF {
					t.Errorf("RA %+v, want threat ICAO 3FFFFF", ra)
				}
			}},
		{bds: BdsEhsSelVertIntent, support: "decoded", msg: "a8000489aaf80030a400003ac636",
			check: func(t *testing.T, f *Frame) {
				ts := f.TargetState()
				if ts == nil || ts.Source != TargetStateFromCommB {
					t.Fatalf("target state %+v", ts)
				}
				if ts.SelectedAltitudeMcp == nil || *ts.SelectedAltitudeMcp != 22000 {
					t.Errorf("MCP altitude %v, want 22000", ts.SelectedAltitudeMcp)
				}
				if ts.SelectedAltitudeFms != nil || ts.NavModes != nil {
					t.Errorf("fields not in the message were set: %+v", ts)
				}
				near(t, "QNH", ts.BaroSetting, 1013.0, 0.01)
				if f.SquawkIdentity() != 4224 {
					t.Errorf("squawk %d, want 4224", f.SquawkIdentity())
				}
			}},
		{bds: BdsEhsAircraftIntent, support: "unsupported", mb: "aa0896412c0000"},
		{bds: BdsMetRoutineAirReport, support: "unsupported", mb: "18661df62c825e"},
		{bds: BdsMetHazartReport, support: "unsupported", mb: "b249fb2d4afa00"},
		{bds: BdsEhsTrackTurnReport, support: "decoded", msg: "a8000489fedf691e7fe483dda1f8",
			check: func(t *testing.T, f *Frame) {
				b := f.CommB().B50
				near(t, "roll", b.Roll, -1.7578125, 1e-9)
				near(t, "track", b.TrueTrack, 346.640625, 1e-9)
				near(t, "ground speed", b.GroundSpeed, 242, 0)
				near(t, "track rate", b.TrackRate, -0.125, 1e-9)
				equalInt(t, "TAS", b.TrueAirspeed, 262)
			}},
		{bds: BdsEhsTrackTurnReport, support: "decoded", msg: "a00000bf001abb0980000026671f", // only track and ground speed
			check: func(t *testing.T, f *Frame) {
				b := f.CommB().B50
				near(t, "track", b.TrueTrack, 241.34765625, 1e-9)
				near(t, "ground speed", b.GroundSpeed, 76, 0)
				if b.Roll != nil || b.TrackRate != nil || b.TrueAirspeed != nil {
					t.Errorf("fields not in the message were set: %+v", b)
				}
				if alt, _ := f.Altitude(); alt != 575 {
					t.Errorf("header altitude %d, want 575", alt)
				}
			}},
		{bds: BdsEhsPosRepCourse, support: "unsupported", mb: "ed1e02158e0bb8"},
		{bds: BdsEhsPosRepFine, support: "unsupported", mb: "92341579834000"},
		{bds: BdsEhsAirRefStateVec, support: "unsupported", mb: "96a9f5644463fb"},
		{bds: BdsEhsHeadingSpeed, support: "decoded", msg: "a000011996c8d10a3fb0003bac25",
			check: func(t *testing.T, f *Frame) {
				b := f.CommB().B60
				near(t, "heading", b.MagneticHeading, 63.984375, 1e-9)
				equalInt(t, "IAS", b.IndicatedAirspeed, 104)
				near(t, "Mach", b.Mach, 0.16, 1e-9)
				equalInt(t, "baro vertical rate", b.BaroVerticalRate, -320)
				if b.InertialVerticalRate != nil {
					t.Errorf("inertial vertical rate %v, not in the message", *b.InertialVerticalRate)
				}
			}},
	}
	for _, tt := range tests {
		t.Run(tt.bds+" "+tt.support, func(t *testing.T) {
			var f *Frame
			if tt.msg != "" {
				f = decodeOrFail(t, tt.msg)
			} else {
				f = commBFrame(t, tt.mb)
			}
			switch tt.support {
			case "decoded":
				if f.BdsMessageType() != tt.bds {
					t.Fatalf("inferred %s, want %s", f.BdsMessageType(), tt.bds)
				}
				tt.check(t, f)
			case "unsupported":
				if f.BdsMessageType() != "0.0" || f.CommB() != nil {
					t.Errorf("inferred %s with candidates %+v, want unknown", f.BdsMessageType(), f.CommB())
				}
			}
		})
	}
}

// Replies the decoder cannot, or must not, settle on its own.
func TestCommBUndecided(t *testing.T) {
	tests := []struct {
		name       string
		msg        string
		candidates []string // nil: no EHS candidates at all
	}{
		// Real reply that fits no register (pyModeS agrees).
		{"no register fits", "a2431f19b74398462124bc6e8481", nil},
		// ELS is tested first: a field that reads as 1,7 is 1,7 even though
		// the EHS tests would pass too.
		{"ELS before EHS", "a0000219ff000000000000fee5c5", nil},
		// Three EHS registers fit; nothing in the reply can choose.
		{"4,0 5,0 and 6,0 all fit", "a000011994e00000000000026bd3", []string{"4.0", "5.0", "6.0"}},
		// Two fit; the tracker decides with the aircraft's velocity
		// (TestResolveCommB50Or60).
		{"5,0 and 6,0 fit", "a00008b2f899a719617c32cec902", []string{"5.0", "6.0"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := decodeOrFail(t, tt.msg)
			if tt.candidates == nil {
				if f.CommB() != nil {
					t.Errorf("candidates %v, want none", f.CommB().Candidates)
				}
				return
			}
			if f.BdsMessageType() != "0.0" {
				t.Errorf("inferred %s, want undecided", f.BdsMessageType())
			}
			c := f.CommB()
			if c == nil || len(c.Candidates) != len(tt.candidates) {
				t.Fatalf("candidates %+v, want %v", c, tt.candidates)
			}
			for i := range tt.candidates {
				if c.Candidates[i] != tt.candidates[i] {
					t.Errorf("candidates %v, want %v", c.Candidates, tt.candidates)
				}
			}
			if f.TargetState() != nil {
				t.Errorf("undecided reply produced a target state %+v", f.TargetState())
			}
		})
	}
}

// Each plausibility rule in the EHS tests: a field that passes, and the same
// field changed by the least that breaks the rule. Bit numbers are within the
// MB field.
func TestCommBRejects(t *testing.T) {
	b40 := "aaf80030a40000" // the 4,0 reply above: MCP 22,000 ft, QNH 1013
	b50 := "fedf691e7fe483" // the 5,0 reply above: roll -1.8, track 347, GS 242, TAS 262
	b60 := "f899a719617c32" // the 5,0/6,0 reply above read as 6,0: IAS 211, Mach 0.404
	alt := int32(13050)
	tests := []struct {
		name string
		reg  string
		base string
		edit func(mb []byte)
		alt  *int32
		pass bool
	}{
		{"4,0 as heard", "4.0", b40, nil, nil, true},
		{"4,0 reserved bits 40-47 set", "4.0", b40, func(mb []byte) { setBits(mb, 40, 40, 1) }, nil, false},
		{"4,0 reserved bits 52-53 set", "4.0", b40, func(mb []byte) { setBits(mb, 53, 53, 1) }, nil, false},
		{"4,0 MCP status clear with a value", "4.0", b40, func(mb []byte) { setBits(mb, 1, 1, 0) }, nil, false},
		{"4,0 MCP altitude 50,000 ft", "4.0", b40, func(mb []byte) { setBits(mb, 2, 13, 3125) }, nil, true},
		{"4,0 MCP altitude above 50,000 ft", "4.0", b40, func(mb []byte) { setBits(mb, 2, 13, 3126) }, nil, false},
		{"4,0 every status clear", "4.0", "00000000000000", nil, nil, false},

		{"5,0 as heard", "5.0", b50, nil, nil, true},
		{"5,0 roll 35 degrees", "5.0", b50, func(mb []byte) { setBits(mb, 2, 11, 199) }, nil, true},
		{"5,0 roll above 35 degrees", "5.0", b50, func(mb []byte) { setBits(mb, 2, 11, 200) }, nil, false},
		{"5,0 ground speed 600 kt", "5.0", b50, func(mb []byte) { setBits(mb, 25, 34, 300); setBits(mb, 47, 56, 300) }, nil, true},
		{"5,0 ground speed above 600 kt", "5.0", b50, func(mb []byte) { setBits(mb, 25, 34, 301); setBits(mb, 47, 56, 300) }, nil, false},
		{"5,0 TAS above 600 kt", "5.0", b50, func(mb []byte) { setBits(mb, 25, 34, 300); setBits(mb, 47, 56, 301) }, nil, false},
		{"5,0 ground speed and TAS 200 kt apart", "5.0", b50, func(mb []byte) { setBits(mb, 25, 34, 121); setBits(mb, 47, 56, 21) }, nil, true},
		{"5,0 ground speed and TAS further apart", "5.0", b50, func(mb []byte) { setBits(mb, 25, 34, 122); setBits(mb, 47, 56, 21) }, nil, false},
		{"5,0 track status clear with a value", "5.0", b50, func(mb []byte) { setBits(mb, 12, 12, 0) }, nil, false},

		{"6,0 as heard, IAS and Mach agree at 13,050 ft", "6.0", b60, nil, &alt, true},
		{"6,0 IAS 300 kt at Mach 0.404 and 13,050 ft", "6.0", b60, func(mb []byte) { setBits(mb, 14, 23, 300) }, &alt, false},
		{"6,0 IAS 300 kt at Mach 0.404, altitude unknown", "6.0", b60, func(mb []byte) { setBits(mb, 14, 23, 300) }, nil, true},
		{"6,0 IAS 500 kt", "6.0", b60, func(mb []byte) { setBits(mb, 14, 23, 500) }, nil, true},
		{"6,0 IAS above 500 kt", "6.0", b60, func(mb []byte) { setBits(mb, 14, 23, 501) }, nil, false},
		{"6,0 Mach 1.0", "6.0", b60, func(mb []byte) { setBits(mb, 25, 34, 250) }, nil, true},
		{"6,0 Mach above 1.0", "6.0", b60, func(mb []byte) { setBits(mb, 25, 34, 251) }, nil, false},
		{"6,0 vertical rate 6,000 ft/min", "6.0", b60, func(mb []byte) { setBits(mb, 35, 35, 1); setBits(mb, 36, 36, 0); setBits(mb, 37, 45, 187) }, nil, true},
		{"6,0 vertical rate above 6,000 ft/min", "6.0", b60, func(mb []byte) { setBits(mb, 35, 35, 1); setBits(mb, 36, 36, 0); setBits(mb, 37, 45, 188) }, nil, false},
		{"6,0 heading status clear with a value", "6.0", b60, func(mb []byte) { setBits(mb, 1, 1, 0) }, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mb, err := hex.DecodeString(tt.base)
			if err != nil {
				t.Fatal(err)
			}
			if tt.edit != nil {
				tt.edit(mb)
			}
			var got bool
			switch tt.reg {
			case "4.0":
				got = decodeBds40(mb) != nil
			case "5.0":
				got = decodeBds50(mb) != nil
			case "6.0":
				got = decodeBds60(mb, tt.alt) != nil
			}
			if got != tt.pass {
				t.Errorf("accepted as %s: %v, want %v", tt.reg, got, tt.pass)
			}
		})
	}
	if decodeEhs(make([]byte, 7), nil) != nil {
		t.Error("an all-zero field passed as an EHS register")
	}
}

// commBFrame wraps a 7-byte MB field in a DF20 frame. DF20/21 overlay the
// CRC with the address, so any tail decodes.
func commBFrame(t *testing.T, mb string) *Frame {
	t.Helper()
	return decodeOrFail(t, "a0000000"+mb+"000000")
}

func near(t *testing.T, name string, got *float64, want float64, tol float64) {
	t.Helper()
	if got == nil || math.Abs(*got-want) > tol {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

func equalInt(t *testing.T, name string, got *int, want int) {
	t.Helper()
	if got == nil || *got != want {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

func decodeOrFail(t *testing.T, msg string) *Frame {
	t.Helper()
	f, err := DecodeString(msg, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// Both 5,0 and 6,0 pass their checks; the aircraft's known state decides.
func TestResolveCommB50Or60(t *testing.T) {
	// DF20 from CT202 at 13,050 ft. Read as 5,0: 202 kt, track 217.
	// Read as 6,0: heading 339, IAS 211, Mach 0.404.
	f := decodeOrFail(t, "a00008b2f899a719617c32cec902")
	c := f.CommB()
	if c == nil || len(c.Candidates) != 2 || f.BdsMessageType() != "0.0" {
		t.Fatalf("want two candidates and no choice, got %+v / %s", c, f.BdsMessageType())
	}
	// CT202 was flying track 347 at 240 kt (its own ADS-B velocity).
	actual := KnownState{GroundSpeed: 240, Track: 347, HasVelocity: true, AltitudeFt: 13050, HasAltitude: true}
	if got := ResolveCommB(c, actual); got != BdsEhsHeadingSpeed {
		t.Errorf("with the real state: %q, want 6.0", got)
	}
	// Had it been flying 202 kt on 217, the 5,0 reading would fit.
	other := KnownState{GroundSpeed: 202, Track: 217, HasVelocity: true}
	if got := ResolveCommB(c, other); got != BdsEhsTrackTurnReport {
		t.Errorf("with a 5,0-like state: %q, want 5.0", got)
	}
	if got := ResolveCommB(c, KnownState{}); got != "" {
		t.Errorf("with no state: %q, want undecided", got)
	}
	// The aircraft's BDS 1,7 can settle it without a velocity.
	only60 := &GicbCapability{Supported: []string{"2.0", "4.0", "6.0"}}
	if got := ResolveCommB(c, KnownState{Gicb: only60}); got != BdsEhsHeadingSpeed {
		t.Errorf("with 1,7 lacking 5,0: %q, want 6.0", got)
	}
	neither := &GicbCapability{Supported: []string{"2.0"}}
	if got := ResolveCommB(c, KnownState{Gicb: neither}); got != "" {
		t.Errorf("with 1,7 lacking both: %q, want undecided", got)
	}
}

// The real three-way reply from TestCommBUndecided. Read as 5,0 it has only
// a roll and as 6,0 only a heading, so no velocity can choose; the
// aircraft's own BDS 1,7 can, by ruling registers out.
func TestResolveCommBWithGicb(t *testing.T) {
	f := decodeOrFail(t, "a000011994e00000000000026bd3")
	c := f.CommB()
	if c == nil || len(c.Candidates) != 3 {
		t.Fatalf("candidates %+v", c)
	}
	k := KnownState{GroundSpeed: 120, Track: 200, HasVelocity: true, AltitudeFt: 825, HasAltitude: true}
	if got := ResolveCommB(c, k); got != "" {
		t.Errorf("without 1,7: %q, want undecided", got)
	}
	k.Gicb = &GicbCapability{Supported: []string{"2.0", "5.0", "6.0"}}
	if got := ResolveCommB(c, k); got != "" {
		t.Errorf("with 1,7 lacking 4,0, no speed either way: %q, want undecided", got)
	}
	k.Gicb = &GicbCapability{Supported: []string{"2.0", "6.0"}}
	if got := ResolveCommB(c, k); got != BdsEhsHeadingSpeed {
		t.Errorf("with 1,7 lacking 4,0 and 5,0: %q, want 6.0", got)
	}
	if !f.ResolveCommB(k) || f.BdsMessageType() != BdsEhsHeadingSpeed {
		t.Errorf("frame not settled: %s", f.BdsMessageType())
	}
}

func TestCommBAmbiguous40(t *testing.T) {
	f := decodeOrFail(t, "a0000117d0100000000000b35c30")
	if f.TargetState() != nil {
		t.Errorf("ambiguous reply produced a target state %+v", f.TargetState())
	}
	c := f.CommB()
	if c == nil || len(c.Candidates) != 2 || c.B40 == nil || c.B60 == nil {
		t.Fatalf("candidates %+v", c)
	}
	k := KnownState{GroundSpeed: 120, Track: 200, HasVelocity: true, AltitudeFt: 775, HasAltitude: true}
	if got := ResolveCommB(c, k); got != "" {
		t.Errorf("resolved to %q, want undecided", got)
	}
}

func TestIsaConversions(t *testing.T) {
	if a := speedOfSound(0); math.Abs(a-661.5) > 0.5 {
		t.Errorf("sea level speed of sound %v kt", a)
	}
	// From the BDS 6,0 reading above: IAS 211 at Mach 0.404, 13,050 ft.
	if cas := machToCas(0.404, 13050); math.Abs(cas-211) > 5 {
		t.Errorf("Mach 0.404 at 13,050 ft = %v kt CAS, want about 211", cas)
	}
	if tas := casToTas(machToCas(0.78, 35000), 35000); math.Abs(tas-0.78*speedOfSound(35000)) > 0.5 {
		t.Errorf("CAS/TAS round trip at FL350: %v", tas)
	}
}
