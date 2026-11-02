package tracker

import (
	"math"
	"testing"
	"time"

	"plane.watch/lib/tracker/mode_s"
)

// Frames below were received at Perth on 2026-10-22 (capture
// beast-30005-20261022T004216Z) and their values checked against pyModeS 3.6.
const (
	// 7CAD1C TC29: selected altitude 4992 ft (MCP), QNH 1019.2, selected
	// heading 236.25, mode bits valid: LNAV only, TCAS not operational
	frameTC29Modes = "*8D7CAD1CEA09D89EA13E0401CFC7;"
	// 7CAF5F TC29: selected altitude 22016 ft (MCP), QNH 1012.8, heading
	// 343.125, mode bits not valid, TCAS operational
	frameTC29 = "*8D7CAF5FEA2B185FD13C085708E5;"
	// 7CAF5F TC19: ground speed 243 kt, track 345.96, vertical rate 1600
	frameTC19 = "*8D7CAF5F990C3C1DB0680EA8304C;"
	// 7CAF5F DF21 BDS 4,0: MCP 22000 ft, QNH 1013.0
	frameBDS40 = "*A8000489AAF80030A400003AC636;"
	// 7CAF5F DF20 reading as both 5,0 (204 kt on 217) and 6,0 (heading
	// 337.68, IAS 211, Mach 0.408, baro rate 1600, inertial 1696)
	frameAmbiguous = "*A00008BEF819A719A1943506371A;"

	icao7CAF5F = 0x7CAF5F
	icao7CAD1C = 0x7CAD1C
)

var navT0 = time.Date(2026, time.October, 22, 0, 50, 0, 0, time.UTC)

func feed(t *testing.T, trk *Tracker, raw string, ts time.Time) *Plane {
	t.Helper()
	frame, err := mode_s.DecodeString(raw, ts)
	if err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	p := trk.GetPlane(frame.Icao())
	p.HandleModeSFrame(frame, nil)
	return p
}

func fv(v *float64) float64 {
	if v == nil {
		return math.NaN()
	}
	return *v
}

func iv(v *int) int {
	if v == nil {
		return math.MinInt
	}
	return *v
}

func TestTargetStateFromFrame(t *testing.T) {
	trk := NewTracker()
	p := feed(t, trk, frameTC29Modes, navT0)
	nav, ok := p.Nav()
	if !ok {
		t.Fatal("nav state not current after a TC29 frame")
	}
	if iv(nav.SelectedAltitudeMCP) != 4992 || nav.SelectedAltitudeFMS != nil {
		t.Errorf("selected altitude MCP %d FMS %v, want 4992 and nil", iv(nav.SelectedAltitudeMCP), nav.SelectedAltitudeFMS)
	}
	if fv(nav.QNH) != 1019.2 {
		t.Errorf("QNH %v, want 1019.2", fv(nav.QNH))
	}
	if fv(nav.SelectedHeading) != 236.25 {
		t.Errorf("selected heading %v, want 236.25", fv(nav.SelectedHeading))
	}
	if nav.Modes == nil || *nav.Modes != (NavModes{LNAV: true}) {
		t.Errorf("modes %+v, want LNAV only", nav.Modes)
	}
	if got := nav.Modes.Strings(); len(got) != 1 || got[0] != "lnav" {
		t.Errorf("mode names %v, want [lnav]", got)
	}
	if !p.NavUpdatedAt().Equal(navT0) {
		t.Errorf("nav updated at %v, want %v", p.NavUpdatedAt(), navT0)
	}
}

func TestTargetStateModesNotValidLeavesModesNil(t *testing.T) {
	trk := NewTracker()
	p := feed(t, trk, frameTC29, navT0)
	nav, ok := p.Nav()
	if !ok {
		t.Fatal("nav state not current")
	}
	if iv(nav.SelectedAltitudeMCP) != 22016 || fv(nav.QNH) != 1012.8 || fv(nav.SelectedHeading) != 343.125 {
		t.Errorf("nav %+v", nav)
	}
	if nav.Modes != nil {
		t.Errorf("modes %+v set from a frame whose mode bits are not valid", *nav.Modes)
	}
}

