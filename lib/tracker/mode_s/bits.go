package mode_s

// bitsAt returns bits first..last of msg as an unsigned integer. Bits are
// numbered from 1 at the most significant bit of the first byte, inclusive at
// both ends, matching the numbering in DO-260B and ICAO Doc 9871.
func bitsAt(msg []byte, first, last int) uint64 {
	var v uint64
	for b := first; b <= last; b++ {
		v = v<<1 | uint64(msg[(b-1)/8]>>(7-(b-1)%8)&1)
	}
	return v
}

// signedBitsAt reads a sign bit at signBit followed by the magnitude bits
// signBit+1..last, as two's complement: a set sign bit subtracts 2^n, where n
// is the number of magnitude bits.
func signedBitsAt(msg []byte, signBit, last int) int64 {
	v := int64(bitsAt(msg, signBit+1, last))
	if bitsAt(msg, signBit, signBit) == 1 {
		v -= 1 << (last - signBit)
	}
	return v
}

// field reads value bits first..last guarded by status bit `status`. It
// returns ok=false when the status is clear but the value is not zero, which
// rules the register out.
func field(mb []byte, status, first, last int) (raw uint64, present, ok bool) {
	raw = bitsAt(mb, first, last)
	if bitsAt(mb, status, status) == 0 {
		return 0, false, raw == 0
	}
	return raw, true, true
}

// signedField is field with a sign bit at first and magnitude first+1..last.
func signedField(mb []byte, status, first, last int) (v int64, present, ok bool) {
	if bitsAt(mb, status, status) == 0 {
		return 0, false, bitsAt(mb, first, last) == 0
	}
	return signedBitsAt(mb, first, last), true, true
}
