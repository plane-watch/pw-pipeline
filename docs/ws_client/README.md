# WebSocket Client

## Overview

The `ws_client` package provides a simple WebSocket client for consuming real-time aircraft position updates from the Plane.Watch WebSocket API. It handles connection management, tile-based subscriptions, and delivers aircraft locations via Go channels.

## Why This Package?

### The Real-Time Requirement

**Aircraft tracking needs live updates**:
- Position changes every second
- HTTP polling is inefficient (overhead, latency)
- Need push-based delivery
- WebSocket provides persistent bidirectional connection

**Without WebSocket**:
```go
// Inefficient HTTP polling
for {
    positions := httpGet("/api/positions")
    process(positions)
    time.Sleep(1 * time.Second)  // Stale data, high latency
}
```

**With WebSocket**:
```go
client := ws_client.NewClient("plane.watch:8080")
client.Connect()

for location := range client.LocationUpdates() {
    process(location)  // Real-time, sub-second delivery
}
```

**Benefits**:
- ✓ Sub-second latency (push, not poll)
- ✓ Reduced bandwidth (only updates sent)
- ✓ Persistent connection (no HTTP overhead per update)
- ✓ Server-initiated updates

### Tile-Based Subscriptions

**Why tiles**: Bandwidth optimization
- Don't need global aircraft positions
- Subscribe to regions of interest
- Server filters before sending

**Example**: Monitoring Seattle area
```go
client.Subscribe("tile60")  // Pacific Northwest
// Only receive aircraft in tile60, not worldwide
```

**Bandwidth savings**: 95%+ for regional monitoring

## Basic Usage

### Simple Connection

```go
import "plane.watch/lib/ws_client"

// Create client
client := ws_client.NewClient("plane.watch:8080")

// Connect
err := client.Connect()
if err != nil {
    log.Fatal(err)
}
defer client.Disconnect()

// Subscribe to tile
client.Subscribe("tile10")  // Europe

// Process locations
for location := range client.LocationUpdates() {
    fmt.Printf("Aircraft %s at %.4f,%.4f\n",
        location.Icao,
        *location.Lat,
        *location.Lon)
}
```

### Default Client

**Pre-configured for plane.watch**:
```go
import "plane.watch/lib/ws_client"

// Uses default client (plane.watch)
err := ws_client.Connect()

// Access via DefaultClient
client := ws_client.DefaultClient
for location := range client.LocationUpdates() {
    // Process
}
```

**Why default client**: Convenience for common use case
- Most users connect to plane.watch
- No host configuration needed
- One-liner connection

### Secure vs Insecure

**TLS/SSL control**:
```go
client := ws_client.NewClient("plane.watch:8080")

// Use TLS (default: true)
client.Secure(true)   // wss://plane.watch:8080/planes

// Disable TLS (development/local)
client.Secure(false)  // ws://plane.watch:8080/planes
```

**Production**: Always use Secure(true)
**Development**: Secure(false) for localhost

## Subscriptions

### Subscribing to Tiles

**Single tile**:
```go
err := client.Subscribe("tile60")
if err != nil {
    log.Error(err)
}
// Now receiving aircraft in Pacific Northwest
```

**Multiple tiles**:
```go
tiles := []string{"tile10", "tile11", "tile12"}
for _, tile := range tiles {
    client.Subscribe(tile)
}
// Receiving aircraft across Central Europe
```

**Subscription is cumulative**: Adding tile doesn't remove previous
```go
client.Subscribe("tile10")  // Subscribed to tile10
client.Subscribe("tile11")  // Subscribed to tile10 + tile11
```

### Unsubscribing

**Remove subscription**:
```go
err := client.Unsubscribe("tile10")
// No longer receiving tile10 updates
// Still subscribed to other tiles
```

**Unsubscribe all**: No built-in method
```go
// Pattern: Track subscriptions, unsubscribe each
subscriptions := []string{"tile10", "tile11"}
for _, tile := range subscriptions {
    client.Unsubscribe(tile)
}
```

### List Current Subscriptions

**Get active subscriptions**:
```go
tiles, err := client.SubscribedTileList()
if err != nil {
    log.Error(err)
}

fmt.Printf("Subscribed to: %v\n", tiles)
// Output: Subscribed to: [tile10 tile11 tile60]
```

**Use case**: Verify subscription state, display to user

## Receiving Locations

### Location Channel

