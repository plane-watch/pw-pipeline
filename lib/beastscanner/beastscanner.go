// Package beastscanner provides a high-throughput, zero-alloc(ish) scanner for
// the BEAST binary feed format.
//
// It is designed for continuous streaming: one producer goroutine reads from a
// net.Conn into a lock-free style Single-Producer, Single-Client (SPSC) ring buffer,
// while the consumer parses frames directly from the ring with minimal copying and optional escape
// collapsing (0x1a 0x1a --> 0x1a).
//
// The scanner is robust to buffer wrap and to escape pairs that straddle the wrap boundary.

package beastscanner

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"

	"golang.org/x/sync/errgroup"
	"plane.watch/lib/bytering"
)

const (

	// maxRead is the maximum number of bytes to reserve/read from the socket
	// per iteration. It is also used as the ring capacity hint. 64 KiB keeps
	// syscalls large and amortised while avoiding oversized reservations.
	maxRead = 64 << 10

	// maxFrameSize is the maximum unescaped BEAST frame length.
	// Longest fixed-size type is 0x33:
	//   (Mode-S long): header(2) + mlat(6) + payload(15) = 23.
	// We leave a little extra slack (24*2 + 2) for safety when sizing the reusable out buffer.
	maxFrameSize = 2 + (2 * 24)
)

// Scanner incrementally reads and parses BEAST frames from an incoming
// connection into a single-producer/single-consumer ring buffer, and exposes
// frames via NextFrame. It aims to minimise allocations and copies.
//
// Concurrency model:
//   - One producer goroutine calls handleIncomingBeast (spawned by Run) and
//     performs blocking reads into reserved ring spans.
//   - The consumer runs NextFrame to parse frames out of the ring.
//
// Ownership:
//   - NextFrame passes a slice backed by Scanner.out to a callback. Do not
//     retain it after NextFrame returns unless you make a copy.
type Scanner struct {

	// incomingBeastConn is the source of raw BEAST bytes (escaped).
	incomingBeastConn net.Conn

	// buffer holds unread bytes between the producer and consumer. It supports
	// Reserve/Commit for contiguous or wrapped writes and blocking Peek/Advance
	// for reads. It is safe for exactly one producer and one consumer.
	buffer *bytering.RingBuffer

	// out is a reusable scratch buffer used to build a single frame to deliver
	// to the callback. When collapseEscapes=true, out receives the unescaped
	// frame. When collapseEscapes=false, out receives the raw physical bytes
	// belonging to the frame. Capacity is pre-allocated to avoid growth.
	out []byte

	frameHandler FrameHandler

	collapseEscapes bool
}

// FrameHandler consumes one frame produced by the scanner.
//
// When collapseEscapes was true in NextFrame, frame is unescaped and has the
// canonical layout:
//
// frame[0]   is 0x1a
// frame[1]   is one of 0x31, 0x32, 0x33 or 0x34
// frame[2:8] is the 6-byte MLAT counter (big-endian)
// frame[8:]  is the message payload (length depends on type)
//
// When collapseEscapes was false, frame contains the raw physical bytes between
// the current header and the next header (escape pairs are intact). In that
// mode, the handler should perform escape processing itself if needed.
//
// The handler must not retain the slice after returning unless it copies it.
type FrameHandler func(frame []byte) error

type Option func(*Scanner) error

func WithBEASTConnection(c net.Conn) Option {
	return func(s *Scanner) error {
		s.incomingBeastConn = c
		return nil
	}
}

func WithFrameHandler(fh FrameHandler) Option {
	return func(s *Scanner) error {
		s.frameHandler = fh
		return nil
	}
}

func WithEscapeCollapsing(collapseEscapes bool) Option {
	return func(s *Scanner) error {
		s.collapseEscapes = collapseEscapes
		return nil
	}
}

// Run constructs a Scanner for the provided BEAST connection, starts the
// producer goroutine that continuously reads into the ring, and (in this
// example harness) loops pulling frames and printing them.
//
// todo(mikenye): This currently blocks forever in a NextFrame loop and waits for the producer on error.
func Run(opts ...Option) (*Scanner, error) {
	var err error

	s := &Scanner{
		buffer: bytering.New(maxRead),
		out:    make([]byte, maxFrameSize),
	}

	for _, opt := range opts {
		if err = opt(s); err != nil {
			return nil, err
		}
	}

	// sanity checks
	if s.incomingBeastConn == nil {
		return s, errors.New("missing BEAST connection, use WithBEASTConnection")
	}
	if s.frameHandler == nil {
		return s, errors.New("missing BEAST connection, use WithFrameHandler")
	}

	wg := errgroup.Group{}

	// start producer
	// todo(mikenye): need context support to allow exit
	wg.Go(s.handleIncomingBeast)

	// start consumer
	// todo(mikenye): need context support to allow exit
	wg.Go(func() error {
		for {
			err = s.NextFrame(s.frameHandler, s.collapseEscapes)
			if err != nil {
				return err
			}
		}
	})

	err = wg.Wait()
	return s, err
}

