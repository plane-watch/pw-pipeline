package main

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
	"plane.watch/lib/rawarchive"
	"plane.watch/lib/rawarchive/partition"
)

var (
	msgLong   = []byte{0x8d, 0x7c, 0x49, 0xf8, 0x58, 0x41, 0xd2, 0x6c, 0xca, 0x39, 0x33, 0xe4, 0x1e, 0xcf}
	msgLong2  = []byte{0x8D, 0x48, 0xFC, 0x83, 0x1C, 0x4D, 0x04, 0xCD, 0x14, 0x48, 0x20, 0x72, 0x37, 0xC3}
	msgShort  = []byte{0x5d, 0x7c, 0x49, 0xf8, 0x28, 0xe9, 0x43}
	perthLine = []byte("MSG,3,1,1,7C6DB8,1,2026/10/25,01:02:03.456,2026/10/25,01:02:04.000,,3400,,,-31.950000,115.860000,,,,,,")
	noPosLine = []byte("MSG,3,1,1,7C6DB8,1,2026/10/25,01:02:03.456,2026/10/25,01:02:04.000,,3400,,,,,,,,,,")
	// t0 is 12 s into the archiver's first minute.
	t0      = time.Date(2026, 10, 24, 6, 30, 12, 0, time.UTC)
	minute0 = t0.Truncate(time.Minute)
)

const tilePerth = 35

type fakeStore struct {
	mu      sync.Mutex
	objects map[string][]byte
	fail    int // Puts to fail before succeeding
	puts    int
}

func newFakeStore() *fakeStore { return &fakeStore{objects: make(map[string][]byte)} }

func (s *fakeStore) Put(_ context.Context, key string, body []byte, _ string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.puts++
	if s.fail > 0 {
		s.fail--
		return false, errors.New("store down")
	}
	if _, ok := s.objects[key]; ok {
		return false, nil
	}
	s.objects[key] = bytes.Clone(body)
	return true, nil
}

func (s *fakeStore) get(t *testing.T, key string) (partition.Index, map[uint8][]partition.Record) {
	t.Helper()
	s.mu.Lock()
	body, ok := s.objects[key]
	s.mu.Unlock()
	if !ok {
		t.Fatalf("no object %s", key)
	}
	idx, tiles, err := partition.ReadAll(body)
	if err != nil {
		t.Fatalf("%s: %v", key, err)
	}
	return idx, tiles
}

func (s *fakeStore) keys() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.objects)
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

type harness struct {
	a       *Archiver
	store   *fakeStore
	clock   *clock
	dropped int64
}

func newHarness(t *testing.T, tiles map[uint8]bool) *harness {
	t.Helper()
	h := &harness{store: newFakeStore(), clock: &clock{t: t0}}
	h.a = New(Config{
		BatchInterval: 5 * time.Second,
		Grace:         5 * time.Second,
		RestartDelay:  15 * time.Second,
		DedupeWindow:  2 * time.Second,
		Tiles:         tiles,
		GridVersion:   1,
		Build:         0x216c1ce0,
		Now:           h.clock.now,
		SubDropped:    func() int64 { return h.dropped },
		Log:           zerolog.Nop(),
	}, h.store)
	return h
}

// tickAndWrite advances the clock, seals what is due and writes it synchronously.
func (h *harness) tickAndWrite(now time.Time) []sealedMinute {
	h.clock.set(now)
	out := h.a.Tick(now)
	for _, sm := range out {
		h.a.write(sm)
	}
	return out
}

type rec struct {
	typ    uint8
	tile   uint8
	offset uint16
	data   []byte
}

func beastFrame(msg []byte) []byte {
	typ := byte(0x33)
	if len(msg) == 7 {
		typ = 0x32
	}
	f := []byte{0x1a, typ, 0x22, 0x1b, 0x54, 0xac, 0xc2, 0xe9, 0x28}
	return append(f, msg...)
}

func mlatMarkedFrame(msg []byte) []byte {
	f := beastFrame(msg)
	copy(f[2:8], beastMLATMarker)
	return f
}

func beastRec(tile uint8, offset uint16, msg []byte) rec {
	return rec{typ: rawarchive.TypeBeast, tile: tile, offset: offset, data: beastFrame(msg)}
}

func sbsRec(offset uint16, line []byte) rec {
	return rec{typ: rawarchive.TypeSBS1, tile: rawarchive.TileUnknown, offset: offset, data: line}
}

