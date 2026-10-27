package main

import (
	"bytes"
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
	"plane.watch/lib/rawarchive"
	"plane.watch/lib/rawarchive/partition"
	"plane.watch/lib/tile_grid"
)

const (
	// beastHeaderLen is what precedes the Mode S bytes in a Beast frame: esc, type,
	// 6 byte receiver timestamp, signal level.
	beastHeaderLen = 9
	// dedupeKeep is how long de-duplication entries are held, well over the window,
	// so a batch that arrives late still meets the copies that came first.
	dedupeKeep = 20 * time.Second
	// skewWarn is how far a batch's start may be from the archiver's clock before
	// it is logged.
	skewWarn = 10 * time.Second
	// writeQueueLen is how many sealed partitions may wait for the store.
	writeQueueLen = 16
	// writeTimeout bounds one PUT.
	writeTimeout = 60 * time.Second
	// writeFailureHealthy is how recently a write may have failed with the archiver
	// still reporting healthy.
	writeFailureHealthy = 5 * time.Minute
	contentType         = "application/zstd"
)

var (
	// beastMLATMarker is the receiver timestamp readsb puts on MLAT results it
	// forwards; such frames are synthesised, not heard, and are not archived.
	beastMLATMarker = []byte{0xFF, 0x00, 0x4D, 0x4C, 0x41, 0x54}
	writeBackoff    = []time.Duration{time.Second, 5 * time.Second, 15 * time.Second}
)

type (
	// Config sets the archiver's timing and scope.
	Config struct {
		// BatchInterval is the interval the ingest instances publish at (their
		// --raw-archive-interval). It sets the seal time and how many batches an
		// instance is expected to send per minute.
		BatchInterval time.Duration
		// Grace is added to the seal time for clock skew and the archiver's own lag.
		Grace time.Duration
		// RestartDelay further delays the first, partial minute after a start, so an
		// archiver still running can write its complete copy first.
		RestartDelay time.Duration
		// DedupeWindow is how close two identical messages must be to count as copies.
		DedupeWindow time.Duration
		// Tiles limits what is archived; nil archives every tile.
		Tiles map[uint8]bool
		// GridVersion and Build go in every partition header.
		GridVersion uint16
		Build       uint32
		// Now is the archiver's clock.
		Now func() time.Time
		// SubDropped returns how many messages the subscription has discarded so far
		// because the archiver fell behind.
		SubDropped func() int64
		// Log receives the archiver's log lines.
		Log zerolog.Logger
	}

	// instance is an ingest process heard on the subject.
	instance struct {
		firstStart time.Time // of its first batch, ingest clock
		lastStart  time.Time // of its latest batch
	}

	// minute is a partition being filled.
	minute struct {
		start      time.Time
		tiles      map[uint8][]partition.Record
		hdr        partition.Header
		starts     map[string]map[int64]struct{} // instance → distinct batch starts
		restarted  bool
		incomplete bool
		sealed     bool
		sealAt     time.Time
	}

	// sealedMinute is a built object waiting to be written.
	sealedMinute struct {
		key  string
		body []byte
		hdr  partition.Header
	}

	// Archiver files batches into minutes, seals them and writes them to the store.
	Archiver struct {
		cfg   Config
		store ObjectStore
		log   zerolog.Logger

		mu             sync.Mutex
		minutes        map[int64]*minute // by start, Unix ms
		instances      map[string]*instance
		dedupe         *dedupeWindow
		firstMinute    time.Time
		oldestOpen     int64 // Unix ms; every minute before it is sealed or predates the start
		subDroppedSeen int64
		lastSkewWarn   time.Time
		lastBadWarn    time.Time

		writes       chan sealedMinute
		writerDone   chan struct{}
		lastWriteErr atomic.Int64 // Unix nanos
	}
)

