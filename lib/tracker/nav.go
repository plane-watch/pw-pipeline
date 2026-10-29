package tracker

import (
	"time"

	"plane.watch/lib/tracker/mode_s"
)

// Navigation (TC29 target state, BDS 4,0), enhanced surveillance (BDS 5,0
// and 6,0) and ELS (BDS 1,0, 1,7, 3,0) fields held on a Plane. They are
// decoded in lib/tracker/mode_s (decode-adsb.go, decode-bds.go); this file
// holds the values and the rules for accepting them, which need state the
// decoder does not have.

const (
	// navExpiry is how long a nav or EHS value stays current without being
	// refreshed, measured against the plane's last seen time so that a
	// stale selected altitude does not ride along with a live position.
	navExpiry = 60 * time.Second

	// velocityResolveWindow is how old a TC19 velocity may be and still
	// decide between an ambiguous BDS 5,0 / 6,0 reply.
	velocityResolveWindow = 10 * time.Second

	// selectedAltitudeTolerance is the difference allowed between a BDS 4,0
	// selected altitude (16 ft steps) and a TC29 one (32 ft steps) for the
	// two to count as the same value.
	selectedAltitudeTolerance = 32
)

type (
	// NavModes are the autopilot modes reported by TC29 (and, for the three
	// it carries, BDS 4,0).
	NavModes struct {
		Autopilot bool
		VNAV      bool
		LNAV      bool
		AltHold   bool
		Approach  bool
		TCAS      bool
	}

	// NavState is what the aircraft's flight control system is set to.
	// A nil pointer means the aircraft has not reported the value.
	NavState struct {
		SelectedAltitudeMCP *int     // ft, MCP/FCU window
		SelectedAltitudeFMS *int     // ft, FMS
		QNH                 *float64 // hPa
		SelectedHeading     *float64 // degrees
		Modes               *NavModes
	}

	// EhsState is the enhanced surveillance (BDS 5,0 and 6,0) picture of the
	// aircraft. A nil pointer means the aircraft has not reported the value.
	EhsState struct {
		IAS                  *int     // kt
		TAS                  *int     // kt
		Mach                 *float64 // ratio
		Roll                 *float64 // degrees, right wing down positive
		MagneticHeading      *float64 // degrees
		TrackRate            *float64 // degrees per second
		BaroVerticalRate     *int     // ft/min
		InertialVerticalRate *int     // ft/min
	}

	// bds40Reading is an inferred BDS 4,0 that has been seen but not yet
	// accepted, kept for the seen-twice rule.
	bds40Reading struct {
		mcp, fms *int
		qnh      *float64
		ts       time.Time
	}

	navTracking struct {
		state NavState
		ts    time.Time // last accepted update

		tc29TS     time.Time // last TC29 with a selected altitude
		tc29SelAlt int
		tc29FMS    bool

		last40 *bds40Reading
	}

	ehsTracking struct {
		state EhsState
		ts    time.Time
	}

	// elsTracking holds the ELS capability and ACAS reports. The
	// capabilities describe the transponder and do not expire; the RA is
	// kept with the time it was last reported.
	elsTracking struct {
		dataLink *mode_s.DataLinkCapability
		gicb     *mode_s.GicbCapability
		acasRA   *mode_s.AcasRA
		acasRATS time.Time
	}
)

// DataLinkCapability returns the aircraft's BDS 1,0 report, or nil.
func (p *Plane) DataLinkCapability() *mode_s.DataLinkCapability {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.els.dataLink
}

// GicbCapability returns the aircraft's BDS 1,7 report, or nil.
func (p *Plane) GicbCapability() *mode_s.GicbCapability {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.els.gicb
}

// AcasRA returns the last BDS 3,0 report and when it was heard.
func (p *Plane) AcasRA() (*mode_s.AcasRA, time.Time) {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.els.acasRA, p.els.acasRATS
}

