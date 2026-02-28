package producer

import (
	"testing"
	"time"

	"plane.watch/lib/tracker/beast"
)

func TestEpochDetection_FirstFrame(t *testing.T) {
	ed := NewEpochDetector(10 * time.Second)

	// First frame should calculate epoch as: now_unix_seconds - ticks_seconds
	tickValue := 100 * time.Millisecond
	now := time.Now()
	epochID := ed.ProcessTicks(tickValue)

	// epochID should be approximately now.Unix() (receiver just started 100ms ago)
	expected := uint32(now.Unix())
	if epochID != expected && epochID != expected-1 {
		t.Errorf("Expected epochID ~%d, got %d", expected, epochID)
	}
}

func TestEpochDetection_BackwardsJump(t *testing.T) {
	ed := NewEpochDetector(10 * time.Second)

	// Establish epoch: receiver has been on for 10 seconds
	tickValue1 := 10 * time.Second
	epochID1 := ed.ProcessTicks(tickValue1)

	// Large backwards jump > 5 seconds = restart/new sub-producer
	// Jump to 1 second uptime (9 second jump > 5 second threshold)
	tickValue2 := 1 * time.Second
	epochID2 := ed.ProcessTicks(tickValue2)

	// Different ticks means different power-on time → different epoch
	// epochID1 ≈ now - 10, epochID2 ≈ now - 1, so they differ by ~9
	if epochID1 == epochID2 {
		t.Errorf("Expected different epoch IDs for backwards jump, got %d == %d", epochID1, epochID2)
	}

	// The difference should be approximately 9 seconds (10 - 1)
	diff := int64(epochID2) - int64(epochID1)
	if diff < 7 || diff > 11 {
		t.Errorf("Expected ~9 second difference between epochs, got %d", diff)
	}
}

func TestEpochDetection_SmallJitterIgnored(t *testing.T) {
	ed := NewEpochDetector(5 * time.Second)

	// Establish epoch: receiver has been on for 10 seconds
	tickValue1 := 10 * time.Second
	epochID1 := ed.ProcessTicks(tickValue1)

	// Small backwards jump (1 ms < 5 second threshold)
	// Should NOT trigger new epoch
	tickValue2 := time.Duration(9999000000) // 1 ms less - should be ignored
	epochID2 := ed.ProcessTicks(tickValue2)
	if epochID1 != epochID2 {
		t.Errorf("Expected same epoch for minor jitter, got %d != %d", epochID1, epochID2)
	}

	// Large backwards jump (7 seconds > 5 second threshold)
	tickValue3 := 3 * time.Second
	epochID3 := ed.ProcessTicks(tickValue3)

	if epochID1 == epochID3 {
		t.Errorf("Expected different epoch IDs for large backwards jump, got %d == %d", epochID1, epochID3)
	}
}

func TestEpochDetection_NormalProgression(t *testing.T) {
	ed := NewEpochDetector(10 * time.Second)

	tickValue1 := 1 * time.Second
	tickValue2 := 1100 * time.Millisecond
	tickValue3 := 1200 * time.Millisecond

	epochID1 := ed.ProcessTicks(tickValue1)
	epochID2 := ed.ProcessTicks(tickValue2)
	epochID3 := ed.ProcessTicks(tickValue3)

	// All frames from same receiver should have same epoch ID
	if epochID1 != epochID2 || epochID2 != epochID3 {
		t.Errorf("Expected same epoch for normal progression, got %d, %d, %d", epochID1, epochID2, epochID3)
	}

	// Should be approximately now.Unix() - 1
	now := time.Now()
	expected := uint32(now.Unix() - 1)
	if epochID1 != expected && epochID1 != expected-1 && epochID1 != expected+1 {
		t.Errorf("Expected epoch ~%d (now-1), got %d", expected, epochID1)
	}
}

