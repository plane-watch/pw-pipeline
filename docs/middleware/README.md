# Middleware

## Overview

The `middleware` package provides two middleware components for the tracker pipeline: **Accounting** for feeder statistics tracking and **IngestTap** for real-time frame stream monitoring. Both implement the tracker middleware interface, allowing them to intercept and process frames as they flow through the pipeline.

## Middleware Pattern

### Interface

```go
type Middleware interface {
    Handle(event *FrameEvent) Frame
    String() string
    HealthCheckName() string
    HealthCheck() bool
    Stop()
}
```

**Handle method**: Receives every frame event, can:
- Inspect the frame
- Perform side effects (logging, metrics, forwarding)
- Pass frame through (return `event.Frame()`)
- Drop frame (return `nil`)

**Pass-through pattern**:
```go
func (m *MyMiddleware) Handle(event *FrameEvent) Frame {
    // Do something with event
    doSomething(event)

    // Always pass through (never drop)
    return event.Frame()
}
```

### Integration with Tracker

```
Producer → [Middleware 1] → [Middleware 2] → Tracker → Sink
              ↓                 ↓
           Accounting       IngestTap
```

**Middleware chain**:
```go
tracker := tracker.New()
tracker.AddMiddleware(middleware.NewAccounting())
tracker.AddMiddleware(middleware.NewIngestTap(natsServer))
```

**Order matters**: Middleware called in registration order
- First registered = first to see frames
- Last registered = last before tracker

## Accounting Middleware

### Purpose

Track frame counts per feeder and periodically send statistics to ATC (Air Traffic Control) backend via NATS.

**Use cases**:
- Monitor feeder health (frames/sec)
- Detect offline feeders (no frames recently)
- Billing/accounting (frame counts)
- Performance analytics

### Architecture

```
FrameEvent → Handle → handleQueue → queueHandler → Update stats
                                        ↓
                                   atcUpdateQueue → NATS Request
                                                      ↓
                                                    ATC Backend
```

**Two goroutines**:
1. `queueHandler`: Updates stats, queues periodic updates
2. `atcUpdateQueueHandler`: Sends updates to NATS

**Why two queues**: Separate concerns (stat tracking vs network I/O)

### Statistics Tracked

**Per-feeder stats**:
```go
type feederStat struct {
    apiKey        string      // Feeder identifier
    frameCount    uint64      // Total frames received
    lastSeen      time.Time   // Most recent frame timestamp
    lastAtcUpdate time.Time   // Last time we sent update to ATC
}
```

**Why track lastAtcUpdate**: Prevent spamming ATC with updates

### Update Frequency

**Update sent when**:
```go
if stat.lastSeen.After(stat.lastAtcUpdate.Add(time.Minute)) {
    a.atcUpdateQueue <- stat
    stat.lastAtcUpdate = stat.lastSeen
}
```

**Minimum 1 minute between updates per feeder**

**Why 1 minute**:
- Frequent enough for health monitoring
- Not so frequent as to overwhelm ATC backend
- Balances latency vs load

**Trade-offs**:
- **30 seconds**: More responsive, 2x network traffic
- **1 minute**: Good balance (current)
- **5 minutes**: Less traffic, slower to detect issues

### NATS Request Format

**Subject**: `v1.api.feeder.stats.update` (from `export.NatsApiFeederStatsUpdateV1`)

**Payload**:
```json
[
  {
    "api_key": "feeder-abc123",
    "last_seen": "2024-11-16T14:23:45Z"
  }
]
```

**Request pattern** (not publish):
```go
_, err = a.natsServer.Request(
    export.NatsApiFeederStatsUpdateV1,
    data,
    map[string]string{},
    time.Second,  // 1 second timeout
)
```

**Why Request instead of Publish**:
- Get acknowledgment from ATC
- Detect if ATC is down/slow
- Backpressure if ATC can't keep up

**Timeout**: 1 second
- Fast enough to not block pipeline
- Long enough for normal network latency
- Failure handled gracefully

### Error Handling

**Marshal failure**:
```go
if err != nil {
    a.log.Error().Err(err).Msg("failed to encode feeder update")
    continue  // Skip this update, try next
}
```

**Request failure**:
```go
if err != nil {
    a.log.Error().Err(err).Msg("failed to update feeder stats")
    return  // Exit handler (stops all updates)
}
```

