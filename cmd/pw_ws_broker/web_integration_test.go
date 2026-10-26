package main

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"plane.watch/lib/export"
	"plane.watch/lib/ws_protocol"
)

// startTestBroker creates a minimal broker with no NATS and returns the WebSocket URL.
func startTestBroker(t *testing.T) (wsURL string, clients *ClientList, shutdown func()) {
	t.Helper()

	// Find a free port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	bw := &PwWsBrokerWeb{
		Addr:             addr,
		sendTickDuration: 500 * time.Millisecond,
	}

	if err := bw.configureWeb(); err != nil {
		t.Fatalf("configureWeb: %v", err)
	}

	exitChan := make(chan bool, 1)
	go bw.listenAndServe(exitChan)

	// Wait for server to be ready
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	return "ws://" + addr + "/planes", bw.clients, func() {
		bw.httpServer.Close()
		<-exitChan
		bw.clients.globalList.Stop()
	}
}

// dialPlanes connects a WebSocket client speaking the "planes" subprotocol.
func dialPlanes(t *testing.T, wsURL string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{ws_protocol.WsProtocolPlanes},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.SetReadLimit(1_048_576)
	return conn
}

// sendRequest sends a WsRequest over the websocket.
func sendRequest(t *testing.T, conn *websocket.Conn, rq ws_protocol.WsRequest) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, conn, rq); err != nil {
		t.Fatalf("write request: %v", err)
	}
}

// readResponse reads a WsResponse from the websocket with a timeout.
func readResponse(t *testing.T, conn *websocket.Conn, timeout time.Duration) ws_protocol.WsResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var resp ws_protocol.WsResponse
	if err := wsjson.Read(ctx, conn, &resp); err != nil {
		t.Fatalf("read response: %v", err)
	}
	return resp
}

// readResponseMaybe reads a WsResponse, returning ok=false on timeout instead of failing.
func readResponseMaybe(t *testing.T, conn *websocket.Conn, timeout time.Duration) (ws_protocol.WsResponse, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var resp ws_protocol.WsResponse
	if err := wsjson.Read(ctx, conn, &resp); err != nil {
		return resp, false
	}
	return resp, true
}

func ptrString(s string) *string { return &s }

// TestIntegration_SetSubTileList_PopulatedTile verifies:
// ack-sub → plane-location-list (immediate snapshot) → initial-sync-complete
func TestIntegration_SetSubTileList_PopulatedTile(t *testing.T) {
	wsURL, clients, shutdown := startTestBroker(t)
	defer shutdown()

	// Pre-populate a plane in the global list
	loc := &export.PlaneLocation{
		Icao:         "ABC123",
		TileLocation: "tile35",
		Lat:          40.0,
		Lon:          -75.0,
		CallSign:     ptrString("TEST1"),
		HasLocation:  true,
		LastMsg:      time.Now(),
	}
	clients.globalList.Store("ABC123", loc)

	conn := dialPlanes(t, wsURL)
	defer conn.CloseNow()

	// Send set-sub-tile-list
	sendRequest(t, conn, ws_protocol.WsRequest{
		Type:      ws_protocol.RequestTypeSetSubscribedTiles,
		GridTile:  "tile35_high",
		RequestId: "req-pop-1",
	})

	// 1. Expect ack-sub
	resp := readResponse(t, conn, 2*time.Second)
	if resp.Type != ws_protocol.ResponseTypeAckSub {
		t.Fatalf("expected ack-sub, got %s", resp.Type)
	}
	if resp.RequestId != "req-pop-1" {
		t.Errorf("ack-sub requestId = %q, want %q", resp.RequestId, "req-pop-1")
	}
	if len(resp.Tiles) != 1 || resp.Tiles[0] != "tile35_high" {
		t.Errorf("ack-sub tiles = %v, want [tile35_high]", resp.Tiles)
	}

	// 2. Expect immediate snapshot plane-location-list
	resp = readResponse(t, conn, 2*time.Second)
	if resp.Type != ws_protocol.ResponseTypePlaneLocations {
		t.Fatalf("expected plane-location-list, got %s", resp.Type)
	}
	if resp.RequestId != "req-pop-1" {
		t.Errorf("snapshot requestId = %q, want %q", resp.RequestId, "req-pop-1")
	}
	if len(resp.Locations) != 1 {
		t.Fatalf("snapshot locations count = %d, want 1", len(resp.Locations))
	}
	if resp.Locations[0].Icao != "ABC123" {
		t.Errorf("snapshot icao = %q, want ABC123", resp.Locations[0].Icao)
	}

	// 3. Expect initial-sync-complete
	resp = readResponse(t, conn, 2*time.Second)
	if resp.Type != ws_protocol.ResponseTypeInitialSyncComplete {
		t.Fatalf("expected initial-sync-complete, got %s", resp.Type)
	}
	if resp.RequestId != "req-pop-1" {
		t.Errorf("sync-complete requestId = %q, want %q", resp.RequestId, "req-pop-1")
	}
	if resp.AircraftCount == nil || *resp.AircraftCount != 1 {
		t.Errorf("sync-complete aircraftCount = %v, want 1", resp.AircraftCount)
	}
}

