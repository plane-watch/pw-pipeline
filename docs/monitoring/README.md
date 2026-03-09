# Monitoring

## Overview

The `monitoring` package provides HTTP endpoints for observability: Prometheus metrics scraping, health checks, and optional runtime profiling (pprof). It runs a dedicated HTTP server separate from the main application logic.

## Why Separate Monitoring Server?

### The Pattern

**Main application**: Processes frames, connects to services
**Monitoring server**: Serves metrics/health on separate port

**Example**:
```
Main app: Beast on :30005, SBS1 on :30003
Monitoring: HTTP on :8080
```

### Reasons for Separation

**Security isolation**:
- Metrics/health may contain sensitive info
- Don't expose on public ports
- Can firewall monitoring port separately

**Port binding clarity**:
- One service per port
- Monitoring doesn't conflict with data ports
- Easy to configure load balancers

**Service mesh compatibility**:
- Kubernetes expects `/metrics` on standard port
- Prometheus scrapes known port
- Health checks on dedicated endpoint

**Failure isolation**:
- Main app crashes ≠ monitoring crashes
- Can still scrape metrics during degradation
- Health check independent of data path

## HTTP Server

### Starting the Server

```go
import "plane.watch/lib/monitoring"

func main() {
    app := &cli.App{
        Action: func(c *cli.Context) error {
            monitoring.RunWebServer(c)

            // Your app logic...
            return nil
        },
    }

    monitoring.IncludeMonitoringFlags(app, 8080)
    app.Run(os.Args)
}
```

**Goroutine**: Server runs in background
```go
go func() {
    _ = http.ListenAndServe(fmt.Sprintf(":%d", monitoringPort), mux)
}()
```

**Non-blocking**: App continues immediately

**Error handling**: Currently ignored (TODO comment)
```go
// TODO(MikeNye): Do we want to add error handling around this?
_ = http.ListenAndServe(...)
```

**Risks of ignoring error**:
- Port already in use → Server doesn't start
- Permission denied → Silent failure
- No notification of monitoring failure

**Recommendation**: Log error at minimum
```go
if err := http.ListenAndServe(...); err != nil {
    log.Error().Err(err).Msg("Monitoring server failed")
}
```

<!--
Maintainers: If you add error handling for monitoring server, document here:
- How errors are handled
- Retry logic
- Alerting/notifications
-->

### Configuration Flags

**Monitoring port**:
```go
&cli.IntFlag{
    Name:    "monitoring-port",
    Usage:   "Port to listen on for prometheus app metrics.",
    Value:   defaultPort,  // Passed to IncludeMonitoringFlags()
    EnvVars: []string{"MONITORING_PORT"},
}
```

**Usage**:
```bash
./pipeline --monitoring-port 9090
# or
MONITORING_PORT=9090 ./pipeline
```

**Default port**: Passed by caller
```go
monitoring.IncludeMonitoringFlags(app, 8080)  // Default 8080
```

**Why configurable**:
- Multiple instances on same host (testing)
- Corporate port standards
- Firewall requirements

**pprof enablement**:
```go
&cli.BoolFlag{
    Name:    "enable-net-pprof",
    Usage:   "Enable net pprof profiling at /debug/pprof",
    Value:   false,  // Disabled by default
    EnvVars: []string{"MONITORING_NET_PPROF"},
}
```

**Why disabled by default**:
- Security: pprof exposes internal details
- Performance: Profiling has overhead
- Production: Enable only when needed

## Prometheus Metrics

### Endpoint: `/metrics`

**Handler**:
```go
mux.Handle("/metrics", promhttp.Handler())
```

**What it does**: Exposes all registered Prometheus metrics

**Example response**:
```
# HELP go_goroutines Number of goroutines that currently exist.
# TYPE go_goroutines gauge
go_goroutines 42

# HELP runway_mlat_feeders_connected The total number of mlat feeders connected.
# TYPE runway_mlat_feeders_connected gauge
runway_mlat_feeders_connected 15

# HELP runway_mlat_input_bytes_total The total number of MLAT bytes received from the feeder.
# TYPE runway_mlat_input_bytes_total counter
runway_mlat_input_bytes_total{feeder_id="123",feeder_label="My Receiver"} 1048576
```

**Format**: Prometheus text format

**Automatic metrics**: Go runtime metrics included
- `go_goroutines`: Goroutine count
- `go_memstats_*`: Memory statistics
- `process_*`: CPU, memory, file descriptors

**Application metrics**: Registered elsewhere
```go
var myCounter = prometheus.NewCounter(...)
prometheus.MustRegister(myCounter)
```

**Metrics visible here**: All metrics from all packages

### Scraping Configuration

**Prometheus config**:
```yaml
scrape_configs:
  - job_name: 'pipeline'
    static_configs:
      - targets: ['localhost:8080']
    metrics_path: '/metrics'
    scrape_interval: 15s
```

