# recorder - Stream Recorder

## Overview

`recorder` captures ADS-B frame streams to disk files for offline analysis, debugging, and test data collection.

## Purpose

**Capture for later**: Record live streams for playback

**Use cases**:
- Debugging decode issues
- Building test data sets
- Performance analysis
- Protocol research

## Usage

```bash
# Record all frames
recorder --fetch beast://receiver:30005

# Record specific aircraft
recorder --fetch beast://receiver:30005 --filter-icao A12345

# Record to specific directory
recorder --fetch beast://receiver:30005 --output /data/captures
```

## Output Format

**Separate files per ICAO**:
```
frames.beast/A12345.beast
frames.beast/B67890.beast
frames.avr/A12345.avr
```

**File naming**: `<icao>.<format>`

## Filtering

**--filter-icao**: Only record specific aircraft
```bash
--filter-icao A12345 --filter-icao B67890
```

**Why filter**: Reduce storage, focus on specific aircraft

## Playback

```bash
# Replay captured file
pw_ingest --file beast://captures/A12345.beast?delay=yes
```

## See Also

- [producer](../producer/README.md) - File replay
- [tracker](../tracker/README.md) - Frame decoding
