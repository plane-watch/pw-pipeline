package mode_s

// Selected (target) state: what the crew has set on the autopilot. It comes
// from ADS-B type 29 subtype 1 (Target State and Status, DO-260B; decoded in
// decode-adsb.go) and from the Comm-B register BDS 4,0 (Selected Vertical
// Intention; decode-bds.go).

// NavModes are the engaged autopilot modes, as flags.
type NavModes uint8

const (
	NavModeAutopilot NavModes = 1 << iota
	NavModeVNAV
	NavModeAltHold
	NavModeApproach
	NavModeLNAV
)

// Strings returns the modes using readsb's nav_modes names.
func (m NavModes) Strings() []string {
	names := []struct {
		m NavModes
		s string
	}{
		{NavModeAutopilot, "autopilot"}, {NavModeVNAV, "vnav"}, {NavModeAltHold, "althold"},
		{NavModeApproach, "approach"}, {NavModeLNAV, "lnav"},
	}
	out := []string{}
	for _, n := range names {
		if m&n.m != 0 {
			out = append(out, n.s)
		}
	}
	return out
}

const (
	TargetStateFromAdsb  = "adsb"  // ADS-B type 29 subtype 1
	TargetStateFromCommB = "commb" // Comm-B BDS 4,0
)

// TargetState holds the selected values from one message. A nil field was
// not in the message, or the message said "no data".
type TargetState struct {
	Source string

	SelectedAltitudeMcp *int32   // ft, set on the MCP/FCU (the "alt bug")
	SelectedAltitudeFms *int32   // ft, from the FMS
	BaroSetting         *float64 // hPa (QNH or QFE set on the altimeter)
	SelectedHeading     *float64 // degrees, ADS-B only
	NavModes            *NavModes
	TcasOperational     *bool // ADS-B only
}

// TargetState returns the selected values decoded from this frame, or nil.
// For Comm-B frames it is set only when BDS 4,0 is the sole candidate; see
// CommB for ambiguous replies.
func (f *Frame) TargetState() *TargetState {
	if f == nil {
		return nil
	}
	return f.targetState
}
