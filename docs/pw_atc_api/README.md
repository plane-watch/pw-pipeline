# pw_atc_api - ATC API Server

## Overview

`pw_atc_api` is the API server that provides search, enrichment, and feeder management services via NATS RPC. It acts as the central authority for aircraft metadata and feeder authentication.

## Purpose

**Centralized data services**: Search, enrichment, feeder stats

**Why NATS RPC**:
- Service-to-service communication (not public HTTP)
- Request/reply pattern
- Load balanced across multiple instances
- Integrated with existing NATS infrastructure

## Services Provided

### Search

**Query**: Aircraft, airports, routes by ICAO/callsign/registration

**Request topic**: `api-search`

**Use case**: User searches "UAL123" in web UI

### Enrichment

**Query**: Aircraft metadata by ICAO

**Request topic**: `api-enrich`

**Returns**:
- Registration (tail number)
- Aircraft type (Boeing 737-800)
- Operator (United Airlines)
- Other metadata

**Use case**: Display enriched info alongside position

### Feeder Management

**Query**: Feeder statistics and configuration

**Request topics**:
- `api-feeder-get`: Get feeder details
- `api-feeder-update`: Update feeder stats

**Use case**: runway validates API keys, updates connection stats

## Command-Line Flags

```bash
pw_atc_api \
  --nats nats://localhost:4222 \
  --postgres postgres://user:pass@host/db \
  --prometheus-port 9603 \
  daemon
```

**Key flags**:
- `--nats`: NATS server URL
- `--postgres`: PostgreSQL database URL
- `--prometheus-port`: Metrics endpoint (default: 9603)

## Database

**PostgreSQL** stores:
- Aircraft registry (ICAO → metadata)
- Airport database
- Feeder configurations
- Historical enrichment data

## Prometheus Metrics

| Metric | Description |
|--------|-------------|
| `pw_atc_api_search_count` | Search requests handled |
| `pw_atc_api_enrich_count` | Enrichment requests handled |
| `pw_atc_api_feeder_count` | Feeder API requests |
| `pw_atc_api_search_summary` | Search latency (ms) |
| `pw_atc_api_enrich_summary` | Enrichment latency (ms) |

## See Also

- [feederauth](../feederauth/README.md) - Feeder authentication
- [nats_io](../nats_io/README.md) - NATS RPC integration
- [runway](../runway/README.md) - Uses ATC API for auth
