package main

import (
	"testing"

	"plane.watch/lib/export"
	"plane.watch/lib/ws_protocol"
)

func TestTileMatchesSubs(t *testing.T) {
	tests := []struct {
		name         string
		subs         map[string]struct{}
		tileLocation string
		highLow      string
		want         bool
	}{
		{
			name:         "exact tile match high",
			subs:         map[string]struct{}{"tile35_high": {}},
			tileLocation: "tile35",
			highLow:      "_high",
			want:         true,
		},
		{
			name:         "exact tile match low",
			subs:         map[string]struct{}{"tile35_low": {}},
			tileLocation: "tile35",
			highLow:      "_low",
			want:         true,
		},
		{
			name:         "wrong suffix does not match",
			subs:         map[string]struct{}{"tile35_low": {}},
			tileLocation: "tile35",
			highLow:      "_high",
			want:         false,
		},
		{
			name:         "all_high matches any tile with high suffix",
			subs:         map[string]struct{}{"all_high": {}},
			tileLocation: "tile99",
			highLow:      "_high",
			want:         true,
		},
		{
			name:         "all_low matches any tile with low suffix",
			subs:         map[string]struct{}{"all_low": {}},
			tileLocation: "tile99",
			highLow:      "_low",
			want:         true,
		},
		{
			name:         "all_high does not match low suffix",
			subs:         map[string]struct{}{"all_high": {}},
			tileLocation: "tile99",
			highLow:      "_low",
			want:         false,
		},
		{
			name:         "no subscriptions",
			subs:         map[string]struct{}{},
			tileLocation: "tile35",
			highLow:      "_high",
			want:         false,
		},
		{
			name:         "different tile does not match",
			subs:         map[string]struct{}{"tile36_high": {}},
			tileLocation: "tile35",
			highLow:      "_high",
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tileMatchesSubs(tt.subs, tt.tileLocation, tt.highLow)
			if got != tt.want {
				t.Errorf("tileMatchesSubs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLocationMatchesSubs(t *testing.T) {
	tests := []struct {
		name string
		subs map[string]struct{}
		loc  *export.PlaneLocation
		want bool
	}{
		{
			name: "matches via high suffix",
			subs: map[string]struct{}{"tile35_high": {}},
			loc:  &export.PlaneLocation{TileLocation: "tile35"},
			want: true,
		},
		{
			name: "matches via low suffix",
			subs: map[string]struct{}{"tile35_low": {}},
			loc:  &export.PlaneLocation{TileLocation: "tile35"},
			want: true,
		},
		{
			name: "matches when both suffixes subscribed",
			subs: map[string]struct{}{"tile35_high": {}, "tile35_low": {}},
			loc:  &export.PlaneLocation{TileLocation: "tile35"},
			want: true,
		},
		{
			name: "no match for different tile",
			subs: map[string]struct{}{"tile36_high": {}},
			loc:  &export.PlaneLocation{TileLocation: "tile35"},
			want: false,
		},
		{
			name: "matches via all_low wildcard",
			subs: map[string]struct{}{ws_protocol.GridTileAllLow: {}},
			loc:  &export.PlaneLocation{TileLocation: "tile35"},
			want: true,
		},
		{
			name: "matches via all_high wildcard",
			subs: map[string]struct{}{ws_protocol.GridTileAllHigh: {}},
			loc:  &export.PlaneLocation{TileLocation: "tile35"},
			want: true,
		},
		{
			name: "no match with empty subs",
			subs: map[string]struct{}{},
			loc:  &export.PlaneLocation{TileLocation: "tile35"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := locationMatchesSubs(tt.subs, tt.loc)
			if got != tt.want {
				t.Errorf("locationMatchesSubs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetSubscribedTilesValidation(t *testing.T) {
	// Build a grid matching the real server's initialization:
	// grid contains suffixed tile keys like "tile35_low", "tile35_high", "all_low", "all_high"
	grid := map[string]bool{
		"all_low":     true,
		"all_high":    true,
		"tile35_low":  true,
		"tile35_high": true,
		"tile36_low":  true,
		"tile36_high": true,
	}

	tests := []struct {
		name           string
		existingSubs   map[string]struct{}
		requestedTiles []string
		wantSubs       map[string]struct{} // expected subs after request
		wantError      bool
	}{
		{
			name:           "valid tiles replace existing subs",
			existingSubs:   map[string]struct{}{"tile35_high": {}},
			requestedTiles: []string{"tile36_high"},
			wantSubs:       map[string]struct{}{"tile36_high": {}},
			wantError:      false,
		},
		{
			name:           "invalid tile rejects entire request",
			existingSubs:   map[string]struct{}{"tile35_high": {}},
			requestedTiles: []string{"tile36_high", "bogus_tile"},
			wantSubs:       map[string]struct{}{"tile35_high": {}}, // unchanged
			wantError:      true,
		},
		{
			name:           "all invalid tiles rejected",
			existingSubs:   map[string]struct{}{"tile35_high": {}},
			requestedTiles: []string{"bogus"},
			wantSubs:       map[string]struct{}{"tile35_high": {}}, // unchanged
			wantError:      true,
		},
		{
			name:           "empty list clears subs",
			existingSubs:   map[string]struct{}{"tile35_high": {}},
			requestedTiles: []string{},
			wantSubs:       map[string]struct{}{},
			wantError:      false,
		},
		{
			name:           "multiple valid tiles applied",
			existingSubs:   map[string]struct{}{},
			requestedTiles: []string{"tile35_high", "tile36_low"},
			wantSubs:       map[string]struct{}{"tile35_high": {}, "tile36_low": {}},
			wantError:      false,
		},
		{
			name:           "bare tile name is invalid",
			existingSubs:   map[string]struct{}{},
			requestedTiles: []string{"tile35"},
			wantSubs:       map[string]struct{}{}, // unchanged
			wantError:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Copy existing subs
			subs := make(map[string]struct{})
			for k, v := range tt.existingSubs {
				subs[k] = v
			}

			// Simulate the validation + application logic from the command loop
			allValid := true
			var invalidTile string
			for _, tile := range tt.requestedTiles {
				if _, ok := grid[tile]; !ok {
					allValid = false
					invalidTile = tile
					break
				}
			}

			if !allValid {
				if !tt.wantError {
					t.Errorf("expected success but got invalid tile: %s", invalidTile)
				}
				// Verify subs unchanged on rejection
				if len(subs) != len(tt.wantSubs) {
					t.Errorf("subs modified on rejection: got %v, want %v", subs, tt.wantSubs)
				}
				for k := range tt.wantSubs {
					if _, ok := subs[k]; !ok {
						t.Errorf("missing expected sub %q after rejection", k)
					}
				}
				return
			}

			if tt.wantError {
				t.Error("expected error but validation passed")
				return
			}

			// Apply (mirrors the command loop)
			clear(subs)
			for _, tile := range tt.requestedTiles {
				subs[tile] = struct{}{}
			}

			if len(subs) != len(tt.wantSubs) {
				t.Errorf("subs = %v, want %v", subs, tt.wantSubs)
			}
			for k := range tt.wantSubs {
				if _, ok := subs[k]; !ok {
					t.Errorf("missing expected sub %q", k)
				}
			}
		})
	}
}

func TestProtocolResponseTypes(t *testing.T) {
	// Verify the new response type constant exists and has the right value
	if ws_protocol.ResponseTypeInitialSyncComplete != "initial-sync-complete" {
		t.Errorf("ResponseTypeInitialSyncComplete = %q, want %q",
			ws_protocol.ResponseTypeInitialSyncComplete, "initial-sync-complete")
	}
}

func TestRequestIdOnResponse(t *testing.T) {
	// Verify requestId is correctly carried through WsResponse
	reqId := "test-req-123"
	count := 42

	resp := ws_protocol.WsResponse{
		Type:          ws_protocol.ResponseTypeInitialSyncComplete,
		Tiles:         []string{"tile35_high"},
		AircraftCount: &count,
		RequestId:     reqId,
	}

	if resp.RequestId != reqId {
		t.Errorf("RequestId = %q, want %q", resp.RequestId, reqId)
	}
	if resp.AircraftCount == nil || *resp.AircraftCount != 42 {
		t.Errorf("AircraftCount = %v, want 42", resp.AircraftCount)
	}
}

func TestRequestIdOmittedWhenEmpty(t *testing.T) {
	// Old client compatibility: omitted requestId should result in empty string
	resp := ws_protocol.WsResponse{
		Type:  ws_protocol.ResponseTypeAckSub,
		Tiles: []string{"tile35_high"},
	}

	if resp.RequestId != "" {
		t.Errorf("RequestId should be empty when not set, got %q", resp.RequestId)
	}
}

func TestZeroAircraftSnapshot(t *testing.T) {
	// Verify that with valid subs but no matching aircraft,
	// locationMatchesSubs returns false for non-matching locations
	subs := map[string]struct{}{"tile99_high": {}}

	// Aircraft in a different tile
	loc := &export.PlaneLocation{TileLocation: "tile35"}
	if locationMatchesSubs(subs, loc) {
		t.Error("should not match aircraft in different tile")
	}

	// No aircraft at all means the Range() loop finds nothing,
	// and initial-sync-complete should still be sent with aircraftCount: 0.
	// That behavior is tested at the integration level; here we verify
	// the AircraftCount field can represent zero.
	count := 0
	resp := ws_protocol.WsResponse{
		Type:          ws_protocol.ResponseTypeInitialSyncComplete,
		Tiles:         []string{"tile99_high"},
		AircraftCount: &count,
	}
	if resp.AircraftCount == nil || *resp.AircraftCount != 0 {
		t.Errorf("AircraftCount should be 0, got %v", resp.AircraftCount)
	}
}
