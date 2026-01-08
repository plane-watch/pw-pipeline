# SBS1 BaseStation Format

## Overview

The SBS1 (BaseStation) format is a human-readable CSV-based protocol developed by Kinetic Avionic Products for their BaseStation receiver. Despite being inefficient, it became ubiquitous due to early adoption and ease of parsing.

## Why SBS1 Still Exists

### Historical Context

**Late 1990s-early 2000s**: First consumer Mode S receivers
- Limited CPU power (parsing binary was "expensive")
- Serial port communication (9600-115200 baud)
- Human operators wanted readable output

**SBS1 format**: Optimized for human readability and low-power parsing

**Today**: We have GHz CPUs and gigabit networks, but SBS1 persists due to:
- Massive existing software ecosystem
- ADS-B aggregator sites (FlightRadar24, etc.) use it
- Simple to debug (readable in text editor)

### The Trade-off

**Bandwidth comparison** (same aircraft position):

```
Raw Mode S:  14 bytes (8D4840D6202CC371C32CE0576098)
Beast:       22 bytes (with timestamp + signal)
SBS1:        ~180 bytes (full CSV line)
```

**10x+ overhead**, but negligible on modern networks.

## Message Structure

CSV format with variable fields based on message type:

```
MSG,3,1,1,4840D6,1,2024/11/16,12:34:56.789,2024/11/16,12:34:56.789,,37000,,,38.89,-77.03,,,,,,
```

### Field Layout

| Position | Field | Description |
|----------|-------|-------------|
| 0 | Message Type | MSG, SEL, ID, AIR, STA, CLK |
| 1 | Transmission Type | 1-8 (sub-category) |
| 2 | Session ID | Database session identifier |
| 3 | Aircraft ID | Database aircraft identifier |
| 4 | ICAO | Hex ICAO address (e.g., "4840D6") |
| 5 | Flight ID | Database flight identifier |
| 6 | Date Generated | YYYY/MM/DD |
| 7 | Time Generated | HH:MM:SS.sss |
| 8 | Date Logged | YYYY/MM/DD |
| 9 | Time Logged | HH:MM:SS.sss |
| 10 | Callsign | Flight number/callsign |
| 11 | Altitude | Feet (barometric or GNSS) |
| 12 | Ground Speed | Knots |
| 13 | Track | Degrees (0-359) |
| 14 | Latitude | Decimal degrees |
| 15 | Longitude | Decimal degrees |
| 16 | Vertical Rate | Feet/minute |
| 17 | Squawk | 4-digit octal code |
| 18 | Alert | Squawk change flag (-1 = changed) |
| 19 | Emergency | Emergency flag (-1 = emergency) |
| 20 | SPI | Ident flag (-1 = active) |
| 21 | On Ground | Ground flag (-1 = on ground) |

**Empty fields**: Common - only populated for relevant message types

## Message Types

### MSG (Transmission)

The main message type carrying actual aircraft data.

#### MSG Sub-types

| Sub | Name | Fields Populated | Source |
|-----|------|------------------|--------|
| 1 | ES Identification | Callsign | TC 1-4 |
| 2 | ES Surface Position | Altitude, Speed, Track, Lat, Lon, On Ground | TC 5-8 |
| 3 | ES Airborne Position | Altitude, Lat, Lon, Alert, Emergency, SPI, On Ground | TC 9-18, 20-22 |
| 4 | ES Airborne Velocity | Speed, Track, Vertical Rate | TC 19 |
| 5 | Surveillance Altitude | Altitude, Alert, SPI, On Ground | DF 4, 20 |
| 6 | Surveillance Identity | Callsign, Altitude, Squawk, Alert, Emergency, SPI, On Ground | DF 5, 21 |
| 7 | Air-to-Air | Altitude, On Ground | DF 0, 16 |
| 8 | All Call Reply | On Ground | DF 11 |

**Why subtypes**: Different Mode S messages contain different data. Subtypes map roughly to Downlink Formats.

### SEL (Selection Change)

```
SEL,,,,4840D6,,,,,,,,,,,,,,,,,
```

