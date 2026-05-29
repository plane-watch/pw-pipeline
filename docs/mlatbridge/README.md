# MLAT Bridge

## Overview

The `mlatbridge` package provides an authenticated TLS proxy that bridges feeder MLAT (Multilateration) connections to regional MLAT servers. It acts as a secure gateway, validating feeders and routing them to geographically-appropriate MLAT processing servers.

## What is MLAT?

**Multilateration**: Calculating aircraft position using time-difference-of-arrival (TDOA) from multiple receivers

**How it works**:
```
Aircraft transmits Mode S frame
  ↓
Receiver 1 sees it at time T1 (timestamp from GPS)
Receiver 2 sees it at time T2
Receiver 3 sees it at time T3
  ↓
MLAT server calculates position from (T1, T2, T3) + receiver positions
```

**Requirements**:
- 3+ receivers see the same frame
- Accurate timestamps (GPS-synchronized)
- Known receiver positions
- Beast format (includes timestamps)

**Result**: Positions for aircraft not transmitting ADS-B

## Why a Bridge?

### The Problem

**Direct connections to MLAT server**:
```
Feeder 1 → MLAT Server (exposed to internet)
Feeder 2 → MLAT Server
Feeder 3 → MLAT Server
```

**Issues**:
- MLAT server exposed to attacks
- No authentication (anyone can connect)
- Can't scale MLAT servers regionally
- Hard to monitor/meter feeder traffic

### The Solution

**Bridge architecture**:
```
Feeder 1 → [TLS] → Bridge → MLAT Server (private network)
Feeder 2 → [TLS] → Bridge → MLAT Server
Feeder 3 → [TLS] → Bridge → MLAT Server
```

**Benefits**:
- ✓ Single authenticated entry point
- ✓ TLS encryption
- ✓ MLAT servers on private network
- ✓ Regional routing (low latency)
- ✓ Per-feeder metrics
- ✓ Centralized access control

## Regional Muxes

### Mux Assignment

**Mux table**:
```go
var muxes = map[string]string{
    "mux-#act":  "mux-act:12346",    // Australian Capital Territory
    "mux-#nsw":  "mux-nsw:12346",    // New South Wales
    "mux-#nt":   "mux-nt:12346",     // Northern Territory
    "mux-#qld":  "mux-qld:12346",    // Queensland
    "mux-#sa":   "mux-sa:12346",     // South Australia
    "mux-#tas":  "mux-tas:12346",    // Tasmania
    "mux-#vic":  "mux-vic:12346",    // Victoria
    "mux-#wa":   "mux-wa:12346",     // Western Australia
    "mux-#nz":   "mux-nz:12346",     // New Zealand
    "mux-#eu":   "mux-eu:12346",     // Europe
    "mux-#us":   "mux-us:12346",     // United States
    "mux-#asia": "mux-asia:12346",   // Asia
}
```

**Port 12346**: Standard MLAT server port (mlat-client convention)

**Why regions matter**:
- MLAT requires low latency (timestamps are microseconds)
- Cross-continent latency degrades accuracy
- Regional clustering improves position quality

**Mux assignment source**: Feederauth cache
```go
feeder, err := mb.feeders.Get(apiKey)
mlatHost := muxes[feeder.Mux]  // e.g., "mux-nsw:12346"
```

**Feeder mux set by**: ATC backend during feeder registration
- Based on feeder's geographic location
- Can be updated dynamically
- Bridge re-validates every 5 seconds

### Mux Hostnames

**Not IP addresses**: Use hostnames for flexibility
```
mux-nsw:12346  (not 10.0.1.5:12346)
```

**Why**:
- DNS can change IPs without code deploy
- Load balancers can distribute connections
- Easy to test (override DNS locally)

**Resolution**: Bridge resolves on dial
```go
mlatConn, err := net.Dial("tcp", mlatHost)  // Resolves "mux-nsw:12346"
```

