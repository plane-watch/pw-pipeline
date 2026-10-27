package main

import "time"

// dedupeRef points at the first copy of a message: the open minute and the tile and
// index of its record, so later copies can be counted on it.
type dedupeRef struct {
	mn   *minute
	tile uint8
	idx  int
	at   int64 // ms, ingest clock
}

// dedupeWindow finds identical message bytes seen within a window of each other.
// Entries are bucketed by second of their time, so a lookup touches a handful of
// small maps and eviction drops whole seconds.
type dedupeWindow struct {
	windowMs int64
	buckets  map[int64]map[string]dedupeRef
	newest   int64 // ms, the latest time added
}

func newDedupeWindow(window time.Duration) *dedupeWindow {
	return &dedupeWindow{
		windowMs: window.Milliseconds(),
		buckets:  make(map[int64]map[string]dedupeRef),
	}
}

// lookup returns the entry for key within the window of at, if there is one.
func (w *dedupeWindow) lookup(key []byte, at int64) (dedupeRef, bool) {
	sec := at / 1000
	span := w.windowMs/1000 + 1
	for s := sec - span; s <= sec+span; s++ {
		b := w.buckets[s]
		if b == nil {
			continue
		}
		if r, ok := b[string(key)]; ok {
			d := at - r.at
			if d < 0 {
				d = -d
			}
			if d <= w.windowMs {
				return r, true
			}
		}
	}
	return dedupeRef{}, false
}

// add records key as first seen at ref.at.
func (w *dedupeWindow) add(key []byte, ref dedupeRef) {
	sec := ref.at / 1000
	b := w.buckets[sec]
	if b == nil {
		b = make(map[string]dedupeRef)
		w.buckets[sec] = b
	}
	b[string(key)] = ref
	if ref.at > w.newest {
		w.newest = ref.at
	}
}

// evict drops every bucket older than keep before the newest time added. keep should
// be well over the window, so a late batch still meets the copies that came first.
func (w *dedupeWindow) evict(keep time.Duration) {
	cutoff := (w.newest - keep.Milliseconds()) / 1000
	for s := range w.buckets {
		if s < cutoff {
			delete(w.buckets, s)
		}
	}
}

// size is the number of entries held, for tests and metrics.
func (w *dedupeWindow) size() int {
	n := 0
	for _, b := range w.buckets {
		n += len(b)
	}
	return n
}