**Asymmetric error handling**:
- **Marshal error**: Continue (recoverable, might be transient)
- **Request error**: Exit (network issue, stop flooding logs)

**Consequence of exit**: No more updates sent
- Feeder stats stop updating in ATC
- Middleware still tracks stats locally
- Restart required to resume updates

**Improvement opportunity**: Retry logic instead of exit

<!--
Maintainers: Consider adding retry logic for NATS request failures:
- Retry with exponential backoff
- Circuit breaker pattern
- Continue after repeated failures (with rate limiting)
Document here if implemented
-->

### Configuration

**Initialization**:
```go
accounting := middleware.NewAccounting(
    middleware.WithNats(natsServer),
)
```

**Required**: NATS server
```go
if a.natsServer == nil {
    panic("You need to specify a NATS server")
}
```

**Why panic**: Accounting is useless without NATS, fail fast

**Pre-sized map**:
```go
a.stats = make(map[string]feederStat, 1000)
```

**Why 1000**: Expect hundreds of feeders in large deployment
- Avoids early reallocations
- Small memory cost (~40KB)

### Buffer Sizes

**Both queues: 1000 items**:
```go
a.handleQueue = make(chan *tracker.FrameEvent, 1000)
a.atcUpdateQueue = make(chan feederStat, 1000)
```

**Why 1000**:
- Handle burst traffic (multiple feeders sending simultaneously)
- Allows brief NATS slowdown without blocking
- Not so large as to hide problems

**Overflow behavior**: Handle() blocks if queue full
- Backpressure to producer
- Prevents unbounded memory growth
- Indicates NATS or ATC backend slowness

### Frame Count Accuracy

**Increments per frame**:
```go
stat.frameCount++
```

**When incremented**: For every frame that passes through middleware

**What's counted**: Raw frames, not viable aircraft
- Includes invalid frames
- Includes duplicate frames (if before dedupe middleware)
- Counts Mode S, Beast, SBS1 all the same

**Use for**:
- Relative activity measurement
- Comparing feeders
- Health checks (frame rate should be consistent)

**Don't use for**:
- Billing (count after validation instead)
- Exact aircraft count (use tracker stats)

### Shutdown Sequence

```go
func (a *Accounting) Stop() {
    close(a.handleQueue)         // 1. Stop accepting frames
    close(a.atcUpdateQueue)      // 2. Stop accepting updates
    a.exitQueueWaiter.Wait()     // 3. Wait for handlers to finish
}
```

**Graceful shutdown**:
1. Close input queue (no more frames)
2. Process remaining items in handleQueue
3. Process remaining updates in atcUpdateQueue
4. Both handlers exit
5. Return (safe to exit program)

**Final updates sent**: Yes, pending updates in queue will be sent

**Unsent stats**: Stats since last update are lost (no final flush)

<!--
Maintainers: Consider adding final flush on shutdown:
- Send one last update for each feeder before exiting
Document here if implemented
-->

## IngestTap Middleware

### Purpose

Dynamically subscribe to frame streams for specific feeders or aircraft. Acts as a "network tap" allowing real-time monitoring and debugging without modifying pipeline code.

**Use cases**:
- Debug specific aircraft behavior
- Monitor specific feeder
- Collect sample frames for analysis
- Real-time integration with external tools

### Architecture

```
NATS Request → requestHandler → Add/Remove condition
                                      ↓
FrameEvent → Handle → queue → processQueue → Match conditions
                                                ↓
                                            Send to NATS subject
```

**Components**:
1. **Condition list**: Linked list of tap filters
2. **Frame queue**: Buffered channel for frames to check
3. **8 worker goroutines**: Process queue in parallel
4. **NATS subscription**: Receive add/remove requests

**Why 8 workers**: Balance parallelism vs overhead
- 1 worker: Single-threaded bottleneck
- 8 workers: Good CPU utilization on multi-core
- 16+ workers: Diminishing returns, more contention

### Condition Matching

**Condition structure**:
```go
type condition struct {
    nextItem *condition  // Linked list next
    prevItem *condition  // Linked list prev
    queue    string      // NATS subject to send matches
    apiKey   string      // Filter by feeder (empty = any)
    icao     uint32      // Filter by aircraft (0 = any)
}
```

