package producer

import (
	"testing"
	"time"

	"plane.watch/lib/tracker/beast"
)

func TestEpochDetection_FirstFrame(t *testing.T) {
	ed := NewEpochDetector(10 * time.Second)

	// First frame should establish epoch at the actual tick value
	// Using tick values that fit within uint32 range
	tickValue := time.Duration(100000000) // 100 million nanoseconds = 100 ms
	epochID := ed.ProcessTicks(tickValue)
	// epochID should be the actual tick value, not a counter (1)
	if epochID != uint32(tickValue) {
		t.Errorf("Expected epochID %d, got %d", uint32(tickValue), epochID)
	}
}

func TestEpochDetection_BackwardsJump(t *testing.T) {
	ed := NewEpochDetector(10 * time.Second)

	// Establish epoch with a tick value
	// Start at 10 seconds in nanoseconds
	tickValue1 := time.Duration(10000000000) // 10 seconds in nanoseconds
	epochID1 := ed.ProcessTicks(tickValue1)
	if epochID1 != uint32(tickValue1) {
		t.Fatalf("Expected first epoch %d, got %d", uint32(tickValue1), epochID1)
	}

	// Large backwards jump > 5 seconds = restart/new sub-producer
	// Should create new epoch at the jumped-to value
	// Jump back to 1 second (9 second jump, more than 5 second threshold)
	tickValue2 := time.Duration(1000000000) // 1 second in nanoseconds
	epochID2 := ed.ProcessTicks(tickValue2)
	if epochID2 != uint32(tickValue2) {
		t.Errorf("Expected epoch at restart value %d, got %d", uint32(tickValue2), epochID2)
	}

	if epochID1 == epochID2 {
		t.Errorf("Expected different epoch IDs for backwards jump, got %d == %d", epochID1, epochID2)
	}
}

func TestEpochDetection_SmallJitterIgnored(t *testing.T) {
	ed := NewEpochDetector(5 * time.Second)

	// Establish epoch
	// Start at 10 seconds in nanoseconds
	tickValue1 := time.Duration(10000000000) // 10 seconds in nanoseconds
	epochID1 := ed.ProcessTicks(tickValue1)
	if epochID1 != uint32(tickValue1) {
		t.Fatalf("Expected first epoch %d, got %d", uint32(tickValue1), epochID1)
	}

	// Small backwards jump (1 second < 5 second threshold)
	// Should NOT trigger new epoch (same epoch ID)
	tickValue2 := time.Duration(9999000000) // 1 ms less - should be ignored
	epochID2 := ed.ProcessTicks(tickValue2)
	if epochID1 != epochID2 {
		t.Errorf("Expected same epoch for minor jitter, got %d != %d", epochID1, epochID2)
	}
	if epochID2 != uint32(tickValue1) {
		t.Errorf("Expected same epoch value %d after jitter, got %d", uint32(tickValue1), epochID2)
	}

	// Now a large backwards jump (more than 5 second threshold)
	// Should trigger new epoch at the new value
	// Jump back from 10 seconds to 3 seconds (7 second jump > 5 second threshold)
	tickValue3 := time.Duration(3000000000) // 3 seconds in nanoseconds
	epochID3 := ed.ProcessTicks(tickValue3)
	if epochID1 == epochID3 {
		t.Errorf("Expected different epoch IDs for large backwards jump, got %d == %d", epochID1, epochID3)
	}
	if epochID3 != uint32(tickValue3) {
		t.Errorf("Expected epoch at large jump value %d, got %d", uint32(tickValue3), epochID3)
	}
}

func TestEpochDetection_NormalProgression(t *testing.T) {
	ed := NewEpochDetector(10 * time.Second)

	tickValue1 := time.Duration(1000000000) // 1 second in nanoseconds
	tickValue2 := time.Duration(1100000000) // 1.1 seconds
	tickValue3 := time.Duration(1200000000) // 1.2 seconds

	epochID1 := ed.ProcessTicks(tickValue1)
	epochID2 := ed.ProcessTicks(tickValue2)
	epochID3 := ed.ProcessTicks(tickValue3)

	if epochID1 != epochID2 || epochID2 != epochID3 {
		t.Errorf("Expected same epoch for normal progression, got %d, %d, %d", epochID1, epochID2, epochID3)
	}

	if epochID1 != uint32(tickValue1) {
		t.Errorf("Expected epoch value %d, got %d", uint32(tickValue1), epochID1)
	}
}

func TestEpochDetection_StaleTimeout(t *testing.T) {
	ed := NewEpochDetector(100 * time.Millisecond)

	tickValue1 := time.Duration(1000000000) // 1 second in nanoseconds
	epochID1 := ed.ProcessTicks(tickValue1)
	if epochID1 != uint32(tickValue1) {
		t.Fatalf("Expected first epoch %d, got %d", uint32(tickValue1), epochID1)
	}

	// Wait for epoch to go stale
	time.Sleep(150 * time.Millisecond)

	// New frame after timeout = new epoch at the new tick value
	tickValue2 := time.Duration(2000000000) // 2 seconds in nanoseconds
	epochID2 := ed.ProcessTicks(tickValue2)
	if epochID1 == epochID2 {
		t.Errorf("Expected different epoch IDs after timeout, got %d == %d", epochID1, epochID2)
	}
	if epochID2 != uint32(tickValue2) {
		t.Errorf("Expected new epoch at tick value %d, got %d", uint32(tickValue2), epochID2)
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
	// Create a minimal valid Beast frame
	frameData := []byte{
		0x1A, 0x33, // Start marker + Mode-S Long type
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, // 6-byte timestamp
		0xFF, // Signal level
		0x8D, 0x48, 0x40, 0xD6, 0x20, 0x2C, 0xC3, 0x71,
		0xC3, 0x2C, 0xE0, 0x57, 0x60, 0x98,
	}

	// Create a frame (ignoring error for test simplicity - it's a valid frame)
	// Note: NewFrame signature is (rawBytes, isRadarCape)
	frame, _ := beast.NewFrame(frameData, false)
	if frame == nil {
		t.Fatal("Failed to create test frame")
	}

	// Test default (unset) epoch ID
	if frame.EpochID() != 0 {
		t.Errorf("Expected default EpochID 0, got %d", frame.EpochID())
	}

	// Test setting epoch ID to an actual MLAT tick value
	// Use a realistic MLAT tick value (1 second in nanoseconds, truncated to uint32)
	epochValue := uint32(time.Duration(1000000000))
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

	// Create some epoch detectors by calling getEpochDetector
	ed1 := p.getEpochDetector("rx-north")
	ed2 := p.getEpochDetector("rx-south")

	if ed1 == nil || ed2 == nil {
		t.Fatal("Failed to create epoch detectors")
	}

	// Verify both detectors are accessible
	ed1Again := p.getEpochDetector("rx-north")
	if ed1 != ed1Again {
		t.Errorf("Expected same epoch detector instance, got different instances")
	}

	// Verify that cleanup() doesn't panic when epochDetectors are present
	// (This is the main purpose of the test - ensure no panic during cleanup)
	// Note: We test cleanup directly since Stop() requires the producer to be running
	p.Cleanup()

	// After cleanup, detectors are still there (sync.Map doesn't require explicit cleanup)
	// But verify we can still read them without panic
	ed1Final := p.getEpochDetector("rx-north")
	if ed1Final != ed1 {
		t.Errorf("Expected same detector after Cleanup(), got different instance")
	}
}
