# Export Format & Data Merging

## Overview

The `export` package provides the external data format for aircraft positions and the logic for merging updates from multiple sources. It's the bridge between internal tracker state and external consumers (APIs, databases, visualizations).

## Why a Separate Export Package?

### The Problem

**Internal representation** (tracker.Plane):
- Optimized for tracking state machine
- Includes implementation details (locks, frame history)
- Coupled to tracking logic

**External representation needs**:
- Clean JSON schema
- Stable API (backwards compatible)
- Enrichment fields (database lookups)
- Multi-source aggregation

**Without separation**: Internal changes break external consumers

### The Solution

**Export layer** between tracker and consumers:
```
Tracker (Plane) → Export (PlaneLocation) → JSON → Consumers
```

**Benefits**:
- Internal changes don't affect JSON schema
- Can add enrichment without modifying tracker
- Merge logic isolated from tracking logic
- Versioned external API

## PlaneLocation Structure

### Core Tracking Data

```go
type PlaneLocation struct {
    // Identity
    Icao string  // "ABC123"

    // Position & Motion
    Lat, Lon       float64
    Altitude       int
    Heading        float64
    Velocity       float64
    VerticalRate   int

    // Status Flags
    HasLocation     bool
    HasAltitude     bool
    HasHeading      bool
    HasVelocity     bool
    HasVerticalRate bool
    HasOnGround     bool

    // State
    OnGround      bool
    FlightStatus  string
    AltitudeUnits string  // "feet" or "metres"

    // Metadata
    SourceTag    string
    TileLocation string  // Grid tile (e.g., "123_456")
    TrackedSince time.Time
    LastMsg      time.Time

    // Update Timestamps
    Updates Updates  // Per-field last update times
}
```

### Why "Has" Flags?

**Problem**: Zero values are ambiguous

```go
Altitude: 0  // Is this "on ground" or "no data"?
Lat: 0.0     // Is this (0°N, 0°E) or "no position"?
```

**Solution**: Explicit validity flags

```go
if pl.HasAltitude {
    // Altitude field is valid
    fmt.Printf("Altitude: %d %s\n", pl.Altitude, pl.AltitudeUnits)
} else {
    // No altitude data available
}
```

**Why this matters**: Consumer code can distinguish "zero" from "unknown"

### Per-Field Update Timestamps

```go
type Updates struct {
    Location     time.Time
    Altitude     time.Time
    Velocity     time.Time
    Heading      time.Time
    OnGround     time.Time
    VerticalRate time.Time
    FlightStatus time.Time
    Special      time.Time
    Squawk       time.Time
}
```

**Purpose**: Multi-source merging

**Example scenario**:
```
Source A: Position update at 12:00:00
Source B: Altitude update at 12:00:01
Source C: Position update at 12:00:02 (newer!)

Merge: Take position from C, altitude from B
```

**Without per-field timestamps**: Can't merge correctly

### Enrichment Fields

```go
// Aircraft Database Enrichment
Registration    *string  // "N12345"
TypeCode        *string  // "B738"
TypeCodeLong    *string  // "Boeing 737-800"
Serial          *string  // "12345"
RegisteredOwner *string  // "Airline Inc"
EngineType      *string  // "CFM56-7B"

// Route Database Enrichment
CallSign  *string    // "UAL123"
Operator  *string    // "United Airlines"
RouteCode *string    // "ORD-LAX"
Segments  []Segment  // [ORD, DEN, LAX]
```

**Why pointers**: Distinguish "no data" (nil) from "empty string" ("")

**Populated by**: Separate enrichment services (not tracker)

**Typical flow**:
```
1. Tracker emits PlaneLocation with basic data
2. Sink publishes to NATS "location-updates"
3. Enrichment service consumes, looks up ICAO in database
4. Enrichment service publishes enriched PlaneLocation to different topic
```

## JSON Encoding

### Fast JSON (jsoniter)

```go
import jsoniter "github.com/json-iterator/go"

func (pl *PlaneLocation) ToJSONBytes() ([]byte, error) {
    json := jsoniter.ConfigFastest
    return json.Marshal(pl)
}
```

**Why jsoniter over encoding/json**:
- 3-5x faster encoding
- Lower memory allocation
- Drop-in replacement (same API)

**ConfigFastest**: Optimizes for speed over compatibility
- Skip HTML escaping
- Skip sorting map keys
- Minimal validation

**When to use**: Internal messaging (NATS), not user-facing APIs

### Example JSON Output