**Private network**: Mux hosts not publicly routable
- Only accessible from bridge servers
- Security: MLAT servers not exposed
- Scalability: Can run multiple bridge instances

## Connection Flow

### 1. Feeder Connects

```
Feeder → TLS handshake → Bridge
```

**Protocol**: TLS 1.2+ on configured port (e.g., 30105)

**Certificate validation**: Bridge presents cert, feeder verifies
- Prevents MITM attacks
- Feeder trusts bridge's certificate authority

### 2. Authentication

```go
func (mb *MLATBridge) authenticator(apiKey string) (bool, error) {
    return mb.feeders.Authenticate(apiKey, feederauth.MLAT)
}
```

**API key extracted from**: TLS handshake (client certificate or SNI)

**Authentication checks**:
1. API key valid?
2. Feeder authorized for MLAT?
3. Not rate-limited?

**Handled by**: Feederauth package

**Authentication failures**: Connection rejected immediately
- No connection to MLAT server established
- Prevents unauthorized MLAT traffic

### 3. MLAT Server Connection

```go
mlatHost, ok := muxes[feeder.Mux]  // Lookup mux
mlatConn, err := net.Dial("tcp", mlatHost)  // Connect
```

**Dial timeout**: Default Go timeout (~30s)

**Connection failures**:
- Mux unknown: "could not find mux"
- Server unreachable: "could not connect to mlat server"
- Network issue: TCP dial error

**Error handling**: Close feeder connection, return error

### 4. Bidirectional Bridge

```
Feeder ←---data---→ Bridge ←---data---→ MLAT Server
```

**Two goroutines**:
```go
eg.Go(func() error {
    return mb.simplexBridge(ctx, cancel, feederConn, mlatConn, prometheusMLATBytesRx)
})
eg.Go(func() error {
    return mb.simplexBridge(ctx, cancel, mlatConn, feederConn, prometheusMLATBytesTx)
})
```

**Simplex bridge**: One-way data flow
- First goroutine: Feeder → MLAT (RX)
- Second goroutine: MLAT → Feeder (TX)

**Why two goroutines**: Full-duplex, simultaneous bidirectional traffic

**Buffer size: 64 KiB**:
```go
buf := make([]byte, 64*1024)
```

**Why 64 KiB**:
- Matches typical TCP window size
- Good balance: memory vs throughput
- MLAT traffic is low-bandwidth (~10-100 KB/sec)

### 5. Continuous Authentication Check

```go
eg.Go(func() error {
    timing.RunOnTickerWithContext(connCtx, mb.log, time.Second*5, func() error {
        if !mb.feeders.IsValid(apiKey) {
            connCtxCancel()  // Poison pill
        }
        return nil
    })
    return nil
})
```

**Every 5 seconds**: Check if feeder still valid
- Feeder deauthorized → close connection
- Feeder rate-limited → close connection
- Feeder deleted → close connection

**Why 5 seconds**: Fast enough to remove bad actors, not too chatty

**Poison pill pattern**: Cancel context → all goroutines exit

## Simplex Bridge Implementation

### Read-Write Loop

```go
for {
    select {
    case <-ctx.Done():
        return ctx.Err()  // Graceful shutdown
    default:
    }

    n, err = from.Read(buf)
    // ...handle read...

    m, err = to.Write(buf[:n])
    // ...handle write...
}
```

**Flow**:
1. Check context (exit if cancelled)
2. Read up to 64 KiB from source
3. Write all bytes to destination
4. Repeat

### Deadline Handling

**Short deadlines** (1 second):
```go
err = from.SetReadDeadline(time.Now().Add(1 * time.Second))
err = to.SetWriteDeadline(time.Now().Add(1 * time.Second))
```

**Why 1 second**:
- Poll context regularly (graceful shutdown)
- Don't block forever on idle connections
- MLAT traffic is frequent (position updates)

**Deadline exceeded handling**:
```go
if err != nil && !errors.Is(err, os.ErrDeadlineExceeded) {
    cancel()
    return fmt.Errorf("read error: %w", err)
}
```

