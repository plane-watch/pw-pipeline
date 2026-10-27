// Package partition reads and writes raw archive partitions: one object per UTC
// minute holding every raw frame the network accepted in that minute, grouped by
// plane.watch map tile.
//
// An object is a valid zstd stream: one skippable frame holding the index, then
// one ordinary zstd frame per tile that has records, in index order.
//
//	skippable frame   magic 0x184D2A50 (u32 LE), length (u32 LE), then:
//	                    header      (64 bytes, little-endian, see Header)
//	                    tile table  (20 bytes per tile present, see TileEntry)
//	zstd frame        records of the first tile in the table
//	zstd frame        records of the second tile
//	...
//
// zstd decoders skip skippable frames and output concatenated frames one after
// another, so `zstd -d` on a whole object yields every record of every tile. A
// reader that wants one tile fetches the first IndexFetchLen bytes, finds the tile
// in the table, fetches that frame by offset and length and decompresses it alone.
// An empty partition is the index with no tile frames.
//
// Each tile's frame decompresses to its records, in arrival order, each
//
//	i32  ms since partition start, little-endian
//	u8   source: SourceFeeder or SourceMLAT
//	u8   copies merged into this record by de-duplication, 1..255
//	u8   length: feeder 7 or 14; mlat 1..255
//	...  feeder: the Mode S message bytes as received, parity included
//	     mlat:   the SBS line as mlat-server wrote it, no line ending
package partition

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
)

const (
	// Magic opens the header.
	Magic = "PWRF"
	// Version is the format version written in the header.
	Version uint16 = 1

	// FlagIncomplete marks a partition during which the archiver's own subscription
	// dropped messages, or an ingest instance reported drops or missed an interval.
	FlagIncomplete uint16 = 1
	// FlagRestarted marks the partition the archiver started in: frames from before
	// its start are missing.
	FlagRestarted uint16 = 2

	SourceFeeder uint8 = 0
	SourceMLAT   uint8 = 1

	// NominalLengthMs is a partition's length.
	NominalLengthMs uint32 = 60_000

	// HeaderLen is the fixed header size.
	HeaderLen = 64
	// TileEntryLen is the size of one tile table entry.
	TileEntryLen = 20
	// skippableHeaderLen is the zstd skippable frame's own magic and length.
	skippableHeaderLen = 8
	// IndexFetchLen is enough bytes from the start of an object to hold the index
	// for any grid of up to 96 tiles. ReadIndex says how much more a larger one needs.
	IndexFetchLen = 2048

	// MaxMLATLine is the longest MLAT line a record can carry.
	MaxMLATLine = 255

	recordHeaderLen = 7
	skippableMagic  = 0x184D2A50
	// Prefix is where partitions live in the bucket; the format version is part of it.
	Prefix = "raw/v1/"
)

var (
	ErrShort      = errors.New("partition: truncated")
	ErrBadMagic   = errors.New("partition: bad magic")
	ErrBadVersion = errors.New("partition: unsupported version")
	ErrBadRecord  = errors.New("partition: bad record")
)

type (
	// Header is the fixed part of the index. Build fills Records, FeederRecords,
	// MLATRecords and Tiles from the records it is given.
	Header struct {
		Flags         uint16
		Start         time.Time // partition start, a UTC minute
		LengthMs      uint32    // nominal, NominalLengthMs
		Records       uint32    // all tiles
		FeederRecords uint32
		MLATRecords   uint32
		Duplicates    uint32 // copies removed by de-duplication
		UnknownTile   uint32 // feeder records dropped for tile 255
		GridVersion   uint16 // tile grid version, see tiles.json
		Tiles         uint16 // tile table entries
		Build         uint32 // archiver build, short git hash as u32
		MLATNoPos     uint32 // MLAT lines skipped for having no usable position
		MLATMarker    uint32 // feeder Beast frames dropped for carrying the MLAT timestamp marker
		MLATTooLong   uint32 // MLAT lines skipped for being longer than MaxMLATLine
		Instances     uint16 // ingest instances heard during the partition
		LateBatches   uint16 // batches for an earlier, sealed partition, dropped
	}

	// TileEntry is one tile table row.
	TileEntry struct {
		Tile        uint16
		Flags       uint16 // reserved, 0
		Offset      uint32 // of the tile's zstd frame from the start of the object
		Length      uint32 // compressed
		Records     uint32
		MLATRecords uint32
	}

	// Record is one frame or MLAT line.
	Record struct {
		OffsetMs int32 // since partition start
		Source   uint8
		Copies   uint8
		Data     []byte
	}

	// Index is the decoded index of an object.
	Index struct {
		Header Header
		Tiles  []TileEntry
		// Size is the index's length in the object, skippable frame header included;
		// the first tile frame starts here.
		Size int
	}
)

