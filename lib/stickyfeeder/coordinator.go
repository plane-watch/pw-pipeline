package stickyfeeder

import (
	"sync"
	"sync/atomic"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
	"plane.watch/lib/dedupe/forgetfulmap"
	"plane.watch/lib/nats_io"
	"plane.watch/lib/randstr"
)

const (
	// NatsApiStickyClaimV1 is the base subject for sticky feeder claims
	NatsApiStickyClaimV1 = "v1.sticky.claim"
)

var (
	prometheusClaimsPublished = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "pw_ingest",
		Subsystem: "sticky_coordination",
		Name:      "claims_published_total",
		Help:      "Total claims published by trigger type",
	}, []string{"trigger"})

	prometheusClaimsReceived = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "pw_ingest",
		Subsystem: "sticky_coordination",
		Name:      "claims_received_total",
		Help:      "Total claims received from other instances",
	})

	prometheusClaimsIgnored = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "pw_ingest",
		Subsystem: "sticky_coordination",
		Name:      "claims_ignored_total",
		Help:      "Claims ignored (own instance, stale sequence)",
	}, []string{"reason"})

	prometheusRemoteDrops = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "pw_ingest",
		Subsystem: "sticky_coordination",
		Name:      "frames_dropped_remote_total",
		Help:      "Frames dropped due to remote instance having better claim",
	})

	prometheusRemoteClaimsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "pw_ingest",
		Subsystem: "sticky_coordination",
		Name:      "remote_claims_active",
		Help:      "Number of active remote claims being tracked",
	})

	prometheusRemoteClaimsExpired = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "pw_ingest",
		Subsystem: "sticky_coordination",
		Name:      "remote_claims_expired_total",
		Help:      "Remote claims that expired (instance went silent)",
	})

	prometheusClaimBatchSize = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "pw_ingest",
		Subsystem: "sticky_coordination",
		Name:      "claim_batch_size",
		Help:      "Size of claim batches published",
		Buckets:   []float64{1, 10, 50, 100, 500, 1000, 5000},
	})
)

type (
	// Coordinator handles multi-instance coordination for sticky feeder selection
	Coordinator struct {
		instanceID string
		config     *Config
		natsServer *nats_io.Server
		filter     *Filter
		log        zerolog.Logger

		// Remote claims tracking
		remoteClaims *forgetfulmap.ForgetfulSyncMap

		// Outbound claim queue (non-blocking)
		claimQueue chan AircraftClaim

		// NATS subscription channel
		claimSub chan *nats.Msg

		// Sequence counter for our claims
		sequence atomic.Uint64

		// Control
		stopCh chan struct{}
		wg     sync.WaitGroup
	}

	// AircraftClaim represents a claim broadcast from one instance
	AircraftClaim struct {
		InstanceID string  `json:"instance_id"`
		ICAO       uint32  `json:"icao"`
		FeederTag  string  `json:"feeder_tag"`
		Score      float64 `json:"score"`
		Timestamp  int64   `json:"timestamp"`
		Sequence   uint64  `json:"sequence"`
	}

	// ClaimBatch allows efficient batching of multiple claims in one message
	ClaimBatch struct {
		InstanceID string          `json:"instance_id"`
		Claims     []AircraftClaim `json:"claims"`
		Timestamp  int64           `json:"timestamp"`
	}

	// RemoteClaim represents another instance's claim on an aircraft
	RemoteClaim struct {
		InstanceID string
		FeederTag  string
		Score      float64
		Sequence   uint64
		Timestamp  int64
	}
)

// NewCoordinator creates a new coordinator for multi-instance coordination
func NewCoordinator(ns *nats_io.Server, filter *Filter, config *Config, log zerolog.Logger) *Coordinator {
	instanceID := randstr.RandString(16)
	c := &Coordinator{
		instanceID: instanceID,
		config:     config,
		natsServer: ns,
		filter:     filter,
		log:        log.With().Str("component", "Coordinator").Str("instance", instanceID).Logger(),
		claimQueue: make(chan AircraftClaim, config.ClaimQueueSize),
		stopCh:     make(chan struct{}),
	}

	// Create remote claims map with TTL-based expiry
	c.remoteClaims = forgetfulmap.NewForgetfulSyncMap(
		forgetfulmap.WithOldAgeAfter(config.ClaimTTL),
		forgetfulmap.WithSweepInterval(config.ClaimTTL/3),
		forgetfulmap.WithPreEvictionAction(func(key, value any) {
			prometheusRemoteClaimsActive.Dec()
			prometheusRemoteClaimsExpired.Inc()
		}),
	)

	return c
}

// Start begins the coordinator's background workers
func (c *Coordinator) Start() error {
	// Subscribe to claims from all instances
	var err error
	c.claimSub, err = c.natsServer.Subscribe(NatsApiStickyClaimV1 + ".*")
	if err != nil {
		return err
	}

	c.log.Info().Str("instance_id", c.instanceID).Msg("Starting coordinator")

	// Start workers
	c.wg.Add(2)
	go c.claimPublisher()
	go c.claimConsumer()

	return nil
}

// Stop halts the coordinator
func (c *Coordinator) Stop() {
	close(c.stopCh)
	close(c.claimQueue)
	c.wg.Wait()
	c.remoteClaims.Stop()
}

// InstanceID returns this coordinator's unique instance ID
func (c *Coordinator) InstanceID() string {
	return c.instanceID
}

// claimPublisher handles outbound claim publishing
func (c *Coordinator) claimPublisher() {
	defer c.wg.Done()

	json := jsoniter.ConfigFastest
	subject := NatsApiStickyClaimV1 + "." + c.instanceID

	for claim := range c.claimQueue {
		data, err := json.Marshal(claim)
		if err != nil {
			c.log.Error().Err(err).Msg("Failed to marshal claim")
			continue
		}

		if err := c.natsServer.Publish(subject, data); err != nil {
			c.log.Error().Err(err).Msg("Failed to publish claim")
		}
	}
}

