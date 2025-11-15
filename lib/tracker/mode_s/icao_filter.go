package mode_s

import "sync"

// ICAOFilterFunc is a function that checks if an ICAO is known/tracked
type ICAOFilterFunc func(icao uint32) bool

// ICAOMessageCountFunc returns the number of messages received from an ICAO
type ICAOMessageCountFunc func(icao uint32) int

var (
	icaoFilterMu        sync.RWMutex
	icaoFilterFunc      ICAOFilterFunc
	icaoMsgCountFunc    ICAOMessageCountFunc
	frameCountThreshold = 3 // Minimum frames required for DF0/4/5/16 validation
)

// RegisterICAOFilter registers a function to check if an ICAO is known.
// This is used by the tracker to provide ICAO filtering for surveillance replies.
func RegisterICAOFilter(f ICAOFilterFunc) {
	icaoFilterMu.Lock()
	defer icaoFilterMu.Unlock()
	icaoFilterFunc = f
}

// RegisterICAOMessageCounter registers a function to get message counts for ICAOs.
// Used for threshold-based filtering of surveillance replies.
func RegisterICAOMessageCounter(f ICAOMessageCountFunc) {
	icaoFilterMu.Lock()
	defer icaoFilterMu.Unlock()
	icaoMsgCountFunc = f
}

// SetFrameCountThreshold sets the minimum number of frames required for
// accepting DF0/4/5/16 surveillance replies from an ICAO
func SetFrameCountThreshold(threshold int) {
	icaoFilterMu.Lock()
	defer icaoFilterMu.Unlock()
	frameCountThreshold = threshold
}

// testICAO checks if an ICAO is in the registered filter (for DF20/21)
func testICAO(icao uint32) bool {
	icaoFilterMu.RLock()
	defer icaoFilterMu.RUnlock()

	if icaoFilterFunc == nil {
		return false
	}

	return icaoFilterFunc(icao)
}

// testICAOFrameCount checks if an ICAO has enough frames for DF0/4/5/16 validation
func testICAOFrameCount(icao uint32) bool {
	icaoFilterMu.RLock()
	defer icaoFilterMu.RUnlock()

	// Check if we have a message counter registered
	if icaoMsgCountFunc == nil {
		return false
	}

	// Get message count for this ICAO
	count := icaoMsgCountFunc(icao)

	// Accept if count meets threshold
	return count >= frameCountThreshold
}
