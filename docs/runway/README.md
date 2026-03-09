# runway - Authenticated Feeder Server

## Overview

`runway` is the server-side application that accepts incoming connections from authenticated feeders, tracks aircraft from multiple sources, and publishes location updates to NATS. It's the "border force" that manages feeder authentication and data ingestion at scale.

**Name origin**: Runway is where planes land (feeders connect) for processing.

## Purpose

**Server-side ingestion**: Accepts connections instead of initiating them

**Why server mode**:
- Centralized control of feeder connections
- Authentication and authorization at connection time
- Simplified feeder configuration (no NATS credentials needed)
- Load balancing across multiple runway instances
- Per-feeder statistics and rate limiting

## Architecture

```
[Feeder 1] ─TLS/SNI─┐
[Feeder 2] ─TLS/SNI─┼──> runway ─┬──> Accounting
[Feeder 3] ─TLS/SNI─┤              ├──> Dedupe
          (authenticated)          ├──> IngestTap
                                   └──> NATS
```

### vs pw_ingest

| Feature | pw_ingest | runway |
|---------|-----------|--------|
| Connection | Client (connects out) | Server (accepts in) |
| Authentication | Not required | SNI-based, required |
| Use case | Trusted receivers | Untrusted feeders |
| Deployment | Edge (at receiver) | Central (data center) |
| Scaling | Horizontal (many clients) | Vertical (powerful server) |

## Components Used

- **[Stunnel](../stunnel/README.md)**: TLS listener with SNI extraction
- **[Feederauth](../feederauth/README.md)**: API key validation
- **[Tracker](../tracker/README.md)**: Aircraft state tracking
- **[Middleware](../middleware/README.md)**: Accounting, deduplication, monitoring
- **[MLATBridge](../mlatbridge/README.md)**: MLAT connection handling
- **[Sink](../sink/README.md)**: NATS publishing

## Operating Mode

### Daemon Mode

**Only mode**: Production server with JSON logging

```bash
runway \
  --cert server.crt \
  --key server.key \
  --listen-beast :12345 \
  --listen-mlat :12346 \
  --sink nats://nats-server:4222 \
  daemon
```

**Why daemon only**: Server applications don't need interactive CLI

## Listener Endpoints

### Beast Endpoint (Port 12345)

**Purpose**: Accept Beast binary format connections

**Protocol**: TLS with SNI-based authentication

**Feeder connects**:
```bash
# Feeder (using stunnel or native TLS)
stunnel-client \
  --host runway.plane.watch \
  --port 12345 \
  --sni <api-key>.feed.plane.watch \
  beast://localhost:30005
```

**SNI format**: `<uuid>.feed.plane.watch`
- UUID: Feeder API key from ATC API
- Validates against atc.plane.watch
- Rejected if invalid/unauthorized

### MLAT Endpoint (Port 12346)

**Purpose**: Accept MLAT (multilateration) connections

**Protocol**: TLS with SNI authentication, bidirectional

**MLAT data flow**:
```
Feeder → runway → NATS (mlat-in)
NATS (mlat-sync) → runway → Feeder
```

**Why bidirectional**: MLAT servers send sync messages back to feeders

## Authentication Flow

1. **Feeder connects** with TLS SNI containing API key
2. **SNI extracted**: `abc123.feed.plane.watch` → API key `abc123`
3. **Key validated**: Checked against feederauth cache
4. **Cache miss**: Queries atc.plane.watch via NATS RPC
5. **Result**:
   - Valid: Connection accepted, producer created
   - Invalid: Connection rejected, TCP close

**Auth refresh**: Every 60 seconds (feederauth polls ATC API)

**Connection tracking**:
- Authorized: API key valid
- Connected: Active connection exists
- Rate-limited: Stats tracked per feeder

## Command-Line Flags

### Network

**--listen-beast**: Beast endpoint address
```bash
--listen-beast :12345      # All interfaces
--listen-beast 0.0.0.0:12345
```