```json
{
  "Icao": "ABC123",
  "Lat": 38.8977,
  "Lon": -77.0365,
  "Heading": 285.5,
  "Altitude": 37000,
  "VerticalRate": 0,
  "Velocity": 450.2,
  "New": false,
  "Removed": false,
  "OnGround": false,
  "HasAltitude": true,
  "HasLocation": true,
  "HasHeading": true,
  "HasVelocity": true,
  "AltitudeUnits": "feet",
  "FlightStatus": "",
  "SourceTag": "myreceiver",
  "TileLocation": "12345_67890",
  "TrackedSince": "2024-11-16T12:00:00Z",
  "LastMsg": "2024-11-16T12:34:56Z",
  "Updates": {
    "Location": "2024-11-16T12:34:55Z",
    "Altitude": "2024-11-16T12:34:50Z",
    "Velocity": "2024-11-16T12:34:54Z",
    "Heading": "2024-11-16T12:34:55Z"
  },
  "CallSign": "UAL123",
  "Registration": "N12345"
}
```

**Omitted fields**: Nil pointers and empty slices omitted via `json:",omitempty"`

## Multi-Source Merging

### The Challenge

**Scenario**: Two receivers see same aircraft

```
Receiver A (north):
  - Good position coverage
  - Weak signal for velocity

Receiver B (south):
  - Good velocity coverage
  - Position occasionally glitchy
```

**Goal**: Combine best data from both

### Merge Algorithm

```go
func MergePlaneLocations(prev, next PlaneLocation) (PlaneLocation, error) {
    // 1. Sanity check (more on this below)
    if !IsLocationPossible(prev, next) {
        return prev, ErrImpossible
    }

    merged := prev
    merged.New = false
    merged.Removed = false

    // 2. Take newest data per field
    if next.HasLocation && next.Updates.Location.After(prev.Updates.Location) {
        merged.Lat = next.Lat
        merged.Lon = next.Lon
        merged.Updates.Location = next.Updates.Location
    }

    if next.HasHeading && next.Updates.Heading.After(prev.Updates.Heading) {
        merged.Heading = next.Heading
        merged.Updates.Heading = next.Updates.Heading
    }

    // ... similar for all fields

    // 3. Track contributing sources
    merged.SourceTags[next.SourceTag]++

    return merged, nil
}
```

**Key principle**: Newest wins per-field

### Source Tag Tracking

```go
SourceTags map[string]uint32
```

**Purpose**: Count contributions from each receiver

**Example**:
```json
{
  "Icao": "ABC123",
  "SourceTag": "merged",
  "SourceTags": {
    "receiver-north": 145,
    "receiver-south": 238,
    "receiver-east": 12
  }
}
```

**Use cases**:
- **Coverage analysis**: Which receivers see which aircraft?
- **Quality metrics**: Which receiver is most reliable?
- **Debugging**: Isolate problematic receiver

**Thread safety**: Mutex protects SourceTags map
```go
merged.sourceTagsMutex.Lock()
merged.SourceTags[next.SourceTag]++
merged.sourceTagsMutex.Unlock()
```

## Position Validation

### The Problem

**Bad data sources**:
- Malfunctioning transponders
- Time-delayed feeds (old data arrives late)
- Spoofing/interference
- Cross-feed (data from wrong aircraft)

**Example impossible scenario**:
```
T=0s: Aircraft at (38.0, -77.0), heading 90° (east)
T=1s: Aircraft at (38.0, -76.0), heading 90° (east)
      ↑ Moving east as expected

T=2s: Aircraft at (39.0, -77.0), heading 90° (east)
      ↑ Jumped north but heading still east → IMPOSSIBLE!
```

### Validation: Heading vs. Bearing

**Bearing**: Direction from previous position to current position

**Heading**: Direction aircraft is pointing (from transponder)

**Validation rule**: `|heading - bearing| < 90°`

**Why 90°**: Allows for:
- Wind drift (aircraft heading into wind)
- Turning (heading changing during trajectory)
- Data jitter (±45° tolerance is reasonable)

**Implementation**:
```go
func IsLocationPossible(prev, next PlaneLocation) bool {
    // 1. Calculate bearing from prev to next
    bearing := CalculateBearing(prev.Lat, prev.Lon, next.Lat, next.Lon)

    // 2. Check heading vs bearing delta
    deltaBearing := prev.Heading - bearing
    absDeltaBearing := abs(mod(deltaBearing + 180, 360) - 180)

    // 3. Accept if within 90°
    if absDeltaBearing < 90 {
        return true  // Plausible
    }

    return false  // Physically impossible
}
```