**Ignore `ErrDeadlineExceeded`**: Normal, just retry

**Other errors**: Cancel context, close connections

**Why this pattern**: Balance responsiveness with efficiency
- Without deadlines: Can't check context (blocked in Read)
- Very short deadlines (<100ms): Excessive syscalls
- Long deadlines (>5s): Slow graceful shutdown

### Short Write Handling

**Write loop**:
```go
written := 0
for written < n {
    m, err = to.Write(buf[written:n])
    if m > 0 {
        written += m
        counter.Add(float64(m))
    }
    // ...handle error...
}
```

**Why necessary**: `Write()` can return partial writes
- Network buffer full
- Flow control
- Write deadline exceeded

**Guarantees**: All `n` bytes written before returning

**Metric accounting**: Count only actually-written bytes

## Prometheus Metrics

### Global Metric: Connected Feeders

```go
prometheusConnectedMLATFeeders = promauto.NewGauge(prometheus.GaugeOpts{
    Namespace: "runway",
    Subsystem: "mlat",
    Name:      "feeders_connected",
    Help:      "The total number of mlat feeders connected.",
})
```

**Type**: Gauge (can go up or down)

**Value**: Current count of active MLAT connections

**Lifecycle**:
```go
prometheusConnectedMLATFeeders.Inc()   // On connect
defer prometheusConnectedMLATFeeders.Dec()  // On disconnect
```

**Use for**:
- Monitoring feeder count
- Alerting on anomalies (sudden drop)
- Capacity planning

### Per-Feeder Metrics: Bytes Transferred

**Dynamically registered**:
```go
prometheusMLATBytesRx := prometheus.NewCounter(prometheus.CounterOpts{
    Namespace: "runway",
    Subsystem: "mlat",
    Name:      "input-bytes-total",
    Help:      "The total number of MLAT bytes received from the feeder.",
    ConstLabels: map[string]string{
        "feeder_id":    strconv.FormatInt(int64(feeder.Id), 10),
        "feeder_label": feeder.Label,
        "feeder_user":  feeder.User,
        "feeder_mux":   feeder.Mux,
    },
})
err = prometheus.Register(prometheusMLATBytesRx)
defer prometheus.Unregister(prometheusMLATBytesRx)
```

**Why register/unregister per connection**:
- Track individual feeder bandwidth
- Labels identify specific feeder
- Unregister on disconnect (prevent metric leak)

**Metrics**:
- `runway_mlat_input_bytes_total{feeder_id="123",feeder_label="My Receiver",...}`
- `runway_mlat_output_bytes_total{feeder_id="123",feeder_label="My Receiver",...}`

**Use for**:
- Per-feeder bandwidth monitoring
- Detect unusually high/low traffic
- Billing (if metered by bandwidth)
- Troubleshooting (zero bytes = connection issue)

**Typical values**:
- Input (feeder → MLAT): 10-50 KB/sec (Beast frames)
- Output (MLAT → feeder): 5-20 KB/sec (MLAT results)

**Asymmetry**: Input usually > output
- Feeder sends all frames (2-5 Hz per aircraft)
- MLAT sends back only calculated positions (subset)

## Error Handling

### Connection Errors

**Feeder connection fails**:
```go
defer func() {
    _ = feederConn.Close()
}()
```

**Always closed**: Failsafe ensures cleanup

**MLAT connection fails**:
```go
mlatConn, err := net.Dial("tcp", mlatHost)
if err != nil {
    return fmt.Errorf("could not connect to mlat server: %w", err)
}
defer func() {
    _ = mlatConn.Close()
}()
```

**Failure modes**:
- DNS resolution failed (mux host unknown)
- Connection refused (MLAT server down)
- Network unreachable (routing issue)
- Timeout (slow network)

**Impact**: Feeder connection rejected, no bridge established

### Authentication Errors

