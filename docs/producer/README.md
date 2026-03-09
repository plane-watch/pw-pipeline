# Producer

## Overview

The `producer` package provides the data ingestion layer for the aircraft tracking pipeline. It supports three frame formats (AVR, Beast, SBS1), multiple input sources (network, files), automatic reconnection, and optional frame replay with timing. Producers are the entry point where raw ADS-B data enters the system.

## Why Producers?

### The Problem

**Aircraft data sources vary**:
- SDR receivers output different formats (Beast, AVR, SBS1)
- Data comes from network or files
- Network connections are unreliable (need reconnection)
- Replaying files requires timing control

**Without abstraction**:
```go
// Brittle, format-specific code scattered everywhere
if isBeast {
    connectToBeastServer()
} else if isAVR {
    connectToAVRServer()
}
```

### The Solution

**Producer abstraction**:
```go
producer := producer.New(
    producer.WithFetcher("localhost", "30005"),
    producer.WithType(producer.Beast),
    producer.WithSourceTag("receiver-north"),
)

frames := producer.Listen()
for frame := range frames {
    tracker.Process(frame)
}
```

**Benefits**:
- ✓ Unified interface for all formats
- ✓ Automatic format parsing
- ✓ Reconnection built-in
- ✓ File replay with timing
- ✓ Network listener mode
- ✓ Health monitoring

## Supported Formats

### AVR (ASCII Mode S)

**Format**: Hex-encoded Mode S frames, newline-delimited
```
*8D4840D6202CC371C32CE0576098;
*5D4840D658A582;
```

**Characteristics**:
- Human-readable (hex strings)
- Easy to debug (cat files, grep)
- ~2x bandwidth overhead vs Beast
- Line-based (uses `bufio.ScanLines`)

**When to use**:
- Debugging (easy to read logs)
- Development (simple to generate test data)
- Low frame rates (<100/sec)

### Beast (Binary Mode S)

**Format**: Binary with timestamps and signal levels
```
0x1A 0x33 [6-byte timestamp] [1-byte signal] [14-byte Mode S]
```

**Characteristics**:
- Compact (~30% less bandwidth)
- Includes GPS timestamps (for MLAT)
- Includes signal level
- Binary parsing required

**When to use**:
- Production (bandwidth efficient)
- MLAT (requires timestamps)
- High frame rates (1000+ frames/sec)

**Escape handling**: `0x1A` escaped as `0x1A 0x1A`

### SBS1 (BaseStation CSV)

**Format**: Comma-separated values with decoded fields
```
MSG,3,1,1,4840D6,1,2024/11/16,14:23:45.123,2024/11/16,14:23:45.456,,37000,,,38.89,-77.03,,,,,,
```

**Characteristics**:
- Already decoded (no raw Mode S)
- Very verbose (~10x overhead)
- Timestamps in CSV
- Human-readable

**When to use**:
- Legacy integrations (lots of tools support SBS1)
- When downstream wants decoded data
- Low frame rates only

**Limitation**: Can't be deduplicated (timestamps in format)

## Input Sources

### Network Fetcher (Client)

**Connect to remote server**:
```go
producer.New(
    producer.WithFetcher("receiver.local", "30005"),
    producer.WithType(producer.Beast),
)
```

**What it does**:
1. Dial TCP to `receiver.local:30005`
2. Read frames from connection
3. Reconnect automatically on disconnect
4. Exponential backoff on failure

**Reconnection logic**:
```go
backOff = time.Second  // Start at 1 second
for {
    conn, err = net.Dial("tcp", host:port)
    if err != nil {
        time.Sleep(backOff)
        backOff = backOff*2 + randomJitter
        if backOff > time.Minute {
            backOff = time.Minute  // Cap at 1 minute
        }
        continue
    }
    // Connected, reset backoff
    backOff = time.Second
    readFrames(conn)
}
```

