package stickyfeeder

import (
	"container/ring"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
	"plane.watch/lib/dedupe/forgetfulmap"
)

var (
	prometheusBackgroundRunDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "pw_ingest",
		Subsystem: "sticky_feeder",
		Name:      "background_run_duration_seconds",
		Help:      "Duration of background scoring computation",
		Buckets:   []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5},
	})

	prometheusFeederLatenessScore = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "pw_ingest",
		Subsystem: "sticky_feeder",
		Name:      "feeder_lateness_score",
		Help:      "Current lateness score per feeder (1.0 = fastest, 0.0 = slowest)",
	}, []string{"feeder"})

	prometheusFeederHonestyScore = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "pw_ingest",
		Subsystem: "sticky_feeder",
		Name:      "feeder_honesty_score",
		Help:      "Current honesty/consensus score per feeder (1.0 = agrees with consensus)",
	}, []string{"feeder"})
)

type (
	// BackgroundWorker computes latency and honesty scores off the hot path
	BackgroundWorker struct {
		config *Config
		log    zerolog.Logger

		// Frame arrival tracking for latency scoring
		arrivals *forgetfulmap.ForgetfulSyncMap

		// Per-feeder latency statistics
		feederLatency   map[string]*latencyStats
		feederLatencyMu sync.RWMutex

		// Per-feeder honesty statistics
		feederHonesty   map[string]*honestyStats
		feederHonestyMu sync.RWMutex

		// Position reports for consensus scoring (ICAO -> feeder -> position)
		positionReports   map[uint32]map[string]*positionReport
		positionReportsMu sync.Mutex

		// Callback for periodic claim publishing (set by Filter if coordination enabled)
		onPeriodicTick func()

		// Callback for decaying packet counts (set by Filter in NewFilter)
		onDecayTick func()

		// Control
		stopCh chan struct{}
		wg     sync.WaitGroup
	}

	// frameArrival tracks when different feeders delivered the same frame
	frameArrival struct {
		mu           sync.Mutex
		firstArrival time.Time
		firstFeeder  string
		arrivals     []feederArrival
	}

	// feederArrival records a single feeder's arrival time for a frame
	feederArrival struct {
		feeder    string
		arrivedAt time.Time
	}

	// latencyStats tracks relative latency for a feeder
	latencyStats struct {
		mu     sync.Mutex
		delays *ring.Ring // ring buffer of time.Duration
		count  int        // number of samples in the ring
		median time.Duration
		p90    time.Duration
		score  float64 // computed score (1.0 = fast, 0.0 = slow)
	}

	// honestyStats tracks consensus agreement for a feeder
	honestyStats struct {
		mu            sync.Mutex
		agreements    uint64
		disagreements uint64
		score         float64 // computed score (1.0 = honest, 0.0 = dishonest)
	}

	// positionReport holds a feeder's reported position for an aircraft
	positionReport struct {
		lat       float64
		lon       float64
		altitude  int32
		timestamp time.Time
	}
)

// NewBackgroundWorker creates a new background scoring worker
func NewBackgroundWorker(config *Config, log zerolog.Logger) *BackgroundWorker {
	w := &BackgroundWorker{
		config:          config,
		log:             log.With().Str("component", "StickyFeederBackground").Logger(),
		feederLatency:   make(map[string]*latencyStats),
		feederHonesty:   make(map[string]*honestyStats),
		positionReports: make(map[uint32]map[string]*positionReport),
		stopCh:          make(chan struct{}),
	}

	// Create arrival tracking map with short TTL
	w.arrivals = forgetfulmap.NewForgetfulSyncMap(
		forgetfulmap.WithOldAgeAfter(500*time.Millisecond), // Only need to track arrivals briefly
		forgetfulmap.WithSweepInterval(500*time.Millisecond),
	)

	return w
}

// Start begins the background worker
func (w *BackgroundWorker) Start() {
	w.wg.Add(1)
	go w.run()
}

// Stop halts the background worker
func (w *BackgroundWorker) Stop() {
	close(w.stopCh)
	w.wg.Wait()
	w.arrivals.Stop()
}

// SetPeriodicCallback sets a callback to be invoked on each background tick
func (w *BackgroundWorker) SetPeriodicCallback(cb func()) {
	w.onPeriodicTick = cb
}

// SetDecayCallback sets a callback to be invoked on each background tick
// before score computation, used for decaying packet counts.
func (w *BackgroundWorker) SetDecayCallback(cb func()) {
	w.onDecayTick = cb
}

// run is the main background loop
func (w *BackgroundWorker) run() {
	defer w.wg.Done()

	ticker := time.NewTicker(w.config.BackgroundInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			// Decay packet counts before scoring so scores reflect decayed values
			if w.onDecayTick != nil {
				w.onDecayTick()
			}
			w.computeScores()
			// Trigger periodic claim publishing if coordination is enabled
			if w.onPeriodicTick != nil {
				w.onPeriodicTick()
			}
		}
	}
}

// computeScores recalculates latency and honesty scores
func (w *BackgroundWorker) computeScores() {
	start := time.Now()

	w.computeLatencyScores()
	w.computeHonestyScores()

	prometheusBackgroundRunDuration.Observe(time.Since(start).Seconds())
}

