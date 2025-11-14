package beast

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"plane.watch/lib/tracker/mode_s"
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
		decodedModeS mode_s.Frame
	}
)

var (
	// UsePoolAllocator when set to true will allocate Frame objects out of a sync.Pool. you will need to free them
	// by calling beast.Release()
	UsePoolAllocator = false
	beastPool        sync.Pool

	magicTimestampMLAT = []byte{0xFF, 0x00, 0x4D, 0x4C, 0x41, 0x54}
	ErrBadBeastFrame   = errors.New("bad beast frame")
	ErrConfigFrame     = errors.New("beast message is config data")
)

func init() {
	beastPool = sync.Pool{
		New: func() any {
			return &Frame{
				raw:           make([]byte, 0, 30),
				msgType:       0,
				mlatTimestamp: make([]byte, 0, 6),
				signalLevel:   0,
				body:          make([]byte, 0, 14),
				bodyString:    "                            ", // 28 chars to fit 112bit squitters
				isRadarCape:   false,
				hasDecoded:    false,
				decodedModeS:  mode_s.Frame{},
			}
		},
	}
}

func Release(frame *Frame) {
	if UsePoolAllocator {
		beastPool.Put(frame)
	}
}

// var msgLenLookup = map[byte]int{
//	0x31: 2,
//	0x32: 7,
//	0x33: 14,
//	0x34: 2,
// }

// Icao returns the airframes ICAO code as an int
func (f *Frame) Icao() uint32 {
	if nil == f {
		return 0
	}
	if !f.hasDecoded {
		_ = f.Decode()
	}
	return f.decodedModeS.Icao()
}

// IcaoStr returns the airframes ICAO code as a readable string
func (f *Frame) IcaoStr() string {
	if nil == f {
		return ""
	}
	if !f.hasDecoded {
		_ = f.Decode()
	}
	return f.decodedModeS.IcaoStr()
}

// Decode is used to turn our beast msg into our mode_s.Frame representation
func (f *Frame) Decode() error {
	if nil == f {
		return errors.New("nil frame")
	}
	if f.hasDecoded {
		return nil
	}
	if f.msgType == 0x32 || f.msgType == 0x33 {
		err := f.decodedModeS.Decode()
		if nil == err {
			f.hasDecoded = true
		}
		return err
	}
	f.hasDecoded = true
	return mode_s.ErrNoOp
}

func (f *Frame) TimeStamp() time.Time {
	// todo: calculate this off the mlat timestamp
	return time.Now()
}

// Raw gives us back our raw beast message
func (f *Frame) Raw() []byte {
	if nil == f {
		return []byte{}
	}
	return f.raw
}

func NewFrame(rawBytes []byte, isRadarCape bool) (*Frame, error) {
	if UsePoolAllocator {
		return newFrameInto(beastPool.Get().(*Frame), rawBytes, isRadarCape)
	} else {
		return newFrameInto(&Frame{}, rawBytes, isRadarCape)
	}
}
func newFrameInto(f *Frame, rawBytes []byte, isRadarCape bool) (*Frame, error) {
	if len(rawBytes) <= 8 {
		return f, ErrBadBeastFrame
	}
	// decode beast into AVR
	if rawBytes[0] != 0x1A {
		// invalid frame
		return f, ErrBadBeastFrame
	}
	if rawBytes[1] < 0x31 || rawBytes[1] > 0x34 {
		return f, ErrBadBeastFrame
	}

	// note: our parts here refer to the underlying slice that was passed in
	f.raw = rawBytes
	f.msgType = rawBytes[1] + 0
	f.mlatTimestamp = rawBytes[2:8]
	f.signalLevel = rawBytes[8]
	f.body = rawBytes[9:]
	//copy(f.body[:], rawBytes[9:])

	f.isRadarCape = isRadarCape

	switch f.msgType {
	case 0x31:
		//if len(f.body) != 2 {
		//	return nil
		//}
		// mode-ac 10 bytes (2+8)
		f.decodeModeAc()
	case 0x32, 0x33:
		// 0x32 = mode-s short 15 bytes
		// 0x33 = mode-s long 22 bytes
		f.decodedModeS = mode_s.NewFrameFromBytes(0, f.body, time.Now())
	case 0x34:
		//if len(f.body) != 2 {
		//	return nil
		//}
		// signal strength 10 bytes
		f.decodeConfig()
		return f, ErrConfigFrame
	default:
	}
	return f, nil
}

func (f *Frame) decodeModeAc() {
	// TODO: Decode ModeAC
}

func (f *Frame) decodeConfig() {
	// TODO: Decode RadarCape Config Info
}

// BeastTicksNs returns the number of nanoseconds the beast has been on for (the mlat timestamp is calculated from power on)
func (f *Frame) BeastTicksNs() time.Duration {
	var t uint64
	inc := 40
	for i := 0; i < 6; i++ {
		t |= uint64(f.mlatTimestamp[i]) << inc
		inc -= 8
	}
	return time.Duration(t * 500)
}

func (f *Frame) String() string {
	if nil == f {
		return ""
	}
	msgTypeString := map[byte]string{
		0x31: "MODE_AC",
		0x32: "MODE_S_SHORT",
		0x33: "MODE_S_LONG",
		0x34: "RADARCAPE_STATUS",
	}
	return fmt.Sprintf(
		"Type: %-16s, Time: %06X, Signal RSSI %0.1f dBFS, Data: %X",
		msgTypeString[f.msgType],
		f.mlatTimestamp,
		f.SignalRssi(),
		f.body,
	)
}

func (f *Frame) isMlat() bool {
	if nil == f {
		return false
	}
	for i, b := range magicTimestampMLAT {
		if b != f.raw[i+2] {
			return false
		}
	}
	return true
}

func (f *Frame) AvrFrame() *mode_s.Frame {
	if nil == f {
		return nil
	}
	if !f.hasDecoded {
		_ = f.Decode()
	}
	return &f.decodedModeS
}

func (f *Frame) AvrRaw() []byte {
	if nil == f {
		return nil
	}
	return f.body
}

func (f *Frame) RawString() string {
	if nil == f {
		return ""
	}

	if f.bodyString == "" {
		f.bodyString = fmt.Sprintf("%X", f.body)
	}

	return f.bodyString
}

func (f *Frame) SignalRssi() float64 {
	return 10 * math.Log10(float64(f.signalLevel))
}