**Linked list pattern**: Easy add/remove at runtime

**Match logic**:
```go
func (c *condition) match(fe *FrameEvent) bool {
    // Both must match if specified
    isMatchAPIKey := (c.apiKey == "" || source.Tag == c.apiKey)
    isMatchIcao := (c.icao == 0 || frame.Icao() == c.icao)

    return isMatchAPIKey && isMatchIcao
}
```

**AND logic**: Both API key and ICAO must match

**Wildcard support**:
- Empty apiKey = match all feeders
- Zero icao = match all aircraft
- Both empty = match everything (firehose)

**Examples**:
```go
// Specific aircraft from specific feeder
{apiKey: "feeder-123", icao: 0xABC123}

// All frames from specific feeder
{apiKey: "feeder-123", icao: 0}

// Specific aircraft from any feeder
{apiKey: "", icao: 0xABC123}

// Everything (careful!)
{apiKey: "", icao: 0}
```

### Adding a Tap

**NATS request to**: `v1.pw-ingest.tap`

**Headers**:
```
action: add
api-key: feeder-123
icao: 7C1234
subject: my-debug-stream
```

**Response**: `Ack`

**What happens**:
1. Parse ICAO from hex string (0x7C1234)
2. Create condition
3. Append to linked list
4. Frames matching condition sent to `my-debug-stream`

**Subject naming**: Choose unique subject per tap
```
debug.aircraft.7C1234
monitor.feeder-123
analysis.all-frames
```

### Removing a Tap

**NATS request to**: `v1.pw-ingest.tap`

**Headers**:
```
action: remove
api-key: feeder-123
icao: 7C1234
subject: my-debug-stream
```

**All three must match** to remove:
- Same queue (subject)
- Same apiKey
- Same icao

**Why all three**: Precise removal, avoid accidentally removing wrong tap

**Edge case**: If you have two taps with same filters but different subjects, only one is removed (the one matching all three fields)

### Frame Format Preservation

**Sends raw frame in original format**:
```go
switch tFrame := fe.Frame().(type) {
case *beast.Frame:
    headers["type"] = "beast"
    msg = tFrame.Raw()
case *mode_s.Frame:
    headers["type"] = "avr"
    msg = tFrame.Raw()
case *sbs1.Frame:
    headers["type"] = "sbs1"
    msg = tFrame.Raw()
}
```

**Why raw format**: Preserve all information
- Beast: Includes timestamps, signal levels
- AVR: Raw Mode S hex
- SBS1: CSV format

**Type header**: Consumer can decode appropriately

**Tag header**: `headers["tag"] = fe.Source().Tag`
- Identifies which feeder sent frame
- Useful when tap matches multiple feeders

### Performance Considerations

**Queue buffer: 100 frames**:
```go
queue: make(chan *tracker.FrameEvent, 100)
```

**Why 100**:
- Smaller than accounting queue (less critical)
- Handle brief bursts
- Not intended for sustained high-rate tapping

**Overflow behavior**: Handle() blocks
- Indicates tap consumers can't keep up
- Backpressure to entire pipeline
- Consider removing slow taps

**Worker pool sizing**:
```go
for i := 0; i < 8; i++ {
    tap.queueWg.Add(1)
    go tap.processQueue()
}
```

**Why 8**: CPU-bound work (condition matching)
- Modern CPUs have 4-16 cores
- 8 workers utilize multi-core well
- More than CPU count OK (workers often wait on I/O)

**Scalability**: Linear with condition count
- 1 condition: Fast (single check)
- 10 conditions: Still fast (~10 checks)
- 100 conditions: May slow down
- 1000 conditions: Consider redesign (use map/set)

### NATS Subscription

**Unique queue name per instance**:
```go
tap.natsQueue = "pw-ingest-tap-" + randstr.RandString(20)
tap.sub, err = tap.natsServer.SubscribeReply(
    NatsAPIv1PwIngestTap,
    tap.natsQueue,
    tap.requestHandler,
)
```

**Why random queue**: Multiple pipeline instances
- Each instance has own subscription
- Requests go to all instances (fanout)
- All instances add the tap condition

**Alternative**: Shared queue (only one instance handles request)
- More efficient (fewer duplicate taps)
- But: Request might go to wrong instance (if multi-region)