// Key is the object key for the partition starting at t: raw/v1/YYYYMMDD/HHMM.zst.
func Key(t time.Time) string {
	t = t.UTC()
	return fmt.Sprintf("%s%04d%02d%02d/%02d%02d.zst", Prefix, t.Year(), int(t.Month()), t.Day(), t.Hour(), t.Minute())
}

var (
	encOnce sync.Once
	enc     *zstd.Encoder
	decOnce sync.Once
	dec     *zstd.Decoder
)

func encoder() *zstd.Encoder {
	encOnce.Do(func() {
		var err error
		enc, err = zstd.NewWriter(nil,
			zstd.WithEncoderLevel(zstd.SpeedBetterCompression),
			zstd.WithEncoderCRC(true),
			zstd.WithEncoderConcurrency(1),
		)
		if err != nil {
			panic(err)
		}
	})
	return enc
}

func decoder() *zstd.Decoder {
	decOnce.Do(func() {
		var err error
		dec, err = zstd.NewReader(nil, zstd.WithDecoderConcurrency(1))
		if err != nil {
			panic(err)
		}
	})
	return dec
}

// EncodeRecords serialises records as a tile frame's content, uncompressed.
func EncodeRecords(recs []Record) ([]byte, error) {
	n := 0
	for _, r := range recs {
		n += recordHeaderLen + len(r.Data)
	}
	out := make([]byte, 0, n)
	for i, r := range recs {
		if err := validRecord(r); err != nil {
			return nil, fmt.Errorf("record %d: %w", i, err)
		}
		out = binary.LittleEndian.AppendUint32(out, uint32(r.OffsetMs))
		out = append(out, r.Source, r.Copies, uint8(len(r.Data)))
		out = append(out, r.Data...)
	}
	return out, nil
}

func validRecord(r Record) error {
	switch r.Source {
	case SourceFeeder:
		if len(r.Data) != 7 && len(r.Data) != 14 {
			return fmt.Errorf("%w: feeder record of %d bytes", ErrBadRecord, len(r.Data))
		}
	case SourceMLAT:
		if len(r.Data) < 1 || len(r.Data) > MaxMLATLine {
			return fmt.Errorf("%w: mlat record of %d bytes", ErrBadRecord, len(r.Data))
		}
	default:
		return fmt.Errorf("%w: source %d", ErrBadRecord, r.Source)
	}
	if r.Copies == 0 {
		return fmt.Errorf("%w: copies 0", ErrBadRecord)
	}
	return nil
}

// ParseRecords parses a tile frame's decompressed content. Data aliases b.
func ParseRecords(b []byte) ([]Record, error) {
	var out []Record
	for len(b) > 0 {
		if len(b) < recordHeaderLen {
			return out, ErrShort
		}
		n := int(b[6])
		if len(b) < recordHeaderLen+n {
			return out, ErrShort
		}
		r := Record{
			OffsetMs: int32(binary.LittleEndian.Uint32(b[0:4])),
			Source:   b[4],
			Copies:   b[5],
			Data:     b[recordHeaderLen : recordHeaderLen+n],
		}
		if err := validRecord(r); err != nil {
			return out, err
		}
		out = append(out, r)
		b = b[recordHeaderLen+n:]
	}
	return out, nil
}

