package partition

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

var (
	msgLong  = []byte{0x8d, 0x7c, 0x49, 0xf8, 0x58, 0x41, 0xd2, 0x6c, 0xca, 0x39, 0x33, 0xe4, 0x1e, 0xcf}
	msgShort = []byte{0x5d, 0x7c, 0x49, 0xf8, 0x28, 0xe9, 0x43}
	mlatLine = []byte("MSG,3,1,1,7C6DB8,1,2026/10/25,01:02:03.456,2026/10/25,01:02:04.000,,3400,,,-31.950000,115.860000,,,,,,")
	t0       = time.Date(2026, 10, 24, 6, 30, 0, 0, time.UTC)
)

func sample() (Header, map[uint8][]Record) {
	h := Header{
		Flags:       FlagRestarted,
		Start:       t0,
		Duplicates:  7,
		UnknownTile: 2,
		GridVersion: 1,
		Build:       0xabcdef01,
		MLATNoPos:   1,
		MLATMarker:  3,
		MLATTooLong: 0,
		Instances:   4,
		LateBatches: 1,
	}
	tiles := map[uint8][]Record{
		35: {
			{OffsetMs: 0, Source: SourceFeeder, Copies: 1, Data: msgLong},
			{OffsetMs: 1500, Source: SourceFeeder, Copies: 3, Data: msgShort},
			{OffsetMs: 59999, Source: SourceMLAT, Copies: 1, Data: mlatLine},
		},
		0:  {{OffsetMs: 10, Source: SourceFeeder, Copies: 1, Data: msgLong}},
		74: {{OffsetMs: 20, Source: SourceMLAT, Copies: 1, Data: mlatLine}},
		12: nil, // no records: not in the table
	}
	return h, tiles
}

func TestKey(t *testing.T) {
	if got := Key(t0); got != "raw/v1/20261024/0630.zst" {
		t.Errorf("Key = %q", got)
	}
	if got := Key(t0.In(time.FixedZone("x", 8*3600))); got != "raw/v1/20261024/0630.zst" {
		t.Errorf("Key in another zone = %q", got)
	}
}

func TestRoundTrip(t *testing.T) {
	h, tiles := sample()
	obj, err := Build(h, tiles)
	if err != nil {
		t.Fatal(err)
	}
	idx, got, err := ReadAll(obj)
	if err != nil {
		t.Fatal(err)
	}
	hdr := idx.Header
	if hdr.Flags != FlagRestarted || !hdr.Start.Equal(t0) || hdr.LengthMs != NominalLengthMs {
		t.Errorf("header basics: %+v", hdr)
	}
	if hdr.Records != 5 || hdr.FeederRecords != 3 || hdr.MLATRecords != 2 || hdr.Tiles != 3 {
		t.Errorf("counts: %+v", hdr)
	}
	if hdr.Duplicates != 7 || hdr.UnknownTile != 2 || hdr.GridVersion != 1 || hdr.Build != 0xabcdef01 ||
		hdr.MLATNoPos != 1 || hdr.MLATMarker != 3 || hdr.Instances != 4 || hdr.LateBatches != 1 {
		t.Errorf("counters: %+v", hdr)
	}
	if len(idx.Tiles) != 3 || idx.Tiles[0].Tile != 0 || idx.Tiles[1].Tile != 35 || idx.Tiles[2].Tile != 74 {
		t.Fatalf("tile table order: %+v", idx.Tiles)
	}
	e35, ok := idx.Tile(35)
	if !ok || e35.Records != 3 || e35.MLATRecords != 1 {
		t.Errorf("tile35 entry: %+v %v", e35, ok)
	}
	if _, ok = idx.Tile(12); ok {
		t.Error("empty tile 12 is in the table")
	}
	// Frames are contiguous after the index.
	want := uint32(idx.Size)
	for _, e := range idx.Tiles {
		if e.Offset != want {
			t.Errorf("tile %d at %d, want %d", e.Tile, e.Offset, want)
		}
		want += e.Length
	}
	if int(want) != len(obj) {
		t.Errorf("frames end at %d, object is %d bytes", want, len(obj))
	}
	for tile, recs := range got {
		exp := tiles[tile]
		if len(recs) != len(exp) {
			t.Fatalf("tile %d: %d records, want %d", tile, len(recs), len(exp))
		}
		for i := range recs {
			if recs[i].OffsetMs != exp[i].OffsetMs || recs[i].Source != exp[i].Source ||
				recs[i].Copies != exp[i].Copies || !bytes.Equal(recs[i].Data, exp[i].Data) {
				t.Errorf("tile %d record %d: %+v want %+v", tile, i, recs[i], exp[i])
			}
		}
	}
}

func TestRangeRead(t *testing.T) {
	h, tiles := sample()
	obj, err := Build(h, tiles)
	if err != nil {
		t.Fatal(err)
	}
	// A reader fetches the first IndexFetchLen bytes (or the whole object if shorter).
	head := obj
	if len(head) > IndexFetchLen {
		head = head[:IndexFetchLen]
	}
	idx, err := ReadIndex(head)
	if err != nil {
		t.Fatal(err)
	}
	e, ok := idx.Tile(35)
	if !ok {
		t.Fatal("no tile35")
	}
	recs, err := DecodeTile(obj[e.Offset : e.Offset+e.Length])
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 || !bytes.Equal(recs[2].Data, mlatLine) {
		t.Errorf("tile35 via range: %+v", recs)
	}
	// A corrupted frame fails its checksum.
	bad := bytes.Clone(obj[e.Offset : e.Offset+e.Length])
	bad[len(bad)-6] ^= 0xff
	if _, err = DecodeTile(bad); err == nil {
		t.Error("corrupted frame decoded without error")
	}
}

