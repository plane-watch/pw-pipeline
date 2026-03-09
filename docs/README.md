# Plane.Watch Pipeline Documentation

## Overview

The Plane.Watch pipeline is a distributed system for collecting, processing, and distributing real-time aircraft tracking data from ADS-B receivers worldwide. This documentation covers both the library packages (`lib/`) that provide core functionality and the command-line applications (`cmd/`) that compose the production system.

## Architecture

```
┌─────────────┐
│  Receivers  │ (dump1090, readsb, etc.)
└──────┬──────┘
       │ Beast/AVR/SBS1
       ↓
┌─────────────┐
│  pw_ingest  │ Ingests & tracks aircraft
│   runway    │ Auth + server mode
└──────┬──────┘
       │ NATS: location-updates
       ↓
┌─────────────┐
│  pw_router  │ Filters significant updates
└──────┬──────┘
       │ NATS: tile queues
       ├──────────────────┬────────────────┐
       ↓                  ↓                ↓
┌─────────────┐   ┌──────────────┐   ┌───────────┐
│pw_ws_broker │   │ ClickHouse   │   │pw_atc_api │
│             │   │  (storage)   │   │ (search,  │
│ (WebSocket) │   └──────────────┘   │ enrichment)│
└─────────────┘                      └───────────┘
       │
       ↓
  Web Clients
```

### Data Flow

1. **Ingestion**: Receivers send ADS-B data to `pw_ingest` or `runway`
2. **Tracking**: Raw frames decoded, aircraft state tracked
3. **Publishing**: Location updates published to NATS
4. **Routing**: `pw_router` filters for significant changes
5. **Distribution**: Updates routed to tile-based queues
6. **Consumption**: WebSocket clients, storage, APIs consume updates

## Library Packages (`lib/`)

Core functionality organized into focused, reusable packages:

### Aircraft Tracking

- **[tracker](tracker/README.md)** - Core aircraft tracking engine
  - **[mode_s](tracker/mode_s/README.md)** - Mode S protocol decoding
  - **[beast](tracker/beast/README.md)** - Beast binary format
  - **[sbs1](tracker/sbs1/README.md)** - SBS1/BaseStation text format
- **[export](export/README.md)** - PlaneLocation struct, JSON serialization, multi-source merging

### Data Ingestion & Processing

- **[producer](producer/README.md)** - Data source abstraction (Beast/AVR/SBS1)
- **[dedupe](dedupe/README.md)** - Frame deduplication using forgetful maps
- **[middleware](middleware/README.md)** - Frame transformation pipeline (accounting, monitoring)
- **[sink](sink/README.md)** - Event publishing abstraction with batching

### Messaging & Storage

- **[nats_io](nats_io/README.md)** - NATS message bus integration
- **[clickhouse](clickhouse/README.md)** - ClickHouse time-series storage
- **[tile_grid](tile_grid/README.md)** - 75-tile geographic partitioning system

### Networking & Security

- **[feederauth](feederauth/README.md)** - Three-tier authentication (authorized, connected, rate-limited)
- **[stunnel](stunnel/README.md)** - TLS listener/dialer with SNI auth
- **[mlatbridge](mlatbridge/README.md)** - TLS proxy for MLAT connections
- **[ws_client](ws_client/README.md)** - Simple WebSocket client (channel-based)
- **[ws_protocol](ws_protocol/README.md)** - WebSocket protocol definitions (callback-based client)

### Utilities

- **[logging](logging/README.md)** - zerolog integration, CPU profiling
- **[monitoring](monitoring/README.md)** - Prometheus metrics, health checks
- **[timing](timing/README.md)** - Periodic task execution with cancellation
- **[setup](setup/README.md)** - CLI flag integration helpers
- **[mapping](mapping/README.md)** - HERE Maps geocoding API
- **[randstr](randstr/README.md)** - Random string generation
- **[example_finder](example_finder/README.md)** - Debug filter for test data collection

## Command-Line Applications (`cmd/`)

Production services and utilities:

### Core Services

- **[pw_ingest](pw_ingest/README.md)** - Ingests ADS-B data, tracks aircraft, publishes to NATS
  - Client mode: Connects to receivers
  - Outputs location updates to sink (typically NATS)

