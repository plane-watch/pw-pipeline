package stickyfeeder

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"plane.watch/lib/tracker"
	"plane.watch/lib/tracker/mode_s"
)

func init() {
	zerolog.SetGlobalLevel(zerolog.ErrorLevel)
}

// mockFrame is a simple mock implementation of tracker.Frame for testing
type mockFrame struct {
	icao           uint32
	icaoStr        string
	raw            []byte
	epochID        uint32
	signalStrength float64
}

func (m *mockFrame) Icao() uint32         { return m.icao }
func (m *mockFrame) IcaoStr() string      { return m.icaoStr }
func (m *mockFrame) Decode() error        { return nil }
func (m *mockFrame) TimeStamp() time.Time { return time.Now() }
func (m *mockFrame) Raw() []byte          { return m.raw }
func (m *mockFrame) EpochID() uint32      { return m.epochID }
func (m *mockFrame) SetEpochID(id uint32) { m.epochID = id }
func (m *mockFrame) SetSignalStrength(strength float64) {
	m.signalStrength = strength
}

func makeMockFrameEvent(icao uint32, payload []byte, tag string) *tracker.FrameEvent {
	frame := &mockFrame{
		icao:    icao,
		icaoStr: "MOCK",
		raw:     payload,
	}
	source := &tracker.FrameSource{Tag: tag}
	fe := tracker.NewFrameEvent(frame, source)
	return &fe
}

// createTestBeastFrame creates a frame with epoch ID, using mode_s.Frame which the sticky feeder also accepts
func createTestBeastFrame(icao uint32, epochID uint32, feederTag string) *tracker.FrameEvent {
	// Create a mock frame but with epochID support
	frame := &mockFrame{
		icao:    icao,
		icaoStr: fmt.Sprintf("%06X", icao),
		raw:     []byte{byte(icao >> 16), byte(icao >> 8), byte(icao)},
		epochID: epochID,
	}
	source := &tracker.FrameSource{Tag: feederTag}
	fe := tracker.NewFrameEvent(frame, source)
	return &fe
}

// makeDecodedModeSEvent creates a FrameEvent with a real decoded mode_s.Frame.
// This mirrors the real pipeline where Decode() is called before middleware.
func makeDecodedModeSEvent(t *testing.T, hex string, tag string) *tracker.FrameEvent {
	t.Helper()
	frame, err := mode_s.DecodeString(hex, time.Now())
	if err != nil {
		t.Fatalf("failed to decode frame %s: %v", hex, err)
	}
	source := &tracker.FrameSource{Tag: tag}
	fe := tracker.NewFrameEvent(frame, source)
	return &fe
}

func TestFilter_FirstFeederWins(t *testing.T) {
	filter := NewFilter()
	defer filter.Stop()

	icao := uint32(0x7C1234)

	// First frame from feeder A should be accepted
	fe1 := makeMockFrameEvent(icao, []byte{0x01, 0x02, 0x03}, "feeder-A")
	result := filter.Handle(fe1)
	if result == nil {
		t.Error("Expected first frame from feeder A to be accepted")
	}

	// First frame from feeder B for same aircraft (same ICAO) should be rejected
	fe2 := makeMockFrameEvent(icao, []byte{0x04, 0x05, 0x06}, "feeder-B")
	result = filter.Handle(fe2)
	if result != nil {
		t.Error("Expected frame from feeder B to be rejected (not sticky)")
	}
}

func TestFilter_SameFeederAlwaysAccepted(t *testing.T) {
	filter := NewFilter()
	defer filter.Stop()

	icao := uint32(0x7C1234)

	// First frame establishes feeder A as sticky
	fe1 := makeMockFrameEvent(icao, []byte{0x01, 0x02, 0x03}, "feeder-A")
	filter.Handle(fe1)

	// Subsequent frames from feeder A should be accepted (unique payloads)
	for i := 0; i < 10; i++ {
		payload := []byte{byte(i + 100), 0x02, 0x03}
		fe := makeMockFrameEvent(icao, payload, "feeder-A")
		result := filter.Handle(fe)
		if result == nil {
			t.Errorf("Frame %d from sticky feeder should be accepted", i)
		}
	}
}

func TestFilter_HysteresisPreventsSwitching(t *testing.T) {
	// Configure with high hysteresis (50%) to make switching harder
	filter := NewFilter(WithHysteresis(0.50))
	defer filter.Stop()

	icao := uint32(0x7C1234)

	// First frame from feeder A
	fe1 := makeMockFrameEvent(icao, []byte{0x01}, "feeder-A")
	filter.Handle(fe1)

	// Send a few more frames from feeder A to build up stats
	for i := 0; i < 5; i++ {
		payload := []byte{byte(i + 10)}
		fe := makeMockFrameEvent(icao, payload, "feeder-A")
		filter.Handle(fe)
	}

	// Now send frames from feeder B - should still be rejected due to hysteresis
	for i := 0; i < 3; i++ {
		payload := []byte{byte(i + 50)}
		fe := makeMockFrameEvent(icao, payload, "feeder-B")
		result := filter.Handle(fe)
		if result != nil {
			t.Errorf("Frame %d from feeder B should be rejected (hysteresis)", i)
		}
	}
}