**Channel-based delivery**:
```go
locationChan := client.LocationUpdates()

for location := range locationChan {
    // Process PlaneLocation
    if location.Lat != nil && location.Lon != nil {
        fmt.Printf("ICAO: %s, Position: %.4f,%.4f\n",
            location.Icao,
            *location.Lat,
            *location.Lon)
    }
}
```

**Channel characteristics**:
- Buffered: 100 locations (line 36 in client.go)
- Closed on disconnect
- Blocks if consumer slow (backpressure)

**Why buffered**: Handle bursts
```go
locationChan: make(chan *export.PlaneLocation, 100)
```

**100 capacity**: ~1-2 seconds buffer at 50 updates/sec

### Channel Blocking

**Consumer slower than producer**:
```go
for location := range client.LocationUpdates() {
    time.Sleep(5 * time.Second)  // Slow processing
    // Channel fills up, WebSocket reader blocks
}
```

**Mitigation**: Fast processing
```go
for location := range client.LocationUpdates() {
    go processAsync(location)  // Non-blocking
}
```

**Or increase buffer**: Modify package (rare)
```go
// In client.go, increase capacity
locationChan: make(chan *export.PlaneLocation, 1000)
```

### Handling Disconnection

**Channel closed on disconnect**:
```go
for location := range client.LocationUpdates() {
    // Loop exits when channel closed
}
fmt.Println("Disconnected")
```

**Reconnection pattern**:
```go
func maintainConnection() {
    for {
        client := ws_client.NewClient("plane.watch:8080")
        err := client.Connect()
        if err != nil {
            time.Sleep(5 * time.Second)
            continue
        }

        client.Subscribe("tile60")

        // Process until disconnect
        for location := range client.LocationUpdates() {
            process(location)
        }

        // Reconnect after 5 seconds
        time.Sleep(5 * time.Second)
    }
}
```

## Grid Information

### Fetching Grid Definition

**HTTP GET for tile boundaries**:
```go
grid, err := client.Grid()
if err != nil {
    log.Fatal(err)
}

// grid is tile_grid.GridLocations (map[string]GlobeIndexSpecialTile)
for tileName, tile := range grid {
    fmt.Printf("%s: N=%.2f E=%.2f S=%.2f W=%.2f\n",
        tileName,
        tile.North, tile.East, tile.South, tile.West)
}
```

**Why HTTP, not WebSocket**: Grid is static
- Grid doesn't change frequently
- No need for real-time updates
- HTTP simpler for one-time fetch
- REST endpoint: `http://plane.watch:8080/grid`

**Use case**: Display coverage area on map
```go
grid, _ := client.Grid()
tile60 := grid["tile60"]

// Draw rectangle on map
map.DrawRectangle(
    tile60.North, tile60.West,  // NW corner
    tile60.South, tile60.East,  // SE corner
)
```

### Grid Endpoint Construction

**Respects Secure() setting**:
```go
client.Secure(true)
grid, _ := client.Grid()
// Fetches: https://plane.watch:8080/grid

client.Secure(false)
grid, _ := client.Grid()
// Fetches: http://plane.watch:8080/grid
```

**Automatic URL building**:
```go
rqUrl := "http"
if c.secure {
    rqUrl += "s"  // https
}
rqUrl += "://" + c.host + "/grid"
```

## WebSocket Protocol

### Connection Details

**Endpoint**: `/planes`
**Subprotocol**: `planes`
**Compression**: Context takeover

```go
conf := websocket.DialOptions{
    Subprotocols:    []string{"planes"},
    CompressionMode: websocket.CompressionContextTakeover,
}
c.conn, _, err = websocket.Dial(ctx, "ws://"+c.host+"/planes", &conf)
```

**Why compression**: Bandwidth savings
- Aircraft JSON can be large
- Compression: 50-70% reduction
- Context takeover: Reuse compression dictionary across messages

**5-second connection timeout**:
```go
ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
defer cancel()
```

**Why 5 seconds**: Balance
- Too short: Fails on slow networks
- Too long: User waits during unavailability
- 5s: Reasonable for most conditions

### Message Flow

**After connection**:
```
1. Client connects → WebSocket established
2. Client sends subscribe request → Server acknowledges
3. Server pushes location updates → Client receives via channel
4. Client sends unsubscribe → Server stops sending
5. Client disconnects → Clean closure
```