// computeLatencyScores calculates lateness scores from collected delay samples
func (w *BackgroundWorker) computeLatencyScores() {
	w.feederLatencyMu.Lock()
	defer w.feederLatencyMu.Unlock()

	for feeder, stats := range w.feederLatency {
		stats.mu.Lock()
		if stats.count == 0 {
			stats.score = 1.0 // No data = assume good
			stats.mu.Unlock()
			continue
		}

		// Extract delays from ring buffer
		delays := make([]time.Duration, 0, stats.count)
		stats.delays.Do(func(v any) {
			if v != nil {
				delays = append(delays, v.(time.Duration))
			}
		})

		if len(delays) == 0 {
			stats.score = 1.0
			stats.mu.Unlock()
			continue
		}

		// Sort for percentile calculation
		sort.Slice(delays, func(i, j int) bool {
			return delays[i] < delays[j]
		})

		// Calculate median and p90
		stats.median = delays[len(delays)/2]
		p90Idx := int(float64(len(delays)) * 0.9)
		if p90Idx >= len(delays) {
			p90Idx = len(delays) - 1
		}
		stats.p90 = delays[p90Idx]

		// Calculate score based on p90 delay
		// Feeders within LatenessThreshold get score 1.0
		// Score decreases linearly as delay increases beyond threshold
		// At 4x threshold, score = 0.0
		if stats.p90 <= w.config.LatenessThreshold {
			stats.score = 1.0
		} else {
			excess := stats.p90 - w.config.LatenessThreshold
			maxExcess := 3 * w.config.LatenessThreshold // Score hits 0 at 4x threshold
			stats.score = 1.0 - math.Min(1.0, float64(excess)/float64(maxExcess))
		}

		stats.mu.Unlock()

		prometheusFeederLatenessScore.WithLabelValues(feeder).Set(stats.score)
	}
}

// computeHonestyScores calculates consensus agreement scores
func (w *BackgroundWorker) computeHonestyScores() {
	w.positionReportsMu.Lock()
	defer w.positionReportsMu.Unlock()

	// For each aircraft, compare positions from different feeders
	now := time.Now()
	staleThreshold := 5 * time.Second

	for icao, feeders := range w.positionReports {
		// Collect fresh positions
		freshPositions := make(map[string]*positionReport)
		for feeder, report := range feeders {
			if now.Sub(report.timestamp) < staleThreshold {
				freshPositions[feeder] = report
			}
		}

		// Need at least 2 feeders to compare
		if len(freshPositions) < 2 {
			continue
		}

		// Calculate consensus position using median (more robust against outliers)
		lats := make([]float64, 0, len(freshPositions))
		lons := make([]float64, 0, len(freshPositions))
		for _, report := range freshPositions {
			lats = append(lats, report.lat)
			lons = append(lons, report.lon)
		}
		sort.Float64s(lats)
		sort.Float64s(lons)

		// Use median for consensus
		mid := len(lats) / 2
		var consensusLat, consensusLon float64
		if len(lats)%2 == 0 {
			consensusLat = (lats[mid-1] + lats[mid]) / 2
			consensusLon = (lons[mid-1] + lons[mid]) / 2
		} else {
			consensusLat = lats[mid]
			consensusLon = lons[mid]
		}

		// Score each feeder based on distance from consensus
		for feeder, report := range freshPositions {
			dist := haversineDistance(report.lat, report.lon, consensusLat, consensusLon)

			w.feederHonestyMu.Lock()
			stats, exists := w.feederHonesty[feeder]
			if !exists {
				stats = &honestyStats{score: 1.0}
				w.feederHonesty[feeder] = stats
			}
			w.feederHonestyMu.Unlock()

			stats.mu.Lock()
			if dist <= w.config.PositionToleranceMeters {
				stats.agreements++
			} else {
				stats.disagreements++
			}

			// Calculate score as agreement ratio
			total := stats.agreements + stats.disagreements
			if total > 0 {
				stats.score = float64(stats.agreements) / float64(total)
			}
			stats.mu.Unlock()

			prometheusFeederHonestyScore.WithLabelValues(feeder).Set(stats.score)
		}

		// Clean up stale position reports
		for feeder, report := range feeders {
			if now.Sub(report.timestamp) > staleThreshold {
				delete(feeders, feeder)
			}
		}
		if len(feeders) == 0 {
			delete(w.positionReports, icao)
		}
	}
}

