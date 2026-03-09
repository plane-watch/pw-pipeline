# pw_router - Location Update Router & Filter

## Overview

`pw_router` is the intelligent routing and filtering service that consumes raw location updates, determines which changes are "significant," and routes them to tile-based queues for geographic distribution. It achieves 90%+ reduction in update volume while preserving all meaningful position changes.

## Purpose

**Update filtering**: Not all location updates are worth propagating

**Why filtering**:
- Aircraft broadcasts every second, but position changes every 5-10 seconds
- 90% of updates are redundant (same position, altitude, heading)
- Downstream consumers (WebSocket clients) can't process every update
- Network bandwidth and storage costs scale with update volume

**The reduction problem**:
```
Raw updates:     1000/sec per tile  (every update)
Significant:      100/sec per tile  (90% reduction)
Clients receive:  Only meaningful changes
```

## Architecture

```
NATS: location-updates (all)
         ↓
    ┌────────┐
    │pw_router│
    │  Cache │ (last known state per aircraft)
    └────┬───┘
         ├──> tile60_low  (significant only)
         ├──> tile60_high (all updates)
         ├──> tile61_low
         └──> ...75 tiles

```

### Significance Determination

**Significant if**:
- Position changed > threshold (e.g., 0.001° ≈ 100m)
- Altitude changed > threshold (e.g., 500 feet)
- Heading changed > threshold (e.g., 5°)
- Velocity changed > threshold (e.g., 50 knots)
- First update seen (new aircraft)
- Last update before removal

**Insignificant if**:
- All fields within thresholds of last update
- Redundant broadcast (same data)

## Components Used

- **[NATS](../nats_io/README.md)**: Message bus for updates
- **[Dedupe/ForgetfulMap](../dedupe/README.md)**: Cache with auto-eviction
- **[TileGrid](../tile_grid/README.md)**: Geographic tile assignment
- **[ClickHouse](../clickhouse/README.md)**: Optional storage integration
- **[Monitoring](../monitoring/README.md)**: Prometheus metrics

## Operating Modes

### Daemon Mode

**Production**: JSON logging

```bash
pw_router \
  --nats nats://localhost:4222 \
  --source-route-key location-updates \
  --spread-updates \
  daemon
```

### CLI Mode

**Development**: Human-readable logging

```bash
pw_router \
  --nats nats://localhost:4222 \
  --source-route-key location-updates \
  cli
```

## Command-Line Flags

### NATS Configuration

**--nats**: NATS server URL
```bash
--nats nats://user:pass@nats-server:4222
```

**--source-route-key**: Input topic (consume from)
```bash
--source-route-key location-updates  # Default
```

**--destination-route-key**: Output topic for significant updates
```bash
--destination-route-key location-updates-enriched-reduced  # Default (low)
```

**--destination-route-key-merged**: Output topic for all updates
```bash
--destination-route-key-merged location-updates-enriched-merged  # Default (high)
```

### Routing

**--spread-updates**: Route to tile-specific queues
```bash
--spread-updates  # Enables tile60_low, tile60_high, etc.
```

**Without spread-updates**:
- Only publishes to single destination queue
- No geographic filtering

**With spread-updates**:
- Publishes to tile-specific queues
- Enables regional subscriptions
- `tile{N}_low`: Significant updates only
- `tile{N}_high`: All updates

### Workers

**--num-workers**: Parallel update processors
```bash
--num-workers 10  # Default
```

**Scaling**:
- Low traffic (<100 updates/sec): 1-5 workers
- Medium (100-1000): 10-20 workers
- High (>1000): 20-50 workers

**CPU usage**: ~5-10% per worker at 100 updates/sec

### Cache Management

**--update-age**: Seconds to retain cached state
```bash
--update-age 300  # Default: 5 minutes
```

**Why 300 seconds**:
- Aircraft not heard from in 5 minutes likely left coverage
- Prevents unbounded cache growth
- Balances memory vs update accuracy

**--update-age-sweep-interval**: Cache cleanup frequency
```bash
--update-age-sweep-interval 30  # Default: 30 seconds
```