**Unknown feeder**:
```go
feeder, err := mb.feeders.Get(apiKey)
if err != nil {
    return fmt.Errorf("failed to get feeder for %s: %w", apiKey, err)
}
```

**Causes**:
- Invalid API key
- Feeder not in cache
- Feederauth cache issue

**Unknown mux**:
```go
mlatHost, ok := muxes[feeder.Mux]
if !ok {
    return fmt.Errorf("could not find mux %q", feeder.Mux)
}
```

**Causes**:
- Feeder assigned invalid mux (data issue)
- Mux not yet added to muxes table

**Fix**: Update muxes table, restart bridge

<!--
Maintainers: If you add new muxes, update muxes.go and document here:
- Mux name:
- Region covered:
- Server hostname:
-->

### Bridge Errors

**Read/write errors**: Cancel context, exit all goroutines

**Error group**:
```go
eg := errgroup.Group{}
err = eg.Wait()  // First error cancels all
```

**Why errgroup**: Coordinate goroutines, propagate first error

**Common errors**:
- `context canceled`: Graceful shutdown or auth revoked
- `read error: EOF`: Remote side closed connection
- `write error: broken pipe`: Connection dropped
- `read error: timeout`: Deadline exceeded (likely dead connection)

## Configuration

### Required Options

**Listen address**:
```go
WithListenHostPort("0.0.0.0:30105")
```

**Format**: `host:port` compatible with `net.Listen`

**Examples**:
- `0.0.0.0:30105` - Listen all interfaces
- `192.168.1.10:30105` - Specific interface
- `:30105` - All interfaces, short form

**Port choice**: Not standard, choose unused port
- 30105 commonly used for MLAT bridge
- Avoid 12346 (MLAT server port, confusing)

**TLS certificate**:
```go
WithTLSCertificate("/path/to/cert.pem", "/path/to/key.pem")
```

**Certificate requirements**:
- Valid X.509 certificate
- Matches hostname feeders connect to
- Not expired
- Private key accessible

**Self-signed OK**: If feeders trust CA

**Let's Encrypt recommended**: Free, auto-renewal

**NATS URL**:
```go
WithNatsURL("nats://localhost:4222")
```

**Why NATS**: Feeder connected/disconnected events (future use)

**Currently**: Connection established but not heavily used

<!--
Maintainers: Document NATS usage if expanded beyond connection setup
-->

**Feeder authenticator**:
```go
WithFeederAuthenticator(feederCache)
```

**Type**: `*feederauth.FeederCache`

**Must be shared**: Same instance used by other services
- Consistent feeder state
- Single source of truth
- Shared NATS subscriptions

### Validation

**Missing options panic startup**:
```go
if mb.natsURL == "" {
    return nil, fmt.Errorf("%w: Please specify the Nats URL (sink)", MissingOption)
}
if mb.feeders == nil {
    return nil, fmt.Errorf("%w: You need to configure the *feederauth.FeederCache", MissingOption)
}
```

**Fail fast**: Better than runtime errors later

## Production Patterns

### Deployment Architecture

**Typical setup**:
```
                           ┌─── mux-nsw:12346
                           │
Internet → Load Balancer → Bridge Instance 1 ─┼─── mux-qld:12346
            (TLS)          │                   │
                           Bridge Instance 2 ──┼─── mux-vic:12346
                           │                   │
                           Bridge Instance 3 ──┴─── ...
```

**Load balancer**: Distribute feeder connections
- Round-robin or least-connections
- Health check bridge instances
- SSL termination or passthrough

**Multiple bridges**: HA and scale
- Stateless (no shared state between bridges)
- Feeder can connect to any bridge
- Mux servers handle load from all bridges

**Private network**: Mux servers not internet-facing
- Only bridges can reach them
- Reduces attack surface
- Simplifies firewall rules

### Health Checks

**Not currently implemented**:
```go
// TODO: Implement health check
func (mb *MLATBridge) HealthCheck() bool {
    return true  // Always healthy
}
```