// New returns an archiver that will start at cfg.Now().
func New(cfg Config, store ObjectStore) *Archiver {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.SubDropped == nil {
		cfg.SubDropped = func() int64 { return 0 }
	}
	if cfg.BatchInterval <= 0 {
		cfg.BatchInterval = 5 * time.Second
	}
	if cfg.Grace < 0 {
		cfg.Grace = 0
	}
	if cfg.DedupeWindow <= 0 {
		cfg.DedupeWindow = 2 * time.Second
	}
	a := &Archiver{
		cfg:        cfg,
		store:      store,
		log:        cfg.Log,
		minutes:    make(map[int64]*minute),
		instances:  make(map[string]*instance),
		dedupe:     newDedupeWindow(cfg.DedupeWindow),
		writes:     make(chan sealedMinute, writeQueueLen),
		writerDone: make(chan struct{}),
	}
	now := cfg.Now()
	a.firstMinute = now.UTC().Truncate(time.Minute)
	a.oldestOpen = a.firstMinute.UnixMilli()
	a.openMinute(a.firstMinute)
	return a
}

// Run seals minutes as they fall due and writes them until ctx is cancelled, then
// finishes the writes already queued. Minutes still open when it returns are not
// written: they would be partial, and an absent key reads as lost.
func (a *Archiver) Run(ctx context.Context) {
	go a.writer()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			close(a.writes)
			<-a.writerDone
			a.mu.Lock()
			open := len(a.minutes)
			a.mu.Unlock()
			if open > 0 {
				a.log.Warn().Int("minutes", open).Msg("stopping with minutes still open; they are not written")
			}
			return
		case now := <-ticker.C:
			for _, sm := range a.Tick(now) {
				a.enqueue(sm)
			}
		}
	}
}

func (a *Archiver) enqueue(sm sealedMinute) {
	select {
	case a.writes <- sm:
	default:
		metPartitions.WithLabelValues("failed").Inc()
		a.lastWriteErr.Store(time.Now().UnixNano())
		a.log.Error().Str("key", sm.key).Msg("write queue full; partition dropped")
	}
}

// Handle files one message from the subject.
func (a *Archiver) Handle(m *nats.Msg) {
	hdr, err := rawarchive.ParseHeader(m.Header)
	if err != nil {
		metBatchesBad.Inc()
		a.warnBad(err, m)
		return
	}
	recs, err := rawarchive.Decode(m.Data)
	if err != nil {
		// Keep what parsed; the batch is counted as bad as well.
		metBatchesBad.Inc()
		a.warnBad(err, m)
	}
	now := a.cfg.Now()

	a.mu.Lock()
	defer a.mu.Unlock()

	if skew := hdr.Start.Sub(now); skew > skewWarn || skew < -skewWarn {
		if now.Sub(a.lastSkewWarn) > time.Minute {
			a.lastSkewWarn = now
			a.log.Warn().Str("instance", hdr.Instance).Time("start", hdr.Start).Dur("skew", skew).
				Msg("batch start far from this clock; check NTP on the ingest host or here")
		}
	}

	mStart := hdr.Start.Truncate(time.Minute)
	mn := a.minuteFor(mStart)
	if mn == nil {
		metBatchesLate.Inc()
		metRecordsDropped.WithLabelValues(dropLate).Add(float64(len(recs)))
		if cur := a.minuteFor(now.UTC().Truncate(time.Minute)); cur != nil && cur.hdr.LateBatches < 0xFFFF {
			cur.hdr.LateBatches++
		}
		a.log.Warn().Str("instance", hdr.Instance).Time("start", hdr.Start).Int("records", len(recs)).
			Msg("batch for a sealed minute; dropped")
		return
	}
	metBatches.Inc()

	inst := a.instances[hdr.Instance]
	if inst == nil {
		inst = &instance{firstStart: hdr.Start}
		a.instances[hdr.Instance] = inst
		metInstances.Set(float64(len(a.instances)))
		a.log.Info().Str("instance", hdr.Instance).Msg("new ingest instance")
	}
	if hdr.Start.After(inst.lastStart) {
		inst.lastStart = hdr.Start
	}
	starts := mn.starts[hdr.Instance]
	if starts == nil {
		starts = make(map[int64]struct{})
		mn.starts[hdr.Instance] = starts
	}
	starts[hdr.Start.UnixMilli()] = struct{}{}
	if hdr.Dropped > 0 {
		metBatchesDropped.Inc()
		if !mn.incomplete {
			a.log.Warn().Str("instance", hdr.Instance).Uint64("dropped", hdr.Dropped).Time("minute", mn.start).
				Msg("instance dropped frames; minute marked incomplete")
		}
		mn.incomplete = true
	}
	for _, r := range recs {
		a.addRecord(mn, hdr.Start, r)
	}
}