func TestBds40NeedsASecondSighting(t *testing.T) {
	trk := NewTracker()
	feed(t, trk, frameTC19, navT0)
	p := feed(t, trk, frameBDS40, navT0.Add(time.Second))
	if _, ok := p.Nav(); ok {
		t.Fatal("a single inferred BDS 4,0 was accepted")
	}
	p = feed(t, trk, frameBDS40, navT0.Add(2*time.Second))
	nav, ok := p.Nav()
	if !ok {
		t.Fatal("the same BDS 4,0 seen twice was not accepted")
	}
	if iv(nav.SelectedAltitudeMCP) != 22000 || fv(nav.QNH) != 1013 {
		t.Errorf("MCP %d QNH %v, want 22000 and 1013", iv(nav.SelectedAltitudeMCP), fv(nav.QNH))
	}
}

func TestBds40AcceptedWhenTc29Agrees(t *testing.T) {
	trk := NewTracker()
	feed(t, trk, frameTC19, navT0)
	feed(t, trk, frameTC29, navT0)
	p := feed(t, trk, frameBDS40, navT0.Add(time.Second))
	nav, ok := p.Nav()
	if !ok {
		t.Fatal("nav state not current")
	}
	// TC29 is current, so its 32 ft value stands and the 4,0 refreshes the timestamp
	if iv(nav.SelectedAltitudeMCP) != 22016 || fv(nav.QNH) != 1012.8 {
		t.Errorf("MCP %d QNH %v, want TC29's 22016 and 1012.8", iv(nav.SelectedAltitudeMCP), fv(nav.QNH))
	}
	if !p.NavUpdatedAt().Equal(navT0.Add(time.Second)) {
		t.Errorf("nav updated at %v, want the 4,0 time", p.NavUpdatedAt())
	}
}

func TestBds40TooFarFromTc29IsNotAccepted(t *testing.T) {
	trk := NewTracker()
	p := trk.GetPlane(icao7CAF5F)
	p.setLastSeen(navT0)
	sel := int32(10000)
	if !p.setTargetState(&mode_s.TargetState{SelectedAltitudeMcp: &sel}, navT0) {
		t.Fatal("target state not applied")
	}
	mcp := int32(22000)
	if p.setBds40(&mode_s.Bds40{SelectedAltitudeMcp: &mcp}, navT0.Add(time.Second)) {
		t.Error("a 4,0 disagreeing with TC29 changed the state")
	}
	nav, _ := p.Nav()
	if iv(nav.SelectedAltitudeMCP) != 10000 {
		t.Errorf("MCP %d, want 10000", iv(nav.SelectedAltitudeMCP))
	}
}

func TestCommBResolvedByVelocity(t *testing.T) {
	trk := NewTracker()
	feed(t, trk, frameTC19, navT0)
	p := feed(t, trk, frameAmbiguous, navT0.Add(2*time.Second))
	ehs, ok := p.EHS()
	if !ok {
		t.Fatal("EHS not current: the 5,0/6,0 reply was not resolved")
	}
	if iv(ehs.IAS) != 211 || math.Abs(fv(ehs.Mach)-0.408) > 1e-9 || fv(ehs.MagneticHeading) != 337.67578125 {
		t.Errorf("IAS %d Mach %v heading %v, want 211, 0.408, 337.67578125", iv(ehs.IAS), fv(ehs.Mach), fv(ehs.MagneticHeading))
	}
	if iv(ehs.BaroVerticalRate) != 1600 || iv(ehs.InertialVerticalRate) != 1696 {
		t.Errorf("vertical rates baro %d inertial %d, want 1600 and 1696", iv(ehs.BaroVerticalRate), iv(ehs.InertialVerticalRate))
	}
	if ehs.Roll != nil || ehs.TAS != nil {
		t.Errorf("5,0 values taken from a reply resolved as 6,0: roll %v TAS %v", ehs.Roll, ehs.TAS)
	}
}

func TestCommBUndecidedWithoutRecentVelocity(t *testing.T) {
	trk := NewTracker()
	feed(t, trk, frameTC19, navT0)
	p := feed(t, trk, frameAmbiguous, navT0.Add(20*time.Second))
	if _, ok := p.EHS(); ok {
		t.Error("an ambiguous reply was decoded with a 20 s old velocity")
	}
}

func TestNavExpiresWithoutRefresh(t *testing.T) {
	trk := NewTracker()
	p := feed(t, trk, frameTC29Modes, navT0)
	p.setLastSeen(navT0.Add(navExpiry))
	if _, ok := p.Nav(); !ok {
		t.Error("nav state expired at exactly the limit")
	}
	p.setLastSeen(navT0.Add(navExpiry + time.Second))
	if _, ok := p.Nav(); ok {
		t.Error("nav state still current 61 s after the last report")
	}
	if p.NavUpdatedAt().IsZero() {
		t.Error("the timestamp of the last report should survive expiry")
	}
}