**Recommended health check**:
```go
func (mb *MLATBridge) HealthCheck() bool {
    // 1. Listener accepting connections?
    if mb.listener == nil {
        return false
    }

    // 2. Can reach at least one mux?
    for _, mux := range muxes {
        conn, err := net.DialTimeout("tcp", mux, time.Second)
        if err == nil {
            conn.Close()
            return true  // At least one mux reachable
        }
    }
    return false  // No muxes reachable
}
```

<!--
Maintainers: If you implement health check, document implementation here
-->

### Monitoring Dashboards

**Key metrics to graph**:

1. **Connected feeders** (`runway_mlat_feeders_connected`):
   - Expect: Relatively stable
   - Alert: Sudden drop (mux failure?)

2. **Bytes RX per feeder** (`runway_mlat_input_bytes_total`):
   - Expect: 10-50 KB/sec per feeder
   - Alert: Zero (feeder offline or not sending)

3. **Bytes TX per feeder** (`runway_mlat_output_bytes_total`):
   - Expect: 5-20 KB/sec per feeder
   - Alert: Very low (MLAT server issue?)

4. **Total bandwidth**:
   - Sum of all feeders
   - Capacity planning

**Example Prometheus queries**:
```promql
# Current feeder count
runway_mlat_feeders_connected

# Input rate (bytes/sec) per feeder
rate(runway_mlat_input_bytes_total[1m])

# Total input bandwidth
sum(rate(runway_mlat_input_bytes_total[1m]))

# Feeders with zero input (dead connections)
runway_mlat_input_bytes_total{} == 0
```

### Certificate Renewal

**Let's Encrypt**: 90-day expiry

**Auto-renewal**: Use certbot or acme.sh

**Reload without downtime**:
1. Renew certificate files
2. SIGHUP bridge process (if implemented)
3. Or: Rolling restart (with load balancer)

**Testing renewal**:
```bash
# Renew cert
certbot renew

# Restart bridge gracefully
# (implement graceful shutdown/reload)
```

<!--
Maintainers: Document graceful reload implementation if added
-->

## Security Considerations

### TLS Best Practices

**Minimum TLS 1.2**: Disable older versions
```go
// In stunnel.Listener configuration
MinVersion: tls.VersionTLS12,
```

**Strong cipher suites**: Disable weak ciphers

**Certificate validation**: Feeders should validate bridge cert
- Prevent MITM
- Use proper CA (not self-signed in production)

### API Key Security

**Never log API keys**:
```go
mb.log.Info().
    Str("feeder_id", strconv.Itoa(feeder.Id)).  // OK
    // Str("api_key", apiKey).  // NEVER DO THIS
    Msg("Feeder connected")
```

**Transmission**: API key in TLS handshake
- Encrypted in transit
- Not visible to network observers

**Storage**: Feeder cache handles storage
- See feederauth documentation

### Rate Limiting

**Handled by feederauth**: Connection rate limits

**Bridge respects auth**: Re-validates every 5 seconds

**Bandwidth limits**: Not currently enforced
- Could add per-feeder bandwidth cap
- Disconnect if exceeded

<!--
Maintainers: If you add bandwidth limiting, document here
-->

## Troubleshooting

### Feeder Can't Connect

**Symptoms**: Connection refused or timeout

**Checks**:

1. **Bridge listening**:
   ```bash
   netstat -tlnp | grep 30105
   ```

2. **Firewall**:
   ```bash
   # Allow port 30105
   ufw allow 30105/tcp
   ```

3. **Certificate valid**:
   ```bash
   openssl s_client -connect bridge.example.com:30105
   # Check certificate not expired
   ```

4. **DNS resolves**:
   ```bash
   dig bridge.example.com
   ```

5. **Feeder authenticated**:
   ```bash
   # Check feederauth logs
   grep "feeder-api-key" bridge.log
   ```

### Connection Drops Immediately

**Symptom**: Connects then disconnects within seconds

**Causes**:

