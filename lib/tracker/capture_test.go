package tracker_test

import (
	"bufio"
	"encoding/binary"
	"os"
	"testing"
	"time"

	"plane.watch/lib/producer"
	"plane.watch/lib/tracker"
	"plane.watch/lib/tracker/mode_s"
)

// TestNavFromBeastCapture replays a raw Beast capture through the tracker and
// reports how many aircraft yield navigation and enhanced surveillance data.
// It runs only when PW_BEAST_CAPTURE names a capture file; PW_BEAST_CAPTURE_START
// (RFC 3339) gives the time of its first frame, else the current time is
// used. The Perth capture in the Read5 repository
// (collector/testdata/beast/beast-30005-20261022T004216Z.bin, start
// 2026-10-22T00:42:16Z) carries 50,339 frames from about 32 aircraft.
//
// Frames are split with producer.ScanBeast and timed from the receiver's
// 12 MHz counter, so the 60 s rules in the tracker see the capture's own
// clock rather than the wall clock.
func TestNavFromBeastCapture(t *testing.T) {
	path := os.Getenv("PW_BEAST_CAPTURE")
	if path == "" {
		t.Skip("set PW_BEAST_CAPTURE to a Beast capture file")
	}
	start := time.Now()
	if s := os.Getenv("PW_BEAST_CAPTURE_START"); s != "" {
		var err error
		if start, err = time.Parse(time.RFC3339, s); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	trk := tracker.NewTracker()
	var frames, decoded int
	navSeen := map[uint32]bool{}
	ehsSeen := map[uint32]bool{}
	modesSeen := map[uint32]bool{}
	var firstTicks uint64
	sc := bufio.NewScanner(bufio.NewReader(f))
	sc.Split(producer.ScanBeast)
	for sc.Scan() {
		tok := sc.Bytes()
		if len(tok) < 10 || (tok[1] != 0x32 && tok[1] != 0x33) {
			continue // Mode A/C or receiver status
		}
		var ticksBuf [8]byte
		copy(ticksBuf[2:], tok[2:8])
		ticks := binary.BigEndian.Uint64(ticksBuf[:])
		frames++
		if firstTicks == 0 {
			firstTicks = ticks
		}
		ts := start.Add(time.Duration(ticks-firstTicks) * time.Second / 12_000_000)
		frame := mode_s.NewFrameFromBytes(ticks, tok[9:], ts)
		if err := frame.Decode(); err != nil {
			continue
		}
		decoded++
		if frame.Icao() == 0 {
			continue
		}
		p := trk.GetPlane(frame.Icao())
		p.HandleModeSFrame(&frame, nil)
		if nav, ok := p.Nav(); ok {
			if nav.SelectedAltitudeMCP != nil || nav.SelectedAltitudeFMS != nil {
				navSeen[frame.Icao()] = true
			}
			if nav.Modes != nil {
				modesSeen[frame.Icao()] = true
			}
		}
		if _, ok := p.EHS(); ok {
			ehsSeen[frame.Icao()] = true
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	planes := 0
	trk.EachPlane(func(*tracker.Plane) bool { planes++; return true })
	t.Logf("%d frames, %d decoded, %d aircraft; selected altitude from %d, nav modes from %d, EHS from %d",
		frames, decoded, planes, len(navSeen), len(modesSeen), len(ehsSeen))
	if len(navSeen) == 0 || len(ehsSeen) == 0 {
		t.Errorf("no aircraft yielded nav (%d) or EHS (%d) data", len(navSeen), len(ehsSeen))
	}
}