func batch(instance string, start time.Time, dropped uint64, recs ...rec) *nats.Msg {
	enc := rawarchive.NewEncoder(0)
	for _, r := range recs {
		if err := enc.Append(r.typ, r.tile, r.offset, r.data); err != nil {
			panic(err)
		}
	}
	m := nats.NewMsg("raw.test")
	rawarchive.SetHeader(m, rawarchive.Header{Instance: instance, Start: start, Dropped: dropped})
	m.Data = bytes.Clone(enc.Bytes())
	return m
}

// sendMinute sends one batch per 5 s interval of the minute starting at start, from
// the interval at offset from onwards, each with the given records.
func (h *harness) sendMinute(instance string, start time.Time, from time.Duration, recs ...rec) int {
	n := 0
	for off := from; off < time.Minute; off += 5 * time.Second {
		h.a.Handle(batch(instance, start.Add(off), 0, recs...))
		n++
	}
	return n
}

func TestFilesAndSealsMinutes(t *testing.T) {
	h := newHarness(t, nil)
	m1 := minute0.Add(time.Minute)

	// The archiver started at :12; instance a's first batch is for the interval at :15.
	h.sendMinute("a", minute0, 15*time.Second, beastRec(tilePerth, 1234, msgLong), sbsRec(10, perthLine))
	h.sendMinute("a", m1, 0, beastRec(tilePerth, 1234, msgLong), sbsRec(10, perthLine))

	// The first minute0 seals at :00 + 5 s + 5 s + 15 s of the next.
	if out := h.tickAndWrite(m1.Add(24 * time.Second)); len(out) != 0 {
		t.Fatalf("sealed %d minutes a second early", len(out))
	}
	out := h.tickAndWrite(m1.Add(25 * time.Second))
	if len(out) != 1 || out[0].key != "raw/v1/20261024/0630.zst" {
		t.Fatalf("sealed %+v", out)
	}
	idx, tiles := h.store.get(t, out[0].key)
	hdr := idx.Header
	if hdr.Flags != partition.FlagRestarted {
		t.Errorf("first minute0 flags %d, want restarted only", hdr.Flags)
	}
	if hdr.Records != 18 || hdr.FeederRecords != 9 || hdr.MLATRecords != 9 || hdr.Instances != 1 || hdr.Tiles != 1 {
		t.Errorf("first minute0 header %+v", hdr)
	}
	if hdr.GridVersion != 1 || hdr.Build != 0x216c1ce0 || !hdr.Start.Equal(minute0) {
		t.Errorf("first minute0 header %+v", hdr)
	}
	recs := tiles[tilePerth]
	if len(recs) != 18 {
		t.Fatalf("tile35 has %d records", len(recs))
	}
	// Batch at :15 with offset 1234 ms: 16234 ms into the minute0; the SBS line at 15010.
	if recs[0].OffsetMs != 16234 || recs[0].Source != partition.SourceFeeder || !bytes.Equal(recs[0].Data, msgLong) || recs[0].Copies != 1 {
		t.Errorf("first record %+v", recs[0])
	}
	if recs[1].OffsetMs != 15010 || recs[1].Source != partition.SourceMLAT || !bytes.Equal(recs[1].Data, perthLine) {
		t.Errorf("second record %+v", recs[1])
	}

	// The second minute0 seals at :10 of the third, without the restart delay.
	out = h.tickAndWrite(m1.Add(70 * time.Second))
	if len(out) != 1 || out[0].key != "raw/v1/20261024/0631.zst" {
		t.Fatalf("sealed %+v", out)
	}
	idx, _ = h.store.get(t, out[0].key)
	if idx.Header.Flags != 0 || idx.Header.Records != 24 || idx.Header.Instances != 1 {
		t.Errorf("second minute0 header %+v", idx.Header)
	}
}

func TestEmptyMinutesAreWritten(t *testing.T) {
	h := newHarness(t, nil)
	h.tickAndWrite(minute0.Add(5 * time.Minute))
	if n := h.store.keys(); n != 4 {
		t.Fatalf("%d objects written for 4 whole minutes with no batches", n)
	}
	idx, tiles := h.store.get(t, "raw/v1/20261024/0632.zst")
	if idx.Header.Records != 0 || len(tiles) != 0 || idx.Header.Flags != 0 {
		t.Errorf("empty minute0: %+v", idx.Header)
	}
	idx, _ = h.store.get(t, "raw/v1/20261024/0630.zst")
	if idx.Header.Flags != partition.FlagRestarted {
		t.Errorf("first minute0 flags %d", idx.Header.Flags)
	}
}