func (p *Plane) setDataLink(d *mode_s.DataLinkCapability, t time.Time) {
	if d == nil {
		return
	}
	p.rwLock.Lock()
	defer p.rwLock.Unlock()
	p.els.dataLink = d
}

func (p *Plane) setGicb(g *mode_s.GicbCapability, t time.Time) {
	if g == nil {
		return
	}
	p.rwLock.Lock()
	defer p.rwLock.Unlock()
	p.els.gicb = g
}

func (p *Plane) setAcasRA(ra *mode_s.AcasRA, t time.Time) {
	if ra == nil {
		return
	}
	p.rwLock.Lock()
	defer p.rwLock.Unlock()
	p.els.acasRA = ra
	p.els.acasRATS = t
}

// Strings gives the modes in readsb's nav_modes names.
func (m NavModes) Strings() []string {
	var out []string
	if m.Autopilot {
		out = append(out, "autopilot")
	}
	if m.VNAV {
		out = append(out, "vnav")
	}
	if m.LNAV {
		out = append(out, "lnav")
	}
	if m.AltHold {
		out = append(out, "althold")
	}
	if m.Approach {
		out = append(out, "approach")
	}
	if m.TCAS {
		out = append(out, "tcas")
	}
	return out
}

func (n NavState) isEmpty() bool {
	return n.SelectedAltitudeMCP == nil && n.SelectedAltitudeFMS == nil && n.QNH == nil &&
		n.SelectedHeading == nil && n.Modes == nil
}

func (e EhsState) isEmpty() bool {
	return e.IAS == nil && e.TAS == nil && e.Mach == nil && e.Roll == nil && e.MagneticHeading == nil &&
		e.TrackRate == nil && e.BaroVerticalRate == nil && e.InertialVerticalRate == nil
}

// Nav returns the navigation state and whether it is current: something has
// been reported and it was refreshed within navExpiry of the last message.
func (p *Plane) Nav() (NavState, bool) {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.nav.state, p.navCurrentLocked()
}

// EHS returns the enhanced surveillance state and whether it is current.
func (p *Plane) EHS() (EhsState, bool) {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.ehs.state, p.ehsCurrentLocked()
}

func (p *Plane) NavUpdatedAt() time.Time {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.nav.ts
}

func (p *Plane) EhsUpdatedAt() time.Time {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	return p.ehs.ts
}

func (p *Plane) navCurrentLocked() bool {
	return !p.nav.state.isEmpty() && !p.nav.ts.IsZero() && p.lastSeen.Sub(p.nav.ts) <= navExpiry
}

func (p *Plane) ehsCurrentLocked() bool {
	return !p.ehs.state.isEmpty() && !p.ehs.ts.IsZero() && p.lastSeen.Sub(p.ehs.ts) <= navExpiry
}

// setTargetState takes a TC29 subtype 1 message as it comes. A field the
// message does not carry (nil) is not a change and leaves the old value.
func (p *Plane) setTargetState(ts *mode_s.TargetState, t time.Time) bool {
	if ts == nil || t.IsZero() {
		return false
	}
	p.rwLock.Lock()
	defer p.rwLock.Unlock()
	var changed bool
	switch {
	case ts.SelectedAltitudeFms != nil:
		changed = setIntPtr(&p.nav.state.SelectedAltitudeFMS, int(*ts.SelectedAltitudeFms)) || changed
		p.nav.tc29TS, p.nav.tc29SelAlt, p.nav.tc29FMS = t, int(*ts.SelectedAltitudeFms), true
	case ts.SelectedAltitudeMcp != nil:
		changed = setIntPtr(&p.nav.state.SelectedAltitudeMCP, int(*ts.SelectedAltitudeMcp)) || changed
		p.nav.tc29TS, p.nav.tc29SelAlt, p.nav.tc29FMS = t, int(*ts.SelectedAltitudeMcp), false
	}
	if ts.BaroSetting != nil {
		changed = setFloatPtr(&p.nav.state.QNH, *ts.BaroSetting) || changed
	}
	if ts.SelectedHeading != nil {
		changed = setFloatPtr(&p.nav.state.SelectedHeading, *ts.SelectedHeading) || changed
	}
	if ts.NavModes != nil {
		modes := navModesFrom(*ts.NavModes)
		if ts.TcasOperational != nil {
			modes.TCAS = *ts.TcasOperational
		}
		changed = setModes(&p.nav.state.Modes, modes) || changed
	}
	p.nav.ts = t
	return changed
}

