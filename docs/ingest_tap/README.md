# ingest_tap - Frame Stream Monitor

## Overview

`ingest_tap` is a TUI (terminal user interface) for real-time monitoring of frame streams flowing through the pipeline. It provides live visibility into what frames are being processed.

## Purpose

**Real-time debugging**: See frames as they're processed

**Use cases**:
- Verify receiver connectivity
- Monitor frame types
- Debug decode issues
- Watch specific aircraft

## Usage

```bash
# Monitor NATS ingest tap
ingest_tap --nats nats://localhost:4222

# Monitor with filters
ingest_tap --nats nats://localhost:4222 --icao A12345
```

## TUI Interface

**Display**:
- Frame rate (frames/sec)
- Frame types (DF 17, DF 11, etc.)
- Aircraft ICAO addresses
- Decode errors
- Scrolling frame list

**Keyboard controls**:
- `q`: Quit
- Arrow keys: Scroll
- `/`: Search/filter

## How It Works

**IngestTap middleware**: Publishes frames to NATS topic

**ingest_tap subscribes**: Receives real-time feed

**No impact**: Read-only monitoring (doesn't affect pipeline)

## See Also

- [middleware](../middleware/README.md) - IngestTap implementation
- [tracker](../tracker/README.md) - Frame processing
