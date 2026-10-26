# WebSocket Protocol

## Overview

The `ws_protocol` package defines the message protocol for real-time aircraft data streaming over WebSockets. It specifies request/response message types, special grid tiles, and provides a flexible WebSocket client with callback-based message handling.

## Why This Package?

### Protocol Standardization

**Without protocol definition**:
```go
// Scattered message types across codebase
conn.Write(`{"type":"subscribe","tile":"tile10"}`)  // Typo-prone
conn.Write(`{"type":"sub","tile":"tile10"}`)        // Inconsistent

// Different clients use different formats
// Server can't validate
// Breaking changes frequent
```

**With protocol package**:
```go
rq := ws_protocol.WsRequest{
    Type:     ws_protocol.RequestTypeSubscribe,  // Constant, validated
    GridTile: "tile10",
}
// Type-safe, consistent, documented
```

**Benefits**:
- ✓ Centralized protocol definition
- ✓ Type safety (compile-time checking)
- ✓ Consistent message format
- ✓ Self-documenting (constants)

### Two Client Flavors

**Simple client** (ws_client package):
```go
client := ws_client.NewClient("plane.watch:8080")
client.Subscribe("tile10")
for loc := range client.LocationUpdates() {
    // Channel-based
}
```

**Flexible client** (ws_protocol.WsClient):
```go
client := ws_protocol.NewClient(
    ws_protocol.WithResponseHandler(func(resp *ws_protocol.WsResponse) {
        // Callback-based, access all response types
        switch resp.Type {
        case ws_protocol.ResponseTypePlaneLocation:
            // Handle location
        case ws_protocol.ResponseTypeSearchResults:
            // Handle search
        }
    }),
)
```

**Why two clients**:
- Simple: 90% use case (just subscribe to tiles, get locations)
- Flexible: Advanced features (search, history, custom handling)

## Protocol Messages

### Request Types

**Subscribe to tile**:
```go
const RequestTypeSubscribe = "sub"

rq := WsRequest{
    Type:     RequestTypeSubscribe,
    GridTile: "tile10",
}
// Server starts sending aircraft in tile10
```

**Unsubscribe from tile**:
```go
const RequestTypeUnsubscribe = "unsub"

rq := WsRequest{
    Type:     RequestTypeUnsubscribe,
    GridTile: "tile10",
}
// Server stops sending tile10 updates
```

**List subscriptions**:
```go
const RequestTypeSubscribeList = "sub-list"

rq := WsRequest{
    Type: RequestTypeSubscribeList,
}
// Server responds with array of subscribed tiles
```

**Set subscribed tile list** (atomic replacement):
```go
const RequestTypeSetSubscribedTiles = "set-sub-tile-list"

rq := WsRequest{
    Type:      RequestTypeSetSubscribedTiles,
    GridTile:  "tile35_high,tile36_high",  // comma-separated
    RequestId: "req-123",                   // optional, echoed on responses
}
// Atomically replaces all subscriptions.
// Server validates all tiles before applying — if any tile is invalid,
// the entire request is rejected and existing subscriptions remain intact.
//
// Lifecycle:
//   1. ack-sub          — subscriptions applied (tiles + requestId)
//   2. plane-location-list — immediate snapshot flush (may be omitted if zero aircraft)
//   3. initial-sync-complete — snapshot phase done (always sent, even for zero aircraft)
//
// The snapshot plane-location-list carries requestId. Later tick-batched
// plane-location-list messages from live updates do NOT carry requestId.
```

**Search aircraft**:
```go
const RequestTypeSearch = "search"

rq := WsRequest{
    Type:  RequestTypeSearch,
    Query: "UAL",  // Search for United Airlines flights
}
// Server responds with matching aircraft, airports, routes
```

**Get aircraft in grid**:
```go
const RequestTypeGridPlanes = "grid-planes"

rq := WsRequest{
    Type:     RequestTypeGridPlanes,
    GridTile: "tile10",
}
// Server responds with current aircraft snapshot in tile
```

**Get location history**:
```go
const RequestTypePlaneLocHistory = "plane-location-history"

rq := WsRequest{
    Type: RequestTypePlaneLocHistory,
    Icao: "A12345",
}
// Server responds with aircraft's recent path
```

**Adjust update frequency**:
```go
const RequestTypeTickAdjust = "adjust-tick"

rq := WsRequest{
    Type: RequestTypeTickAdjust,
    Tick: 5000,  // Milliseconds between updates
}
// Server adjusts update rate to 5 seconds
```

