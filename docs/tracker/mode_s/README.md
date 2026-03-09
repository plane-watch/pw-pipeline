# Mode S Frame Decoding

## Overview

The `mode_s` package implements comprehensive Mode S (Mode Select) and ADS-B (Automatic Dependent Surveillance-Broadcast) message decoding. Mode S is the foundation of modern aircraft surveillance, used worldwide for air traffic control.

## What is Mode S?

Mode S is a Secondary Surveillance Radar (SSR) system that allows selective interrogation of aircraft and transmission of data between aircraft and ground stations.

**Key Differences from older systems**:
- **Mode A/C**: Broadcast-only, no addressing, 4096 codes (squawk)
- **Mode S**: Addressed interrogations, 16.7 million unique ICAOs, rich data

**Why it exists**: As air traffic grew, Mode A/C's limited address space and broadcast nature created congestion. Mode S enables selective interrogation and bi-directional data transfer.

## Message Structure

All Mode S messages share a common structure:

```
[DF: 5 bits][Type-specific data][Parity: 24 bits]
```

### Downlink Formats (DF)

The first 5 bits determine message type:

| DF | Name | Length | Purpose |
|----|------|--------|---------|
| 0 | Short Air-Air (ACAS) | 56 bits | TCAS surveillance replies |
| 4 | Surveillance Altitude | 56 bits | Altitude reply to ground |
| 5 | Surveillance Identity | 56 bits | Squawk reply to ground |
| 11 | All-Call Reply | 56 bits | Initial acquisition |
| 16 | Long Air-Air (ACAS) | 112 bits | Extended TCAS |
| 17 | Extended Squitter (ADS-B) | 112 bits | Automatic broadcasts |
| 18 | Extended Squitter (TIS-B) | 112 bits | Ground relay |
| 20 | Comm-B Altitude | 112 bits | Altitude + data |
| 21 | Comm-B Identity | 112 bits | Squawk + data |
| 24 | Comm-D (ELM) | 112 bits | Extended messages |

## CRC and ICAO Address Extraction

### The Challenge

Mode S uses two different parity field encodings:

1. **PI Field** (DF 11, 17, 18): Pure CRC for error detection
2. **AP Field** (DF 0, 4, 5, 16, 20, 21, 24): CRC XORed with an address

**The Problem**: In AP field messages, the "address" might be:
- Aircraft ICAO (what we want)
- Interrogator ID (not useful for tracking)
- Corrupted data (dangerous)

### PI Field Validation (DF 11, 17, 18)

**Straightforward case**: Aircraft ICAO is in message body (AA field).

```
Validation: CRC(entire_message) must equal 0
```

**Why it works**: Transmitter encodes PI = CRC(data), so CRC(data + PI) = 0.

**See**: `crc.go:152-167`

### AP Field Extraction - The Hard Cases

#### DF 0/16: ACAS Surveillance

**The Problem**: These are aircraft-to-aircraft (TCAS) messages. The AP field contains:
```
AP = CRC(message) ⊕ Interrogator_ID
```

But the interrogator ID **looks exactly like an aircraft ICAO** - it's also 24 bits.

**Failed Approach**: Tried to find bit patterns distinguishing ICAOs from interrogator IDs. No reliable patterns exist.

**Current Solution**: Frame count heuristics
- Real aircraft transmit DF0/16 repeatedly over time
- Interrogator IDs appear random and infrequent
- Only accept ICAO if we've seen ≥3 frames with it

**Why 3 frames**: Balances false positive rejection vs. latency to first position.

**See**: `crc.go:169-194`

#### DF 4/5: Surveillance Replies

**The Problem**: These are responses to ground interrogations. AP field encoding depends on interrogation type:
- **Broadcast interrogation** (no specific interrogator): `AP = CRC ⊕ Aircraft_ICAO`
- **Selective interrogation** (targeted): `AP = CRC ⊕ Interrogator_ID`

**The Solution**: Use UM (Utility Message) field

```go
if UM == 0:
    // Broadcast interrogation → AP contains aircraft ICAO
    // But still require one previous message to avoid tiny outliers
else:
    // Selective interrogation → fall back to frame count threshold
```

**Why this works**:
- UM=0 is highly reliable (~99.9% accurate)
- The "one previous message" requirement catches the 0.1% edge cases
- Selective interrogations still filtered by frame count

**See**: `crc.go:196-243`

#### DF 20/21: Comm-B Messages

