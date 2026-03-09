# Setup Utilities

## Overview

The `setup` package provides CLI integration helpers for configuring data sources and sinks. It parses URL-style configuration strings and creates properly configured producer and sink instances with minimal boilerplate.

## Why This Package?

### The Configuration Problem

**Complex producer setup**:
```go
// Manual configuration is verbose
producer := producer.New(
    producer.WithType(producer.Beast),
    producer.WithFetcher("receiver.local", "30005"),
    producer.WithSourceTag("north-antenna"),
    producer.WithReferenceLatLon(&lat, &lon),
    producer.WithPrometheusCounters(avrCounter, beastCounter, sbs1Counter),
)
```

**Repeated for each source**: Error-prone, inconsistent

### The Solution

**URL-style configuration**:
```bash
./pipeline --fetch beast://receiver.local:30005?tag=north-antenna&refLat=47.6&refLon=-122.3
```

**Or environment variables**:
```bash
SOURCE="beast://receiver.local:30005?tag=north" ./pipeline
```

**Benefits**:
- ✓ Single string configuration
- ✓ URL familiarity (scheme://host:port?params)
- ✓ CLI or env var support
- ✓ Multiple sources easily specified
- ✓ Consistent parsing

## Source Configuration

### URL Format

**Scheme**: `[avr|beast|sbs1]://host:port?parameters`

**Components**:
- **Scheme**: Frame format (avr, beast, sbs1)
- **Host**: Hostname or IP address
- **Port**: TCP port number
- **Parameters** (query string):
  - `tag`: Source identifier
  - `refLat`: Reference latitude
  - `refLon`: Reference longitude

### Fetch Mode (Client)

**Connect to remote server**:
```bash
--fetch beast://receiver:30005?tag=my-receiver
```

**Multiple sources**:
```bash
--fetch beast://receiver1:30005?tag=rx1 \
--fetch beast://receiver2:30005?tag=rx2 \
--fetch avr://receiver3:30002?tag=rx3
```

**Environment variable**:
```bash
SOURCE="beast://receiver:30005?tag=rx1,beast://receiver2:30005?tag=rx2" ./pipeline
```

**What it creates**:
```go
producer.New(
    producer.WithType(producer.Beast),
    producer.WithFetcher("receiver", "30005"),
    producer.WithSourceTag("my-receiver"),
    producer.WithPrometheusCounters(...),
)
```

### Listen Mode (Server)

**Accept incoming connections**:
```bash
--listen beast://0.0.0.0:30005?tag=feeder-input
```

**Difference from fetch**: Uses `WithListener` instead of `WithFetcher`

**Use case**: Receive from multiple feeders

### File Mode

**Read from files**:
```bash
--file beast:///path/to/capture.beast?tag=replay&delay=yes
```

**Additional parameters**:
- `delay`: Replay with timing (`yes`/`no`)

**Compression auto-detected**: .gz and .bz2 handled automatically

**Example with delay**:
```bash
--file beast:///captures/flight.beast.gz?delay=yes&refLat=47.6&refLon=-122.3
```

**What it creates**:
```go
producer.New(
    producer.WithType(producer.Beast),
    producer.WithFiles([]string{"/path/to/capture.beast"}),
    producer.WithBeastDelay(true),
    producer.WithSourceTag("replay"),
)
```

## Reference Position

### Purpose

**Required for surface position decoding**: CPR surface positions need reference point

**Within 45nm**: Reference must be near actual position
- Too far: Decoding fails
- Typical: Receiver location

### Configuration Methods

**URL parameter** (per-source):
```bash
--fetch beast://receiver:30005?refLat=47.6062&refLon=-122.3321
```

**Global flag** (all sources):
```bash
--ref-lat 47.6062 --ref-lon -122.3321 \
--fetch beast://receiver1:30005 \
--fetch beast://receiver2:30005
```

**Priority**: URL parameter > global flag

**Missing reference**:
```
ERROR Do not have a reference lat/lon - will not decode surface position frames
```

**Impact**: Airborne positions work, surface positions fail

## Tag Assignment

### Purpose

**Identify data source**: Track which receiver/file sent frame

**Used for**:
- Multi-source attribution
- Accounting/metrics
- Debugging (trace issues to specific source)
- MLAT (requires source identification)

### Configuration

**URL parameter**:
```bash
--fetch beast://receiver:30005?tag=north-site
```

**Global default**:
```bash
--tag default-receiver \
--fetch beast://receiver1:30005 \
--fetch beast://receiver2:30005
# Both use "default-receiver"
```

**Override global**:
```bash
--tag default \
--fetch beast://rx1:30005?tag=north \
--fetch beast://rx2:30005
# rx1 uses "north", rx2 uses "default"
```

**Priority**: URL tag > global tag > empty

## ADS-C Support

### What is ADS-C?

**Automatic Dependent Surveillance - Contract**: Satellite-based aircraft tracking
- Updates every 15-30 minutes (infrequent)
- Used over oceans (no radar coverage)
- Often delivered in SBS1 format

### Configuration

```bash
--ads-c --fetch sbs1://adsc-feed:30003?tag=satellite
```

**What it does**: Enables keep-alive repeater
```go
producer.WithKeepAliveRepeater()
```

**Why**: Infrequent updates cause connection timeouts
- Repeater republishes last position every 30s
- Keeps connection alive
- Downstream sees activity

## Prometheus Metrics

### Automatic Counters

**Pre-configured metrics**:
```go
prometheusInputBeastFrames  // pw_ingest_input_beast_total
prometheusInputAvrFrames    // pw_ingest_input_avr_total
prometheusInputSbs1Frames   // pw_ingest_input_sbs1_total
```

**Automatically added**: All sources get metrics

**Queries**:
```promql
# Frames per second by format
rate(pw_ingest_input_beast_total[1m])

# Total frame rate
sum(rate(pw_ingest_input_*_total[1m]))
```

## CLI Integration

### Add Flags to Application

```go
import "plane.watch/lib/setup"

func main() {
    app := &cli.App{
        Name: "pipeline",
        Action: func(c *cli.Context) error {
            producers, err := setup.HandleSourceFlags(c)
            if err != nil {
                return err
            }

            // Use producers
            for _, p := range producers {
                tracker.AddProducer(p)
            }

            return nil
        },
    }

    setup.IncludeSourceFlags(app)
    app.Run(os.Args)
}
```

### Available Flags

**Source flags**:
```
--fetch    Connect to remote source
--listen   Accept incoming connections
--file     Read from file
```

**Reference flags**:
```
--ref-lat  Reference latitude (global)
--ref-lon  Reference longitude (global)
```

**Tagging**:
```
--tag      Default source tag
```

**Special modes**:
```
--ads-c    Enable ADS-C mode (keep-alive)
```

### Environment Variables

**All flags have env var equivalents**:
```bash
SOURCE="beast://rx:30005"        # --fetch
LISTEN="beast://:30005"           # --listen
FILE="beast:///capture.beast"    # --file
REF_LAT=47.6                     # --ref-lat
REF_LON=-122.3                   # --ref-lon
TAG=my-receiver                  # --tag
ADS_C=true                       # --ads-c
```

**Use case**: Docker/Kubernetes configuration

## Configuration Examples

### Single Receiver

```bash
./pipeline \
  --fetch beast://receiver.local:30005 \
  --ref-lat 47.6062 \
  --ref-lon -122.3321 \
  --tag north-antenna
```

### Multiple Receivers

```bash
./pipeline \
  --fetch beast://rx-north:30005?tag=north&refLat=47.7&refLon=-122.4 \
  --fetch beast://rx-south:30005?tag=south&refLat=47.5&refLon=-122.2 \
  --fetch avr://rx-east:30002?tag=east&refLat=47.6&refLon=-122.1
```

**Each has own position**: Important for MLAT

### Mixed Sources

```bash
./pipeline \
  --listen beast://0.0.0.0:30005?tag=feeders \
  --fetch beast://backup:30005?tag=backup \
  --file beast:///test/capture.beast.gz?tag=replay&delay=no
```

**Three producers**: Listener + fetcher + file

### ADS-C Feed

```bash
./pipeline \
  --ads-c \
  --fetch sbs1://satellite-feed:30003?tag=ocean-coverage \
  --ref-lat 0 \
  --ref-lon 0
```

**Keep-alive enabled**: Handles infrequent updates

### Environment Variable Configuration

```bash
export SOURCE="beast://receiver:30005?tag=rx1"
export REF_LAT=47.6062
export REF_LON=-122.3321

./pipeline
```

**Docker compose**:
```yaml
services:
  pipeline:
    environment:
      - SOURCE=beast://receiver:30005?tag=docker-rx
      - REF_LAT=47.6
      - REF_LON=-122.3
```

## Implementation Details

### URL Parsing

**Parse scheme, host, port**:
```go
parsedUrl, err := url.Parse("beast://receiver:30005?tag=rx1")

scheme := parsedUrl.Scheme      // "beast"
host := parsedUrl.Hostname()    // "receiver"
port := parsedUrl.Port()        // "30005"
tag := parsedUrl.Query().Get("tag")  // "rx1"
```

**Type determination**:
```go
switch strings.ToLower(parsedUrl.Scheme) {
case "avr":
    producer.WithType(producer.Avr)
case "beast":
    producer.WithType(producer.Beast)
case "sbs1":
    producer.WithType(producer.Sbs1)
default:
    return error("unknown scheme")
}
```

### Parameter Extraction

**Reference position**:
```go
refLat := getRef(parsedUrl, "refLat", defaultRefLat)
refLon := getRef(parsedUrl, "refLon", defaultRefLon)

if refLat != 0 && refLon != 0 {
    producer.WithReferenceLatLon(&refLat, &refLon)
}
```

**Tag**:
```go
tag := getTag(parsedUrl, defaultTag)
producer.WithSourceTag(tag)
```

**Beast delay** (file only):
```go
delay := false
if parsedUrl.Query().Has("delay") {
    switch strings.ToLower(parsedUrl.Query().Get("delay")) {
    case "", "no", "false", "0":
        delay = false
    default:
        delay = true
    }
}
```

### Mode Selection

**Fetch vs Listen**:
```go
if listen {
    producer.WithListener(host, port)
} else {
    producer.WithFetcher(host, port)
}
```

**Determined by**: Flag used (`--fetch` vs `--listen`)

## Common Issues

### Unknown Scheme Error

**Symptom**: "unknown scheme: bst"

**Cause**: Typo in scheme name
```bash
--fetch bst://receiver:30005  # Wrong
--fetch beast://receiver:30005  # Correct
```

**Valid schemes**: `avr`, `beast`, `sbs1` (case-insensitive)

### Missing Reference Position

**Symptom**: Surface positions not decoded

**Log**:
```
ERROR Do not have a reference lat/lon - will not decode surface position frames
```

**Cause**: No refLat/refLon specified

**Solution**: Add reference position
```bash
--ref-lat 47.6 --ref-lon -122.3
```

### Invalid URL Format

**Symptom**: "Failed to understand URL"

**Common mistakes**:
```bash
# Missing ://
--fetch beast:receiver:30005  ❌

# Correct
--fetch beast://receiver:30005  ✓

# Missing port
--fetch beast://receiver  ❌

# Correct (port required)
--fetch beast://receiver:30005  ✓
```

### Tag Not Applied

**Symptom**: All sources have same tag

**Cause**: Only global tag specified, no per-source tags

**Solution**: Add tag to URL
```bash
--fetch beast://rx1:30005?tag=north
--fetch beast://rx2:30005?tag=south
```

## Best Practices

### Use Descriptive Tags

**Good**:
```bash
--fetch beast://rx:30005?tag=north-antenna-1090mhz
```

**Bad**:
```bash
--fetch beast://rx:30005?tag=rx1
```

**Why**: Debugging and metrics are clearer

### Always Specify Reference

**Even if not using surface positions**:
```bash
--ref-lat 47.6 --ref-lon -122.3
```

**Why**: Future-proof, enables all features

### Environment Variables for Production

**Configuration file**:
```bash
# /etc/pipeline/sources.env
SOURCE_1="beast://rx-north:30005?tag=north&refLat=48.0&refLon=-122.5"
SOURCE_2="beast://rx-south:30005?tag=south&refLat=47.2&refLon=-122.1"
```

**Load in systemd**:
```ini
[Service]
EnvironmentFile=/etc/pipeline/sources.env
ExecStart=/usr/bin/pipeline
```

**Why**: Centralized config, easy updates

## File Guide

| File | Purpose |
|------|---------|
| `common.go` | URL parameter extraction helpers |
| `source.go` | Source flag handling, producer creation |
| `sink.go` | Sink configuration (not detailed here) |

## See Also

- [Producer](../producer/README.md) - Created by setup package
- [Logging](../logging/README.md) - CLI flag integration pattern

## References

- Go URL package: https://pkg.go.dev/net/url
- urfave/cli: https://cli.urfave.org/