func TestShortIndexSaysHowMuch(t *testing.T) {
	h, tiles := sample()
	obj, err := Build(h, tiles)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ReadIndex(obj[:40])
	if !errors.Is(err, ErrShort) {
		t.Fatalf("ReadIndex on 40 bytes: %v", err)
	}
	size, err := IndexSize(obj[:8])
	if err != nil {
		t.Fatal(err)
	}
	if want := 8 + HeaderLen + 3*TileEntryLen; size != want {
		t.Errorf("IndexSize = %d, want %d", size, want)
	}
	if _, err = ReadIndex(obj[:size]); err != nil {
		t.Errorf("ReadIndex on exactly the index: %v", err)
	}
	if _, err = IndexSize([]byte{1, 2, 3, 4, 5, 6, 7, 8}); !errors.Is(err, ErrBadMagic) {
		t.Errorf("bad magic: %v", err)
	}
}

func TestEmptyPartition(t *testing.T) {
	h := Header{Start: t0, GridVersion: 1}
	obj, err := Build(h, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(obj) != 8+HeaderLen {
		t.Errorf("empty partition is %d bytes", len(obj))
	}
	idx, tiles, err := ReadAll(obj)
	if err != nil {
		t.Fatal(err)
	}
	if idx.Header.Records != 0 || len(idx.Tiles) != 0 || len(tiles) != 0 {
		t.Errorf("empty partition read back as %+v %v", idx, tiles)
	}
	// Still a valid zstd stream.
	dec, _ := zstd.NewReader(nil)
	defer dec.Close()
	out, err := dec.DecodeAll(obj, nil)
	if err != nil || len(out) != 0 {
		t.Errorf("zstd decode of empty partition: %d bytes, %v", len(out), err)
	}
}

// A stock zstd decoder skips the index and outputs every tile's records in table order.
func TestWholeObjectIsAZstdStream(t *testing.T) {
	h, tiles := sample()
	obj, err := Build(h, tiles)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	out, err := dec.DecodeAll(obj, nil)
	if err != nil {
		t.Fatal(err)
	}
	var want []byte
	for _, tile := range []uint8{0, 35, 74} {
		raw, err := EncodeRecords(tiles[tile])
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, raw...)
	}
	if !bytes.Equal(out, want) {
		t.Errorf("zstd -d output differs: %d bytes, want %d", len(out), len(want))
	}
	// The header is where the layout says, little-endian.
	if string(obj[8:12]) != Magic || binary.LittleEndian.Uint16(obj[12:14]) != Version {
		t.Errorf("header not at offset 8: % x", obj[:16])
	}
}

func TestRecordValidation(t *testing.T) {
	cases := map[string]Record{
		"feeder 8 bytes": {Source: SourceFeeder, Copies: 1, Data: make([]byte, 8)},
		"mlat empty":     {Source: SourceMLAT, Copies: 1},
		"mlat 256":       {Source: SourceMLAT, Copies: 1, Data: make([]byte, 256)},
		"copies 0":       {Source: SourceFeeder, Copies: 0, Data: msgShort},
		"source 2":       {Source: 2, Copies: 1, Data: msgShort},
	}
	for name, r := range cases {
		if _, err := EncodeRecords([]Record{r}); !errors.Is(err, ErrBadRecord) {
			t.Errorf("%s: %v", name, err)
		}
		if _, err := Build(Header{Start: t0}, map[uint8][]Record{1: {r}}); err == nil {
			t.Errorf("%s: Build accepted it", name)
		}
	}
	// 255 copies and a 255 byte line are the limits.
	ok := []Record{
		{Source: SourceFeeder, Copies: 255, Data: msgLong},
		{Source: SourceMLAT, Copies: 1, Data: make([]byte, 255)},
	}
	raw, err := EncodeRecords(ok)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseRecords(raw)
	if err != nil || len(back) != 2 || back[0].Copies != 255 || len(back[1].Data) != 255 {
		t.Errorf("limits: %v %+v", err, back)
	}
	if _, err = ParseRecords(raw[:len(raw)-1]); !errors.Is(err, ErrShort) {
		t.Errorf("truncated records: %v", err)
	}
}

func TestSizes(t *testing.T) {
	// A 112-bit message is 21 bytes, a 56-bit one 14, as the format document says.
	raw, err := EncodeRecords([]Record{{Source: SourceFeeder, Copies: 1, Data: msgLong}})
	if err != nil || len(raw) != 21 {
		t.Errorf("long record is %d bytes (%v)", len(raw), err)
	}
	raw, err = EncodeRecords([]Record{{Source: SourceFeeder, Copies: 1, Data: msgShort}})
	if err != nil || len(raw) != 14 {
		t.Errorf("short record is %d bytes (%v)", len(raw), err)
	}
}
