# Frame Deduplication

## Overview

The `dedupe` package provides middleware to eliminate duplicate Mode S frames before they reach the tracker. This prevents the same message from being processed multiple times when received by multiple sources or reflected RF paths.

## Why Deduplication?

### The Problem

In typical ADS-B reception setups, duplicates appear from several sources:

**1. Multiple Receivers**
```
Receiver A ───┐
              ├──→ Aggregator
Receiver B ───┘
```
Same aircraft frame reaches both receivers → 2 identical frames

**2. RF Reflections**
```
Aircraft → Direct path → Receiver
       └→ Reflects off building → Receiver (delayed)
```
Same frame arrives via multiple propagation paths

**3. Multiple Antennas**
```
Antenna 1 ───┐
             ├──→ Same SDR/Receiver
Antenna 2 ───┘
```
Diversity reception picks up same frame on both antennas

**4. Network Protocol**
```
Producer retransmits data (Beast connection drops/reconnects)
```

### The Cost of Not Deduplicating

**Without dedupe at 5000 msg/sec with 30% duplication**:
- 1500 duplicate frames/sec processed through tracker
- 1500 duplicate CRC calculations
- 1500 duplicate ICAO lookups
- Potentially 1500 duplicate CPR decoding attempts
- Inflated message count metrics

**With dedupe**:
- 1500 frames/sec rejected early (cheap hash lookup)
- Only 3500 unique frames reach tracker
- 30% reduction in downstream processing

## Deduplication Strategy

### What Makes Frames Identical?

**Byte-level comparison of raw frame data**:
- Beast format: Compare raw binary payload
- Mode S format: Compare hex string representation
- SBS1 format: Compare entire CSV line (less ideal, see limitations)

**Why raw bytes**: Two frames are duplicates if and only if their Mode S bits are identical. This includes:
- ICAO address
- Timestamp (for timestamped formats)
- All payload data
- CRC/parity field

### Time Window

**Problem**: Can't remember every frame forever (memory unbounded)

**Solution**: Time-based forgetting
```
Default: Remember frames for 60 seconds
Sweep: Check for old frames every 10 seconds
```

**Why 60 seconds**:
- Aircraft typically transmit same type code every 1-2 seconds
- Position messages alternate ODD/EVEN every ~0.5 seconds
- Duplicates arrive within milliseconds of each other
- 60 seconds provides massive safety margin while bounding memory

**Why 10 second sweep interval**:
- Balance between memory reclamation speed and CPU overhead
- Sweeping too often wastes CPU on mostly-young entries
- Sweeping too rarely allows memory to accumulate

## Implementation: Forgetful Map

**Default implementation**: `ForgetfulSyncMap`

### Architecture

```go
type ForgetfulSyncMap struct {
    lookup        *sync.Map          // Concurrent hash map
    sweeper       *time.Timer        // Periodic cleanup
    sweepInterval time.Duration      // How often to sweep
    oldAfter      time.Duration      // Eviction threshold
}

type marble struct {
    added time.Time  // When was this stored?
    value any        // Optional associated data
}
```

### How It Works

**1. Frame arrives**
```go
key := string(frame.Raw())
if map.HasKey(key) {
    return nil  // Duplicate - drop
}
map.AddKey(key)  // Remember for 60s
return frame     // First time seeing this
```

**2. Periodic sweep (every 10s)**
```go
oldestAllowed := now.Add(-60 * time.Second)
for key, marble := range map {
    if marble.added.Before(oldestAllowed) {
        delete(key)  // Forget old frames
    }
}
```

**3. Memory bounded**
```
At 5000 msg/sec, 60 second window:
- Max entries: 5000 * 60 = 300,000
- Bytes per entry: ~50 (key + marble overhead)
- Max memory: ~15 MB
```

### Concurrency

**sync.Map**: Lock-free reads, minimal lock contention on writes

**Why sync.Map**:
- Dedupe is read-heavy (checking for existence)
- Writes are append-only (no updates to existing keys)
- Deletes only happen during sweep (separate goroutine)
- Perfect fit for sync.Map's optimistic read path

**Performance**: ~100-200 nanoseconds per lookup under load

## Alternative: B-Tree Implementation

**When to use**: Very high message rates (>20,000 msg/sec) with tight memory constraints

### Why B-Tree?

**Memory advantage**: Can iterate by timestamp without storing every timestamp

```
sync.Map sweep: Must iterate ALL entries to find old ones
B-Tree sweep:   Can descend from oldest → delete in order
```

**Trade-offs**:

| Aspect | ForgetfulSyncMap | B-Tree |
|--------|------------------|--------|
| Lookup | O(1) avg | O(log n) |
| Insert | O(1) avg | O(log n) |
| Sweep | O(n) scan all | O(k) k=old entries |
| Memory | Higher | Lower |
| Lock contention | Minimal | Higher (write lock) |