### Response Types

**Error**:
```go
const ResponseTypeError = "error"

resp := WsResponse{
    Type:    ResponseTypeError,
    Message: "Unknown tile: tile999",
}
```

**Info message**:
```go
const ResponseTypeMsg = "info"

resp := WsResponse{
    Type:    ResponseTypeMsg,
    Message: "Connected to Plane.Watch",
}
```

**Subscribe acknowledged**:
```go
const ResponseTypeAckSub = "ack-sub"

resp := WsResponse{
    Type:  ResponseTypeAckSub,
    Tiles: []string{"tile10", "tile11"},
}
// Confirms subscriptions active
```

**Unsubscribe acknowledged**:
```go
const ResponseTypeAckUnsub = "ack-unsub"

resp := WsResponse{
    Type:  ResponseTypeAckUnsub,
    Tiles: []string{"tile10"},
}
// Confirms unsubscribe processed
```

**Subscription list**:
```go
const ResponseTypeSubTiles = "sub-list"

resp := WsResponse{
    Type:  ResponseTypeSubTiles,
    Tiles: []string{"tile10", "tile60"},
}
// Response to RequestTypeSubscribeList
```

**Aircraft location update**:
```go
const ResponseTypePlaneLocation = "plane-location"

resp := WsResponse{
    Type: ResponseTypePlaneLocation,
    Location: &export.PlaneLocation{
        Icao: "A12345",
        Lat:  &lat,
        Lon:  &lon,
        // ... other fields
    },
}
// Real-time position update
```

**Multiple locations**:
```go
const ResponseTypePlaneLocations = "plane-location-list"

resp := WsResponse{
    Type: ResponseTypePlaneLocations,
    Locations: []*export.PlaneLocation{
        {Icao: "A12345", ...},
        {Icao: "B67890", ...},
    },
}
// Response to RequestTypeGridPlanes (snapshot)
```

**Location history**:
```go
const ResponseTypePlaneLocHistory = "plane-location-history"

resp := WsResponse{
    Type:     ResponseTypePlaneLocHistory,
    Icao:     "A12345",
    CallSign: "UAL123",
    History: []LocationHistory{
        {Lat: 47.6, Lon: -122.3, Heading: 90, Velocity: 450, Altitude: &alt},
        {Lat: 47.7, Lon: -122.1, Heading: 92, Velocity: 455, Altitude: &alt2},
    },
}
// Aircraft's path over time
```

**Search results**:
```go
const ResponseTypeSearchResults = "search-results"

resp := WsResponse{
    Type: ResponseTypeSearchResults,
    Results: &SearchResult{
        Query: "UAL",
        Aircraft: []*export.PlaneLocation{
            {CallSign: "UAL123", ...},
            {CallSign: "UAL456", ...},
        },
        Airport: []AirportLocation{
            {Name: "United Terminal", Icao: "KSFO", ...},
        },
        Route: []string{"SFO-LAX", "SFO-ORD"},
    },
}
```

**Initial sync complete**:
```go
const ResponseTypeInitialSyncComplete = "initial-sync-complete"

resp := WsResponse{
    Type:          ResponseTypeInitialSyncComplete,
    Tiles:         []string{"tile35_high", "tile36_high"},
    AircraftCount: &count,  // number of aircraft in snapshot (can be 0)
    RequestId:     "req-123",
}
// Sent after the snapshot phase of set-sub-tile-list completes.
// Always sent, even when zero aircraft match.
// Distinct from ack-sub — this signals data delivery is complete,
// not just that subscriptions were applied.
```

## Special Grid Tiles

### All Low Altitude

```go
const GridTileAllLow = "all_low"

rq := WsRequest{
    Type:     RequestTypeSubscribe,
    GridTile: GridTileAllLow,
}
// Subscribe to all aircraft below altitude threshold
```

**Use case**: Ground operations, landing/takeoff monitoring
- Aircraft below ~10,000 feet (implementation-specific)
- Worldwide coverage
- High traffic volume

### All High Altitude

```go
const GridTileAllHigh = "all_high"

rq := WsRequest{
    Type:     RequestTypeSubscribe,
    GridTile: GridTileAllHigh,
}
// Subscribe to all aircraft above altitude threshold
```

**Use case**: Enroute monitoring, oceanic flight tracking
- Aircraft above ~10,000 feet
- Lower volume than all_low
- Global coverage

**Why altitude-based tiles**: Operational context
- Ground ops care about low aircraft
- Enroute monitoring cares about cruising aircraft
- Filtering reduces irrelevant data