// synchronise advances the ring’s read index until it sits exactly on the 0x1a
// of a real BEAST header. A real header satisfies:
//
//	prev != 0x1a, cur == 0x1a, and next is one of 0x31, 0x32, 0x33 or 0x34.
//
// The method blocks (via ring conditions) until enough bytes are available to
// evaluate the pattern, and skips escaped pairs (“0x1a 0x1a”) and lone 0x1a
// within payloads. On success, the next read begins at the header’s 0x1a.
func (s *Scanner) synchronise() error {
	for {
		// Find the next 0x1a among unread bytes (blocks until there is one).
		off := s.buffer.IndexByteBlock(0x1a)

		// If it's at relative offset 0, we can't test prev!=0x1a. Skip this byte and continue.
		if off == 0 {
			_ = s.buffer.Advance(1)
			continue
		}

		// Ensure we can read up to off+1 (need window length >= off+2).
		if _, _, err := s.buffer.Peek2BlockAtLeast(off + 2); err != nil {
			return err
		}

		// Zero-alloc random access at relative offsets.
		prev, _ := s.buffer.PeekByteAt(off - 1)
		cur, _ := s.buffer.PeekByteAt(off)
		next, _ := s.buffer.PeekByteAt(off + 1)

		// Check for true (unescaped) header.
		if prev != 0x1a && cur == 0x1a &&
			(next == 0x31 || next == 0x32 || next == 0x33 || next == 0x34) {
			// Leave reader positioned at the 0x1a
			_ = s.buffer.Advance(off)
			return nil
		}

		// Not a real header (escaped 0x1a 0x1a or junk after 0x1a): skip this 0x1a and continue.
		_ = s.buffer.Advance(off + 1)
	}
}

// handleIncomingBeast is the producer loop. It continuously reserves space in
// the ring and reads from the network connection into at most two slices
// (handling wrap). Partial reads are committed; errors are coalesced after any
// successful commit so that no data is lost.
//
// Behavior:
//   - Reserves up to maxRead bytes (possibly split across the wrap).
//   - Performs one or two Read calls into the reserved spans.
//   - Commits the total number of bytes read, or cancels if zero.
//   - Continues on temporary timeouts; returns nil on io.EOF; wraps other
//     errors with context.
func (s *Scanner) handleIncomingBeast() error {
	for {
		// Reserve up to maxRead across the wrap if needed.
		s1, s2 := s.buffer.Reserve2Block(maxRead)

		total := 0

		// 1) Read into the first contiguous span.
		n1, err1 := s.incomingBeastConn.Read(s1)
		total += n1

		// 2) Only read into the wrapped span if:
		//    - first read filled s1,
		//    - there's a wrapped span,
		//    - and no error occurred on the first read.
		var err2 error
		if err1 == nil && n1 == len(s1) && len(s2) > 0 {
			n2, e2 := s.incomingBeastConn.Read(s2)
			total += n2
			err2 = e2
		}

		// Publish or cancel the reservation.
		if total == 0 {
			s.buffer.CancelReserve()
		} else {
			if err := s.buffer.Commit(total); err != nil {
				return fmt.Errorf("commit to ring: %w", err)
			}
		}

		// Coalesce errors and decide what to do (after committing any bytes).
		if err := firstErr(err1, err2); err != nil {
			if ne, ok := err.(net.Error); ok && (ne.Timeout() || ne.Temporary()) {
				continue // benign; keep reading
			}
			if errors.Is(err, io.EOF) {
				return nil // clean shutdown
			}
			return fmt.Errorf("read beast: %w", err)
		}
	}
}

