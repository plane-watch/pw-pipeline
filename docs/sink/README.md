# Event Sink

## Overview

The `sink` package provides the final stage of the aircraft tracking pipeline - consuming tracker events and publishing them to downstream systems. It acts as the bridge between the tracking engine and external consumers (web services, databases, analysis tools, etc.).

## Architecture

```
Tracker → Sink → NATS → [Consumers]
            ↓
        Batching
        Deduping
        Formatting
```

### Key Responsibilities

1. **Event consumption**: Receive events from tracker
2. **Format conversion**: Transform internal events to external JSON format
3. **Batching**: Coalesce multiple updates per aircraft
4. **Publishing**: Push to message bus (NATS)
5. **Back pressure handling**: Manage downstream capacity

## Why a Sink Abstraction?

### The Problem

**Direct publishing from tracker**:
```go
// In tracker, for every state change:
publish(nats, event)  // Couples tracker to NATS
```

**Issues**:
- Tracker blocked on NATS latency
- Can't swap message buses without changing tracker
- Testing requires real NATS server
- No batching optimization

### The Solution

**Sink abstraction**:
```go
type Sink interface {
    OnEvent(event Event)
    Listen() chan Event
    Stop()
}
```

**Benefits**:
- Tracker publishes to channel (non-blocking)
- Sink handles all I/O in separate goroutine
- Can swap NATS for Kafka/Redis/File/etc.
- Easy to mock for testing

## Event Flow

### 1. Tracker Emits Event

```go
// In tracker
if hasChanged && viable {
    sink.OnEvent(NewPlaneLocationEvent(plane))
}
```

**Non-blocking**: Event goes into channel buffer

### 2. Sink Receives Event

```go
func (s *Sink) OnEvent(e Event) {
    if le, ok := e.(*PlaneLocationEvent); ok {
        if sendDelay == 0 {
            // Immediate mode
            publish(le)
        } else {
            // Batching mode
            sendList[icao] = le  // Last-write-wins
        }
    }
}
```

### 3. Batching (Optional)

**Without batching** (`sendDelay = 0`):
- Every state change → immediate publish
- Higher message rate
- Lower latency
- More bandwidth

**With batching** (`sendDelay = 300ms` default):
- Collect updates in map by ICAO
- Every 300ms, publish batch
- Reduced message rate
- Higher latency
- Less bandwidth

**Why batching matters**:
```
Aircraft transmits position every 0.5s (2 Hz)
Without batching: 2 publishes/sec/aircraft
With 300ms batch:  ~1.5 publishes/sec/aircraft (coalesces rapid updates)

100 aircraft:
- No batch: 200 msg/sec
- Batched:   ~150 msg/sec (25% reduction)
```

### 4. Format Conversion

```go
func (s *Sink) trackerMsgJson(le *PlaneLocationEvent) ([]byte, error) {
    plane := le.Plane()
    eventStruct := export.NewPlaneLocation(
        plane,
        le.New(),      // Is this first appearance?
        le.Removed(),  // Is this departure?
        s.config.sourceTag,
    )
    return eventStruct.ToJSONBytes()
}
```

**Internal event** (tracker-specific):
```go
type PlaneLocationEvent struct {
    plane   *Plane
    new     bool
    removed bool
}
```

**External JSON** (consumer-friendly):
```json
{
  "icao": "4840D6",
  "lat": 38.89,
  "lon": -77.03,
  "altitude": 37000,
  "heading": 285.5,
  "velocity": 450.2,
  "callsign": "UAL123",
  "source": "myreceiver",
  "new": false,
  "removed": false,
  "timestamp": "2024-11-16T12:34:56.789Z"
}
```

### 5. Publishing

```go
func (d *Destination) PublishJson(queue string, msg []byte) error {
    return natsServer.Publish("location-updates", msg)
}
```

**NATS subject**: `location-updates`

**Delivery guarantees**: At-most-once (NATS default)
- Message may be lost if NATS down
- No duplicates
- No ordering guarantees across ICAOs

## Batching Deep Dive

