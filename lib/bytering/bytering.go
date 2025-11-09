// Package bytering implements a zero-allocation, power-of-two single-producer,
// single-consumer (SPSC) ring buffer of bytes.
//
// It is designed for high-throughput streaming of fixed-size or variable-size
// data, where a producer continuously writes and a consumer continuously reads.
//
// The implementation guarantees:
//   - Zero allocations after creation
//   - Constant-time O(1) read/write operations
//   - Power-of-two capacity and mask wrapping (no modulo)
//   - Safe use by exactly one producer and one consumer (SPSC)
//
// Typical use cases include buffering network frames, decoded messages, or
// streaming telemetry where maximum throughput and minimal GC impact are desired.
package bytering

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"time"
)

var (

	// ErrBufferOverflow indicates that a RingBuffer.Write operation could not fit all data
	// into the RingBuffer because it is full. The caller can inspect the number of items
	// written and decide what to do.
	ErrBufferOverflow = errors.New("ringbuf: buffer full")

	// ErrAdvanceOutOfRange indicates that an RingBuffer.Advance operation could not be completed because
	// doing so would exceed the capacity of the RingBuffer.
	ErrAdvanceOutOfRange = errors.New("ringbuf: advance out of range")

	// ErrPeekExceedsCapacity indicates that the requested peek length > ring capacity (would deadlock).
	ErrPeekExceedsCapacity = errors.New("ringbuf: peek length exceeds capacity")

	// ErrPeekInvalidLen indicates that non-positive k passed to AtLeast variants.
	ErrPeekInvalidLen = errors.New("ringbuf: peek length must be > 0")
)

// RingBuffer is a single-producer, single-consumer ring buffer of bytes.
// It stores byte values in a fixed-size circular buffer.
//
// The internal indices (r and w) are monotonic counters. Wrapping within
// the buffer is handled via a power-of-two mask.
//
// This type is not safe for concurrent use by multiple producers or consumers.
// Use a single dedicated writer and reader goroutine for maximum performance.
type RingBuffer struct {
	buf  []byte
	mask uint64 // cap(buf)-1 (cap is power of two)
	r    uint64 // read index (monotonic)
	w    uint64 // write index (monotonic)

	// reservation (single-producer): only one outstanding at a time
	resLen int  // length of last Reserve/Reserve2 (total writable view)
	inRes  bool // true between Reserve* and Commit/CancelReserve

	// blocking support
	mu   sync.Mutex
	cond *sync.Cond
}

// nextPow2 returns the next power-of-two >= x.
// Used to size the ring capacity for efficient wrapping.
func nextPow2(x uint64) uint64 {
	if x < 2 {
		return 2
	}
	x--
	x |= x >> 1
	x |= x >> 2
	x |= x >> 4
	x |= x >> 8
	x |= x >> 16
	x |= x >> 32
	return x + 1
}

// New creates a new RingBuffer with a capacity rounded up to the next power of two.
//
// The capacity determines how many elements can be stored concurrently.
// Once full, subsequent writes will either return ErrBufferOverflow
// (short-write semantics) or you can use WriteOverwrite to forcibly replace old data.
//
// Example:
//
//	rb := ringbuf[byte].New(1023)  // 1024 entries (power-of-two rounded)
func New(capHint int) *RingBuffer {
	if capHint <= 0 {
		capHint = 1024
	}
	c := int(nextPow2(uint64(capHint)))
	r := &RingBuffer{
		buf:  make([]byte, c),
		mask: uint64(c - 1),
	}
	r.cond = sync.NewCond(&r.mu)
	return r
}

// Cap returns the total capacity of the buffer (the number of elements
// it can hold before becoming full).
func (r *RingBuffer) Cap() int { return len(r.buf) }

// Len returns the number of elements currently stored in the buffer.
func (r *RingBuffer) Len() int {
	r.mu.Lock()
	n := r.lenUnlocked()
	r.mu.Unlock()
	return n
}

