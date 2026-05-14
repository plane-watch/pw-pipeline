# MLAT Epoch Timestamps in Runway

## Overview

MLAT (Multilateration) timestamps represent the time elapsed since the Beast receiver powered on. Runway uses these timestamps to:

1. **Detect epoch boundaries** - When MLAT ticks reset (receiver restart) or timeout (stale feeder)
2. **Partition frames by sub-producer** - Each independent receiver gets its own epoch
3. **Store timing information** - The epoch value itself is the MLAT tick when it began

## MLAT Timestamp Format

**Source:** Beast protocol frames contain a 6-byte MLAT timestamp field

**Units:** 1/12 microsecond increments (per Beast specification)

**Conversion to nanoseconds:**
```
ticks * (1000 / 12) = nanoseconds
```

**Examples:**
- 12 ticks = 1 microsecond = 1,000 nanoseconds
- 1,000,000 ticks ≈ 83.3 milliseconds
- 12,000,000 ticks = 1 second = 1,000,000,000 nanoseconds

## Epoch Values as Identifiers

When the sticky feeder system partitions frames by receiver (sub-producer), it uses the MLAT tick value at which each epoch began as the identifier:

**Why MLAT ticks are good identifiers:**
1. **Natural timing reference** - The value directly represents receiver uptime
2. **Pseudo-random uniqueness** - Two receivers starting at identical tick values is ~impossible
3. **No need for external coordination** - Derived purely from frame data
4. **Monotonic** - MLAT ticks never decrease within an epoch

**Example scenario:**
- Receiver A powers on, starts sending frames with MLAT ticks ≈ 1,000,000
- Epoch A ID = 1,000,000
- Receiver A restarts, sends frames with MLAT ticks ≈ 500,000 (much smaller)
- This backwards jump > 5 seconds triggers a new epoch
- Epoch B ID = 500,000 (the tick where restart happened)
- Frames are now partitioned: some tagged with epoch 1,000,000, others with 500,000

## Sticky Feeder Integration

The sticky feeder uses composite keys internally:

```
compositeKey = feederTag + "#" + epochID
Example: "LEPP-2043#1000000"
```

This allows tracking quality metrics per sub-producer separately. When a new frame arrives:
1. Extract feeder tag (e.g., "LEPP-2043")
2. Extract MLAT ticks and detect epoch via EpochDetector
3. Create composite key with actual epoch value
4. Score and select best sub-producer per aircraft
5. Only advertise winning feeder's base tag externally (epoch ID stripped)

## Timing Reconstruction

With actual MLAT epoch values stored, downstream systems can reconstruct absolute wall-clock time:

1. **Start time:** Receiver power-on timestamp = some wall-clock time T0
2. **Frame time:** Frame's MLAT ticks = T_epoch_start + elapsed_since_epoch_start
3. **Absolute frame time:** T0 + (frame_mlat_ticks / 12_MHz) = absolute time

The epoch boundary value enables the first step of this reconstruction.

## Backward Compatibility

- Frames without epoch data (epochID=0) fall back to using just feederTag
- Existing metrics and APIs unchanged - only internal representation differs
- No changes needed to frame source code outside producer/sticky feeder layers
