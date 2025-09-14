package beast

import (
	"errors"
	"plane.watch/lib/tracker2/modes2"
)

type (
	// Frame represents our Beast frame and is used to decode into AVR
	Frame struct {
		raw           []byte
		mlatTimestamp []byte
		body          []byte
		msgType       byte
		signalLevel   byte
		bodyString    string

		isRadarCape  bool
		hasDecoded   bool
		isPool       bool
		decodedModeS modes2.AvrFrame
	}
)

var (
	magicTimestampMLAT     = []byte{0xFF, 0x00, 0x4D, 0x4C, 0x41, 0x54}
	ErrBadBeastFrame       = errors.New("bad beast frame")
	ErrUnhandledBeastFrame = errors.New("unhandled Beast Frame")
	ErrNilFrame            = errors.New("nil message")
)

// var msgLenLookup = map[byte]int{
//	0x31: 2,
//	0x32: 7,
//	0x33: 14,
//	0x34: 2,
// }

func Decode(rawBytes []byte) (modes2.AvrFrame, error) {
	if nil == rawBytes {
		return modes2.AvrFrame{}, ErrNilFrame
	}
	l := len(rawBytes)
	if !(l == 11 || l == 16 || l == 23) {
		return modes2.AvrFrame{}, ErrBadBeastFrame
	}

	msgType := rawBytes[1] + 0
	// mlatTimestamp = rawBytes[2:8]
	// signalLevel := rawBytes[8] + 0
	// body := rawBytes[9:]

	var (
		err   error
		frame modes2.AvrFrame
	)

	switch msgType {
	case 0x31:
		// decodeModeAc message
		err = ErrUnhandledBeastFrame

	case 0x32:
		// 0x32 = mode-s short 15 bytes
		frame, err = modes2.FromBytes56(rawBytes[9:]), nil
		frame.SignalLevel = rawBytes[8] + 0
	case 0x33:
		// 0x33 = mode-s long 22 bytes
		frame, err = modes2.FromBytes112(rawBytes[9:]), nil
		frame.SignalLevel = rawBytes[8] + 0
	case 0x34:
		// decodeConfig
		err = ErrUnhandledBeastFrame
	default:
		err = ErrUnhandledBeastFrame
	}
	return frame, err
}