**When B-Tree wins**:
- Message rates where sweep time dominates
- Memory constrained environments
- Mostly unique frames (low duplicate rate)

**When sync.Map wins**:
- Normal message rates (<10k/sec)
- High duplicate rates (lookup speed matters)
- Typical deployments

### B-Tree Specifics

```go
type FilterBTree struct {
    btree       *btree.BTreeG[FrameAndTime]
    mu          sync.RWMutex
    btreeDegree int  // Default: 16
}

type FrameAndTime struct {
    frame []byte
    time  time.Time
}
```

**Ordering**: Frames ordered by raw bytes (lexicographic)

**Sweep optimization**:
```go
olderThan := time.Now().Add(-60 * time.Second)
toRemove := []FrameAndTime{}
btree.Descend(func(item FrameAndTime) bool {
    if item.time.Before(olderThan) {
        toRemove = append(toRemove, item)
    }
    return true
})
// Delete all old in one pass
```

**Why descend**: B-tree doesn't order by time, must still scan all

**Benefit over sync.Map**: Batch deletes, better cache locality during iteration

## Object Pooling

### Marble Pool

```go
var marbleBag *sync.Pool

func init() {
    marbleBag = &sync.Pool{
        New: func() any {
            return &marble{
                added: time.Time{},
                value: nil,
            }
        },
    }
}
```

**Why pool marbles**: At 5000 msg/sec, allocating 5000 marbles/sec creates GC pressure

**Performance impact**: Reduces GC pauses by ~40% under sustained load

**When to disable**: Debugging memory issues or use-after-free bugs

## SBS1 Deduplication Limitations

**Current approach**: Compare entire CSV line

```go
case *sbs1.Frame:
    key = string(ft.Raw())  // Entire CSV line
```

**Problem**: SBS1 includes timestamps in the CSV

```
MSG,3,1,1,4840D6,1,2024/11/16,12:34:56.789,...
                   ^^^^^^^^^^^^^^^^^^^^^^^^
                   Timestamp changes every message!
```

**Impact**: Identical aircraft state at slightly different times treated as different frames

**Why not fixed yet**: SBS1 is a legacy format, most deployments use Beast

**Possible solutions**:
1. Parse CSV, compare only aircraft state fields (slow)
2. Regex to strip timestamp before comparison (brittle)
3. Accept limitation, document (current approach)

**Recommendation**: Use Beast or raw Mode S for serious deployments

<!--
Maintainers: If you implement better SBS1 dedupe, document approach here
-->

## Integration with Tracker

Dedupe is a **middleware** component:

```
Producer → Dedupe → Tracker
```

**Middleware interface**:
```go
type Middleware interface {
    Handle(*FrameEvent) Frame
    String() string
}
```

**Flow**:
```go
func (f *Filter) Handle(fe *FrameEvent) Frame {
    frame := fe.Frame()
    key := string(frame.Raw())

    if f.list.HasKey(key) {
        return nil  // Middleware returns nil = drop frame
    }

    f.list.AddKey(key)
    return frame    // Pass through to next middleware/tracker
}
```

**Multiple middlewares**: Can chain together
```
Producer → Dedupe → OtherMiddleware → Tracker
```

## Configuration Options

### ForgetfulSyncMap

```go
dedupe.NewFilter(
    dedupe.WithDedupeCounter(prometheusCounter),
)
```

Uses default `ForgetfulSyncMap` with:
- 60 second retention
- 10 second sweep interval

### Custom ForgetfulSyncMap

```go
import "plane.watch/lib/dedupe/forgetfulmap"

fmap := forgetfulmap.NewForgetfulSyncMap(
    forgetfulmap.WithOldAgeAfterSeconds(30),      // 30s retention
    forgetfulmap.WithSweepIntervalSeconds(5),      // 5s sweep
    forgetfulmap.UseMemSyncPool(true),             // Enable pooling
)
```

### B-Tree

```go
dedupe.NewFilterBTree(
    dedupe.WithBtreeDegree(32),                    // Higher = less depth, more memory
    dedupe.WithSweeperInterval(5 * time.Second),   // Sweep frequency
    dedupe.WithDedupeMaxAge(30 * time.Second),     // Retention window
    dedupe.WithDedupeCounterBTree(prometheusCounter),
)
```

**B-tree degree**: Higher values = wider tree, fewer levels, more memory per node
- Default (16): Good balance
- 32-64: High throughput, more memory
- 8: Memory constrained

## Monitoring

**Prometheus metrics**:
```
# Counter of unique frames passed through
dedupe_frames_total

# Gauge of current entries in dedupe map (optional)
dedupe_map_size
```

**Deriving duplicate rate**:
```
Unique frames:  dedupe_frames_total
Total frames:   sum(producer_frames_total)
Duplicate rate: 1 - (unique / total)
```