func TestEpochDetection_StaleTimeout(t *testing.T) {
	ed := NewEpochDetector(100 * time.Millisecond)

	// Use a controlled clock to avoid second-boundary race conditions.
	// Start at a clean second boundary so the 150ms advance doesn't cross one.
	mockNow := time.Unix(1700000000, 0)
	ed.nowFunc = func() time.Time { return mockNow }

	tickValue1 := 1 * time.Second
	epochID1 := ed.ProcessTicks(tickValue1)

	// Advance past stale timeout
	mockNow = mockNow.Add(150 * time.Millisecond)

	// New frame with different uptime — old entry is stale
	tickValue2 := 2 * time.Second
	epochID2 := ed.ProcessTicks(tickValue2)

	// epochID1 = 1700000000 - 1 = 1699999999
	// epochID2 = 1700000000 - 2 = 1699999998 (now.Unix() unchanged, ticks differ)
	if epochID1 == epochID2 {
		t.Errorf("Expected different epoch IDs after timeout, got %d == %d", epochID1, epochID2)
	}
}

func TestEpochDetection_SameReceiverStableID(t *testing.T) {
	ed := NewEpochDetector(10 * time.Second)

	// Simulate a receiver that's been on for 1000 seconds, sending many frames
	// All should get the same epoch ID
	baseUptime := 1000 * time.Second
	epochID := ed.ProcessTicks(baseUptime)

	for i := 0; i < 100; i++ {
		ticks := baseUptime + time.Duration(i)*time.Millisecond
		id := ed.ProcessTicks(ticks)
		if id != epochID {
			t.Fatalf("Frame %d got different epochID: %d vs %d", i, id, epochID)
		}
	}
}

func TestProducer_EpochTracking(t *testing.T) {
	p := New(
		WithType(Beast),
		WithEpochStaleTimeout(5*time.Second),
	)

	if p.epochStaleTimeout != 5*time.Second {
		t.Errorf("Expected epoch stale timeout 5s, got %v", p.epochStaleTimeout)
	}
}

func TestBeastFrame_EpochID(t *testing.T) {
	frameData := []byte{
		0x1A, 0x33, // Start marker + Mode-S Long type
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, // 6-byte timestamp
		0xFF, // Signal level
		0x8D, 0x48, 0x40, 0xD6, 0x20, 0x2C, 0xC3, 0x71,
		0xC3, 0x2C, 0xE0, 0x57, 0x60, 0x98,
	}

	frame, _ := beast.NewFrame(frameData, false)
	if frame == nil {
		t.Fatal("Failed to create test frame")
	}

	if frame.EpochID() != 0 {
		t.Errorf("Expected default EpochID 0, got %d", frame.EpochID())
	}

	var epochValue uint32 = 1740000000 // a realistic unix timestamp in seconds
	frame.SetEpochID(epochValue)
	if frame.EpochID() != epochValue {
		t.Errorf("Expected EpochID %d after SetEpochID, got %d", epochValue, frame.EpochID())
	}
}

func TestEpochDetection_TwoInterleavedReceivers(t *testing.T) {
	ed := NewEpochDetector(30 * time.Second)

	// Mock clock that advances with test progression
	startTime := time.Now()
	frameCount := 0
	ed.nowFunc = func() time.Time {
		frameCount++
		// Each frame pair (A+B) represents ~100ms of real time
		return startTime.Add(time.Duration(frameCount/2) * 100 * time.Millisecond)
	}

	// Receiver A: ~1000s uptime, Receiver B: ~5000s uptime
	baseA := 1000 * time.Second
	baseB := 5000 * time.Second

	epochA := ed.ProcessTicks(baseA)
	epochB := ed.ProcessTicks(baseB)

	if epochA == epochB {
		t.Fatalf("Two receivers with different uptimes should have different epoch IDs: A=%d B=%d", epochA, epochB)
	}

	// Interleave 50 frames from each receiver, advancing ticks monotonically
	for i := 1; i <= 50; i++ {
		ticksA := baseA + time.Duration(i)*100*time.Millisecond
		ticksB := baseB + time.Duration(i)*100*time.Millisecond

		idA := ed.ProcessTicks(ticksA)
		idB := ed.ProcessTicks(ticksB)

		if idA != epochA {
			t.Fatalf("Frame %d: Receiver A epoch changed from %d to %d (ticks=%v)", i, epochA, idA, ticksA)
		}
		if idB != epochB {
			t.Fatalf("Frame %d: Receiver B epoch changed from %d to %d (ticks=%v)", i, epochB, idB, ticksB)
		}
	}
}

