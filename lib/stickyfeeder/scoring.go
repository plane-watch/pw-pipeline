package stickyfeeder

import "math"

// normalizePacketCount converts a packet count to a 0-1 scale based on the rate.
// Uses logarithmic scaling to compress the range:
// - A rate of ~30 packets/sec is considered "good"
// - A rate of ~100 packets/sec is considered "excellent"
// The log scale prevents high-rate feeders from completely dominating.
func normalizePacketCount(packetCount uint64, windowSeconds float64) float64 {
	if windowSeconds <= 0 {
		return 0
	}
	rate := float64(packetCount) / windowSeconds
	// Use log scale: log1p(rate) / log1p(maxExpectedRate)
	// maxExpectedRate of 100 means rates above 100/sec all score ~1.0
	normalized := math.Log1p(rate) / math.Log1p(100)
	return math.Min(1.0, normalized)
}

// normalizeRSSI converts a dBFS value to a 0-1 scale.
// dBFS (decibels relative to full scale) is always negative or zero:
// - 0 dBFS = maximum possible signal (ratio = 1.0)
// - -3 dBFS = half power (~0.7 voltage ratio)
// - -40 dBFS = very weak signal
//
// We map the practical range of -40 to -1 dBFS to 0.0 to 1.0.
// Values outside this range are clamped.
func normalizeRSSI(rssiDBFS float64) float64 {
	// Handle edge cases
	if math.IsNaN(rssiDBFS) || math.IsInf(rssiDBFS, 0) {
		return 0
	}

	// dBFS ranges from -inf to 0, but practical range is about -40 to -1
	// Map -40 -> 0.0, -1 -> 1.0
	const minDBFS = -40.0
	const maxDBFS = -1.0

	if rssiDBFS <= minDBFS {
		return 0.0
	}
	if rssiDBFS >= maxDBFS {
		return 1.0
	}

	// Linear mapping: (-40 to -1) -> (0 to 1)
	return (rssiDBFS - minDBFS) / (maxDBFS - minDBFS)
}

// calculateScore computes a weighted composite score for a feeder.
// The score is a weighted combination of normalized packet count and RSSI.
// Higher scores indicate better feeder quality.
func calculateScore(stats *feederStats, cfg *Config) float64 {
	windowSeconds := cfg.MetricDecayWindow.Seconds()

	packetScore := normalizePacketCount(stats.packetCount, windowSeconds)
	rssiScore := normalizeRSSI(stats.rssiEMA)

	return cfg.PacketCountWeight*packetScore + cfg.SignalWeight*rssiScore
}

// shouldSwitch determines if we should switch from the current sticky feeder to a challenger.
// The challenger must exceed the current feeder's score by at least the hysteresis threshold.
func shouldSwitch(currentScore, challengerScore float64, hysteresis float64) bool {
	// Challenger must be better by at least the hysteresis threshold
	threshold := currentScore * (1 + hysteresis)
	return challengerScore > threshold
}

// updateRSSI updates the exponential moving average for signal strength.
// Uses a smoothing factor (alpha) of 0.3 which provides a balance between
// responsiveness to changes and smoothing out noise.
func (s *feederStats) updateRSSI(newRSSI float64) {
	if math.IsNaN(newRSSI) || math.IsInf(newRSSI, 0) {
		return
	}

	const alpha = 0.3 // Smoothing factor - higher = more responsive to recent values
	if s.rssiEMA == 0 {
		// First sample - use it directly
		s.rssiEMA = newRSSI
	} else {
		// EMA: new_avg = alpha * new_value + (1 - alpha) * old_avg
		s.rssiEMA = alpha*newRSSI + (1-alpha)*s.rssiEMA
	}
}