**Subscribe flow**:
```go
// Client sends
{
    "type": "sub",
    "gridTile": "tile60"
}

// Server responds
{
    "type": "ack-sub",
    "tiles": ["tile60"]
}
```

**Location update**:
```go
// Server pushes (no request)
{
    "type": "plane-location",
    "location": {
        "icao": "A12345",
        "lat": 47.6062,
        "lon": -122.3321,
        // ... more fields
    }
}
```

### Reader Goroutine

**Spawned on connection**:
```go
if nil == err {
    go c.listen()  // Background reader
}
```

**Runs until disconnect**:
```go
func (c *Client) listen() {
    for {
        msg := ws_protocol.WsResponse{}
        err := wsjson.Read(ctx, c.conn, &msg)
        if err != nil {
            return  // Connection closed
        }

        // Route message to channel
        switch msg.Type {
        case ws_protocol.ResponseTypePlaneLocation:
            c.locationChan <- msg.Location
        case ws_protocol.ResponseTypeAckSub:
            c.ackSubChan <- msg.Tiles
        // ...
        }
    }
}
```

**Single goroutine**: Reads all messages
- Location updates → locationChan
- Ack responses → ackSubChan/ackUnsubChan
- Errors → Logged

## Synchronization

### Subscription Locks

**Mutex protection**:
```go
subLock, unsubLock, gridLock sync.Mutex
```

**Why locks**: Prevent concurrent requests
```go
func (c *Client) Subscribe(tileName string) error {
    c.subLock.Lock()
    defer c.subLock.Unlock()

    // Send request
    wsjson.Write(context.Background(), c.conn, &rq)

    // Wait for acknowledgment
    <-c.ackSubChan

    return nil
}
```

**Without lock**: Race condition
```go
// Goroutine 1: Subscribe("tile10")
// Goroutine 2: Subscribe("tile11")
// Both waiting on ackSubChan
// Goroutine 1 might receive tile11 ack, Goroutine 2 receives tile10 ack
// Wrong associations!
```

**Separate locks**: Allow concurrent operations
- Subscribe and Unsubscribe can happen simultaneously (different locks)
- Two Subscribes cannot (same lock)
- Grid fetch and Subscribe can happen simultaneously (different locks)

### Acknowledgment Channels

**Response routing**:
```go
ackSubChan    chan []string  // Subscribe acknowledgments
ackUnsubChan  chan []string  // Unsubscribe acknowledgments
gridTilesChan chan []string  // Subscribed tile list
```

**Blocking receive**: Wait for server response
```go
// Send subscribe request
wsjson.Write(context.Background(), c.conn, &rq)

// Block until server acknowledges
<-c.ackSubChan
```

**Why blocking**: Synchronous API
- Caller knows when subscription active
- Error handling straightforward
- No callback complexity

## Error Handling

### Connection Errors

**Connection failure**:
```go
err := client.Connect()
if err != nil {
    // Network down, server unreachable, etc.
    log.Error().Err(err).Msg("Failed to connect")
}
```

**Common errors**:
- Network unreachable: Server down or network issue
- Timeout: Connection took >5 seconds
- Protocol error: Server doesn't support "planes" subprotocol

### Message Errors

**Server error response**:
```go
// Server sends
{
    "type": "error",
    "message": "Unknown tile: tile999"
}

// Client logs
case ws_protocol.ResponseTypeError:
    log.Error().Str("Response", msg.Message)
```

**No exception thrown**: Logged only
- Subscribe/Unsubscribe return error on write failure
- Server errors logged but not surfaced to caller

<!--
Maintainers: Consider surfacing server errors to caller
Enhancement: Return server error from Subscribe() if server rejects
-->

### Read Errors

**WebSocket read failure**:
```go
err := wsjson.Read(ctx, c.conn, &msg)
if err != nil {
    log.Debug().Err(err).Msg("Failed to understand WS message")
    return  // Terminate reader goroutine
}
```

**Causes reader termination**:
- Connection closed (normal or abnormal)
- Protocol violation
- Malformed JSON

**Effect**: Location channel stops receiving
- Channel not closed (only on explicit Disconnect())
- Consumer blocks waiting for next message

<!--
Maintainers: Consider closing locationChan on reader termination
Current behavior: Consumer hangs if reader dies unexpectedly
-->

## Performance Characteristics

### Connection Overhead

**Handshake**: ~50-100ms
- TCP connection: ~10-30ms
- TLS handshake (if secure): ~20-50ms
- WebSocket upgrade: ~10-20ms

