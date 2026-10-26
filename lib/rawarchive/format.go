// Package rawarchive defines the batch format the raw archive middleware publishes
// and an archiver consumes: one NATS message per ingest instance per interval,
// carrying the raw frames the instance accepted during that interval.
//
// Headers
//
//	instance  name of the publishing ingest process, stable for its lifetime
//	start     interval start, Unix milliseconds, on an interval boundary
//	dropped   frames the instance dropped from its queue since its last message
//	v         format version, "1"
//
// Body: records back to back, each
//
//	1 byte   type (TypeBeast, TypeAVR, TypeSBS1)
//	1 byte   tile number, TileUnknown when the source has no position
//	2 bytes  offset from start when the frame reached the middleware, ms, big-endian
//	2 bytes  length n, big-endian
//	n bytes  the frame as Frame.Raw() returns it: whole Beast frame (escape, type,
//	         timestamp, signal, Mode S bytes), Mode S bytes for AVR, the SBS line
//	         without its line ending
//
// An empty body is a heartbeat: the instance was alive and heard nothing.
package rawarchive

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
)

const (
	// Version is the format version carried in the "v" header.
	Version = 1

	HeaderInstance = "instance"
	HeaderStart    = "start"
	HeaderDropped  = "dropped"
	HeaderVersion  = "v"

	// DefaultSubject is the subject the proposal uses; it is configured, not fixed.
	DefaultSubject = "v1.pw-ingest.raw"

	TypeBeast uint8 = 1
	TypeAVR   uint8 = 2
	TypeSBS1  uint8 = 3

	// TileUnknown marks a record whose source has no position, or whose position is
	// outside every tile. Tile numbers themselves start at 0 (tile0 exists).
	TileUnknown uint8 = 255

	recordHeaderLen = 6
	// MaxRecordData is the longest payload a record can carry.
	MaxRecordData = 0xFFFF
	// MaxOffset is the largest offset a record can carry; intervals are at most a minute.
	MaxOffset = 0xFFFF
)

var (
	ErrShortRecord    = errors.New("rawarchive: truncated record")
	ErrBadVersion     = errors.New("rawarchive: unsupported format version")
	ErrMissingHeader  = errors.New("rawarchive: missing header")
	ErrRecordTooLarge = errors.New("rawarchive: record payload too large")
)

// Record is one frame in a batch. Data aliases the batch body it was decoded from.
type Record struct {
	Type   uint8
	Tile   uint8
	Offset uint16 // ms after the batch start
	Data   []byte
}

// Header is the parsed message header.
type Header struct {
	Instance string
	Start    time.Time
	Dropped  uint64
	Version  int
}

// TypeName names a record type for logs and headers.
func TypeName(t uint8) string {
	switch t {
	case TypeBeast:
		return "beast"
	case TypeAVR:
		return "avr"
	case TypeSBS1:
		return "sbs1"
	default:
		return "unknown"
	}
}

// TileNumber converts a tile_grid name ("tile35") to its number. Anything else,
// including "tileUnknown", is TileUnknown.
func TileNumber(name string) uint8 {
	n, ok := strings.CutPrefix(name, "tile")
	if !ok {
		return TileUnknown
	}
	v, err := strconv.ParseUint(n, 10, 8)
	if err != nil || v >= uint64(TileUnknown) {
		return TileUnknown
	}
	return uint8(v)
}

// Encoder builds a batch body in memory. The zero value is ready to use.
type Encoder struct {
	buf     []byte
	records int
}

// NewEncoder returns an encoder with room for about capacity bytes before growing.
func NewEncoder(capacity int) *Encoder {
	return &Encoder{buf: make([]byte, 0, capacity)}
}

