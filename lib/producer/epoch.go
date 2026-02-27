package producer

import (
	"sync"
	"time"
)

// EpochDetector detects MLAT epoch changes and stores the epoch start time.
// The epoch ID is the unix timestamp (seconds) when the receiver powered on:
// epoch_id = uint32(now_unix_seconds - mlat_ticks_in_seconds)
//
// Using seconds (not nanoseconds) avoids uint32 overflow -- wraps every ~136 years.
// All frames from the same receiver will have the same epoch_id until it restarts.
// When MLAT ticks reset or jump backwards significantly, it signals either:
// 1. A sub-producer restart (same receiver, new epoch)
// 2. A new sub-producer behind the aggregator (different receiver)
type EpochDetector struct {
	mu sync.RWMutex

	// currentEpochID is the unix timestamp (seconds) when the receiver powered on.
	// Calculated as: now_unix - mlat_seconds. All frames from same receiver have same ID.
	currentEpochID uint32

	// lastTicks is the last MLAT tick value seen
	lastTicks time.Duration

	// lastSeen is when we last saw a frame from this feeder
	lastSeen time.Time

	// staleTimeout: if we haven't seen frames for this long, consider the epoch stale
	staleTimeout time.Duration

	// resetThreshold: if ticks jump backwards by more than this, assume restart/new sub-producer
	// This filters out minor clock jitter and NTP adjustments
	resetThreshold time.Duration

	// initialized tracks whether we've seen the first frame
	initialized bool
}

// NewEpochDetector creates a new epoch detector for a feeder.
// staleTimeout defines how long to wait before considering an epoch stale.
func NewEpochDetector(staleTimeout time.Duration) *EpochDetector {
	return &EpochDetector{
		currentEpochID: 0,
		staleTimeout:   staleTimeout,
		resetThreshold: 5 * time.Second, // Filter out jitter < 5 seconds, detect real restarts
		initialized:    false,
	}
}

// ProcessTicks processes MLAT ticks (in nanoseconds) and returns the current epoch ID.
// The epoch ID is: uint32(now_unix_seconds - mlat_seconds) — the unix timestamp when
// the receiver powered on. Using seconds avoids uint32 overflow (wraps every ~136 years).
func (ed *EpochDetector) ProcessTicks(ticks time.Duration) uint32 {
	ed.mu.Lock()
	defer ed.mu.Unlock()

	now := time.Now()
	epochStart := func() uint32 {
		return uint32(now.Unix() - int64(ticks.Seconds()))
	}

	// First frame - calculate when this receiver powered on
	if !ed.initialized {
		ed.initialized = true
		ed.currentEpochID = epochStart()
		ed.lastTicks = ticks
		ed.lastSeen = now
		return ed.currentEpochID
	}

	// Check if current epoch is stale
	if now.Sub(ed.lastSeen) > ed.staleTimeout {
		ed.currentEpochID = epochStart()
		ed.lastTicks = ticks
		ed.lastSeen = now
		return ed.currentEpochID
	}

	// Check for backwards jump (reset/new sub-producer)
	// Only trigger if backwards jump exceeds threshold (filters network jitter)
	if ticks < ed.lastTicks && (ed.lastTicks-ticks) > ed.resetThreshold {
		ed.currentEpochID = epochStart()
		ed.lastTicks = ticks
		ed.lastSeen = now
		return ed.currentEpochID
	}

	// Normal progression - same epoch
	if ticks > ed.lastTicks {
		ed.lastTicks = ticks
		ed.lastSeen = now
	}

	return ed.currentEpochID
}

// CurrentEpochID returns the current epoch ID without processing ticks
func (ed *EpochDetector) CurrentEpochID() uint32 {
	ed.mu.RLock()
	defer ed.mu.RUnlock()
	return ed.currentEpochID
}
