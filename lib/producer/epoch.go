package producer

import (
	"sync"
	"time"
)

// tickStream represents one physical receiver's tick progression with a stable epoch ID.
type tickStream struct {
	epochID   uint32        // Stable: computed once at stream creation
	lastTicks time.Duration // Most recent tick value from this receiver
	lastSeen  time.Time     // Wall-clock time of most recent frame
}

// epochLookupTolerance is the maximum difference (seconds) between a candidate
// epoch ID and a stored epoch ID for them to be considered the same receiver.
// Handles ±1 from integer truncation jitter, plus minor crystal drift.
const epochLookupTolerance = 2

// staleSweepThreshold triggers a full stale-entry sweep when the map exceeds
// this size during new-entry creation.
const staleSweepThreshold = 1000

// EpochDetector detects MLAT epoch changes for multiple concurrent receivers behind
// a single Beast feeder connection.
//
// A single feeder can aggregate many physical receivers (LEPP-2043 aggregates 7000+),
// each with independent MLAT clocks. The detector uses a map keyed by epoch ID
// (receiver power-on time in unix seconds) for O(1) lookup regardless of receiver count.
//
// The epoch ID is the unix timestamp (seconds) when the receiver powered on:
// epoch_id = uint32(now_unix_seconds - mlat_ticks_in_seconds)
type EpochDetector struct {
	mu           sync.Mutex
	streams      map[uint32]*tickStream
	staleTimeout time.Duration    // 30s default
	nowFunc      func() time.Time // injectable for testing; defaults to time.Now
}

// NewEpochDetector creates a new epoch detector for a feeder.
// staleTimeout defines how long to wait before considering a stream stale.
func NewEpochDetector(staleTimeout time.Duration) *EpochDetector {
	return &EpochDetector{
		staleTimeout: staleTimeout,
		streams:      make(map[uint32]*tickStream),
		nowFunc:      time.Now,
	}
}

// ProcessTicks processes MLAT ticks and returns the epoch ID for the matching receiver stream.
// The epoch ID is: uint32(now_unix_seconds - mlat_seconds) — the unix timestamp when
// the receiver powered on.
func (ed *EpochDetector) ProcessTicks(ticks time.Duration) uint32 {
	ed.mu.Lock()
	defer ed.mu.Unlock()

	now := ed.nowFunc()
	candidateEpoch := uint32(now.Unix() - int64(ticks.Seconds()))

	// Match by epoch ID with ±tolerance for integer truncation jitter.
	// This is O(1): at most 2*tolerance+1 = 5 map lookups regardless of receiver count.
	for delta := int64(-epochLookupTolerance); delta <= epochLookupTolerance; delta++ {
		key := uint32(int64(candidateEpoch) + delta)
		s, ok := ed.streams[key]
		if !ok {
			continue
		}
		if now.Sub(s.lastSeen) > ed.staleTimeout {
			delete(ed.streams, key)
			continue
		}
		// Match found — update stream
		if ticks > s.lastTicks {
			s.lastTicks = ticks
		}
		s.lastSeen = now
		return s.epochID
	}

	// No match — new receiver.
	// Sweep stale entries if map is getting large.
	if len(ed.streams) >= staleSweepThreshold {
		for k, s := range ed.streams {
			if now.Sub(s.lastSeen) > ed.staleTimeout {
				delete(ed.streams, k)
			}
		}
	}

	ed.streams[candidateEpoch] = &tickStream{
		epochID:   candidateEpoch,
		lastTicks: ticks,
		lastSeen:  now,
	}
	return candidateEpoch
}

// CurrentEpochID returns the epoch ID of the most recently seen stream.
func (ed *EpochDetector) CurrentEpochID() uint32 {
	ed.mu.Lock()
	defer ed.mu.Unlock()
	if len(ed.streams) == 0 {
		return 0
	}
	var latest *tickStream
	for _, s := range ed.streams {
		if latest == nil || s.lastSeen.After(latest.lastSeen) {
			latest = s
		}
	}
	return latest.epochID
}