**--listen-mlat**: MLAT endpoint address
```bash
--listen-mlat :12346
```

### TLS Certificate

**--cert**: Server certificate (PEM format)
```bash
--cert /path/to/server.crt
```

**--key**: Private key (PEM format)
```bash
--key /path/to/server.key
```

**Requirements**:
- X.509 certificate
- Wildcard or SAN for `*.feed.plane.watch`
- Valid certificate chain
- Auto-reload every 5 minutes

### Sink

**--sink**: NATS destination
```bash
--sink nats://user:pass@nats-server:4222
```

**Published topics**:
- `location-updates`: Aircraft positions
- `feeder-updates`: Feeder statistics
- `mlat-in`: MLAT data from feeders

### ATC API

**--atcupdatefreq**: Feeder auth refresh interval (minutes)
```bash
--atcupdatefreq 1  # Default: every minute
```

**Why refresh**: Feeder API keys can be revoked

### Monitoring

**--prometheus-port**: Metrics endpoint
```bash
--prometheus-port 9602  # Default
```

## Middleware Pipeline

**Order matters** - configured in this sequence:

1. **Accounting**: Frame count per feeder (before dedupe)
2. **Dedupe**: Eliminate duplicate frames
3. **IngestTap**: Monitoring tap for debugging

```go
trk.AddMiddleware(middleware.NewAccounting(...))  // Count raw frames
trk.AddMiddleware(dedupe.NewFilter(...))          // Remove duplicates
trk.AddMiddleware(middleware.NewIngestTap(...))   // Monitor post-dedupe
```

**Why accounting first**: Track raw input from each feeder

## Prometheus Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `pw_ingest_num_decoded_frames` | Gauge | Total frames decoded across all feeders |
| `pw_ingest_num_decode_errors` | Gauge | Decode errors |
| `pw_ingest_current_tracked_planes_count` | Gauge | Aircraft currently tracked |
| `pw_ingest_num_planes_purged_before_viable` | Gauge | Aircraft purged before viable |
| `pw_ingest_output_frame_dedupe_total` | Counter | Duplicate frames filtered |
| `pw_ingest_info` | Gauge | Application version |

**Note**: Uses same metrics namespace as pw_ingest for consistency

## Production Deployment

### Docker

```dockerfile
FROM golang:1.21 AS builder
WORKDIR /build
COPY . .
RUN go build -o runway ./cmd/runway

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=builder /build/runway /usr/local/bin/
EXPOSE 12345 12346 9602
ENTRYPOINT ["runway"]
CMD ["daemon"]
```

**docker-compose.yml**:
```yaml
services:
  runway:
    image: planewatch/runway:latest
    command: daemon
    environment:
      - LISTEN_BEAST=:12345
      - LISTEN_MLAT=:12346
      - CERT_FILE=/etc/runway/server.crt
      - KEY_FILE=/etc/runway/server.key
      - SINK=nats://nats:4222
    volumes:
      - ./certs:/etc/runway:ro
    ports:
      - "12345:12345"  # Beast
      - "12346:12346"  # MLAT
      - "9602:9602"    # Metrics
    restart: unless-stopped
```

### Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: runway
spec:
  replicas: 2  # For high availability
  selector:
    matchLabels:
      app: runway
  template:
    metadata:
      labels:
        app: runway
    spec:
      containers:
      - name: runway
        image: planewatch/runway:latest
        args: ["daemon"]
        env:
          - name: LISTEN_BEAST
            value: ":12345"
          - name: LISTEN_MLAT
            value: ":12346"
          - name: SINK
            value: "nats://nats:4222"
          - name: CERT_FILE
            value: "/etc/tls/tls.crt"
          - name: KEY_FILE
            value: "/etc/tls/tls.key"
        ports:
          - containerPort: 12345
            name: beast
          - containerPort: 12346
            name: mlat
          - containerPort: 9602
            name: metrics
        volumeMounts:
          - name: tls
            mountPath: /etc/tls
            readOnly: true
      volumes:
        - name: tls
          secret:
            secretName: runway-tls