// navModesFrom converts the decoder's mode flags.
func navModesFrom(m mode_s.NavModes) NavModes {
	return NavModes{
		Autopilot: m&mode_s.NavModeAutopilot != 0,
		VNAV:      m&mode_s.NavModeVNAV != 0,
		LNAV:      m&mode_s.NavModeLNAV != 0,
		AltHold:   m&mode_s.NavModeAltHold != 0,
		Approach:  m&mode_s.NavModeApproach != 0,
	}
}

func i32p(v *int32) *int {
	if v == nil {
		return nil
	}
	i := int(*v)
	return &i
}

// setBds40 takes an inferred BDS 4,0 (selected vertical intention) reply.
// Inference can be wrong, and a wrong register read as 4,0 would become a
// false selected altitude, so a reading is accepted only when it agrees
// with a TC29 selected altitude seen in the last navExpiry, or when the
// same values have been seen twice. While TC29 is current the reply adds
// only what TC29 lacks (the FMS altitude); without TC29, as on ADS-B
// version 0 aircraft, it supplies everything.
func (p *Plane) setBds40(b *mode_s.Bds40, t time.Time) bool {
	if b == nil || t.IsZero() {
		return false
	}
	mcp, fms := i32p(b.SelectedAltitudeMcp), i32p(b.SelectedAltitudeFms)
	if mcp == nil && fms == nil && b.BaroSetting == nil {
		return false
	}
	p.rwLock.Lock()
	defer p.rwLock.Unlock()

	reading := &bds40Reading{mcp: mcp, fms: fms, qnh: b.BaroSetting, ts: t}
	tc29Current := !p.nav.tc29TS.IsZero() && t.Sub(p.nav.tc29TS) <= navExpiry && t.Sub(p.nav.tc29TS) >= -navExpiry
	accepted := false
	if tc29Current {
		if p.nav.tc29FMS {
			accepted = fms != nil && near(*fms, p.nav.tc29SelAlt, selectedAltitudeTolerance)
		} else {
			accepted = mcp != nil && near(*mcp, p.nav.tc29SelAlt, selectedAltitudeTolerance)
		}
	}
	if !accepted && p.nav.last40 != nil && t.Sub(p.nav.last40.ts) <= navExpiry && p.nav.last40.sameValues(reading) {
		accepted = true
	}
	p.nav.last40 = reading
	if !accepted {
		return false
	}

	var changed bool
	if fms != nil {
		changed = setIntPtr(&p.nav.state.SelectedAltitudeFMS, *fms) || changed
	}
	if !tc29Current {
		if mcp != nil {
			changed = setIntPtr(&p.nav.state.SelectedAltitudeMCP, *mcp) || changed
		}
		if b.BaroSetting != nil {
			changed = setFloatPtr(&p.nav.state.QNH, *b.BaroSetting) || changed
		}
		if b.NavModes != nil {
			// 4,0 carries VNAV, altitude hold and approach only; keep the
			// rest from the last TC29.
			modes := navModesFrom(*b.NavModes)
			if p.nav.state.Modes != nil {
				modes.Autopilot = p.nav.state.Modes.Autopilot
				modes.LNAV = p.nav.state.Modes.LNAV
				modes.TCAS = p.nav.state.Modes.TCAS
			}
			changed = setModes(&p.nav.state.Modes, modes) || changed
		}
	}
	p.nav.ts = t
	return changed
}