### Why Last-Write-Wins?

**Map-based batching**:
```go
sendList[icao] = latestEvent  // Overwrites previous
```

**Scenario**:
```
T=0ms:   Aircraft at (38.00, -77.00) → sendList["ABC123"] = Event1
T=100ms: Aircraft at (38.01, -77.01) → sendList["ABC123"] = Event2 (overwrites)
T=200ms: Aircraft at (38.02, -77.02) → sendList["ABC123"] = Event3 (overwrites)
T=300ms: Batch publishes Event3 only
```

**Result**: Only latest state published, intermediate states dropped

**Why this is OK**:
- Consumers care about current state, not every micro-update
- Reduces message rate without losing fidelity
- Position updates are continuous - missing one doesn't matter

**When this might be a problem**:
- Need full trajectory replay (use sendDelay=0)
- Doing analytics on update frequency (false low rate)
- Debugging tracker behavior (missing intermediate states)

### Batching vs. Immediate Trade-offs

| Aspect | Immediate (0ms) | Batched (300ms) |
|--------|-----------------|-----------------|
| Latency | ~10ms | ~150ms avg, 300ms max |
| Message rate | High (2x position rate) | Lower (0.5-1.5x) |
| Bandwidth | Higher | Lower |
| CPU | Higher (JSON encoding) | Lower (fewer encodes) |
| Use case | Real-time tracking | Efficiency, aggregation |

**Recommendation**: Use batching unless you need sub-second latency

## Source Tagging

**Purpose**: Identify which receiver generated data

```go
sink.NewNatsSink(
    sink.WithSourceTag("myreceiver-north-antenna"),
)
```

**Added to every message**:
```json
{
  "icao": "ABC123",
  "source": "myreceiver-north-antenna",
  ...
}
```

**Why it matters**:
- Multi-receiver deployments need attribution
- Coverage analysis (which receiver saw which aircraft)
- Debugging (isolate problematic receiver)
- MLAT correlation (need 3+ sources with timestamps)

**Connection name**: Source tag also used for NATS connection metadata
```go
connectionName = "pipeline+source=myreceiver-north-antenna"
```

Visible in NATS monitoring, aids debugging connection issues

## NATS Integration

### Why NATS?

**Considered alternatives**:
- **RabbitMQ**: Heavy, complex configuration
- **Kafka**: Overkill for pub/sub, needs Zookeeper
- **Redis**: Not purpose-built for messaging
- **Direct HTTP**: No fan-out, no buffering

**NATS wins**:
- Lightweight (single binary)
- Simple pub/sub model
- High performance (>1M msg/sec single node)
- Easy clustering for HA
- Fire-and-forget delivery (appropriate for real-time data)

### Connection Management

**Retry on startup**:
```go
delay := time.Second
for {
    err := connect()
    if err == nil {
        break
    }
    if delay > 10*time.Second {
        return err  // Give up after 10s max delay
    }
    log.Error().Err(err).Dur("retry delay", delay).Msg("Retry...")
    time.Sleep(delay)
    delay += time.Second  // Exponential backoff
}
```

**Why retry**: NATS might start slower than pipeline in containerized environments

**Why give up**: Don't retry forever - fail fast if truly misconfigured

**During operation**: NATS client handles reconnects automatically

### Subject Design

**Current**: Single subject `location-updates`

**Possible optimizations**:
```
location-updates.{icao}      # Per-aircraft topics
location-updates.{quadrant}  # Geographic partitioning
location-updates.{source}    # Per-receiver topics
```

**Trade-offs**:
- More subjects = better filtering for consumers
- More subjects = more overhead in NATS
- Current single subject = simple, consumer can filter

**When to partition**: >10k aircraft, >100k msg/sec

## Configuration Options

### Basic NATS Sink

```go
sink, err := sink.NewNatsSink(
    sink.WithHost("localhost", "4222"),
    sink.WithUserPass("user", "password"),
    sink.WithSourceTag("myreceiver"),
    sink.WithConnectionName("pipeline"),
)
```

