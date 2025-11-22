# Example Finder (Debug Filter)

## Overview

The `example_finder` package provides a selective frame filter for development, debugging, and creating test datasets. It allows filtering the frame stream by ICAO address, Downlink Format (DF) type, or ADS-B message type, logging matches for analysis or example collection.

## Purpose

**Not for production** - This is a development/debugging tool.

### Use Cases

1. **Collecting test examples**:
   ```
   "I need examples of DF5 frames for unit tests"
   → Filter by DF type, capture frames
   ```

2. **Debugging specific aircraft**:
   ```
   "ICAO ABC123 is behaving strangely"
   → Filter by ICAO, see all its frames
   ```

3. **Understanding message types**:
   ```
   "What do TC 19 velocity messages actually look like?"
   → Filter by DF17 + ME type 19, examine frames
   ```

4. **Creating documentation examples**:
   ```
   "Need real-world examples for docs"
   → Filter by type, collect diverse samples
   ```

## How It Works

### Middleware Pattern

```
Producer → Example Finder → Other Middleware → Tracker
              ↓
         Logs matches
        Passes through or drops
```

**Filters in middleware chain**:
- Returns `nil` → Frame dropped (filtered out)
- Returns `frame` → Frame passes to next middleware/tracker

### Filtering Logic

**Multi-criteria AND logic**:
```go
func (f *Filter) Handle(fe *FrameEvent) Frame {
    // 1. ICAO filter (if configured)
    if len(f.listIcaos) > 0 && !matchesICAO(frame) {
        return nil  // Drop: ICAO not in list
    }

    // 2. DF/MT filter (if configured)
    if len(f.listDfType) > 0 && !matchesDF(frame) {
        return nil  // Drop: DF type not in list
    }

    // 3. Passed all filters
    log.Info().Msg("Found Frame")
    return frame
}
```

**Default behavior** (no filters configured): Pass all frames through

## Configuration Options

### Filter by ICAO Address

**Single aircraft**:
```go
filter := example_finder.NewFilter(
    example_finder.WithPlaneIcao(0xABC123),
)
```

**Multiple aircraft**:
```go
filter := example_finder.NewFilter(
    example_finder.WithPlaneIcao(0xABC123),
    example_finder.WithPlaneIcao(0xDEF456),
)
```

**From hex string**:
```go
filter := example_finder.NewFilter(
    example_finder.WithPlaneIcaoStr("ABC123"),  // Parses hex
    example_finder.WithPlaneIcaoStr("7C1234"),
)
```

**Why string option**: Convenient when reading ICAOs from config files or CLI args

### Filter by Downlink Format

**Single DF type**:
```go
filter := example_finder.NewFilter(
    example_finder.WithDownlinkFormatType(17),  // ADS-B only
)
```

**Multiple DF types**:
```go
filter := example_finder.NewFilter(
    example_finder.WithDownlinkFormatType(4),   // Surveillance Altitude
    example_finder.WithDownlinkFormatType(5),   // Surveillance Identity
    example_finder.WithDownlinkFormatType(17),  // ADS-B
)
```

**Common DF types**:
- **DF 0**: Short Air-Air (ACAS)
- **DF 4**: Surveillance Altitude Reply
- **DF 5**: Surveillance Identity Reply
- **DF 11**: All-Call Reply
- **DF 17**: Extended Squitter (ADS-B)
- **DF 20**: Comm-B Altitude Reply
- **DF 21**: Comm-B Identity Reply

### Filter by ADS-B Message Type

**Position messages only**:
```go
filter := example_finder.NewFilter(
    example_finder.WithDF17MessageTypeLocation(),
)
```

**What this includes**:
```
DF 17 (ADS-B)
  ME Type 5-8:   Surface Position
  ME Type 9-18:  Airborne Position (Baro)
  ME Type 20-22: Airborne Position (GNSS)
```

**Why surface + airborne**: Captures all position-related messages