## Data Structures

### WsRequest

```go
type WsRequest struct {
    Type      string `json:"type"`                 // Request type constant
    GridTile  string `json:"gridTile"`             // Tile name (for sub/unsub/grid-planes), comma-separated for set-sub-tile-list
    Icao      string `json:"icao,omitempty"`       // Aircraft ICAO (for history)
    CallSign  string `json:"callSign,omitempty"`   // Callsign (for search)
    Tick      int    `json:"tick,omitempty"`        // Update interval (milliseconds)
    Query     string `json:"query,omitempty"`       // Search query
    RequestId string `json:"requestId,omitempty"`   // Optional correlation ID, echoed on responses
}
```

**Minimal fields**: Only populate what's needed
```go
// Subscribe: Only Type and GridTile
rq := WsRequest{
    Type:     RequestTypeSubscribe,
    GridTile: "tile10",
}

// Search: Only Type and Query
rq := WsRequest{
    Type:  RequestTypeSearch,
    Query: "UAL",
}
```

### WsResponse

```go
type WsResponse struct {
    Type          string                  `json:"type"`
    Message       string                  `json:"message,omitempty"`
    Tiles         []string                `json:"tiles,omitempty"`
    Location      *export.PlaneLocation   `json:"location,omitempty"`
    Locations     []*export.PlaneLocation `json:"locations,omitempty"`
    Icao          string                  `json:"icao,omitempty"`
    CallSign      string                  `json:"callSign,omitempty"`
    History       []LocationHistory       `json:"history,omitempty"`
    Results       *SearchResult           `json:"results,omitempty"`
    RequestId     string                  `json:"requestId,omitempty"`
    AircraftCount *int                    `json:"aircraftCount,omitempty"`
}
```

**Type determines which fields populated**:
- `ResponseTypePlaneLocation`: Location field set
- `ResponseTypePlaneLocations`: Locations field set (+ RequestId on snapshot flush only)
- `ResponseTypeAckSub`: Tiles field set (+ RequestId if provided)
- `ResponseTypeInitialSyncComplete`: Tiles, AircraftCount (+ RequestId if provided)
- `ResponseTypeSearchResults`: Results field set

### LocationHistory

```go
type LocationHistory struct {
    Lat, Lon          float64
    LatLon            orb.Point `json:"-"`  // Not serialized
    Heading, Velocity float64
    Altitude          *int32
}
```

**Simplified vs PlaneLocation**: Essential fields only
- Position: Lat, Lon
- Movement: Heading, Velocity
- Altitude: Optional (pointer)

**Why simpler**: Historical path doesn't need:
- Source tags
- Update timestamps
- Aircraft metadata (provided once in response)

**orb.Point**: GIS library type
```go
import "github.com/paulmach/orb"

LatLon orb.Point  // [lon, lat] - GeoJSON order
```

**Not serialized**: `json:"-"` omits from JSON
- Derived from Lat/Lon
- Used for spatial calculations
- Would duplicate data in JSON

### SearchResult

```go
type SearchResult struct {
    Query    string
    Aircraft AircraftList            // Matching aircraft
    Airport  []AirportLocation       // Matching airports
    Route    []string                // Matching routes
}
```

**Multi-category results**: Search across all types
```go
// Search "SFO"
{
    "query": "SFO",
    "aircraft": [...],  // Flights to/from SFO
    "airport": [{
        "name": "San Francisco Intl",
        "icao": "KSFO",
        "iata": "SFO",
        "lat": 37.6189,
        "lon": -122.3750
    }],
    "route": ["SFO-LAX", "SFO-ORD", "ORD-SFO"]
}
```

### AirportLocation

```go
type AirportLocation struct {
    Name     string
    Icao     string  // 4-letter code (KSFO)
    Iata     string  // 3-letter code (SFO)
    Lat, Lon float64
}
```

**Airport identification**: Multiple codes
- ICAO: International standard (KSFO)
- IATA: Common passenger code (SFO)
- Both provided for flexibility

## Flexible Client (WsClient)

### Basic Usage

