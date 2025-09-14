package modes2

var (
	modesChecksumTable [256]uint64
)

const modesGeneratorPoly uint64 = 0xfff409

func init() {
	var i uint64
	var j int

	for i = 0; i < 256; i++ {
		var c = i << 16

		for j = 0; j < 8; j++ {
			if c&0x800000 != 0 {
				c = (c << 1) ^ modesGeneratorPoly
			} else {
				c <<= 1
			}
		}

		modesChecksumTable[i] = c & 0x00ffffff
	}
}

//
// func (f *Frame) decodeModeSChecksum() uint32 {
//	var n = f.getMessageLengthBytes()
//	var i, index uint32
//
//	var checkSum uint32
//	for i = 0; i < n-3; i++ {
//		index = uint32(f.message[i]) ^ ((f.checkSum & 0xff0000) >> 16)
//		f.checkSum = (f.checkSum << 8) ^ modesChecksumTable[index]
//		f.checkSum = f.checkSum & 0xffffff
//	}
//
//	f.checkSum = f.checkSum ^ (uint32(f.message[n-3]) << 16) ^ (uint32(f.message[n-2]) << 8) ^ uint32(f.message[n-1])
//
//	return checkSum
// }
// func (f *Frame) decodeModeSChecksumAddr() uint32 {
//	var n = f.getMessageLengthBytes()
//	var i, index uint32
//
//	msg := make([]byte, len(f.message))
//	copy(msg, f.message)
//	msg[n-3] = 0
//	msg[n-2] = 0
//	msg[n-1] = 0
//	var checkSum uint32
//	for i = 0; i < n-3; i++ {
//		index = uint32(msg[i]) ^ ((checkSum & 0xff_00_00) >> 16)
//		checkSum = (checkSum << 8) ^ modesChecksumTable[index]
//		checkSum = checkSum & 0xff_ff_ff
//	}
//
//	checkSum = checkSum ^ (uint32(msg[n-3]) << 16) ^ (uint32(msg[n-2]) << 8) ^ uint32(msg[n-1])
//
//	crc := uint32(f.message[n-3])<<16 | uint32(f.message[n-2])<<8 | uint32(f.message[n-1])
//
//	return checkSum ^ crc
// }
//
// func (f *Frame) checkCrc() error {
//	if "MLAT" == f.mode {
//		// not currently able to messageCrc beast AVR timestamp format messages
//		return nil
//	}
//	switch f.downLinkFormat {
//	case 0, 4, 5, 16, 20, 21, 24:
//		// decoding/checking CRC here is tricky. Field Type AP
//		return nil
//	case 11, 17, 18: // Field Type PI
//		f.checkSum = f.decodeModeSChecksum()
//		if 0 == f.checkSum {
//			return nil
//		}
//		return fmt.Errorf("invalid messageCrc for DF %d (%s)", f.downLinkFormat, f.raw)
//	default:
//		return fmt.Errorf("do not know how to CRC Downlink Format %d", f.downLinkFormat)
//	}
// }
