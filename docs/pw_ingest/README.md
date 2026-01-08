# pw_ingest - Aircraft Tracking Client

## Overview

`pw_ingest` is the primary client application for ingesting ADS-B data from receivers, tracking aircraft in real-time, and publishing location updates to downstream consumers (typically NATS). It acts as the entry point for raw ADS-B data into the Plane.Watch pipeline.

## Purpose

**Client-side tracking**: Connects to receivers (dump1090, readsb, etc.) to collect and track aircraft

**Why client mode**:
- Deployed alongside receivers for low-latency tracking
- Reduces central infrastructure load (tracking distributed)
- Enables edge processing and filtering
- Supports multiple simultaneous receivers per client

## Architecture

```
[dump1090] ──beast──┐
[readsb]   ──avr────┤
[other]    ──sbs1───┼──> pw_ingest ──┐
                                     ├──> Dedupe
                                     ├──> Tracker
                                     ├──> IngestTap
                                     └──> Sink (NATS)
```

### Components Used

- **[Producer](../producer/README.md)**: Fetches frames from sources
- **[Tracker](../tracker/README.md)**: Tracks aircraft state
- **[Dedupe](../dedupe/README.md)**: Optional frame deduplication
- **[Middleware](../middleware/README.md)**: IngestTap for monitoring
- **[Sink](../sink/README.md)**: Publishes events to NATS
- **[Monitoring](../monitoring/README.md)**: Prometheus metrics

## Operating Modes

### Simple Mode

**Usage**: Interactive CLI with human-readable output

```bash
pw_ingest \
  --fetch beast://192.168.1.10:30005 \
  --sink nats://localhost:4222 \
  --tag my-receiver \
  simple
```

**When to use**:
- Development and testing
- Manual monitoring
- Debugging receiver issues
- Learning how the system works

**Output**: Pretty-printed logs to console

### Daemon Mode

**Usage**: Production deployment with JSON logging

```bash
pw_ingest \
  --fetch beast://receiver:30005 \
  --sink nats://nats-server:4222 \
  --tag production-rx1 \
  daemon
```

**When to use**:
- Production deployments
- Docker containers
- SystemD services
- Log aggregation systems (ELK, Splunk)

**Output**: JSON-formatted logs for machine parsing

### Filter Mode

**Usage**: Hunt for specific DF/ME frame examples

```bash
pw_ingest \
  --fetch beast://receiver:30005 \
  --df 17 \
  --me 19 \
  --filter-icao A12345 \
  --locations-only \
  filter
```

**When to use**:
- Collecting test data
- Finding specific frame types for debugging
- Building frame example databases
- Research and analysis

**Output**: Matching frames logged/saved

## Command-Line Flags

### Source Flags (from setup package)

**--fetch**: Connect to receiver (client mode)
```bash
--fetch beast://host:port?tag=name&refLat=47.6&refLon=-122.3
--fetch avr://host:port
--fetch sbs1://host:port
```

**--listen**: Accept connections (server mode - not typical for pw_ingest)
```bash
--listen beast://:30005
```

**--file**: Replay from file
```bash
--file beast://path/to/recording.beast?delay=yes
```

### Sink Flags

**--sink**: Output destination
```bash
--sink nats://localhost:4222
--sink file://output.json
```

**--tag**: Source identifier for multi-receiver setups
```bash
--tag north-antenna
```

### Tracking Flags

**--dedupe-filter**: Enable frame deduplication
```bash
--dedupe-filter
```

**Why dedupe**: Combo feeds (multiple SDRs) produce duplicates
- Without: 30-50% duplicate frames
- With: Duplicates eliminated, reduced downstream load

**--decode-worker-count**: Number of parallel decode workers
```bash
--decode-worker-count 4
```

**Default**: 1 worker
**When to increase**: High frame rate (>2000 frames/sec), multi-core CPU

### Filter Flags (filter mode only)

**--filter-icao**: Filter specific aircraft
```bash
--filter-icao E48DF6 --filter-icao 123ABC
```

**--locations-only**: Only DF17 location messages
```bash
--locations-only
```

### Monitoring Flags

**--prometheus-port**: Metrics endpoint port
```bash
--prometheus-port 9602  # Default
```

**--quiet**: Suppress non-error output
**--debug**: Verbose debug logging

## Configuration Examples