```go
import "plane.watch/lib/ws_protocol"

client := ws_protocol.NewClient(
    ws_protocol.WithSourceURL("wss://plane.watch/planes"),
    ws_protocol.WithResponseHandler(func(resp *ws_protocol.WsResponse) {
        switch resp.Type {
        case ws_protocol.ResponseTypePlaneLocation:
            fmt.Printf("Aircraft: %s at %.4f,%.4f\n",
                resp.Location.Icao,
                *resp.Location.Lat,
                *resp.Location.Lon)
        case ws_protocol.ResponseTypeError:
            log.Error().Str("error", resp.Message).Send()
        }
    }),
    ws_protocol.WithLogger(log),
)

err := client.Connect()
if err != nil {
    log.Fatal(err)
}
defer client.Disconnect()

// Subscribe
client.Subscribe("tile10")

// Keep running
select {}
```

### Callback-Based Handling

**Multiple handlers**:
```go
client := ws_protocol.NewClient(
    ws_protocol.WithResponseHandler(locationHandler),
    ws_protocol.WithResponseHandler(errorHandler),
    ws_protocol.WithResponseHandler(metricsHandler),
)
```

**All handlers called**: For each message
```go
func locationHandler(resp *ws_protocol.WsResponse) {
    if resp.Type == ws_protocol.ResponseTypePlaneLocation {
        updateMap(resp.Location)
    }
}

func errorHandler(resp *ws_protocol.WsResponse) {
    if resp.Type == ws_protocol.ResponseTypeError {
        log.Error().Msg(resp.Message)
    }
}

func metricsHandler(resp *ws_protocol.WsResponse) {
    messageCounter.Inc()
}
```

**Why multiple handlers**: Separation of concerns
- Location handler updates UI
- Error handler logs
- Metrics handler counts
- Each focused on one responsibility

### Insecure TLS

**Default**: `InsecureSkipVerify: true`
```go
customTransport := http.DefaultTransport.(*http.Transport).Clone()
customTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
```

**Why insecure by default**: Development ease
- Self-signed certificates common in dev
- Quick testing without cert setup
- **NOT production-safe**

<!--
Maintainers: Consider making this configurable
Enhancement: Add WithSecure() option to enable proper TLS verification
Production deployments should verify certificates
-->

### Read Limit

**1 MiB message limit**:
```go
c.conn.SetReadLimit(1_048_576)  // 1 MiB
```

**Why limit**: Prevent memory exhaustion
- Malicious server sends giant message
- Client allocates memory, crashes
- Limit protects client

**1 MiB sufficient**: Typical messages
- Location update: ~1-2 KB
- Snapshot of 1000 aircraft: ~1-2 MB (over limit!)
- History: ~10-50 KB

<!--
Maintainers: Consider making read limit configurable
Large tile snapshots might exceed 1 MiB
Enhancement: WithReadLimit(bytes int64) option
-->

### Reader Goroutine

**Background message processing**:
```go
go c.reader()

func (c *WsClient) reader() {
    for {
        mType, msg, err := c.conn.Read(context.Background())
        if mType != websocket.MessageText {
            continue  // Skip binary messages
        }

        r := &WsResponse{}
        json.Unmarshal(msg, r)

        // Call all handlers
        for _, f := range c.responseHandlers {
            f(r)
        }
    }
}
```

**Runs until disconnect**: Terminates on WebSocket close

## Subscribing

### Individual Tiles

```go
client.Subscribe("tile10")  // Europe
client.Subscribe("tile60")  // Pacific Northwest
```

### All Low Altitude

```go
client.SubscribeAllLow()

// Equivalent to:
client.Subscribe(ws_protocol.GridTileAllLow)
```

### All High Altitude

```go
client.SubscribeAllHigh()

// Equivalent to:
client.Subscribe(ws_protocol.GridTileAllHigh)
```

### Fire-and-Forget

**No acknowledgment wait**:
```go
func (c *WsClient) Subscribe(gridTile string) error {
    rq := WsRequest{
        Type:     RequestTypeSubscribe,
        GridTile: gridTile,
    }
    return c.writeRequest(&rq)  // Writes, doesn't wait
}
```

**Contrast with ws_client**:
```go
// ws_client.Client waits for ack
func (c *Client) Subscribe(tileName string) error {
    // Send request
    wsjson.Write(context.Background(), c.conn, &rq)

    // Wait for ack
    <-c.ackSubChan  // Blocks until server responds
    return nil
}
```

**Why no wait**: Callback model
- Ack arrives as normal message
- Handler processes ack
- Async, non-blocking

## Aircraft Sorting

### AircraftList

```go
type AircraftList []*export.PlaneLocation

func (l AircraftList) Less(i, j int) bool {
    left := unPtr(l[i].CallSign) + ":" + unPtr(l[i].Registration) + ":" + l[i].Icao
    right := unPtr(l[j].CallSign) + ":" + unPtr(l[j].Registration) + ":" + l[j].Icao
    return left < right
}
```