// firstErr returns a if non-nil, otherwise b. It is used to coalesce errors
// from the two socket reads performed when the ring reservation spans wrap.
func firstErr(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

// NextFrame blocks until a complete BEAST frame is available starting at the
// current read position (ideally at the frame’s 0x1a), finds the next real
// header, optionally collapses escape pairs across ring spans, performs basic
// validation, advances the ring to the next header, and delivers the frame to
// the provided callback.
//
// Escapes:
//
//	If collapseEscapes is true, the scanner collapses “0x1a 0x1a” → “0x1a”,
//	even when the pair is split across ring wrap. The callback receives the
//	canonical unescaped frame. If false, the callback receives raw physical
//	bytes between current and next header; it must handle escapes itself.
//
// Validation:
//
//	With collapseEscapes=true, fixed-length types (0x31, 0x32, 0x33) are
//	length-checked: header(2) + mlat(6) + payload(3/8/15). Type 0x34 (status)
//	is variable; it is printed and skipped. In all cases, NextFrame never
//	advances past the next header unless a complete frame has been consumed.
//
// Positioning:
//
//	On return, the ring’s read index is positioned at the next frame’s 0x1a.
//	The callback must not retain the frame slice after return unless copied.
func (s *Scanner) NextFrame(cb FrameHandler, collapseEscapes bool) error {
	// Self-heal if we aren't at a header
	if b, ok := s.buffer.PeekByteAt(0); !ok || b != 0x1a {
		if err := s.synchronise(); err != nil {
			return err
		}
	}

	searchFrom := 1 // skip current header byte
	for {
		s1, s2 := s.buffer.PeekAll2Block()
		total := len(s1) + len(s2)
		if total <= searchFrom {
			if _, _, err := s.buffer.Peek2BlockAtLeast(total + 1); err != nil {
				return err
			}
			continue
		}

		// Find next 0x1a at/after searchFrom across s1/s2.
		nextRel := -1
		if searchFrom < len(s1) {
			if j := bytes.IndexByte(s1[searchFrom:], 0x1a); j >= 0 {
				nextRel = searchFrom + j
			}
		}
		if nextRel < 0 && len(s2) > 0 {
			start2 := searchFrom - len(s1)
			if start2 < 0 {
				start2 = 0
			}
			if start2 < len(s2) {
				if j := bytes.IndexByte(s2[start2:], 0x1a); j >= 0 {
					nextRel = len(s1) + start2 + j
				}
			}
		}

		if nextRel < 0 {
			if _, _, err := s.buffer.Peek2BlockAtLeast(total + 1); err != nil {
				return err
			}
			continue
		}

		// Verify this is a REAL header (not the second byte of an escaped pair)
		// and that its lookahead is a frame type.
		if _, ok := s.buffer.PeekByteAt(nextRel + 1); !ok {
			if _, _, err := s.buffer.Peek2BlockAtLeast(nextRel + 2); err != nil {
				return err
			}
			continue
		}
		prev, _ := s.buffer.PeekByteAt(nextRel - 1)
		look, _ := s.buffer.PeekByteAt(nextRel + 1)

		if prev == 0x1a { // escaped 0x1a within payload
			searchFrom = nextRel + 1
			continue
		}
		if look != 0x31 && look != 0x32 && look != 0x33 && look != 0x34 {
			// Lone 0x1a in payload; keep searching
			searchFrom = nextRel + 1
			continue
		}

		// Current frame occupies physical bytes [0 : nextRel).
		framePhys := nextRel

		// ----- Build output (collapse across spans if requested) -----
		s.out = s.out[:0]
		if collapseEscapes {
			// stateful collapse across both spans
			carry := false
			collapseSpan := func(dst *[]byte, src []byte, final bool) bool {
				out := *dst
				i := 0
				if carry {
					// Need one byte to complete a possible "0x1a 0x1a" pair.
					if i < len(src) && src[i] == 0x1a {
						out = append(out, 0x1a) // complete the pair
						i++
					} else {
						// A dangling 0x1a at end of previous span without a matching 0x1a.
						// In valid BEAST inside a frame this shouldn't happen; treat the 0x1a as literal.
						out = append(out, 0x1a)
					}
					carry = false
				}
				for i < len(src) {
					b := src[i]
					if b == 0x1a {
						if i+1 < len(src) {
							if src[i+1] == 0x1a {
								out = append(out, 0x1a)
								i += 2
								continue
							}
							// Single 0x1a followed by non-0x1a inside frame shouldn't occur;
							// treat as literal for robustness.
							out = append(out, 0x1a)
							i++
							continue
						}
						// 0x1a at end of this span — carry to next span
						carry = true
						i++
						break
					}
					out = append(out, b)
					i++
				}
				*dst = out
				// If final and carry is still true, it would mean frame ended with a dangling 0x1a,
				// which cannot happen because the next byte is header 0x1a (different frame).
				// We purposely do NOT resolve it here.
				return carry
			}

			if framePhys <= len(s1) {
				_ = collapseSpan(&s.out, s1[:framePhys], true)
			} else {
				n1 := len(s1)
				_ = collapseSpan(&s.out, s1, false)
				_ = collapseSpan(&s.out, s2[:framePhys-n1], true)
			}
		} else {
			// raw physical bytes
			if framePhys <= len(s1) {
				s.out = append(s.out, s1[:framePhys]...)
			} else {
				n1 := len(s1)
				s.out = append(s.out, s1...)
				s.out = append(s.out, s2[:framePhys-n1]...)
			}
		}

		// Advance to the next frame's header.
		_ = s.buffer.Advance(framePhys)

		// ----- Validate + dispatch -----
		f := s.out
		if len(f) < 2 || f[0] != 0x1a {
			// Bad frame; leave pointer at next header and try to re-sync on the next call.
			return nil
		}
		t := f[1]

		if t == 0x34 {
			// Print and return WITHOUT re-synchronising (we're sitting at the next header already).
			fmt.Printf("unsupported frame type 0x34: % X\n", f)
			return nil
		}

		if collapseEscapes {
			want := -1
			switch t {
			case 0x31:
				want = 2 + 6 + 3
			case 0x32:
				want = 2 + 6 + 8
			case 0x33:
				want = 2 + 6 + 15
			default:
				// Unknown type; ignore and let next call attempt again.
				return nil
			}
			if want > 0 && len(f) != want {
				// Likely previously-split escape bug; just return and let the next pass rescan.
				return nil
			}
		} else {
			switch t {
			case 0x31, 0x32, 0x33:
				// ok
			default:
				return nil
			}
		}

		// Deliver the frame.
		return cb(f)
	}
}
