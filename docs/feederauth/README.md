# Feeder Authentication & Connection Management

## Overview

The `feederauth` package provides centralized authentication and connection management for aircraft data feeders (receivers). It enforces connection limits, rate limiting, and maintains a cached list of authorized feeders synchronized with a central API.

## Why Feeder Authentication?

### The Problem

**Without authentication**:
- Anyone can connect to your aggregator
- Resource exhaustion (unlimited connections)
- Data quality issues (untrusted sources)
- No accountability (can't track who sent what)

**Naive authentication** (static API keys in config):
- Adding/removing feeders requires config changes + restarts
- Can't disable compromised keys quickly
- No central management for multi-aggregator setups

### The Solution

**Cached authentication with NATS sync**:

```
Central DB → NATS API → FeederCache (1min refresh) → Connection handlers
```

**Benefits**:
- Add/remove feeders without restarting aggregators
- Changes propagate to all aggregators within 1 minute
- Connection limits prevent abuse
- Rate limiting prevents connection spam
- Central audit trail

## Architecture

### Three-Tier State Management

```go
type FeederCache struct {
    // Tier 1: Authorized feeders (refreshed periodically)
    feeders map[string]export.Feeder

    // Tier 2: Currently connected feeders
    feedersConnected map[string]map[Protocol]struct{}

    // Tier 3: Connection rate limiting
    feederConnectionTime map[string]map[Protocol]time.Time
}
```

**Tier 1**: Authorization list
- **Source**: Central NATS API
- **Refresh**: Every 60 seconds
- **Purpose**: Is this API key valid?

**Tier 2**: Connection state
- **Source**: Local tracking
- **Update**: On connect/disconnect
- **Purpose**: Is this feeder currently connected?

**Tier 3**: Rate limit state
- **Source**: Local tracking
- **Cleanup**: Every 60 seconds (stale entries)
- **Purpose**: Is this feeder connecting too frequently?

### Protocol Types

```go
const (
    BEAST Protocol = iota  // Beast binary format
    MLAT                   // MLAT (multilateration) data
)
```

**Why separate protocols**:
- Feeder can connect for Beast data AND MLAT simultaneously
- Different connection limits per protocol
- Separate accounting/metrics

**Per-protocol connection state**:
```go
feedersConnected["api-key-123"][BEAST]  // Connected via Beast
feedersConnected["api-key-123"][MLAT]   // Also connected via MLAT
```

## Authentication Flow

### 1. Initial Feeder List Fetch

```go
func New(opts ...Option) (*FeederCache, error) {
    // Connect to NATS
    server, err := nats_io.NewServer(...)

    // Fetch initial feeder list
    err = fetchFeeders()  // Synchronous on startup

    // Start periodic refresh (every 60s)
    refresherCancelFunc = timing.RunOnTicker(log, time.Minute, fetchFeeders)

    return &FeederCache{...}, nil
}
```

**Why fetch on startup**: Fail fast if NATS unavailable

**Why periodic refresh**: Pick up feeder changes without restart

### 2. Feeder List Refresh

```go
func (f *FeederCache) fetchFeeders() error {
    // Request feeder list from NATS API
    ret, err := natsServer.Request(export.NatsApiFeederListV1, nil, map[string]string{}, time.Second)

    // Decode JSON response
    var feeders export.Feeders
    json.Unmarshal(ret, &feeders)

    // Atomically replace feeder list
    f.populate(&feeders)

    return nil
}
```

**API endpoint**: `v1.feeder.list`

**Response format**:
```json
[
  {
    "Id": 123,
    "ApiKey": "550e8400-e29b-41d4-a716-446655440000",
    "User": "john@example.com",
    "Label": "North Antenna",
    "FeederCode": "YPPH-0001",
    "Latitude": "-31.9505",
    "Longitude": "115.8605",
    "MlatEnabled": true
  },
  ...
]
```

**Populate logic**:
```go
func (f *FeederCache) populate(feeders *export.Feeders) {
    f.muFeeders.Lock()
    defer f.muFeeders.Unlock()

    // Clear old entries (keeps capacity)
    clear(f.feeders)

    // Rebuild from API response
    for _, feeder := range *feeders {
        f.feeders[feeder.ApiKey.String()] = feeder
    }

    // Clean up stale connection state
    f.cleanup()
}
```

**Why clear + rebuild**: Simpler than diff, handles deletions automatically

### 3. Connection Authentication

```go
func (f *FeederCache) Authenticate(apiKey string, p Protocol) (bool, error) {
    // Check 1: Is API key valid?
    if !f.IsValid(apiKey) {
        return false, fmt.Errorf("feeder not found")
    }

    // Check 2: Already connected?
    if f.IsConnected(apiKey, p) {
        return false, fmt.Errorf("feeder already connected")
    }

    // Check 3: Connecting too frequently?
    if f.IsConnectingTooFrequently(apiKey, p) {
        return false, fmt.Errorf("feeder connecting too frequently")
    }

    return true, nil
}
```

**Three-phase validation**: Authorization → Connection limit → Rate limit

### 4. Connection State Management

**On connect**:
```go
func (f *FeederCache) SetConnected(apiKey string, p Protocol) {
    f.feederConnectionTime[apiKey][p] = time.Now()  // Record connection time
    f.feedersConnected[apiKey][p] = struct{}{}      // Mark as connected
}
```

**On disconnect**:
```go
func (f *FeederCache) SetDisconnected(apiKey string, p Protocol) {
    delete(f.feedersConnected[apiKey], p)  // Remove connection marker
    // Note: feederConnectionTime left intact for rate limiting
}
```

## Connection Limits

### One Connection Per Protocol

**Enforcement**:
```go
func (f *FeederCache) IsConnected(apiKey string, p Protocol) bool {
    if _, ok := f.feedersConnected[apiKey][p]; ok {
        return true  // Already connected via this protocol
    }
    return false
}
```

**Why limit to one**:
- Prevents accidental double-connections (configuration errors)
- Reduces server resource usage
- Simplifies accounting (1 feeder = 1 connection)

**Example**:
```
Feeder tries to connect via Beast
  → Already connected via Beast → REJECTED
  → Not connected via MLAT → ALLOWED (different protocol)
```

**Bypass mechanism**: None intentionally
- If feeder needs multiple Beast connections, deploy multiple API keys
- This is a business logic decision, not technical limitation

### Rate Limiting (30 Second Window)

**Purpose**: Prevent connection spam

**Scenario**:
```
Feeder misconfigured:
  - Connects
  - Immediately disconnects (bad auth)
  - Reconnects
  - Repeat 100x/sec → Resource exhaustion
```

**Protection**:
```go
func (f *FeederCache) IsConnectingTooFrequently(apiKey string, p Protocol) bool {
    if lastConnTime, ok := f.feederConnectionTime[apiKey][p]; ok {
        if lastConnTime.After(time.Now().Add(-30 * time.Second)) {
            return true  // Too soon since last connection
        }
    }
    return false
}
```

**Window**: 30 seconds

**Why 30 seconds**:
- Long enough to prevent spam loops
- Short enough to allow legitimate reconnects (network blip)
- Doesn't penalize normal disconnect→reconnect patterns

**Edge case**:
```
T=0s:   Connect → SetConnected (records time.Now())
T=1s:   Disconnect → SetDisconnected (time remains in map)
T=15s:  Try reconnect → REJECTED (only 15s elapsed)
T=31s:  Try reconnect → ALLOWED (>30s elapsed)
```

**Cleanup**: Stale entries (>1 minute old) removed during `cleanup()`

## Concurrency & Locking

### Three Separate Mutexes

**Why separate mutexes**:
- Reduce lock contention
- Different access patterns per map
- Read-heavy vs. write-heavy optimizations

**Mutex 1**: `muFeeders` (RWMutex)
```go
// Read-heavy: Every authentication check
f.muFeeders.RLock()
_, ok := f.feeders[apiKey]
f.muFeeders.RUnlock()

// Write-rare: Every 60s on refresh
f.muFeeders.Lock()
f.feeders = ...
f.muFeeders.Unlock()
```

**Mutex 2**: `muFeedersConnected` (RWMutex)
```go
// Read: Every authentication check, connection state query
f.muFeedersConnected.RLock()
isConnected := f.feedersConnected[apiKey][p]
f.muFeedersConnected.RUnlock()

// Write: On connect/disconnect (relatively frequent)
f.muFeedersConnected.Lock()
f.feedersConnected[apiKey][p] = struct{}{}
f.muFeedersConnected.Unlock()
```

**Mutex 3**: `muFeederConnectionTime` (RWMutex)
```go
// Read: Every connection attempt
f.muFeederConnectionTime.RLock()
lastTime := f.feederConnectionTime[apiKey][p]
f.muFeederConnectionTime.RUnlock()

// Write: On connect, periodic cleanup
f.muFeederConnectionTime.Lock()
f.feederConnectionTime[apiKey][p] = time.Now()
f.muFeederConnectionTime.Unlock()
```

### Lock Ordering (Deadlock Prevention)

**Populate function locks ALL mutexes**:
```go
func (f *FeederCache) populate(feeders *export.Feeders) {
    f.muFeeders.Lock()
    defer f.muFeeders.Unlock()
    f.muFeedersConnected.Lock()
    defer f.muFeedersConnected.Unlock()
    f.muFeederConnectionTime.Lock()
    defer f.muFeederConnectionTime.Unlock()

    // Critical section
}
```

**Lock order**: Always acquire in this order to prevent deadlock
1. muFeeders
2. muFeedersConnected
3. muFeederConnectionTime

**Other functions**: Lock only what needed (single mutex)

## Cleanup & Memory Management

### Automatic Cleanup

**Triggered**: During `populate()` (every 60s)

**Targets**:

**1. Empty connection maps**:
```go
for apikey := range f.feedersConnected {
    if len(f.feedersConnected[apikey]) == 0 {
        delete(f.feedersConnected, apikey)
    }
}
```

**Why**: Feeder disconnected from all protocols → remove outer map entry

**2. Stale connection times** (>1 minute old):
```go
for apiKey := range f.feederConnectionTime {
    for p := range f.feederConnectionTime[apiKey] {
        if f.feederConnectionTime[apiKey][p].Before(time.Now().Add(-time.Minute)) {
            delete(f.feederConnectionTime[apiKey], p)
        }
    }
}
```

**Why**: Rate limit data no longer relevant after 1 minute (2x the rate limit window)

**Memory impact**:
```
Without cleanup:
  1000 feeders, each connects once → 1000 map entries forever

With cleanup:
  1000 feeders, only 50 currently connected → 50 entries (98% reduction)
```

### Manual Reset

**Per-protocol reset**:
```go
func (f *FeederCache) Reset(p Protocol) {
    f.muFeedersConnected.Lock()
    defer f.muFeedersConnected.Unlock()

    // Disconnect all feeders using this protocol
    for apiKey := range f.feedersConnected {
        delete(f.feedersConnected[apiKey], p)
    }
}
```

**Use case**: Server restart, protocol handler crash

**Example**:
```
Beast handler crashes and restarts
→ All feeders think they're still connected
→ Call Reset(BEAST)
→ Clears all Beast connections
→ Feeders reconnect successfully
```

## Integration Example

### Typical Usage Pattern

```go
// Startup
cache, err := feederauth.New(
    feederauth.WithNatsURL("nats://localhost:4222"),
    feederauth.WithLogger(log),
)
if err != nil {
    log.Fatal().Err(err).Msg("Failed to initialize feeder cache")
}
defer cache.Close()

// Connection handler
func handleConnection(apiKey string) {
    // Authenticate
    ok, err := cache.Authenticate(apiKey, feederauth.BEAST)
    if !ok {
        log.Warn().Err(err).Str("apiKey", apiKey).Msg("Authentication failed")
        return
    }

    // Mark connected
    cache.SetConnected(apiKey, feederauth.BEAST)
    defer cache.SetDisconnected(apiKey, feederauth.BEAST)

    // Get feeder metadata
    feeder, _ := cache.Get(apiKey)
    log.Info().
        Str("label", feeder.Label).
        Str("code", feeder.FeederCode).
        Msg("Feeder connected")

    // Handle connection...
}
```

## Error Scenarios

### NATS API Unavailable

**On startup**:
```go
cache, err := feederauth.New(...)
if err != nil {
    // Cannot proceed without feeder list
    // Options:
    // 1. Fail fast (current behavior)
    // 2. Retry with backoff
    // 3. Load from local cache file
}
```

**During operation**:
```go
err := fetchFeeders()
if err != nil {
    // Logged but not fatal
    // Continue using stale feeder list
    // Will retry in 60 seconds
}
```

**Impact**:
- New feeders won't be authorized (until NATS recovers)
- Deleted feeders will remain authorized (until NATS recovers)
- Existing connections unaffected

**Mitigation**:
- NATS HA cluster for redundancy
- Local cache file fallback (not implemented)

### Feeder Removed from API

**Scenario**:
```
T=0s:   Feeder connected, authorized
T=30s:  Admin removes feeder from database
T=60s:  FeederCache refreshes, feeder gone
```

**Behavior**: Feeder remains connected until disconnect

**Why not forcibly disconnect**:
- FeederCache doesn't track connection objects
- Connection handler is separate component
- Clean separation of concerns

**To force disconnect**: Connection handler must poll `IsValid()` periodically

### Clock Skew

**Problem**: Server clock changes during operation

**Example**:
```
T=12:00:00  Feeder connects, time recorded
T=12:00:05  Clock jumps back to 11:59:00 (NTP correction)
T=12:00:06  IsConnectingTooFrequently() compares:
            lastConnTime (12:00:00) vs. now (11:59:00)
            → 12:00:00.After(11:59:00 - 30s) = true
            → Blocked for ~61 seconds until 12:01:00
```

**Mitigation**:
- Use monotonic time (not implemented)
- NTP gradual adjustments (not jumps)
- 30s window absorbs small skew

## Performance Characteristics

### Memory Usage

**Per feeder in cache**:
```
export.Feeder struct: ~200 bytes
Map overhead:         ~50 bytes
Total:                ~250 bytes/feeder
```

**With 1000 authorized feeders, 50 connected**:
```
feeders:              1000 × 250 = 250 KB
feedersConnected:     50 × 100 = 5 KB
feederConnectionTime: 50 × 100 = 5 KB
Total:                ~260 KB
```

**Negligible** compared to frame processing (MB/s)

### CPU Usage

**Periodic operations**:
- Feeder list refresh: ~1-5ms every 60s
- Cleanup: ~0.1-1ms every 60s

**Per connection**:
- Authenticate(): ~10-50µs (map lookups + time comparisons)

**Lock contention**: Minimal (RW locks, read-heavy workload)

## Common Issues

### "Feeder already connected" Error

**Symptom**: Legitimate reconnect rejected

**Causes**:

1. **Previous connection not cleaned up**:
   ```
   Feeder disconnects ungracefully
   → SetDisconnected() never called
   → Still marked as connected
   ```

   **Solution**: Connection handler must call SetDisconnected() in defer

2. **Multiple instances with same API key**:
   ```
   Two receivers using same API key
   → First connects successfully
   → Second rejected
   ```

   **Solution**: Provision unique API keys per feeder

### "Connecting too frequently" Error

**Symptom**: Feeder can't reconnect after legitimate disconnect

**Causes**:

1. **Reconnecting within 30s window**:
   ```
   Network blip causes disconnect
   → Feeder auto-reconnects immediately
   → Blocked by rate limit
   ```

   **Solution**: Client-side backoff (wait 30s before reconnect)

2. **Connection loop**:
   ```
   Misconfigured client connects, immediately errors, reconnects
   → Rate limit prevents spam
   ```

   **Solution**: Fix client configuration, respect rate limit

### Stale Feeder List

**Symptom**: New feeder can't connect despite being added to database

**Causes**:

1. **Refresh hasn't run yet**:
   ```
   Feeder added at T=0s
   Last refresh at T=50s
   Next refresh at T=110s (60s interval)
   → Wait up to 60s
   ```

   **Solution**: Document propagation delay

2. **NATS API not returning new feeder**:
   ```
   Database updated
   → API cache not invalidated
   → FeederCache sees stale data
   ```

   **Solution**: Check API caching layers

## Production Lessons

> **Note to maintainers**: Add your observations here

### Typical Feeder Counts

**Small deployment**: 5-20 feeders
**Medium deployment**: 50-200 feeders
**Large deployment**: 500-2000 feeders

**Connection churn**:
- Normal: 1-5 disconnects/hour (network blips)
- Problematic: >20/hour (investigate)

<!--
Maintainers: Add feeder statistics from your deployments:
- Feeder count:
- Connection churn rate:
- Common auth failure reasons:
- NATS API latency:
-->

### When to Adjust Rate Limit

**30s too strict**:
- Mobile feeders (cellular handoffs)
- Flaky network environments
- Frequent legitimate restarts

**Consider**: 60s or 120s windows

**30s too lenient**:
- Abuse patterns observed
- Resource constraints

**Consider**: 10s or 15s windows

## Configuration

### Options

```go
cache, err := feederauth.New(
    // Required: NATS server URL
    feederauth.WithNatsURL("nats://user:pass@nats.example.com:4222"),

    // Optional: Custom logger
    feederauth.WithLogger(customLogger),
)
```

### Environment-Specific Settings

**Development**:
```go
feederauth.New(
    feederauth.WithNatsURL("nats://localhost:4222"),
)
```

**Production**:
```go
feederauth.New(
    feederauth.WithNatsURL("nats://user:pass@nats-cluster:4222"),
    // NATS cluster provides HA
)
```

## File Guide

| File | Purpose |
|------|---------|
| `feedercache.go` | Authentication, connection state, periodic refresh |

## See Also

- [Producer](../producer/README.md) - Where feeders connect
- [NATS API](../export/README.md#nats-api-message-types) - API message formats
- [Export](../export/README.md) - Feeder struct definition
