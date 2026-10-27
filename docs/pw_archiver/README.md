# pw_archiver

## Overview

`pw_archiver` keeps the raw frames the network accepts. It subscribes to the
batches that `runway` and `pw_ingest` publish when run with
`--raw-archive-subject` (see [rawarchive](../rawarchive/README.md)), groups
every frame by UTC minute and map tile, merges copies that several ingest
instances heard, and writes one zstd object per minute to an S3 bucket
(Cloudflare R2 included). Nothing is kept on disk. Run exactly one archiver
per network.

```
runway / pw_ingest ──one batch per instance every 5 s──► NATS v1.pw-ingest.raw
                                                            │
                                                     pw_archiver (one per network)
                                                            │  file by minute and tile, de-duplicate, seal at T + 70 s
                                                            ▼
                                                     bucket: raw/v1/YYYYMMDD/HHMM.zst, meta/tiles.json
```

What is archived: the Mode S message bytes of every Beast and AVR frame the
ingest accepted (7 or 14 bytes, parity included), and plane.watch's own MLAT
results as the SBS lines the muxes publish. What is not: the receiver
timestamp and signal level of Beast frames, Mode A/C, frames the decoder
rejected, frames carrying readsb's MLAT marker (synthesised, not heard),
and anything that identifies a feeder. Records carry a tile, never a
feeder. See [Privacy](#privacy).

## Running

```
pw_archiver \
  --nats=nats://nats-ingest:4222 \
  --s3-endpoint=https://<account>.r2.cloudflarestorage.com \
  --s3-bucket=pw-raw-archive \
  --s3-access-key-id=... --s3-secret-access-key=... \
  daemon
```

| Flag | Env | Default | Meaning |
|---|---|---|---|
| `--nats` | `NATS` | | NATS URL the ingest instances publish on |
| `--subject` | `SUBJECT` | `v1.pw-ingest.raw` | the ingest `--raw-archive-subject` |
| `--batch-interval` | `BATCH_INTERVAL` | `5s` | the ingest `--raw-archive-interval`. Sets the seal time and how many batches an instance is expected to send per minute |
| `--seal-grace` | `SEAL_GRACE` | `5s` | added to the seal time for clock skew and lag |
| `--restart-delay` | `RESTART_DELAY` | `15s` | extra delay before writing the first, partial minute after a start |
| `--dedupe-window` | `DEDUPE_WINDOW` | `2s` | identical messages this close together are one record |
| `--tiles` | `TILES` | all | comma separated tiles to archive, e.g. `35,36` |
| `--queue-length` | `QUEUE_LENGTH` | 16384 | batches buffered between the NATS client and the archiver |
| `--s3-endpoint` | `S3_ENDPOINT` | | S3 endpoint |
| `--s3-region` | `S3_REGION` | `auto` | S3 region; `auto` for R2 |
| `--s3-bucket` | `S3_BUCKET` | | bucket; keep it for the archive alone |
| `--s3-access-key-id`, `--s3-secret-access-key` | `S3_ACCESS_KEY_ID`, `S3_SECRET_ACCESS_KEY` (or the `AWS_` names) | | a token that can write the bucket |
| `--s3-path-style` | `S3_PATH_STYLE` | off | bucket in the path (MinIO) instead of the host |
| `--first-day` | `FIRST_DAY` | | first day archived, written into `meta/tiles.json` when it is created |
| `--monitoring-port` | `MONITORING_PORT` | 9607 | Prometheus metrics at `/metrics`, health at `/status` |

`pw_archiver tiles` prints the `meta/tiles.json` this build would write.

Commands: `daemon` (JSON logs), `cli` (human readable), `tiles`.

### Order of switching it on

1. Create the bucket (R2: location hint Oceania or wherever the archiver
   runs), a lifecycle rule deleting `raw/` after the retention wanted (30 days
   in the proposal; `meta/` is outside it), and a token with object write
   on this bucket only.
2. Start the archiver. It writes `meta/tiles.json` if absent and begins
   writing an object every minute, empty until an instance publishes.
3. Set `--raw-archive-subject` on one instance (a mux `pw_ingest` first, then
   one `runway`), check the minutes fill, then on all of them.

Readers need object read: a public bucket on a custom domain, or a read-only
token. Cloudflare's cache needs a rule for `.zst` and `.json`, caching 200s
for a long time (objects never change) and never caching 404s, since a reader
polls for a minute that is not sealed yet.

## How a minute is built

1. **Subscribe** to the subject. Every message names its `instance`; the set
   heard is a gauge and goes in each partition's header.
2. **File by `start`.** A batch belongs to the minute its `start` header
   falls in; intervals divide the minute, so a batch is never split. Times and
   keys come from the ingest hosts' clocks. The archiver logs a warning when a
   batch's `start` is more than 10 s from its own clock.
3. **Each record**, at `start` plus its offset:
   - Beast: dropped and counted if the receiver timestamp is readsb's MLAT
     marker `FF 00 4D 4C 41 54`; otherwise only the Mode S bytes are kept.
     Tile from the record (the feeder's position, resolved inside the
     ingest). Tile 255, a feeder without a position, is dropped and counted.
   - AVR: the Mode S bytes as sent, tile as for Beast.
   - SBS: kept whole as an MLAT record. The tile is `tile_grid.LookupTile`
     of the line's own position, read from fields 14 and 15 by the archiver
     itself. A line with either field empty, unparsable, out of range or
     exactly 0,0 is skipped and counted, as is a line over 255 bytes.
   - With `--tiles`, records for other tiles are dropped (not counted in the
     header; a metric counts them).
4. **De-duplicate** feeder records across the network: identical message
   bytes within `--dedupe-window` of each other are one record, its `copies`
   field counting the instances that sent it. `runway` already merges copies
   from its own feeders over 9 s, so behind a single `runway` the count is
   almost always 1; it is a lower bound on receptions, not coverage. MLAT
   records are never de-duplicated.
5. **Seal** minute `T` at `T + 60 s + batch interval + grace` (`T + 70 s` by
   default). A batch for a sealed minute is counted in the current minute's
   header as late and its records dropped.
6. **Write** `raw/v1/YYYYMMDD/HHMM.zst` with `If-None-Match: *`, so an
   object is written once and never overwritten. A 412 (it exists) is logged
   and counted. A failed write is retried three times; then the minute is
   lost and logged.
7. **Every minute is written, even an empty one.** An absent key means "not
   written yet" or, once a few minutes have passed, lost: the archiver was
   down or could not write it. A tile missing from a written minute's index
   means quiet, not lost.
8. **On start**, the minute the archiver started in is flagged `restarted`
   (frames from before the start are gone) and written `--restart-delay`
   later than usual, so an archiver still running, as in a deploy that
   overlaps, writes its complete copy first. Minutes before the start are not
   written. Minutes still open at shutdown are not written either: they would
   be partial, and absent reads as lost.

### The incomplete flag

A minute is flagged `incomplete` when the archive may be missing frames the
network accepted:

- an instance reported `dropped` frames in a batch of that minute;
- an instance sent fewer batches than its interval implies (one per
  interval from the minute's start, or from its first batch if it started
  during the minute);
- an instance heard in the previous minute sent nothing in this one (it is
  then forgotten, so it is flagged once);
- the NATS client discarded batches because the archiver fell behind
  (`Subscription.Dropped`); every minute open at the time is flagged.

The flag says "there was loss"; the records present are still good.

## Object format, version 1

An object is a valid zstd stream: one skippable frame holding the index, then
one ordinary zstd frame per tile that has records, in index order.
`zstd -d` on a whole object gives every record of every tile. The Go reader
is `lib/rawarchive/partition` (`ReadIndex`, `DecodeTile`, `ReadAll`).

```
skippable frame   magic 0x184D2A50 (u32 LE), length (u32 LE), then:
                    header      (64 bytes, little-endian)
                    tile table  (20 bytes per tile present)
zstd frame        records of the first tile in the table
zstd frame        records of the second tile
...
```

A reader that wants one tile: Range GET `bytes=0-2047` (the index is at most
8 + 64 + 75 × 20 = 1,572 bytes for the current grid; the skippable frame's
length says if more is needed), find the tile, Range GET its `offset` and
`length`, decompress that frame. Its content checksum verifies it. An empty
minute is the index alone.

### Header

| Offset | Type | Field |
|---|---|---|
| 0 | 4 bytes | Magic `PWRF` |
| 4 | u16 | Format version, 1 |
| 6 | u16 | Flags: 1 `incomplete`, 2 `restarted` |
| 8 | i64 | Partition start, Unix ms UTC |
| 16 | u32 | Nominal length, ms (60000) |
| 20 | u32 | Record count, all tiles |
| 24 | u32 | Records from feeders |
| 28 | u32 | Records from MLAT |
| 32 | u32 | Duplicates removed |
| 36 | u32 | Feeder records dropped for tile 255 (source without a position) |
| 40 | u16 | Tile grid version (see `meta/tiles.json`) |
| 42 | u16 | Tile table entries, *n* |
| 44 | u32 | Archiver build, short git hash as u32 |
| 48 | u32 | MLAT lines skipped for having no usable position |
| 52 | u32 | Feeder Beast frames dropped for carrying the MLAT marker |
| 56 | u32 | MLAT lines skipped for being longer than 255 bytes |
| 60 | u16 | Ingest instances heard during the partition |
| 62 | u16 | Batches for an earlier, sealed partition, dropped as late |

### Tile table

*n* entries of 20 bytes, sorted by tile number, only for tiles with records:

| Offset | Type | Field |
|---|---|---|
| 0 | u16 | Tile number (`tile35` is 35) |
| 2 | u16 | Flags, reserved, 0 |
| 4 | u32 | Offset of the tile's zstd frame from the start of the object |
| 8 | u32 | Compressed length of the frame |
| 12 | u32 | Records in the tile |
| 16 | u32 | Of which MLAT |

### Record

Each tile's frame decompresses to its records, in arrival order, no header:

```
i32  ms since partition start (batch start plus record offset; signed for future use)
u8   source: 0 feeder, 1 mlat
u8   copies merged into this record by de-duplication, 1..255
u8   length: feeder 7 or 14; mlat 1..255
...  feeder: the Mode S message bytes as received, parity included
     mlat:   the SBS line as mlat-server wrote it, ASCII, no line ending
```

21 bytes for a 112-bit message, 14 for a 56-bit one, about 115 for an MLAT
line. Fixed-width fields, no varints: zstd removes the redundancy, and a
reader is a dozen lines in any language.

MLAT records are `MSG,3` lines from mlat-server: `MSG,3,1,1,<ICAO>,1,<rcv
date>,<rcv time>,<sent date>,<sent time>,<callsign>,<altitude>,<speed>,
<heading>,<lat>,<lon>,<vrate>,<squawk>,<receivers>,<error>,,`. The line's
own time is when the position was computed; the record's offset is when the
line reached the ingest, up to a few seconds later, so readers use the
line's time.

Positions: tiles are far larger than the 180 NM within which a single CPR
frame decodes unambiguously, so the format carries no reference point. A
reader decodes airborne positions globally from an even/odd pair, then
locally against the aircraft's last position. Surface positions also need
the right 90° quadrant; the tile's `centre` in `meta/tiles.json` gives one.

## Bucket layout

```
raw/v1/<YYYYMMDD>/<HHMM>.zst    partition, all tiles, HHMM = start minute, UTC
meta/tiles.json                 tile grid: version, tiles, cell map; first day archived
```

Keys come from the time, so a reader fetches a known key and never lists the
bucket. Normally a minute appears at about `T + 70 s`, the first after a
restart about 15 s later. `v1` is the format version; a format change gets
a new prefix. A change to the tile grid is not a format change: it gets a
new grid version in the header and in `tiles.json`.

`meta/tiles.json` holds, per grid version, the tiles (number, name, bounds
as written in `lib/tile_grid`, a reference point inside the tile) and
`cells`: 180 rows from 90°S north, each the 360 one-degree cells from 180°W
east, run-length encoded `"<tile>x<count>,…"`. A point at (lat, lon) is in
the tile of cell `[floor(lat) + 90][floor(lon) + 180]`. The cells are what
the archiver (and the tracker) actually apply, generated from `LookupTile`;
readers should use them, not the bounds, to decide which tiles cover an
area. `LookupTile` reads a one-degree grid filled from each cell's
south-west corner, so a tile's southernmost one-degree row belongs to the
tile below it; the cell map includes that.

The archiver writes `meta/tiles.json` on start if it is absent. A build with
a new grid version should append its grid to the existing file by hand
before it is deployed.

## Privacy

Nothing in the archive identifies or locates a feeder:

- The feeder tag and API key never leave the ingest process; the batcher
  sends a tile, resolved inside the ingest, and the archiver never requests
  the feeder list.
- The receiver timestamp and signal level are stripped from Beast frames.
  Without them, and without a receiver identity, the archive cannot be used
  for MLAT or to locate a receiver.
- A tile is hundreds of miles across. Tiles with very few feeders can be left
  out with `--tiles`.

## Metrics

Namespace `pw_archiver`:

| Metric | Meaning |
|---|---|
| `batches_total`, `batches_bad_total`, `batches_late_total`, `batches_with_drops_total` | batches filed, unparseable, for a sealed minute, reporting instance drops |
| `records_total{type}` | records filed, by `beast`, `avr`, `sbs1` |
| `records_dropped_total{reason}` | `unknown_tile`, `mlat_marker`, `no_position`, `too_long`, `bad_length`, `filtered_tile`, `late`, `unknown_type` |
| `duplicates_total` | copies merged |
| `partitions_total{result}` | `written`, `exists`, `failed` |
| `partitions_incomplete_total`, `partition_bytes_total` | |
| `instances`, `open_minutes`, `dedupe_entries` | gauges |
| `subscription_dropped_total` | batches the NATS client discarded (slow consumer) |

`/status` is unhealthy while NATS is disconnected, or for five minutes after
a partition could not be written.

## Load and cost

One write a minute whatever the number of tiles: 1,440 a day, 43,200 a
month. Reads cost whoever reads. Storage is what grows with the network's
frame rate: as an illustration, 10,000 de-duplicated frames a second is
about 9 MB of records a minute, roughly 4.5 MB compressed, 6.5 GB a day.
`--tiles` and the lifecycle rule scale it down directly.

The archiver holds about 70 s of records in memory plus the de-duplication
window, and one message every interval per instance crosses NATS whether or
not it is running (the batcher's flag, not the archiver, controls that).

## See Also

- [rawarchive](../rawarchive/README.md): the batch format the archiver consumes
- [middleware](../middleware/README.md#raw-archive-middleware): the batcher and its flags
- [tile_grid](../tile_grid/README.md): tile numbering
- `lib/rawarchive/partition`: the object format, writer and reader
