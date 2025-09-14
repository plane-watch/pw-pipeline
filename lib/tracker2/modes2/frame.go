// package modes2
package modes2

import (
	"fmt"
	"math"
	"strconv"
	"unsafe"
)

const ChecksumUnknown uint64 = 0xFF_FF_FF_FF_FF_FF_FF_FF

type AvrFrame struct {
	a, b        uint64
	checkSum    uint64 // the messages calculated crc check sum  (non zero for msg's that overlay)
	len         int
	DF          byte
	SignalLevel byte
}

func fromBytes112Unsafe(buf []byte) AvrFrame {
	// this is a cheaper operation than the one commented out above
	// go build -gcflags='-m=2 -l=4' .
	// says this is cost 67, the one above is cost 90 (and thus is not inlineable)
	// but, benchmarking shows the safe method is faster
	arrA := [8]byte{buf[7], buf[6], buf[5], buf[4], buf[3], buf[2], buf[1], buf[0]}
	arrB := [8]byte{buf[13], buf[12], buf[11], buf[10], buf[9], buf[8], 0, 0}

	frame := AvrFrame{
		a:        *(*uint64)(unsafe.Pointer(&arrA)),
		b:        *(*uint64)(unsafe.Pointer(&arrB)) << 16,
		len:      len(buf),
		DF:       0,
		checkSum: ChecksumUnknown,
	}
	frame.DF = frame.DownlinkFormat()
	frame.checkSum = frame.Checksum()

	return frame
}

func FromBytes112(buf []byte) AvrFrame {
	if len(buf) != 14 {
		return AvrFrame{}
	}
	frame := AvrFrame{
		a:   uint64(buf[0])<<56 | uint64(buf[1])<<48 | uint64(buf[2])<<40 | uint64(buf[3])<<32 | uint64(buf[4])<<24 | uint64(buf[5])<<16 | uint64(buf[6])<<8 | uint64(buf[7]),
		b:   uint64(buf[8])<<56 | uint64(buf[9])<<48 | uint64(buf[10])<<40 | uint64(buf[11])<<32 | uint64(buf[12])<<24 | uint64(buf[13])<<16,
		len: len(buf),
	}

	frame.DF = frame.DownlinkFormat()
	frame.checkSum = frame.Checksum()

	return frame
}

func FromBytes56(buf []byte) AvrFrame {
	if len(buf) != 7 {
		return AvrFrame{}
	}
	frame := AvrFrame{
		a:   uint64(buf[0])<<56 | uint64(buf[1])<<48 | uint64(buf[2])<<40 | uint64(buf[3])<<32 | uint64(buf[4])<<24 | uint64(buf[5])<<16 | uint64(buf[6])<<8,
		b:   0,
		len: len(buf),
	}
	frame.DF = frame.DownlinkFormat()
	frame.checkSum = frame.Checksum()
	return frame
}

func FromHexString(s string) (AvrFrame, error) {
	frame := AvrFrame{
		a:   0,
		b:   0,
		len: len(s),
	}
	var err error
	if len(s) == 7 {
		frame.a, err = strconv.ParseUint(s[0:7], 16, 64)
	} else if len(s) == 14 {
		frame.b, err = strconv.ParseUint(s[7:14], 16, 64)
	}

	frame.DF = frame.DownlinkFormat()
	frame.checkSum = frame.Checksum()

	return frame, err
}

func (a AvrFrame) ByteLen() int {
	return a.len
}

// String returns an uppercase Hex representation of the AVR Frame
func (a AvrFrame) String() string {
	if a.len == 7 {
		return fmt.Sprintf("%014X", a.a>>8)
	}
	return fmt.Sprintf("%016X%12X", a.a, a.b>>16)
}

func (a AvrFrame) SignalRssi() float64 {
	return 10 * math.Log10(float64(a.SignalLevel))
}
