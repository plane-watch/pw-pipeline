# pw_ws_broker - WebSocket Broker

## Overview

`pw_ws_broker` is the WebSocket server that provides real-time aircraft position updates to web clients. It consumes tile-based location queues from NATS, maintains in-memory aircraft state, and pushes updates to subscribed WebSocket clients.

## Purpose

**Real-time client delivery**: Bridge between NATS message bus and WebSocket clients

**Why WebSocket broker**:
- Web clients need real-time push updates
- Tile-based subscriptions reduce bandwidth (subscribe to regions of interest)
- Compression reduces data size 60-70%
- Scales to thousands of concurrent clients

## Architecture

```
NATS: tile60_low, tile60_high, tile61_low, ...
         ↓
    pw_ws_broker
         ↓
    WebSocket clients (web browsers, mobile apps)
```

## Key Features

- **Tile-based subscriptions**: Clients subscribe to geographic regions
- **Compression**: WebSocket compression (60-70% reduction)
- **Rate limiting**: Configurable update tick (default: 1 second)
- **Search**: NATS RPC to pw_atc_api
- **Grid information**: Tile boundaries via HTTP endpoint
- **ClickHouse integration**: Optional position storage

## Command-Line Flags

```bash
pw_ws_broker \
  --nats nats://localhost:4222 \
  --http :8080 \
  --prometheus-port 9600 \
  daemon
```

**Key flags**:
- `--nats`: NATS server URL
- `--http`: WebSocket/HTTP listen address
- `--clickhouse`: Optional storage URL
- `--send-tick`: Update interval (milliseconds, default: 1000)
- `--serve-test`: Serve test web page

## WebSocket Protocol

See [ws_protocol documentation](../ws_protocol/README.md) for complete protocol details.

**Subscribe to tile**:
```json
{"type": "sub", "gridTile": "tile60_high"}
```

**Set subscribed tile list** (atomic replacement):
```json
{"type": "set-sub-tile-list", "gridTile": "tile35_high,tile36_high", "requestId": "req-1"}
```

The `set-sub-tile-list` lifecycle:
1. Server validates all tiles — rejects entire request if any tile is unknown (existing subscriptions preserved)
2. `ack-sub` — subscriptions applied, includes sorted tile list and `requestId`
3. `plane-location-list` — immediate snapshot of matching aircraft (omitted if zero match), includes `requestId`
4. `initial-sync-complete` — snapshot phase done, includes `tiles`, `aircraftCount`, and `requestId`. **Always sent**, even when zero aircraft match.

After the initial sync, live updates arrive as tick-batched `plane-location-list` messages (without `requestId`).

**Tile matching**: Subscriptions use suffixed tile names (e.g. `tile35_high`). Aircraft `TileLocation` is always bare (e.g. `tile35`). Both the snapshot path and the live streaming path use the same matching function (`tileMatchesSubs`) to determine whether an aircraft matches the subscription set.

**Receive updates**:
```json
{
  "type": "plane-location",
  "location": {
    "icao": "A12345",
    "lat": 47.6062,
    "lon": -122.3321,
    ...
  }
}
```

## Endpoints

**WebSocket**: `ws://host:8080/planes`
**Grid info**: `http://host:8080/grid`
**Metrics**: `http://host:9600/metrics`
**Health**: `http://host:9600/status`

## Prometheus Metrics

| Metric | Description |
|--------|-------------|
| `pw_ws_broker_num_clients` | Current WebSocket clients |
| `pw_ws_broker_incoming_messages` | Messages from NATS |
| `pw_ws_broker_known_planes` | Aircraft in memory |
| `pw_ws_broker_messages_sent` | Updates sent to clients |
| `pw_ws_broker_subscriptions` | Subscription counts per tile |

## See Also

- [ws_protocol](../ws_protocol/README.md) - WebSocket protocol details
- [ws_client](../ws_client/README.md) - Client library
- [pw_router](../pw_router/README.md) - Produces tile queues
- [tile_grid](../tile_grid/README.md) - Geographic tiles