func TestDuplicatesMerged(t *testing.T) {
	h := newHarness(t, nil)
	m1 := minute0.Add(time.Minute)
	// Two instances hear the same message 800 ms apart; b also hears another one.
	h.a.Handle(batch("a", m1, 0, beastRec(tilePerth, 100, msgLong)))
	h.a.Handle(batch("b", m1, 0, beastRec(tilePerth, 900, msgLong), beastRec(36, 950, msgLong2)))
	// The same bytes again 3 s later are a new record.
	h.a.Handle(batch("a", m1.Add(5*time.Second), 0, beastRec(tilePerth, 100, msgLong)))
	// Fill the rest of the minute0 so neither instance looks like it missed intervals.
	h.sendMinute("a", m1, 10*time.Second)
	h.sendMinute("b", m1, 5*time.Second)

	out := h.tickAndWrite(m1.Add(70 * time.Second))
	key := "raw/v1/20261024/0631.zst"
	if len(out) != 2 || out[1].key != key {
		t.Fatalf("sealed %+v", out)
	}
	idx, tiles := h.store.get(t, key)
	if idx.Header.Flags != 0 {
		t.Errorf("flags %d", idx.Header.Flags)
	}
	if idx.Header.Records != 3 || idx.Header.Duplicates != 1 || idx.Header.Instances != 2 {
		t.Errorf("header %+v", idx.Header)
	}
	recs := tiles[tilePerth]
	if len(recs) != 2 || recs[0].Copies != 2 || recs[0].OffsetMs != 100 || recs[1].Copies != 1 || recs[1].OffsetMs != 5100 {
		t.Errorf("tile35 %+v", recs)
	}
	if len(tiles[36]) != 1 {
		t.Errorf("tile36 %+v", tiles[36])
	}
}

func TestIncompleteMarks(t *testing.T) {
	t.Run("missed interval", func(t *testing.T) {
		h := newHarness(t, nil)
		m1 := minute0.Add(time.Minute)
		for off := time.Duration(0); off < time.Minute; off += 5 * time.Second {
			if off == 30*time.Second {
				continue
			}
			h.a.Handle(batch("a", m1.Add(off), 0))
		}
		h.tickAndWrite(m1.Add(70 * time.Second))
		idx, _ := h.store.get(t, "raw/v1/20261024/0631.zst")
		if idx.Header.Flags&partition.FlagIncomplete == 0 {
			t.Error("11 of 12 batches, not marked incomplete")
		}
	})
	t.Run("instance reported drops", func(t *testing.T) {
		h := newHarness(t, nil)
		m1 := minute0.Add(time.Minute)
		h.sendMinute("a", m1, 0)
		h.a.Handle(batch("a", m1.Add(20*time.Second), 3))
		h.tickAndWrite(m1.Add(70 * time.Second))
		idx, _ := h.store.get(t, "raw/v1/20261024/0631.zst")
		if idx.Header.Flags&partition.FlagIncomplete == 0 {
			t.Error("dropped header 3, not marked incomplete")
		}
	})
	t.Run("subscription dropped", func(t *testing.T) {
		h := newHarness(t, nil)
		m1 := minute0.Add(time.Minute)
		h.sendMinute("a", m1, 0)
		h.dropped = 1
		h.tickAndWrite(m1.Add(70 * time.Second))
		idx, _ := h.store.get(t, "raw/v1/20261024/0631.zst")
		if idx.Header.Flags&partition.FlagIncomplete == 0 {
			t.Error("slow consumer drop, not marked incomplete")
		}
		// Every minute open when the drop was noticed is marked, the one just opened
		// included; the next is clean again.
		h.sendMinute("a", m1.Add(time.Minute), 0)
		h.sendMinute("a", m1.Add(2*time.Minute), 0)
		h.tickAndWrite(m1.Add(190 * time.Second))
		idx, _ = h.store.get(t, "raw/v1/20261024/0632.zst")
		if idx.Header.Flags&partition.FlagIncomplete == 0 {
			t.Error("minute open at the drop not marked incomplete")
		}
		idx, _ = h.store.get(t, "raw/v1/20261024/0633.zst")
		if idx.Header.Flags&partition.FlagIncomplete != 0 {
			t.Error("minute after the drop still marked incomplete")
		}
	})
	t.Run("complete minute0 is clean", func(t *testing.T) {
		h := newHarness(t, nil)
		m1 := minute0.Add(time.Minute)
		h.sendMinute("a", m1, 0)
		h.tickAndWrite(m1.Add(70 * time.Second))
		idx, _ := h.store.get(t, "raw/v1/20261024/0631.zst")
		if idx.Header.Flags != 0 {
			t.Errorf("flags %d", idx.Header.Flags)
		}
	})
}