**Why 15 seconds**:
- Balance between resolution and load
- Prometheus default
- Sufficient for most use cases

**Higher frequency**: 5s for critical services
**Lower frequency**: 60s for low-traffic services

### Common Metrics to Monitor

**From this codebase**:

| Metric | Type | Purpose |
|--------|------|---------|
| `runway_mlat_feeders_connected` | Gauge | Active MLAT connections |
| `runway_mlat_input_bytes_total` | Counter | MLAT data received |
| `runway_mlat_output_bytes_total` | Counter | MLAT data sent |
| `sink_frames_total` | Counter | Frames processed |
| `sink_plane_loc_total` | Counter | Positions published |

**Go runtime metrics**:

| Metric | Purpose |
|--------|---------|
| `go_goroutines` | Detect goroutine leaks |
| `go_memstats_alloc_bytes` | Memory usage |
| `process_cpu_seconds_total` | CPU consumption |

## Health Checks

### Endpoint: `/status`

**Purpose**: Aggregate health of all components

**Response** (healthy):
```http
HTTP/1.1 200 OK
Content-Type: application/json

{"status": "pass"}
```

**Response** (unhealthy):
```http
HTTP/1.1 500 Internal Server Error
Content-Type: application/json

{"status": "fail"}
```

**Used by**:
- Load balancers (upstream health check)
- Kubernetes liveness/readiness probes
- Monitoring systems (Uptime Kuma, etc.)

### Health Check Interface

**Components must implement**:
```go
type HealthCheck interface {
    HealthCheckName() string
    HealthCheck() bool
}
```

**Example implementation**:
```go
type NatsConnection struct {
    conn *nats.Conn
}

func (n *NatsConnection) HealthCheckName() string {
    return "NATS Connection"
}

func (n *NatsConnection) HealthCheck() bool {
    return n.conn != nil && n.conn.IsConnected()
}
```

### Registering Health Checks

**Add during startup**:
```go
natsConn := connectToNats()
monitoring.AddHealthCheck(natsConn)
```

**Remove on shutdown** (optional):
```go
defer monitoring.RemoveHealthCheck(natsConn)
```

**Concurrent safe**: Uses RWMutex
```go
func AddHealthCheck(f HealthCheck) {
    healthChecksLock.Lock()
    defer healthChecksLock.Unlock()
    healthChecks[f.HealthCheckName()] = f
}
```

### Health Check Logic

**Aggregation**:
```go
healthy := len(healthChecks) > 0  // Must have at least one check
for _, check := range healthChecks {
    ok := check.HealthCheck()
    if !ok {
        healthy = false  // Any failure = overall failure
    }
}
```

**AND logic**: All checks must pass

**Why require at least one**:
```go
healthy := len(healthChecks) > 0
```

- Empty health checks = suspicious
- Likely misconfiguration
- Better to fail than false positive

**Alternative approach**: `len(healthChecks) == 0` → healthy
- Useful during startup (services not yet registered)
- Trade-off: Masks configuration errors

### Logging During Health Checks

**Debug logs each check**:
```go
lgr.Debug().
    Str("Name", check.HealthCheckName()).
    Msg("Performing check...")

lgr.Info().
    Str("Name", check.HealthCheckName()).
    Bool("Ok", ok).
    Msg("Performing returned...")
```

**Why log at INFO**: Visibility into which component failed

**Verbosity concern**: Health checks frequent (every few seconds)
- At INFO level, logs can be noisy
- Consider moving to DEBUG after stabilization
- Use structured logging for filtering

<!--
Maintainers: Consider adjusting log levels:
- DEBUG during check execution
- INFO/WARN only on failures
- Reduce log volume in production
-->

## pprof Profiling

### Enabling pprof

**CLI flag**:
```bash
./pipeline --enable-net-pprof
```

**Environment variable**:
```bash
MONITORING_NET_PPROF=true ./pipeline
```

**Why disabled by default**:
1. **Security**: Exposes internal state, heap dumps, goroutine stacks
2. **Performance**: ~5% overhead when actively profiling
3. **Production**: Should be opt-in, not default

### Profile Configuration

**Block profile**:
```go
runtime.SetBlockProfileRate(10_000)  // ~10µs resolution
```

**What it measures**: Time goroutines blocked on synchronization

**10µs resolution**:
- Blocks shorter than 10µs not captured
- Balance: Precision vs overhead
- Too low (1µs): High overhead
- Too high (1ms): Miss short blocks

**Mutex profile**:
```go
runtime.SetMutexProfileFraction(5)  // sample ~1 in 5 contentions
```

**What it measures**: Mutex contention

**1 in 5 sampling**:
- Reduces overhead
- Still catches hot contention points
- 100% sampling = high overhead

### Available Endpoints

**Once enabled** (via `--enable-net-pprof`):