### Use Case Examples

#### Debugging Specific Aircraft

**Scenario**: ICAO 0x7C1234 showing wrong position

**Setup**:
```bash
# Subscribe to debug stream
nats sub debug.7C1234 &

# Request tap
nats req v1.pw-ingest.tap "" \
  --header "action:add" \
  --header "icao:7C1234" \
  --header "subject:debug.7C1234"
```

**Result**: All frames from 0x7C1234 forwarded to `debug.7C1234`

**Analysis**:
```bash
# See raw frames
nats sub debug.7C1234

# Count frame rate
nats sub debug.7C1234 | pv -l -i 1

# Decode and analyze
nats sub debug.7C1234 | ./decode-avr
```

#### Monitoring Feeder Health

**Scenario**: Check if specific feeder is working

**Setup**:
```bash
nats req v1.pw-ingest.tap "" \
  --header "action:add" \
  --header "api-key:feeder-123" \
  --header "subject:health.feeder-123"

# Count frames
nats sub health.feeder-123 | pv -l -i 1
```

**Expected**: ~100-1000 frames/sec depending on coverage

**Troubleshooting**:
- 0 frames/sec: Feeder offline or not authorized
- <10 frames/sec: Poor antenna or location
- Normal rate but no positions: Decoding issue

#### Collecting Sample Data

**Scenario**: Need examples of DF17 velocity messages

**Setup**:
```bash
nats req v1.pw-ingest.tap "" \
  --header "action:add" \
  --header "subject:samples.all"

# Save first 1000 frames
nats sub samples.all | head -n 1000 > samples.avr

# Remove tap
nats req v1.pw-ingest.tap "" \
  --header "action:remove" \
  --header "subject:samples.all"
```

**Post-process**: Filter for DF17 TC19 in samples.avr

**Why not filter by DF in tap**: Tap filters by ICAO/feeder only
- Could extend to support DF/TC filtering
- Current design: Filter client-side

<!--
Maintainers: If you add DF/TC filtering to IngestTap, document here
-->

### Security Considerations

**No authentication on tap requests**:
- Anyone with NATS access can add taps
- Taps can see all frames (privacy concern)
- Malicious taps can DoS pipeline (backpressure)

**Mitigations**:
1. **NATS security**: Restrict who can publish to `v1.pw-ingest.tap`
2. **Network isolation**: NATS only on private network
3. **Monitoring**: Log tap add/remove events

**Production recommendation**: Add authentication
```go
// Check if requester is authorized
if !isAuthorized(msg.Header.Get("auth-token")) {
    _ = msg.Respond([]byte("Unauthorized"))
    return
}
```

<!--
Maintainers: If you add authentication to IngestTap, document here:
- Authentication method
- Authorization rules
- Token management
-->

### Memory Management

**Linked list size**: Unbounded
- Each condition: ~48 bytes (3 pointers, string, uint32)
- 100 taps: ~5 KB
- 1000 taps: ~50 KB
- 10,000 taps: ~500 KB

**No automatic cleanup**: Taps persist until explicitly removed

**Memory leak risk**: If clients add taps but don't remove
- Consider TTL (time-to-live) for taps
- Automatically remove taps after N minutes inactive

<!--
Maintainers: If you add TTL for taps, document here:
- TTL duration
- How to refresh TTL
- Cleanup mechanism
-->

## Middleware Comparison

| Aspect | Accounting | IngestTap |
|--------|-----------|-----------|
| **Purpose** | Stats tracking | Frame monitoring |
| **Output** | NATS request to ATC | NATS publish to custom subjects |
| **Frequency** | Every 1 minute | Real-time |
| **Overhead** | Low (simple counter) | Medium (condition matching) |
| **Production** | Always enabled | Debug/dev tool |
| **Stateful** | Yes (per-feeder stats) | Yes (condition list) |

## Common Issues

### Accounting Not Sending Updates

**Symptom**: ATC backend shows outdated stats

**Checks**:

1. **NATS connection**:
   ```go
   if !natsServer.HealthCheck() {
       // NATS is down
   }
   ```

2. **Check logs for errors**:
   ```
   grep "failed to update feeder stats" pipeline.log
   ```

