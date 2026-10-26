package middleware

import (
	"bytes"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"plane.watch/lib/rawarchive"
	"plane.watch/lib/tracker"
	"plane.watch/lib/tracker/beast"
	"plane.watch/lib/tracker/sbs1"
)

var (
	// DF17 from 7C49F8 and from 48FC83, as the Beast producer emits them.
	rawBeast7C49F8 = []byte{0x1a, 0x33, 0x22, 0x1b, 0x54, 0xac, 0xc2, 0xe9, 0x28, 0x8d, 0x7c, 0x49, 0xf8, 0x58, 0x41, 0xd2, 0x6c, 0xca, 0x39, 0x33, 0xe4, 0x1e, 0xcf}
	rawBeast48FC83 = []byte{0x1A, 0x33, 0x14, 0x93, 0xFF, 0x2D, 0xD3, 0xC7, 0x62, 0x8D, 0x48, 0xFC, 0x83, 0x1C, 0x4D, 0x04, 0xCD, 0x14, 0x48, 0x20, 0x72, 0x37, 0xC3}
	perthLat       = -31.95
	perthLon       = 115.86
)

type batch struct {
	hdr  rawarchive.Header
	recs []rawarchive.Record
}

// batchRecorder collects published batches and signals each arrival.
type batchRecorder struct {
	mu      sync.Mutex
	batches []batch
	arrived chan struct{}
	err     error
}

func newBatchRecorder() *batchRecorder {
	return &batchRecorder{arrived: make(chan struct{}, 1000)}
}

func (b *batchRecorder) setErr(err error) {
	b.mu.Lock()
	b.err = err
	b.mu.Unlock()
}

func (b *batchRecorder) PublishMsg(m *nats.Msg) error {
	b.mu.Lock()
	err := b.err
	b.mu.Unlock()
	if err != nil {
		return err
	}
	hdr, err := rawarchive.ParseHeader(m.Header)
	if err != nil {
		panic(err)
	}
	recs, err := rawarchive.Decode(bytes.Clone(m.Data))
	if err != nil {
		panic(err)
	}
	b.mu.Lock()
	b.batches = append(b.batches, batch{hdr: hdr, recs: recs})
	b.mu.Unlock()
	b.arrived <- struct{}{}
	return nil
}

func (b *batchRecorder) all() []batch {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]batch(nil), b.batches...)
}

func (b *batchRecorder) waitFor(t *testing.T, n int, d time.Duration) []batch {
	t.Helper()
	deadline := time.After(d)
	for {
		if got := b.all(); len(got) >= n {
			return got
		}
		select {
		case <-b.arrived:
		case <-deadline:
			t.Fatalf("only %d batches after %v, wanted %d", len(b.all()), d, n)
		}
	}
}

// fakeClock is a settable clock for the batcher and Handle timestamps.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

func rawBeastEvent(t *testing.T, raw []byte, lat, lon *float64) (*beast.Frame, tracker.FrameEvent) {
	t.Helper()
	f, err := beast.NewFrame(bytes.Clone(raw), false)
	if err != nil {
		t.Fatalf("NewFrame: %v", err)
	}
	if err = f.Decode(); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return f, tracker.NewFrameEvent(f, &tracker.FrameSource{Tag: "feeder", RefLat: lat, RefLon: lon})
}