---
apiVersion: v1
kind: Service
metadata:
  name: runway
spec:
  type: LoadBalancer
  ports:
    - port: 12345
      name: beast
    - port: 12346
      name: mlat
  selector:
    app: runway
```

### TLS Certificate Setup

**Generate self-signed** (development):
```bash
openssl req -x509 -newkey rsa:4096 \
  -keyout server.key \
  -out server.crt \
  -days 365 \
  -nodes \
  -subj "/CN=*.feed.plane.watch"
```

**Let's Encrypt** (production):
```bash
certbot certonly --standalone \
  -d feed.plane.watch \
  -d *.feed.plane.watch
```

**Certificate requirements**:
- Must cover `*.feed.plane.watch` (wildcard)
- Or individual SANs for each possible SNI
- PEM format
- Readable by runway process

## Feeder Configuration

### Feeder-Side Setup

**Option 1**: Native TLS client (if supported)
```bash
# Some clients support TLS natively
dump1090 --net-sbs-port 30003 \
         --net-beast-output runway.plane.watch:12345 \
         --tls-sni abc123.feed.plane.watch
```

**Option 2**: stunnel wrapper
```ini
# /etc/stunnel/runway.conf
[beast-to-runway]
client = yes
accept = 127.0.0.1:31005
connect = runway.plane.watch:12345
sni = abc123.feed.plane.watch  # Your API key
```

```bash
stunnel /etc/stunnel/runway.conf

# Point local receiver to stunnel
dump1090 --net-beast-reduce-port 31005
```

**API Key**: Obtained from ATC API (atc.plane.watch)

## Performance & Scaling

### Capacity

**Single runway instance**:
- Feeders: 100-500 concurrent
- Frame rate: 50,000-100,000 frames/sec
- CPU: 2-4 cores (1 decode worker per feeder)
- Memory: 500 MB - 2 GB

**Scaling horizontally**:
```
                     ┌──> runway-1 (feeders 1-250)
Load Balancer (L4) ──┼──> runway-2 (feeders 251-500)
                     └──> runway-3 (feeders 501-750)
```

**Why L4 (TCP) load balancing**: Sticky sessions required (stateful connections)

### Decode Workers

**One worker per feeder**: Hard-coded to 1
```go
tracker.WithDecodeWorkerCount(1)  // Only need single decoder per source
```

**Why 1**: Each feeder is independent source
- Parallel decode across feeders
- No benefit to multiple workers per feeder
- Reduces context switching

### Resource Usage

**Per feeder**:
- CPU: ~2-5% per active feeder
- Memory: ~2-5 MB per feeder
- Network: 10-50 KB/sec per feeder

**Total (100 feeders)**:
- CPU: 200-500% (2-5 cores)
- Memory: 200-500 MB
- Network: 1-5 MB/sec aggregate

## Troubleshooting

### Feeders Can't Connect

**Symptom**: Connection refused or timeout

**Causes**:
1. **Firewall blocking ports**
   ```bash
   # Check port open
   nc -zv runway-host 12345
   ```

2. **Certificate issues**
   ```bash
   # Verify certificate
   openssl s_client -connect runway:12345 -servername test.feed.plane.watch
   ```

3. **Wrong SNI format**
   ```bash
   # Must be: <uuid>.feed.plane.watch
   # Not: just <uuid> or plane.watch
   ```

### Authentication Failures

**Symptom**: Connections immediately closed

**Causes**:
1. **Invalid API key**
   - Check key registered in ATC API
   - Verify SNI contains correct key

2. **ATC API unreachable**
   ```bash
   # Check feederauth can reach ATC
   # Logs will show NATS RPC failures
   ```

3. **Auth cache stale**
   - Restart runway to refresh
   - Check `--atcupdatefreq` setting

### No Aircraft Tracked Despite Connections

**Symptom**: Feeders connected but `current_tracked_planes_count` = 0

**Investigation**:
```bash
# Check frame decode errors
curl http://localhost:9602/metrics | grep decode_errors