### Single Receiver

```bash
pw_ingest \
  --fetch beast://192.168.1.10:30005 \
  --sink nats://nats-server:4222 \
  --tag home-receiver \
  --prometheus-port 9602 \
  daemon
```

### Multiple Receivers (Combo Feed)

```bash
pw_ingest \
  --fetch beast://sdr1:30005?tag=north \
  --fetch beast://sdr2:30005?tag=south \
  --fetch beast://sdr3:30005?tag=east \
  --dedupe-filter \
  --sink nats://nats-server:4222 \
  daemon
```

**Why dedupe**: Same aircraft seen by multiple SDRs produces duplicate frames

### File Replay

```bash
pw_ingest \
  --file beast://capture.beast?delay=yes \
  --sink file://decoded.json \
  simple
```

**delay=yes**: Replay at original timing (respects Beast timestamps)
**delay=no**: Process as fast as possible

### MLAT Receiver

```bash
pw_ingest \
  --fetch beast://receiver:30005?refLat=47.6062&refLon=-122.3321 \
  --sink nats://nats-server:4222 \
  --tag seattle-mlat \
  daemon
```

**refLat/refLon**: Reference position for CPR decoding (required for MLAT)

## Prometheus Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `pw_ingest_num_decoded_frames` | Gauge | Total frames successfully decoded |
| `pw_ingest_num_decode_errors` | Gauge | Frames with decode errors |
| `pw_ingest_current_tracked_planes_count` | Gauge | Aircraft currently being tracked |
| `pw_ingest_num_planes_purged_before_viable` | Gauge | Aircraft purged before reaching viability threshold |
| `pw_ingest_output_frame_dedupe_total` | Counter | Duplicate frames filtered (if dedupe enabled) |
| `pw_ingest_info` | Gauge | Application version metadata |

### Monitoring Examples

**Check frame rate**:
```bash
curl -s http://localhost:9602/metrics | grep num_decoded_frames
```

**Check aircraft count**:
```bash
curl -s http://localhost:9602/metrics | grep current_tracked_planes
```

**Check dedupe effectiveness**:
```bash
curl -s http://localhost:9602/metrics | grep dedupe_total
```

## Production Deployment

### Docker

```dockerfile
FROM golang:1.21 AS builder
WORKDIR /build
COPY . .
RUN go build -o pw_ingest ./cmd/pw_ingest

FROM debian:bookworm-slim
COPY --from=builder /build/pw_ingest /usr/local/bin/
ENTRYPOINT ["pw_ingest"]
CMD ["daemon"]
```

**docker-compose.yml**:
```yaml
services:
  pw_ingest:
    image: planewatch/pw_ingest:latest
    command: daemon
    environment:
      - FETCH=beast://dump1090:30005
      - SINK=nats://nats:4222
      - TAG=docker-receiver
      - DEDUPE=true
    ports:
      - "9602:9602"
    restart: unless-stopped
```

### SystemD

**/etc/systemd/system/pw_ingest.service**:
```ini
[Unit]
Description=Plane.Watch Ingest Client
After=network.target

[Service]
Type=simple
User=planewatch
ExecStart=/usr/local/bin/pw_ingest \
  --fetch=beast://localhost:30005 \
  --sink=nats://nats-server:4222 \
  --tag=%H \
  daemon
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

**Enable and start**:
```bash
sudo systemctl enable pw_ingest
sudo systemctl start pw_ingest
sudo systemctl status pw_ingest
```

### Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: pw-ingest
spec:
  replicas: 1
  selector:
    matchLabels:
      app: pw-ingest
  template:
    metadata:
      labels:
        app: pw-ingest
    spec:
      containers:
      - name: pw-ingest
        image: planewatch/pw_ingest:latest
        args:
          - daemon
        env:
          - name: FETCH
            value: "beast://dump1090:30005"
          - name: SINK
            value: "nats://nats:4222"
          - name: TAG
            valueFrom:
              fieldRef:
                fieldPath: metadata.name
        ports:
          - containerPort: 9602
            name: metrics
```

## Performance Tuning

### Decode Workers

**Single receiver, low traffic** (< 1000 frames/sec):
```bash
--decode-worker-count 1  # Default, sufficient
```

**High traffic** (> 2000 frames/sec):
```bash
--decode-worker-count 4  # Match CPU cores
```

