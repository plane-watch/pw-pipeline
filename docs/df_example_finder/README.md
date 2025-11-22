# df_example_finder - Frame Example Collector

## Overview

`df_example_finder` filters live frame streams to find specific DF (Downlink Format) and ME (Message Type) combinations for building test datasets and debugging.

## Purpose

**Collect examples**: Find rare or specific frame types

**Use cases**:
- Building test suites
- Finding edge cases
- Protocol research
- Documentation examples

## Usage

```bash
# Find all DF 17 frames
df_example_finder --fetch beast://receiver:30005 --df 17

# Find DF 17, ME 19 (velocity) frames
df_example_finder --fetch beast://receiver:30005 --df 17 --me 19

# Find from specific aircraft
df_example_finder --fetch beast://receiver:30005 --icao A12345

# Only location frames
df_example_finder --fetch beast://receiver:30005 --locations-only
```

## Output

**Logs matching frames**: Human-readable format

**Can pipe to file**: Capture for later analysis

## Filters

**--df**: Downlink Format (0-24)
**--me**: Message Type/ME (0-31, for DF 17/18)
**--icao**: Specific aircraft
**--locations-only**: Only position messages

## See Also

- [example_finder](../example_finder/README.md) - Middleware implementation
- [tracker/mode_s](../tracker/mode_s/README.md) - DF types reference
