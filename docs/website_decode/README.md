# website_decode - Web-Based Mode S Decoder

## Overview

`website_decode` is a web application that provides an interactive interface for decoding Mode S frames. Paste hex-encoded frames, get human-readable decoded output.

## Purpose

**Interactive debugging**: Decode frames in browser

**Use cases**:
- Understanding Mode S protocol
- Debugging decode issues
- Learning ADS-B message structure
- Quick frame analysis without code

## Usage

```bash
# HTTP only
website_decode --listen-http :8080

# HTTPS
website_decode \
  --listen-https :8443 \
  --tls-cert server.crt \
  --tls-cert-key server.key
```

**Access**: `http://localhost:8080` or `https://localhost:8443`

## Web Interface

### Input

**Paste hex frames**:
```
8D4840D6202CC371C32CE0576098
8D4840D658990DB89CE9E3C3C659
```

**Formats accepted**:
- Raw hex (AVR format)
- With newlines
- Multiple frames at once

### Output

**Decoded information**:
- Downlink Format (DF)
- ICAO address
- Message type
- Decoded fields (position, altitude, velocity, etc.)
- Human-readable description

**Example**:
```
Frame: 8D4840D6202CC371C32CE0576098
  DF: 17 (ADS-B Extended Squitter)
  ICAO: 4840D6
  TC: 4 (Aircraft Identification)
  Callsign: UAL123
  CRC: Valid
```

## Endpoints

**Web UI**: `/`
**Decode API**: `/decode` (POST)
**Health**: `:9605/status` (separate metrics port)

## API Usage

**POST /decode**:
```bash
curl -X POST http://localhost:8080/decode \
  -H 'Content-Type: application/json' \
  -d '{"frames": ["8D4840D6202CC371C32CE0576098"]}'
```

**Response**:
```json
{
  "results": [
    {
      "frame": "8D4840D6202CC371C32CE0576098",
      "df": 17,
      "icao": "4840D6",
      "type": "Aircraft Identification",
      "callsign": "UAL123",
      ...
    }
  ]
}
```

## Command-Line Flags

**--listen-http**: HTTP port
```bash
--listen-http :8080  # Default
```

**--listen-https**: HTTPS port
```bash
--listen-https :8443  # Default
```

**--tls-cert**: TLS certificate (PEM)
```bash
--tls-cert /path/to/cert.pem
```

**--tls-cert-key**: TLS private key (PEM)
```bash
--tls-cert-key /path/to/key.pem
```

**--prometheus-port**: Metrics endpoint
```bash
--prometheus-port 9605  # Default
```

## Deployment

### Docker

```yaml
services:
  website_decode:
    image: planewatch/website_decode:latest
    ports:
      - "8080:8080"
      - "9605:9605"
    restart: unless-stopped
```

### Public Deployment

**HTTPS recommended**:
```bash
website_decode \
  --listen-https :443 \
  --tls-cert /etc/letsencrypt/live/decode.plane.watch/fullchain.pem \
  --tls-cert-key /etc/letsencrypt/live/decode.plane.watch/privkey.pem
```

**Redirect HTTP to HTTPS**: Use nginx or similar

## Educational Use

**Learning Mode S**:
1. Find example frames (from recorder or dumps)
2. Paste into website_decode
3. See decoded structure
4. Compare with ICAO Annex 10 spec

**Testing decode logic**:
- Verify frame parsing
- Check CRC validation
- Confirm field extraction

## See Also

- [tracker/mode_s](../tracker/mode_s/README.md) - Mode S decoding library
- [recorder](../recorder/README.md) - Capture frames for decoding
- [df_example_finder](../df_example_finder/README.md) - Find specific frames