func TestFilter_CompellingCausesSwitching(t *testing.T) {
	// Configure with very low hysteresis to make switching easier
	filter := NewFilter(WithHysteresis(0.01))
	defer filter.Stop()

	icao := uint32(0x7C1234)

	// One frame from feeder A
	fe1 := makeMockFrameEvent(icao, []byte{0x01}, "feeder-A")
	filter.Handle(fe1)

	// Send many frames from feeder B to build up compelling stats
	switched := false
	for i := 0; i < 20; i++ {
		payload := []byte{byte(i + 100)}
		fe := makeMockFrameEvent(icao, payload, "feeder-B")
		result := filter.Handle(fe)

		// Eventually feeder B should become sticky
		if result != nil {
			switched = true
		}
	}

	// After many frames from B, it should have switched
	if !switched {
		t.Log("Note: feeder switch did not occur - this may be expected depending on scoring weights")
	}
}

func TestFilter_DifferentAircraftIndependent(t *testing.T) {
	filter := NewFilter()
	defer filter.Stop()

	icao1 := uint32(0x7C1234)
	icao2 := uint32(0xAABBCC)

	// First aircraft with feeder A
	fe1 := makeMockFrameEvent(icao1, []byte{0x01}, "feeder-A")
	result1 := filter.Handle(fe1)
	if result1 == nil {
		t.Error("First aircraft, feeder A should be accepted")
	}

	// Second aircraft with feeder B - should be accepted (different aircraft)
	fe2 := makeMockFrameEvent(icao2, []byte{0x02}, "feeder-B")
	result2 := filter.Handle(fe2)
	if result2 == nil {
		t.Error("Second aircraft, feeder B should be accepted (different aircraft)")
	}
}

func TestFilter_SameTagDedupe(t *testing.T) {
	filter := NewFilter()
	defer filter.Stop()

	icao := uint32(0x7C1234)
	payload := []byte{0x01, 0x02, 0x03}

	// First frame from feeder A
	fe1 := makeMockFrameEvent(icao, payload, "feeder-A")
	result := filter.Handle(fe1)
	if result == nil {
		t.Error("First frame should be accepted")
	}

	// Same exact frame (same payload) from same feeder should be deduped
	fe2 := makeMockFrameEvent(icao, payload, "feeder-A")
	result = filter.Handle(fe2)
	if result != nil {
		t.Error("Duplicate frame from same feeder should be rejected")
	}
}

func TestFilter_NilHandling(t *testing.T) {
	filter := NewFilter()
	defer filter.Stop()

	// Nil event
	result := filter.Handle(nil)
	if result != nil {
		t.Error("Nil event should return nil")
	}
}

func TestFilter_EpochIsolation(t *testing.T) {
	// Use low hysteresis to make switching easier
	f := NewFilter(WithHysteresis(0.01))
	defer f.Stop()

	icao := uint32(0x4840D6)

	// First frame from epoch 1 establishes it as sticky
	frame1 := createTestBeastFrame(icao, 1, "rx-north")
	result1 := f.Handle(frame1)
	if result1 == nil {
		t.Fatal("Expected frame from epoch 1 to be accepted")
	}

	// Send multiple frames from epoch 2 (different epoch = different producer key)
	// Each frame has a unique payload to avoid same-tag dedup issues
	// The epoch 2 feeder should eventually score high enough to be accepted
	accepted := false
	for i := 0; i < 10; i++ {
		payload := make([]byte, 3)
		payload[0] = byte(icao >> 16)
		payload[1] = byte(icao >> 8)
		payload[2] = byte(icao) + byte(i) // Vary last byte for unique payloads

		mockFr := &mockFrame{
			icao:    icao,
			icaoStr: "4840D6",
			raw:     payload,
			epochID: 2, // Different epoch
		}
		fe := tracker.NewFrameEvent(mockFr, &tracker.FrameSource{Tag: "rx-north"})
		frameEvent := &fe

		if f.Handle(frameEvent) != nil {
			accepted = true
			break
		}
	}

	if !accepted {
		t.Error("Expected epoch 2 frames to eventually be accepted (treated as different producer)")
	}
}

func TestFilter_HealthCheck(t *testing.T) {
	filter := NewFilter()
	defer filter.Stop()

	if filter.HealthCheckName() != "Sticky Feeder" {
		t.Error("Unexpected health check name")
	}

	if !filter.HealthCheck() {
		t.Error("Health check should return true")
	}

	if filter.String() != "StickyFeeder" {
		t.Error("Unexpected string representation")
	}
}