**Why exponential backoff**:
- Don't hammer server when down
- Quick reconnect if transient (1s, 2s, 4s...)
- Cap at 1 minute (don't wait forever)

**Random jitter**: Prevents thundering herd
- Multiple clients don't reconnect simultaneously
- Spreads load over time

**Health check**: `producer.HealthCheck()` returns connection state

### Network Listener (Server)

**Accept incoming connections**:
```go
producer.New(
    producer.WithListener("0.0.0.0", "30005"),
    producer.WithType(producer.Beast),
)
```

**What it does**:
1. Listen on `0.0.0.0:30005`
2. Accept connections (multiple clients)
3. Each connection handled in goroutine
4. Frames from all clients merged into single stream

**Use case**: Receive from multiple feeders
```
Feeder 1 → Port 30005 → Producer → Tracker
Feeder 2 → Port 30005 → Producer
Feeder 3 → Port 30005 → Producer
```

**Multi-client**: Each connection handled independently
- One goroutine per connection
- Frames interleaved in output channel

**Graceful shutdown**: Closes listener on `Stop()`

### Direct Connection

**Existing network connection**:
```go
conn, _ := net.Dial("tcp", "receiver:30005")
producer.New(
    producer.WithConnection(conn),
    producer.WithType(producer.Beast),
)
```

**Use case**: Custom connection management
- TLS wrapping
- SSH tunnels
- Custom protocols

**Ownership**: Producer takes ownership, closes on exit

### File Replay

**Read from files**:
```go
producer.New(
    producer.WithFiles([]string{
        "capture.beast",
        "capture2.beast.gz",
        "capture3.beast.bz2",
    }),
    producer.WithType(producer.Beast),
    producer.WithBeastDelay(true),  // Replay at original speed
)
```

**Compression support**: Automatic detection
- `.gz` → gzip decompression
- `.bz2` → bzip2 decompression
- Plain files → no decompression

**Detection**: File extension suffix check
```go
isGzip := strings.ToLower(inFileName[len(inFileName)-2:]) == "gz"
isBzip2 := strings.ToLower(inFileName[len(inFileName)-3:]) == "bz2"
```

**Sequential processing**: Files processed in order

**Why goroutines**: Non-blocking startup
```go
go func() {
    defer p.Cleanup()
    for _, file := range files {
        processFile(file)
    }
}()
```

## Beast Delay (Timing Replay)

### Purpose

**Replay files at original speed** based on timestamps

**Without delay**:
```go
producer.WithBeastDelay(false)  // Default
// Processes frames as fast as possible
// 1 hour capture replayed in ~1 second
```

**With delay**:
```go
producer.WithBeastDelay(true)
// Processes frames at original timing
// 1 hour capture takes 1 hour to replay
```

### Implementation

**Extract timestamps**:
```go
lastTimeStamp := time.Duration(0)
for scan.Scan() {
    frame := beast.NewFrame(msg)
    currentTs := frame.BeastTicksNs()  // Nanoseconds since epoch

    if lastTimeStamp > 0 && lastTimeStamp < currentTs {
        time.Sleep(currentTs - lastTimeStamp)  // Sleep delta
    }

    lastTimeStamp = currentTs
    processFrame(frame)
}
```

**Delta sleep**: Sleep difference between frames
- Frame 1 at T=0: No sleep
- Frame 2 at T=500ms: Sleep 500ms
- Frame 3 at T=1000ms: Sleep 500ms

**Why only Beast**: AVR and SBS1 don't have reliable timestamps
- AVR: No timestamps
- SBS1: Timestamps in CSV (decode time, not capture time)

### Use Cases

**Fast replay** (delay=false):
- Testing decoder logic
- Generating test datasets
- Performance benchmarking
- CPR debugging (need ODD/EVEN pairs quickly)

**Timed replay** (delay=true):
- Integration testing (realistic timing)
- Demo/visualization (smooth aircraft movement)
- Algorithm testing (velocity checks, etc.)
- Simulating live stream

**VelocityCheck disabled**: When using Beast delay
```go
p.FrameSource.VelocityCheck = p.beastDelay
```

**Why**: Fast replay creates impossible velocities
- Frame at 38°N in file position 1
- Frame at 39°N in file position 2 (1s later in real time)
- Replayed in microseconds → 1°/µs = physically impossible
- Velocity check would reject

## Beast Binary Parsing

### Scanner (ScanBeast)

**Custom bufio.SplitFunc for Beast format**:

**Message structure**:
```
0x1A | Type | Timestamp (6B) | Signal (1B) | Payload
```

**Type determines length**:
| Type | Name | Total Bytes |
|------|------|-------------|
| 0x31 | Mode AC | 11 |
| 0x32 | Mode S Short | 16 |
| 0x33 | Mode S Long | 23 |
| 0x34 | Config/Stats | 11 |

**Parsing flow**:
1. Find `0x1A` (message start marker)
2. Read type byte (`data[i+1]`)
3. Determine message length from type
4. Check buffer has enough data
5. Handle escaping (`0x1A 0x1A` → `0x1A`)
6. Return complete message

**Escape handling**:
```go
for tokenIndex < msgLen && dataIndex < i+tokenBufLen {
    token[tokenIndex] = data[dataIndex]

    // If next byte is escaped 0x1A, skip it
    if data[dataIndex] == 0x1A && data[dataIndex+1] == 0x1A {
        bufferAdvance++  // Advance source buffer
        dataIndex++      // Skip escape byte
    }

    dataIndex++
    tokenIndex++
}
```

**Why escaping**: `0x1A` is start marker
- Payload might contain `0x1A` naturally
- Escape as `0x1A 0x1A` to distinguish
- Parser removes escape during extraction

**Buffer size: 50 bytes**:
```go
const tokenBufLen = 50
token := [tokenBufLen]byte{}
```

**Why 50**: Longest message (Mode S Long) is 23 bytes
- 23 bytes message
- Worst case: Every byte is `0x1A` (doubled) = 46 bytes
- 50 bytes = safe margin

**Insufficient data**: Return `(0, nil, nil)`
```go
if bufLen >= tokenBufLen {
    return bufferAdvance, token[0:msgLen], nil
}
// Not enough data yet
return 0, nil, nil
```

**Tells scanner**: Need more data, don't advance

## Keep-Alive Repeater

### Purpose

**Problem**: Idle connections closed by firewalls/NAT
- No frames for 30+ seconds
- Router closes "idle" connection
- Producer disconnected, no notification

**Solution**: Periodically resend last frame
```go
producer.WithKeepAliveRepeater()
```

**How it works**:
1. Track last frame per ICAO
2. Every 30 seconds, republish last frame
3. Keeps connection active
4. Downstream sees repeated frames

### Implementation

**Frame tracking**:
```go
type keepAliveRepeater struct {
    listFrames map[uint32]tracker.FrameEvent  // ICAO → Last frame
    chanFrame  chan tracker.FrameEvent
}

func (k *keepAliveRepeater) processor(p *Producer) {
    ticker := time.NewTicker(time.Second * 30)
    for {
        select {
        case fe := <-k.chanFrame:
            k.listFrames[fe.Frame().Icao()] = fe  // Update last frame

        case <-ticker.C:
            for _, fe := range k.listFrames {
                p.AddEvent(fe)  // Republish
            }
        }
    }
}
```

**Every frame updates map**: Last frame per ICAO

**Every 30 seconds**: Republish all tracked aircraft

### Trade-offs

**Pros**:
- Prevents connection timeouts
- Keeps data flowing
- No special protocol needed

**Cons**:
- Duplicate frames downstream
- Dedupe middleware required
- Memory: Stores one frame per aircraft

**When to use**:
- Behind NAT with aggressive timeouts
- Firewall with connection tracking
- Long-distance connections (WAN)

**When NOT to use**:
- Direct LAN connections
- Already have frequent traffic
- Downstream can't handle duplicates

## Poison Pill Pattern

### Purpose

**Graceful shutdown when condition met**:
```go
producer.WithPoisonPill(func() bool {
    return !feederauth.IsValid(apiKey)
}, time.Second*5)
```

**Check function every 5 seconds**: If returns true, stop producer

**Use cases**:

1. **Auth revoked**:
   ```go
   WithPoisonPill(func() bool {
       return !isAuthorized(apiKey)
   })
   ```

2. **File size exceeded**:
   ```go
   WithPoisonPill(func() bool {
       return bytesProcessed > maxBytes
   })
   ```

3. **Time limit**:
   ```go
   startTime := time.Now()
   WithPoisonPill(func() bool {
       return time.Since(startTime) > time.Hour
   })
   ```

### Implementation

**Periodic check**:
```go
if p.poisonPill != nil {
    p.poisonPillCancel = timing.RunOnTicker(p.log, time.Second*5, func() error {
        if p.poisonPill() {
            log.Debug().Msg("took poison pill")
            p.Stop()  // Graceful shutdown
        }
        return nil
    })
}
```

**Cleanup on shutdown**:
```go
if p.poisonPillCancel != nil {
    p.poisonPillCancel()  // Stop ticker
}
```

**Why "poison pill"**: Termination signal in messaging patterns
- Self-destruct when condition met
- Graceful vs forceful
- Clean shutdown

## Output Channel

### Buffer Size

**100 frames**:
```go
out: make(chan tracker.FrameEvent, 100)
```

**Why 100**:
- Small buffer (not primary buffering point)
- Tracker should consume quickly
- Backpressure if tracker slow

**At 1000 frames/sec**: ~100ms buffer
- Very brief
- Indicates tracker keeping up

**Full channel**: Producer blocks
```go
p.out <- frameEvent  // Blocks if buffer full
```

**Backpressure propagates**:
```
Producer blocks → Connection buffer fills → TCP window shrinks → Source slows
```

**Panic recovery**:
```go
func (p *Producer) AddEvent(e tracker.FrameEvent) {
    defer func() {
        if r := recover(); r != nil {
            p.log.Error().Interface("recover", r).Msg("Failed to add event")
        }
    }()
    p.out <- e
}
```

**Why recover**: Channel might be closed during shutdown
- Race condition: Shutdown vs late frame
- Recover prevents crash
- Log error for debugging

## Source Metadata

### FrameSource

**Metadata attached to each frame**:
```go
type FrameSource struct {
    OriginIdentifier string     // "receiver.local:30005" or "file://capture.beast"
    Name             string     // Human-readable name
    Tag              string     // Unique identifier (API key, etc.)
    RefLat           *float64   // Reference latitude for surface position decoding
    RefLon           *float64   // Reference longitude
    VelocityCheck    bool       // Enable velocity sanity checks
}
```

**OriginIdentifier**: Where frames came from
- Network: `host:port`
- File: `file://path/to/file`

**Name**: Display name
- Default: OriginIdentifier or Tag
- Can override with `WithOriginName()`

**Tag**: For tracking/authentication
- Feeder API key
- Distinguishes multiple sources
- Used in accounting

**RefLat/RefLon**: Surface position decoding
- CPR surface positions need reference point
- Usually receiver location
- Set with `WithReferenceLatLon()`

**VelocityCheck**: Position sanity
- Reject impossible velocities (>Mach 3)
- Disabled during fast file replay
- Enabled for live streams

## Prometheus Metrics

### Frame Counters

**Per-format counters**:
```go
producer.WithPrometheusCounters(
    avrCounter,    // prometheus.Counter for AVR frames
    beastCounter,  // prometheus.Counter for Beast frames
    sbs1Counter,   // prometheus.Counter for SBS1 frames
)
```

**Incremented per frame**:
```go
if p.stats.beast != nil {
    p.stats.beast.Inc()
}
```

**Use for**:
- Monitor frame rate per format
- Detect source issues (rate drop)
- Capacity planning

**Example Prometheus query**:
```promql
# Frames per second by format
rate(producer_frames_total{format="beast"}[1m])

# Total frames across all producers
sum(rate(producer_frames_total[5m]))
```

## Health Checks

### Fetcher Health

**Connection-based health**:
```go
func (p *Producer) HealthCheck() bool {
    if p.hasFetcher {
        return p.fetcherConnected  // true if connected
    }
    return true  // Always healthy if not a fetcher
}
```

**Logic**:
- **Fetcher**: Healthy = currently connected
- **Listener**: Always healthy (accepts connections)
- **File**: Always healthy (reading files)

**Health endpoint integration**:
```go
monitoring.AddHealthCheck(producer)
```

**Load balancer use**: Remove unhealthy instances
```
if !producer.HealthCheck() {
    // Don't route traffic to this instance
}
```

## Cleanup and Shutdown

### Graceful Shutdown

**Stop method**:
```go
func (p *Producer) Stop() {
    p.cmdChan <- cmdExit
}
```

**Command channel**: Signals shutdown to goroutines

**Cleanup sequence**:
```go
func (p *Producer) Cleanup() {
    // 1. Cancel poison pill ticker
    if p.poisonPillCancel != nil {
        p.poisonPillCancel()
    }

    // 2. Run user-defined cleanup functions
    for _, cleanUpFunc := range p.cleanUpTasks {
        err := cleanUpFunc()
        if err != nil {
            p.log.Error().Err(err).Msg("error in user-defined clean-up function")
        }
    }

    // 3. Close output channel (signals consumers)
    close(p.out)
}
```

**Always runs**: Deferred in goroutine entry points
```go
defer p.Cleanup()
```

**Panic recovery**:
```go
defer func() {
    if r := recover(); r != nil {
        p.log.Error().Interface("recover", r).Msg("Cleanup() had a panic")
    }
}()
```

**Why recover**: Ensure channel closed even if cleanup panics

### Custom Cleanup

**User-defined tasks**:
```go
producer.WithCleanUpTasks(
    func() error {
        return closeDatabase()
    },
    func() error {
        return flushBuffers()
    },
)
```

**Use cases**:
- Close file handles
- Flush buffers
- Update status
- Send shutdown notification

**Error handling**: Logged but doesn't stop cleanup
```go
for _, cleanUpFunc := range p.cleanUpTasks {
    err := cleanUpFunc()
    if err != nil {
        p.log.Error().Err(err).Msg("error in user-defined clean-up function")
        // Continue with other cleanup tasks
    }
}
```

## Production Patterns

### Multiple Producers

**Aggregate from multiple sources**:
```go
producer1 := producer.New(
    producer.WithFetcher("receiver-north:30005", "30005"),
    producer.WithType(producer.Beast),
    producer.WithSourceTag("north"),
)

producer2 := producer.New(
    producer.WithFetcher("receiver-south:30005", "30005"),
    producer.WithType(producer.Beast),
    producer.WithSourceTag("south"),
)

tracker := tracker.New()
tracker.AddProducer(producer1.Listen())
tracker.AddProducer(producer2.Listen())
```

**Source tag distinguishes**: Frames tagged with source

**Use for**:
- Multi-site coverage
- Redundancy (failover)
- MLAT (requires 3+ receivers)

### File Replay for Testing

**Capture live stream**:
```bash
nc receiver:30005 > capture.beast
```

**Replay for testing**:
```go
producer := producer.New(
    producer.WithFiles([]string{"capture.beast"}),
    producer.WithType(producer.Beast),
    producer.WithBeastDelay(false),  // Fast replay
)
```

**Use for**:
- Unit tests (deterministic input)
- Debugging (reproduce specific frames)
- Performance testing (high frame rate)

### Compression for Storage

**Capture compressed**:
```bash
nc receiver:30005 | gzip > capture.beast.gz
```

**10:1 compression typical**: Beast compresses well
- Repetitive ICAO addresses
- Similar timestamps
- Position data patterns

**1 hour uncompressed**: ~500 MB
**1 hour gzipped**: ~50 MB

**Replay**: Automatic decompression
```go
producer.WithFiles([]string{"capture.beast.gz"})
// No special handling needed
```

## Common Issues

### "Unknown Producer Type"

**Symptom**: Producer doesn't start, logs "Unknown Producer type"

**Cause**: Type not set or invalid
```go
producer.New()  // No type specified!
```

**Solution**: Specify type
```go
producer.New(
    producer.WithType(producer.Beast),
)
```

### Reconnection Loop

**Symptom**: Logs show constant connect/disconnect

**Causes**:
1. **Server not listening**: Check server running
2. **Firewall blocking**: Check ports open
3. **Wrong host/port**: Verify configuration
4. **Server rejecting**: Check server logs

**Debug**:
```bash
# Test connectivity
telnet receiver 30005

# Check server logs
tail -f receiver.log
```

### High Memory Usage

**Symptom**: Producer memory grows over time

**Cause**: Keep-alive repeater storing frames
```go
listFrames map[uint32]tracker.FrameEvent  // One per aircraft
```

**Calculation**:
```
1000 aircraft × ~1KB per frame = ~1 MB
```

**Usually not a problem**: Unless 100k+ aircraft

**Mitigation**: Limit tracked aircraft
```go
if len(k.listFrames) > 10000 {
    // Evict oldest aircraft
}
```

<!--
Maintainers: If you add LRU eviction to repeater, document here
-->

### No Frames Received

**Symptom**: Producer connected, but no frames

**Checks**:

1. **Data flowing on wire**:
   ```bash
   tcpdump -i eth0 -A port 30005
   # Should see binary data
   ```

2. **Correct format**:
   ```go
   // Make sure type matches source
   WithType(producer.Beast)  // If source sends Beast
   ```

3. **Scanner errors**:
   ```go
   // Check scanner error
   scan.Err()
   ```

4. **Server sending**: Some servers need subscription
   ```bash
   echo "SUB" | nc receiver 30005
   ```

## Performance Characteristics

### Throughput

**AVR**: ~10,000 frames/sec (line parsing overhead)
**Beast**: ~50,000 frames/sec (binary parsing)
**SBS1**: ~5,000 frames/sec (CSV parsing)

**Bottleneck**: Usually network, not parsing

### Memory

**Per producer**:
- Output channel: ~10 KB (100 frames × ~100 bytes)
- Keep-alive repeater: ~1 MB (1000 aircraft × 1KB)
- Connection buffers: ~64 KB
- **Total**: ~1-2 MB per producer

**Scales linearly**: 10 producers = ~20 MB

### CPU

**Negligible**: <1% CPU per producer
- Parsing is simple
- No complex logic
- I/O bound, not CPU bound

## Future Enhancements

<!--
Maintainers: Document enhancements as implemented
-->

### Configurable Keep-Alive Interval

**Current**: Hardcoded 30 seconds

**Proposed**: Configurable
```go
producer.WithKeepAliveRepeater(time.Second * 10)
```

### Frame Filtering

**Current**: All frames forwarded

**Proposed**: Filter at producer
```go
producer.WithFilter(func(frame tracker.Frame) bool {
    return frame.DownLinkType() == 17  // ADS-B only
})
```

**Why**: Reduce downstream processing

### Statistics

**Current**: Only Prometheus counters

**Proposed**: Detailed stats
```go
type Stats struct {
    FramesReceived uint64
    FramesDropped  uint64
    BytesReceived  uint64
    Reconnects     uint32
    Uptime         time.Duration
}
```

## File Guide

| File | Purpose |
|------|---------|
| `common.go` | Core producer logic, options, cleanup |
| `avr.go` | AVR format parser |
| `beast.go` | Beast format parser and scanner |
| `sbs1.go` | SBS1 format parser |
| `repeater.go` | Keep-alive frame repeater |
| `*_test.go` | Unit tests |

## See Also

- [Tracker](../tracker/README.md) - Consumes producer frames
- [Beast Format](../tracker/beast/README.md) - Beast binary format details
- [SBS1 Format](../tracker/sbs1/README.md) - SBS1 CSV format details

## References

- Beast binary format: https://wiki.modesbeast.com/Radarcape:Firmware_Versions#The_BEAST_binary_format
- SBS BaseStation format: http://woodair.net/sbs/article/baserstation_socket_data.htm
- AVR format: https://github.com/antirez/dump1090 (original implementation)
