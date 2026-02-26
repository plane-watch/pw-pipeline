package producer

import (
	"testing"
	"time"

	"plane.watch/lib/tracker/beast"
)

func TestEpochDetection_FirstFrame(t *testing.T) {
	ed := NewEpochDetector(10 * time.Second)

	// First frame should establish epoch
	epochID := ed.ProcessTicks(time.Duration(1000) * time.Second)
	if epochID != 1 {
		t.Errorf("Expected epochID 1, got %d", epochID)
	}
}

func TestEpochDetection_BackwardsJump(t *testing.T) {
	ed := NewEpochDetector(10 * time.Second)

	// Establish epoch with a large tick value (e.g., boot time in seconds)
	epochID1 := ed.ProcessTicks(time.Duration(1000) * time.Second)

	// Large backwards jump > 5 seconds = restart/new sub-producer
	epochID2 := ed.ProcessTicks(time.Duration(100) * time.Second)

	if epochID1 == epochID2 {
		t.Errorf("Expected different epoch IDs for backwards jump, got %d == %d", epochID1, epochID2)
	}
}

func TestEpochDetection_SmallJitterIgnored(t *testing.T) {
	ed := NewEpochDetector(5 * time.Second)

	// Establish epoch
	epochID1 := ed.ProcessTicks(time.Duration(1000) * time.Second)

	// Small backwards jump (1 second < 5 second threshold)
	// Should NOT trigger new epoch (same epoch ID)
	epochID2 := ed.ProcessTicks(time.Duration(999) * time.Second)

	if epochID1 != epochID2 {
		t.Errorf("Expected same epoch for minor jitter, got %d != %d", epochID1, epochID2)
	}

	// Now a large backwards jump (10 seconds > 5 second threshold)
	// Should trigger new epoch
	epochID3 := ed.ProcessTicks(time.Duration(990) * time.Second)

	if epochID1 == epochID3 {
		t.Errorf("Expected different epoch IDs for large backwards jump, got %d == %d", epochID1, epochID3)
	}
}

func TestEpochDetection_NormalProgression(t *testing.T) {
	ed := NewEpochDetector(10 * time.Second)

	epochID1 := ed.ProcessTicks(time.Duration(1000) * time.Second)
	epochID2 := ed.ProcessTicks(time.Duration(1100) * time.Second)
	epochID3 := ed.ProcessTicks(time.Duration(1200) * time.Second)

	if epochID1 != epochID2 || epochID2 != epochID3 {
		t.Errorf("Expected same epoch for normal progression, got %d, %d, %d", epochID1, epochID2, epochID3)
	}
}

func TestEpochDetection_StaleTimeout(t *testing.T) {
	ed := NewEpochDetector(100 * time.Millisecond)

	epochID1 := ed.ProcessTicks(time.Duration(1000) * time.Second)

	// Wait for epoch to go stale
	time.Sleep(150 * time.Millisecond)

	// New frame after timeout = new epoch
	epochID2 := ed.ProcessTicks(time.Duration(2000) * time.Second)

	if epochID1 == epochID2 {
		t.Errorf("Expected different epoch IDs after timeout, got %d == %d", epochID1, epochID2)
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

	// Test setting epoch ID
	frame.SetEpochID(42)
	if frame.EpochID() != 42 {
		t.Errorf("Expected EpochID 42 after SetEpochID, got %d", frame.EpochID())
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