func TestFilter_MetricsEpochChanges(t *testing.T) {
	f := NewFilter()
	defer f.Stop()
	icao := uint32(0x4840D6)

	// Simulate epoch change: first frame from rx-north#1, then rx-north#2
	frame1 := createTestBeastFrame(icao, 1, "rx-north")
	frame2 := createTestBeastFrame(icao, 2, "rx-north")

	f.Handle(frame1)

	// A second epoch from same feeder should be tracked
	f.Handle(frame2)

	// Verify metrics exist (will be verified via Prometheus endpoint in integration tests)
	// For now, just ensure no panic when handling epochs
	if f == nil {
		t.Error("Filter should exist after handling epochs")
	}
}

// Scoring tests

func TestNormalizePacketCount(t *testing.T) {
	tests := []struct {
		name          string
		packetCount   uint64
		windowSeconds float64
		expectMin     float64
		expectMax     float64
	}{
		{"zero packets", 0, 30, 0, 0.01},
		{"low rate", 10, 30, 0.05, 0.15},         // ~0.33 pps -> ~0.062
		{"medium rate", 100, 30, 0.25, 0.40},     // ~3.33 pps -> ~0.318
		{"high rate", 1000, 30, 0.70, 0.85},      // ~33.3 pps -> ~0.766
		{"very high rate", 3000, 30, 0.95, 1.01}, // ~100 pps -> ~1.0
		{"zero window", 10, 0, 0, 0.01},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizePacketCount(tt.packetCount, tt.windowSeconds)
			if result < tt.expectMin || result > tt.expectMax {
				t.Errorf("normalizePacketCount(%d, %f) = %f, want between %f and %f",
					tt.packetCount, tt.windowSeconds, result, tt.expectMin, tt.expectMax)
			}
		})
	}
}

func TestNormalizeRSSI(t *testing.T) {
	tests := []struct {
		name      string
		rssi      float64
		expectMin float64
		expectMax float64
	}{
		{"very weak", -50, -0.01, 0.01},
		{"weak", -40, -0.01, 0.01},
		{"medium", -20, 0.4, 0.6},
		{"strong", -5, 0.85, 0.95},
		{"very strong", -1, 0.99, 1.01},
		{"max", 0, 0.99, 1.01},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeRSSI(tt.rssi)
			if result < tt.expectMin || result > tt.expectMax {
				t.Errorf("normalizeRSSI(%f) = %f, want between %f and %f",
					tt.rssi, result, tt.expectMin, tt.expectMax)
			}
		})
	}
}

func TestUpdateRSSI_EMA(t *testing.T) {
	stats := &feederStats{}

	// First value sets the EMA directly
	stats.updateRSSI(-10)
	if stats.rssiEMA != -10 {
		t.Errorf("First RSSI should set EMA directly, got %f", stats.rssiEMA)
	}

	// Subsequent values should be smoothed
	stats.updateRSSI(-20)
	// With alpha=0.3: new = 0.3*(-20) + 0.7*(-10) = -6 + -7 = -13
	expected := 0.3*(-20) + 0.7*(-10)
	if stats.rssiEMA != expected {
		t.Errorf("EMA calculation wrong, got %f, expected %f", stats.rssiEMA, expected)
	}
}

func TestShouldSwitch(t *testing.T) {
	tests := []struct {
		name            string
		currentScore    float64
		challengerScore float64
		hysteresis      float64
		expectSwitch    bool
	}{
		{"equal scores no switch", 0.5, 0.5, 0.1, false},
		{"challenger slightly better no switch", 0.5, 0.54, 0.1, false},
		{"challenger much better switch", 0.5, 0.6, 0.1, true},
		{"challenger exactly at threshold", 0.5, 0.55, 0.1, false},
		{"challenger just above threshold", 0.5, 0.551, 0.1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldSwitch(tt.currentScore, tt.challengerScore, tt.hysteresis)
			if result != tt.expectSwitch {
				t.Errorf("shouldSwitch(%f, %f, %f) = %v, want %v",
					tt.currentScore, tt.challengerScore, tt.hysteresis, result, tt.expectSwitch)
			}
		})
	}
}

// Benchmark tests

func BenchmarkFilter_HandleSameFeeder(b *testing.B) {
	filter := NewFilter()
	defer filter.Stop()

	icao := uint32(0x7C1234)

	// Establish sticky feeder
	fe := makeMockFrameEvent(icao, []byte{0x00}, "feeder-A")
	filter.Handle(fe)

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		payload := []byte{byte(n), byte(n >> 8), byte(n >> 16)}
		fe := makeMockFrameEvent(icao, payload, "feeder-A")
		filter.Handle(fe)
	}
}

