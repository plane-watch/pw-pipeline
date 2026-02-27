// Package stickyfeeder implements the sticky feeder middleware for aircraft tracking.
//
// The sticky feeder algorithm picks one preferred data source (feeder) per aircraft
// and rejects competing sources unless they demonstrate significantly better performance.
// This improves data quality by avoiding interleaved position data from multiple sources
// with different latencies.
//
// # Epoch-Based Sub-Feeder Isolation
//
// When a single feeder connection aggregates multiple independent receivers (sub-producers),
// each receiver has its own MLAT epoch. The sticky feeder algorithm now treats each
// (feederTag, epochID) pair as a separate logical producer:
//
//   - Epoch detection happens in the Producer layer (lib/producer/epoch.go)
//   - Each frame is tagged with its MLAT epoch ID
//   - Sticky feeder internally uses composite keys like "rx-north#1" and "rx-north#2"
//   - Each epoch competes independently for being the preferred source
//   - When advertising to other instances, only the winning feeder tag is published (epoch hidden)
//
// This naturally filters out weaker sub-producers without explicit rejection logic.
// The coordinator remains unchanged - it sees clean feeder names without epoch details.
package stickyfeeder

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"plane.watch/lib/dedupe/forgetfulmap"
	"plane.watch/lib/nats_io"
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

	prometheusSameTagDuplicates = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "pw_ingest",
		Subsystem: "sticky_feeder",
		Name:      "same_tag_duplicates_total",
		Help:      "Total duplicate frames from the same feeder tag (multiple receivers)",
	}, []string{"feeder"})

	prometheusNonPositionDropped = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "pw_ingest",
		Subsystem: "sticky_feeder",
		Name:      "non_position_dropped_total",
		Help:      "Total non-position frames dropped from non-sticky feeders",
	})

	prometheusEpochChanges = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "pw_ingest",
		Subsystem: "sticky_feeder",
		Name:      "epoch_changes_total",
		Help:      "Number of MLAT epoch changes detected (sub-producer restarts/new sub-producers)",
	}, []string{"feeder"})

	prometheusActiveEpochs = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "pw_ingest",
		Subsystem: "sticky_feeder",
		Name:      "active_epochs",
		Help:      "Number of active epochs per feeder (sub-producers behind single feeder)",
	}, []string{"feeder"})
)

// EpochIDProvider is an interface for frames that can provide an epoch ID
type EpochIDProvider interface {
	EpochID() uint32
}

// producerKey creates a unique key for (feederTag, epochID) pair
// Used internally to treat each epoch as a separate logical producer
func producerKey(feederTag string, epochID uint32) string {
	if epochID == 0 {
		// Fallback for frames without epoch info (backwards compatibility)
		return feederTag
	}
	return fmt.Sprintf("%s#%d", feederTag, epochID)
}

// extractFeederTag extracts the feeder tag from a composite key (feederTag#epochID)
func extractFeederTag(compositeKey string) string {
	idx := strings.Index(compositeKey, "#")
	if idx < 0 {
		return compositeKey // Fallback for legacy keys without epoch suffix
	}
	return compositeKey[:idx]
}

