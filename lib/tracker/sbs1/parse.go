package sbs1

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	sbsMsgTypeField      = 0
	sbsMsgSubCatField    = 1 // transmission type
	sbsSessionDBID       = 2
	sbsAircraftDBID      = 3
	sbsIcaoField         = 4
	sbsFlightDBID        = 5
	sbsRecvDate          = 6
	sbsRecvTime          = 7
	sbsDateLogged        = 8
	sbsTimeLogged        = 9
	sbsCallsignField     = 10
	sbsAltitudeField     = 11
	sbsGroundSpeedField  = 12
	sbsTrackField        = 13 // Aircraft Track, not heading
	sbsLatField          = 14
	sbsLonField          = 15
	sbsVerticalRateField = 16
	sbsSquawkField       = 17
	sbsAlertSquawkField  = 18 // Flag to indicate squawk has changed.
	sbsEmergencyField    = 19 // Flag to indicate emergency code has been set
	sbsSpiIdentField     = 20 // Flag to indicate transponder Ident has been activated.
	sbsOnGroundField     = 21
)

type Frame struct {
	// original is our unadulterated string
	MsgType      string
	original     string
	icaoStr      string
	IcaoInt      uint32
	Received     time.Time
	CallSign     string
	Altitude     int
	GroundSpeed  int
	Track        float64
	Lat, Lon     float64
	VerticalRate int
	Squawk       string
	Alert        string
	Emergency    string
	SpiFlag      bool
	OnGround     bool

	HasPosition bool
}

var (
	ErrNoIcao      = errors.New("no ICAO presented")
	ErrInvalidIcao = errors.New("failed to decode ICAO HEX into uint32")
)

func NewFrame(sbsString string) *Frame {
	return &Frame{
		original: strings.TrimSpace(sbsString),
	}
}

func (f *Frame) TimeStamp() time.Time {
	return f.Received
}

func getField(fields []string, fieldId int) string {
	if len(fields) >= fieldId {
		return fields[fieldId]
	}
	return ""
}