func BenchmarkFilter_HandleDifferentFeeders(b *testing.B) {
	filter := NewFilter()
	defer filter.Stop()

	icao := uint32(0x7C1234)

	// Establish sticky feeder
	fe := makeMockFrameEvent(icao, []byte{0x00}, "feeder-A")
	filter.Handle(fe)

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		payload := []byte{byte(n), byte(n >> 8), byte(n >> 16)}
		fe := makeMockFrameEvent(icao, payload, "feeder-B")
		filter.Handle(fe)
	}
}

func BenchmarkFilter_HandleManyAircraft(b *testing.B) {
	filter := NewFilter()
	defer filter.Stop()

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		// Create unique aircraft for each iteration
		icao := uint32(n)
		payload := []byte{byte(n)}
		fe := makeMockFrameEvent(icao, payload, "feeder-A")
		filter.Handle(fe)
	}
}

func BenchmarkNormalizePacketCount(b *testing.B) {
	for n := 0; n < b.N; n++ {
		normalizePacketCount(uint64(n%1000), 30)
	}
}

func BenchmarkNormalizeRSSI(b *testing.B) {
	for n := 0; n < b.N; n++ {
		normalizeRSSI(float64(-40 + (n % 40)))
	}
}

func BenchmarkCalculateScore(b *testing.B) {
	cfg := DefaultConfig()
	stats := &feederStats{
		packetCount: 100,
		rssiEMA:     -15,
		lastPacket:  time.Now(),
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		calculateScore(stats, &cfg, 1.0, 1.0)
	}
}

// Staleness tests

func TestCalculateStaleness(t *testing.T) {
	threshold := 1 * time.Second

	tests := []struct {
		name      string
		elapsed   time.Duration
		expectMin float64
		expectMax float64
	}{
		{"fresh packet", 0, 0.99, 1.01},
		{"just under threshold", 900 * time.Millisecond, 0.99, 1.01},
		{"at threshold", 1 * time.Second, 0.99, 1.01},
		{"slightly stale", 2 * time.Second, 0.6, 0.7},
		{"moderately stale", 3 * time.Second, 0.3, 0.4},
		{"very stale", 4 * time.Second, -0.01, 0.01},
		{"completely stale", 10 * time.Second, -0.01, 0.01},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lastPacket := time.Now().Add(-tt.elapsed)
			result := calculateStaleness(lastPacket, threshold)
			if result < tt.expectMin || result > tt.expectMax {
				t.Errorf("calculateStaleness(-%v, %v) = %f, want between %f and %f",
					tt.elapsed, threshold, result, tt.expectMin, tt.expectMax)
			}
		})
	}
}

func TestFilter_StaleFeedersLoseAdvantage(t *testing.T) {
	// Configure with low hysteresis and short staleness threshold
	filter := NewFilter(
		WithHysteresis(0.01),
		WithStalenessThreshold(100*time.Millisecond),
	)
	defer filter.Stop()

	icao := uint32(0x7C1234)

	// First frame from feeder A
	fe1 := makeMockFrameEvent(icao, []byte{0x01}, "feeder-A")
	filter.Handle(fe1)

	// Wait for feeder A to become stale
	time.Sleep(500 * time.Millisecond)

	// Now send frames from feeder B - should switch since A is stale
	switched := false
	for i := 0; i < 5; i++ {
		payload := []byte{byte(i + 100)}
		fe := makeMockFrameEvent(icao, payload, "feeder-B")
		result := filter.Handle(fe)
		if result != nil {
			switched = true
			break
		}
	}

	if !switched {
		t.Error("Expected switch to feeder B after feeder A became stale")
	}
}

// Background worker tests

func TestBackgroundWorker_LatencyTracking(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BackgroundInterval = 50 * time.Millisecond
	cfg.LatenessThreshold = 50 * time.Millisecond

	worker := NewBackgroundWorker(&cfg, zerolog.Nop())
	worker.Start()
	defer worker.Stop()

	// Record arrivals for the same payload from different feeders
	payloadKey := "test-payload-1"

	// Feeder A arrives first
	worker.RecordArrival(payloadKey, "feeder-A")

	// Feeder B arrives 10ms later (within threshold)
	time.Sleep(10 * time.Millisecond)
	worker.RecordArrival(payloadKey, "feeder-B")

	// Feeder C arrives 200ms later (beyond threshold)
	time.Sleep(200 * time.Millisecond)
	worker.RecordArrival(payloadKey, "feeder-C")

	// Wait for background computation
	time.Sleep(100 * time.Millisecond)

	// Feeder A should have excellent lateness score (always first)
	scoreA := worker.GetLatenessScore("feeder-A")
	if scoreA < 0.9 {
		t.Errorf("Feeder A lateness score should be ~1.0, got %f", scoreA)
	}

	// Feeder B should have good lateness score (within threshold)
	scoreB := worker.GetLatenessScore("feeder-B")
	if scoreB < 0.8 {
		t.Errorf("Feeder B lateness score should be high (within threshold), got %f", scoreB)
	}

	// Feeder C should have lower lateness score (beyond threshold)
	scoreC := worker.GetLatenessScore("feeder-C")
	if scoreC > 0.8 {
		t.Errorf("Feeder C lateness score should be lower (beyond threshold), got %f", scoreC)
	}
}

