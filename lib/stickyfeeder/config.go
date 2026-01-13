package stickyfeeder

import "time"

// Config holds all configurable parameters for the sticky feeder middleware
type Config struct {
	// HysteresisThreshold is the minimum improvement required to switch feeders.
	// A value of 0.10 means a challenger must be 10% better than the current sticky feeder.
	HysteresisThreshold float64

	// PacketCountWeight is the weight applied to packet count in the scoring algorithm.
	// Must be between 0.0 and 1.0. PacketCountWeight + SignalWeight should equal 1.0.
	PacketCountWeight float64

	// SignalWeight is the weight applied to signal strength (RSSI) in the scoring algorithm.
	// Must be between 0.0 and 1.0. PacketCountWeight + SignalWeight should equal 1.0.
	SignalWeight float64

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
}

// DefaultConfig returns the default configuration for the sticky feeder middleware
func DefaultConfig() Config {
	return Config{
		HysteresisThreshold: 0.10,             // 10% better to switch
		PacketCountWeight:   0.6,              // Weight packet count more heavily
		SignalWeight:        0.4,              // Signal is secondary
		MetricDecayWindow:   30 * time.Second, // 30 second window for rate calculation
		AircraftRetention:   5 * time.Minute,  // Match tracker pruneAfter
		SweepInterval:       10 * time.Second, // Match tracker pruneTick
		SameTagDedupeTTL:    2 * time.Second,  // Short window for same-tag duplicates
	}
}