func TestInstanceComesAndGoes(t *testing.T) {
	h := newHarness(t, nil)
	m1 := minute0.Add(time.Minute)
	m2 := m1.Add(time.Minute)
	m3 := m2.Add(time.Minute)
	// a runs through minute0 1 and 2 and stops; b starts half way through minute0 2.
	h.sendMinute("a", m1, 0)
	h.sendMinute("a", m2, 0)
	h.sendMinute("b", m2, 30*time.Second)
	h.sendMinute("b", m3, 0)
	h.tickAndWrite(m1.Add(70 * time.Second))
	idx, _ := h.store.get(t, "raw/v1/20261024/0631.zst")
	if idx.Header.Flags != 0 || idx.Header.Instances != 1 {
		t.Errorf("minute0 1 (b not yet started must not count as silent): %+v", idx.Header)
	}
	h.tickAndWrite(m2.Add(70 * time.Second))
	idx, _ = h.store.get(t, "raw/v1/20261024/0632.zst")
	if idx.Header.Flags != 0 || idx.Header.Instances != 2 {
		t.Errorf("minute0 2 (b started mid-minute0): %+v", idx.Header)
	}
	h.tickAndWrite(m3.Add(70 * time.Second))
	idx, _ = h.store.get(t, "raw/v1/20261024/0633.zst")
	if idx.Header.Flags&partition.FlagIncomplete == 0 || idx.Header.Instances != 1 {
		t.Errorf("minute0 3 (a went silent): %+v", idx.Header)
	}
	// a is forgotten after one flag.
	h.sendMinute("b", m3.Add(time.Minute), 0)
	h.tickAndWrite(m3.Add(130 * time.Second))
	idx, _ = h.store.get(t, "raw/v1/20261024/0634.zst")
	if idx.Header.Flags != 0 {
		t.Errorf("minute0 4 still flagged for a: %+v", idx.Header)
	}
}

func TestLateAndEarlyBatchesDropped(t *testing.T) {
	h := newHarness(t, nil)
	m1 := minute0.Add(time.Minute)
	// A batch for the minute0 before the archiver started.
	h.a.Handle(batch("a", minute0.Add(-30*time.Second), 0, beastRec(tilePerth, 0, msgLong)))
	h.sendMinute("a", m1, 0)
	h.tickAndWrite(m1.Add(70 * time.Second))
	// Now a batch for minute0 1, already sealed.
	h.a.Handle(batch("a", m1.Add(55*time.Second), 0, beastRec(tilePerth, 0, msgLong2)))
	h.sendMinute("a", m1.Add(time.Minute), 0)
	h.tickAndWrite(m1.Add(130 * time.Second))

	idx, _ := h.store.get(t, "raw/v1/20261024/0630.zst")
	if idx.Header.LateBatches != 1 || idx.Header.Records != 0 {
		t.Errorf("first minute0: %+v", idx.Header)
	}
	idx, _ = h.store.get(t, "raw/v1/20261024/0631.zst")
	if idx.Header.Records != 0 {
		t.Errorf("sealed minute0 changed: %+v", idx.Header)
	}
	idx, _ = h.store.get(t, "raw/v1/20261024/0632.zst")
	if idx.Header.LateBatches != 1 || idx.Header.Records != 0 {
		t.Errorf("minute0 after the late batch: %+v", idx.Header)
	}
}