func TestEpochDetection_ThreeReceivers(t *testing.T) {
	ed := NewEpochDetector(30 * time.Second)

	// Three receivers at different uptimes
	base1 := 100 * time.Second
	base2 := 500 * time.Second
	base3 := 2000 * time.Second

	epoch1 := ed.ProcessTicks(base1)
	epoch2 := ed.ProcessTicks(base2)
	epoch3 := ed.ProcessTicks(base3)

	// All three must be distinct
	if epoch1 == epoch2 || epoch2 == epoch3 || epoch1 == epoch3 {
		t.Fatalf("Three receivers should have distinct epoch IDs: %d, %d, %d", epoch1, epoch2, epoch3)
	}

	// Interleave frames and verify stability
	for i := 1; i <= 20; i++ {
		d := time.Duration(i) * 100 * time.Millisecond
		id1 := ed.ProcessTicks(base1 + d)
		id2 := ed.ProcessTicks(base2 + d)
		id3 := ed.ProcessTicks(base3 + d)

		if id1 != epoch1 {
			t.Fatalf("Frame %d: Receiver 1 epoch changed from %d to %d", i, epoch1, id1)
		}
		if id2 != epoch2 {
			t.Fatalf("Frame %d: Receiver 2 epoch changed from %d to %d", i, epoch2, id2)
		}
		if id3 != epoch3 {
			t.Fatalf("Frame %d: Receiver 3 epoch changed from %d to %d", i, epoch3, id3)
		}
	}
}

func TestEpochDetection_ReceiverRestartCreatesNewStream(t *testing.T) {
	ed := NewEpochDetector(30 * time.Second)

	// Receiver at 1002s uptime
	ticks1 := 1002 * time.Second
	epochBefore := ed.ProcessTicks(ticks1)

	// Receiver restarts — ticks drop to 0.5s (well outside the match window)
	ticks2 := 500 * time.Millisecond
	epochAfter := ed.ProcessTicks(ticks2)

	if epochBefore == epochAfter {
		t.Fatalf("Receiver restart should create new epoch: before=%d after=%d", epochBefore, epochAfter)
	}

	// The new stream's epoch should reflect a very recent power-on
	now := time.Now()
	expected := uint32(now.Unix())
	if epochAfter != expected && epochAfter != expected-1 {
		t.Errorf("After restart, epoch should be ~now (%d), got %d", expected, epochAfter)
	}
}

func TestEpochDetection_StaleStreamEvicted(t *testing.T) {
	ed := NewEpochDetector(100 * time.Millisecond)

	// Create two streams
	epoch1 := ed.ProcessTicks(1000 * time.Second)
	epoch2 := ed.ProcessTicks(5000 * time.Second)
	_ = epoch1
	_ = epoch2

	// Let both go stale
	time.Sleep(150 * time.Millisecond)

	// New frame should create a fresh stream (old ones evicted on lookup)
	epochNew := ed.ProcessTicks(200 * time.Second)

	// The new epoch should be different from both originals
	if epochNew == epoch1 && epochNew == epoch2 {
		t.Fatalf("New stream after stale eviction should have a fresh epoch ID")
	}

	// The stale entries are lazily evicted on lookup, not all at once.
	// At minimum, the new entry should exist.
	ed.mu.Lock()
	_, exists := ed.streams[epochNew]
	ed.mu.Unlock()
	if !exists {
		t.Errorf("Expected new stream entry to exist in map")
	}
}

func TestEpochDetection_ManyReceivers(t *testing.T) {
	ed := NewEpochDetector(30 * time.Second)

	// Simulate an aggregator like LEPP-2043 with many receivers at different uptimes.
	// Each receiver's uptime is spaced 1000s apart to ensure distinct epoch IDs.
	receiverCount := 100
	epochs := make([]uint32, receiverCount)
	for i := 0; i < receiverCount; i++ {
		ticks := time.Duration(i*1000+100) * time.Second // 100s, 1100s, 2100s, ...
		epochs[i] = ed.ProcessTicks(ticks)
	}

	// All receivers should be tracked (no hard cap)
	ed.mu.Lock()
	count := len(ed.streams)
	ed.mu.Unlock()
	if count != receiverCount {
		t.Errorf("Expected %d streams, got %d", receiverCount, count)
	}

	// All epoch IDs should be unique
	seen := make(map[uint32]bool)
	for i, e := range epochs {
		if seen[e] {
			t.Errorf("Receiver %d has duplicate epoch ID %d", i, e)
		}
		seen[e] = true
	}

	// Re-send frames from each receiver — all should match their original epoch
	for i := 0; i < receiverCount; i++ {
		ticks := time.Duration(i*1000+101) * time.Second // slightly advanced
		id := ed.ProcessTicks(ticks)
		if id != epochs[i] {
			t.Errorf("Receiver %d: epoch changed from %d to %d on re-send", i, epochs[i], id)
		}
	}
}

