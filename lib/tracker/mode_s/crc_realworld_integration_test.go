package mode_s_test

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"plane.watch/lib/producer"
	"plane.watch/lib/tracker/beast"
	"plane.watch/lib/tracker/mode_s"
)

// TestCRCWithRealWorldData validates CRC implementation against real Beast format data
func TestCRCWithRealWorldData(t *testing.T) {
	// Read the beast file
	data, err := os.ReadFile("cmd/stress/full-feed.beast")
	if err != nil {
		t.Skip("Skipping real-world test: beast file not found")
		return
	}

	stats := struct {
		totalFrames      int
		validFrames      int
		invalidCRC       int
		unknownDF        int
		parseErrors      int
		df0, df4, df5    int
		df11, df16       int
		df17, df18       int
		df20, df21, df24 int
	}{}

	// Use the Beast scanner to properly handle escapes
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Split(producer.ScanBeast)

	for scanner.Scan() {
		beastFrame := scanner.Bytes()

		// Parse Beast frame
		frame, err := beast.NewFrame(beastFrame, false)
		if err != nil {
			continue // Skip bad beast frames
		}

		// Decode the Beast frame first to ensure Mode S frame is initialized
		err = frame.Decode()
		if err != nil {
			if errors.Is(err, mode_s.ErrNoOp) {
				continue // Skip NoOp frames
			}
			// Some other decode error - skip
			if testing.Verbose() {
				t.Logf("Beast frame decode error: %v", err)
			}
			continue
		}

		// Only process Mode S frames
		modeS := frame.AvrFrame()
		if modeS == nil {
			continue
		}

		stats.totalFrames++

		// The frame should already be decoded, but check for errors
		err = modeS.Decode()
		if err != nil {
			if errors.Is(err, mode_s.ErrNoOp) {
				// Skip NoOp frames
				continue
			}

			// Track what kind of error
			if errors.Is(err, mode_s.ErrInvalidChecksum) {
				stats.invalidCRC++
				if testing.Verbose() {
					t.Logf("CRC Error DF%d: %v", modeS.DownLinkType(), err)
				}
			} else if strings.Contains(err.Error(), "do not know how to CRC") {
				stats.unknownDF++
			} else {
				stats.parseErrors++
				if testing.Verbose() {
					t.Logf("Parse error: %v", err)
				}
			}
		} else {
			stats.validFrames++

			// Count by downlink format
			df := modeS.DownLinkType()
			switch df {
			case 0:
				stats.df0++
			case 4:
				stats.df4++
			case 5:
				stats.df5++
			case 11:
				stats.df11++
			case 16:
				stats.df16++
			case 17:
				stats.df17++
			case 18:
				stats.df18++
			case 20:
				stats.df20++
			case 21:
				stats.df21++
			case 24:
				stats.df24++
			}
		}
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("Scanner error: %v", err)
	}

	// Report results
	t.Logf("\n=== Real-World CRC Validation Results ===")
	t.Logf("Total frames processed: %d", stats.totalFrames)
	t.Logf("Valid frames:           %d (%.2f%%)", stats.validFrames, 100.0*float64(stats.validFrames)/float64(stats.totalFrames))
	t.Logf("Invalid CRC:            %d (%.2f%%)", stats.invalidCRC, 100.0*float64(stats.invalidCRC)/float64(stats.totalFrames))
	t.Logf("Unknown DF:             %d", stats.unknownDF)
	t.Logf("Parse errors:           %d", stats.parseErrors)
	t.Logf("\nBreakdown by Downlink Format:")
	t.Logf("  DF0  (AP): %d", stats.df0)
	t.Logf("  DF4  (AP): %d", stats.df4)
	t.Logf("  DF5  (AP): %d", stats.df5)
	t.Logf("  DF11 (PI): %d", stats.df11)
	t.Logf("  DF16 (AP): %d", stats.df16)
	t.Logf("  DF17 (PI): %d", stats.df17)
	t.Logf("  DF18 (PI): %d", stats.df18)
	t.Logf("  DF20 (AP): %d", stats.df20)
	t.Logf("  DF21 (AP): %d", stats.df21)
	t.Logf("  DF24 (AP): %d", stats.df24)

	// Assertions
	if stats.totalFrames == 0 {
		t.Fatal("No frames were processed!")
	}

	// We expect most frames to be valid (real-world data should have good CRC)
	validPercent := 100.0 * float64(stats.validFrames) / float64(stats.totalFrames)
	if validPercent < 95.0 {
		t.Errorf("Warning: Only %.2f%% of frames passed CRC validation. Expected >95%%", validPercent)
		t.Errorf("This might indicate a problem with CRC implementation or corrupt data file.")
	}

	// We should see a mix of DF types
	if stats.df17 == 0 && stats.df11 == 0 {
		t.Error("No DF11 or DF17 frames found - expected ADS-B data")
	}

	t.Logf("\n✓ CRC validation appears to be working correctly with real-world data")
}

// TestCRCRejectionRate tests that corrupted frames are properly rejected
func TestCRCRejectionWithCorruptedRealWorldData(t *testing.T) {
	// Read the beast file
	data, err := os.ReadFile("cmd/stress/full-feed.beast")
	if err != nil {
		t.Skip("Skipping real-world test: beast file not found")
		return
	}

	validCount := 0
	rejectedCount := 0
	piFieldFrames := 0

	// Use the Beast scanner to properly handle escapes
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Split(producer.ScanBeast)

	for scanner.Scan() && piFieldFrames < 1000 {
		beastFrame := scanner.Bytes()

		// Parse Beast frame
		frame, err := beast.NewFrame(beastFrame, false)
		if err != nil {
			continue // Skip bad beast frames
		}

		// Decode the Beast frame
		err = frame.Decode()
		if err != nil {
			continue // Skip frames that don't decode
		}

		// Get Mode S frame
		modeS := frame.AvrFrame()
		if modeS == nil {
			continue
		}

		// Check if it's a PI field frame (DF 11, 17, 18)
		df := modeS.DownLinkType()
		if df != 11 && df != 17 && df != 18 {
			continue
		}

		// Test original frame - get the raw message bytes
		message := modeS.Raw()
		if len(message) == 0 {
			continue
		}

		// Create a fresh frame from the bytes
		testFrame := mode_s.NewFrameFromBytes(0, message, time.Now())
		originalErr := testFrame.Decode()

		if originalErr == nil {
			piFieldFrames++
			validCount++

			// Corrupt it and verify it's rejected
			corrupted := mode_s.CorruptCRC(message)
			frameCorrupted := mode_s.NewFrameFromBytes(0, corrupted, time.Now())
			corruptedErr := frameCorrupted.Decode()

			if corruptedErr != nil {
				rejectedCount++
			} else {
				t.Logf("Warning: Corrupted frame was accepted! DF%d: %X", df, corrupted)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("Scanner error: %v", err)
	}

	if validCount == 0 {
		t.Skip("No valid PI field frames found in test data")
	}

	rejectionRate := 100.0 * float64(rejectedCount) / float64(validCount)

	t.Logf("\n=== CRC Corruption Detection ===")
	t.Logf("Valid frames tested:    %d", validCount)
	t.Logf("Corrupted & rejected:   %d", rejectedCount)
	t.Logf("Rejection rate:         %.2f%%", rejectionRate)

	if rejectionRate < 99.0 {
		t.Errorf("CRC rejection rate too low: %.2f%% (expected >99%%)", rejectionRate)
		t.Error("Corrupted frames should be reliably detected!")
	} else {
		t.Logf("\n✓ CRC corruption detection working correctly")
	}
}