**The Problem**: These contain rich aircraft data (BDS registers) not available in ADS-B:
- Selected altitude (what autopilot is targeting)
- Meteorological data
- Aircraft intent

But the AP field is unreliable for ICAO extraction.

**The Solution**: Only accept frames from aircraft we've already seen via reliable messages (DF 11/17/18).

**Why it works**: Once we know an ICAO from a PI field message, we can trust subsequent AP field messages from that ICAO.

**Trade-off**: We miss Comm-B data from aircraft only seen via DF20/21, but this is rare - most aircraft transmit ADS-B.

**See**: `crc.go:245-267`

### CRC-24 Implementation

Mode S uses a 24-bit CRC with generator polynomial `0xFFF409`.

**Optimization**: Table-based lookup
- Precompute CRC for all 256 possible byte values
- Process message one byte at a time via table lookups
- ~8x faster than bit-by-bit polynomial division

**See**: `crc.go:36-95`

## Known Edge Cases & Production Evolution

> **Note to maintainers**: This section documents known challenges and evolution of the ICAO extraction logic. If you encounter specific failure modes, weird frames, or edge cases in production, please add them here with examples. The git history won't capture the "why" - this section should.

### The AP Field ICAO Extraction Journey

**The Core Challenge**: Extracting reliable ICAO addresses from AP field messages (DF 0, 4, 5, 16, 20, 21, 24) has been iteratively refined based on production experience.

**What makes it hard**:
- Interrogator IDs are 24-bit addresses that look identical to aircraft ICAOs
- CRC can pass validation even when the extracted "ICAO" is actually an interrogator ID
- No bit-level patterns reliably distinguish ICAOs from interrogator IDs
- Different DF types have different reliability characteristics

### Current Validation Strategy (As of 2024)

The code now uses **multiple defense layers** based on DF type:

#### DF 4/5 with UM=0: High Confidence
```go
if um == 0 {
    // Broadcast interrogation - AP contains aircraft ICAO
    // Require ONE previous message as safety net
}
```

**Why UM=0 is reliable**: ~99.9% correlation with valid aircraft ICAO in practice

**Why still require one previous message**: Edge case safety - one known outlier case in production sample data showed UM=0 with invalid ICAO. This guard catches that 0.1%.

**Trade-off**: Slightly higher latency to first valid ICAO (need 2 frames instead of 1) but prevents false positives.

#### DF 0/16: Require Frame Count Threshold
```go
if !hasSufficientExistingMessages(icao) {
    return ErrUnreliableICAOInsufficientFrameCount
}
```

**Why count-based**: These are ACAS (TCAS) messages. No reliable field distinguishes ICAOs from interrogator IDs.

**Current threshold**: 3 frames (configurable)

**Rationale**: Real aircraft transmit repeatedly, interrogator IDs appear sporadically

#### DF 20/21: Only Accept Known Aircraft
```go
if !hasAnyExistingMessages(icao) {
    return ErrUnreliableICAONotPreviouslySeen
}
```

**Why strict**: Comm-B data is valuable (selected altitude, met data) but AP field is unreliable for initial ICAO determination

**Assumption**: Aircraft already seen via DF11/17/18 (reliable PI field messages)

**Trade-off**: Miss Comm-B data from aircraft only transmitting DF20/21 (rare in practice)

### Known Failure Modes (Add Your Observations Here)

**Failed approach: Bit pattern analysis**
- Attempted to find bit patterns in the extracted ICAO that distinguish aircraft from interrogator IDs
- Result: No reliable patterns found
- Why: Both are arbitrary 24-bit addresses with similar distributions

**Observable behavior: Interrogator ID "aircraft"**
- Symptom: ICAO appears, transmits 1-2 frames, disappears forever
- Cause: DF0/4/5/16 frame where extracted ICAO was actually interrogator ID
- Solution: Frame count thresholding catches this

**Observable behavior: Valid ICAO rejected**
- Symptom: Aircraft missing positions despite good RF
- Cause: Only receiving DF4/5 with UM≠0, never reaching threshold
- Mitigation: Ensure ADS-B (DF17) reception, or lower threshold for sparse coverage areas

<!--
Maintainers: Add specific examples here. Format:
**[Date/Version] Issue description**
- Symptoms:
- Root cause:
- Frame example (hex):
- Solution/mitigation:
-->

## Production Lessons & Debugging

> **Note to maintainers**: Real-world issues that aren't obvious from specs or code. Add your war stories here.

### Common Pitfalls

#### Pitfall: Trusting CRC Alone

**Wrong assumption**: If CRC passes, the frame is valid and ICAO is correct