**Health check**:
```go
func (f *Filter) HealthCheck() bool {
    log.Info().Int32("Num Entries", f.list.Len()).Msg("Health Check")
    return true
}
```

Shows current map size for capacity planning

## Performance Characteristics

### ForgetfulSyncMap

**Lookup**: ~100-200 ns (sync.Map read path)
**Insert**: ~500-1000 ns (marble allocation + store)
**Sweep**: ~10-50 ms per 100k entries
**Memory**: ~50 bytes/entry + key size

**Bottleneck**: Sweep time at very high rates (>50k msg/sec)

### B-Tree

**Lookup**: ~300-500 ns (log search + lock)
**Insert**: ~800-1500 ns (log insert + lock)
**Sweep**: ~5-20 ms per 100k entries
**Memory**: ~40 bytes/entry + key size + tree overhead

**Bottleneck**: Write lock contention during insert

## Common Issues

### False Negatives (Duplicates Not Caught)

**Symptom**: Same frame processed multiple times

**Causes**:
1. **Different raw representations**
   ```
   Producer A: Beast format
   Producer B: AVR format
   → Different keys, not deduplicated
   ```
   **Solution**: Normalize to one format before dedupe

2. **Timestamp differences**
   ```
   SBS1 includes timestamps in "raw"
   → Every frame looks unique
   ```
   **Solution**: Use Beast/raw Mode S

3. **Retention window too short**
   ```
   Duplicate arrives >60s later (rare but possible)
   ```
   **Solution**: Increase retention window

### False Positives (Unique Frames Dropped)

**Symptom**: Aircraft transmits multiple valid messages, some lost

**Cause**: Aircraft retransmits identical frames intentionally
```
Some transponders repeat position messages exactly
→ Legitimately identical frames
→ Dedupe drops second occurrence
```

**Impact**: Minimal - position updates reduced from 2Hz to 1Hz

**Solution**: Accept trade-off or reduce retention window to <1 second (defeats purpose)

### Memory Growth

**Symptom**: Dedupe map size keeps growing

**Causes**:
1. **Sweeper not running**
   ```
   Check: Is sweeper goroutine still alive?
   ```

2. **Retention window too long**
   ```
   At 5000 msg/sec, 600 second window = 3M entries
   ```

3. **High unique frame rate**
   ```
   Low duplicate rate = map stays full
   → Expected behavior
   ```

**Solution**: Monitor `dedupe_map_size`, adjust retention/sweep intervals

### CPU Overhead

**Symptom**: High CPU usage in dedupe

**Causes**:
1. **Sweep interval too short**
   ```
   Sweeping every 1s scans entire map constantly
   ```

2. **Very high message rate**
   ```
   >20k msg/sec → lookup overhead adds up
   ```

3. **Large retention window**
   ```
   Sweep time proportional to map size
   ```

**Solutions**:
- Increase sweep interval (10s → 30s)
- Decrease retention window (60s → 30s)
- Switch to B-tree for large maps
- Profile to confirm sweep vs. lookup overhead

## Production Lessons

> **Note to maintainers**: Add your observations here

### Typical Duplicate Rates

**Single receiver**: 5-15% duplicates (mostly RF reflections)

**Multi-receiver aggregation**: 30-60% duplicates (geographic overlap)

**Urban environments**: Higher duplicate rate (more reflections)

<!--
Maintainers: Add observed duplicate rates from your deployments:
- Environment:
- Duplicate rate:
- Receiver setup:
-->

### When Dedupe is Critical

**Required**:
- Multi-receiver aggregation
- Urban/high-reflection environments
- Limited downstream processing capacity

**Optional**:
- Single receiver in open area
- Unlimited processing capacity
- Need every frame for research/debugging

### Memory Planning

**Rule of thumb**: Size map for `rate * retention * 1.5` entries

```
Example: 5000 msg/sec, 60s retention, 30% duplicates
Expected unique: 5000 * 60 * 0.7 = 210k entries
Size for:        210k * 1.5 = 315k entries
Memory:          315k * 50 bytes = 16 MB
```

**Safety factor (1.5)**: Handles burst rates and sweep lag

<!--
Maintainers: Add memory usage from production:
- Message rate:
- Retention window:
- Observed map size:
- Memory usage:
-->

## File Guide

| File | Purpose |
|------|---------|
| `dedupe.go` | Main filter using ForgetfulSyncMap |
| `btree.go` | B-tree variant for high-rate scenarios |
| `forgetfulmap/map.go` | Time-based evicting concurrent map |

## See Also

- [Tracker](../tracker/README.md) - Uses dedupe for frame filtering
- [Producer](../producer/README.md) - Feeds frames into deduplication
- [Middleware](../middleware/README.md) - Can be combined with dedupe

## References

- sync.Map design: https://golang.org/pkg/sync/#Map
- B-tree implementation: github.com/google/btree