type (
	// Option is a functional option for Filter configuration
	Option func(*Filter)

	// Filter is the main sticky feeder middleware implementation
	Filter struct {
		config Config
		log    zerolog.Logger

		// decayFactor is the per-tick multiplier for packet count decay.
		// Computed as pow(0.5, BackgroundInterval / MetricDecayWindow) so that
		// after one full MetricDecayWindow of silence, counts decay to 50% of peak.
		decayFactor float64

		// Per-aircraft tracking, keyed by ICAO (uint32)
		aircraft *forgetfulmap.ForgetfulSyncMap

		// Same-tag deduplication, keyed by payload bytes (string)
		sameTagDedupe *forgetfulmap.ForgetfulSyncMap

		// Track feeder tags we've already warned about for same-tag duplicates
		loggedDuplicateTags sync.Map

		// Background worker for latency and honesty scoring
		background *BackgroundWorker

		// Coordinator for multi-instance coordination (nil if disabled)
		coordinator *Coordinator

		// lastSeenEpochID tracks the highest epoch ID seen per feeder, for metrics
		// No explicit cleanup needed - sync.Map is garbage collected when Filter is freed.
		lastSeenEpochID sync.Map // map[string]uint32 keyed by feeder tag
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

	// Pre-compute the per-tick decay factor for packet counts.
	// pow(0.5, interval/window) means after one full MetricDecayWindow of silence,
	// counts decay to 50% of their peak value.
	f.decayFactor = math.Pow(0.5, f.config.BackgroundInterval.Seconds()/f.config.MetricDecayWindow.Seconds())

	// Create and start the background worker for latency/honesty scoring
	f.background = NewBackgroundWorker(&f.config, f.log)
	f.background.SetDecayCallback(f.decayPacketCounts)
	f.background.Start()

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

// WithLatenessWeight sets the weight for relative latency in scoring
func WithLatenessWeight(weight float64) Option {
	return func(f *Filter) {
		f.config.LatenessWeight = weight
	}
}

// WithHonestyWeight sets the weight for consensus agreement in scoring
func WithHonestyWeight(weight float64) Option {
	return func(f *Filter) {
		f.config.HonestyWeight = weight
	}
}

// WithStalenessThreshold sets how long without packets before a feeder is stale
func WithStalenessThreshold(threshold time.Duration) Option {
	return func(f *Filter) {
		f.config.StalenessThreshold = threshold
	}
}

// WithLatenessThreshold sets the relative delay threshold for lateness penalty
func WithLatenessThreshold(threshold time.Duration) Option {
	return func(f *Filter) {
		f.config.LatenessThreshold = threshold
	}
}

// WithPositionTolerance sets the position tolerance for honesty scoring
func WithPositionTolerance(meters float64) Option {
	return func(f *Filter) {
		f.config.PositionToleranceMeters = meters
	}
}

// WithCoordinationEnabled enables/disables multi-instance coordination
func WithCoordinationEnabled(enabled bool) Option {
	return func(f *Filter) {
		f.config.CoordinationEnabled = enabled
	}
}

// WithClaimTTL sets how long remote claims are valid
func WithClaimTTL(ttl time.Duration) Option {
	return func(f *Filter) {
		f.config.ClaimTTL = ttl
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

	// Extract epoch ID from frame (if the frame implements EpochIDProvider)
	epochID := uint32(0)
	if provider, ok := frame.(EpochIDProvider); ok {
		epochID = provider.EpochID()
	}

	// Use composite key combining feeder tag and epoch
	compositeFeederKey := producerKey(feederTag, epochID)

	// Track epoch changes per feeder for metrics
	if epochID > 0 {
		val, _ := f.lastSeenEpochID.LoadOrStore(feederTag, uint32(0))
		lastSeenEpoch := val.(uint32)
		if epochID > lastSeenEpoch {
			prometheusEpochChanges.WithLabelValues(feederTag).Inc()
			f.lastSeenEpochID.Store(feederTag, epochID)
		}
	}

	// Fast path: check existing aircraft state to skip work for common cases.
	// If the frame is from the sticky feeder, skip the coordinator check entirely
	// (it does 3+ map lookups and multiple lock acquisitions — wasted for sticky frames).
	// If the frame is from a non-sticky feeder and is non-position, drop it early.
	if existing, ok := f.aircraft.Load(icao); ok {
		state := existing.(*aircraftState)
		state.mu.RLock()
		sticky := state.stickyFeeder
		state.mu.RUnlock()

		if sticky != "" {
			if sticky == compositeFeederKey {
				// Frame from the sticky feeder — skip coordinator, go straight to processing
				goto process
			}
			// Non-sticky feeder: drop non-position frames early
			if !isPositionFrame(frame) {
				prometheusNonPositionDropped.Inc()
				prometheusFramesRejected.Inc()
				return nil
			}
		}
	}

	// Coordinator check: only reached for frames from non-sticky feeders (or new aircraft)
	if f.coordinator != nil {
		localScore := f.GetLocalScore(icao, compositeFeederKey)
		if f.coordinator.ShouldDropForRemote(icao, localScore, f.config.HysteresisThreshold) {
			return nil // Remote instance has better feeder
		}
	}

process:
	// Get the payload key for same-tag deduplication
	payloadKey := f.getPayloadKey(frame)

	// Extract RSSI if available (only beast frames have it)
	var rssi float64
	if beastFrame, ok := frame.(*beast.Frame); ok {
		rssi = beastFrame.SignalRssi()
	}

	// Get or create aircraft state
	state := f.getOrCreateAircraftState(icao)

	// Record arrival for latency tracking (before processing)
	if payloadKey != "" {
		f.background.RecordArrival(payloadKey, compositeFeederKey)
	}

	// Process the frame and decide whether to accept or reject
	accepted, switched := state.processFrame(compositeFeederKey, rssi, payloadKey, &f.config, f.sameTagDedupe, &f.loggedDuplicateTags, &f.log, f.background)

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

// isPositionFrame checks whether a frame contains ADS-B position data using raw bytes.
// Non-position frames from non-sticky feeders are dropped early to avoid unnecessary scoring work.
func isPositionFrame(frame tracker.Frame) bool {
	switch ft := frame.(type) {
	case *beast.Frame:
		return isModeSPositionBytes(ft.AvrRaw())
	case *mode_s.Frame:
		return isModeSPositionBytes(ft.Raw())
	case *sbs1.Frame:
		return ft.HasPosition
	default:
		return true // unknown frame type, conservatively let through
	}
}

// isModeSPositionBytes checks raw Mode-S bytes for DF17/18 with position type codes.
// TC 5-8: surface position, TC 9-18: airborne position (baro), TC 20-22: airborne position (GNSS).
// TC 19 (velocity) is excluded — it has no lat/lon.
func isModeSPositionBytes(raw []byte) bool {
	if len(raw) < 5 {
		return false // short frames (DF0/4/5/11) don't contain position
	}
	df := raw[0] >> 3
	if df != 17 && df != 18 {
		return false
	}
	tc := raw[4] >> 3
	return (tc >= 5 && tc <= 18) || (tc >= 20 && tc <= 22)
}

// getOrCreateAircraftState retrieves or creates the state for an aircraft.
// Uses LoadOrStore to atomically avoid the check-then-act race where two
// goroutines could both create state, both Store, and both increment the gauge.
func (f *Filter) getOrCreateAircraftState(icao uint32) *aircraftState {
	state := &aircraftState{
		feeders:  make(map[string]*feederStats),
		lastSeen: time.Now(),
	}

	actual, loaded := f.aircraft.LoadOrStore(icao, state)
	if !loaded {
		// We stored a new entry — increment the gauge exactly once
		prometheusActiveAircraft.Inc()
	}
	return actual.(*aircraftState)
}

// processFrame handles the frame processing logic for a single aircraft.
// Returns (accepted, switched) - whether the frame was accepted and whether we switched feeders.
func (s *aircraftState) processFrame(
	feederTag string,
	rssi float64,
	payloadKey string,
	cfg *Config,
	sameTagDedupe *forgetfulmap.ForgetfulSyncMap,
	sameTagLoggedTags *sync.Map,
	logger *zerolog.Logger,
	background *BackgroundWorker,
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
	// check if we've seen this exact payload recently from the same tag.
	// Use the base feeder tag (without epoch) for dedup so we detect duplicates
	// across different sub-producers behind the same ingress feeder.
	if payloadKey != "" && (s.stickyFeeder == "" || s.stickyFeeder == feederTag) {
		baseTag := extractFeederTag(feederTag)
		dedupeKey := baseTag + ":" + payloadKey
		if sameTagDedupe.HasKeyStr(dedupeKey) {
			// Duplicate from same tag - log first occurrence per base feeder
			if _, alreadyLogged := sameTagLoggedTags.LoadOrStore(baseTag, true); !alreadyLogged {
				logger.Info().
					Str("feeder", baseTag).
					Msg("Detected same-tag duplicate frames (multiple receivers with same API key)")
			}
			prometheusSameTagDuplicates.WithLabelValues(baseTag).Inc()
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

	// Calculate scores with background-computed lateness and honesty
	stickyLatenessScore := background.GetLatenessScore(s.stickyFeeder)
	stickyHonestyScore := background.GetHonestyScore(s.stickyFeeder)
	stickyScore := calculateScore(stickyStats, cfg, stickyLatenessScore, stickyHonestyScore)

	// Apply staleness penalty to sticky feeder - if they've gone quiet, reduce their score
	stalenessFactor := calculateStaleness(stickyStats.lastPacket, cfg.StalenessThreshold)
	stickyScore *= stalenessFactor

	challengerLatenessScore := background.GetLatenessScore(feederTag)
	challengerHonestyScore := background.GetHonestyScore(feederTag)
	challengerScore := calculateScore(stats, cfg, challengerLatenessScore, challengerHonestyScore)

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

// decayPacketCounts applies exponential decay to all feeder packet counts.
// Called once per background tick (BackgroundInterval). This prevents packetCount
// from growing monotonically and saturating the scoring normalization.
func (f *Filter) decayPacketCounts() {
	factor := f.decayFactor
	f.aircraft.Range(func(key, value any) bool {
		state := value.(*aircraftState)
		state.mu.Lock()
		for _, stats := range state.feeders {
			// Truncation (not Round) ensures counts converge to zero when a feeder goes silent.
			// Round can get stuck at small values (e.g., Round(4 * 0.891) = 4).
			stats.packetCount = uint64(float64(stats.packetCount) * factor)
		}
		state.mu.Unlock()
		return true
	})
}

// Stop stops the filter and releases resources
func (f *Filter) Stop() {
	if f.coordinator != nil {
		f.coordinator.Stop()
	}
	f.background.Stop()
	f.aircraft.Stop()
	f.sameTagDedupe.Stop()
}

// SetupCoordinator initializes the multi-instance coordinator with a NATS server.
// This must be called after NewFilter if coordination is desired.
func (f *Filter) SetupCoordinator(ns *nats_io.Server) error {
	if !f.config.CoordinationEnabled {
		return nil
	}
	if ns == nil {
		f.log.Warn().Msg("Coordination enabled but no NATS server provided, disabling")
		return nil
	}

	var err error
	f.coordinator, err = NewCoordinator(ns, f, &f.config, f.log)
	if err != nil {
		return err
	}
	if err := f.coordinator.Start(); err != nil {
		return err
	}

	// Set up periodic claim publishing callback
	f.background.SetPeriodicCallback(func() {
		scores := f.GetAllAircraftScores()
		f.coordinator.PublishPeriodicClaims(scores)
	})

	f.log.Info().
		Str("instance_id", f.coordinator.InstanceID()).
		Msg("Multi-instance coordination enabled")

	return nil
}

// GetLocalScore returns the current score for an aircraft from our local state
func (f *Filter) GetLocalScore(icao uint32, feederTag string) float64 {
	stateVal, ok := f.aircraft.Load(icao)
	if !ok {
		return 0
	}

	state := stateVal.(*aircraftState)
	state.mu.RLock()
	defer state.mu.RUnlock()

	stats, exists := state.feeders[feederTag]
	if !exists {
		return 0
	}

	latenessScore := f.background.GetLatenessScore(feederTag)
	honestyScore := f.background.GetHonestyScore(feederTag)
	return calculateScore(stats, &f.config, latenessScore, honestyScore)
}

// GetAllAircraftScores returns the current scores for all tracked aircraft
func (f *Filter) GetAllAircraftScores() map[uint32]AircraftScore {
	scores := make(map[uint32]AircraftScore)

	f.aircraft.Range(func(key, value any) bool {
		icao := key.(uint32)
		state := value.(*aircraftState)

		state.mu.RLock()
		if state.stickyFeeder != "" {
			if stats, exists := state.feeders[state.stickyFeeder]; exists {
				latenessScore := f.background.GetLatenessScore(state.stickyFeeder)
				honestyScore := f.background.GetHonestyScore(state.stickyFeeder)
				score := calculateScore(stats, &f.config, latenessScore, honestyScore)
				scores[icao] = AircraftScore{
					FeederTag: extractFeederTag(state.stickyFeeder),
					Score:     score,
				}
			}
		}
		state.mu.RUnlock()
		return true
	})

	return scores
}

// countActiveEpochsPerFeeder returns the number of unique epochs per feeder
func (f *Filter) countActiveEpochsPerFeeder() map[string]int {
	result := make(map[string]int)

	f.aircraft.Range(func(key, value any) bool {
		state := value.(*aircraftState)
		state.mu.RLock()
		defer state.mu.RUnlock()

		// Count unique epochs per feeder
		feederEpochs := make(map[string]map[uint32]bool)
		for compositeKey := range state.feeders {
			feederTag := extractFeederTag(compositeKey)

			// Extract epoch ID from composite key
			idx := strings.Index(compositeKey, "#")
			if idx >= 0 {
				var epochID uint32
				fmt.Sscanf(compositeKey[idx+1:], "%d", &epochID)
				if feederEpochs[feederTag] == nil {
					feederEpochs[feederTag] = make(map[uint32]bool)
				}
				feederEpochs[feederTag][epochID] = true
			} else {
				// Legacy key without epoch suffix
				if feederEpochs[feederTag] == nil {
					feederEpochs[feederTag] = make(map[uint32]bool)
				}
				feederEpochs[feederTag][0] = true
			}
		}

		for feeder, epochs := range feederEpochs {
			result[feeder] += len(epochs)
		}

		return true
	})

	return result
}

// SetPostDecodeCallback registers this filter to receive position updates from the tracker.
// This enables honesty scoring by feeding decoded position data to the background worker.
func (f *Filter) SetPostDecodeCallback(trk *tracker.Tracker) {
	trk.SetPostDecodeCallback(func(icao uint32, source *tracker.FrameSource, lat, lon float64, alt int32) {
		f.background.RecordPosition(icao, source.Tag, lat, lon, alt)
	})
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
	activeEpochs := f.countActiveEpochsPerFeeder()

	f.log.Info().
		Int32("NumAircraft", f.aircraft.Len()).
		Int32("DedupeEntries", f.sameTagDedupe.Len()).
		Interface("ActiveEpochs", activeEpochs).
		Msg("Health Check")

	// Update gauge metrics
	for feeder, count := range activeEpochs {
		prometheusActiveEpochs.WithLabelValues(feeder).Set(float64(count))
	}

	return true
}