**Purpose**: User selected aircraft in BaseStation GUI

**Modern use**: Mostly ignored, vestige of interactive software

### ID (New ID)

```
ID,,,,4840D6,,,,,,,UAL123,,,,,,,,,,
```

**Purpose**: Aircraft callsign first seen or changed

**Modern use**: Redundant with MSG,1 but some parsers rely on it

### AIR (New Aircraft)

```
AIR,,,,4840D6,,,,,,,,,,,,,,,,,
```

**Purpose**: New ICAO address detected

**Modern use**: Some systems use for tracking appearance/disappearance

### STA (Status Change)

```
STA,,,,4840D6,,,,,,PL,,,,,,,,,,
```

**Callsign field values**:
- **PL**: Position Lost (no recent position updates)
- **SL**: Signal Lost (no frames received)
- **RM**: Remove (aircraft left coverage)
- **AD**: Delete (manual removal)
- **OK**: Restored (aircraft returned)

**Modern use**: Connection status tracking

### CLK (Click)

```
CLK,,,,4840D6,,,,,,,,,,,,,,,,,
```

**Purpose**: User clicked aircraft in BaseStation GUI

**Modern use**: Never - pure GUI artifact

## Parsing Strategy

### Timestamp Handling

**Two timestamp pairs**: Generated vs. Logged

**Generated**: When receiver saw the frame
**Logged**: When software processed/stored it

**Why both**: Measure processing latency

**Common case**: Both timestamps identical (real-time processing)

**Parser choice**: Use "Generated" for aircraft state timestamps

**See**: `parse.go:108-113`

### Empty Field Handling

**Challenge**: CSV with many empty fields

```
MSG,3,1,1,4840D6,1,2024/11/16,12:34:56.789,2024/11/16,12:34:56.789,,37000,,,38.89,-77.03,,,,,,
      ↑                                                                        ↑   ↑   ↑
   Several empty fields                                              Empty fields at end
```

**Strategy**: `getField()` helper with bounds checking

```go
func getField(fields []string, fieldId int) string {
    if len(fields) >= fieldId {
        return fields[fieldId]
    }
    return ""
}
```

**Conversion**: Attempt parsing, ignore errors on empty
```go
altitude, _ := strconv.Atoi(getField(fields, sbsAltitudeField))
// If field empty or invalid, altitude = 0 (zero value)
```

**See**: `parse.go:74-79`, usage throughout `Parse()`

### ICAO Conversion

**In SBS1**: Hex string without `0x` prefix (e.g., `"4840D6"`)

**Internal representation**: `uint32`

**Conversion**:
```go
bytes, err := hex.DecodeString(icao)
icaoInt := uint32(bytes[0])<<16 | uint32(bytes[1])<<8 | uint32(bytes[2])
```

**Why validate**: Malformed hex causes decode errors

**See**: `parse.go:192-203`

### Flag Decoding

**SBS1 encoding**: `-1` = true, `0` = false, empty = unknown

**Examples**:
```
On Ground: "-1" → true (on ground)
On Ground: "0"  → false (airborne)
On Ground: ""   → unknown (field not applicable)
```

**Parsing**:
```go
f.OnGround = getField(fields, sbsOnGroundField) == "-1"
// Empty string != "-1", so defaults to false
```

**Trade-off**: Lost distinction between "false" and "unknown", acceptable for this use case

**See**: `parse.go:143`, `parse.go:153`, etc.

## Lossy Translation

**Critical limitation**: SBS1 is pre-decoded