func (f *Frame) Parse() error {
	// decode the string
	var err error

	if f.original == "" {
		// Handle NoOp
		return nil
	}

	fields := strings.Split(f.original, ",")
	if len(fields) > 22 {
		return fmt.Errorf("failed to parse input - too many parameters: %s", f.original)
	}
	// ignore 0 length
	if len(fields) == 0 {
		return nil
	}
	// ignore a blank field field
	if getField(fields, sbsMsgTypeField) == "" {
		return errors.New("unknown msg format, expecting a value in first field")
	}

	f.icaoStr = getField(fields, sbsIcaoField)
	f.IcaoInt, err = icaoStringToInt(getField(fields, sbsIcaoField))
	if nil != err {
		return err
	}
	sTime := getField(fields, sbsRecvDate) + " " + getField(fields, sbsRecvTime)
	// 2016/06/03 00:00:38.350
	f.Received, err = time.Parse("2006/01/02 15:04:05.999999999", sTime)
	if nil != err {
		f.Received = time.Now()
	}

	f.MsgType = getField(fields, sbsMsgTypeField)

	switch getField(fields, sbsMsgTypeField) { // message type
	case "SEL": // SELECTION_CHANGE
		f.CallSign = getField(fields, sbsCallsignField)
	case "ID": // NEW_ID
		f.CallSign = getField(fields, sbsCallsignField)
	case "AIR": // NEW_AIRCRAFT - just indicates when a new aircraft pops up
	case "STA": // STATUS_AIRCRAFT
	// call sign field (10) contains one of:
	//	PL (Position Lost)
	// 	SL (Signal Lost)
	// 	RM (Remove)
	// 	AD (Delete)
	// 	OK (used to reset time-outs if aircraft returns into cover).
	case "CLK": // CLICK
	case "MSG": // TRANSMISSION
		switch getField(fields, sbsMsgSubCatField) {
		case "1": // ES Identification and Category
			f.CallSign = getField(fields, sbsCallsignField)

		case "2": // ES Surface Position Message
			f.Altitude, _ = strconv.Atoi(getField(fields, sbsAltitudeField))
			f.GroundSpeed, _ = strconv.Atoi(getField(fields, sbsGroundSpeedField))
			f.Track, _ = strconv.ParseFloat(getField(fields, sbsTrackField), 32)
			f.Lat, _ = strconv.ParseFloat(getField(fields, sbsLatField), 32)
			f.Lon, _ = strconv.ParseFloat(getField(fields, sbsLonField), 32)
			f.HasPosition = true
			f.OnGround = getField(fields, sbsOnGroundField) == "-1"

		case "3": // ES Airborne Position Message
			f.Altitude, _ = strconv.Atoi(getField(fields, sbsAltitudeField))
			f.Lat, _ = strconv.ParseFloat(getField(fields, sbsLatField), 64)
			f.Lon, _ = strconv.ParseFloat(getField(fields, sbsLonField), 64)
			f.HasPosition = true
			f.Alert = getField(fields, sbsAlertSquawkField)
			f.Emergency = getField(fields, sbsEmergencyField)
			f.SpiFlag = getField(fields, sbsSpiIdentField) == "-1"
			f.OnGround = getField(fields, sbsOnGroundField) == "-1"

		case "4": // ES Airborne velocity Message
			f.GroundSpeed, _ = strconv.Atoi(getField(fields, sbsGroundSpeedField))
			f.Track, _ = strconv.ParseFloat(getField(fields, sbsTrackField), 32)
			f.VerticalRate, _ = strconv.Atoi(getField(fields, sbsVerticalRateField))
			f.OnGround = false // getField(fields, sbsOnGroundField) == "-1"

		case "5": // Surveillance Alt Message
			f.Altitude, _ = strconv.Atoi(getField(fields, sbsAltitudeField))
			f.Alert = getField(fields, sbsAlertSquawkField)
			f.OnGround = getField(fields, sbsOnGroundField) == "-1"
			f.SpiFlag = getField(fields, sbsSpiIdentField) == "-1"
			f.CallSign = getField(fields, sbsCallsignField)

		case "6": // Surveillance ID Message
			f.CallSign = getField(fields, sbsCallsignField)
			f.Altitude, _ = strconv.Atoi(getField(fields, sbsAltitudeField))
			f.Squawk = getField(fields, sbsSquawkField)
			f.Alert = getField(fields, sbsAlertSquawkField)
			f.Emergency = getField(fields, sbsEmergencyField)
			f.OnGround = getField(fields, sbsOnGroundField) == "-1"
			f.SpiFlag = getField(fields, sbsSpiIdentField) == "-1"
		// SPI Flag Ignored

		case "7": // Air To Air Message
			f.Altitude, _ = strconv.Atoi(getField(fields, sbsAltitudeField))
			f.OnGround = getField(fields, sbsOnGroundField) == "-1"

		case "8": // All Call Reply
			f.OnGround = getField(fields, sbsOnGroundField) == "-1"
		}
	default:
		return errors.New("unknown msg type, it is probably not SBS1")
	}

	return nil
}

func icaoStringToInt(icao string) (uint32, error) {
	if icao == "" {
		return 0, ErrNoIcao
	}

	btoi, err := hex.DecodeString(icao)
	if nil != err {
		return 0, fmt.Errorf("%w: (%s) - %w", ErrInvalidIcao, icao, err)
	}

	return uint32(btoi[0])<<16 | uint32(btoi[1])<<8 | uint32(btoi[2]), nil
}

func (f *Frame) Icao() uint32 {
	if nil == f {
		return 0
	}
	return f.IcaoInt
}
func (f *Frame) IcaoStr() string {
	return f.icaoStr
}

func (f *Frame) Decode() error {
	return f.Parse()
}

func (f *Frame) Raw() []byte {
	return []byte(f.original)
}