// Free returns the number of free slots remaining before the buffer is full.
func (r *RingBuffer) Free() int {
	r.mu.Lock()
	f := r.freeUnlocked()
	r.mu.Unlock()
	return f
}

func (r *RingBuffer) FreeLocked() int   { return r.freeUnlocked() } // require r.mu held
func (r *RingBuffer) lenUnlocked() int  { return int(r.w - r.r) }
func (r *RingBuffer) freeUnlocked() int { return len(r.buf) - r.lenUnlocked() }

// Reset clears all contents of the buffer and resets read and write indices
// without reallocating memory.
//
// This is equivalent to consuming all elements instantly.
func (r *RingBuffer) Reset() {
	r.mu.Lock()
	r.r, r.w, r.resLen, r.inRes = 0, 0, 0, false
	r.mu.Unlock()
	r.cond.Broadcast()
}

// Write inserts as many elements from vals as will fit into the buffer.
// It returns the number of elements successfully written and an optional error.
//
// If the buffer does not have enough space, the write is truncated to what
// fits, and ErrBufferOverflow is returned.
//
// Write performs at most two copy operations (pre-wrap and post-wrap) and
// never allocates new memory.
//
// Example:
//
//	n, err := rb.Write(data)
//	if errors.Is(err, ringbuf.ErrBufferOverflow) {
//	    // handle partial write
//	}
func (r *RingBuffer) Write(vals []byte) (n int, err error) {
	free := r.Free()
	if free == 0 {
		return 0, ErrBufferOverflow
	}
	if len(vals) > free {
		vals, err = vals[:free], ErrBufferOverflow
	}
	n = len(vals)
	i := int(r.w & r.mask)
	// first contiguous segment
	n1 := min(n, len(r.buf)-i)
	copy(r.buf[i:i+n1], vals[:n1])
	// wrapped segment
	copy(r.buf[:n-n1], vals[n1:])
	r.w += uint64(n)
	return n, err
}

// Read removes up to len(out) elements from the buffer and copies them into out.
// It returns the number of elements actually read.
//
// Read performs at most two copy operations (pre-wrap and post-wrap).
// When the buffer is empty, Read returns 0.
//
// Example:
//
//	n := rb.Read(dst)
//	if n == 0 {
//	    // buffer empty
//	}
func (r *RingBuffer) Read(out []byte) (n int) {
	r.mu.Lock()
	avail := r.lenUnlocked()
	if avail != 0 && len(out) != 0 {
		if len(out) < avail {
			avail = len(out)
		}
		n = avail
		i := int(r.r & r.mask)
		n1 := min(n, len(r.buf)-i)
		copy(out[:n1], r.buf[i:i+n1])
		copy(out[n1:n], r.buf[:n-n1])
		r.r += uint64(n)
	}
	r.mu.Unlock()
	if n > 0 {
		// space available → wake producer
		r.cond.Signal()
	}
	return n
}

// Peek returns a slice view into the buffer containing up to k contiguous
// elements starting from the current read position.
//
// The returned slice is backed by the internal buffer and is only valid until
// the next call to Write, Read, or Reset. It allows inspecting or parsing data
// without performing a copy.
//
// Peek never wraps across the end of the buffer; if data wraps, only the first
// contiguous run is returned.
//
// Example:
//
//	if s := rb.Peek(128); len(s) > 0 {
//	    process(s)
//	    rb.Advance(len(s)) // manually consume later
//	}
func (r *RingBuffer) Peek(k int) []byte {
	r.mu.Lock()
	avail := r.lenUnlocked()
	if avail == 0 {
		r.mu.Unlock()
		return nil
	}
	if k > avail {
		k = avail
	}
	i := int(r.r & r.mask)
	max1 := len(r.buf) - i
	if k > max1 {
		k = max1
	}
	out := r.buf[i : i+k]
	r.mu.Unlock()
	return out
}