// TestIntegration_SetSubTileList_EmptyArea verifies:
// ack-sub → initial-sync-complete (no snapshot list when zero aircraft)
func TestIntegration_SetSubTileList_EmptyArea(t *testing.T) {
	wsURL, _, shutdown := startTestBroker(t)
	defer shutdown()

	conn := dialPlanes(t, wsURL)
	defer conn.CloseNow()

	sendRequest(t, conn, ws_protocol.WsRequest{
		Type:      ws_protocol.RequestTypeSetSubscribedTiles,
		GridTile:  "tile74_high",
		RequestId: "req-empty-1",
	})

	// 1. Expect ack-sub
	resp := readResponse(t, conn, 2*time.Second)
	if resp.Type != ws_protocol.ResponseTypeAckSub {
		t.Fatalf("expected ack-sub, got %s", resp.Type)
	}
	if resp.RequestId != "req-empty-1" {
		t.Errorf("requestId = %q, want %q", resp.RequestId, "req-empty-1")
	}

	// 2. Expect initial-sync-complete (no snapshot list)
	resp = readResponse(t, conn, 2*time.Second)
	if resp.Type != ws_protocol.ResponseTypeInitialSyncComplete {
		t.Fatalf("expected initial-sync-complete, got %s (snapshot should be skipped for zero aircraft)", resp.Type)
	}
	if resp.AircraftCount == nil || *resp.AircraftCount != 0 {
		t.Errorf("aircraftCount = %v, want 0", resp.AircraftCount)
	}
}

// TestIntegration_InvalidTile_PreservesExistingSubs verifies:
// invalid tile → error, existing subscriptions unchanged
func TestIntegration_InvalidTile_PreservesExistingSubs(t *testing.T) {
	wsURL, _, shutdown := startTestBroker(t)
	defer shutdown()

	conn := dialPlanes(t, wsURL)
	defer conn.CloseNow()

	// First, subscribe to a valid tile via set-sub-tile-list
	sendRequest(t, conn, ws_protocol.WsRequest{
		Type:     ws_protocol.RequestTypeSetSubscribedTiles,
		GridTile: "tile35_high",
	})

	// Drain ack-sub + initial-sync-complete from the valid set
	resp := readResponse(t, conn, 2*time.Second)
	if resp.Type != ws_protocol.ResponseTypeAckSub {
		t.Fatalf("expected ack-sub for initial set, got %s", resp.Type)
	}
	resp = readResponse(t, conn, 2*time.Second)
	if resp.Type != ws_protocol.ResponseTypeInitialSyncComplete {
		t.Fatalf("expected initial-sync-complete for initial set, got %s", resp.Type)
	}

	// Now send set-sub-tile-list with an invalid tile
	sendRequest(t, conn, ws_protocol.WsRequest{
		Type:      ws_protocol.RequestTypeSetSubscribedTiles,
		GridTile:  "tile35_high,bogus_tile",
		RequestId: "req-invalid",
	})

	// Expect error
	resp = readResponse(t, conn, 2*time.Second)
	if resp.Type != ws_protocol.ResponseTypeError {
		t.Fatalf("expected error, got %s", resp.Type)
	}

	// Verify subs are preserved by requesting sub-list
	sendRequest(t, conn, ws_protocol.WsRequest{
		Type: ws_protocol.RequestTypeSubscribeList,
	})

	resp = readResponse(t, conn, 2*time.Second)
	if resp.Type != ws_protocol.ResponseTypeSubTiles {
		t.Fatalf("expected sub-list, got %s", resp.Type)
	}
	if len(resp.Tiles) != 1 || resp.Tiles[0] != "tile35_high" {
		t.Errorf("sub-list after invalid request = %v, want [tile35_high]", resp.Tiles)
	}
}