func (r *bds40Reading) sameValues(o *bds40Reading) bool {
	return eqIntPtr(r.mcp, o.mcp) && eqIntPtr(r.fms, o.fms) && eqFloatPtr(r.qnh, o.qnh)
}

// setBds50 takes a BDS 5,0 (track and turn) reply that is known to be one.
func (p *Plane) setBds50(b *mode_s.Bds50, t time.Time) bool {
	if b == nil || t.IsZero() {
		return false
	}
	p.rwLock.Lock()
	defer p.rwLock.Unlock()
	var changed bool
	if b.Roll != nil {
		changed = setFloatPtr(&p.ehs.state.Roll, *b.Roll) || changed
	}
	if b.TrackRate != nil {
		changed = setFloatPtr(&p.ehs.state.TrackRate, *b.TrackRate) || changed
	}
	if b.TrueAirspeed != nil {
		changed = setIntPtr(&p.ehs.state.TAS, *b.TrueAirspeed) || changed
	}
	// Ground speed and true track duplicate TC19 and are not kept.
	p.ehs.ts = t
	return changed
}

// setBds60 takes a BDS 6,0 (heading and speed) reply that is known to be one.
func (p *Plane) setBds60(b *mode_s.Bds60, t time.Time) bool {
	if b == nil || t.IsZero() {
		return false
	}
	p.rwLock.Lock()
	defer p.rwLock.Unlock()
	var changed bool
	if b.MagneticHeading != nil {
		changed = setFloatPtr(&p.ehs.state.MagneticHeading, *b.MagneticHeading) || changed
	}
	if b.IndicatedAirspeed != nil {
		changed = setIntPtr(&p.ehs.state.IAS, *b.IndicatedAirspeed) || changed
	}
	if b.Mach != nil {
		changed = setFloatPtr(&p.ehs.state.Mach, *b.Mach) || changed
	}
	if b.BaroVerticalRate != nil {
		changed = setIntPtr(&p.ehs.state.BaroVerticalRate, *b.BaroVerticalRate) || changed
	}
	if b.InertialVerticalRate != nil {
		changed = setIntPtr(&p.ehs.state.InertialVerticalRate, *b.InertialVerticalRate) || changed
	}
	p.ehs.ts = t
	return changed
}

// knownState is what the tracker tells the decoder about the aircraft so
// that a DF20/21 reply reading as both BDS 5,0 and 6,0 can be settled: the
// TC19 ground speed and track when they are within velocityResolveWindow
// of t, and the altitude.
func (p *Plane) knownState(t time.Time) mode_s.KnownState {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	k := mode_s.KnownState{Gicb: p.els.gicb}
	if p.location == nil {
		return k
	}
	if p.location.hasVelocity && p.location.hasHeading && !p.location.velocityTS.IsZero() {
		age := t.Sub(p.location.velocityTS)
		if age < 0 {
			age = -age
		}
		if age <= velocityResolveWindow {
			k.HasVelocity = true
			k.GroundSpeed = p.location.velocity
			k.Track = p.location.heading
		}
	}
	if !p.location.altitudeTS.IsZero() {
		k.HasAltitude = true
		k.AltitudeFt = float64(p.location.altitude)
	}
	return k
}

func setIntPtr(dst **int, v int) bool {
	if *dst != nil && **dst == v {
		return false
	}
	*dst = &v
	return true
}

func setFloatPtr(dst **float64, v float64) bool {
	if *dst != nil && **dst == v {
		return false
	}
	*dst = &v
	return true
}

func setModes(dst **NavModes, v NavModes) bool {
	if *dst != nil && **dst == v {
		return false
	}
	*dst = &v
	return true
}

func eqIntPtr(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func eqFloatPtr(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func near(a, b, tolerance int) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= tolerance
}