// Advance consumes n elements from the buffer without copying.
// It is typically used after a Peek/parse step.
//
// Constraints:
//   - n must be in [0, Len()]. Advancing beyond available data is an error.
//   - The returned view from a previous Peek becomes invalid after Advance.
//   - Single-producer/single-consumer only; do not call concurrently with Write/Read.
//
// Example:
//
//	if s := rb.Peek(4096); len(s) > 0 {
//	    // parse/process s[:consumed]
//	    _ = rb.Advance(consumed)
//	}
func (r *RingBuffer) Advance(n int) error {
	r.mu.Lock()
	if n < 0 || n > r.lenUnlocked() {
		r.mu.Unlock()
		return ErrAdvanceOutOfRange
	}
	r.r += uint64(n)
	r.mu.Unlock()
	if n > 0 {
		r.cond.Signal()
	}
	return nil
}

// ContigFree returns the number of free bytes available in a single contiguous
// run from the current write index (may be < Free() if wrap would be needed).
func (r *RingBuffer) ContigFree() int {
	free := r.Free()
	if free == 0 {
		return 0
	}
	i := int(r.w & r.mask)
	max1 := len(r.buf) - i
	if free < max1 {
		return free
	}
	return max1
}

// Reserve returns up to k contiguous writable bytes starting at the current
// write index. It may return fewer than k (including zero) depending on space.
// Exactly one reservation may be active at a time; call Commit() or
// CancelReserve() before calling Reserve() again.
//
// Returned slice is invalidated by any Write/Read/Reset/Commit/CancelReserve.
func (r *RingBuffer) Reserve(k int) []byte {
	if r.inRes {
		// single outstanding reservation enforced
		return nil
	}
	if k <= 0 {
		return nil
	}
	avail := r.Free()
	if avail == 0 {
		return nil
	}
	if k > avail {
		k = avail
	}
	i := int(r.w & r.mask)
	max1 := len(r.buf) - i
	if k > max1 {
		k = max1
	}
	r.resLen = k
	r.inRes = true
	return r.buf[i : i+k]
}

// Reserve2 returns up to k writable bytes as two slices that may span the wrap.
// If all bytes fit contiguously, s2 will be nil. May return fewer than k
// (including zero) if the ring is nearly full. Only one outstanding
// reservation at a time.
func (r *RingBuffer) Reserve2(k int) (s1, s2 []byte) {
	if r.inRes {
		return nil, nil
	}
	if k <= 0 {
		return nil, nil
	}
	avail := r.Free()
	if avail == 0 {
		return nil, nil
	}
	if k > avail {
		k = avail
	}
	i := int(r.w & r.mask)
	max1 := len(r.buf) - i

	if k <= max1 {
		r.resLen = k
		r.inRes = true
		return r.buf[i : i+k], nil
	}

	// split across wrap
	n1 := max1
	n2 := k - n1
	r.resLen = n1 + n2
	r.inRes = true
	return r.buf[i : i+n1], r.buf[:n2]
}

// Commit publishes the first n bytes written into the most recent reservation.
// n must be in [0, resLen]. After Commit (or CancelReserve) the reservation is
// cleared and a new Reserve can be made.
func (r *RingBuffer) Commit(n int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.inRes {
		return errors.New("ringbuf: no reservation to commit")
	}
	if n < 0 || n > r.resLen || n > r.freeUnlocked() {
		r.inRes, r.resLen = false, 0
		return errors.New("ringbuf: commit out of range")
	}
	r.w += uint64(n)
	r.inRes, r.resLen = false, 0
	// data available → wake consumer
	r.cond.Signal()
	return nil
}

// CancelReserve abandons the outstanding reservation without publishing bytes.
func (r *RingBuffer) CancelReserve() {
	r.mu.Lock()
	r.inRes, r.resLen = false, 0
	r.mu.Unlock()
}

