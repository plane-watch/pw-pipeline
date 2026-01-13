package stickyfeeder

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"plane.watch/lib/dedupe/forgetfulmap"
	"plane.watch/lib/tracker"
	"plane.watch/lib/tracker/beast"
	"plane.watch/lib/tracker/mode_s"
	"plane.watch/lib/tracker/sbs1"
)

var (
	prometheusFramesAccepted = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "pw_ingest",
		Subsystem: "sticky_feeder",
		Name:      "frames_accepted_total",
		Help:      "Total frames accepted (from sticky feeder or causing a switch)",
	})

	prometheusFramesRejected = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "pw_ingest",
		Subsystem: "sticky_feeder",
		Name:      "frames_rejected_total",
		Help:      "Total frames rejected (from non-sticky feeder)",
	})

	prometheusFeederSwitches = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "pw_ingest",
		Subsystem: "sticky_feeder",
		Name:      "feeder_switches_total",
		Help:      "Number of times aircraft switched to a new sticky feeder",
	})

	prometheusActiveAircraft = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "pw_ingest",
		Subsystem: "sticky_feeder",
		Name:      "active_aircraft",
		Help:      "Number of aircraft currently being tracked by sticky feeder",
	})

	prometheusSameTagDuplicates = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "pw_ingest",
		Subsystem: "sticky_feeder",
		Name:      "same_tag_duplicates_total",
		Help:      "Total duplicate frames from the same feeder tag (multiple receivers)",
	})
)

type (
	// Option is a functional option for Filter configuration
	Option func(*Filter)

	// Filter is the main sticky feeder middleware implementation
	Filter struct {
		config Config
		log    zerolog.Logger

		// Per-aircraft tracking, keyed by ICAO (uint32)
		aircraft *forgetfulmap.ForgetfulSyncMap

		// Same-tag deduplication, keyed by payload bytes (string)
		sameTagDedupe *forgetfulmap.ForgetfulSyncMap
	}

	// aircraftState tracks the sticky feeder state for a single aircraft
	aircraftState struct {
		mu sync.RWMutex

		// stickyFeeder is the tag of the currently preferred feeder for this aircraft
		stickyFeeder string

		// lockedAt is when we locked onto the current sticky feeder
		lockedAt time.Time

		// feeders contains per-feeder statistics for all feeders that have seen this aircraft
		feeders map[string]*feederStats

		// lastSeen is the last time any frame was received for this aircraft
		lastSeen time.Time
	}

	// feederStats tracks quality metrics for a single feeder on a single aircraft
	feederStats struct {
		// packetCount is the number of packets in the current time window
		packetCount uint64

		// rssiEMA is the exponential moving average of RSSI (in dBFS)
		rssiEMA float64

		// lastPacket is the time of the last packet from this feeder
		lastPacket time.Time

		// totalPackets is the total packets ever seen (for observability)
		totalPackets uint64

		// hasLoggedDuplicate tracks if we've already logged a same-tag duplicate warning
		hasLoggedDuplicate bool
	}
)

// NewFilter creates a new sticky feeder middleware filter
func NewFilter(opts ...Option) *Filter {
	f := &Filter{
		config: DefaultConfig(),
		log:    log.With().Str("section", "StickyFeeder").Logger(),
	}

	for _, opt := range opts {
		opt(f)
	}

	// Create the aircraft state map with automatic eviction
	f.aircraft = forgetfulmap.NewForgetfulSyncMap(
		forgetfulmap.WithSweepInterval(f.config.SweepInterval),
		forgetfulmap.WithForgettableAction(func(key, value any, added time.Time) bool {
			state, ok := value.(*aircraftState)
			if !ok {
				return true // Remove invalid entries
			}
			state.mu.RLock()
			lastSeen := state.lastSeen
			state.mu.RUnlock()

			return time.Since(lastSeen) > f.config.AircraftRetention
		}),
		forgetfulmap.WithPreEvictionAction(func(key, value any) {
			prometheusActiveAircraft.Dec()
		}),
	)

	// Create the same-tag dedupe map with short TTL
	f.sameTagDedupe = forgetfulmap.NewForgetfulSyncMap(
		forgetfulmap.WithOldAgeAfter(f.config.SameTagDedupeTTL),
		forgetfulmap.WithSweepInterval(f.config.SameTagDedupeTTL),
	)

	return f
}

// WithHysteresis sets the hysteresis threshold for feeder switching
func WithHysteresis(threshold float64) Option {
	return func(f *Filter) {
		f.config.HysteresisThreshold = threshold
	}
}

// WithPacketWeight sets the weight for packet count in scoring
func WithPacketWeight(weight float64) Option {
	return func(f *Filter) {
		f.config.PacketCountWeight = weight
	}
}

// WithSignalWeight sets the weight for signal strength in scoring
func WithSignalWeight(weight float64) Option {
	return func(f *Filter) {
		f.config.SignalWeight = weight
	}
}

// Handle processes a frame event and returns the frame if it should be passed through,
// or nil if it should be dropped.
func (f *Filter) Handle(fe *tracker.FrameEvent) tracker.Frame {
	if fe == nil {
		return nil
	}
	frame := fe.Frame()
	if frame == nil {
		return nil
	}

	icao := frame.Icao()
	if icao == 0 {
		return nil
	}

	source := fe.Source()
	if source == nil {
		return nil
	}
	feederTag := source.Tag

	// Get the payload key for same-tag deduplication
	payloadKey := f.getPayloadKey(frame)

	// Extract RSSI if available (only beast frames have it)
	var rssi float64
	if beastFrame, ok := frame.(*beast.Frame); ok {
		rssi = beastFrame.SignalRssi()
	}

	// Get or create aircraft state
	state := f.getOrCreateAircraftState(icao)

	// Process the frame and decide whether to accept or reject
	accepted, switched := state.processFrame(feederTag, rssi, payloadKey, &f.config, f.sameTagDedupe, &f.log)

	// Update metrics
	if accepted {
		prometheusFramesAccepted.Inc()
		if switched {
			prometheusFeederSwitches.Inc()
		}
		return frame
	}

	prometheusFramesRejected.Inc()
	return nil
}