**Sweep overhead**: Minimal (<1% CPU)

### ClickHouse Integration

**--clickhouse**: Optional storage for updates
```bash
--clickhouse clickhouse://user:pass@host:9000/database
```

**When enabled**: All significant updates also written to ClickHouse

### Monitoring

**--prometheus-port**: Metrics endpoint
```bash
--prometheus-port 9601  # Default (note: different from pw_ingest 9602)
```

## Prometheus Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `pw_router_updates_processed_total` | Counter | All messages processed |
| `pw_router_updates_significant_total` | Counter | Updates determined significant |
| `pw_router_updates_insignificant_total` | Counter | Updates filtered as insignificant |
| `pw_router_updates_ignored_total` | Counter | Insignificant updates not published |
| `pw_router_updates_published_total` | Counter | Updates published to output queues |
| `pw_router_updates_error_total` | Counter | Processing errors |
| `pw_router_cache_planes_count` | Gauge | Aircraft in reduction cache |
| `pw_router_cache_eviction_total` | Counter | Cache evictions (aged out) |
| `pw_router_info` | Gauge | Application version |

### Calculating Reduction Rate

```bash
# Fetch metrics
curl -s http://localhost:9601/metrics > metrics.txt

# Calculate reduction
processed=$(grep updates_processed_total metrics.txt | awk '{print $2}')
published=$(grep updates_published_total metrics.txt | awk '{print $2}')
reduction=$(echo "scale=2; 100 * (1 - $published / $processed)" | bc)

echo "Reduction: ${reduction}%"
```

**Typical reduction**: 85-95%

## Configuration Examples

### Basic Setup

```bash
pw_router \
  --nats nats://localhost:4222 \
  --spread-updates \
  daemon
```

### High-Traffic Setup

```bash
pw_router \
  --nats nats://nats-cluster:4222 \
  --num-workers 30 \
  --update-age 600 \
  --spread-updates \
  daemon
```

### With ClickHouse Storage

```bash
pw_router \
  --nats nats://localhost:4222 \
  --clickhouse clickhouse://default:@localhost:9000/planewatch \
  --spread-updates \
  daemon
```

### Development/Debugging

```bash
pw_router \
  --nats nats://localhost:4222 \
  --num-workers 1 \
  --debug \
  cli
```

## Production Deployment

### Docker

```yaml
# docker-compose.yml
services:
  pw_router:
    image: planewatch/pw_router:latest
    command: daemon
    environment:
      - NATS=nats://nats:4222
      - SPREAD=true
      - NUM_WORKERS=20
      - UPDATE_AGE=300
    ports:
      - "9601:9601"
    restart: unless-stopped
    depends_on:
      - nats
```

### Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: pw-router
spec:
  replicas: 3  # For high availability
  selector:
    matchLabels:
      app: pw-router
  template:
    metadata:
      labels:
        app: pw-router
    spec:
      containers:
      - name: pw-router
        image: planewatch/pw_router:latest
        args: ["daemon"]
        env:
          - name: NATS
            value: "nats://nats:4222"
          - name: SPREAD
            value: "true"
          - name: NUM_WORKERS
            value: "20"
        ports:
          - containerPort: 9601
            name: metrics
        resources:
          requests:
            cpu: 500m
            memory: 256Mi
          limits:
            cpu: 2000m
            memory: 1Gi