3. **ATC backend responsive**:
   ```bash
   # Manual request should complete in <1s
   nats req v1.api.feeder.stats.update '[]'
   ```

**Solution**: Fix NATS or ATC backend issue

### IngestTap Not Forwarding Frames

**Symptom**: Added tap but no frames received

**Debug checklist**:

1. **Tap actually added**:
   ```
   # Should see log line
   grep "Network Tap Request" pipeline.log | grep "action=add"
   ```

2. **Matching frames exist**:
   ```
   # Check if aircraft is being tracked
   # Check if feeder is sending frames
   ```

3. **ICAO format correct**:
   ```go
   // Correct: "7C1234" (hex string)
   // Wrong:   "0x7C1234" (will fail to parse)
   // Wrong:   "8110132" (decimal, not hex)
   ```

4. **Subject reachable**:
   ```bash
   # Test publishing directly
   nats pub your-subject "test"
   ```

### Pipeline Slowdown with Many Taps

**Symptom**: Frame processing slows when many taps active

**Cause**: Linear condition checking
```go
for c := tap.head; c != nil; c = c.nextItem {
    if c.match(frame) {
        tap.send(c.queue, frame)
    }
}
```

**Scale**: O(n) per frame where n = tap count
- 10 taps: Negligible
- 100 taps: Noticeable (~1% CPU)
- 1000 taps: Significant (10%+ CPU)

**Solutions**:

1. **Remove unused taps**:
   ```bash
   # Remove each tap when done
   nats req v1.pw-ingest.tap "" --header "action:remove" ...
   ```

2. **Optimize condition matching** (code change):
   ```go
   // Use map for O(1) lookup instead of O(n) linear search
   tapsByICAO map[uint32][]*condition
   tapsByAPIKey map[string][]*condition
   ```

<!--
Maintainers: If you optimize tap matching, document here:
- Data structure used
- Performance improvement
- Trade-offs
-->

## Production Lessons

<!--
Maintainers: Add your middleware experiences:
- Issue encountered:
- Root cause:
- Solution:
- Lessons learned:
-->

### Accounting Update Frequency

**Initial implementation**: Every frame triggered update
- ATC backend overwhelmed (1000 req/sec)
- NATS bandwidth saturated
- Pipeline slowed due to backpressure

**Solution**: 1 minute throttling
- Reduced to ~10 req/sec (100 feeders)
- ATC backend handled easily
- Minimal latency impact (stats only need ~minute granularity)

**Lesson**: Throttle outbound updates, not every event needs immediate propagation

### IngestTap for Production Debugging

**Scenario**: Aircraft showing incorrect positions, only in production

**Challenge**: Can't reproduce locally

**Solution**: Add tap for specific ICAO in production
```bash
nats req v1.pw-ingest.tap "" \
  --header "action:add" \
  --header "icao:7C1234" \
  --header "subject:debug.prod.7C1234"

# Stream to local for analysis
nats sub debug.prod.7C1234 > frames.txt
```

**Discovery**: Frame corruption from specific feeder
- Beast timestamp had bit flips
- Only happened on one feeder's hardware
- Caught by comparing raw frames from multiple feeders

**Lesson**: IngestTap invaluable for production debugging without deploying code changes

### Memory Leak from Forgotten Taps

**Scenario**: Pipeline memory grew slowly over weeks

**Cause**: Developers adding taps for debugging, forgetting to remove

**Discovery**: 500+ conditions in linked list

**Solution**:
1. Removed all taps manually
2. Added monitoring for tap count
3. Documented removal procedure

**Future improvement**: Auto-expire taps after 1 hour

<!--
Maintainers: If you add auto-expiry, document implementation here
-->

## File Guide

| File | Purpose |
|------|---------|
| `accounting.go` | Feeder statistics tracking and ATC updates |
| `ingest_tap.go` | Dynamic frame stream tapping and forwarding |
| `ingest_tap_test.go` | Unit tests for tap condition matching |

## See Also

- [Tracker](../tracker/README.md) - Middleware integration
- [NATS](../nats_io/README.md) - NATS client for messaging
- [Export](../export/README.md) - Defines feeder update format

## References

- Middleware pattern: https://en.wikipedia.org/wiki/Middleware_(distributed_applications)
- NATS request-reply: https://docs.nats.io/nats-concepts/core-nats/reqreply
