package rawarchive

import (
	"bytes"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func TestRoundTrip(t *testing.T) {
	enc := NewEncoder(64)
	beast := []byte{0x1a, 0x33, 1, 2, 3, 4, 5, 6, 7, 0x8d, 0x7c, 0x49, 0xf8, 0x58, 0x41, 0xd2, 0x6c, 0xca, 0x39, 0x33, 0xe4, 0x1e, 0xcf}
	sbs := []byte("MSG,3,1,1,7C6DB8,1,2026/10/25,01:02:03.456,2026/10/25,01:02:04.000,,3400,,,-31.95,115.86,,,,,,")
	if err := enc.Append(TypeBeast, 35, 1234, beast); err != nil {
		t.Fatal(err)
	}
	if err := enc.Append(TypeSBS1, TileUnknown, 4999, sbs); err != nil {
		t.Fatal(err)
	}
	if err := enc.Append(TypeAVR, 0, 0, nil); err != nil {
		t.Fatal(err)
	}
	if enc.Records() != 3 {
		t.Fatalf("records = %d", enc.Records())
	}
	recs, err := Decode(bytes.Clone(enc.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 {
		t.Fatalf("decoded %d records", len(recs))
	}
	if recs[0].Type != TypeBeast || recs[0].Tile != 35 || recs[0].Offset != 1234 || !bytes.Equal(recs[0].Data, beast) {
		t.Errorf("record 0 = %+v", recs[0])
	}
	if recs[1].Type != TypeSBS1 || recs[1].Tile != TileUnknown || recs[1].Offset != 4999 || !bytes.Equal(recs[1].Data, sbs) {
		t.Errorf("record 1 = %+v", recs[1])
	}
	if recs[2].Type != TypeAVR || len(recs[2].Data) != 0 {
		t.Errorf("record 2 = %+v", recs[2])
	}
	enc.Reset()
	if enc.Len() != 0 || enc.Records() != 0 {
		t.Errorf("reset left %d bytes, %d records", enc.Len(), enc.Records())
	}
}

func TestDecodeEmptyIsHeartbeat(t *testing.T) {
	recs, err := Decode(nil)
	if err != nil || len(recs) != 0 {
		t.Errorf("got %v, %v", recs, err)
	}
}

func TestDecodeTruncated(t *testing.T) {
	enc := NewEncoder(16)
	_ = enc.Append(TypeBeast, 1, 0, []byte{1, 2, 3, 4})
	body := enc.Bytes()
	for cut := 1; cut < len(body); cut++ {
		if _, err := Decode(body[:cut]); err == nil {
			t.Errorf("no error for body cut to %d bytes", cut)
		}
	}
}

func TestRecordTooLarge(t *testing.T) {
	enc := NewEncoder(0)
	if err := enc.Append(TypeSBS1, 1, 0, make([]byte, MaxRecordData+1)); err == nil {
		t.Error("oversized payload accepted")
	}
	if enc.Len() != 0 {
		t.Error("oversized payload left bytes behind")
	}
}

func TestHeaderRoundTrip(t *testing.T) {
	m := nats.NewMsg("x")
	start := time.Date(2026, 10, 26, 3, 4, 5, 0, time.UTC)
	SetHeader(m, Header{Instance: "runway-1-abc", Start: start, Dropped: 7})
	h, err := ParseHeader(m.Header)
	if err != nil {
		t.Fatal(err)
	}
	if h.Instance != "runway-1-abc" || !h.Start.Equal(start) || h.Dropped != 7 || h.Version != Version {
		t.Errorf("got %+v", h)
	}
}

func TestHeaderErrors(t *testing.T) {
	cases := map[string]nats.Header{
		"no version":  {HeaderInstance: {"a"}, HeaderStart: {"1"}},
		"bad version": {HeaderVersion: {"9"}, HeaderInstance: {"a"}, HeaderStart: {"1"}},
		"no instance": {HeaderVersion: {"1"}, HeaderStart: {"1"}},
		"no start":    {HeaderVersion: {"1"}, HeaderInstance: {"a"}},
		"bad start":   {HeaderVersion: {"1"}, HeaderInstance: {"a"}, HeaderStart: {"soon"}},
		"bad dropped": {HeaderVersion: {"1"}, HeaderInstance: {"a"}, HeaderStart: {"1"}, HeaderDropped: {"-1"}},
	}
	for name, hdr := range cases {
		if _, err := ParseHeader(hdr); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestTileNumber(t *testing.T) {
	cases := map[string]uint8{
		"tile0":       0,
		"tile35":      35,
		"tile74":      74,
		"tileUnknown": TileUnknown,
		"tile255":     TileUnknown,
		"tile300":     TileUnknown,
		"":            TileUnknown,
		"grid7":       TileUnknown,
	}
	for name, want := range cases {
		if got := TileNumber(name); got != want {
			t.Errorf("TileNumber(%q) = %d, want %d", name, got, want)
		}
	}
}

func TestValidInterval(t *testing.T) {
	good := []time.Duration{time.Second, 5 * time.Second, 50 * time.Millisecond, 30 * time.Second, time.Minute}
	bad := []time.Duration{0, -time.Second, 7 * time.Second, 45 * time.Second, 2 * time.Minute, 1500 * time.Microsecond}
	for _, d := range good {
		if !ValidInterval(d) {
			t.Errorf("%v should be valid", d)
		}
	}
	for _, d := range bad {
		if ValidInterval(d) {
			t.Errorf("%v should be invalid", d)
		}
	}
}

func TestIntervalStartAligned(t *testing.T) {
	loc := time.FixedZone("plus8", 8*3600)
	at := time.Date(2026, 10, 26, 11, 7, 23, 456_000_000, loc)
	got := IntervalStart(at, 5*time.Second)
	want := time.Date(2026, 10, 26, 3, 7, 20, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v want %v", got, want)
	}
	// A minute boundary is always an interval boundary.
	minute := time.Date(2026, 10, 26, 3, 8, 0, 0, time.UTC)
	for _, d := range []time.Duration{time.Second, 5 * time.Second, 15 * time.Second, time.Minute} {
		if !IntervalStart(minute.Add(d-time.Millisecond), d).Equal(minute) {
			t.Errorf("interval %v straddles the minute", d)
		}
	}
}
