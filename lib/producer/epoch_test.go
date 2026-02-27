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

	tickValue1 := 1 * time.Second
	epochID1 := ed.ProcessTicks(tickValue1)

	// Wait for epoch to go stale
	time.Sleep(150 * time.Millisecond)

	// New frame after timeout = new epoch
	tickValue2 := 2 * time.Second
	epochID2 := ed.ProcessTicks(tickValue2)

	// Different because stale timeout triggered a recalculation
	// epochID1 ≈ now - 1, epochID2 ≈ now - 2 (but 150ms later), so they should differ
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