**Manual ME type selection**:
```go
filter := example_finder.NewFilter(
    example_finder.WithDownlinkFormatType(17),
    example_finder.WithDF17MessageType(19),  // Velocity only
)
```

### Combining Filters

**Example: Track specific aircraft's positions**:
```go
filter := example_finder.NewFilter(
    example_finder.WithPlaneIcaoStr("ABC123"),
    example_finder.WithDF17MessageTypeLocation(),
)
```

**Result**: Only position messages from ICAO ABC123

## Output Format

### Logged Information

**When frame matches**:
```
INFO Found Frame
  AVR="8D4840D6202CC371C32CE0576098"
  DF=17
  DF17MT=2
  DF17MT Sub=1
  icao="4840D6"
```

**Fields explained**:
- `AVR`: Raw Mode S frame (hex string)
- `DF`: Downlink Format type
- `DF17MT`: ADS-B Message Type (TC) if DF17
- `DF17MT Sub`: ADS-B Message Sub-type if applicable
- `icao`: ICAO address (hex string)

### Collecting Examples

**Typical workflow**:

1. **Run with filter**:
   ```bash
   ./pipeline --debug example_finder:DF17MT=19
   ```

2. **Grep log output**:
   ```bash
   grep "Found Frame" pipeline.log > examples.log
   ```

3. **Extract AVR frames**:
   ```bash
   grep -oP 'AVR="\K[^"]+' examples.log > frames.txt
   ```

4. **Use in unit tests**:
   ```go
   testFrames := []string{
       "8D4840D6202CC371C32CE0576098",
       "8D4840D6234...
   }
   ```

## Why Not SBS1 Support?

**Current implementation**:
```go
case *sbs1.Frame:
    // no SBS1 support
    return nil
```

**Why**: SBS1 is pre-decoded (no raw DF/ME type data)

**Workaround**: Use Beast or raw Mode S producers for example collection

**If you need SBS1 support**: Filter by altitude, position, or callsign fields instead

<!--
Maintainers: If you implement SBS1 support, document approach here
-->

## Development Patterns

### Building Test Datasets

**Goal**: Create comprehensive test suite covering all message types

**Approach**:
```go
// Collect examples for each ME type
for meType := 1; meType <= 31; meType++ {
    filter := example_finder.NewFilter(
        example_finder.WithDownlinkFormatType(17),
        example_finder.WithDF17MessageType(byte(meType)),
    )

    // Run until you have 10+ examples of each type
    // Save to test_data/me_{meType}_examples.txt
}
```

**Result**: Test suite with real-world examples of every message type

### Debugging Position Decoding

**Scenario**: CPR decoding seems wrong for certain aircraft

**Debug approach**:
```go
filter := example_finder.NewFilter(
    example_finder.WithPlaneIcaoStr("ABC123"),  // Problem aircraft
    example_finder.WithDF17MessageTypeLocation(),
)
```

**Analysis**:
1. Collect ODD and EVEN frames
2. Manually verify CPR lat/lon calculations
3. Compare to known good position
4. Identify decoding bug

### Understanding Message Patterns

**Scenario**: "How often do aircraft send velocity updates?"

**Collect data**:
```go
filter := example_finder.NewFilter(
    example_finder.WithDownlinkFormatType(17),
    example_finder.WithDF17MessageType(19),  // Velocity
)
```

**Analyze logs**:
```bash
# Count velocity messages per ICAO
grep "Found Frame" log | grep -oP 'icao="\K[^"]+' | sort | uniq -c

# Calculate inter-message timing
grep "Found Frame" log | awk '{print $1}' | ...
```

## Performance Considerations

### Filtering Overhead

**ICAO lookup** (linear search):
```go
for _, icao := range f.listIcaos {
    if icao == frame.Icao() {
        found = true
        break
    }
}
```

**O(n) where n = number of filter ICAOs**

