package main

import (
	"testing"
	"time"
)

func TestDedupeWindow(t *testing.T) {
	w := newDedupeWindow(2 * time.Second)
	key := []byte{1, 2, 3, 4, 5, 6, 7}
	w.add(key, dedupeRef{at: 1_999, idx: 1})
	cases := []struct {
		at   int64
		want bool
	}{
		{1_999, true},
		{3_998, true},  // 1,999 ms later, in another second bucket
		{3_999, true},  // exactly the window
		{4_000, false}, // one past it
		{0, true},      // before it, within the window
		{-2, false},
	}
	for _, c := range cases {
		if ref, ok := w.lookup(key, c.at); ok != c.want || (ok && ref.idx != 1) {
			t.Errorf("lookup at %d: %v %+v, want %v", c.at, ok, ref, c.want)
		}
	}
	if _, ok := w.lookup([]byte{1, 2, 3, 4, 5, 6, 8}, 1_999); ok {
		t.Error("different bytes matched")
	}

	// A later unrelated sighting keeps both; the earlier one is found first.
	w.add(key, dedupeRef{at: 10_000, idx: 2})
	if ref, ok := w.lookup(key, 9_500); !ok || ref.idx != 2 {
		t.Errorf("lookup at 9500: %v %+v", ok, ref)
	}
	if w.size() != 2 {
		t.Errorf("size %d", w.size())
	}
	w.evict(5 * time.Second) // newest is 10,000: drop seconds before 5
	if w.size() != 1 {
		t.Errorf("size after evict %d", w.size())
	}
	if _, ok := w.lookup(key, 1_999); ok {
		t.Error("evicted entry found")
	}
}