1. **Authentication failure**:
   - Check feederauth cache
   - API key valid?
   - MLAT permission granted?

2. **Unknown mux**:
   ```
   grep "could not find mux" bridge.log
   ```

3. **MLAT server unreachable**:
   ```
   grep "could not connect to mlat server" bridge.log
   ```

### Low MLAT Output

**Symptom**: Input bytes high, output bytes very low

**Possible causes**:

1. **MLAT server issue**:
   - Server not calculating positions
   - Check MLAT server logs/health

2. **Insufficient coverage**:
   - Need 3+ receivers for MLAT
   - Receivers too far apart
   - Poor geometry

3. **Timestamp quality**:
   - GPS synchronization lost
   - Non-GPS receivers (CPU timestamps, inaccurate)

4. **Frame quality**:
   - Low SNR frames (unreliable timestamps)
   - Corrupted Beast frames

**Not bridge issue**: Bridge just proxies, doesn't process

### High Memory Usage

**Symptom**: Bridge memory grows over time

**Possible causes**:

1. **Prometheus metric leak**:
   - Per-feeder metrics not unregistered
   - Check: Metrics count keeps growing

2. **Goroutine leak**:
   ```bash
   # Check goroutine count
   curl localhost:8080/debug/pprof/goroutine?debug=1
   ```

3. **Connection leak**:
   ```bash
   # Check connection count
   netstat -an | grep 30105 | wc -l
   ```

**Monitoring**:
```promql
# Memory usage
process_resident_memory_bytes{job="mlatbridge"}

# Goroutine count
go_goroutines{job="mlatbridge"}
```

## Performance Characteristics

### Latency

**Bridge overhead**: ~1-5 ms
- TLS decryption
- Two memory copies (feeder→bridge, bridge→mux)
- Kernel TCP stack

**Total latency** (feeder → mux): ~10-50 ms
- Depends on network path
- Bridge adds minimal overhead

**Why low latency matters**: MLAT accuracy
- Timestamp precision is microseconds
- Extra milliseconds are acceptable
- But minimize where possible

### Throughput

**Per connection**: ~100 KB/sec typical
- MLAT traffic is low-bandwidth
- Well below network capacity

**Total throughput**: 100 feeders × 100 KB/sec = 10 MB/sec
- Easily handled by gigabit network
- Bridge is not bottleneck

**CPU usage**: Low (~1-5% per 100 connections)
- Mostly I/O wait
- TLS overhead modest

**Scaling**: Can handle 1000+ connections per bridge instance
- Limited by file descriptors, not CPU
- Increase ulimit if needed

## Future Enhancements

<!--
Maintainers: Document enhancements as implemented
-->

### Dynamic Mux Discovery

**Current**: Hardcoded mux table

**Proposed**: Fetch from NATS or config service
- Add/remove muxes without code deploy
- Automatic failover if mux down
- Geographic load balancing

### Connection Pooling to Mux

**Current**: One bridge connection per feeder connection

**Proposed**: Pool/multiplex connections
- Reduce connection count to mux servers
- Add protocol framing (identify feeder)
- More complex but scales better

### Compression

**Current**: Raw TCP proxy

**Proposed**: Compress feeder↔bridge traffic
- Reduce bandwidth costs
- Trade CPU for bandwidth
- MLAT data compresses well (repetitive)

## File Guide

| File | Purpose |
|------|---------|
| `mlatbridge.go` | Main bridge logic, connection handling, simplex bridging |
| `muxes.go` | Regional mux server mapping |

## See Also

- [Feederauth](../feederauth/README.md) - Authentication and authorization
- [Stunnel](../stunnel/README.md) - TLS listener wrapper
- [NATS](../nats_io/README.md) - Message bus integration

## References

- MLAT overview: https://mode-s.org/decode/content/ads-b/4-mlat.html
- mlat-client: https://github.com/mutability/mlat-client
- Plane.watch MLAT: https://plane.watch/about/mlat