// Build writes a partition object. tiles maps tile number to its records in arrival
// order; tiles are compressed in parallel. Build sets h.Records, h.FeederRecords,
// h.MLATRecords, h.Tiles and h.LengthMs itself.
func Build(h Header, tiles map[uint8][]Record) ([]byte, error) {
	numbers := make([]int, 0, len(tiles))
	for t, recs := range tiles {
		if len(recs) > 0 {
			numbers = append(numbers, int(t))
		}
	}
	sort.Ints(numbers)

	type compressed struct {
		entry TileEntry
		frame []byte
		err   error
	}
	frames := make([]compressed, len(numbers))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, t := range numbers {
		wg.Add(1)
		go func(i int, t uint8) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			recs := tiles[t]
			raw, err := EncodeRecords(recs)
			if err != nil {
				frames[i].err = fmt.Errorf("tile %d: %w", t, err)
				return
			}
			var mlat uint32
			for _, r := range recs {
				if r.Source == SourceMLAT {
					mlat++
				}
			}
			frames[i].frame = encoder().EncodeAll(raw, nil)
			frames[i].entry = TileEntry{
				Tile:        uint16(t),
				Records:     uint32(len(recs)),
				MLATRecords: mlat,
			}
		}(i, uint8(t))
	}
	wg.Wait()

	indexLen := skippableHeaderLen + HeaderLen + TileEntryLen*len(numbers)
	total := indexLen
	for i := range frames {
		if frames[i].err != nil {
			return nil, frames[i].err
		}
		frames[i].entry.Offset = uint32(total)
		frames[i].entry.Length = uint32(len(frames[i].frame))
		total += len(frames[i].frame)
		h.Records += frames[i].entry.Records
		h.MLATRecords += frames[i].entry.MLATRecords
	}
	h.FeederRecords = h.Records - h.MLATRecords
	h.Tiles = uint16(len(numbers))
	h.LengthMs = NominalLengthMs

	out := make([]byte, 0, total)
	out = binary.LittleEndian.AppendUint32(out, skippableMagic)
	out = binary.LittleEndian.AppendUint32(out, uint32(indexLen-skippableHeaderLen))
	out = appendHeader(out, h)
	for i := range frames {
		out = appendTileEntry(out, frames[i].entry)
	}
	for i := range frames {
		out = append(out, frames[i].frame...)
	}
	return out, nil
}

func appendHeader(b []byte, h Header) []byte {
	b = append(b, Magic...)
	b = binary.LittleEndian.AppendUint16(b, Version)
	b = binary.LittleEndian.AppendUint16(b, h.Flags)
	b = binary.LittleEndian.AppendUint64(b, uint64(h.Start.UnixMilli()))
	b = binary.LittleEndian.AppendUint32(b, h.LengthMs)
	b = binary.LittleEndian.AppendUint32(b, h.Records)
	b = binary.LittleEndian.AppendUint32(b, h.FeederRecords)
	b = binary.LittleEndian.AppendUint32(b, h.MLATRecords)
	b = binary.LittleEndian.AppendUint32(b, h.Duplicates)
	b = binary.LittleEndian.AppendUint32(b, h.UnknownTile)
	b = binary.LittleEndian.AppendUint16(b, h.GridVersion)
	b = binary.LittleEndian.AppendUint16(b, h.Tiles)
	b = binary.LittleEndian.AppendUint32(b, h.Build)
	b = binary.LittleEndian.AppendUint32(b, h.MLATNoPos)
	b = binary.LittleEndian.AppendUint32(b, h.MLATMarker)
	b = binary.LittleEndian.AppendUint32(b, h.MLATTooLong)
	b = binary.LittleEndian.AppendUint16(b, h.Instances)
	b = binary.LittleEndian.AppendUint16(b, h.LateBatches)
	return b
}

func parseHeader(b []byte) (Header, error) {
	var h Header
	if len(b) < HeaderLen {
		return h, ErrShort
	}
	if string(b[0:4]) != Magic {
		return h, ErrBadMagic
	}
	if v := binary.LittleEndian.Uint16(b[4:6]); v != Version {
		return h, fmt.Errorf("%w: %d", ErrBadVersion, v)
	}
	h.Flags = binary.LittleEndian.Uint16(b[6:8])
	h.Start = time.UnixMilli(int64(binary.LittleEndian.Uint64(b[8:16]))).UTC()
	h.LengthMs = binary.LittleEndian.Uint32(b[16:20])
	h.Records = binary.LittleEndian.Uint32(b[20:24])
	h.FeederRecords = binary.LittleEndian.Uint32(b[24:28])
	h.MLATRecords = binary.LittleEndian.Uint32(b[28:32])
	h.Duplicates = binary.LittleEndian.Uint32(b[32:36])
	h.UnknownTile = binary.LittleEndian.Uint32(b[36:40])
	h.GridVersion = binary.LittleEndian.Uint16(b[40:42])
	h.Tiles = binary.LittleEndian.Uint16(b[42:44])
	h.Build = binary.LittleEndian.Uint32(b[44:48])
	h.MLATNoPos = binary.LittleEndian.Uint32(b[48:52])
	h.MLATMarker = binary.LittleEndian.Uint32(b[52:56])
	h.MLATTooLong = binary.LittleEndian.Uint32(b[56:60])
	h.Instances = binary.LittleEndian.Uint16(b[60:62])
	h.LateBatches = binary.LittleEndian.Uint16(b[62:64])
	return h, nil
}