**Implements sort.Interface**: Can use sort.Sort()
```go
aircraft := ws_protocol.AircraftList{...}
sort.Sort(aircraft)
// Sorted by CallSign:Registration:Icao
```

**Sort key**: Multi-field concatenation
- Primary: CallSign (UAL123)
- Secondary: Registration (N12345)
- Tertiary: Icao (A12345)

**Why this order**: User-facing priority
- CallSign most recognizable (flight number)
- Registration secondary identifier (tail number)
- ICAO always present (fallback)

**Handles nil**: `unPtr()` helper
```go
func unPtr[t any](what *t) t {
    var def t
    if nil == what {
        return def  // Empty string for string, 0 for int
    }
    return *what
}
```

**Example sorted list**:
```
""::A12345                 // No callsign, no registration
""::B67890                 // No callsign, no registration
"":N12345:C00001           // No callsign, has registration
AAL123:N12345:A12345       // American Airlines 123
UAL456:N67890:B67890       // United Airlines 456
UAL789::C11111             // United 789, no registration
```

## Use Cases

### Advanced Search Interface

```go
client := ws_protocol.NewClient(
    ws_protocol.WithResponseHandler(func(resp *ws_protocol.WsResponse) {
        if resp.Type == ws_protocol.ResponseTypeSearchResults {
            displayResults(resp.Results)
        }
    }),
)

client.Connect()

// User searches for "United"
rq := ws_protocol.WsRequest{
    Type:  ws_protocol.RequestTypeSearch,
    Query: "United",
}
client.writeRequest(&rq)
```

### Flight Path Display

```go
// Request history for specific aircraft
rq := ws_protocol.WsRequest{
    Type: ws_protocol.RequestTypePlaneLocHistory,
    Icao: "A12345",
}
client.writeRequest(&rq)

// Handler draws path
ws_protocol.WithResponseHandler(func(resp *ws_protocol.WsResponse) {
    if resp.Type == ws_protocol.ResponseTypePlaneLocHistory {
        for _, point := range resp.History {
            map.AddPathPoint(point.Lat, point.Lon)
        }
    }
})
```

### Dynamic Update Rate

```go
// Slow updates for battery saving
rq := ws_protocol.WsRequest{
    Type: ws_protocol.RequestTypeTickAdjust,
    Tick: 10000,  // 10 seconds
}
client.writeRequest(&rq)

// Fast updates for real-time tracking
rq := ws_protocol.WsRequest{
    Type: ws_protocol.RequestTypeTickAdjust,
    Tick: 500,  // 0.5 seconds
}
client.writeRequest(&rq)
```

## Comparison: ws_client vs ws_protocol.WsClient

### ws_client.Client

**Pros**:
- Simple API (Subscribe(), LocationUpdates())
- Channel-based (Go-idiomatic)
- Synchronous subscriptions (blocks until ack)
- Easy to use

**Cons**:
- Only exposes location updates
- No access to search, history
- Fixed buffer size (100)
- Less flexible

**Use when**: Simple location streaming

### ws_protocol.WsClient

**Pros**:
- Full protocol access (search, history, etc.)
- Callback-based (multiple handlers)
- Flexible message handling
- Configurable

**Cons**:
- More complex setup (handlers)
- Async (no ack waiting)
- No built-in channel delivery
- Callback-style less Go-idiomatic

**Use when**: Advanced features needed

## Common Issues

### Message Not Received

**Symptom**: Handler not called

**Causes**:
1. **Wrong response type check**
   ```go
   if resp.Type == "plane-loc" {  // Typo! Should be "plane-location"
       // Never matches
   }
   ```
   **Solution**: Use constants
   ```go
   if resp.Type == ws_protocol.ResponseTypePlaneLocation {
       // Correct
   }
   ```

2. **Binary message sent**
   ```go
   if mType != websocket.MessageText {
       continue  // Binary messages skipped
   }
   ```
   **Solution**: Protocol is text-only (JSON)

3. **Handler panics**
   ```go
   // Handler crashes, stops processing
   ws_protocol.WithResponseHandler(func(resp *ws_protocol.WsResponse) {
       panic("oops")  // Kills reader goroutine
   })
   ```
   **Solution**: Recover in handler
   ```go
   ws_protocol.WithResponseHandler(func(resp *ws_protocol.WsResponse) {
       defer func() {
           if r := recover(); r != nil {
               log.Error().Interface("panic", r).Send()
           }
       }()
       // Handler code
   })
   ```