- **[runway](runway/README.md)** - Server for feeder connections with authentication
  - Accepts Beast/MLAT connections over TLS
  - SNI-based authentication via API key
  - Routes authenticated data to NATS

- **[pw_router](pw_router/README.md)** - Filters and routes location updates
  - Reduces update rate (only significant changes)
  - Routes to tile-based queues for geographic filtering
  - Maintains cache to detect significant changes

- **[pw_ws_broker](pw_ws_broker/README.md)** - WebSocket server for real-time clients
  - Tile-based subscriptions
  - Compression and rate limiting
  - Serves web clients with aircraft positions

- **[pw_atc_api](pw_atc_api/README.md)** - API server for search and enrichment
  - NATS RPC for search queries
  - Aircraft enrichment (registration, type, operator)
  - Feeder statistics and management

### Utilities

- **[recorder](recorder/README.md)** - Records ADS-B streams to disk files
- **[ingest_tap](ingest_tap/README.md)** - TUI for monitoring frame streams
- **[df_example_finder](df_example_finder/README.md)** - Finds specific frame examples
- **[plane.path](plane.path/README.md)** - Aircraft path tracking and visualization
- **[stress](stress/README.md)** - Load testing and benchmarking
- **[website_decode](website_decode/README.md)** - Web-based Mode S decoder

## Key Concepts

### Aircraft Tracking

**Mode S/ADS-B**: Aviation surveillance protocol where aircraft broadcast identity, position, velocity

**Frame viability**: Aircraft requires multiple valid frames before considered "viable" (reduces noise)

**CPR decoding**: Compact Position Reporting requires odd/even frame pairs to determine position

**ICAO address**: 24-bit unique aircraft identifier (e.g., `A12345`)

### Performance & Scalability

**Forgetful maps**: Time-based automatic eviction prevents unbounded memory growth

**Parallel decode workers**: Multi-core frame decoding (default: NumCPU)

**Batching**: Aggregate updates before publishing/storing (reduces overhead)

**Tile-based routing**: Geographic partitioning distributes load, enables regional subscriptions

### Messaging Patterns

**Producer-Consumer**: Abstracted data sources feed into tracker

**Middleware**: Transformations applied to frame stream (dedupe, accounting, filtering)

**Sink**: Abstract destination for events (NATS, file, etc.)

**NATS topics**:
- `location-updates`: All aircraft position updates
- `tile{N}_low`: Significant updates for tile N
- `tile{N}_high`: All updates for tile N

### Security

**SNI-based auth**: TLS Server Name Indication carries API key in hostname

**Three-tier auth**: Authorized (valid key), Connected (active), Rate-limited (tracking stats)

**TLS required**: All production feeder connections encrypted

## Getting Started

### Development Setup

```bash
# Install dependencies
go mod download

# Build all commands
go build ./cmd/...

# Run tests
go test ./lib/...
```

### Running pw_ingest (Client Mode)

```bash
# Connect to local receiver, output to NATS
pw_ingest \
  --fetch beast://192.168.1.10:30005 \
  --sink nats://localhost:4222 \
  --tag my-receiver \
  simple
```

### Running runway (Server Mode)

```bash
# Accept feeder connections
runway \
  --cert server.crt \
  --key server.key \
  --nats nats://localhost:4222
```

### Monitoring

```bash
# Prometheus metrics (all services)
curl http://localhost:9602/metrics

# Health check
curl http://localhost:9602/status
```

## Production Deployment

### Typical Production Stack

1. **Feeders** → `runway` (port 12345 Beast, 12346 MLAT)
2. **runway** → NATS (`location-updates` topic)
3. **pw_router** consumes `location-updates`, publishes to tile queues
4. **pw_ws_broker** consumes tile queues, serves WebSocket clients
5. **ClickHouse** stores historical data
6. **pw_atc_api** provides search/enrichment via NATS RPC

### Scaling Considerations

**Horizontal scaling**:
- Multiple `pw_ingest` instances for different receivers
- Multiple `runway` instances behind load balancer
- Tile-based queue partitioning naturally distributes `pw_ws_broker` load