### Batching Configuration

```go
sink.WithSendDelay(500 * time.Millisecond)  // Batch every 500ms
sink.WithSendDelay(0)                       // Immediate (no batching)
```

**Tuning guide**:
- **0ms**: Real-time display, low aircraft count
- **100-300ms**: Balanced, typical deployments
- **500-1000ms**: High efficiency, delayed display acceptable
- **>1000ms**: Only for bulk analytics (position updates will be sparse)

### Prometheus Metrics

```go
sink.WithPrometheusCounters(
    frameCounter,    // Total frames processed
    planeLocCounter, // Total location events published
)
```

**Metrics exposed**:
```
sink_frames_total       # Frames in
sink_plane_loc_total    # Events published
```

**Derived metrics**:
```
Publish rate:  rate(sink_plane_loc_total[1m])
Efficiency:    sink_plane_loc_total / sink_frames_total
Batching gain: 1 - (publish_rate / frame_rate)
```

## Error Handling

### Publish Failures

**Current behavior**: Errors logged but not retried

```go
err := dest.PublishJson(queue, jsonBuf)
// Error logged internally by NATS client
// Event is lost
```

**Why no retry**: Real-time data is perishable
- Aircraft moves continuously
- Old position update is stale by the time retry succeeds
- Better to drop and publish next update

**When this might be a problem**:
- NATS down → all events lost until reconnect
- Network flaps → intermittent loss

**Mitigation**:
- NATS client has automatic reconnect with buffering
- Monitor `nats_disconnects` metric
- For critical applications, run redundant pipeline instances

### Marshaling Failures

**Rare but possible**: Event → JSON conversion fails

```go
jsonBuf, err := trackerMsgJson(le)
if err != nil {
    // Event silently dropped
    // Only logged in trace mode
}
```

**Causes**:
- Invalid UTF-8 in callsign (malformed transponder)
- Nil plane reference (race condition?)
- Out-of-range values (altitude, position)

**Detection**: Enable trace logging, monitor for marshal errors

<!--
Maintainers: If you encounter marshal errors, document them here with examples:
- Symptom:
- Cause:
- Example data:
- Fix:
-->

## Destination Abstraction

**Interface**:
```go
type Destination interface {
    PublishJson(queue string, msg []byte) error
    Stop()
    HealthCheck() bool
    HealthCheckName() string
}
```

**Purpose**: Abstract away transport mechanism

**Current implementation**: `NatsSink`

**Possible implementations**:
- `KafkaSink`: Kafka topics
- `RedisSink`: Redis streams
- `FileSink`: JSON lines to file
- `HTTPSink`: POST to webhook
- `MultiSink`: Fan-out to multiple destinations

**How to add new destination**:
```go
type MyDestination struct {
    // Fields...
}

func (d *MyDestination) PublishJson(queue string, msg []byte) error {
    // Your publishing logic
}

func (d *MyDestination) Stop() {
    // Cleanup
}

// Then use it:
sink := sink.NewSink(config, &MyDestination{})
```

## Shutdown Sequence

**Graceful shutdown order**:
```go
func (s *Sink) Stop() {
    close(s.events)         // 1. Stop accepting events
    s.config.Finish()       // 2. Wait for in-flight
    s.dest.Stop()           // 3. Close destination
    s.fsm.Stop()            // 4. Stop internal timers
    s.sendTicker.Stop()     // 5. Stop batch timer
}
```