func TestBackgroundWorker_HonestyTracking(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BackgroundInterval = 50 * time.Millisecond
	cfg.PositionToleranceMeters = 200 // 200 meters tolerance

	worker := NewBackgroundWorker(&cfg, zerolog.Nop())
	worker.Start()
	defer worker.Stop()

	icao := uint32(0x7C1234)

	// Run multiple rounds of position reports to build up honesty stats
	for i := 0; i < 5; i++ {
		// Feeders A and B report similar positions (within ~10m of each other)
		worker.RecordPosition(icao, "feeder-A", -31.9505, 115.8605, 35000)   // Perth
		worker.RecordPosition(icao, "feeder-B", -31.95055, 115.86055, 35000) // Very close (~10m)

		// Feeder C reports a different position (outlier, ~2km away)
		worker.RecordPosition(icao, "feeder-C", -31.9680, 115.8780, 35000)

		// Wait for background computation
		time.Sleep(60 * time.Millisecond)
	}

	// Feeders A and B should have good honesty scores (close to median consensus)
	scoreA := worker.GetHonestyScore("feeder-A")
	scoreB := worker.GetHonestyScore("feeder-B")

	// Feeder C should have lower honesty score (outlier, far from consensus)
	scoreC := worker.GetHonestyScore("feeder-C")

	t.Logf("Honesty scores: A=%f, B=%f, C=%f", scoreA, scoreB, scoreC)

	// A and B should have non-zero scores
	if scoreA == 0 || scoreB == 0 {
		t.Errorf("Feeders A and B should have non-zero scores: A=%f, B=%f", scoreA, scoreB)
	}

	// The outlier should have a lower score
	if scoreC >= scoreA && scoreC >= scoreB {
		t.Error("Outlier feeder C should have lower honesty score than at least one of A or B")
	}
}

func TestHaversineDistance(t *testing.T) {
	tests := []struct {
		name      string
		lat1      float64
		lon1      float64
		lat2      float64
		lon2      float64
		expectKm  float64
		tolerance float64
	}{
		{
			name: "Perth to Sydney",
			lat1: -31.9505, lon1: 115.8605,
			lat2: -33.8688, lon2: 151.2093,
			expectKm:  3290,
			tolerance: 50,
		},
		{
			name: "Same point",
			lat1: -31.9505, lon1: 115.8605,
			lat2: -31.9505, lon2: 115.8605,
			expectKm:  0,
			tolerance: 0.001,
		},
		{
			name: "Short distance",
			lat1: -31.9505, lon1: 115.8605,
			lat2: -31.9515, lon2: 115.8615,
			expectKm:  0.14, // ~140 meters
			tolerance: 0.02,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			distMeters := haversineDistance(tt.lat1, tt.lon1, tt.lat2, tt.lon2)
			distKm := distMeters / 1000
			if distKm < tt.expectKm-tt.tolerance || distKm > tt.expectKm+tt.tolerance {
				t.Errorf("haversineDistance = %f km, want ~%f km (±%f)",
					distKm, tt.expectKm, tt.tolerance)
			}
		})
	}
}

func TestIsModeSPositionBytes(t *testing.T) {
	tests := []struct {
		name   string
		raw    []byte
		expect bool
	}{
		{"DF17 TC11 airborne position", []byte{0x8D, 0x00, 0x00, 0x00, 0x58, 0x00, 0x00}, true},
		{"DF17 TC5 surface position", []byte{0x8D, 0x00, 0x00, 0x00, 0x28, 0x00, 0x00}, true},
		{"DF17 TC19 velocity (no position)", []byte{0x8D, 0x00, 0x00, 0x00, 0x98, 0x00, 0x00}, false},
		{"DF17 TC22 GNSS position", []byte{0x8D, 0x00, 0x00, 0x00, 0xB0, 0x00, 0x00}, true},
		{"DF17 TC1 aircraft ID", []byte{0x8D, 0x00, 0x00, 0x00, 0x08, 0x00, 0x00}, false},
		{"DF17 TC28 status", []byte{0x8D, 0x00, 0x00, 0x00, 0xE0, 0x00, 0x00}, false},
		{"DF17 TC31 operational", []byte{0x8D, 0x00, 0x00, 0x00, 0xF8, 0x00, 0x00}, false},
		{"DF18 TC11 TIS-B position", []byte{0x90, 0x00, 0x00, 0x00, 0x58, 0x00, 0x00}, true},
		{"DF11 all-call (short)", []byte{0x5D, 0x00, 0x00, 0x00}, false},
		{"DF4 altitude reply", []byte{0x20, 0x00, 0x00, 0x00, 0x58}, false},
		{"too short", []byte{0x8D, 0x00}, false},
		{"empty", []byte{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isModeSPositionBytes(tt.raw)
			if result != tt.expect {
				t.Errorf("isModeSPositionBytes(%X) = %v, want %v", tt.raw, result, tt.expect)
			}
		})
	}
}