// RecordArrival records when a frame arrived from a feeder (called from hot path).
//
// feederTag may be either a bare feeder tag or a composite "tag#epoch" key —
// it is normalized to the bare tag here so the per-frame frameArrival entries
// agree with the per-feeder latency stats produced by recordDelay (which
// performs the canonical normalization).
func (w *BackgroundWorker) RecordArrival(payloadKey string, feederTag string) {
	bareTag := extractFeederTag(feederTag)
	now := time.Now()

	// Try to load existing arrival record
	if existing, ok := w.arrivals.Load(payloadKey); ok && existing != nil {
		arrival := existing.(*frameArrival)
		arrival.mu.Lock()
		arrival.arrivals = append(arrival.arrivals, feederArrival{
			feeder:    bareTag,
			arrivedAt: now,
		})
		delay := now.Sub(arrival.firstArrival)
		arrival.mu.Unlock()

		// Record the delay for this feeder
		w.recordDelay(bareTag, delay)
		return
	}

	// First arrival for this frame
	arrival := &frameArrival{
		firstArrival: now,
		firstFeeder:  bareTag,
		arrivals: []feederArrival{{
			feeder:    bareTag,
			arrivedAt: now,
		}},
	}
	w.arrivals.Store(payloadKey, arrival)

	// First arrival has zero delay
	w.recordDelay(bareTag, 0)
}

// recordDelay adds a delay sample to a feeder's latency stats.
// Uses RLock for the common case where the feeder already exists,
// upgrading to Lock only when creating a new entry.
//
// This is the canonical normalization point for latency writes: feeder may
// be either bare or composite, and is reduced to the bare feeder tag before
// touching feederLatency. Keeping this map bare-keyed bounds the cardinality
// of prometheusFeederLatenessScore (one label combination per physical feeder)
// and matches the read-side normalization in GetLatenessScore.
func (w *BackgroundWorker) recordDelay(feeder string, delay time.Duration) {
	feeder = extractFeederTag(feeder)
	w.feederLatencyMu.RLock()
	stats, exists := w.feederLatency[feeder]
	w.feederLatencyMu.RUnlock()

	if !exists {
		w.feederLatencyMu.Lock()
		// Double-check after acquiring write lock
		stats, exists = w.feederLatency[feeder]
		if !exists {
			stats = &latencyStats{
				delays: ring.New(w.config.LatencyWindowSize),
				score:  1.0,
			}
			w.feederLatency[feeder] = stats
		}
		w.feederLatencyMu.Unlock()
	}

	stats.mu.Lock()
	stats.delays.Value = delay
	stats.delays = stats.delays.Next()
	if stats.count < w.config.LatencyWindowSize {
		stats.count++
	}
	stats.mu.Unlock()
}

// RecordPosition records a position report for consensus scoring (called from hot path).
//
// This is the canonical normalization point for honesty writes: feederTag may
// be either bare or composite, and is reduced to the bare feeder tag before
// touching positionReports. Honesty scores are aggregated per physical feeder,
// not per MLAT epoch, and the read-side (GetHonestyScore) normalizes to match.
func (w *BackgroundWorker) RecordPosition(icao uint32, feederTag string, lat, lon float64, altitude int32) {
	bareTag := extractFeederTag(feederTag)
	w.positionReportsMu.Lock()
	defer w.positionReportsMu.Unlock()

	if _, exists := w.positionReports[icao]; !exists {
		w.positionReports[icao] = make(map[string]*positionReport)
	}

	w.positionReports[icao][bareTag] = &positionReport{
		lat:       lat,
		lon:       lon,
		altitude:  altitude,
		timestamp: time.Now(),
	}
}

// GetLatenessScore returns the current lateness score for a feeder (called from hot path).
//
// feeder may be either a bare feeder tag or a composite "tag#epoch" key —
// it is normalized to the bare tag before lookup so callers in the hot path
// (which typically hold composite keys) match the bare-keyed feederLatency map.
func (w *BackgroundWorker) GetLatenessScore(feeder string) float64 {
	feeder = extractFeederTag(feeder)
	w.feederLatencyMu.RLock()
	stats, exists := w.feederLatency[feeder]
	w.feederLatencyMu.RUnlock()

	if !exists {
		return 1.0 // No data = assume good
	}

	stats.mu.Lock()
	score := stats.score
	stats.mu.Unlock()

	return score
}

// GetHonestyScore returns the current honesty score for a feeder (called from hot path).
//
// feeder may be either a bare feeder tag or a composite "tag#epoch" key —
// it is normalized to the bare tag before lookup. Prior to this normalization,
// callers in the hot path passed composite keys while feederHonesty was
// bare-keyed (RecordPosition is called with source.Tag), so every lookup
// missed and silently returned the default 1.0 — meaning honesty scoring had
// no effect on feeder selection. This normalization restores intended behavior.
func (w *BackgroundWorker) GetHonestyScore(feeder string) float64 {
	feeder = extractFeederTag(feeder)
	w.feederHonestyMu.RLock()
	stats, exists := w.feederHonesty[feeder]
	w.feederHonestyMu.RUnlock()

	if !exists {
		return 1.0 // No data = assume honest
	}

	stats.mu.Lock()
	score := stats.score
	stats.mu.Unlock()

	return score
}

// haversineDistance calculates the distance between two lat/lon points in meters
func haversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusMeters = 6371000.0

	lat1Rad := lat1 * math.Pi / 180
	lat2Rad := lat2 * math.Pi / 180
	deltaLat := (lat2 - lat1) * math.Pi / 180
	deltaLon := (lon2 - lon1) * math.Pi / 180

	a := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*math.Sin(deltaLon/2)*math.Sin(deltaLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadiusMeters * c
}