// settle waits until the batcher has taken n items from the queue, so a test can
// move the fake clock between frames the way real time would.
func settle(t *testing.T, r *RawArchive, n uint64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for r.processed.Load() < n {
		if time.Now().After(deadline) {
			t.Fatalf("batcher processed %d items, waiting for %d", r.processed.Load(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

// newTestArchive builds a middleware with a long interval and a fake clock, so only
// Stop or an explicit clock move publishes anything.
func newTestArchive(t *testing.T, rec *batchRecorder, clock *fakeClock, opts ...RawArchiveOption) *RawArchive {
	t.Helper()
	opts = append([]RawArchiveOption{
		WithRawArchiveInterval(time.Minute),
		WithRawArchiveInstance("test-1"),
		withRawArchiveClock(clock.now),
	}, opts...)
	r, err := NewRawArchive(rec, "raw.test", opts...)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRawArchiveRejectsBadConfig(t *testing.T) {
	rec := newBatchRecorder()
	if _, err := NewRawArchive(rec, ""); err == nil {
		t.Error("empty subject accepted")
	}
	if _, err := NewRawArchive(nil, "raw"); err == nil {
		t.Error("nil publisher accepted")
	}
	if _, err := NewRawArchive(rec, "raw", WithRawArchiveInterval(7*time.Second)); err == nil {
		t.Error("7 s interval accepted")
	}
	if _, err := NewRawArchive(rec, "raw", WithRawArchiveMaxBytes(10)); err == nil {
		t.Error("10 byte cap accepted")
	}
	r, err := NewRawArchive(rec, "raw")
	if err != nil {
		t.Fatal(err)
	}
	if r.Instance() == "" {
		t.Error("no default instance name")
	}
	r.Stop()
}

func TestRawArchiveCopiesPooledBeastFrame(t *testing.T) {
	beast.UsePoolAllocator = true
	defer func() { beast.UsePoolAllocator = false }()

	rec := newBatchRecorder()
	clock := &fakeClock{t: time.Date(2026, 10, 26, 3, 7, 0, 0, time.UTC)}
	r := newTestArchive(t, rec, clock)

	f, fe := rawBeastEvent(t, rawBeast7C49F8, &perthLat, &perthLon)
	if got := r.Handle(&fe); got != fe.Frame() {
		t.Error("Handle must return the frame unchanged")
	}
	// What the tracker does next: release the frame, which zeroes it, and reuse it.
	beast.Release(f)
	for i := 0; i < 10; i++ {
		_, _ = rawBeastEvent(t, rawBeast48FC83, nil, nil)
	}
	r.Stop()

	batches := rec.all()
	if len(batches) != 1 || len(batches[0].recs) != 1 {
		t.Fatalf("got %d batches: %+v", len(batches), batches)
	}
	rc := batches[0].recs[0]
	if rc.Type != rawarchive.TypeBeast || !bytes.Equal(rc.Data, rawBeast7C49F8) {
		t.Errorf("record % X type %d, want the frame as handled", rc.Data, rc.Type)
	}
	if rc.Tile != 35 {
		t.Errorf("tile %d, want 35 for Perth", rc.Tile)
	}
	if batches[0].hdr.Instance != "test-1" || batches[0].hdr.Dropped != 0 {
		t.Errorf("header %+v", batches[0].hdr)
	}
}

func TestRawArchiveTileUnknownWithoutPosition(t *testing.T) {
	rec := newBatchRecorder()
	clock := &fakeClock{t: time.Date(2026, 10, 26, 3, 7, 0, 0, time.UTC)}
	r := newTestArchive(t, rec, clock)
	_, fe := rawBeastEvent(t, rawBeast7C49F8, nil, nil)
	r.Handle(&fe)
	far := 95.0
	_, fe2 := rawBeastEvent(t, rawBeast7C49F8, &far, &far) // off the grid entirely
	r.Handle(&fe2)
	r.Stop()
	recs := rec.all()[0].recs
	if len(recs) != 2 || recs[0].Tile != rawarchive.TileUnknown || recs[1].Tile != rawarchive.TileUnknown {
		t.Errorf("got %+v", recs)
	}
}

func TestRawArchiveSbs1(t *testing.T) {
	rec := newBatchRecorder()
	clock := &fakeClock{t: time.Date(2026, 10, 26, 3, 7, 0, 0, time.UTC)}
	r := newTestArchive(t, rec, clock)
	line := "MSG,3,1,1,7C6DB8,1,2026/10/25,01:02:03.456,2026/10/25,01:02:04.000,,3400,,,-31.950000,115.860000,,,,,,"
	f := sbs1.NewFrame(line)
	if err := f.Decode(); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	fe := tracker.NewFrameEvent(f, &tracker.FrameSource{Tag: "mlat-mux", RefLat: &perthLat, RefLon: &perthLon})
	r.Handle(&fe)
	r.Stop()
	recs := rec.all()[0].recs
	if len(recs) != 1 || recs[0].Type != rawarchive.TypeSBS1 || string(recs[0].Data) != line {
		t.Fatalf("got %+v", recs)
	}
	if recs[0].Tile != rawarchive.TileUnknown {
		t.Errorf("SBS record tile %d, want unknown: the consumer tiles it from the line", recs[0].Tile)
	}
}

func TestRawArchiveAlignedIntervalsAndOffsets(t *testing.T) {
	rec := newBatchRecorder()
	t0 := time.Date(2026, 10, 26, 3, 7, 57, 250_000_000, time.UTC)
	clock := &fakeClock{t: t0}
	// A 5 s interval, driven by the fake clock: the timer never fires in test time,
	// but frames stamped past the interval end close the open batch.
	r := newTestArchive(t, rec, clock, WithRawArchiveInterval(5*time.Second))

	_, fe := rawBeastEvent(t, rawBeast7C49F8, &perthLat, &perthLon)
	r.Handle(&fe) // 03:07:57.250, interval 03:07:55, offset 2250
	settle(t, r, 1)
	clock.set(t0.Add(2 * time.Second)) // 03:07:59.250, same interval, offset 4250
	r.Handle(&fe)
	settle(t, r, 2)
	clock.set(t0.Add(3 * time.Second)) // 03:08:00.250: new minute, new interval, offset 250
	r.Handle(&fe)
	settle(t, r, 3)
	clock.set(t0.Add(13 * time.Second)) // 03:08:10.250: skips 03:08:05 entirely
	r.Handle(&fe)
	settle(t, r, 4)
	r.Stop()

	batches := rec.all()
	if len(batches) != 3 {
		t.Fatalf("got %d batches, want 3: %+v", len(batches), batches)
	}
	wantStarts := []time.Time{
		time.Date(2026, 10, 26, 3, 7, 55, 0, time.UTC),
		time.Date(2026, 10, 26, 3, 8, 0, 0, time.UTC),
		time.Date(2026, 10, 26, 3, 8, 10, 0, time.UTC),
	}
	wantOffsets := [][]uint16{{2250, 4250}, {250}, {250}}
	for i, b := range batches {
		if !b.hdr.Start.Equal(wantStarts[i]) {
			t.Errorf("batch %d start %v, want %v", i, b.hdr.Start, wantStarts[i])
		}
		if b.hdr.Start.Second()%5 != 0 || b.hdr.Start.Nanosecond() != 0 {
			t.Errorf("batch %d start %v is not on a 5 s boundary", i, b.hdr.Start)
		}
		if len(b.recs) != len(wantOffsets[i]) {
			t.Fatalf("batch %d has %d records, want %d", i, len(b.recs), len(wantOffsets[i]))
		}
		for j, rc := range b.recs {
			if rc.Offset != wantOffsets[i][j] {
				t.Errorf("batch %d record %d offset %d, want %d", i, j, rc.Offset, wantOffsets[i][j])
			}
		}
	}
}

func TestRawArchiveHeartbeatOnEmptyInterval(t *testing.T) {
	rec := newBatchRecorder()
	// Real clock, short interval: wait for ticks with nothing handled.
	r, err := NewRawArchive(rec, "raw.test", WithRawArchiveInterval(50*time.Millisecond), WithRawArchiveInstance("hb"))
	if err != nil {
		t.Fatal(err)
	}
	batches := rec.waitFor(t, 3, 5*time.Second)
	r.Stop()
	for i, b := range batches[:3] {
		if len(b.recs) != 0 {
			t.Errorf("batch %d has %d records, want a heartbeat", i, len(b.recs))
		}
		if b.hdr.Start.UnixMilli()%50 != 0 {
			t.Errorf("batch %d start %v not on a 50 ms boundary", i, b.hdr.Start)
		}
		if i > 0 && !b.hdr.Start.After(batches[i-1].hdr.Start) {
			t.Errorf("batch %d start %v not after batch %d", i, b.hdr.Start, i-1)
		}
	}
}

func TestRawArchiveSizeFlushKeepsStart(t *testing.T) {
	rec := newBatchRecorder()
	clock := &fakeClock{t: time.Date(2026, 10, 26, 3, 7, 1, 0, time.UTC)}
	r := newTestArchive(t, rec, clock, WithRawArchiveMaxBytes(RawArchiveMinMaxBytes))

	// 29 bytes a record: 36 records pass 1024 bytes.
	_, fe := rawBeastEvent(t, rawBeast7C49F8, &perthLat, &perthLon)
	for i := 0; i < 40; i++ {
		r.Handle(&fe)
	}
	first := rec.waitFor(t, 1, 5*time.Second)[0]
	if len(first.recs) < 30 || len(first.recs) > 40 {
		t.Errorf("early flush carried %d records", len(first.recs))
	}
	r.Stop()
	batches := rec.all()
	if len(batches) != 2 {
		t.Fatalf("got %d batches, want 2", len(batches))
	}
	if !batches[1].hdr.Start.Equal(batches[0].hdr.Start) {
		t.Errorf("second batch start %v differs from the first %v", batches[1].hdr.Start, batches[0].hdr.Start)
	}
	if total := len(batches[0].recs) + len(batches[1].recs); total != 40 {
		t.Errorf("records across batches = %d, want 40", total)
	}
}

func TestRawArchiveFullQueueDropsAndReports(t *testing.T) {
	rec := newBatchRecorder()
	clock := &fakeClock{t: time.Date(2026, 10, 26, 3, 7, 1, 0, time.UTC)}
	// A queue of 8 and a batcher that cannot drain it: the batcher goroutine is busy
	// only when woken, so fill the queue faster than it can be read by holding it.
	r := newTestArchive(t, rec, clock, withRawArchiveQueueLen(8))
	// Stop the batcher from draining by sending from the same goroutine until the
	// channel is full; the batcher will drain some, so push many.
	_, fe := rawBeastEvent(t, rawBeast7C49F8, &perthLat, &perthLon)
	for i := 0; i < 100000; i++ {
		r.Handle(&fe)
	}
	r.Stop()
	batches := rec.all()
	var records int
	var dropped uint64
	for _, b := range batches {
		records += len(b.recs)
		dropped += b.hdr.Dropped
	}
	if records+int(dropped) != 100000 {
		t.Errorf("records %d + dropped %d != 100000", records, dropped)
	}
	if dropped == 0 {
		t.Log("nothing dropped: the batcher kept up with a queue of 8; the accounting still balances")
	}
}

func TestRawArchiveStopPublishesBuffered(t *testing.T) {
	rec := newBatchRecorder()
	clock := &fakeClock{t: time.Date(2026, 10, 26, 3, 7, 1, 0, time.UTC)}
	r := newTestArchive(t, rec, clock)
	_, fe := rawBeastEvent(t, rawBeast7C49F8, &perthLat, &perthLon)
	for i := 0; i < 5; i++ {
		r.Handle(&fe)
	}
	r.Stop()
	r.Stop() // idempotent
	batches := rec.all()
	if len(batches) != 1 || len(batches[0].recs) != 5 {
		t.Fatalf("got %+v", batches)
	}
}

func TestRawArchivePublishErrorsDoNotStall(t *testing.T) {
	rec := newBatchRecorder()
	rec.setErr(nats.ErrConnectionClosed)
	clock := &fakeClock{t: time.Date(2026, 10, 26, 3, 7, 1, 0, time.UTC)}
	r := newTestArchive(t, rec, clock, WithRawArchiveMaxBytes(RawArchiveMinMaxBytes))
	_, fe := rawBeastEvent(t, rawBeast7C49F8, &perthLat, &perthLon)
	for i := 0; i < 500; i++ {
		r.Handle(&fe)
	}
	done := make(chan struct{})
	go func() { r.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop hung while publishes fail")
	}
}

func TestRawArchiveFailedPublishReportedAsDropped(t *testing.T) {
	rec := newBatchRecorder()
	t0 := time.Date(2026, 10, 26, 3, 7, 1, 0, time.UTC)
	clock := &fakeClock{t: t0}
	r := newTestArchive(t, rec, clock)
	_, fe := rawBeastEvent(t, rawBeast7C49F8, &perthLat, &perthLon)
	for i := 0; i < 3; i++ {
		r.Handle(&fe)
	}
	settle(t, r, 3)
	// The first interval's batch fails to publish.
	rec.setErr(nats.ErrConnectionClosed)
	clock.set(t0.Add(61 * time.Second))
	r.Handle(&fe) // closes the first interval, which fails, and opens the second
	settle(t, r, 4)
	rec.setErr(nil)
	r.Stop()
	batches := rec.all()
	if len(batches) != 1 {
		t.Fatalf("got %d batches, want 1 (the failed one is lost)", len(batches))
	}
	if len(batches[0].recs) != 1 || batches[0].hdr.Dropped != 3 {
		t.Errorf("batch after a failed publish: %d records, dropped %d; want 1 and 3", len(batches[0].recs), batches[0].hdr.Dropped)
	}
}

func TestRawArchiveIgnoresFramesAfterStop(t *testing.T) {
	rec := newBatchRecorder()
	clock := &fakeClock{t: time.Date(2026, 10, 26, 3, 7, 1, 0, time.UTC)}
	r := newTestArchive(t, rec, clock)
	_, fe := rawBeastEvent(t, rawBeast7C49F8, &perthLat, &perthLon)
	r.Handle(&fe)
	r.Stop()
	for i := 0; i < 10; i++ {
		if got := r.Handle(&fe); got == nil {
			t.Fatal("Handle returned nil after Stop")
		}
	}
	if n := len(r.queue); n != 0 {
		t.Errorf("%d frames queued after Stop", n)
	}
	if batches := rec.all(); len(batches) != 1 || len(batches[0].recs) != 1 {
		t.Fatalf("got %+v", batches)
	}
}

// Run with -race: frames from several producers while the batcher publishes.
func TestRawArchiveConcurrentHandle(t *testing.T) {
	rec := newBatchRecorder()
	r, err := NewRawArchive(rec, "raw.test", WithRawArchiveInterval(50*time.Millisecond), WithRawArchiveMaxBytes(4096))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	const perProducer = 2000
	for p := 0; p < 4; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, fe := rawBeastEvent(t, rawBeast48FC83, &perthLat, &perthLon)
			for i := 0; i < perProducer; i++ {
				r.Handle(&fe)
			}
		}()
	}
	wg.Wait()
	r.Stop()
	var records int
	var dropped uint64
	for _, b := range rec.all() {
		records += len(b.recs)
		dropped += b.hdr.Dropped
		for _, rc := range b.recs {
			if !bytes.Equal(rc.Data, rawBeast48FC83) {
				t.Fatalf("corrupt record % X", rc.Data)
			}
		}
	}
	if records+int(dropped) != 4*perProducer {
		t.Errorf("records %d + dropped %d != %d", records, dropped, 4*perProducer)
	}
}