// TestIntegration_ClearSubscriptions verifies:
// empty gridTile clears all subscriptions (strings.Split("",",") → [""] must be normalized)
func TestIntegration_ClearSubscriptions(t *testing.T) {
	wsURL, _, shutdown := startTestBroker(t)
	defer shutdown()

	conn := dialPlanes(t, wsURL)
	defer conn.CloseNow()

	// First, subscribe to a valid tile
	sendRequest(t, conn, ws_protocol.WsRequest{
		Type:     ws_protocol.RequestTypeSetSubscribedTiles,
		GridTile: "tile35_high",
	})

	// Drain ack-sub + initial-sync-complete
	resp := readResponse(t, conn, 2*time.Second)
	if resp.Type != ws_protocol.ResponseTypeAckSub {
		t.Fatalf("expected ack-sub, got %s", resp.Type)
	}
	resp = readResponse(t, conn, 2*time.Second)
	if resp.Type != ws_protocol.ResponseTypeInitialSyncComplete {
		t.Fatalf("expected initial-sync-complete, got %s", resp.Type)
	}

	// Now clear all subscriptions with empty gridTile
	sendRequest(t, conn, ws_protocol.WsRequest{
		Type:     ws_protocol.RequestTypeSetSubscribedTiles,
		GridTile: "",
	})

	// Expect ack-sub with empty tile list
	resp = readResponse(t, conn, 2*time.Second)
	if resp.Type != ws_protocol.ResponseTypeAckSub {
		t.Fatalf("expected ack-sub for clear, got %s: %s", resp.Type, resp.Message)
	}
	if len(resp.Tiles) != 0 {
		t.Errorf("tiles after clear = %v, want empty", resp.Tiles)
	}

	// Expect initial-sync-complete with zero aircraft
	resp = readResponse(t, conn, 2*time.Second)
	if resp.Type != ws_protocol.ResponseTypeInitialSyncComplete {
		t.Fatalf("expected initial-sync-complete for clear, got %s", resp.Type)
	}
	if resp.AircraftCount == nil || *resp.AircraftCount != 0 {
		t.Errorf("aircraftCount after clear = %v, want 0", resp.AircraftCount)
	}

	// Verify sub-list is empty
	sendRequest(t, conn, ws_protocol.WsRequest{
		Type: ws_protocol.RequestTypeSubscribeList,
	})
	resp = readResponse(t, conn, 2*time.Second)
	if resp.Type != ws_protocol.ResponseTypeSubTiles {
		t.Fatalf("expected sub-list, got %s", resp.Type)
	}
	if len(resp.Tiles) != 0 {
		t.Errorf("sub-list after clear = %v, want empty", resp.Tiles)
	}
}