# Check feeder accounting
# Look in NATS for feeder-updates topic

# Enable debug logging
# Check for specific decode failures
```

### High Dedupe Rate

**Symptom**: `output_frame_dedupe_total` very high

**Normal**: 30-50% with overlapping coverage
**Very high (>80%)**: Possible issues
- Multiple feeders on same receiver
- Shared antenna between feeders
- Misconfigured feeder tags

### Memory Leak

**Symptom**: Memory continuously growing

**Check**:
```bash
# Connections not cleaned up?
netstat -an | grep :12345 | wc -l

# Aircraft count growing?
curl http://localhost:9602/metrics | grep tracked_planes

# Dedupe map size growing?
# Check frame rate vs retention window
```

## Security Considerations

### TLS Required

**All connections encrypted**: No plain TCP option

**Why**: Feeder data could be intercepted/manipulated

**Certificate rotation**: Auto-reloads every 5 minutes
```bash
# Update certificate
cp new-cert.crt /etc/runway/server.crt
cp new-key.key /etc/runway/server.key

# Runway detects and reloads within 5 minutes
```

### SNI-Based Authentication

**Advantages**:
- No credentials in feeder config files
- API key in TLS handshake (not plain text)
- Central revocation (ATC API)

**Disadvantages**:
- SNI visible in network traffic
- Not suitable for highly sensitive environments

**Alternative**: Use VPN + IP whitelisting

### Rate Limiting

**Tracked via accounting middleware**:
- Frames per feeder
- Updates to ATC API every minute
- Can implement throttling if needed

## Monitoring

### Health Check

```bash
# Application health
curl http://localhost:9602/status

# Prometheus metrics
curl http://localhost:9602/metrics
```

### Grafana Dashboard

**Key metrics to monitor**:
1. Active feeder connections (track via accounting)
2. Total frame rate across all feeders
3. Dedupe effectiveness
4. Aircraft count trend
5. Decode error rate

**Alerts**:
- Frame rate drop (feeder disconnections)
- High decode errors (data quality)
- Memory growth (potential leak)
- CPU saturation (need more instances)

## Best Practices

### Use Load Balancer

**For high availability**:
- Multiple runway instances
- L4 (TCP) load balancing
- Health check on metrics endpoint

### Monitor Certificate Expiry

```bash
# Check certificate expiration
openssl x509 -in server.crt -noout -enddate

# Set up renewal automation (Let's Encrypt)
certbot renew --deploy-hook "cp /etc/letsencrypt/live/feed.plane.watch/* /etc/runway/"
```

### Tune Auth Refresh

**Default 1 minute**: Good for most

**Faster (30 seconds)**: Quick revocation response
```bash
--atcupdatefreq 0.5
```

**Slower (5 minutes)**: Reduce ATC API load
```bash
--atcupdatefreq 5
```

### Separate MLAT Infrastructure

**Consideration**: MLAT has different requirements
- Bidirectional communication
- Time-critical sync messages
- Separate port already (12346)

**Could deploy**:
- Separate runway instance for MLAT only
- Different scaling characteristics

## See Also

- [pw_ingest](../pw_ingest/README.md) - Client-side alternative
- [Stunnel](../stunnel/README.md) - TLS handling
- [Feederauth](../feederauth/README.md) - Authentication system
- [MLATBridge](../mlatbridge/README.md) - MLAT connection handling
- [Middleware](../middleware/README.md) - Accounting and monitoring
- [Tracker](../tracker/README.md) - Aircraft tracking engine

## References

- TLS SNI: https://en.wikipedia.org/wiki/Server_Name_Indication
- stunnel: https://www.stunnel.org/
- Let's Encrypt: https://letsencrypt.org/
