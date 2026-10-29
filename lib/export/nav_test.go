package export

import (
	"reflect"
	"testing"
	"time"
)

func TestMergeNavNewerWins(t *testing.T) {
	t1 := time.Date(2026, time.October, 22, 0, 50, 0, 0, time.UTC)
	t2 := t1.Add(time.Second)

	prev := PlaneLocation{
		NavAltitudeMCP: ptr(22016),
		NavQNH:         ptr(1012.8),
		NavModes:       []string{"tcas"},
		Updates:        Updates{Nav: t1},
	}
	next := PlaneLocation{
		NavAltitudeMCP: ptr(23008),
		NavQNH:         ptr(1012.8),
		NavHeading:     ptr(343.125),
		Updates:        Updates{Nav: t2},
	}

	merged, err := MergePlaneLocations(prev, next)
	if err != nil {
		t.Fatalf("MergePlaneLocations() error = %v", err)
	}
	if got := unPtr(merged.NavAltitudeMCP); got != 23008 {
		t.Errorf("NavAltitudeMCP = %d, want 23008", got)
	}
	if got := unPtr(merged.NavHeading); got != 343.125 {
		t.Errorf("NavHeading = %v, want 343.125", got)
	}
	if merged.NavModes != nil {
		t.Errorf("NavModes = %v, want nil: the newer report carries the whole nav state", merged.NavModes)
	}
	if !merged.Updates.Nav.Equal(t2) {
		t.Errorf("Updates.Nav = %v, want %v", merged.Updates.Nav, t2)
	}
}

func TestMergeNavRejectsStaleAndEmpty(t *testing.T) {
	t1 := time.Date(2026, time.October, 22, 0, 50, 1, 0, time.UTC)
	t2 := t1.Add(-time.Second)

	prev := PlaneLocation{NavAltitudeMCP: ptr(22016), Updates: Updates{Nav: t1}}

	stale := PlaneLocation{NavAltitudeMCP: ptr(10000), Updates: Updates{Nav: t2}}
	merged, err := MergePlaneLocations(prev, stale)
	if err != nil {
		t.Fatalf("MergePlaneLocations() error = %v", err)
	}
	if got := unPtr(merged.NavAltitudeMCP); got != 22016 {
		t.Errorf("stale nav accepted: NavAltitudeMCP = %d, want 22016", got)
	}

	// A feeder that has not decoded any nav data must not clear it
	empty := PlaneLocation{Updates: Updates{Nav: t1.Add(time.Minute)}}
	merged, err = MergePlaneLocations(prev, empty)
	if err != nil {
		t.Fatalf("MergePlaneLocations() error = %v", err)
	}
	if got := unPtr(merged.NavAltitudeMCP); got != 22016 {
		t.Errorf("empty nav cleared the value: NavAltitudeMCP = %d, want 22016", got)
	}
	if !merged.Updates.Nav.Equal(t1) {
		t.Errorf("Updates.Nav = %v, want %v", merged.Updates.Nav, t1)
	}
}

func TestMergeEHS(t *testing.T) {
	t1 := time.Date(2026, time.October, 22, 0, 50, 0, 0, time.UTC)
	t2 := t1.Add(time.Second)

	prev := PlaneLocation{IAS: ptr(200), Mach: ptr(0.4), Updates: Updates{EHS: t1}}
	next := PlaneLocation{IAS: ptr(211), Mach: ptr(0.408), MagHeading: ptr(337.67578125), TrackRate: ptr(-0.125), BaroRate: ptr(-320), GeomRate: ptr(-352), Updates: Updates{EHS: t2}}

	merged, err := MergePlaneLocations(prev, next)
	if err != nil {
		t.Fatalf("MergePlaneLocations() error = %v", err)
	}
	want := PlaneLocation{IAS: ptr(211), Mach: ptr(0.408), MagHeading: ptr(337.67578125), TrackRate: ptr(-0.125), BaroRate: ptr(-320), GeomRate: ptr(-352), Updates: Updates{EHS: t2}}
	for name, pair := range map[string][2]any{
		"IAS":        {unPtr(merged.IAS), unPtr(want.IAS)},
		"Mach":       {unPtr(merged.Mach), unPtr(want.Mach)},
		"MagHeading": {unPtr(merged.MagHeading), unPtr(want.MagHeading)},
		"TrackRate":  {unPtr(merged.TrackRate), unPtr(want.TrackRate)},
		"BaroRate":   {unPtr(merged.BaroRate), unPtr(want.BaroRate)},
		"GeomRate":   {unPtr(merged.GeomRate), unPtr(want.GeomRate)},
	} {
		if !reflect.DeepEqual(pair[0], pair[1]) {
			t.Errorf("%s = %v, want %v", name, pair[0], pair[1])
		}
	}
	if !merged.Updates.EHS.Equal(t2) {
		t.Errorf("Updates.EHS = %v, want %v", merged.Updates.EHS, t2)
	}

	// stale
	merged, _ = MergePlaneLocations(next, prev)
	if got := unPtr(merged.IAS); got != 211 {
		t.Errorf("stale EHS accepted: IAS = %d, want 211", got)
	}
}

func TestNavJSONOmittedWhenAbsent(t *testing.T) {
	pl := PlaneLocation{Icao: "7CAF5F"}
	pl.InitSourceTags()
	b, err := pl.ToJSONBytes()
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"NavAltitudeMCP", "NavQNH", "NavModes", "IAS", "Mach", "MagHeading", "TrackRate", "BaroRate", "GeomRate"} {
		if contains(b, field) {
			t.Errorf("JSON carries %s for an aircraft without it: %s", field, b)
		}
	}
	pl.NavAltitudeMCP = ptr(22016)
	pl.NavModes = []string{"autopilot", "tcas"}
	b, _ = pl.ToJSONBytes()
	if !contains(b, `"NavAltitudeMCP":22016`) || !contains(b, `"NavModes":["autopilot","tcas"]`) {
		t.Errorf("JSON missing nav fields: %s", b)
	}
}

func contains(b []byte, s string) bool {
	return len(s) == 0 || (len(b) >= len(s) && indexOf(string(b), s) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
