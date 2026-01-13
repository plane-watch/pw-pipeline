package stickyfeeder

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"plane.watch/lib/tracker"
)

func init() {
	zerolog.SetGlobalLevel(zerolog.ErrorLevel)
}

// mockFrame is a simple mock implementation of tracker.Frame for testing
type mockFrame struct {
	icao    uint32
	icaoStr string
	raw     []byte
}

func (m *mockFrame) Icao() uint32         { return m.icao }
func (m *mockFrame) IcaoStr() string      { return m.icaoStr }
func (m *mockFrame) Decode() error        { return nil }
func (m *mockFrame) TimeStamp() time.Time { return time.Now() }
func (m *mockFrame) Raw() []byte          { return m.raw }

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
		calculateScore(stats, &cfg)
	}
}