func (a *Archiver) warnBad(err error, m *nats.Msg) {
	now := a.cfg.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	if now.Sub(a.lastBadWarn) > time.Minute {
		a.lastBadWarn = now
		a.log.Warn().Err(err).Str("subject", m.Subject).Int("bytes", len(m.Data)).Msg("message is not a usable batch")
	}
}

// minuteFor returns the open minute starting at start, opening it if needed, or nil
// if that minute is sealed or predates the archiver.
func (a *Archiver) minuteFor(start time.Time) *minute {
	ms := start.UnixMilli()
	if ms < a.oldestOpen {
		return nil
	}
	if mn := a.minutes[ms]; mn != nil {
		return mn
	}
	return a.openMinute(start)
}

func (a *Archiver) openMinute(start time.Time) *minute {
	mn := &minute{
		start:  start,
		tiles:  make(map[uint8][]partition.Record),
		starts: make(map[string]map[int64]struct{}),
	}
	mn.sealAt = start.Add(time.Minute + a.cfg.BatchInterval + a.cfg.Grace)
	if start.Equal(a.firstMinute) {
		mn.restarted = true
		mn.sealAt = mn.sealAt.Add(a.cfg.RestartDelay)
	}
	a.minutes[start.UnixMilli()] = mn
	metOpenMinutes.Set(float64(len(a.minutes)))
	return mn
}

func (a *Archiver) wantTile(t uint8) bool {
	return a.cfg.Tiles == nil || a.cfg.Tiles[t]
}

func (a *Archiver) addRecord(mn *minute, batchStart time.Time, r rawarchive.Record) {
	at := batchStart.Add(time.Duration(r.Offset) * time.Millisecond)
	offset := int32(at.Sub(mn.start) / time.Millisecond)
	switch r.Type {
	case rawarchive.TypeBeast:
		if len(r.Data) < beastHeaderLen {
			metRecordsDropped.WithLabelValues(dropBadLength).Inc()
			return
		}
		if bytes.Equal(r.Data[2:8], beastMLATMarker) {
			mn.hdr.MLATMarker++
			metRecordsDropped.WithLabelValues(dropMLATMarker).Inc()
			return
		}
		a.addFeeder(mn, at, offset, r.Tile, r.Data[beastHeaderLen:], "beast")
	case rawarchive.TypeAVR:
		a.addFeeder(mn, at, offset, r.Tile, r.Data, "avr")
	case rawarchive.TypeSBS1:
		a.addMLAT(mn, offset, r.Data)
	default:
		metRecordsDropped.WithLabelValues(dropUnknownType).Inc()
	}
}

func (a *Archiver) addFeeder(mn *minute, at time.Time, offset int32, tile uint8, msg []byte, typ string) {
	if len(msg) != 7 && len(msg) != 14 {
		metRecordsDropped.WithLabelValues(dropBadLength).Inc()
		return
	}
	if tile == rawarchive.TileUnknown {
		mn.hdr.UnknownTile++
		metRecordsDropped.WithLabelValues(dropUnknownTile).Inc()
		return
	}
	if !a.wantTile(tile) {
		metRecordsDropped.WithLabelValues(dropFilteredTile).Inc()
		return
	}
	atMs := at.UnixMilli()
	if ref, ok := a.dedupe.lookup(msg, atMs); ok {
		if !ref.mn.sealed {
			if recs := ref.mn.tiles[ref.tile]; ref.idx < len(recs) && recs[ref.idx].Copies < 255 {
				recs[ref.idx].Copies++
			}
		}
		mn.hdr.Duplicates++
		metDuplicates.Inc()
		return
	}
	recs := mn.tiles[tile]
	a.dedupe.add(msg, dedupeRef{mn: mn, tile: tile, idx: len(recs), at: atMs})
	mn.tiles[tile] = append(recs, partition.Record{
		OffsetMs: offset,
		Source:   partition.SourceFeeder,
		Copies:   1,
		Data:     msg,
	})
	metRecords.WithLabelValues(typ).Inc()
}