```

## How It Works

### Worker Pool

**10 workers** (default) process updates in parallel:

1. **Message received** from NATS `location-updates`
2. **Worker picks up** message from channel (buffered: 1000)
3. **Parse JSON** to PlaneLocation struct
4. **Lookup cache**: Get last known state for this ICAO
5. **Compare**: Determine if significant change
6. **Route**:
   - If significant → publish to `_low` queues
   - If --spread-updates → also route to tile queues
   - If merged → publish to `_high` queues
7. **Update cache**: Store current state as "last known"
8. **Repeat**

### Cache Strategy

**ForgetfulSyncMap**:
- Key: Aircraft ICAO (e.g., "A12345")
- Value: Last PlaneLocation
- Auto-eviction: After 300 seconds idle
- Sweep: Every 30 seconds

**Why forgetful**:
- Aircraft leave coverage areas
- Without eviction: unbounded growth
- 300 seconds: ~2.5x typical absence before return

### Tile Routing

**Tile assignment**:
```go
tile := tile_grid.GetTile(lat, lon)  // e.g., "tile60"
```

**Queue names**:
- `tile60_low`: Significant updates for Pacific Northwest
- `tile60_high`: All updates for Pacific Northwest
- (Repeated for all 75 tiles)

**Why low/high split**:
- WebSocket clients subscribe to `_low` (bandwidth-efficient)
- Storage/analytics subscribe to `_high` (complete data)
- Research subscribes to `_high` (every update)

## Performance

### Typical Metrics

| Metric | Value |
|--------|-------|
| Input rate | 100-1000 updates/sec |
| Output rate (significant) | 10-100 updates/sec |
| Reduction | 85-95% |
| CPU (10 workers) | 10-30% (1-2 cores) |
| Memory | 50-200 MB |
| Cache size | 100-2000 aircraft |

### Bottlenecks

**NATS throughput**: 100k+ msg/sec (not a bottleneck)

**Worker count**: CPU-bound at high rates
- Too few workers: Input channel backs up
- Too many workers: Context switching overhead

**Cache lookup**: O(1) with sync.Map
- Not a bottleneck even at 10k updates/sec

## Troubleshooting

### Low Reduction Rate

**Symptom**: Reduction <50%

**Causes**:
1. **Cache eviction too aggressive**
   ```bash
   # Increase retention
   --update-age 600  # 10 minutes
   ```

2. **High aircraft turnover**
   - Airport environments: Aircraft constantly arriving/departing
   - Every "first seen" counts as significant
   - Normal in high-traffic areas

3. **Significance thresholds too tight**
   - Coded in worker logic
   - May need adjustment for use case

### High Cache Evictions

**Symptom**: `cache_eviction_total` very high

**Normal**: In low-traffic periods
- Aircraft leave coverage
- Cache evicts after 5 minutes

**Abnormal**: Evictions during high traffic
- `--update-age` too short
- Increase to 600 or more

### Input Channel Backing Up

**Symptom**: Lag between ingestion and routing

**Cause**: Too few workers

**Solution**:
```bash
--num-workers 30  # Increase workers
```

**Verify**:
```bash
# Check NATS queue depth
# Should be near zero if keeping up
```

### Memory Growth

**Symptom**: RSS growing over time

**Check cache size**:
```bash
curl -s http://localhost:9601/metrics | grep cache_planes_count
```

**Expected**: Stabilizes at ~100-2000 aircraft
**Growing unbounded**: Cache eviction may be disabled (bug)

## Best Practices

### Always Use --spread-updates

**Production deployments**:
```bash
--spread-updates  # Essential for tile-based routing
```

**Why**: Enables geographic subscriptions downstream

### Tune Workers for Load

**Monitor CPU**:
- <50% CPU: Can reduce workers
- >80% CPU: Need more workers

**Rule of thumb**: 1 worker per 100 updates/sec

### Set Appropriate Cache Age

**High-traffic hubs** (KSFO, KJFK):
```bash
--update-age 180  # 3 minutes (fast turnover)
```

**Low-traffic areas**:
```bash
--update-age 600  # 10 minutes (aircraft linger)
```

### Monitor Reduction Rate

**Grafana dashboard**:
- Track `updates_processed` vs `updates_published`
- Alert if reduction <70% (may indicate issue)
- Trend over time

## See Also

- [pw_ingest](../pw_ingest/README.md) - Produces location-updates
- [pw_ws_broker](../pw_ws_broker/README.md) - Consumes tile queues
- [NATS](../nats_io/README.md) - Message bus
- [TileGrid](../tile_grid/README.md) - Geographic partitioning
- [Dedupe](../dedupe/README.md) - ForgetfulMap implementation

## References

- NATS JetStream: https://docs.nats.io/jetstream
- Forgetful maps: Internal docs in dedupe package