**Persistent**: One-time cost
- Connection reused for thousands of messages
- Amortized overhead: negligible

### Message Throughput

**Typical rates**:
- 10 tiles subscribed: ~50-200 updates/second
- 1 tile: ~5-20 updates/second
- All tiles: ~500-2000 updates/second (high traffic)

**Channel buffer**: 100 locations
- At 100 updates/sec: 1 second buffer
- At 500 updates/sec: 0.2 second buffer (tight!)

**Consumer must keep up**: Or channel fills
```go
// Fast consumer (good)
for location := range client.LocationUpdates() {
    quickProcess(location)  // <10ms per location
}

// Slow consumer (risky)
for location := range client.LocationUpdates() {
    database.Insert(location)  // ~50ms per location
    // Can't keep up at high rates!
}
```

### Memory Usage

**Per client**:
- Client struct: ~200 bytes
- WebSocket connection: ~10 KB
- Channel buffers: 100 × ~500 bytes = ~50 KB
- **Total**: ~60-70 KB per client

**Goroutines**: 1 per client (reader)

**Lightweight**: 1000 clients = ~60-70 MB

### Compression Benefit

**Context takeover compression**:
- First message: ~2 KB aircraft location JSON
- Subsequent messages: ~500-800 bytes (60-70% savings)

**Why effective**: Similar structure
- Same JSON keys repeated
- Compression dictionary reused
- Position changes small (delta)

**Bandwidth**: 500 updates/sec × 800 bytes = ~400 KB/sec compressed
- Uncompressed: ~1 MB/sec
- Savings: ~60%

## Use Cases

### Real-Time Map Display

```go
client := ws_client.NewClient("plane.watch:8080")
client.Connect()
defer client.Disconnect()

// Subscribe to visible tiles
client.Subscribe("tile10")
client.Subscribe("tile11")

// Update map as locations arrive
for location := range client.LocationUpdates() {
    if location.Lat != nil && location.Lon != nil {
        map.UpdateAircraft(location.Icao, *location.Lat, *location.Lon)
    }
}
```

### Regional Monitoring

```go
// Monitor Pacific Northwest
client := ws_client.NewClient("plane.watch:8080")
client.Connect()
client.Subscribe("tile60")

alerts := make([]string, 0)
for location := range client.LocationUpdates() {
    // Alert if low altitude
    if location.Altitude != nil && *location.Altitude < 5000 {
        alerts = append(alerts, fmt.Sprintf(
            "Low altitude: %s at %d ft",
            location.Icao,
            *location.Altitude))
    }
}
```

### Flight Tracking

```go
// Track specific aircraft by ICAO
targetIcao := "A12345"

client := ws_client.NewClient("plane.watch:8080")
client.Connect()

// Subscribe to all tiles (to catch aircraft anywhere)
grid, _ := client.Grid()
for tileName := range grid {
    client.Subscribe(tileName)
}

for location := range client.LocationUpdates() {
    if location.Icao == targetIcao {
        fmt.Printf("Target at %.4f,%.4f, heading %.1f\n",
            *location.Lat,
            *location.Lon,
            *location.Heading)
    }
}
```

## Common Issues

### No Locations Received

**Symptom**: Channel empty, no updates

**Causes**:
1. **Not subscribed to any tiles**
   ```go
   client.Connect()
   // Forgot to subscribe!
   for location := range client.LocationUpdates() {
       // Never receives anything
   }
   ```
   **Solution**: Subscribe to at least one tile

2. **Subscribed to empty tile**
   ```go
   client.Subscribe("tile2")  // Pacific Ocean, sparse coverage
   // Few aircraft, infrequent updates
   ```
   **Solution**: Subscribe to high-traffic tiles (tile10, tile60, etc.)

3. **Reader goroutine died**
   - Check logs for WebSocket errors
   - Connection might have closed

### Channel Blocks

**Symptom**: Application freezes

**Cause**: Channel buffer full, consumer slow
```go
for location := range client.LocationUpdates() {
    time.Sleep(1 * time.Second)  // Way too slow!
    // Buffer fills, WebSocket reader blocks
}
```

**Solution**: Fast processing or async
```go
for location := range client.LocationUpdates() {
    go processAsync(location)  // Non-blocking
}
```

### Subscription Doesn't Work

**Symptom**: Subscribe() hangs