func (a *Archiver) addMLAT(mn *minute, offset int32, line []byte) {
	if len(line) > partition.MaxMLATLine {
		mn.hdr.MLATTooLong++
		metRecordsDropped.WithLabelValues(dropTooLong).Inc()
		return
	}
	lat, lon, ok := sbsPosition(line)
	if !ok {
		mn.hdr.MLATNoPos++
		metRecordsDropped.WithLabelValues(dropNoPosition).Inc()
		return
	}
	tile := rawarchive.TileNumber(tile_grid.LookupTile(lat, lon))
	if tile == rawarchive.TileUnknown {
		mn.hdr.MLATNoPos++
		metRecordsDropped.WithLabelValues(dropNoPosition).Inc()
		return
	}
	if !a.wantTile(tile) {
		metRecordsDropped.WithLabelValues(dropFilteredTile).Inc()
		return
	}
	mn.tiles[tile] = append(mn.tiles[tile], partition.Record{
		OffsetMs: offset,
		Source:   partition.SourceMLAT,
		Copies:   1,
		Data:     line,
	})
	metRecords.WithLabelValues("sbs1").Inc()
}

// Tick opens the minutes the clock has reached, notes subscription drops, and seals
// every minute that is due, returning the objects to write in order.
func (a *Archiver) Tick(now time.Time) []sealedMinute {
	a.mu.Lock()
	defer a.mu.Unlock()

	current := now.UTC().Truncate(time.Minute)
	for ms := a.oldestOpen; ms <= current.UnixMilli(); ms += int64(time.Minute / time.Millisecond) {
		if a.minutes[ms] == nil {
			a.openMinute(time.UnixMilli(ms).UTC())
		}
	}

	if d := a.cfg.SubDropped(); d > a.subDroppedSeen {
		metSubscriptionDropped.Add(float64(d - a.subDroppedSeen))
		a.log.Warn().Int64("dropped", d-a.subDroppedSeen).Msg("subscription dropped batches; open minutes marked incomplete")
		a.subDroppedSeen = d
		for _, mn := range a.minutes {
			mn.incomplete = true
		}
	}

	var due []*minute
	for _, mn := range a.minutes {
		if !mn.sealAt.After(now) {
			due = append(due, mn)
		}
	}
	sort.Slice(due, func(i, j int) bool { return due[i].start.Before(due[j].start) })
	out := make([]sealedMinute, 0, len(due))
	for _, mn := range due {
		out = append(out, a.seal(mn))
		delete(a.minutes, mn.start.UnixMilli())
		if end := mn.start.Add(time.Minute).UnixMilli(); end > a.oldestOpen {
			a.oldestOpen = end
		}
	}
	metOpenMinutes.Set(float64(len(a.minutes)))

	a.dedupe.evict(dedupeKeep)
	metDedupeEntries.Set(float64(a.dedupe.size()))
	return out
}

