// package modes2
package main

import (
	"fmt"
	"unsafe"
)

const ChecksumUnknown uint64 = 0xFF_FF_FF_FF_FF_FF_FF_FF

type AvrFrame struct {
	a, b     uint64
	len      int
	checkSum uint64 // the messages calculated crc check sum  (non zero for msg's that overlay)
	df       byte
}

func FromBytes112(buf []byte) AvrFrame {
	// try and get the cost down, currently 100, need < 80
	// return AvrFrame{
	//	a:   uint64(buf[0])<<56 | uint64(buf[1])<<48 | uint64(buf[2])<<40 | uint64(buf[3])<<32 | uint64(buf[4])<<24 | uint64(buf[5])<<16 | uint64(buf[6])<<8 | uint64(buf[7]),
	//	b:   uint64(buf[8])<<56 | uint64(buf[9])<<48 | uint64(buf[10])<<40 | uint64(buf[11])<<32 | uint64(buf[12])<<24 | uint64(buf[13])<<16,
	//	len: len(buf),
	// }

	// this is a cheaper operation than the one commented out above
	// go build -gcflags='-m=2 -l=4' .
	// says this is cost 67, the one above is cost 90 (and thus is not inlineable)
	arrA := [8]byte{buf[7], buf[6], buf[5], buf[4], buf[3], buf[2], buf[1], buf[0]}
	arrB := [8]byte{buf[13], buf[12], buf[11], buf[10], buf[9], buf[8], 0, 0}

	frame := AvrFrame{
		a:        *(*uint64)(unsafe.Pointer(&arrA)),
		b:        *(*uint64)(unsafe.Pointer(&arrB)) << 16,
		len:      len(buf),
		df:       0,
		checkSum: ChecksumUnknown,
	}
	frame.df = frame.DownlinkFormat()
	frame.checkSum = frame.Checksum()

	return frame
}

func FromBytes56(buf []byte) AvrFrame {
	frame := AvrFrame{
		a:   uint64(buf[0])<<56 | uint64(buf[1])<<48 | uint64(buf[2])<<40 | uint64(buf[3])<<32 | uint64(buf[4])<<24 | uint64(buf[5])<<16 | uint64(buf[6])<<8,
		b:   0,
		len: len(buf),
	}
	frame.df = frame.DownlinkFormat()
	frame.checkSum = frame.Checksum()
	return frame
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

func main() {
	f := FromBytes112([]byte{0x8D, 0x76, 0xCE, 0x88, 0x20, 0x4C, 0x90, 0x72, 0xCB, 0x48, 0x20, 0x9A, 0x50, 0x4D})

	if f.ChecksumValid() {
		println("checksum is valid")
	} else {
		println("checksum is invalid")
	}

	println(f.String())
	println(f.DownlinkFormat())
}