// TestIntegration_OmittedRequestId verifies backward compatibility:
// client omitting requestId still gets proper lifecycle responses
func TestIntegration_OmittedRequestId(t *testing.T) {
	wsURL, _, shutdown := startTestBroker(t)
	defer shutdown()

	conn := dialPlanes(t, wsURL)
	defer conn.CloseNow()

	// Send without requestId
	sendRequest(t, conn, ws_protocol.WsRequest{
		Type:     ws_protocol.RequestTypeSetSubscribedTiles,
		GridTile: "tile35_high",
	})

	// 1. ack-sub with empty requestId
	resp := readResponse(t, conn, 2*time.Second)
	if resp.Type != ws_protocol.ResponseTypeAckSub {
		t.Fatalf("expected ack-sub, got %s", resp.Type)
	}
	if resp.RequestId != "" {
		t.Errorf("requestId should be empty, got %q", resp.RequestId)
	}

	// 2. initial-sync-complete with empty requestId
	resp = readResponse(t, conn, 2*time.Second)
	if resp.Type != ws_protocol.ResponseTypeInitialSyncComplete {
		t.Fatalf("expected initial-sync-complete, got %s", resp.Type)
	}
	if resp.RequestId != "" {
		t.Errorf("requestId should be empty, got %q", resp.RequestId)
	}
}

// TestIntegration_LiveUpdates_SuffixedMatching verifies:
// after snapshot, live updates via SendLocationUpdate are correctly routed
// using the same suffixed matching as snapshot.
func TestIntegration_LiveUpdates_SuffixedMatching(t *testing.T) {
	wsURL, clients, shutdown := startTestBroker(t)
	defer shutdown()

	conn := dialPlanes(t, wsURL)
	defer conn.CloseNow()

	// Subscribe to tile35_high
	sendRequest(t, conn, ws_protocol.WsRequest{
		Type:     ws_protocol.RequestTypeSetSubscribedTiles,
		GridTile: "tile35_high",
	})

	// Drain ack-sub + initial-sync-complete
	resp := readResponse(t, conn, 2*time.Second)
	if resp.Type != ws_protocol.ResponseTypeAckSub {
		t.Fatalf("expected ack-sub, got %s", resp.Type)
	}
	resp = readResponse(t, conn, 2*time.Second)
	if resp.Type != ws_protocol.ResponseTypeInitialSyncComplete {
		t.Fatalf("expected initial-sync-complete, got %s", resp.Type)
	}

	// Inject a live update for tile35_high (should match)
	locHigh := &export.PlaneLocation{
		Icao:         "HIGH01",
		TileLocation: "tile35",
		Lat:          40.0,
		Lon:          -75.0,
		CallSign:     ptrString("HI01"),
		HasLocation:  true,
		LastMsg:      time.Now(),
	}
	clients.SendLocationUpdate("_high", "tile35_high", locHigh)

	// Inject a live update for tile35_low (should NOT match)
	locLow := &export.PlaneLocation{
		Icao:         "LOW01",
		TileLocation: "tile35",
		Lat:          40.0,
		Lon:          -75.0,
		CallSign:     ptrString("LO01"),
		HasLocation:  true,
		LastMsg:      time.Now(),
	}
	clients.SendLocationUpdate("_low", "tile35_low", locLow)

	// Wait for a tick cycle to flush batched updates
	time.Sleep(700 * time.Millisecond)

	// Read the tick-batched response — should contain HIGH01 but not LOW01
	resp = readResponse(t, conn, 2*time.Second)
	if resp.Type != ws_protocol.ResponseTypePlaneLocations {
		t.Fatalf("expected plane-location-list, got %s", resp.Type)
	}

	// requestId should NOT be present on tick-batched live updates
	if resp.RequestId != "" {
		t.Errorf("tick-batched requestId should be empty, got %q", resp.RequestId)
	}

	foundHigh := false
	foundLow := false
	for _, loc := range resp.Locations {
		if loc.Icao == "HIGH01" {
			foundHigh = true
		}
		if loc.Icao == "LOW01" {
			foundLow = true
		}
	}
	if !foundHigh {
		t.Error("expected HIGH01 in live update (tile35_high matches subscription)")
	}
	if foundLow {
		t.Error("LOW01 should not appear (tile35_low does not match tile35_high subscription)")
	}
}