// Append adds one record. It returns ErrRecordTooLarge if data does not fit a record.
func (e *Encoder) Append(typ, tile uint8, offset uint16, data []byte) error {
	if len(data) > MaxRecordData {
		return ErrRecordTooLarge
	}
	e.buf = append(e.buf, typ, tile)
	e.buf = binary.BigEndian.AppendUint16(e.buf, offset)
	e.buf = binary.BigEndian.AppendUint16(e.buf, uint16(len(data)))
	e.buf = append(e.buf, data...)
	e.records++
	return nil
}

// Len is the body length so far.
func (e *Encoder) Len() int { return len(e.buf) }

// Records is the number of records appended since the last Reset.
func (e *Encoder) Records() int { return e.records }

// Bytes returns the body. It is only valid until the next Append or Reset.
func (e *Encoder) Bytes() []byte { return e.buf }

// Reset empties the encoder, keeping its buffer.
func (e *Encoder) Reset() {
	e.buf = e.buf[:0]
	e.records = 0
}

// Decode parses a batch body. Records alias body.
func Decode(body []byte) ([]Record, error) {
	var out []Record
	for len(body) > 0 {
		if len(body) < recordHeaderLen {
			return out, ErrShortRecord
		}
		n := int(binary.BigEndian.Uint16(body[4:6]))
		if len(body) < recordHeaderLen+n {
			return out, ErrShortRecord
		}
		out = append(out, Record{
			Type:   body[0],
			Tile:   body[1],
			Offset: binary.BigEndian.Uint16(body[2:4]),
			Data:   body[recordHeaderLen : recordHeaderLen+n],
		})
		body = body[recordHeaderLen+n:]
	}
	return out, nil
}

// SetHeader writes the batch headers onto m.
func SetHeader(m *nats.Msg, h Header) {
	if m.Header == nil {
		m.Header = nats.Header{}
	}
	m.Header.Set(HeaderInstance, h.Instance)
	m.Header.Set(HeaderStart, strconv.FormatInt(h.Start.UnixMilli(), 10))
	m.Header.Set(HeaderDropped, strconv.FormatUint(h.Dropped, 10))
	m.Header.Set(HeaderVersion, strconv.Itoa(Version))
}

// ParseHeader reads the batch headers from a message.
func ParseHeader(hdr nats.Header) (Header, error) {
	var h Header
	v := hdr.Get(HeaderVersion)
	if v == "" {
		return h, fmt.Errorf("%w: %s", ErrMissingHeader, HeaderVersion)
	}
	ver, err := strconv.Atoi(v)
	if err != nil || ver != Version {
		return h, fmt.Errorf("%w: %q", ErrBadVersion, v)
	}
	h.Version = ver
	h.Instance = hdr.Get(HeaderInstance)
	if h.Instance == "" {
		return h, fmt.Errorf("%w: %s", ErrMissingHeader, HeaderInstance)
	}
	startStr := hdr.Get(HeaderStart)
	if startStr == "" {
		return h, fmt.Errorf("%w: %s", ErrMissingHeader, HeaderStart)
	}
	ms, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil {
		return h, fmt.Errorf("rawarchive: bad %s header %q: %w", HeaderStart, startStr, err)
	}
	h.Start = time.UnixMilli(ms).UTC()
	if d := hdr.Get(HeaderDropped); d != "" {
		h.Dropped, err = strconv.ParseUint(d, 10, 64)
		if err != nil {
			return h, fmt.Errorf("rawarchive: bad %s header %q: %w", HeaderDropped, d, err)
		}
	}
	return h, nil
}

// ValidInterval reports whether interval is a whole number of milliseconds that
// divides a minute, so that intervals aligned to the clock never straddle one.
func ValidInterval(interval time.Duration) bool {
	return interval > 0 &&
		interval <= time.Minute &&
		interval%time.Millisecond == 0 &&
		time.Minute%interval == 0
}

// IntervalStart returns the start of the interval containing t: the latest whole
// multiple of interval past the UTC minute boundary at or before t.
func IntervalStart(t time.Time, interval time.Duration) time.Time {
	return t.UTC().Truncate(interval)
}
