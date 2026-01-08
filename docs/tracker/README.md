# Aircraft Tracker

## Overview

The `lib/tracker` package is the core aircraft tracking engine for the plane.watch pipeline. It orchestrates the real-time tracking of aircraft by consuming ADS-B and Mode S messages from various sources, maintaining aircraft state, and emitting position updates.

## Architecture

The tracker follows a producer-consumer-sink architecture:

```
[Producers] → [Middlewares] → [Tracker] → [Sink]
     ↓             ↓              ↓          ↓
  Raw Data    Transform     State Mgmt   Events
```

### Key Components

1. **Tracker** (`tracker.go`) - Central orchestrator managing aircraft state
2. **Plane** (`plane.go`) - Individual aircraft state machine
3. **Producers** - Data sources (Beast, SBS1 formats)
4. **Middlewares** - Frame transformation pipeline
5. **Sink** - Event consumer for downstream processing

## Why This Design?

### Producer/Consumer Pattern

**Problem**: Aircraft data arrives from multiple sources in different formats (Beast binary, SBS1 text, raw Mode S) at varying rates.

**Solution**: The producer pattern abstracts data sources behind a common `Producer` interface. Each producer:
- Manages its own connection lifecycle
- Converts proprietary formats to internal `Frame` types
- Pushes frames into worker pools for parallel processing

**Why it matters**: This allows the tracker to scale horizontally - adding more receivers is just adding more producers, without changing core tracking logic.

### Forgetful Map for Aircraft State

**Problem**: Aircraft come and go. Maintaining unbounded state for every aircraft ever seen would cause memory exhaustion.

**Solution**: Custom `ForgetfulSyncMap` implementation that:
- Automatically evicts aircraft not seen for `pruneAfter` duration (default: 5 minutes)
- Sweeps for stale entries every `pruneTick` interval (default: 10 seconds)
- Provides pre-eviction callbacks for cleanup

**Why it matters**: In busy airspace, hundreds of aircraft may be tracked simultaneously. Without automatic eviction, memory would grow unbounded as aircraft transition through coverage areas.

### Frame Count Threshold for Viability

**Problem**: Single-frame detections are often noise, interference, or malformed data.

**Solution**: Aircraft must transmit `numFramesToBeViable` frames (default: 1, configurable up to ~3) before the tracker emits events.

**Why it matters**: Reduces false positives in the data pipeline. Most spurious frames are isolated - real aircraft transmit continuously (2Hz for position, 1Hz for velocity).

### Parallel Decode Workers

**Problem**: Frame decoding (CRC validation, field extraction, CPR decoding) is CPU-intensive.

**Solution**: Each producer spawns `decodeWorkerCount` goroutines (default: 5) consuming from a shared channel.

**Why it matters**: On busy receivers (>1000 msg/sec), single-threaded decoding becomes a bottleneck. Parallel workers saturate available CPU cores.

## Data Flow

### 1. Frame Reception
```
Producer.Listen() → FrameEvent channel → Decode Workers
```

Each producer runs in its own goroutine, continuously reading frames and publishing to a channel.

### 2. Decoding & Validation
```
Raw Frame → Decode() → CRC Check → ICAO Extraction → Middleware Pipeline
```

**Why CRC matters**: Mode S uses 24-bit CRC for error detection. Invalid frames are discarded immediately.

**Why ICAO filtering matters**: Not all extracted ICAOs are aircraft. Some are:
- Interrogator IDs (DF0/4/5/16) - filtered using frame count thresholds
- Corrupted data - filtered via CRC

See `mode_s/crc.go` for detailed ICAO extraction logic.

### 3. State Update
```
Frame → GetPlane(ICAO) → Plane.HandleModeSFrame() → State Machine
```

Each aircraft is a state machine tracking:
- **Position**: CPR-decoded lat/lon, altitude
- **Motion**: Heading, velocity, vertical rate
- **Identity**: Flight number, squawk, registration
- **Status**: On-ground, alert, emergency

### 4. Event Emission
```
State Change → hasChanged flag → Sink.OnEvent(PlaneLocationEvent)
```

**Why `hasChanged`**: Events are only emitted when state actually changes, reducing downstream load.

## Compact Position Reporting (CPR)

**The Challenge**: Aircraft broadcast positions in a compact format to save bandwidth.

**How it works**:
- Positions encoded as 17-bit integers (not lat/lon degrees)
- Requires pairing ODD and EVEN frames to decode
- Valid for ~60 seconds (aircraft velocity limits)

**Why pairs**: Each frame divides the globe into different zone grids. The intersection of ODD and EVEN grids gives the actual position.

**See**: `cpr.go` for full decoding algorithm, including:
- Global airborne decoding
- Surface position decoding (requires reference position)
- Velocity sanity checking (rejects impossible jumps)

## Message Throttling & Frame Count

**Problem**: Some aircraft malfunction and spam the frequency.

**Solution**: The tracker limits state updates based on frame maturity:

