# Raw Archive Batches

## Overview

The `rawarchive` package defines the message format the **Raw Archive**
middleware (`lib/middleware/raw_archive.go`) publishes: one NATS message per
ingest instance per interval, carrying every raw frame the instance accepted
during that interval. A consumer, such as an archiver that writes minute
files to object storage, subscribes to the subject and decodes the batches
with this package.

The middleware is off by default. It is enabled per process with
`--raw-archive-subject` on `runway` and `pw_ingest` (see
[middleware](../middleware/README.md#raw-archive-middleware)).

## Why Batches

The ingest tap publishes one NATS message per frame, which is fine for a
debugging session on one feeder and wrong for a permanent, network-wide
consumer. Batching bounds the message rate by the number of ingest
processes, not the frame rate: ten processes at the default 5 s interval
put two messages a second on the bus whatever the traffic. The bytes are the
frame bytes plus six per record.

## Message

**Subject**: whatever the flag names. The proposal for the archive uses
`v1.pw-ingest.raw`.

**Headers**:

| Header | Meaning |
|---|---|
| `instance` | Name of the publishing process: hostname plus a random suffix chosen at start, so a restart shows as a new instance. Settable with `--raw-archive-instance` |
| `start` | Interval start, Unix milliseconds UTC, always on an interval boundary |
| `dropped` | Frames this instance lost since its previous message: dropped from its queue (the batcher never blocks the live path), or carried by a batch that failed to publish |
| `v` | Format version, `1` |

**Body**: records back to back, no body header.

| Bytes | Field |
|---|---|
| 1 | Type: 1 Beast, 2 AVR, 3 SBS1 |
| 1 | Tile number from `lib/tile_grid` (`tile35` is 35); 255 when unknown |
| 2 | Offset from `start` when the frame reached the middleware, ms, big-endian |
| 2 | Length *n*, big-endian |
| *n* | The frame as `Frame.Raw()` returns it |

What `Raw()` returns by type:

- **Beast**: the whole frame as the producer emitted it, escapes removed:
  `0x1a`, type byte, 6-byte receiver timestamp, signal level, 7 or 14 Mode S
  bytes. At most 23 bytes. Receiver timestamp and signal level are
  included; a consumer that must not keep them strips them.
- **AVR**: the Mode S bytes (`mode_s.Frame.Raw` returns the decoded
  message bytes once decoded, which they are by the time middleware runs).
- **SBS1**: the line as received, without its line ending.

**An empty body is a heartbeat**: the instance was alive and heard nothing
in the interval.

## Intervals

The interval (`--raw-archive-interval`, default `5s`) must be a whole number
of milliseconds that divides one minute. Intervals are aligned to the
clock: each starts on a whole multiple of the interval past the UTC minute
boundary, so **no batch straddles a minute** and `start` alone says which
minute every record belongs to. A consumer building per-minute output
partitions by `start` and never needs its own receive time.

A frame that reaches the middleware after the open interval has ended
closes that interval first, so a quiet period skips intervals rather than
stretching one; the heartbeat for an empty interval comes from the timer.

Record offsets are milliseconds from `start` by the ingest host's clock.
Ingest hosts should run NTP, as they already do for the live feed.

## Size Flush

If the body reaches `--raw-archive-max-bytes` (default 512 KiB) before the
interval ends, the batch is published at once with the same `start`, and
the rest of the interval continues in a new body. The interval is therefore
an upper bound on how long frames wait, and the NATS payload limit (1 MB by
default) cannot be reached at any frame rate. A consumer must expect
several messages with the same `start` from one instance, and a trailing
heartbeat with that `start` after a size flush.

## Tiles

Beast and AVR records carry the tile of the **feeder's reference position**
(`FrameSource.RefLat/RefLon`, which `runway` sets from the feeder record),
looked up with `tile_grid.LookupTile`, the same function the tracker uses
for aircraft. A source without a position, or whose position is off the
grid, gives 255. SBS1 records always carry 255: the line carries the
aircraft's own position, and the consumer tiles it from that.

Nothing else about the feeder is sent. The feeder tag and API key stay
inside the ingest process.

## Loss

Messaging is core NATS: nothing is retained. A consumer that is down, or
that falls behind (`nats.go` counts those in `Subscription.Dropped`), misses
batches, and a batch lost in flight loses up to one interval of one
instance's frames. Instances also drop frames when their own queue is full,
and report the count in the next `dropped` header. A consumer can therefore
tell a complete interval from an incomplete one, and a quiet instance from
a dead one (no heartbeat for a few intervals).

Publishing with no subscriber is not free: the client still sends each
message to the server, which discards it. With the flag set and no consumer
running, each instance spends one small message per interval.

## Using the Package

```go
import "plane.watch/lib/rawarchive"

func handle(m *nats.Msg) {
    hdr, err := rawarchive.ParseHeader(m.Header)
    if err != nil {
        return // not a batch, or a newer version
    }
    recs, err := rawarchive.Decode(m.Data)  // records alias m.Data
    for _, r := range recs {
        at := hdr.Start.Add(time.Duration(r.Offset) * time.Millisecond)
        switch r.Type {
        case rawarchive.TypeBeast:
            // r.Data is the whole Beast frame; Mode S bytes are r.Data[9:]
        case rawarchive.TypeSBS1:
            // r.Data is the SBS line
        }
        _ = at
    }
    if len(recs) == 0 {
        // heartbeat from hdr.Instance
    }
}
```

`Encoder` is the writing side the middleware uses; `ValidInterval` and
`IntervalStart` implement the alignment rules above, for consumers that
want to check them.

## File Guide

- `format.go`: constants, `Encoder`, `Decode`, `SetHeader`, `ParseHeader`,
  `TileNumber`, `ValidInterval`, `IntervalStart`
- `format_test.go`: round trips, truncation, header errors, alignment

## See Also

- [middleware](../middleware/README.md): the Raw Archive middleware and its flags
- [pw_archiver](../pw_archiver/README.md): the consumer, and the partition objects it writes
- [tile_grid](../tile_grid/README.md): tile numbering
- [nats_io](../nats_io/README.md): `PublishMsg`