**Vertical scaling**:
- Increase decode workers: `--decode-worker-count`
- Tune NATS queue depth for burst handling
- Increase ClickHouse batch size for write throughput

**Memory management**:
- Tracker prunes aircraft after 5 minutes idle
- Dedupe retention window (default 60 seconds)
- Router cache eviction for stale aircraft

## Troubleshooting

### Common Issues

**No aircraft tracked**:
- Check receiver connection: `curl http://localhost:9602/metrics | grep frames`
- Verify frames decoded: Look for `num_decoded_frames` metric
- Check frame viability threshold (default: 2 frames)

**High memory usage**:
- Check `current_tracked_planes_count` metric
- Verify pruning working (stale aircraft evicted)
- Check dedupe map size if using deduplication

**NATS connection issues**:
- Verify NATS server running: `nats-server -V`
- Check connection string format: `nats://user:pass@host:4222`
- Review firewall rules for port 4222

**WebSocket clients not receiving updates**:
- Verify `pw_router` publishing to tile queues
- Check tile subscription matches aircraft location
- Review `pw_ws_broker` logs for connection errors

### Debug Tools

**ingest_tap**: Real-time frame stream monitoring
```bash
ingest_tap --nats nats://localhost:4222
```

**recorder**: Capture frames for offline analysis
```bash
recorder --fetch beast://receiver:30005 --filter-icao A12345
```

**df_example_finder**: Find specific frame types
```bash
df_example_finder --fetch beast://receiver:30005 --df 17 --me 19
```

## Performance Benchmarks

### Typical Throughput

| Component | Metric | Typical Value |
|-----------|--------|---------------|
| pw_ingest | Frames/sec | 1,000-5,000 |
| pw_ingest | Aircraft tracked | 50-500 |
| pw_router | Updates/sec in | 100-1,000 |
| pw_router | Updates/sec out | 10-100 (90%+ reduction) |
| pw_ws_broker | Clients | 100-1,000 |
| pw_ws_broker | Messages/sec | 1,000-10,000 |

### Resource Usage

| Component | CPU | Memory | Network |
|-----------|-----|--------|---------|
| pw_ingest | 10-30% (1 core) | 50-200 MB | 100 KB/s in, 50 KB/s out |
| pw_router | 5-15% (1 core) | 20-100 MB | 50 KB/s in, 10 KB/s out |
| pw_ws_broker | 10-40% (2 cores) | 100-500 MB | 10 KB/s in, 500 KB/s out |

## Contributing

### Code Organization

- `lib/`: Reusable packages, no main functions
- `cmd/`: Applications with main functions
- `docs/`: Package documentation (this directory)

### Documentation Standards

- **Why-focused**: Explain rationale, not just mechanics
- **Production lessons**: Include real-world experiences
- **Code examples**: Demonstrate actual usage
- **Troubleshooting**: Common issues and solutions

### Adding New Packages

1. Create package in `lib/` or `cmd/`
2. Write comprehensive `README.md` in `docs/{package}/`
3. Include "See Also" cross-references
4. Add entry to this index

## External Resources

- **Mode S / ADS-B**:
  - [ICAO Annex 10, Volume IV](https://www.icao.int/safety/acp/repository/annex_10_volume_iv_surveillance_radar_and_collision_avoidance_systems.pdf)
  - [DO-260B ADS-B Specification](https://www.rtca.org/content/standards-guidance-materials)

- **Beast Format**:
  - [Mode-S Beast Wiki](https://wiki.modesbeast.com/)
  - [Radarcape Documentation](https://wiki.jetvision.de/)

- **Go Libraries**:
  - [NATS.io](https://docs.nats.io/)
  - [ClickHouse Go Driver](https://github.com/ClickHouse/clickhouse-go)
  - [Prometheus Client](https://github.com/prometheus/client_golang)
  - [zerolog](https://github.com/rs/zerolog)

## License

(License information would go here)

## Authors

- Jason Playne - <jason@jasonplayne.com>
- Mike Nye - <mike.nye@gmail.com>
- Tim Raphael <me@timraphael.com>
- Shane Short <shane@short.id.au>
- And many others!

## Version

Documentation last updated: 2025-01-22

For application-specific documentation, see the individual package README files linked above.