// seal checks the instances heard, builds the object and marks the minute sealed.
func (a *Archiver) seal(mn *minute) sealedMinute {
	mn.sealed = true
	end := mn.start.Add(time.Minute)
	perMinute := int(time.Minute / a.cfg.BatchInterval)

	for name, starts := range mn.starts {
		expected := perMinute
		if inst := a.instances[name]; inst != nil && inst.firstStart.After(mn.start) {
			// First heard during this minute: expect batches from its first interval on.
			first := rawarchive.IntervalStart(inst.firstStart, a.cfg.BatchInterval)
			expected = int(end.Sub(first) / a.cfg.BatchInterval)
		}
		if len(starts) < expected {
			if !mn.incomplete {
				a.log.Warn().Str("instance", name).Int("batches", len(starts)).Int("expected", expected).
					Time("minute", mn.start).Msg("instance missed intervals; minute marked incomplete")
			}
			mn.incomplete = true
		}
	}
	for name, inst := range a.instances {
		if _, present := mn.starts[name]; present {
			continue
		}
		if inst.firstStart.Before(mn.start) && !inst.lastStart.Before(mn.start.Add(-time.Minute)) {
			// Heard in the previous minute, silent in this one: it went away.
			a.log.Warn().Str("instance", name).Time("minute", mn.start).Msg("instance went silent; minute marked incomplete")
			mn.incomplete = true
		}
		if inst.lastStart.Before(mn.start) {
			// Nothing from it in this minute or later: forget it, so it is flagged once.
			delete(a.instances, name)
		}
	}
	metInstances.Set(float64(len(a.instances)))

	hdr := mn.hdr
	hdr.Start = mn.start
	hdr.GridVersion = a.cfg.GridVersion
	hdr.Build = a.cfg.Build
	hdr.Instances = uint16(min(len(mn.starts), 0xFFFF))
	if mn.incomplete {
		hdr.Flags |= partition.FlagIncomplete
		metPartitionsIncomplete.Inc()
	}
	if mn.restarted {
		hdr.Flags |= partition.FlagRestarted
	}
	body, err := partition.Build(hdr, mn.tiles)
	if err != nil {
		// Only a record the archiver itself built wrongly can cause this.
		a.log.Error().Err(err).Time("minute", mn.start).Msg("building partition failed; writing it empty")
		body, _ = partition.Build(hdr, nil)
	}
	sm := sealedMinute{key: partition.Key(mn.start), body: body, hdr: hdr}
	if idx, err := partition.ReadIndex(body); err == nil {
		sm.hdr = idx.Header
	}
	a.log.Info().Str("key", sm.key).Uint32("records", sm.hdr.Records).Uint32("mlat", sm.hdr.MLATRecords).
		Uint32("duplicates", sm.hdr.Duplicates).Uint16("tiles", sm.hdr.Tiles).Uint16("instances", sm.hdr.Instances).
		Uint16("flags", sm.hdr.Flags).Int("bytes", len(body)).Msg("sealed")
	return sm
}

// writer puts sealed minutes in the store, retrying a few times.
func (a *Archiver) writer() {
	defer close(a.writerDone)
	for sm := range a.writes {
		a.write(sm)
	}
}

func (a *Archiver) write(sm sealedMinute) {
	var err error
	for attempt := 0; ; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
		var created bool
		created, err = a.store.Put(ctx, sm.key, sm.body, contentType)
		cancel()
		if err == nil {
			if created {
				metPartitions.WithLabelValues("written").Inc()
				metPartitionBytes.Add(float64(len(sm.body)))
			} else {
				metPartitions.WithLabelValues("exists").Inc()
				a.log.Warn().Str("key", sm.key).Msg("partition already exists; another archiver wrote it first")
			}
			return
		}
		if attempt >= len(writeBackoff) {
			break
		}
		a.log.Warn().Err(err).Str("key", sm.key).Int("attempt", attempt+1).Msg("write failed; retrying")
		time.Sleep(writeBackoff[attempt])
	}
	metPartitions.WithLabelValues("failed").Inc()
	a.lastWriteErr.Store(time.Now().UnixNano())
	a.log.Error().Err(err).Str("key", sm.key).Int("bytes", len(sm.body)).Msg("write failed; partition lost")
}

// HealthCheckName names the archiver in /status.
func (a *Archiver) HealthCheckName() string { return "Archiver" }

// HealthCheck is false for a while after a partition could not be written.
func (a *Archiver) HealthCheck() bool {
	last := a.lastWriteErr.Load()
	return last == 0 || time.Since(time.Unix(0, last)) > writeFailureHealthy
}