| Endpoint | Purpose |
|----------|---------|
| `/debug/pprof/` | Index of available profiles |
| `/debug/pprof/cmdline` | Command line arguments |
| `/debug/pprof/profile` | CPU profile (30 sec default) |
| `/debug/pprof/symbol` | Symbol lookup |
| `/debug/pprof/trace` | Execution trace |
| `/debug/pprof/heap` | Heap allocations |
| `/debug/pprof/goroutine` | Goroutine stacks |
| `/debug/pprof/block` | Blocking profile |
| `/debug/pprof/mutex` | Mutex contention |

**CPU profile** (30 seconds):
```bash
curl http://localhost:8080/debug/pprof/profile > cpu.pprof
go tool pprof cpu.pprof
```

**Heap profile** (current state):
```bash
curl http://localhost:8080/debug/pprof/heap > heap.pprof
go tool pprof heap.pprof
```

**Goroutine dump**:
```bash
curl http://localhost:8080/debug/pprof/goroutine?debug=1
```

**Trace** (5 seconds):
```bash
curl http://localhost:8080/debug/pprof/trace?seconds=5 > trace.out
go tool trace trace.out
```

### Security Warning

**pprof exposes**:
- All goroutine stacks (may contain secrets in variables)
- Heap contents (may contain API keys, passwords)
- CPU usage patterns
- Command line arguments (may include credentials)

**Production recommendation**:
1. **Firewall**: Block pprof port from public internet
2. **Authentication**: Add auth middleware (not currently implemented)
3. **Time-limited**: Enable only during troubleshooting
4. **Network isolation**: Only accessible from internal network

**Example auth wrapper**:
```go
func pprofAuth(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Check auth token
        if r.Header.Get("X-Auth-Token") != expectedToken {
            http.Error(w, "Unauthorized", http.StatusUnauthorized)
            return
        }
        next.ServeHTTP(w, r)
    })
}

// Wrap pprof handlers
mux.Handle("/debug/pprof/", pprofAuth(http.HandlerFunc(pprof.Index)))
```

<!--
Maintainers: If you add pprof authentication, document here:
- Auth method
- Token/credential management
- Access logging
-->

## Production Patterns

### Kubernetes Integration

**Deployment manifest**:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: pipeline
  annotations:
    prometheus.io/scrape: "true"
    prometheus.io/port: "8080"
    prometheus.io/path: "/metrics"
spec:
  containers:
  - name: pipeline
    image: pipeline:latest
    ports:
    - name: monitoring
      containerPort: 8080
    livenessProbe:
      httpGet:
        path: /status
        port: 8080
      initialDelaySeconds: 30
      periodSeconds: 10
    readinessProbe:
      httpGet:
        path: /status
        port: 8080
      initialDelaySeconds: 5
      periodSeconds: 5
```

**Prometheus autodiscovery**: Annotations tell Prometheus to scrape

**Liveness probe**: Restart pod if unhealthy
**Readiness probe**: Remove from service if unhealthy

**Why different delays**:
- Liveness: 30s (give app time to start)
- Readiness: 5s (detect issues quickly)

### Load Balancer Health Checks

**HAProxy**:
```
backend pipeline_servers
    option httpchk GET /status
    http-check expect status 200
    server pipeline1 10.0.1.10:8080 check
    server pipeline2 10.0.1.11:8080 check
```

**Nginx**:
```nginx
upstream pipeline {
    server 10.0.1.10:8080;
    server 10.0.1.11:8080;

    check interval=3000 rise=2 fall=3 timeout=1000 type=http;
    check_http_send "GET /status HTTP/1.0\r\n\r\n";
    check_http_expect_alive http_2xx;
}
```

### Monitoring Best Practices

**Firewall monitoring port**:
```bash
# Allow only from prometheus server
ufw allow from 10.0.2.0/24 to any port 8080

# Or localhost only (SSH tunnel for access)
ufw allow from 127.0.0.1 to any port 8080
```

**SSH tunnel for metrics**:
```bash
# Access metrics via tunnel
ssh -L 8080:localhost:8080 user@pipeline-server

# Then locally:
curl http://localhost:8080/metrics
```

**Separate network interface**:
```bash
# Listen only on private IP
./pipeline --monitoring-port 8080
# Configure to bind specific interface (code change needed)
```

<!--
Maintainers: Add bind address configuration if implemented
-->

## Common Issues

### Port Already in Use

**Symptom**: Monitoring server doesn't start (silent failure currently)

**Cause**: Another process using port 8080

**Check**:
```bash
netstat -tlnp | grep 8080
# or
lsof -i :8080
```

**Solution**: Use different port
```bash
./pipeline --monitoring-port 9090
```

### Health Check Always Failing

**Symptom**: `/status` returns 500

**Debugging**:
```bash
# Enable debug logging to see which check fails
./pipeline --debug