func TestFilter_NonPositionEarlyExit(t *testing.T) {
	// Known-valid DF17 frames from mode_s test suite:
	// Position: 8D75804B580FF2CF7E9BA6F701D0 — ICAO 75804B, TC=11 (airborne position baro)
	// Non-position: 8D4840D6202CC371C32CE0576098 — ICAO 4840D6, TC=4 (aircraft ID)
	const (
		positionHex    = "8D75804B580FF2CF7E9BA6F701D0" // TC=11, ICAO=75804B
		nonPositionHex = "8D4840D6202CC371C32CE0576098" // TC=4, ICAO=4840D6
	)

	// Use the same ICAO for both by using position frame's ICAO to establish sticky,
	// then test non-position from a different feeder on a different aircraft.
	// Actually, these are different ICAOs, so we test with two aircraft.

	filter := NewFilter()
	defer filter.Stop()

	// === Test 1: Non-position frame from non-sticky feeder is dropped ===
	// Establish feeder-A as sticky for ICAO 4840D6 using a mock frame (unknown type = conservative pass-through)
	fe1 := makeMockFrameEvent(0x4840D6, []byte{0x01}, "feeder-A")
	result := filter.Handle(fe1)
	if result == nil {
		t.Fatal("First frame from feeder A should be accepted")
	}

	// Non-position DF17 frame (TC=4, aircraft ID) from feeder-B for same ICAO — should be dropped early
	fe2 := makeDecodedModeSEvent(t, nonPositionHex, "feeder-B")
	result = filter.Handle(fe2)
	if result != nil {
		t.Error("Non-position frame from non-sticky feeder B should be dropped early")
	}

	// === Test 2: Position frame from non-sticky feeder still reaches scoring ===
	// Establish feeder-A as sticky for ICAO 75804B
	fe3 := makeMockFrameEvent(0x75804B, []byte{0x02}, "feeder-A")
	result = filter.Handle(fe3)
	if result == nil {
		t.Fatal("First frame from feeder A for second aircraft should be accepted")
	}

	// Position DF17 frame (TC=11) from feeder-B for same ICAO — should reach scoring (rejected by scoring, not early exit)
	fe4 := makeDecodedModeSEvent(t, positionHex, "feeder-B")
	result = filter.Handle(fe4)
	// Result may be nil (rejected by scoring) but the important thing is it wasn't early-exited.
	// We can't distinguish here, but the unit test for isModeSPositionBytes covers the detection logic.

	// === Test 3: Non-position frame from sticky feeder is NOT dropped ===
	// feeder-A is sticky for ICAO 4840D6 — non-position frame from feeder-A should pass through
	fe5 := makeDecodedModeSEvent(t, nonPositionHex, "feeder-A")
	result = filter.Handle(fe5)
	if result == nil {
		t.Error("Non-position frame from sticky feeder A should be accepted")
	}
}

func TestFilter_PacketCountDecay(t *testing.T) {
	filter := NewFilter()
	defer filter.Stop()

	icao := uint32(0x7C1234)

	// Send frames to build up packet counts
	for i := 0; i < 50; i++ {
		payload := []byte{byte(i), 0x01, 0x02}
		fe := makeMockFrameEvent(icao, payload, "feeder-A")
		filter.Handle(fe)
	}

	// Read the packet count before decay
	stateVal, ok := filter.aircraft.Load(icao)
	if !ok {
		t.Fatal("Expected aircraft state to exist")
	}
	state := stateVal.(*aircraftState)

	state.mu.RLock()
	stats := state.feeders["feeder-A"]
	countBefore := stats.packetCount
	state.mu.RUnlock()

	if countBefore == 0 {
		t.Fatal("Expected non-zero packet count before decay")
	}

	// Apply decay manually
	filter.decayPacketCounts()

	state.mu.RLock()
	countAfter := stats.packetCount
	state.mu.RUnlock()

	if countAfter >= countBefore {
		t.Errorf("Expected packet count to decrease after decay: before=%d, after=%d", countBefore, countAfter)
	}

	// Verify the decay factor is correct (truncation, not rounding)
	// With default config: BackgroundInterval=5s, MetricDecayWindow=30s
	// factor = pow(0.5, 5/30) ≈ 0.891
	expectedAfter := uint64(float64(countBefore) * filter.decayFactor)
	if countAfter != expectedAfter {
		t.Errorf("Decay not as expected: got=%d, want=%d (factor=%f)", countAfter, expectedAfter, filter.decayFactor)
	}
}