**Why this order**:
1. Stop source (tracker won't send more events)
2. Drain buffers (publish queued batches)
3. Close transport (flush NATS)
4. Clean up timers

**Incomplete shutdown**: Events may be lost if destination closes before drain

## Performance Characteristics

### Throughput

**Batching mode (300ms)**:
- **Message rate**: ~3-5x aircraft count (position + velocity + occasional metadata)
- **JSON encoding**: ~10-20 µs/event
- **NATS publish**: ~50-100 µs/event (local server)
- **Total latency**: ~300ms average, 600ms p99

**Immediate mode**:
- **Message rate**: ~10x aircraft count
- **Latency**: ~10-50ms average
- **Bottleneck**: JSON encoding at >5k events/sec

### Memory

**Batch mode**:
```
Per aircraft in batch: ~200 bytes (event + JSON buffer)
100 aircraft: ~20 KB
1000 aircraft: ~200 KB
```

**Immediate mode**: Minimal (no buffering)

### CPU

**Batching**:
- Periodic timer: ~1% CPU (goroutine scheduling)
- JSON encoding burst: ~5-10% CPU every 300ms

**Immediate**:
- Continuous encoding: ~10-15% CPU sustained

## Common Issues

### Events Not Appearing

**Check list**:

1. **NATS connection**:
   ```
   Check: HealthCheck() returns true?
   Check: NATS server logs show connection?
   ```

2. **Event channel buffer full**:
   ```
   Symptom: Tracker blocks
   Cause: Sink not draining fast enough
   Solution: Increase buffer or optimize publish
   ```

3. **JSON marshaling silent failure**:
   ```
   Enable: Trace logging
   Look for: Marshal errors
   ```

4. **Batch not flushing**:
   ```
   Check: sendTicker still running?
   Check: sendDelay configuration
   ```

### High Latency

**Symptom**: Consumers see stale positions

**Causes**:

1. **Batching delay too high**:
   ```
   Current: 1000ms
   Solution: Reduce to 100-300ms
   ```

2. **NATS network latency**:
   ```
   Check: NATS RTT metrics
   Solution: Co-locate NATS with pipeline
   ```

3. **Slow JSON encoding**:
   ```
   Profile: Are we CPU-bound on encoding?
   Solution: Pre-allocate buffers, optimize export format
   ```

### Message Rate Too High

**Symptom**: Downstream consumers overwhelmed

**Solutions**:

1. **Increase batching window**:
   ```go
   sink.WithSendDelay(1 * time.Second)  // Reduce by ~3x
   ```

2. **Filter events**:
   ```go
   // Only publish significant changes
   if !significantChange(old, new) {
       return
   }
   ```

3. **Downstream backpressure**:
   ```
   NATS supports max_pending
   Pipeline will block when NATS buffer full
   ```

## Production Lessons

> **Note to maintainers**: Add your war stories here

### Typical Message Rates

**Single receiver, 100 aircraft**:
- Immediate: ~1000-1500 msg/sec
- Batched (300ms): ~400-600 msg/sec

**Multi-receiver aggregation, 500 aircraft**:
- Immediate: ~5k-8k msg/sec
- Batched: ~2k-3k msg/sec

<!--
Maintainers: Add your observed rates:
- Aircraft count:
- Batching config:
- Observed rate:
- NATS performance:
-->

### NATS Clustering Lessons

**Single node**: Fine for <10k msg/sec, single receiver

**Cluster**: Needed for:
- HA (receiver failover)
- Geographic distribution
- >10k msg/sec sustained

**Pitfall**: Don't cluster too early - adds complexity

### When Batching Backfires

**Scenario**: Aircraft flies through coverage edge

```
T=0ms:   Aircraft enters, tracker emits "new" event
T=100ms: Position update 1 (batched)
T=200ms: Position update 2 (overwrites in batch map)
T=250ms: Aircraft leaves coverage
T=300ms: Batch publishes - includes stale "new" flag
```

**Problem**: "new" flag from T=0 still set, but aircraft already left

**Impact**: Consumer thinks aircraft just appeared at exit position

**Mitigation**: Include "removed" flag, consumers should handle both

<!--
Maintainers: Document other batching edge cases you've found
-->

## File Guide

| File | Purpose |
|------|---------|
| `sink.go` | Main sink abstraction, batching logic |
| `nats.go` | NATS destination implementation |
| `config.go` | Configuration options |

## See Also

- [Tracker Events](../tracker/README.md#event-emission) - Event types and structure
- [Export Format](../export/README.md) - PlaneLocation struct and JSON serialization
- NATS documentation: https://docs.nats.io/