// ReserveBlock blocks (zero alloc) until there is at least 1 byte of free space,
// then returns a contiguous writable slice of up to k bytes.
func (r *RingBuffer) ReserveBlock(k int) []byte {
	r.mu.Lock()
	for r.inRes || r.FreeLocked() == 0 {
		r.cond.Wait() // releases and re-acquires r.mu
	}
	i := int(r.w & r.mask)
	max1 := len(r.buf) - i
	avail := r.FreeLocked()
	if k <= 0 || k > avail {
		k = avail
	}
	if k > max1 {
		k = max1
	}
	r.resLen, r.inRes = k, true
	out := r.buf[i : i+k]
	r.mu.Unlock()
	return out
}

// Reserve2Block blocks (zero alloc) until there is at least 1 byte of free space,
// then returns up to k bytes possibly split across the wrap.
func (r *RingBuffer) Reserve2Block(k int) (s1, s2 []byte) {
	r.mu.Lock()
	for r.inRes || r.FreeLocked() == 0 {
		r.cond.Wait()
	}
	avail := r.FreeLocked()
	if k <= 0 || k > avail {
		k = avail
	}
	i := int(r.w & r.mask)
	max1 := len(r.buf) - i
	if k <= max1 {
		r.resLen, r.inRes = k, true
		s1, s2 = r.buf[i:i+k], nil
		r.mu.Unlock()
		return
	}
	n1 := max1
	n2 := k - n1
	r.resLen, r.inRes = n1+n2, true
	s1, s2 = r.buf[i:i+n1], r.buf[:n2]
	r.mu.Unlock()
	return
}

// Peek2 returns up to k bytes starting at the current read index, possibly
// split across the end-of-buffer wrap. If all requested bytes are contiguous,
// s2 will be nil. The returned slices alias the ring’s internal storage and
// are valid until the next mutation (Write/Commit/CancelReserve/Read/Advance/Reset).
//
// Zero-alloc; SPSC only.
func (r *RingBuffer) Peek2(k int) (s1, s2 []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	avail := r.lenUnlocked()
	if avail == 0 {
		return nil, nil
	}
	if k > avail {
		k = avail
	}
	i := int(r.r & r.mask)
	max1 := len(r.buf) - i
	if k <= max1 {
		return r.buf[i : i+k], nil
	}
	// wrap across the end
	n1 := max1
	n2 := k - n1
	return r.buf[i : i+n1], r.buf[:n2]
}

// PeekAll2 returns all currently available bytes split into at most two
// slices that together cover the full readable range. s2 will be nil if
// everything is contiguous. Zero-alloc; SPSC only.
func (r *RingBuffer) PeekAll2() (s1, s2 []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	avail := r.lenUnlocked()
	if avail == 0 {
		return nil, nil
	}
	i := int(r.r & r.mask)
	max1 := len(r.buf) - i
	if avail <= max1 {
		return r.buf[i : i+avail], nil
	}
	return r.buf[i:], r.buf[:avail-max1]
}

// AdvanceBlock blocks until at least n bytes are available, then consumes n.
// It never spins and never allocs. Single consumer only.
func (r *RingBuffer) AdvanceBlock(n int) error {
	if n < 0 {
		return ErrAdvanceOutOfRange
	}
	r.mu.Lock()
	for r.lenUnlocked() < n {
		r.cond.Wait() // releases + re-acquires r.mu
	}
	r.r += uint64(n)
	r.mu.Unlock()
	// space freed → wake producer
	if n > 0 {
		r.cond.Signal()
	}
	return nil
}