// claimConsumer handles incoming claims from other instances
func (c *Coordinator) claimConsumer() {
	defer c.wg.Done()

	json := jsoniter.ConfigFastest

	for {
		select {
		case <-c.stopCh:
			return
		case msg, ok := <-c.claimSub:
			if !ok {
				return
			}

			prometheusClaimsReceived.Inc()

			// Try to parse as single claim first
			var claim AircraftClaim
			if err := json.Unmarshal(msg.Data, &claim); err != nil {
				// Try as batch
				var batch ClaimBatch
				if err := json.Unmarshal(msg.Data, &batch); err != nil {
					c.log.Error().Err(err).Msg("Failed to unmarshal claim")
					continue
				}
				for _, bc := range batch.Claims {
					bc.InstanceID = batch.InstanceID
					c.handleIncomingClaim(bc)
				}
				continue
			}

			c.handleIncomingClaim(claim)
		}
	}
}

// handleIncomingClaim processes a single incoming claim
func (c *Coordinator) handleIncomingClaim(claim AircraftClaim) {
	// Ignore our own claims
	if claim.InstanceID == c.instanceID {
		prometheusClaimsIgnored.WithLabelValues("self").Inc()
		return
	}

	existing, exists := c.remoteClaims.Load(claim.ICAO)
	if !exists {
		// New claim
		c.remoteClaims.Store(claim.ICAO, &RemoteClaim{
			InstanceID: claim.InstanceID,
			FeederTag:  claim.FeederTag,
			Score:      claim.Score,
			Sequence:   claim.Sequence,
			Timestamp:  claim.Timestamp,
		})
		prometheusRemoteClaimsActive.Inc()
		return
	}

	remote := existing.(*RemoteClaim)

	// Update if: newer sequence from same instance, OR different instance with better score
	if (claim.InstanceID == remote.InstanceID && claim.Sequence > remote.Sequence) ||
		(claim.InstanceID != remote.InstanceID && claim.Score > remote.Score) {
		c.remoteClaims.Store(claim.ICAO, &RemoteClaim{
			InstanceID: claim.InstanceID,
			FeederTag:  claim.FeederTag,
			Score:      claim.Score,
			Sequence:   claim.Sequence,
			Timestamp:  claim.Timestamp,
		})
	} else {
		prometheusClaimsIgnored.WithLabelValues("stale").Inc()
	}
}

// QueueClaim queues a claim for publishing (non-blocking)
func (c *Coordinator) QueueClaim(icao uint32, feederTag string, score float64, trigger string) {
	claim := AircraftClaim{
		InstanceID: c.instanceID,
		ICAO:       icao,
		FeederTag:  feederTag,
		Score:      score,
		Timestamp:  time.Now().UnixNano(),
		Sequence:   c.sequence.Add(1),
	}

	select {
	case c.claimQueue <- claim:
		prometheusClaimsPublished.WithLabelValues(trigger).Inc()
	default:
		// Queue full, drop the claim (not critical)
		c.log.Warn().Uint32("icao", icao).Msg("Claim queue full, dropping claim")
	}
}

// PublishPeriodicClaims publishes all current aircraft claims as a batch
func (c *Coordinator) PublishPeriodicClaims(aircraftScores map[uint32]AircraftScore) {
	if len(aircraftScores) == 0 {
		return
	}

	json := jsoniter.ConfigFastest
	subject := NatsApiStickyClaimV1 + "." + c.instanceID

	batch := ClaimBatch{
		InstanceID: c.instanceID,
		Timestamp:  time.Now().UnixNano(),
		Claims:     make([]AircraftClaim, 0, len(aircraftScores)),
	}

	for icao, as := range aircraftScores {
		batch.Claims = append(batch.Claims, AircraftClaim{
			InstanceID: c.instanceID,
			ICAO:       icao,
			FeederTag:  as.FeederTag,
			Score:      as.Score,
			Timestamp:  batch.Timestamp,
			Sequence:   c.sequence.Add(1),
		})
	}

	prometheusClaimBatchSize.Observe(float64(len(batch.Claims)))

	data, err := json.Marshal(batch)
	if err != nil {
		c.log.Error().Err(err).Msg("Failed to marshal claim batch")
		return
	}

	if err := c.natsServer.Publish(subject, data); err != nil {
		c.log.Error().Err(err).Msg("Failed to publish claim batch")
	}

	prometheusClaimsPublished.WithLabelValues("periodic").Add(float64(len(batch.Claims)))
}

// GetBestRemoteClaim returns the best remote claim for an aircraft, if one exists
func (c *Coordinator) GetBestRemoteClaim(icao uint32) (*RemoteClaim, bool) {
	existing, ok := c.remoteClaims.Load(icao)
	if !ok {
		return nil, false
	}
	return existing.(*RemoteClaim), true
}

// ShouldDropForRemote checks if we should drop a frame because a remote instance has a better claim
func (c *Coordinator) ShouldDropForRemote(icao uint32, localScore float64, hysteresis float64) bool {
	remote, exists := c.GetBestRemoteClaim(icao)
	if !exists {
		return false
	}

	// Remote must be significantly better (using same hysteresis as local switching)
	if remote.Score > localScore*(1+hysteresis) {
		prometheusRemoteDrops.Inc()
		return true
	}

	return false
}

// AircraftScore holds the current score info for an aircraft
type AircraftScore struct {
	FeederTag string
	Score     float64
}