### TLS Certificate Error

**Symptom**: Connection fails with certificate error (if InsecureSkipVerify removed)

**Cause**: Invalid certificate
- Self-signed certificate
- Expired certificate
- Hostname mismatch

**Current behavior**: Ignored (InsecureSkipVerify: true)

**If verification enabled**:
- Use valid certificate (Let's Encrypt, commercial CA)
- Match hostname to certificate CN/SAN
- Ensure certificate not expired

### Message Too Large

**Symptom**: Connection closed after large message

**Cause**: Exceeds 1 MiB read limit
```go
// Large snapshot request
rq := WsRequest{
    Type:     RequestTypeGridPlanes,
    GridTile: "tile0",  // Huge tile with many aircraft
}
// Response might exceed 1 MiB
```

**Solution**: Increase read limit
```go
// After Connect(), before operations
client.conn.SetReadLimit(10_485_760)  // 10 MiB
```

<!--
Maintainers: Make read limit configurable via option
-->

## Best Practices

### Always Use Constants

```go
// Good
rq := WsRequest{
    Type: ws_protocol.RequestTypeSubscribe,
}

// Bad
rq := WsRequest{
    Type: "subscribe",  // Typo-prone, breaks on protocol change
}
```

### Check Response Type

```go
ws_protocol.WithResponseHandler(func(resp *ws_protocol.WsResponse) {
    switch resp.Type {
    case ws_protocol.ResponseTypePlaneLocation:
        // Handle location
    case ws_protocol.ResponseTypeError:
        // Handle error
    default:
        // Unknown message type (future protocol additions)
    }
})
```

### Handle Errors

```go
ws_protocol.WithResponseHandler(func(resp *ws_protocol.WsResponse) {
    if resp.Type == ws_protocol.ResponseTypeError {
        log.Error().Str("error", resp.Message).Msg("Server error")
        // Maybe retry, alert user, etc.
    }
})
```

### Nil-Safe Access

```go
if resp.Location != nil {
    if resp.Location.Lat != nil && resp.Location.Lon != nil {
        lat := *resp.Location.Lat
        lon := *resp.Location.Lon
        // Use lat, lon
    }
}
```

### Defer Disconnect

```go
client := ws_protocol.NewClient(...)
err := client.Connect()
if err != nil {
    return err
}
defer client.Disconnect()  // Clean closure
```

## Future Enhancements

<!--
Maintainers: Document enhancements as implemented
-->

### Configurable TLS Verification

**Proposed**:
```go
ws_protocol.WithSecure(true)  // Enable proper TLS verification
ws_protocol.WithInsecure()    // Disable (development only)
```

**Default**: Secure by default, insecure opt-in

### Configurable Read Limit

**Proposed**:
```go
ws_protocol.WithReadLimit(10 * 1024 * 1024)  // 10 MiB
```

### Request/Response Correlation

**Implemented**: Optional `requestId` field on `WsRequest` is echoed on associated responses.
Currently supported on `set-sub-tile-list` — the `requestId` is echoed on `ack-sub`,
the immediate snapshot `plane-location-list`, and `initial-sync-complete`.

```go
rq := WsRequest{
    Type:      RequestTypeSetSubscribedTiles,
    GridTile:  "tile35_high",
    RequestId: "my-req-1",
}

// Responses will include RequestId: "my-req-1"
// Useful for ignoring superseded loads during rapid panning
```

**Omitting `requestId`**: Responses simply omit the field (backward compatible).

### Typed Handlers

**Proposed**: Type-specific callbacks
```go
ws_protocol.WithLocationHandler(func(loc *export.PlaneLocation) {
    // Only location updates
})

ws_protocol.WithErrorHandler(func(msg string) {
    // Only errors
})

ws_protocol.WithSearchHandler(func(results *SearchResult) {
    // Only search results
})
```

**Benefit**: Simpler handlers, no type switching

## File Guide

| File | Purpose |
|------|---------|
| `protocol.go` | Message type definitions, constants |
| `client.go` | Flexible WebSocket client with callbacks |

## See Also

- [ws_client](../ws_client/README.md) - Simple channel-based WebSocket client
- [export](../export/README.md) - PlaneLocation struct definition
- [tile_grid](../tile_grid/README.md) - Geographic tile system

## References

- WebSocket library: https://github.com/coder/websocket
- Orb GIS library: https://github.com/paulmach/orb
- PlaneWatch API: https://plane.watch