**What's lost**:
- Raw Mode S bytes (can't re-decode differently)
- CRC validation ability (pre-validated by sender)
- Signal strength
- Precision timestamp (second-level only)
- CPR raw values (lat/lon already decoded)

**Implications**:
- Cannot implement different CPR decoder
- Cannot validate message integrity
- Cannot perform MLAT (no nanosecond timestamps)

**When SBS1 is appropriate**:
- Low-bandwidth links
- Human debugging
- Integration with legacy software

**When to avoid**:
- MLAT systems (need Beast)
- Custom decoding logic (need raw Mode S)
- Performance-critical paths (use Beast)

## Integration with Tracker

**Direct use**: SBS1 frames implement `Frame` interface

```go
type Frame interface {
    Icao() uint32
    Decode() error
    TimeStamp() time.Time
}
```

**No Mode S decoding**: SBS1 frames skip Mode S decoder entirely

**Why this works**: SBS1 already decoded, just need to parse CSV

**Trade-off**: Duplicate field storage (SBS1 frame + Plane object)

**See**: `parse.go:205-222` (interface implementation)

## Error Handling

### Malformed Lines

**Too few fields**: Return nil, skip line

**Too many fields**: Accept first 22, ignore rest

**Rationale**: Lenient parsing - some BaseStation variants add custom fields

### Invalid Timestamps

**Fallback**: Use current time

```go
received, err := time.Parse("2006/01/02 15:04:05.999999999", sTime)
if err != nil {
    received = time.Now()
}
```

**Why fallback**: Better to track aircraft with approximate time than reject

### Missing ICAO

**Fatal**: Cannot track aircraft without identifier

```go
if icao == "" {
    return ErrNoIcao
}
```

**See**: `parse.go:192-195`

## Performance Considerations

### Parsing Overhead

**String splitting**: `strings.Split()` allocates

**Number parsing**: `strconv.Atoi()` per field

**Typical parse time**: ~5 microseconds/message (vs. ~100ns for Beast)

**Why acceptable**: At 5000 msg/sec, only 2.5% CPU overhead

### Memory Usage

**Per frame**: ~200 bytes (string storage)

**GC pressure**: Higher than binary formats due to string allocations

**Mitigation**: If performance critical, use Beast instead

## Common Issues

### Windows Line Endings

**Symptom**: Extra `\r` characters in last field

**Cause**: Windows `\r\n` vs. Unix `\n`

**Solution**: `strings.TrimSpace()` in parser

**See**: `parse.go:86` (implicit in field handling)

### Timezone Confusion

**SBS1 spec**: Timestamps are in UTC

**Reality**: Some implementations use local time

**Solution**: Treat as opaque timestamp, rely on frame order

### Coordinate Precision

**Limited**: Typically 2-5 decimal places (~1-100 meters)

**Why**: Lossy conversion from CPR (17-bit integers) to decimal degrees to text

**Better**: Use raw Mode S and decode CPR yourself for full precision

## Format Variants

### SBS3

**Additions**:
- Extended position precision
- Additional status fields
- Backward compatible (parsers ignore extra fields)

**This implementation**: Ignores SBS3 extensions

### Planeplotter

**Differences**:
- Custom message types
- Different field ordering

**Compatibility**: None - Planeplotter is separate format

### FlightAware

**Differences**:
- Additional metadata fields
- Enrichment with database info

**Compatibility**: Core fields compatible, ignore extensions

## When to Use SBS1

### Good Use Cases

- Human debugging (readable in text editor)
- Low-bandwidth (<100 msg/sec)
- Legacy software integration
- Quick prototyping

### Bad Use Cases

- High message rates (>1000 msg/sec)
- MLAT systems (no precision timing)
- Custom Mode S decoding
- Low-latency requirements

### Recommendation

**New systems**: Use Beast for raw data, generate SBS1 on-demand for humans

**Existing systems**: Keep SBS1 for compatibility, but consider Beast for internal use

## File Guide

| File | Purpose |
|------|---------|
| `parse.go` | CSV parsing and field extraction |
| `doco.go` | (If exists) Documentation/examples |

## See Also

- [Tracker](../README.md) - Core tracking engine
- [Mode S Decoding](../mode_s/README.md) - Raw frame decoding
- [Beast Format](../beast/README.md) - Alternative binary format
- [Producer](../../producer/README.md) - SBS1 frame source

## References

- BaseStation Format Documentation: http://woodair.net/sbs/article/barebones42_socket_data.htm
- Kinetic Avionic Products: Original format creators (now defunct)