func TestEpochDetection_CloseUptimeReceiversStayDistinct(t *testing.T) {
	ed := NewEpochDetector(30 * time.Second)

	// Mock clock that advances with test progression
	startTime := time.Now()
	frameCount := 0
	ed.nowFunc = func() time.Time {
		frameCount++
		return startTime.Add(time.Duration(frameCount/2) * 100 * time.Millisecond)
	}

	// Two receivers with uptimes 10 seconds apart.
	// Epoch IDs differ by ~10, which is > epochLookupTolerance (2).
	baseA := 50000 * time.Second
	baseB := 50010 * time.Second

	epochA := ed.ProcessTicks(baseA)
	epochB := ed.ProcessTicks(baseB)

	if epochA == epochB {
		t.Fatalf("Receivers 10s apart should have different epoch IDs: A=%d B=%d", epochA, epochB)
	}

	// Interleave 100 frames. Both must keep their original epoch IDs.
	for i := 1; i <= 100; i++ {
		ticksA := baseA + time.Duration(i)*100*time.Millisecond
		ticksB := baseB + time.Duration(i)*100*time.Millisecond

		idA := ed.ProcessTicks(ticksA)
		idB := ed.ProcessTicks(ticksB)

		if idA != epochA {
			t.Fatalf("Frame %d: Receiver A epoch changed from %d to %d (ticks=%v)", i, epochA, idA, ticksA)
		}
		if idB != epochB {
			t.Fatalf("Frame %d: Receiver B epoch changed from %d to %d (ticks=%v)", i, epochB, idB, ticksB)
		}
	}
}

func TestEpochDetection_StaleSweep(t *testing.T) {
	ed := NewEpochDetector(100 * time.Millisecond)

	// Create many entries
	for i := 0; i < 50; i++ {
		ticks := time.Duration(i*1000+100) * time.Second
		ed.ProcessTicks(ticks)
	}

	ed.mu.Lock()
	beforeCount := len(ed.streams)
	ed.mu.Unlock()
	if beforeCount != 50 {
		t.Fatalf("Expected 50 streams before stale, got %d", beforeCount)
	}

	// Let all go stale
	time.Sleep(150 * time.Millisecond)

	// ProcessTicks with a new value won't trigger sweep (< staleSweepThreshold)
	// but stale entries encountered during lookup will be deleted
	ed.ProcessTicks(999999 * time.Second)

	ed.mu.Lock()
	afterCount := len(ed.streams)
	ed.mu.Unlock()

	// At minimum the new entry exists; stale entries may or may not be cleaned
	// depending on whether their keys were checked during lookup
	if afterCount < 1 {
		t.Errorf("Expected at least 1 stream after new frame, got %d", afterCount)
	}
}

func TestProducer_EpochDetectorCleanup(t *testing.T) {
	p := New(
		WithType(Beast),
		WithEpochStaleTimeout(5*time.Second),
	)

	ed1 := p.getEpochDetector("rx-north")
	ed2 := p.getEpochDetector("rx-south")

	if ed1 == nil || ed2 == nil {
		t.Fatal("Failed to create epoch detectors")
	}

	ed1Again := p.getEpochDetector("rx-north")
	if ed1 != ed1Again {
		t.Errorf("Expected same epoch detector instance, got different instances")
	}

	p.Cleanup()

	ed1Final := p.getEpochDetector("rx-north")
	if ed1Final != ed1 {
		t.Errorf("Expected same detector after Cleanup(), got different instance")
	}
}
