package producer

import (
	"math"
	"sync"
	"time"
)

// tickStream represents one physical receiver's tick progression with a stable epoch ID.
type tickStream struct {
	epochID   uint32        // Stable: computed once at stream creation
	lastTicks time.Duration // Most recent tick value from this receiver
	lastSeen  time.Time     // Wall-clock time of most recent frame
}

// EpochDetector detects MLAT epoch changes for multiple concurrent receivers behind
// a single Beast feeder connection.
//
// A single feeder (e.g., SBKP-1742) can aggregate multiple physical receivers, each
// with independent MLAT clocks. The detector maintains a slice of tickStream entries,
// one per receiver, and matches incoming ticks to the correct stream based on proximity.
//
// The epoch ID is the unix timestamp (seconds) when the receiver powered on:
// epoch_id = uint32(now_unix_seconds - mlat_ticks_in_seconds)
type EpochDetector struct {
	mu             sync.Mutex
	streams        []tickStream
	staleTimeout   time.Duration // 30s default
	resetThreshold time.Duration // 5s default (jitter tolerance)
	maxStreams     int           // 10 (hard cap)
}

// NewEpochDetector creates a new epoch detector for a feeder.
// staleTimeout defines how long to wait before considering a stream stale.
func NewEpochDetector(staleTimeout time.Duration) *EpochDetector {
	return &EpochDetector{
		staleTimeout:   staleTimeout,
		resetThreshold: 5 * time.Second,
		maxStreams:     10,
	}
}

// ProcessTicks processes MLAT ticks and returns the epoch ID for the matching receiver stream.
// The epoch ID is: uint32(now_unix_seconds - mlat_seconds) — the unix timestamp when
// the receiver powered on.
func (ed *EpochDetector) ProcessTicks(ticks time.Duration) uint32 {
	ed.mu.Lock()
	defer ed.mu.Unlock()

	now := time.Now()

	// Step 1: Evict stale streams
	alive := ed.streams[:0]
	for _, s := range ed.streams {
		if now.Sub(s.lastSeen) <= ed.staleTimeout {
			alive = append(alive, s)
		}
	}
	ed.streams = alive

	// Step 2: Match incoming ticks to an existing stream.
	// A stream matches if ticks falls within [lastTicks - resetThreshold, lastTicks + staleTimeout].
	// Among matches, pick the closest by absolute distance.
	bestIdx := -1
	bestDist := time.Duration(math.MaxInt64)
	for i, s := range ed.streams {
		lo := s.lastTicks - ed.resetThreshold
		hi := s.lastTicks + ed.staleTimeout
		if ticks >= lo && ticks <= hi {
			dist := ticks - s.lastTicks
			if dist < 0 {
				dist = -dist
			}
			if dist < bestDist {
				bestDist = dist
				bestIdx = i
			}
		}
	}

	// Step 3: Match found — update stream
	if bestIdx >= 0 {
		s := &ed.streams[bestIdx]
		if ticks > s.lastTicks {
			s.lastTicks = ticks
		}
		s.lastSeen = now
		return s.epochID
	}

	// Step 4: No match — new receiver. Create a new stream.
	epochID := uint32(now.Unix() - int64(ticks.Seconds()))

	// If at capacity, evict least-recently-seen stream.
	if len(ed.streams) >= ed.maxStreams {
		lruIdx := 0
		for i := 1; i < len(ed.streams); i++ {
			if ed.streams[i].lastSeen.Before(ed.streams[lruIdx].lastSeen) {
				lruIdx = i
			}
		}
		ed.streams[lruIdx] = tickStream{
			epochID:   epochID,
			lastTicks: ticks,
			lastSeen:  now,
		}
		return epochID
	}

	ed.streams = append(ed.streams, tickStream{
		epochID:   epochID,
		lastTicks: ticks,
		lastSeen:  now,
	})
	return epochID
}

// CurrentEpochID returns the epoch ID of the most recently seen stream.
func (ed *EpochDetector) CurrentEpochID() uint32 {
	ed.mu.Lock()
	defer ed.mu.Unlock()
	if len(ed.streams) == 0 {
		return 0
	}
	// Return the most recently seen stream's epoch ID
	latest := ed.streams[0]
	for _, s := range ed.streams[1:] {
		if s.lastSeen.After(latest.lastSeen) {
			latest = s
		}
	}
	return latest.epochID
}