// AdvanceBlockCtx is the same as AdvanceBlock but supports cancellation.
// Returns ctx.Err() if the context is cancelled before n bytes are available.
func (r *RingBuffer) AdvanceBlockCtx(ctx context.Context, n int) error {
	if n < 0 {
		return ErrAdvanceOutOfRange
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	for r.lenUnlocked() < n {
		if ctx != nil {
			// Wait in small steps so we can observe ctx cancellation
			// without allocating; use timeouts + recheck.
			done := false
			timer := time.NewTimer(200 * time.Millisecond)
			go func() {
				r.cond.Wait()
				if !timer.Stop() {
					<-timer.C
				}
				done = true
			}()
			r.mu.Unlock()

			select {
			case <-ctx.Done():
				// reacquire to keep lock discipline
				r.mu.Lock()
				return ctx.Err()
			case <-timer.C:
				// timed wakeup; recheck condition
			}

			r.mu.Lock()
			if done {
				continue
			}
			// Spurious path: if the cond waiter returned early we still just loop.
		} else {
			r.cond.Wait()
		}
	}
	r.r += uint64(n)
	// signal outside the loop (producer may be waiting for space)
	r.cond.Signal()
	return nil
}

// AdvanceWhenData blocks until at least one byte is available, then consumes
// up to n bytes (<= available). Returns the number actually advanced.
// Useful when your parser says "I can consume up to n; give me whatever is ready".
func (r *RingBuffer) AdvanceWhenData(n int) (int, error) {
	if n < 0 {
		return 0, ErrAdvanceOutOfRange
	}
	r.mu.Lock()
	for r.lenUnlocked() == 0 {
		r.cond.Wait()
	}
	avail := r.lenUnlocked()
	if n > avail {
		n = avail
	}
	r.r += uint64(n)
	r.mu.Unlock()
	if n > 0 {
		r.cond.Signal()
	}
	return n, nil
}

// Peek2Block blocks until at least one byte is available, then returns up to
// k bytes from the current read position, possibly split across the wrap.
// If k <= 0, it returns all currently available bytes.
// Zero-alloc; SPSC only. Returned slices are invalid after the next mutation.
func (r *RingBuffer) Peek2Block(k int) (s1, s2 []byte) {
	r.mu.Lock()
	for r.lenUnlocked() == 0 {
		r.cond.Wait()
	}

	avail := r.lenUnlocked()
	if k <= 0 || k > avail {
		k = avail
	}
	i := int(r.r & r.mask)
	max1 := len(r.buf) - i

	if k <= max1 {
		s1 = r.buf[i : i+k]
		// s2 = nil
	} else {
		n1 := max1
		n2 := k - n1
		s1 = r.buf[i : i+n1]
		s2 = r.buf[:n2]
	}
	r.mu.Unlock()
	return
}

// PeekAll2Block blocks until at least one byte is available, then returns all
// currently available bytes as at most two slices (s2 will be nil if contiguous).
// Zero-alloc; SPSC only. Returned slices are invalid after the next mutation.
func (r *RingBuffer) PeekAll2Block() (s1, s2 []byte) {
	r.mu.Lock()
	for r.lenUnlocked() == 0 {
		r.cond.Wait()
	}

	avail := r.lenUnlocked()
	i := int(r.r & r.mask)
	max1 := len(r.buf) - i

	if avail <= max1 {
		s1 = r.buf[i : i+avail]
		// s2 = nil
	} else {
		s1 = r.buf[i:]
		s2 = r.buf[:avail-max1]
	}
	r.mu.Unlock()
	return
}

// Peek2BlockAtLeast blocks until at least k bytes are available, then returns
// exactly k bytes starting at the current read index, possibly split across wrap.
// Returned slices alias ring storage and are invalid after the next mutation.
// Zero-alloc; SPSC only.
func (r *RingBuffer) Peek2BlockAtLeast(k int) (s1, s2 []byte, err error) {
	if k <= 0 {
		return nil, nil, ErrPeekInvalidLen
	}
	if k > len(r.buf) {
		return nil, nil, ErrPeekExceedsCapacity
	}

	r.mu.Lock()
	for r.lenUnlocked() < k {
		r.cond.Wait() // releases + re-acquires r.mu
	}
	i := int(r.r & r.mask)
	max1 := len(r.buf) - i
	if k <= max1 {
		s1, s2 = r.buf[i:i+k], nil
	} else {
		n1 := max1
		n2 := k - n1
		s1, s2 = r.buf[i:i+n1], r.buf[:n2]
	}
	r.mu.Unlock()
	return s1, s2, nil
}

// Peek2BlockAtLeastCtx is a context-aware version of Peek2BlockAtLeast.
func (r *RingBuffer) Peek2BlockAtLeastCtx(ctx context.Context, k int) (s1, s2 []byte, err error) {
	if k <= 0 {
		return nil, nil, ErrPeekInvalidLen
	}
	if k > len(r.buf) {
		return nil, nil, ErrPeekExceedsCapacity
	}

	r.mu.Lock()
	for r.lenUnlocked() < k {
		// Wait in a loop; cond.Wait() is zero-alloc and handles spurious wakeups.
		// To observe ctx cancellation, release lock, check ctx, then retry.
		r.cond.Wait()
		if ctx != nil {
			select {
			case <-ctx.Done():
				r.mu.Unlock()
				return nil, nil, ctx.Err()
			default:
			}
		}
	}
	i := int(r.r & r.mask)
	max1 := len(r.buf) - i
	if k <= max1 {
		s1, s2 = r.buf[i:i+k], nil
	} else {
		n1 := max1
		n2 := k - n1
		s1, s2 = r.buf[i:i+n1], r.buf[:n2]
	}
	r.mu.Unlock()
	return s1, s2, nil
}

// PeekByteAt returns the byte located `rel` bytes after the current read index.
// If rel >= Len(), it returns (0, false). Zero-alloc; SPSC only.
func (r *RingBuffer) PeekByteAt(rel int) (byte, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rel < 0 || rel >= r.lenUnlocked() {
		return 0, false // out of range
	}
	i := int((r.r + uint64(rel)) & r.mask)
	return r.buf[i], true
}

// IndexByte returns the zero-based offset of the first unread occurrence of b
// relative to the current read position, or -1 if not present in the unread region.
// Zero-alloc; SPSC only.
func (r *RingBuffer) IndexByte(b byte) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.indexByteUnlocked(b)
}

