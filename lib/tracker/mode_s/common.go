package mode_s

// var format string = "%20s = %13s"

// decode an AC12 altitude field
func (f *Frame) decodeAC12Field(aC12Field int32) int32 {
	f.acQ = (aC12Field & 0x10) == 0x10
	var n int32

	if f.acQ {
		// log.Printf(format, "Q Bit Set", strconv.FormatInt(int64(AC12Field), 2))
		/// N is the 11 bit integer resulting from the removal of bit Q at bit 4
		n = ((aC12Field & 0x0FE0) >> 1) | (aC12Field & 0x000F)
		// The final altitude is the resulting number multiplied by 25, minus 1000.

		return (n * 25) - 1000
	}
	// Make N a 13 bit Gillham coded altitude by inserting M=0 at bit 6
	n = ((aC12Field & 0x0FC0) << 1) | (aC12Field & 0x003F)
	// log.Printf(format, "Q Bit Clear", strconv.FormatInt(int64(n), 2))
	n = modeAToModeC(decodeID13Field(n))
	if n < -12 {
		n = 0
	}
	return 100 * n
}

func decodeID13Field(ID13Field int32) int32 {
	var hexGillham int32
	// log.Printf(format, "Decoding ID13 Field", strconv.FormatInt(int64(ID13Field), 2))

	if 0 < (ID13Field & 0x1000) {
		hexGillham |= 0x0010
	} // Bit 12 = C1
	if 0 < (ID13Field & 0x0800) {
		hexGillham |= 0x1000
	} // Bit 11 = A1
	if 0 < (ID13Field & 0x0400) {
		hexGillham |= 0x0020
	} // Bit 10 = C2
	if 0 < (ID13Field & 0x0200) {
		hexGillham |= 0x2000
	} // Bit  9 = A2
	if 0 < (ID13Field & 0x0100) {
		hexGillham |= 0x0040
	} // Bit  8 = C4
	if 0 < (ID13Field & 0x0080) {
		hexGillham |= 0x4000
	} // Bit  7 = A4
	//if (ID13Field & 0x0040) {hexGillham |= 0x0800;} // Bit  6 = X  or M
	if 0 < (ID13Field & 0x0020) {
		hexGillham |= 0x0100
	} // Bit  5 = B1
	if 0 < (ID13Field & 0x0010) {
		hexGillham |= 0x0001
	} // Bit  4 = D1 or Q
	if 0 < (ID13Field & 0x0008) {
		hexGillham |= 0x0200
	} // Bit  3 = B2
	if 0 < (ID13Field & 0x0004) {
		hexGillham |= 0x0002
	} // Bit  2 = D2
	if 0 < (ID13Field & 0x0002) {
		hexGillham |= 0x0400
	} // Bit  1 = B4
	if 0 < (ID13Field & 0x0001) {
		hexGillham |= 0x0004
	} // Bit  0 = D4
	//log.Printf(format, "Decoded ID13 Field", strconv.FormatInt(int64(hexGillham), 2))

	return hexGillham
}

// Mode A to Mode C Height/altitude
func modeAToModeC(ModeA int32) int32 {
	var OneHundreds, FiveHundreds int32
	//log.Printf(format, "Mode A -> C", strconv.FormatInt(int64(ModeA), 2))
	//log.Printf(format, "Mask 1", strconv.FormatInt(int64(0x0FFF8889), 2))
	//log.Printf(format, "Mask 2", strconv.FormatInt(int64(0x000000F0), 2))

	if (ModeA&0x0FFF8889) > 0 || ((ModeA & 0x000000F0) == 0) {
		// check zero bits are zero, D1 set is illegal || C1,,C4 cannot be Zero
		return -9999
	}

	if (ModeA & 0x0010) > 0 {
		OneHundreds ^= 0x007
	} // C1
	if (ModeA & 0x0020) > 0 {
		OneHundreds ^= 0x003
	} // C2
	if (ModeA & 0x0040) > 0 {
		OneHundreds ^= 0x001
	} // C4

	// Remove 7s from OneHundreds (Make 7->5, snd 5->7).
	if (OneHundreds & 5) == 5 {
		OneHundreds ^= 2
	}

	// Check for invalid codes, only 1 to 5 are valid
	if OneHundreds > 5 {
		return -9999
	}

	// if (ModeA & 0x0001) {FiveHundreds ^= 0x1FF;} // D1 never used for altitude
	if (ModeA & 0x0002) > 0 {
		FiveHundreds ^= 0x0FF
	} // D2
	if (ModeA & 0x0004) > 0 {
		FiveHundreds ^= 0x07F
	} // D4

	if (ModeA & 0x1000) > 0 {
		FiveHundreds ^= 0x03F
	} // A1
	if (ModeA & 0x2000) > 0 {
		FiveHundreds ^= 0x01F
	} // A2
	if (ModeA & 0x4000) > 0 {
		FiveHundreds ^= 0x00F
	} // A4

	if (ModeA & 0x0100) > 0 {
		FiveHundreds ^= 0x007
	} // B1
	if (ModeA & 0x0200) > 0 {
		FiveHundreds ^= 0x003
	} // B2
	if (ModeA & 0x0400) > 0 {
		FiveHundreds ^= 0x001
	} // B4

	// Correct order of OneHundreds.
	if (FiveHundreds & 1) > 0 {
		OneHundreds = 6 - OneHundreds
	}

	result := (FiveHundreds * 5) + OneHundreds - 13
	// log.Printf(format, "Converted", strconv.FormatInt(int64(result), 2))
	return result
}