func TestRecordFilters(t *testing.T) {
	h := newHarness(t, map[uint8]bool{tilePerth: true})
	m1 := minute0.Add(time.Minute)
	longLine := append(bytes.Clone(perthLine), bytes.Repeat([]byte("x"), 200)...)
	h.a.Handle(batch("a", m1, 0,
		beastRec(tilePerth, 0, msgLong),                                                              // kept
		rec{typ: rawarchive.TypeAVR, tile: tilePerth, offset: 1, data: msgShort},                     // kept
		rec{typ: rawarchive.TypeBeast, tile: tilePerth, offset: 2, data: mlatMarkedFrame(msgLong2)},  // MLAT marker
		beastRec(rawarchive.TileUnknown, 3, msgLong2),                                                // no position
		beastRec(36, 4, msgLong2),                                                                    // not an archived tile
		rec{typ: rawarchive.TypeBeast, tile: tilePerth, offset: 5, data: beastFrame(msgLong2)[:8+9]}, // 8 byte message
		rec{typ: 9, tile: tilePerth, offset: 6, data: msgLong2},                                      // unknown type
		sbsRec(7, perthLine), // kept
		sbsRec(8, noPosLine), // no position
		sbsRec(9, longLine),  // too long
		sbsRec(10, []byte("MSG,3,1,1,7C6DB8,1,2026/10/25,01:02:03.456,2026/10/25,01:02:04.000,,,,,-32.0,140.0,,,,,,")), // east of tile35, filtered
	))
	h.sendMinute("a", m1, 5*time.Second)
	h.tickAndWrite(m1.Add(70 * time.Second))
	idx, tiles := h.store.get(t, "raw/v1/20261024/0631.zst")
	hdr := idx.Header
	if hdr.Records != 3 || hdr.FeederRecords != 2 || hdr.MLATRecords != 1 || hdr.Tiles != 1 {
		t.Errorf("counts %+v", hdr)
	}
	if hdr.MLATMarker != 1 || hdr.UnknownTile != 1 || hdr.MLATNoPos != 1 || hdr.MLATTooLong != 1 || hdr.Duplicates != 0 {
		t.Errorf("drop counters %+v", hdr)
	}
	recs := tiles[tilePerth]
	if len(recs) != 3 || !bytes.Equal(recs[0].Data, msgLong) || !bytes.Equal(recs[1].Data, msgShort) || !bytes.Equal(recs[2].Data, perthLine) {
		t.Errorf("records %+v", recs)
	}
}

func TestExistingObjectIsNotOverwritten(t *testing.T) {
	h := newHarness(t, nil)
	key := "raw/v1/20261024/0630.zst"
	h.store.objects[key] = []byte("older archiver's copy")
	h.sendMinute("a", minute0, 15*time.Second, beastRec(tilePerth, 0, msgLong))
	h.tickAndWrite(minute0.Add(85 * time.Second))
	if string(h.store.objects[key]) != "older archiver's copy" {
		t.Error("the restarted minute0 overwrote an existing object")
	}
	if !h.a.HealthCheck() {
		t.Error("an existing object is not a failure")
	}
}

func TestWriteRetriesThenGivesUp(t *testing.T) {
	saved := writeBackoff
	writeBackoff = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { writeBackoff = saved })

	h := newHarness(t, nil)
	h.store.fail = 2
	h.tickAndWrite(minute0.Add(85 * time.Second))
	if h.store.keys() != 1 || h.store.puts != 3 {
		t.Errorf("after two failures: %d objects, %d puts", h.store.keys(), h.store.puts)
	}
	if !h.a.HealthCheck() {
		t.Error("unhealthy after a write that succeeded on retry")
	}

	h.store.fail = 10
	h.tickAndWrite(minute0.Add(130 * time.Second))
	if h.store.keys() != 1 {
		t.Error("object appeared although every put failed")
	}
	if h.a.HealthCheck() {
		t.Error("healthy right after a partition was lost")
	}
}

func TestBadMessagesCounted(t *testing.T) {
	h := newHarness(t, nil)
	m := nats.NewMsg("raw.test")
	m.Data = []byte("not a batch")
	h.a.Handle(m) // no headers
	m = batch("a", minute0.Add(15*time.Second), 0, beastRec(tilePerth, 0, msgLong))
	m.Data = m.Data[:len(m.Data)-3] // truncated record
	h.a.Handle(m)
	h.tickAndWrite(minute0.Add(85 * time.Second))
	idx, _ := h.store.get(t, "raw/v1/20261024/0630.zst")
	if idx.Header.Records != 0 || idx.Header.Instances != 1 {
		t.Errorf("header %+v", idx.Header)
	}
}

func TestRunStops(t *testing.T) {
	h := newHarness(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		h.a.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestBuildID(t *testing.T) {
	if got, ok := hexPrefix("216c1ce0abcd"); !ok || got != 0x216c1ce0 {
		t.Errorf("hexPrefix = %x %v", got, ok)
	}
	if _, ok := hexPrefix("v1.2.3"); ok {
		t.Error("version string taken as a hash")
	}
	if _, ok := hexPrefix("dev"); ok {
		t.Error("short string taken as a hash")
	}
}