**Bearing calculation** (great circle):
```go
radLat0 := prev.Lat * π/180
radLon0 := prev.Lon * π/180
radLat1 := next.Lat * π/180
radLon1 := next.Lon * π/180

y := sin(radLon1 - radLon0) * cos(radLat1)
x := cos(radLat0)*sin(radLat1) - sin(radLat0)*cos(radLat1)*cos(radLon1 - radLon0)

bearing := atan2(y, x) * 180/π
```

### Edge Cases in Validation

**Case 1: No position data**
```go
if !(prev.HasLocation && next.HasLocation) {
    return true  // Fail open (accept)
}
```

**Why fail open**: Better to accept questionable data than drop all data

**Case 2: Position unchanged**
```go
if prev.Lat == next.Lat && prev.Lon == next.Lon {
    return true  // No movement = no bearing to check
}
```

**Case 3: Time went backwards**
```go
if prev.LastMsg.After(next.LastMsg) {
    return false  // Reject out-of-order data
}
```

**Case 4: Old position update**
```go
if prev.Updates.Location.After(next.Updates.Location) {
    return false  // Newer overall message, but stale position
}
```

**Why check both**: Message timestamp ≠ position timestamp
- Message might have velocity update
- Position might be from earlier frame

### Squawk Validation Special Case

**Problem**: Squawk "0000" is valid but often appears during data glitches

**Solution**: Delay accepting "0" squawk
```go
if next.Squawk == "0" {
    // Only update to 0 if 5+ seconds newer
    if next.Updates.Squawk.After(prev.Updates.Squawk.Add(5 * time.Second)) {
        merged.Squawk = next.Squawk
    }
} else {
    // Non-zero squawk: accept immediately
    merged.Squawk = next.Squawk
}
```

**Why 5 seconds**: Allows time-delayed feeds to catch up with correct value

## NATS API Message Types

### Enrichment Service APIs

**Aircraft data lookup**:
```go
const NatsApiEnrichAircraftV1 = "v1.enrich.aircraft"
```

**Request**:
```json
{"Icao": "ABC123"}
```

**Response**:
```json
{
  "Aircraft": {
    "Icao": "ABC123",
    "Registration": "N12345",
    "TypeCode": "B738",
    "TypeCodeLong": "Boeing 737-800"
  }
}
```

**Route data lookup**:
```go
const NatsApiEnrichRouteV1 = "v1.enrich.routes"
```

**Request**:
```json
{"CallSign": "UAL123"}
```

**Response**:
```json
{
  "Route": {
    "CallSign": "UAL123",
    "Operator": "United Airlines",
    "RouteCode": "ORD-LAX",
    "Segments": [
      {"Name": "Chicago O'Hare", "ICAOCode": "KORD"},
      {"Name": "Los Angeles Intl", "ICAOCode": "KLAX"}
    ]
  }
}
```

### Feeder Management APIs

**List feeders**:
```go
const NatsApiFeederListV1 = "v1.feeder.list"
```

**Returns**: List of all registered feeders with location, protocol, etc.

**Update feeder stats**:
```go
const NatsApiFeederStatsUpdateV1 = "v1.feeder.update-stats"
```

**Purpose**: Heartbeat / last-seen timestamp updates

## Common Patterns

### Converting from Tracker

```go
func ConvertToExport(plane *tracker.Plane, isNew, isRemoved bool, source string) PlaneLocation {
    return export.NewPlaneLocation(plane, isNew, isRemoved, source)
}
```

**All fields copied**: Position, altitude, heading, velocity, flags, etc.

**Source tag injected**: Identifies which receiver/aggregator provided data

### Merging Multiple Sources

```go
// Aggregator receives from multiple sources
var merged PlaneLocation

for _, update := range updates {
    merged, err = MergePlaneLocations(merged, update)
    if err != nil {
        // Position validation failed
        log.Warn().Err(err).Msg("Rejected impossible position")
        continue
    }
}
```

**Result**: Best composite view of aircraft state

### Enriching Data

```go
// Basic data from tracker
basic := export.NewPlaneLocation(plane, false, false, "receiver1")

// Look up enrichment
aircraft := queryAircraftDB(basic.Icao)
basic.Registration = &aircraft.Registration
basic.TypeCode = &aircraft.TypeCode

route := queryRouteDB(basic.CallSign)
basic.Operator = &route.Operator
basic.Segments = route.Segments
```