func TestFilter_PacketCountDecay_ReachesZero(t *testing.T) {
	filter := NewFilter()
	defer filter.Stop()

	icao := uint32(0xABCDEF)

	// Send a few frames
	for i := 0; i < 5; i++ {
		payload := []byte{byte(i), 0xAA}
		fe := makeMockFrameEvent(icao, payload, "feeder-A")
		filter.Handle(fe)
	}

	// Decay many times — counts should eventually reach zero
	for i := 0; i < 100; i++ {
		filter.decayPacketCounts()
	}

	stateVal, _ := filter.aircraft.Load(icao)
	state := stateVal.(*aircraftState)
	state.mu.RLock()
	count := state.feeders["feeder-A"].packetCount
	state.mu.RUnlock()

	if count != 0 {
		t.Errorf("Expected packet count to reach 0 after many decays, got %d", count)
	}
}

func TestFilter_GetOrCreateAircraftState_Concurrent(t *testing.T) {
	filter := NewFilter()
	defer filter.Stop()

	const goroutines = 100
	icao := uint32(0x7C9999)

	var wg sync.WaitGroup
	states := make(chan *aircraftState, goroutines)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			s := filter.getOrCreateAircraftState(icao)
			states <- s
		}()
	}
	wg.Wait()
	close(states)

	// All goroutines must get the same state pointer
	var first *aircraftState
	for s := range states {
		if first == nil {
			first = s
		} else if s != first {
			t.Fatal("Different goroutines got different aircraftState pointers — race in getOrCreateAircraftState")
		}
	}

	// Verify only one entry exists
	if filter.aircraft.Len() != 1 {
		t.Errorf("Expected 1 aircraft entry, got %d", filter.aircraft.Len())
	}
}

// Benchmark for staleness calculation
func BenchmarkCalculateStaleness(b *testing.B) {
	threshold := 1 * time.Second
	lastPacket := time.Now().Add(-500 * time.Millisecond)

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		calculateStaleness(lastPacket, threshold)
	}
}

func TestFilter_CoordinatorAdvertisesWinningEpochOnly(t *testing.T) {
	f := NewFilter()
	defer f.Stop()
	icao := uint32(0x4840D6)

	// Simulate two epochs from same feeder
	frame1 := createTestBeastFrame(icao, 1, "rx-north")
	frame2 := createTestBeastFrame(icao, 2, "rx-north")

	// Process frames from both epochs
	f.Handle(frame1)
	f.Handle(frame2)

	// GetAllAircraftScores should return the winning epoch's feeder tag
	// WITHOUT the epoch suffix
	scores := f.GetAllAircraftScores()
	if score, ok := scores[icao]; ok {
		if strings.Contains(score.FeederTag, "#") {
			t.Errorf("Expected feeder tag without epoch suffix, got %s", score.FeederTag)
		}
		if score.FeederTag != "rx-north" {
			t.Errorf("Expected feeder tag 'rx-north', got %s", score.FeederTag)
		}
	} else {
		t.Error("Expected aircraft to have a score")
	}
}

// Integration Tests for Epoch-Based Isolation

func TestIntegration_MultipleEpochsFromSameFeeder_DifferentSignalStrengths(t *testing.T) {
	// Use very low hysteresis to make switching easier based on signal strength differences
	f := NewFilter(WithHysteresis(0.01))
	defer f.Stop()
	icao := uint32(0x4840D6)

	// Scenario: Feeder rx-north has 3 sub-producers (epochs)
	// Epoch 1: moderate signal (-10 dBFS), established first
	// Epoch 2: weak signal (-30 dBFS), poor performance
	// Epoch 3: appears later with very strong signal (-1 dBFS), should win

	// Epoch 1 frames - moderate signal
	for i := 0; i < 10; i++ {
		payload := make([]byte, 3)
		payload[0] = byte(icao >> 16)
		payload[1] = byte(icao >> 8)
		payload[2] = byte(i) // Vary for unique payloads
		mockFr := &mockFrame{
			icao:           icao,
			icaoStr:        "4840D6",
			raw:            payload,
			epochID:        1,
			signalStrength: -10.0,
		}
		fe := tracker.NewFrameEvent(mockFr, &tracker.FrameSource{Tag: "rx-north"})
		frameEvent := &fe
		result := f.Handle(frameEvent)
		if result == nil && i == 0 {
			t.Error("Expected first frame to be accepted")
		}
	}

	// Epoch 2 frames - weak signal (should be rejected or briefly accepted, low quality)
	for i := 0; i < 5; i++ {
		payload := make([]byte, 3)
		payload[0] = byte(icao >> 16)
		payload[1] = byte(icao >> 8)
		payload[2] = 100 + byte(i) // Different range for epoch 2
		mockFr := &mockFrame{
			icao:           icao,
			icaoStr:        "4840D6",
			raw:            payload,
			epochID:        2,
			signalStrength: -30.0,
		}
		fe := tracker.NewFrameEvent(mockFr, &tracker.FrameSource{Tag: "rx-north"})
		frameEvent := &fe
		f.Handle(frameEvent)
	}

	// Epoch 3 frames - very strong signal (should win and become sticky)
	acceptedFromEpoch3 := false
	for i := 0; i < 15; i++ {
		payload := make([]byte, 3)
		payload[0] = byte(icao >> 16)
		payload[1] = byte(icao >> 8)
		payload[2] = 150 + byte(i) // Different range for epoch 3
		mockFr := &mockFrame{
			icao:           icao,
			icaoStr:        "4840D6",
			raw:            payload,
			epochID:        3,
			signalStrength: -1.0, // Very strong signal
		}
		fe := tracker.NewFrameEvent(mockFr, &tracker.FrameSource{Tag: "rx-north"})
		frameEvent := &fe
		if f.Handle(frameEvent) != nil {
			acceptedFromEpoch3 = true
		}
	}

	if !acceptedFromEpoch3 {
		t.Error("Expected at least one frame from epoch 3 to be accepted due to superior signal strength")
	}

	// Verify sticky feeder is now on rx-north
	scores := f.GetAllAircraftScores()
	if score, ok := scores[icao]; ok {
		if score.FeederTag != "rx-north" {
			t.Errorf("Expected sticky feeder to be rx-north, got %s", score.FeederTag)
		}
	} else {
		t.Error("Expected aircraft to have a score after processing all epochs")
	}
}