// IndexByteBlock blocks until byte b appears in the unread region and returns
// its zero-based offset from the current read position. Zero-alloc; SPSC only.
// WARNING: If the producer stops and b never arrives, this blocks indefinitely.
func (r *RingBuffer) IndexByteBlock(b byte) int {
	r.mu.Lock()
	for {
		if off := r.indexByteUnlocked(b); off >= 0 {
			r.mu.Unlock()
			return off
		}
		// wait for either producer Commit (more data) or consumer Advance (unlikely here)
		r.cond.Wait()
	}
}

// IndexByteBlockCtx is like IndexByteBlock but returns an error if ctx is cancelled.
func (r *RingBuffer) IndexByteBlockCtx(ctx context.Context, b byte) (int, error) {
	r.mu.Lock()
	for {
		if off := r.indexByteUnlocked(b); off >= 0 {
			r.mu.Unlock()
			return off, nil
		}
		// Wait and also observe ctx cancellation
		done := make(chan struct{})
		go func() { r.cond.Wait(); close(done) }()
		r.mu.Unlock()

		select {
		case <-ctx.Done():
			// ensure waiter goroutine is not leaked
			<-done
			return -1, ctx.Err()
		case <-done:
			// try again
		}
		r.mu.Lock()
	}
}

// --- helper: requires r.mu held ---
func (r *RingBuffer) indexByteUnlocked(b byte) int {
	avail := r.lenUnlocked()
	if avail == 0 {
		return -1
	}
	i := int(r.r & r.mask)
	// first contiguous run
	max1 := len(r.buf) - i
	n1 := avail
	if n1 > max1 {
		n1 = max1
	}
	if j := bytes.IndexByte(r.buf[i:i+n1], b); j >= 0 {
		return j
	}
	// wrapped run (if any)
	n2 := avail - n1
	if n2 > 0 {
		if j := bytes.IndexByte(r.buf[:n2], b); j >= 0 {
			return n1 + j
		}
	}
	return -1
}
