# Plane.Watch Pipeline

**Real-time aircraft tracking infrastructure for processing ADS-B data at scale**

The Plane.Watch pipeline is a distributed system for ingesting, decoding, tracking, and distributing aircraft position data from ADS-B receivers worldwide. Built in Go for performance and reliability.

[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](LICENSE.md)
[![Go Version](https://img.shields.io/github/go-mod/go-version/plane-watch/pw-pipeline)](go.mod)

## Features

- **Multi-format ingestion**: Beast, AVR, SBS1 protocol support
- **Distributed tracking**: Edge-based aircraft state tracking
- **Smart routing**: 90%+ reduction in update volume via significance filtering
- **Geographic partitioning**: 75-tile global grid for regional subscriptions
- **Real-time WebSocket**: Push updates to thousands of concurrent clients
- **Production-ready**: Prometheus metrics, health checks, graceful shutdown
- **Scalable**: Handles 100k+ frames/second across distributed deployments

## Quick Start

### Prerequisites

- Go 1.21 or later
- NATS server (for distributed mode)
- ADS-B receiver (dump1090, readsb, etc.)

### Build

```bash
# Clone repository
git clone https://github.com/plane-watch/pw-pipeline.git
cd pw-pipeline

# Build all binaries
make

# Binaries are in bin/
ls bin/
```

### Run Single-Node Demo

```bash
# Start NATS server
docker run -d --name nats -p 4222:4222 nats:latest

# Ingest from local receiver and track aircraft
bin/pw_ingest \
  --fetch beast://localhost:30005 \
  --sink nats://localhost:4222 \
  --tag my-receiver \
  simple

# In another terminal, monitor metrics
curl http://localhost:9602/metrics | grep planes
```

## Architecture

```
┌──────────┐
│Receivers │ (dump1090, readsb)
└────┬─────┘
     │ Beast/AVR/SBS1
     ↓
┌──────────┐
│pw_ingest │ Decode & track aircraft
│ runway   │ (client or server mode)
└────┬─────┘
     │ NATS: location-updates
     ↓
┌──────────┐
│pw_router │ Filter significant changes (90% reduction)
└────┬─────┘
     │ NATS: tile-based queues
     ├────────────────┬─────────────┐
     ↓                ↓             ↓
┌───────────┐  ┌──────────┐  ┌──────────┐
│pw_ws_broker│  │ClickHouse│  │pw_atc_api│
│(WebSocket) │  │ (storage)│  │ (search) │
└───────────┘  └──────────┘  └──────────┘
```

**See [Architecture Documentation](docs/README.md#architecture) for detailed explanation.**

## Core Applications

| Application | Purpose | Documentation |
|------------|---------|---------------|
| **pw_ingest** | Ingest from receivers, track aircraft | [docs/pw_ingest](docs/pw_ingest/README.md) |
| **runway** | Accept authenticated feeder connections | [docs/runway](docs/runway/README.md) |
| **pw_router** | Route & filter significant updates | [docs/pw_router](docs/pw_router/README.md) |
| **pw_ws_broker** | WebSocket server for real-time clients | [docs/pw_ws_broker](docs/pw_ws_broker/README.md) |
| **pw_atc_api** | Search, enrichment, feeder management | [docs/pw_atc_api](docs/pw_atc_api/README.md) |

**See [all commands](docs/README.md#command-line-applications-cmd) for complete list including utilities.**

## Library Packages

The `lib/` directory contains reusable packages for building aircraft tracking applications:

- **[tracker](docs/tracker/README.md)** - Core aircraft state tracking engine
- **[mode_s](docs/tracker/mode_s/README.md)** - Mode S / ADS-B protocol decoding
- **[producer](docs/producer/README.md)** - Multi-format data source abstraction
- **[nats_io](docs/nats_io/README.md)** - NATS message bus integration
- **[tile_grid](docs/tile_grid/README.md)** - 75-tile geographic partitioning

**See [all libraries](docs/README.md#library-packages-lib) for complete list.**

## Documentation

**📚 [Complete Documentation](docs/README.md)** - Start here for comprehensive guides

### Quick Links

- [Getting Started Guide](docs/README.md#getting-started)
- [Architecture & Data Flow](docs/README.md#architecture)
- [Troubleshooting](docs/README.md#troubleshooting)
- [Performance Benchmarks](docs/README.md#performance-benchmarks)
- [Production Deployment](docs/README.md#production-deployment)

### By Topic

- **Understanding the System**: [Key Concepts](docs/README.md#key-concepts)
- **Running Services**: [Core Services](docs/README.md#core-services)
- **Monitoring**: [Prometheus Metrics](docs/monitoring/README.md)
- **Development**: [Contributing](#contributing)

## Development

### Building

```bash
# Build all binaries
make

# Build specific command
go build ./cmd/pw_ingest

# Run without building
go run ./cmd/pw_ingest --help
```

### Testing

```bash
# Run all tests
make test

# Test specific package
go test ./lib/tracker/...

# Run with race detection
make race

# Verbose test output
go test -v ./lib/tracker
```

### Code Quality

```bash
# Run linter (requires Docker)
make lint

# Run gitleaks security scan
make leakcheck

# Go vet
make vet
```

## Examples

### Ingest from Multiple Receivers

```bash
pw_ingest \
  --fetch beast://sdr1:30005?tag=north \
  --fetch beast://sdr2:30005?tag=south \
  --dedupe-filter \
  --sink nats://nats-server:4222 \
  daemon
```

### Accept Feeder Connections (Server Mode)

```bash
runway \
  --cert server.crt \
  --key server.key \
  --listen-beast :12345 \
  --listen-mlat :12346 \
  --sink nats://nats-server:4222 \
  daemon
```

### Monitor Frame Stream

```bash
# Real-time TUI monitoring
ingest_tap --nats nats://localhost:4222

# Record frames to disk
recorder --fetch beast://receiver:30005 --filter-icao A12345
```

### Decode Mode S Frames

```bash
# Web interface
website_decode --listen-http :8080

# Visit http://localhost:8080 and paste hex frames
```

## Monitoring

All services expose Prometheus metrics:

```bash
# View metrics
curl http://localhost:9602/metrics

# Common metrics
curl -s http://localhost:9602/metrics | grep -E '(frames|planes|clients)'

# Health check
curl http://localhost:9602/status
```

**See [Monitoring Documentation](docs/monitoring/README.md) for Grafana dashboards and alerts.**

## Performance

Typical single-node performance:

| Metric | Value |
|--------|-------|
| Frame ingestion | 5,000 frames/sec |
| Aircraft tracked | 500 concurrent |
| Update reduction | 90-95% |
| WebSocket clients | 1,000+ concurrent |
| Memory usage | 200-500 MB |
| CPU usage | 1-2 cores |

**See [Performance Benchmarks](docs/README.md#performance-benchmarks) for detailed metrics.**

## Contributing

We welcome contributions! Here's how to get started:

1. **Fork the repository**
2. **Create a feature branch**: `git checkout -b feature/amazing-feature`
3. **Make your changes** and add tests
4. **Run tests**: `make test`
5. **Commit**: `git commit -m 'Add amazing feature'`
6. **Push**: `git push origin feature/amazing-feature`
7. **Open a Pull Request**

### Development Guidelines

- Write tests for new code
- Follow Go best practices
- Document exported functions
- Update relevant documentation in `docs/`
- Ensure `make lint` passes

## Resources

### ADS-B / Mode S References

- [The 1090MHz Riddle](https://mode-s.org/decode/book-the_1090mhz_riddle-junzi_sun.pdf) - Comprehensive Mode S guide
- [ADS-B Decoding Guide](http://airmetar.main.jp/radio/ADS-B%20Decoding%20Guide.pdf)
- [pyModeS](https://github.com/junzis/pyModeS) - Python Mode S decoder (reference)
- [ICAO Annex 10](https://www.icao.int/) - Official Mode S specification

### Related Projects

- [dump1090](https://github.com/antirez/dump1090) - Original ADS-B decoder
- [readsb](https://github.com/wiedehopf/readsb) - Modern dump1090 fork
- [tar1090](https://github.com/wiedehopf/tar1090) - Web interface for readsb

### Infrastructure

- [NATS.io](https://nats.io/) - Message bus
- [ClickHouse](https://clickhouse.com/) - Time-series database
- [Prometheus](https://prometheus.io/) - Metrics and monitoring

## License

This project is licensed under the GNU General Public License v3.0 - see the [LICENSE.md](LICENSE.md) file for details.

## Authors

- Jason Playne - <jason@jasonplayne.com>
- Mike Nye - <mike.nye@gmail.com>
- Tim Raphael - <me@timraphael.com>
- Shane Short - <shane@short.id.au>

And many other contributors - thank you! 🙏

## Support

- **Documentation**: [docs/README.md](docs/README.md)
- **Issues**: [GitHub Issues](https://github.com/plane-watch/pw-pipeline/issues)
- **Discussions**: [GitHub Discussions](https://github.com/plane-watch/pw-pipeline/discussions)

---

**Built with ❤️ for the aviation community**