**Impact**:
- 1-10 ICAOs: Negligible (<1µs)
- 100 ICAOs: Noticeable (~10µs)
- 1000+ ICAOs: Use different approach (hash map)

**Why linear search is fine**: Example finder is for development, not production

### Logging Overhead

**Every match logs**:
```go
log.Info().
    Str("AVR", frame.RawString()).
    Int("DF", int(frame.DownLinkType())).
    ...
    Msg("Found Frame")
```

**Log formatting cost**: ~50-100µs per matched frame

**At high match rates**:
- 10 matches/sec: Negligible
- 100 matches/sec: ~1% CPU
- 1000 matches/sec: ~10% CPU (consider buffering)

### When to Disable

**Production environments**: Remove example_finder from middleware chain

**High-rate testing**: Comment out logging, just return frame

## Common Use Cases

### Case 1: Collecting Diversity Examples

**Goal**: Get examples from many different aircraft types

**Filter**: None (let all through), log everything

```go
filter := example_finder.NewFilter()
```

**Post-process**: Deduplicate by ICAO, take first N examples per type

### Case 2: Debugging Specific Format

**Goal**: Understand DF21 (Comm-B Identity) format

**Filter**:
```go
filter := example_finder.NewFilter(
    example_finder.WithDownlinkFormatType(21),
)
```

**Analysis**: Manually decode BDS register, verify implementation

### Case 3: Tracking Aircraft Through Coverage

**Goal**: Full trajectory of aircraft entering/leaving coverage

**Filter**:
```go
filter := example_finder.NewFilter(
    example_finder.WithPlaneIcaoStr("ABC123"),
)
```

**Result**: Complete frame history from first appearance to last

### Case 4: Emergency Squawk Examples

**Goal**: Collect real emergency messages for testing

**Filter**:
```go
filter := example_finder.NewFilter(
    example_finder.WithDownlinkFormatType(17),
    example_finder.WithDF17MessageType(28),  // Emergency status
)
```

**Patience required**: Emergencies are rare!

## Limitations

### No Complex Conditions

**Can't do**: "DF17 with ME type 19 AND velocity > 500 knots"

**Only supports**: Simple equality filters on ICAO/DF/ME type

**Workaround**: Filter broadly, post-process logs

### No Frame Capture to File

**Current**: Logs only (must grep/parse logs)

**Future enhancement**: Optional AVR frame file output

```go
// Potential API:
example_finder.WithAVROutputFile("captures/df17_examples.txt")
```

<!--
Maintainers: If you implement file output, document it here
-->

### No Statistics

**Doesn't track**: Match rate, total frames seen, etc.

**Workaround**: Parse logs for stats

```bash
# Total frames with "Found Frame"
grep -c "Found Frame" log

# Match rate over time
grep "Found Frame" log | awk '{print $1}' | uniq -c
```

## Production Warning

**DO NOT use in production pipelines**:
- Logging overhead
- Not optimized for filtering
- Linear search doesn't scale
- Debug tool, not performance tool

**For production filtering**: Use dedicated middleware with:
- Hash map lookups
- Minimal logging
- Optimized branches

## Integration Example

```go
package main

import (
    "plane.watch/lib/example_finder"
    "plane.watch/lib/tracker"
)

func main() {
    // Create filter
    filter := example_finder.NewFilter(
        example_finder.WithPlaneIcaoStr("7C1234"),
        example_finder.WithDownlinkFormatType(17),
    )

    // Add to tracker middleware
    tracker := tracker.New()
    tracker.AddMiddleware(filter)

    // Run pipeline
    // Logs will show only DF17 frames from 7C1234
}
```

## File Guide

| File | Purpose |
|------|---------|
| `filter.go` | ICAO/DF/ME type filtering logic |

## See Also

- [Middleware](../middleware/README.md) - Middleware pattern details
- [Tracker](../tracker/README.md) - Where middleware integrates
- [Mode S](../tracker/mode_s/README.md) - DF and ME type reference
