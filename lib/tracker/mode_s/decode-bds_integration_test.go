package mode_s_test

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"testing"

	"plane.watch/lib/producer"
	"plane.watch/lib/tracker/beast"
	"plane.watch/lib/tracker/mode_s"
)

// TestBDSDecodingRealWorld validates BDS decoder improvements with real captured data
func TestBDSDecodingRealWorld(t *testing.T) {
	// Read the Beast-encoded file with DF20/21 Comm-B messages
	data, err := os.ReadFile("../../../lib/producer/testdata/df20_df21.sample")
	if err != nil {
		t.Skipf("Skipping real-world test: DF20/21 sample file not found: %v", err)
		return
	}

	stats := &bdsStats{
		bdsTypes:     make(map[string]int),
		dfTypes:      make(map[int]int),
		drUMFiltered: 0,
		errorCounts:  make(map[string]int),
	}

	// Use the Beast scanner to properly handle escapes
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Split(producer.ScanBeast)

	totalFrames := 0
	for scanner.Scan() {
		beastFrame := scanner.Bytes()

		// Parse Beast frame
		frame, err := beast.NewFrame(beastFrame, false)
		if err != nil {
			continue // Skip bad beast frames
		}

		// Decode the Beast frame first
		err = frame.Decode()
		if err != nil {
			if errors.Is(err, mode_s.ErrNoOp) {
				continue // Skip NoOp frames
			}
			continue
		}

		// Only process Mode S frames
		modeS := frame.AvrFrame()
		if modeS == nil {
			continue
		}

		totalFrames++

		// Track DF types
		df := int(modeS.DownLinkType())
		stats.dfTypes[df]++

		// Check for decode errors
		err = modeS.Decode()
		if err != nil {
			// Track errors for DF20/21
			if df == 20 || df == 21 {
				errMsg := err.Error()
				stats.errorCounts[errMsg]++
			}
			continue
		}

		// Focus on Comm-B frames (DF20, DF21)
		if df == 20 || df == 21 {
			stats.commBFrames++

			// Track BDS types decoded
			bdsType := modeS.BdsMessageType()
			if bdsType != "" && bdsType != "0.0" {
				stats.bdsTypes[bdsType]++
				stats.bdsDecoded++
			} else if bdsType == "0.0" {
				stats.emptyResponses++
			} else {
				// Empty string means either DR/UM filtered or ambiguous
				stats.bdsAmbiguous++
			}
		}
	}

	// Report statistics
	t.Logf("\n=== BDS Decoder Real-World Performance ===")
	t.Logf("Total frames parsed:       %d", totalFrames)
	t.Logf("")

	t.Logf("DF Types Distribution:")
	for df := 0; df <= 24; df++ {
		if count, exists := stats.dfTypes[df]; exists {
			t.Logf("  DF%02d: %6d frames", df, count)
		}
	}
	t.Logf("")

	t.Logf("Comm-B Analysis (DF20/21):")
	t.Logf("  Total Comm-B frames:     %d", stats.commBFrames)
	t.Logf("  DR/UM filtered:          %d (%.1f%%)", stats.drUMFiltered,
		100.0*float64(stats.drUMFiltered)/float64(maxInt(stats.commBFrames, 1)))
	t.Logf("  BDS decoded:             %d (%.1f%%)", stats.bdsDecoded,
		100.0*float64(stats.bdsDecoded)/float64(maxInt(stats.commBFrames, 1)))
	t.Logf("  Empty responses:         %d (%.1f%%)", stats.emptyResponses,
		100.0*float64(stats.emptyResponses)/float64(maxInt(stats.commBFrames, 1)))
	t.Logf("  Ambiguous:               %d (%.1f%%)", stats.bdsAmbiguous,
		100.0*float64(stats.bdsAmbiguous)/float64(maxInt(stats.commBFrames, 1)))
	t.Logf("")

	// Show DF20/21 decode errors
	if len(stats.errorCounts) > 0 {
		t.Logf("DF20/21 Decode Errors:")
		for errMsg, count := range stats.errorCounts {
			t.Logf("  %s: %d", errMsg, count)
		}
		t.Logf("")
	}

	if len(stats.bdsTypes) > 0 {
		t.Logf("BDS Types Decoded:")
		// Sort by BDS type
		bdsOrder := []string{"1.0", "1.7", "2.0", "3.0", "4.0", "5.0", "6.0"}
		for _, bdsType := range bdsOrder {
			if count, exists := stats.bdsTypes[bdsType]; exists {
				percentage := 100.0 * float64(count) / float64(stats.bdsDecoded)
				t.Logf("  BDS %s: %6d (%.1f%%)", bdsType, count, percentage)
			}
		}
		// Check for any other types not in the expected list
		for bdsType, count := range stats.bdsTypes {
			found := false
			for _, expected := range bdsOrder {
				if bdsType == expected {
					found = true
					break
				}
			}
			if !found {
				percentage := 100.0 * float64(count) / float64(stats.bdsDecoded)
				t.Logf("  BDS %s: %6d (%.1f%%) [UNEXPECTED]", bdsType, count, percentage)
			}
		}
	}

	// Validate we're actually decoding Comm-B frames
	if stats.commBFrames > 0 && stats.bdsDecoded == 0 && stats.emptyResponses == 0 {
		t.Errorf("Found %d Comm-B frames but decoded 0 BDS messages - decoder may not be working",
			stats.commBFrames)
	}
}

// TestBDSDecoderCoverage verifies all decoder types are being tested
func TestBDSDecoderCoverage(t *testing.T) {
	data, err := os.ReadFile("../../../lib/producer/testdata/df20_df21.sample")
	if err != nil {
		t.Skipf("Skipping real-world test: DF20/21 sample file not found: %v", err)
		return
	}

	foundTypes := make(map[string]bool)
	expectedTypes := []string{"1.0", "1.7", "2.0", "3.0", "4.0", "5.0", "6.0"}

	// Use the Beast scanner to properly handle escapes
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Split(producer.ScanBeast)

	for scanner.Scan() && len(foundTypes) < len(expectedTypes) {
		beastFrame := scanner.Bytes()

		// Parse Beast frame
		frame, err := beast.NewFrame(beastFrame, false)
		if err != nil {
			continue
		}

		// Decode the Beast frame
		err = frame.Decode()
		if err != nil {
			if errors.Is(err, mode_s.ErrNoOp) {
				continue
			}
			continue
		}

		// Only process Mode S frames
		modeS := frame.AvrFrame()
		if modeS == nil {
			continue
		}

		// Check for decode errors
		err = modeS.Decode()
		if err != nil {
			continue
		}

		df := int(modeS.DownLinkType())
		if df == 20 || df == 21 {
			bdsType := modeS.BdsMessageType()
			if bdsType != "" && bdsType != "0.0" {
				foundTypes[bdsType] = true
			}
		}
	}

	t.Logf("\n=== BDS Decoder Coverage ===")
	t.Logf("Expected BDS types: %v", expectedTypes)
	t.Logf("Found in test data:")
	for _, bdsType := range expectedTypes {
		if foundTypes[bdsType] {
			t.Logf("  ✓ BDS %s", bdsType)
		} else {
			t.Logf("  ✗ BDS %s (not found in test data)", bdsType)
		}
	}
}

type bdsStats struct {
	commBFrames    int
	drUMFiltered   int
	bdsDecoded     int
	emptyResponses int
	bdsAmbiguous   int
	bdsTypes       map[string]int
	dfTypes        map[int]int
	errorCounts    map[string]int
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
