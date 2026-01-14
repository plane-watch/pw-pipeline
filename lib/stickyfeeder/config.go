package stickyfeeder

import "time"

// Config holds all configurable parameters for the sticky feeder middleware
type Config struct {
	// HysteresisThreshold is the minimum improvement required to switch feeders.
	// A value of 0.10 means a challenger must be 10% better than the current sticky feeder.
	HysteresisThreshold float64

	// PacketCountWeight is the weight applied to packet count in the scoring algorithm.
	// Must be between 0.0 and 1.0. All weights should sum to 1.0.
	PacketCountWeight float64

	// SignalWeight is the weight applied to signal strength (RSSI) in the scoring algorithm.
	// Must be between 0.0 and 1.0. All weights should sum to 1.0.
	SignalWeight float64

	// LatenessWeight is the weight applied to relative latency in the scoring algorithm.
	// Higher lateness scores (closer to 1.0) mean the feeder is typically faster.
	// Must be between 0.0 and 1.0. All weights should sum to 1.0.
	LatenessWeight float64

	// HonestyWeight is the weight applied to consensus/honesty scoring.
	// Higher honesty scores mean the feeder agrees with other feeders more often.
	// Must be between 0.0 and 1.0. All weights should sum to 1.0.
	HonestyWeight float64

	// MetricDecayWindow is the time window used for normalizing packet counts.
	// Packet rates are calculated relative to this window.
	MetricDecayWindow time.Duration

	// AircraftRetention is how long to remember aircraft state after last seeing them.
	// Should match the tracker's pruneAfter setting.
	AircraftRetention time.Duration

	// SweepInterval is how often to check for stale aircraft entries.
	// Should match the tracker's pruneTick setting.
	SweepInterval time.Duration

	// SameTagDedupeTTL is how long to remember payloads for same-tag deduplication.
	// This prevents duplicate frames from multiple receivers with the same feeder tag.
	SameTagDedupeTTL time.Duration

	// StalenessThreshold is how long without a packet before a feeder is considered stale.
	// When the sticky feeder is stale, challengers can take over more easily.
	StalenessThreshold time.Duration

	// BackgroundInterval is how often the background worker runs to compute
	// latency and honesty scores.
	BackgroundInterval time.Duration

	// LatencyWindowSize is the number of arrival samples to keep for latency calculation.
	LatencyWindowSize int

	// LatenessThreshold is the relative delay (from first arrival) beyond which
	// a feeder starts getting penalized. Feeders within this threshold are considered "on time".
	LatenessThreshold time.Duration

	// PositionToleranceMeters is the maximum position difference allowed before
	// considering feeders to be in disagreement for honesty scoring.
	PositionToleranceMeters float64

	// CoordinationEnabled enables multi-instance coordination via NATS.
	CoordinationEnabled bool

	// ClaimTTL is how long remote claims are considered valid before expiry.
	// If an instance goes silent, its claims expire after this duration.
	ClaimTTL time.Duration

	// ClaimBatchInterval is how often to publish periodic claim batches.
	ClaimBatchInterval time.Duration

	// ClaimQueueSize is the buffer size for outbound claims.
	ClaimQueueSize int

	// ScoreChangeThreshold is the minimum score change (0.0-1.0) to trigger a claim.
	ScoreChangeThreshold float64
}

// DefaultConfig returns the default configuration for the sticky feeder middleware
func DefaultConfig() Config {
	return Config{
		HysteresisThreshold:     0.10,                   // 10% better to switch
		PacketCountWeight:       0.5,                    // Reliability is important
		SignalWeight:            0.25,                   // Signal quality matters
		LatenessWeight:          0.15,                   // Relative latency
		HonestyWeight:           0.10,                   // Consensus with other feeders
		MetricDecayWindow:       30 * time.Second,       // 30 second window for rate calculation
		AircraftRetention:       5 * time.Minute,        // Match tracker pruneAfter
		SweepInterval:           10 * time.Second,       // Match tracker pruneTick
		SameTagDedupeTTL:        2 * time.Second,        // Short window for same-tag duplicates
		StalenessThreshold:      1 * time.Second,        // Consider stale after 1s without packets
		BackgroundInterval:      5 * time.Second,        // Compute scores every 5s
		LatencyWindowSize:       100,                    // Keep last 100 arrival samples
		LatenessThreshold:       100 * time.Millisecond, // Penalty kicks in after 100ms relative delay
		PositionToleranceMeters: 500,                    // Allow 500m difference before disagreement
		CoordinationEnabled:     true,                   // Enable coordination by default
		ClaimTTL:                15 * time.Second,       // Remote claims expire after 15s
		ClaimBatchInterval:      5 * time.Second,        // Publish claims every 5s
		ClaimQueueSize:          10000,                  // Buffer for outbound claims
		ScoreChangeThreshold:    0.05,                   // 5% score change triggers a claim
	}
}
