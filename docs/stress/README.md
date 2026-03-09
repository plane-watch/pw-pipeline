# stress - Load Testing Tool

## Overview

`stress` is a load testing tool that simulates multiple feeders connecting to runway, enabling performance testing and capacity planning.

## Purpose

**Load testing**: Simulate hundreds of concurrent feeders

**Use cases**:
- Performance testing runway
- Capacity planning (how many feeders per instance?)
- Finding bottlenecks
- Regression testing

## Usage

```bash
stress \
  --feeders 100 \
  --duration 5m \
  --beastout feed.plane.watch:12345 \
  --sink nats://localhost:4222
```

## How It Works

1. **Fetch API keys** from NATS (valid feeder credentials)
2. **Spawn workers**: One per simulated feeder
3. **Connect to runway**: TLS with SNI authentication
4. **Stream data**: Embedded Beast data with realistic timing
5. **Run for duration**: Then cleanly disconnect

## Command-Line Flags

**--feeders**: Number of simulated feeders
```bash
--feeders 100  # Simulate 100 concurrent connections
```

**--duration**: How long to run
```bash
--duration 5m   # 5 minutes
--duration 30s  # 30 seconds
```

**--beastout**: Runway endpoint
```bash
--beastout feed.plane.watch:12345
```

**--spawndelay**: Delay between spawning feeders
```bash
--spawndelay 50ms  # Default
```

**Why delay**: Prevents thundering herd, simulates gradual connection

**--ifgmin/--ifgmax**: Inter-frame gap (timing between frames)
```bash
--ifgmin 5ms   # Minimum gap
--ifgmax 50ms  # Maximum gap
```

**Random timing**: Each feeder sends frames at random intervals within range

**--sink**: NATS for fetching API keys
```bash
--sink nats://localhost:4222
```

## Test Data

**Embedded Beast file**: `full-feed.beast`
- Real aircraft data
- Multiple aircraft
- Varied frame types
- Realistic patterns

**Loops continuously**: Streams embedded data repeatedly

## Monitoring

**Watch runway metrics**:
```bash
# Monitor during stress test
watch -n 1 curl -s http://runway:9602/metrics | grep -E '(frames|planes|clients)'
```

**Key metrics to observe**:
- `num_decoded_frames`: Frame ingestion rate
- `current_tracked_planes_count`: Aircraft being tracked
- CPU and memory usage
- Network throughput

## Example Workflow

```bash
# Start runway
runway --cert server.crt --key server.key --sink nats://localhost:4222 daemon

# In another terminal, start stress test
stress \
  --feeders 200 \
  --duration 10m \
  --beastout localhost:12345 \
  --sink nats://localhost:4222

# Monitor in third terminal
watch -n 1 'curl -s http://localhost:9602/metrics | grep -E "(frames|planes)"'
```

## Interpreting Results

**Success**: runway handles load without errors
- Frame rate steady
- No connection drops
- CPU/memory stable

**Failure indicators**:
- Dropped connections
- Frame decode errors spike
- CPU saturation
- Memory growth

## See Also

- [runway](../runway/README.md) - Service being tested
- [feederauth](../feederauth/README.md) - API key validation
- [stunnel](../stunnel/README.md) - TLS connections