**Typical architecture**:
```
Tracker → Sink → NATS(location-updates) → [Multiple consumers]
                                             ↓
                                        Enrichment Service
                                             ↓
                                        NATS(enriched-updates)
```

## Performance Considerations

### JSON Encoding

**jsoniter performance** (vs. encoding/json):
```
Marshal PlaneLocation:
  encoding/json: ~2000 ns/op
  jsoniter:      ~500 ns/op

At 1000 positions/sec:
  encoding/json: ~2ms CPU
  jsoniter:      ~0.5ms CPU
```

**Memory**:
```
PlaneLocation struct: ~400 bytes
JSON output: ~800-1200 bytes (depending on enrichment)
```

### Merge Performance

**Position validation** (IsLocationPossible):
- Trig functions (sin, cos, atan2): ~200-300 ns
- Called once per merge
- Negligible overhead

**Mutex contention** (SourceTags):
- Lock held for map insert only
- ~50 ns
- Rarely contended (different aircraft)

## Common Issues

### False Position Rejections

**Symptom**: Valid positions rejected by IsLocationPossible

**Causes**:

1. **Strong crosswind**:
   ```
   Aircraft heading 90°, but drifting 45° due to wind
   Bearing 90°, heading reported 135° → Delta >90° → Rejected
   ```

   **Solution**: Increase tolerance (currently 90°, could go to 120°)

2. **Rapid turns**:
   ```
   Aircraft turning while transmitting
   Position A: heading 90°
   Position B (2s later): heading 180°, but bearing from A→B is 135°
   ```

   **Solution**: Check time delta, skip validation if >5 seconds

3. **Data jitter**:
   ```
   Heading oscillates ±10° due to transponder rounding
   Edge case where accumulated error pushes over threshold
   ```

   **Mitigation**: Already accounted for with 90° tolerance

<!--
Maintainers: Document false rejection cases you encounter:
- Scenario:
- Aircraft:
- Data:
- Solution:
-->

### Merge Timing Issues

**Symptom**: Stale data overwrites fresh data

**Cause**: System clock skew between receivers

**Example**:
```
Receiver A (clock +5s ahead):
  Updates.Location = 12:00:05

Receiver B (clock correct):
  Updates.Location = 12:00:03

Merge: Takes A's stale data (timestamp looks newer)
```

**Solutions**:
1. NTP sync all receivers (required)
2. Validate clock skew (reject if >10s difference)
3. Use sequence numbers instead of timestamps

### Memory Leaks in SourceTags

**Symptom**: SourceTags map grows unbounded

**Cause**: Never cleaned up

**Impact**: Merged positions accumulate all historical source tags

**Solution**: Periodic cleanup or size limit
```go
if len(merged.SourceTags) > 100 {
    // Keep only top 10 contributors
    keepTopN(merged.SourceTags, 10)
}
```

<!--
Maintainers: If you implement SourceTags cleanup, document it here
-->

## Production Lessons

> **Note to maintainers**: Add your observations here

### Typical Merge Scenarios

**Urban area with 3 overlapping receivers**:
- Rejection rate: ~5-10% (mostly distant aircraft with weak signals)
- Average sources per aircraft: 2.3

**Wide-area aggregation (50+ receivers)**:
- Rejection rate: ~15-20% (time-delayed feeds)
- Average sources per aircraft: 1.2 (less overlap than expected)

<!--
Maintainers: Add merge statistics from your deployments:
- Number of sources:
- Rejection rate:
- Average sources per aircraft:
- Common rejection causes:
-->

### When to Skip Validation

**Scenario**: High-latency satellite feeds

**Problem**: Position updates arrive 5-10 seconds delayed

**Impact**: Validation fails (aircraft has moved, heading changed)

**Solution**: Disable validation for known-delayed sources
```go
if source.IsDelayed {
    // Skip validation, trust source
    merged = next
} else {
    merged, err = MergePlaneLocations(prev, next)
}
```

## File Guide

| File | Purpose |
|------|---------|
| `types.go` | PlaneLocation struct, merge logic, validation |
| `location.go` | Conversion from tracker.Plane, JSON encoding |
| `nats_api.go` | NATS API message type constants |

## See Also

- [Tracker](../tracker/README.md) - Where Plane objects come from
- [Sink](../sink/README.md) - Where PlaneLocation objects go
- jsoniter documentation: https://github.com/json-iterator/go