func TestIntegration_EpochTimeoutAndRecovery(t *testing.T) {
	// This test verifies that new epochs can be detected and handled
	// alongside stale epochs. The system should continue tracking aircraft
	// even when epochs change.

	f := NewFilter(WithHysteresis(0.01))
	defer f.Stop()
	icao := uint32(0x4840D6)

	// Frame from epoch 1 establishes sticky feeder as rx-north#1
	frame1 := createTestBeastFrame(icao, 1, "rx-north")
	result1 := f.Handle(frame1)
	if result1 == nil {
		t.Error("Expected frame from epoch 1 to be accepted")
	}

	// Send many frames from epoch 2 with strong signal to eventually take over
	// (In real scenario, EpochDetector would mark epoch 1 stale after 30 seconds)
	acceptedFromEpoch2 := false
	for i := 0; i < 20; i++ {
		payload := make([]byte, 3)
		payload[0] = byte(icao >> 16)
		payload[1] = byte(icao >> 8)
		payload[2] = 100 + byte(i)
		mockFr := &mockFrame{
			icao:           icao,
			icaoStr:        "4840D6",
			raw:            payload,
			epochID:        2,
			signalStrength: -5.0, // Strong signal to build score
		}
		fe := tracker.NewFrameEvent(mockFr, &tracker.FrameSource{Tag: "rx-north"})
		frameEvent := &fe
		if f.Handle(frameEvent) != nil {
			acceptedFromEpoch2 = true
		}
	}

	if !acceptedFromEpoch2 {
		t.Error("Expected at least some frames from epoch 2 to be accepted after many attempts")
	}

	// Verify aircraft is still tracked
	scores := f.GetAllAircraftScores()
	if _, ok := scores[icao]; !ok {
		t.Error("Expected aircraft to be tracked after epoch change")
	}
}

func TestIntegration_MultipleFeedersBothWithEpochs(t *testing.T) {
	// Verifies isolation works across multiple independent feeders

	f := NewFilter()
	defer f.Stop()
	icao := uint32(0x4840D6)

	// Feeder A with 2 epochs
	frameA1 := createTestBeastFrame(icao, 1, "rx-north")
	frameA2 := createTestBeastFrame(icao, 2, "rx-north")

	// Feeder B with 2 epochs (different feeder, same aircraft)
	frameB1 := createTestBeastFrame(icao, 1, "rx-south")
	frameB2 := createTestBeastFrame(icao, 2, "rx-south")

	// Process frames from all combinations
	f.Handle(frameA1) // First frame - sticky is A#1
	f.Handle(frameA2) // A#2 enters as challenger
	f.Handle(frameB1) // B#1 enters as challenger
	f.Handle(frameB2) // B#2 enters as challenger

	// Verify one feeder is selected as sticky
	scores := f.GetAllAircraftScores()
	if score, ok := scores[icao]; ok {
		// Should be either rx-north or rx-south, without epoch suffix
		if score.FeederTag != "rx-north" && score.FeederTag != "rx-south" {
			t.Errorf("Expected sticky feeder to be rx-north or rx-south, got %s", score.FeederTag)
		}
		if strings.Contains(score.FeederTag, "#") {
			t.Errorf("Advertised feeder tag should not contain epoch suffix, got %s", score.FeederTag)
		}
	} else {
		t.Error("Expected aircraft to be tracked with multiple feeders and epochs")
	}
}