**Cause**: Server didn't acknowledge
- Network issue (packet loss)
- Server bug (no ack sent)
- Invalid tile name

**Debug**:
```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

done := make(chan error)
go func() {
    done <- client.Subscribe("tile10")
}()

select {
case err := <-done:
    if err != nil {
        log.Error().Err(err).Msg("Subscribe failed")
    }
case <-ctx.Done():
    log.Error().Msg("Subscribe timeout - server didn't ack")
}
```

### Disconnect Not Clean

**Symptom**: Errors on shutdown

**Cause**: Didn't call Disconnect()
```go
client.Connect()
// Application exits
// WebSocket connection not cleanly closed
```

**Solution**: Always defer
```go
client.Connect()
defer client.Disconnect()
```

## Best Practices

### Always Defer Disconnect

```go
err := client.Connect()
if err != nil {
    return err
}
defer client.Disconnect()  // Ensures cleanup
```

### Check Nil Pointers

**PlaneLocation has optional fields**:
```go
for location := range client.LocationUpdates() {
    // Check before dereferencing
    if location.Lat != nil && location.Lon != nil {
        lat := *location.Lat
        lon := *location.Lon
        // Use lat, lon
    }

    if location.Altitude != nil {
        alt := *location.Altitude
        // Use alt
    }
}
```

**Why optional**: Not all fields in every update
- Partial updates common
- Check nil before access

### Fast Processing

```go
// Good: Fast processing
for location := range client.LocationUpdates() {
    updateCache(location)  // In-memory, fast
}

// Bad: Slow processing
for location := range client.LocationUpdates() {
    database.Insert(location)  // Blocking I/O, slow
}

// Better: Async processing
for location := range client.LocationUpdates() {
    go database.Insert(location)  // Non-blocking
}

// Best: Batching
batch := make([]*export.PlaneLocation, 0, 100)
for location := range client.LocationUpdates() {
    batch = append(batch, location)
    if len(batch) >= 100 {
        go database.BatchInsert(batch)
        batch = batch[:0]
    }
}
```

### Reconnection Logic

```go
func connectWithRetry() *ws_client.Client {
    backoff := time.Second

    for {
        client := ws_client.NewClient("plane.watch:8080")
        err := client.Connect()
        if err == nil {
            return client
        }

        log.Error().Err(err).Dur("backoff", backoff).Msg("Reconnecting")
        time.Sleep(backoff)

        backoff *= 2
        if backoff > time.Minute {
            backoff = time.Minute
        }
    }
}
```

## Future Enhancements

<!--
Maintainers: Document enhancements as implemented
-->

### Error Channel

**Proposed**: Return errors to caller
```go
type Client struct {
    locationChan chan *export.PlaneLocation
    errorChan    chan error  // New: error channel
}

for {
    select {
    case location := <-client.LocationUpdates():
        // Process location
    case err := <-client.Errors():
        // Handle error
    }
}
```

**Benefit**: Application can react to errors

### Connection State

**Proposed**: Expose connection state
```go
func (c *Client) IsConnected() bool {
    return c.conn != nil
}

func (c *Client) ConnectionState() ConnectionState {
    // Connecting, Connected, Disconnected, Reconnecting
}
```

**Use case**: Display connection status to user

### Automatic Reconnection

**Proposed**: Built-in reconnect logic
```go
client := ws_client.NewClient("plane.watch:8080",
    ws_client.WithAutoReconnect(true),
    ws_client.WithReconnectBackoff(time.Second, time.Minute))

// Connection automatically maintained
for location := range client.LocationUpdates() {
    // Process (no manual reconnect needed)
}
```

### Ping/Pong Health

**Proposed**: WebSocket ping/pong for connection health
```go
// Detect dead connections
conn.SetReadDeadline(time.Now().Add(30 * time.Second))
go func() {
    ticker := time.NewTicker(15 * time.Second)
    for range ticker.C {
        conn.Ping()  // Keep-alive
    }
}()
```

## File Guide

| File | Purpose |
|------|---------|
| `client.go` | WebSocket client implementation, subscriptions |
| `default.go` | Default client for plane.watch |

## See Also

- [ws_protocol](../ws_protocol/README.md) - Protocol definitions and alternative client
- [export](../export/README.md) - PlaneLocation struct definition
- [tile_grid](../tile_grid/README.md) - Tile system for geographic filtering

## References

- WebSocket library: https://github.com/coder/websocket
- PlaneWatch API: https://plane.watch