// getPayloadKey extracts the raw payload bytes as a string key for deduplication
func (f *Filter) getPayloadKey(frame tracker.Frame) string {
	switch ft := frame.(type) {
	case *beast.Frame:
		return string(ft.AvrRaw())
	case *mode_s.Frame:
		return string(ft.Raw())
	case *sbs1.Frame:
		return string(ft.Raw())
	default:
		// Fallback to Raw() for unknown frame types
		return string(frame.Raw())
	}
}

// getOrCreateAircraftState retrieves or creates the state for an aircraft
func (f *Filter) getOrCreateAircraftState(icao uint32) *aircraftState {
	if existing, ok := f.aircraft.Load(icao); ok {
		return existing.(*aircraftState)
	}

	// Create new state
	state := &aircraftState{
		feeders:  make(map[string]*feederStats),
		lastSeen: time.Now(),
	}

	// Store - note: race is acceptable, we might briefly have duplicate state
	// but ForgetfulSyncMap.Store() will overwrite atomically
	f.aircraft.Store(icao, state)
	prometheusActiveAircraft.Inc()

	return state
}

// processFrame handles the frame processing logic for a single aircraft.
// Returns (accepted, switched) - whether the frame was accepted and whether we switched feeders.
func (s *aircraftState) processFrame(
	feederTag string,
	rssi float64,
	payloadKey string,
	cfg *Config,
	sameTagDedupe *forgetfulmap.ForgetfulSyncMap,
	logger *zerolog.Logger,
) (accepted bool, switched bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	s.lastSeen = now

	// Get or create feeder stats
	stats, exists := s.feeders[feederTag]
	if !exists {
		stats = &feederStats{}
		s.feeders[feederTag] = stats
	}

	// Same-tag dedupe check: if this is from the sticky feeder (or will become sticky),
	// check if we've seen this exact payload recently from the same tag
	if payloadKey != "" && (s.stickyFeeder == "" || s.stickyFeeder == feederTag) {
		dedupeKey := feederTag + ":" + payloadKey
		if sameTagDedupe.HasKeyStr(dedupeKey) {
			// Duplicate from same tag - log first occurrence per feeder
			if !stats.hasLoggedDuplicate {
				stats.hasLoggedDuplicate = true
				logger.Info().
					Str("feeder", feederTag).
					Msg("Detected same-tag duplicate frames (multiple receivers with same API key)")
			}
			prometheusSameTagDuplicates.Inc()
			return false, false
		}
	}

	// Update stats for this feeder
	stats.packetCount++
	stats.totalPackets++
	stats.lastPacket = now
	if rssi != 0 {
		stats.updateRSSI(rssi)
	}

	// First frame for this aircraft - lock onto this feeder
	if s.stickyFeeder == "" {
		s.stickyFeeder = feederTag
		s.lockedAt = now
		// Record payload for same-tag dedupe
		if payloadKey != "" {
			sameTagDedupe.AddKeyStr(feederTag + ":" + payloadKey)
		}
		return true, false
	}

	// Same feeder as sticky - always accept
	if s.stickyFeeder == feederTag {
		// Record payload for same-tag dedupe
		if payloadKey != "" {
			sameTagDedupe.AddKeyStr(feederTag + ":" + payloadKey)
		}
		return true, false
	}

	// Different feeder - check if we should switch
	stickyStats, stickyExists := s.feeders[s.stickyFeeder]
	if !stickyExists {
		// Sticky feeder has no stats (weird state) - switch to new feeder
		s.stickyFeeder = feederTag
		s.lockedAt = now
		if payloadKey != "" {
			sameTagDedupe.AddKeyStr(feederTag + ":" + payloadKey)
		}
		return true, true
	}

	// Calculate scores
	stickyScore := calculateScore(stickyStats, cfg)
	challengerScore := calculateScore(stats, cfg)

	// Check hysteresis
	if shouldSwitch(stickyScore, challengerScore, cfg.HysteresisThreshold) {
		// Switch to the challenger
		logger.Debug().
			Str("from", s.stickyFeeder).
			Str("to", feederTag).
			Float64("oldScore", stickyScore).
			Float64("newScore", challengerScore).
			Msg("Switching sticky feeder")

		s.stickyFeeder = feederTag
		s.lockedAt = now
		if payloadKey != "" {
			sameTagDedupe.AddKeyStr(feederTag + ":" + payloadKey)
		}
		return true, true
	}

	// Reject - not sticky and not compelling enough
	return false, false
}

// Stop stops the filter and releases resources
func (f *Filter) Stop() {
	f.aircraft.Stop()
	f.sameTagDedupe.Stop()
}

// String returns the name of this middleware
func (f *Filter) String() string {
	return "StickyFeeder"
}

// HealthCheckName returns the name for health checks
func (f *Filter) HealthCheckName() string {
	return "Sticky Feeder"
}

// HealthCheck performs a health check and logs status
func (f *Filter) HealthCheck() bool {
	f.log.Info().
		Int32("NumAircraft", f.aircraft.Len()).
		Int32("DedupeEntries", f.sameTagDedupe.Len()).
		Msg("Health Check")
	return true
}