func appendTileEntry(b []byte, e TileEntry) []byte {
	b = binary.LittleEndian.AppendUint16(b, e.Tile)
	b = binary.LittleEndian.AppendUint16(b, e.Flags)
	b = binary.LittleEndian.AppendUint32(b, e.Offset)
	b = binary.LittleEndian.AppendUint32(b, e.Length)
	b = binary.LittleEndian.AppendUint32(b, e.Records)
	b = binary.LittleEndian.AppendUint32(b, e.MLATRecords)
	return b
}

func parseTileEntry(b []byte) TileEntry {
	return TileEntry{
		Tile:        binary.LittleEndian.Uint16(b[0:2]),
		Flags:       binary.LittleEndian.Uint16(b[2:4]),
		Offset:      binary.LittleEndian.Uint32(b[4:8]),
		Length:      binary.LittleEndian.Uint32(b[8:12]),
		Records:     binary.LittleEndian.Uint32(b[12:16]),
		MLATRecords: binary.LittleEndian.Uint32(b[16:20]),
	}
}

// IndexSize returns the index's length in bytes (skippable frame header included)
// from the first 8 bytes of an object, so a reader whose first fetch was too short
// knows how much to ask for.
func IndexSize(b []byte) (int, error) {
	if len(b) < skippableHeaderLen {
		return 0, ErrShort
	}
	if binary.LittleEndian.Uint32(b[0:4]) != skippableMagic {
		return 0, ErrBadMagic
	}
	return skippableHeaderLen + int(binary.LittleEndian.Uint32(b[4:8])), nil
}

// ReadIndex parses the index from the start of an object. b may be a prefix of the
// object; ErrShort means more is needed, see IndexSize.
func ReadIndex(b []byte) (Index, error) {
	var idx Index
	size, err := IndexSize(b)
	if err != nil {
		return idx, err
	}
	if len(b) < size {
		return idx, ErrShort
	}
	idx.Size = size
	body := b[skippableHeaderLen:size]
	idx.Header, err = parseHeader(body)
	if err != nil {
		return idx, err
	}
	body = body[HeaderLen:]
	if len(body) != TileEntryLen*int(idx.Header.Tiles) {
		return idx, fmt.Errorf("%w: tile table of %d bytes for %d tiles", ErrBadRecord, len(body), idx.Header.Tiles)
	}
	idx.Tiles = make([]TileEntry, idx.Header.Tiles)
	for i := range idx.Tiles {
		idx.Tiles[i] = parseTileEntry(body[i*TileEntryLen:])
	}
	return idx, nil
}

// Tile returns the table entry for tile t, if present.
func (idx Index) Tile(t uint8) (TileEntry, bool) {
	for _, e := range idx.Tiles {
		if e.Tile == uint16(t) {
			return e, true
		}
	}
	return TileEntry{}, false
}

// DecodeTile decompresses one tile's zstd frame, as fetched by the offset and length
// in its table entry, and parses its records.
func DecodeTile(frame []byte) ([]Record, error) {
	raw, err := decoder().DecodeAll(frame, nil)
	if err != nil {
		return nil, fmt.Errorf("partition: tile frame: %w", err)
	}
	return ParseRecords(raw)
}

// ReadAll parses a whole object: the index and every tile's records.
func ReadAll(obj []byte) (Index, map[uint8][]Record, error) {
	idx, err := ReadIndex(obj)
	if err != nil {
		return idx, nil, err
	}
	tiles := make(map[uint8][]Record, len(idx.Tiles))
	for _, e := range idx.Tiles {
		end := int(e.Offset) + int(e.Length)
		if end > len(obj) || e.Offset < uint32(idx.Size) {
			return idx, nil, fmt.Errorf("%w: tile %d frame at %d+%d in %d bytes", ErrShort, e.Tile, e.Offset, e.Length, len(obj))
		}
		recs, err := DecodeTile(obj[e.Offset:end])
		if err != nil {
			return idx, nil, fmt.Errorf("tile %d: %w", e.Tile, err)
		}
		if uint32(len(recs)) != e.Records {
			return idx, nil, fmt.Errorf("%w: tile %d has %d records, index says %d", ErrBadRecord, e.Tile, len(recs), e.Records)
		}
		tiles[uint8(e.Tile)] = recs
	}
	return idx, tiles, nil
}