**Reality**: CRC validates message integrity, not ICAO authenticity

**Example scenario**:
```
Frame: DF4 (Surveillance Altitude Reply)
CRC: Valid ✓
Extracted ICAO: 0xABC123
Problem: 0xABC123 is interrogator ID, not aircraft
```

**Defense**: Multi-layer validation (CRC + UM field + frame count + ICAO filter)

**See**: `crc.go:152-298` for validation pipeline

#### Pitfall: Assuming All DF17 Have Positions

**Wrong assumption**: DF17 = ADS-B = position data

**Reality**: DF17 is just the transport; Type Code determines content

**Common mistake**:
```go
if frame.DF == 17 {
    lat, lon := frame.Lat(), frame.Lon() // May be 0,0!
}
```

**Correct approach**:
```go
if frame.DF == 17 && frame.MessageType >= 9 && frame.MessageType <= 18 {
    // Now safe to extract position
}
```

**See**: `decode-adsb.go:32-314` for TC-specific handling

#### Pitfall: Ignoring Frame History for CPR

**Wrong assumption**: Each position message contains full lat/lon

**Reality**: Need to pair ODD and EVEN frames (CPR decoding)

**Failure mode**: Trying to decode position from single frame yields garbage coordinates

**Solution**: Store frame history per aircraft, pair within time window

**See**: `../cpr.go` for pairing logic

### Debugging Frame Rejection Issues

#### Symptom: Aircraft Not Appearing Despite RF Signal

**Check list**:

1. **CRC validation failing?**
   ```
   Error: ErrInvalidChecksum
   → Check RF quality, signal strength, interference
   → Try different antenna/location
   ```

2. **ICAO filtering rejecting?**
   ```
   Error: ErrUnreliableICAO*
   → Check which DF types being received
   → If only DF0/4/5/16, may need to lower threshold
   → Verify ADS-B (DF17) reception
   ```

3. **Frame count below viability threshold?**
   ```
   Frames received but no events emitted
   → Check tracker.numFramesToBeViable setting
   → Check purgedBeforeViable metric
   → May be edge-of-coverage intermittent reception
   ```

4. **Enable trace logging**:
   ```
   Set log level to TRACE
   → See every CRC validation attempt
   → See ICAO extraction and filtering decisions
   → See frame count updates
   ```

#### Symptom: Positions Jumping or Invalid

**Check list**:

1. **CPR decoding failure?**
   ```
   → Check ODD/EVEN frame pairing
   → Verify time delta between frames <60 seconds
   → Check for velocity sanity (distance/time reasonable)
   ```

2. **Altitude encoding issues?**
   ```
   → Check Q-bit and M-bit values
   → Gillham vs. 25-foot encoding
   → Validate altitude in reasonable range
   ```

3. **Reference position for surface messages?**
   ```
   → Surface positions (TC 5-8) need receiver lat/lon
   → Without reference, positions will be wrong
   ```

#### Symptom: Missing Callsign or Other Data

**Check list**:

1. **Callsign validation rejecting?**
   ```
   → Check for invalid characters (see TC 1-4 validation)
   → Check for patterns like "@@@@@@@@"
   → Some aircraft never transmit callsign (not required)
   ```

2. **BDS register inference failing?**
   ```
   → DF20/21 Comm-B data requires inference
   → May not match any known pattern
   → Log ErrUnknownCommBMessage to see prevalence
   ```

### When Decoding Logic Seems Wrong

Mode S is **simultaneously well-defined and poorly-defined**:

**Well-defined**:
- CRC polynomial and calculation
- DF field encoding
- Basic message structure
- ADS-B Type Codes (mostly)

**Poorly-defined or ambiguous**:
- AP field interpretation (our main challenge)
- Comm-B BDS register types (must infer)
- Error handling for malformed messages
- Edge cases in altitude encoding
- Validity windows for data

**When to trust the spec vs. empirical testing**:
- Spec is authoritative for CRC, basic framing, TC definitions
- Empirical testing needed for AP field filtering, BDS inference, edge cases
- Production data reveals corner cases specs don't cover

**Decision framework**:
1. Does spec clearly define this? → Follow spec
2. Is spec ambiguous or silent? → Empirical testing + conservative approach
3. Does production show different behavior? → Trust production, file spec deviation note