**Combo feed, multiple receivers**:
```bash
--decode-worker-count 8  # More sources = more parallelism
```

**CPU usage**: Each worker ~10-20% CPU at 1000 frames/sec

### Memory Management

**Typical usage**: 50-200 MB
- Tracker: ~200 bytes per aircraft
- Dedupe map: ~50 bytes per unique frame (if enabled)
- Frame buffers: ~10 KB per producer

**High aircraft count** (500+ aircraft):
- Memory: ~100-150 MB
- Pruning after 5 minutes keeps bounded

### Deduplication

**Enable when**:
- Multiple receivers seeing same aircraft
- Combo feeds (multiple SDRs)
- Overlapping coverage areas

**Disable when**:
- Single receiver
- Non-overlapping coverage
- Need every frame for research

**Overhead**: ~2-3% CPU, ~10-30 MB RAM

## Troubleshooting

### No Aircraft Tracked

**Symptom**: `current_tracked_planes_count` stays at 0

**Causes**:
1. **Receiver not sending data**
   ```bash
   # Check receiver
   nc -v receiver-host 30005
   ```

2. **Wrong format specified**
   ```bash
   # Verify format: beast vs avr vs sbs1
   # Check receiver output format configuration
   ```

3. **Network issues**
   ```bash
   # Check connectivity
   telnet receiver-host 30005
   ```

4. **Frames invalid**
   ```bash
   # Check decode errors
   curl http://localhost:9602/metrics | grep decode_errors
   ```

### High Decode Errors

**Symptom**: `num_decode_errors` increasing rapidly

**Causes**:
- Poor receiver signal quality (check antenna, LNA)
- Interference (check spectrum for noise)
- Wrong data format (beast/avr/sbs1 mismatch)

**Investigation**:
```bash
# Enable debug logging
pw_ingest --debug ... simple

# Check for specific error patterns in logs
```

### Aircraft Purged Before Viable

**Symptom**: `num_planes_purged_before_viable` high

**Meaning**: Aircraft only received 1 frame before timeout

**Normal**: 10-20% purge rate in busy airspace (distant/weak aircraft)
**High (>50%)**: Signal quality issue or high noise floor

### Memory Growth

**Symptom**: RSS continuously increasing

**Check**:
```bash
# Aircraft count should stabilize
curl http://localhost:9602/metrics | grep current_tracked_planes

# If growing unbounded, pruning may be disabled (shouldn't happen)
```

**Dedupe memory**:
```bash
# If using dedupe, map size grows with frame rate
# 60-second window: 5000 frames/sec = ~300k entries = ~15 MB
```

### NATS Connection Failures

**Symptom**: "Failed to connect to NATS" errors

**Check**:
```bash
# NATS server running
nats-server --version

# Connection string format
--sink nats://user:pass@host:4222

# Network connectivity
telnet nats-host 4222
```

## Best Practices

### Always Tag Receivers

```bash
--tag north-antenna  # Enables source tracking
```

**Why**: Multi-source deployments need attribution

### Use Dedupe for Combo Feeds

```bash
--dedupe-filter  # Essential for multiple SDRs
```

**Why**: 30-50% duplicate reduction

### Monitor Metrics

**Grafana dashboard**:
- Frame rate trends
- Aircraft count over time
- Decode error rate
- Purge rate patterns

### Set Reference Position

```bash
--fetch beast://rx:30005?refLat=47.6&refLon=-122.3
```

**Why**: CPR decoding requires known reference for first position

### Appropriate Worker Count

**Don't over-provision**:
```bash
--decode-worker-count 32  # Wasteful for 1000 frames/sec
```

**Rule of thumb**: 1 worker per 1000-2000 frames/sec

## See Also

- [Tracker](../tracker/README.md) - Core tracking engine used by pw_ingest
- [Producer](../producer/README.md) - Data source abstraction
- [Sink](../sink/README.md) - Event publishing
- [Dedupe](../dedupe/README.md) - Frame deduplication
- [Middleware](../middleware/README.md) - IngestTap monitoring
- [runway](../runway/README.md) - Server alternative (accepts connections)

## References

- dump1090: https://github.com/antirez/dump1090 (original)
- readsb: https://github.com/wiedehopf/readsb (modern fork)
- NATS.io: https://docs.nats.io/