# Check logs
grep "Performing returned" pipeline.log | grep "Ok=false"
```

**Common causes**:
1. **No health checks registered**: `len(healthChecks) == 0`
2. **Service not started**: Health check registered before service ready
3. **Dependency down**: NATS/ClickHouse unavailable

**Fix**: Ensure services healthy, checks registered after startup

### Metrics Not Appearing in Prometheus

**Symptom**: Prometheus shows no data for pipeline

**Checks**:

1. **Prometheus scraping**:
   ```bash
   # Check Prometheus targets page
   http://prometheus:9090/targets
   # Should show pipeline as UP
   ```

2. **Manual scrape**:
   ```bash
   curl http://pipeline:8080/metrics
   # Should return metrics text
   ```

3. **Firewall**:
   ```bash
   # From Prometheus server
   telnet pipeline 8080
   ```

4. **Scrape config**:
   ```yaml
   # prometheus.yml
   - job_name: 'pipeline'
     static_configs:
       - targets: ['pipeline:8080']  # Correct host:port?
   ```

### pprof Not Working

**Symptom**: 404 on `/debug/pprof`

**Cause**: Not enabled (default)

**Solution**: Enable via flag
```bash
./pipeline --enable-net-pprof
```

**Verify**:
```bash
curl http://localhost:8080/debug/pprof/
# Should return index HTML
```

## Performance Considerations

### Monitoring Overhead

**Metrics endpoint** (`/metrics`):
- ~1-5ms to serialize all metrics
- Depends on metric count (100s = fast, 10000s = slower)
- Prometheus scrapes every 15s (minimal impact)

**Health checks** (`/status`):
- Depends on check implementations
- Fast checks (<1ms each): OK
- Slow checks (>100ms): Problem

**Recommendation**: Health checks should be fast
```go
func (n *NatsConnection) HealthCheck() bool {
    return n.conn.IsConnected()  // Fast: Just check status
    // NOT: n.conn.Request(...)   // Slow: Network call
}
```

**pprof overhead**:
- **Disabled**: Zero overhead
- **Enabled, not profiling**: ~1% (block/mutex sampling)
- **Active CPU profile**: ~5% CPU
- **Active trace**: ~10-20% CPU

### Goroutine for HTTP Server

**Why goroutine**:
```go
go func() {
    _ = http.ListenAndServe(...)
}()
```

- Non-blocking: App starts immediately
- Server runs in background
- No startup delay

**Trade-off**: Error handling harder
- Main app doesn't know if monitoring failed
- Silent failures possible
- Recommend adding error channel

**Improved pattern**:
```go
errCh := make(chan error, 1)
go func() {
    if err := http.ListenAndServe(...); err != nil {
        errCh <- err
    }
}()

// Check for immediate failures
select {
case err := <-errCh:
    log.Fatal().Err(err).Msg("Monitoring server failed to start")
case <-time.After(time.Second):
    // Server started successfully
}
```

<!--
Maintainers: If you improve error handling, document here
-->

## Future Enhancements

<!--
Maintainers: Document enhancements as implemented
-->

### Authentication for pprof

**Current**: No authentication (security risk)

**Proposed**: Token-based auth
```go
func pprofAuth(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        token := r.Header.Get("X-Pprof-Token")
        if token != os.Getenv("PPROF_TOKEN") {
            http.Error(w, "Unauthorized", http.StatusUnauthorized)
            return
        }
        next.ServeHTTP(w, r)
    })
}
```

### Detailed Health Check Response

**Current**: Binary pass/fail

**Proposed**: Per-component status
```json
{
  "status": "fail",
  "checks": {
    "NATS Connection": {"status": "pass"},
    "ClickHouse": {"status": "fail", "error": "connection refused"},
    "Tracker": {"status": "pass"}
  }
}
```

**Why**: Easier troubleshooting (know which component failed)

### Custom Metrics Endpoint

**Current**: All metrics mixed together

**Proposed**: Filtered endpoints
```
/metrics/runtime    - Go runtime metrics only
/metrics/app        - Application metrics only
/metrics/business   - Business metrics only
```

**Why**: Reduce scrape size, separate concerns

## File Guide

| File | Purpose |
|------|---------|
| `monitoring.go` | HTTP server, health checks, pprof setup |

## See Also

- [Logging](../logging/README.md) - Structured logging (separate from metrics)
- [Prometheus client library](https://github.com/prometheus/client_golang)
- [pprof documentation](https://pkg.go.dev/net/http/pprof)

## References

- Prometheus exposition formats: https://prometheus.io/docs/instrumenting/exposition_formats/
- Health check RFC: https://inadarei.github.io/rfc-healthcheck/
- Go pprof usage: https://go.dev/blog/pprof
- Runtime profiling: https://pkg.go.dev/runtime/pprof