<!--
Maintainers: Add your own "spec vs reality" observations here:
- Issue:
- What spec says (or doesn't say):
- What we observed:
- Our solution:
-->

### Performance Issues Observed

**CPU hotspots** (based on profiling):
1. CRC calculation (mitigated with table lookups)
2. Gillham altitude decoding (complex but rare)
3. String allocations in debugging/logging

**Memory issues**:
1. Frame history accumulation (use lossy circular buffers)
2. String conversions for hex display (lazy computation)

**Concurrency issues**:
1. Lock contention at high message rates (use RW locks)
2. GC pressure from allocations (use sync.Pool for hot paths)

<!--
Maintainers: Add specific performance issues you've found:
- Symptom:
- Profiling results:
- Solution:
- Performance delta:
-->

## ADS-B Message Types (DF 17/18)

ADS-B messages are identified by a Type Code (TC) in bits 33-37:

### Aircraft Identification (TC 1-4)

**Purpose**: Transmit flight number/callsign

**Encoding**: 8 characters from a 64-character alphabet (6 bits each)

**Character Set**: `@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\]^_ !"#$%&'()*+,-./0123456789:;<=>?`

**Why @ and special chars**: Legacy compatibility. `@` represents "no character", common in pre-departure aircraft.

**Validation**: Reject callsigns with invalid characters or patterns like `@@@@@@@@` (misconfigured transponders).

**See**: `decode.go:555-603`

### Surface Position (TC 5-8)

**Purpose**: Aircraft on ground reporting position and movement

**Requires**: Reference lat/lon (receiver location) - surface positions are relative

**Ground Speed Encoding**: Non-linear scale
- 0: Stationary
- 1-8: 0.125 kt steps (0-1 kt)
- 9-12: 0.25 kt steps (1-2 kt)
- 13-38: 0.5 kt steps (2-15 kt)
- 39-93: 1 kt steps (15-70 kt)
- 94-108: 2 kt steps (70-100 kt)
- 109-123: 5 kt steps (100-175 kt)
- 124: >175 kt

**Why non-linear**: Higher precision at taxi speeds, coarser at high speeds.

**See**: `decode-adsb.go:423-463`

### Airborne Position (TC 9-18, 20-22)

**Purpose**: In-flight position with altitude

**Types**:
- TC 9-18: Barometric altitude
- TC 20-22: GNSS altitude (GPS)

**Altitude Encoding**:
- **Q-bit set** (most common): 25-foot increments, -1000 to 50,175 feet
- **Q-bit clear**: Gillham code (100-foot increments)

**Gillham Code**: Legacy altitude encoding from Mode C
- Non-binary Gray code variant
- Prevents brief invalid altitudes during transitions
- Requires complex decoding table

**Why Gillham still exists**: Backward compatibility with older transponders.

**See**: `common.go:6-142` (Gillham decoding)

### Airborne Velocity (TC 19)

**Sub-types**:
1. **Ground Speed** (Sub 1-2): East/West and North/South components
2. **Airspeed** (Sub 3-4): Heading and true/indicated airspeed

**Ground Speed Decoding**:
```
velocity = sqrt(EW_velocity² + NS_velocity²)
heading = atan2(EW_velocity, NS_velocity)
```

**Supersonic**: Sub-types 2 and 4 use 4 kt resolution (vs. 1 kt for sub-sonic)

**Vertical Rate**: Encoded separately, 64 fpm resolution

**Why two sub-types**: Ground speed available from GPS, airspeed from pitot-static system. Aircraft transmit whichever is available/more accurate.

**See**: `decode-adsb.go:80-161`

### Emergency/Priority Status (TC 28)

**Purpose**: Transmit emergency squawks and status

**Emergency Codes**:
- 0: No emergency
- 1: General emergency (7700)
- 2: Lifeguard/Medical
- 3: Minimum fuel
- 4: No communications (7600)
- 5: Unlawful interference (7500)
- 6: Downed aircraft
- 7: Reserved

**Also contains**: Mode A squawk code

**See**: `decode-adsb.go:193-207`

### Operational Status (TC 31)

**Purpose**: Aircraft capabilities and configuration

**Contains**:
- ADS-B version (v0, v1, v2)
- Navigation accuracy (NACp, NIC)
- Airframe size (for wake turbulence separation)
- System capabilities (TCAS, UAT receiver, etc.)

**Why it matters**: ATC uses this for:
- Determining separation standards
- Verifying equipment compliance
- Routing decisions

**See**: `decode-adsb.go:260-311`

## Comm-B Data Selector (BDS) Registers

**Background**: Comm-B messages (DF 20/21) contain 56 bits of aircraft data, but the format isn't specified in the message itself.

**The Challenge**: Must infer BDS register type from message content.

### BDS Register Types

| Register | Name | Data |
|----------|------|------|
| 1.0 | Data Link Capability | Transponder capabilities |
| 1.7 | GICB Capability | Available BDS registers |
| 2.0 | Aircraft Identification | Callsign |
| 3.0 | ACAS Resolution Advisory | TCAS alerts |
| 4.0 | Selected Vertical Intent | Target altitude |
| 4.4 | Meteorological | Wind, temperature |
| 5.0 | Track and Turn | Roll angle |
| 6.0 | Heading and Speed | Magnetic heading, IAS/TAS/Mach |

### Inference Strategy

**BDS 1.0**: Fixed pattern `0x10` in first byte, reserved bits zero

**BDS 1.7**: Bit 7 set, bits 29-56 all zero

**BDS 2.0**: BDS code `0x20`, callsign characters valid

**BDS 3.0**: BDS code `0x30`, threat type field in valid range

**Why inference is hard**: Multiple BDS types can have similar bit patterns. The code uses sequential checks, most specific first.

**Limitations**: Cannot reliably decode BDS 4.0-6.0 yet - patterns overlap too much.

**See**: `decode-bds.go:110-162`

## ICAO Address Filtering

**Problem**: Prevents false positives from interrogator IDs and corrupted frames.

**Solution**: Callback registration

```go
mode_s.RegisterICAOFilter(tracker.HasICAO)
mode_s.RegisterICAOMessageCounter(tracker.GetPlaneMessageCount)
```

**How it works**:
1. Tracker maintains set of known ICAOs (from reliable DF11/17/18 messages)
2. During DF0/4/5/16 decoding, CRC code queries tracker
3. Only accepts ICAO if sufficient messages seen

**Why callbacks**: Decouples Mode S decoding from tracker implementation. Could use different tracking backends.

**See**: `icao_filter.go`

## Frame Validation Pipeline

Every frame goes through validation:

```
1. Parse raw hex/binary → byte array
2. Decode Downlink Format (first 5 bits)
3. Verify message length matches DF type
4. CRC validation / ICAO extraction
5. DF-specific field decoding
6. Content validation (altitude in range, etc.)
```

**Early rejection**: Invalid frames are rejected at step 3-4, before expensive field decoding.

**Error handling**: Each step returns explicit errors for debugging:
- `ErrInvalidChecksum`: Corrupted frame
- `ErrUnreliableICAO*`: ICAO filtering rejection
- `ErrNoOp`: Empty/null frame (not an error)

## Performance Considerations

### Memory Allocation

**Problem**: High frame rates (>5000 msg/sec) create GC pressure if allocating per frame.

**Solution**: Frame objects are lightweight, fields decoded on-demand.

### CPU Hotspots

**CRC calculation**: Table-based lookup optimization

**Gillham decoding**: Complex but rare (most use Q-bit altitudes)

**CPR decoding**: Done at Plane level (not per-frame) to pair ODD/EVEN

## Common Pitfalls

### Assuming all DF17 messages have positions

**Wrong**: TC 1-4 are identification, TC 19 is velocity

**Right**: Check message type before assuming lat/lon fields exist

### Trusting ICAO from AP field messages immediately

**Wrong**: Could be interrogator ID or corrupted

**Right**: Use frame count thresholds and filtering

### Ignoring Gillham altitude encoding

**Wrong**: Assuming all altitudes are Q-bit encoded

**Right**: Check `acQ` bit, use `decodeAC12Field()` which handles both

## File Guide

| File | Purpose |
|------|---------|
| `frame.go` | Core Frame struct and getters |
| `decode.go` | Main decoding pipeline, DF routing |
| `decode-adsb.go` | ADS-B (DF17/18) message decoding |
| `decode-bds.go` | Comm-B BDS register inference |
| `crc.go` | CRC-24 calculation and ICAO extraction |
| `icao_filter.go` | ICAO filtering callbacks |
| `common.go` | Gillham code and altitude utilities |
| `describe.go` | Human-readable frame descriptions |

## See Also

- [Tracker](../README.md) - Core tracking engine using Mode S frames
- [Beast Format](../beast/README.md) - Binary Mode S container format
- [SBS1 Format](../sbs1/README.md) - Decoded Mode S in text format
- [Export](../../export/README.md) - PlaneLocation from decoded frames

## References

- ICAO Annex 10, Volume IV - Mode S specification
- DO-260B - ADS-B specification
- RTCA DO-181E - Mode S minimum operational performance
