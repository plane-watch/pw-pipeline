package beast

import (
	"encoding/hex"
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

		// raw represents the entire BEAST message, including the first 0x1a byte
		raw []byte

		// mlatTimestamp represents the 6-byte MLAT timestamp from the BEAST message
		mlatTimestamp []byte

		// body represents the message payload, Mode S long/short or Mode AC data
		body []byte

		// msgType represents the encapsulated message type:
		//  - 0x31 = 2 byte Mode-AC
		//  - 0x32 = 7 byte Mode-S short frame
		//  - 0x33 = 14 byte Mode-S long frame
		//  - 0x34 = DIP switch configuration settings, time stamp error ticks as int8_t (1 tick is 15ns) (message "4" not on Mode-S Beast classic)
		msgType byte

		// signalLevel represents the received signal level for Mode-AC and Mode-S messages.
		//
		// To get the actual dBFS value:
		//  - The raw 0-255 byte value is converted to 0.0 - 1.0 (by dividing bt 255).
		//  - It should then be squared, base 10 log'd, and multiplied by 10.
		signalLevel byte

		// bodyString is the string representation of the Mode-AC/S message encapsulated in the BEAST frame.
		// It is populated when Frame.RawString is run.
		bodyString string

		// todo(mikenye): isRadarCape appears to be unused. It is set in newFrameInto but then never read?
		isRadarCape bool

		// hasDecoded indicates whether the Mode-AC/S message encapsulated in the BEAST frame has been decoded.
		hasDecoded bool

		// decodedModeS contains the decoded Mode-S frame (if the msgType is 0x32 or 0x33 - Mode-S short or Mode-S long).
		decodedModeS mode_s.Frame

		// epochID identifies which MLAT epoch this frame belongs to.
		// Used for sub-producer isolation when multiple receivers are aggregated.
		epochID uint32
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
	ErrModeAC          = errors.New("beast message is ModeAC")
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
				epochID:       0,
			}
		},
	}
}

func Release(frame *Frame) {
	if UsePoolAllocator {
		// clear frame before returning to the pool
		clear(frame.raw)
		frame.msgType = 0
		clear(frame.mlatTimestamp)
		frame.signalLevel = 0
		clear(frame.body)
		frame.bodyString = "                            " // 28 chars to fit 112bit squitters
		frame.isRadarCape = false
		frame.hasDecoded = false
		frame.decodedModeS = mode_s.Frame{}
		frame.epochID = 0
		// return to pool
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

// SetEpochID sets the MLAT epoch ID for this frame
func (f *Frame) SetEpochID(id uint32) {
	f.epochID = id
}

// EpochID returns the MLAT epoch ID for this frame
func (f *Frame) EpochID() uint32 {
	return f.epochID
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
	f.msgType = rawBytes[1]
	f.mlatTimestamp = rawBytes[2:8]
	f.signalLevel = rawBytes[8]
	f.body = rawBytes[9:]
	f.bodyString = "" // Reset for lazy computation in RawString()
	//copy(f.body[:], rawBytes[9:])

	f.isRadarCape = isRadarCape

	switch f.msgType {
	case 0x31:
		//if len(f.body) != 2 {
		//	return nil
		//}
		// mode-ac 10 bytes (2+8)
		f.decodeModeAc()
		return f, ErrModeAC
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

// BeastTicksNs returns the number of nanoseconds since the Beast receiver powered on.
// MLAT timestamps are in 1/12 microsecond increments per Beast format specification.
// Conversion: ticks * (1000 nanoseconds / 12) = ticks in nanoseconds
func (f *Frame) BeastTicksNs() time.Duration {
	var t uint64
	inc := 40
	for i := 0; i < 6; i++ {
		t |= uint64(f.mlatTimestamp[i]) << inc
		inc -= 8
	}

	if f.isRadarCape {
		// RadarCape may use different scaling, keep as-is for now
		return time.Duration(t)
	}

	// Standard Beast: convert from 1/12 microsecond ticks to nanoseconds
	// Formula: ticks * (1e9 / 12) = ticks * 1000 / 12 (integer division safe here)
	return time.Duration(t * 1000 / 12)
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
		f.bodyString = hex.EncodeToString(f.body)
	}
	return f.bodyString
}

// SignalRssi returns the Received Signal Strength Indicator expressed in dBFS (decibels relative to full scale),
// it means the signal strength is being measured relative to the maximum possible level the receiver’s
// analogue-to-digital converter (ADC) can represent.
func (f *Frame) SignalRssi() float64 {
	// we get the raw 0-255 byte value
	rawRSSI := float64(f.signalLevel)

	// we scale it to 0.0 - 1.0 (voltage = rawRSSI / 255)
	RSSIRatio := rawRSSI / 255

	// we convert it to a dBFS power value (rolling the squaring of the voltage into the dB calculation)
	signalLevel := RSSIRatio * RSSIRatio
	RSSI := 10 * math.Log10(signalLevel)

	return RSSI
}