```go
if hasChanged && p.MsgCount() > uint64(p.tracker.numFramesToBeViable) {
    p.tracker.sink.OnEvent(NewPlaneLocationEvent(p))
}
```

**Why it matters**: Prevents a single misbehaving transponder from flooding the pipeline with events for every frame received.

## Concurrency & Thread Safety

### Locking Strategy

**Plane-level locks**: Each `Plane` has its own `sync.RWMutex`. Most operations are reads (many consumers checking positions), so RW locks reduce contention.

**Tracker-level locks**: Minimal. Only producer list modification requires locking.

**Why this matters**: At high message rates (>5000 msg/sec across all aircraft), lock contention would serialize updates and create a bottleneck.

### Worker Pool Shutdown

The tracker has a carefully orchestrated shutdown sequence:

```
1. Stop producers (no more frames)
2. Wait for decode workers to drain
3. Wait for middleware pipeline
4. Stop sink
```

**Why the order matters**: Each stage depends on the previous. Stopping the sink before workers drain would lose in-flight frames.

## Monitoring & Statistics

The tracker exposes Prometheus metrics:

- `currentPlanes` - Currently tracked aircraft count
- `decodedFrames` - Total frames processed
- `erroredFrames` - Frames that failed decoding
- `purgedBeforeViable` - Aircraft evicted before reaching viability threshold

**Why these metrics**: They reveal:
- Coverage area size (currentPlanes)
- Receiver health (erroredFrames ratio)
- Noise floor (purgedBeforeViable)

## Configuration Options

### `WithDecodeWorkerCount(n int)`
**Default**: 5
**Why**: Balance between CPU usage and decode latency. More workers = more throughput but diminishing returns due to lock contention.

### `WithNumFramesToBeViable(n int)`
**Default**: 1
**Why**: Higher values reduce false positives but increase latency to first position fix. Busy airspace can tolerate higher thresholds.

### `WithPruneTiming(tick, after time.Duration)`
**Defaults**: 10s sweep, 5min eviction
**Why**: More frequent sweeps catch stale aircraft faster but increase CPU overhead. Longer eviction windows track aircraft through coverage gaps.

## Performance Characteristics

**Memory**: ~2-5KB per tracked aircraft (mostly frame history for debugging)

**CPU**: Dominated by CRC calculation and CPR decoding. Scales linearly with frame rate.

**Latency**: Typically <10ms from frame reception to event emission for viable aircraft.

## Integration Points

### Adding a New Producer

```go
producer := NewMyProducer(...)
tracker.AddProducer(producer)
```

Producer must implement:
```go
type Producer interface {
    Listen() chan FrameEvent
    Stop()
    Source() *FrameSource
    String() string
    HealthCheck() bool
    HealthCheckName() string
}
```

### Adding Middleware

```go
middleware := NewMyMiddleware(...)
tracker.AddMiddleware(middleware)
```

Middleware can:
- Transform frames (e.g., inject reference positions)
- Filter frames (return nil to drop)
- Enrich frames (add metadata)

### Consuming Events

```go
type MySink struct{}

func (s *MySink) OnEvent(event Event) {
    planeEvent, ok := event.(*PlaneLocationEvent)
    if !ok {
        return
    }
    plane := planeEvent.Plane()
    // Use plane.Lat(), plane.Lon(), etc.
}
```

## Common Issues & Debugging

### No positions decoded

**Symptom**: Aircraft have ICAOs but no lat/lon

**Causes**:
1. Missing ODD or EVEN CPR frames - check frame history
2. Invalid velocity checks - aircraft moving too fast between frames
3. Surface positions without reference - need receiver location

**Debug**: Enable `TRACE` logging to see CPR decode attempts

### High `purgedBeforeViable` count

**Symptom**: Many aircraft evicted before viable

**Causes**:
1. High RF noise - spurious detections
2. `numFramesToBeViable` too high for coverage area
3. Aircraft at edge of range - only receiving fragments

**Debug**: Lower threshold or check antenna/receiver health

### Memory growth

**Symptom**: Memory usage increases over time

**Causes**:
1. `pruneAfter` too long - adjust down
2. Sink blocked - check downstream pipeline
3. Frame history accumulation - only stored in debug mode

**Debug**: Check goroutine counts, heap profiles

## File Guide

| File | Purpose |
|------|---------|
| `tracker.go` | Main orchestrator, producer/sink/middleware management |
| `plane.go` | Aircraft state machine, position/velocity/identity tracking |
| `cpr.go` | CPR position decoding (ODD/EVEN frame pairing) |
| `frame-list.go` | Lossy circular buffer for frame history (debugging) |
| `event.go` | Event types and definitions |
| `input.go` | Frame, Producer, Sink interfaces |

## See Also

- [Mode S Decoding](mode_s/README.md) - Low-level frame parsing
- [Beast Format](beast/README.md) - Binary Mode S format
- [SBS1 Format](sbs1/README.md) - Basestation text format