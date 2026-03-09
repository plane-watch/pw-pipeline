package mode_s

// extended squitter decoding

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
)

func (f *Frame) decodeAdsbLatLon() {
	var msg6 = int(f.message[6])
	var msg7 = int(f.message[7])
	var msg8 = int(f.message[8])
	var msg9 = int(f.message[9])
	var msg10 = int(f.message[10])

	// CPR LAT/LON
	f.rawLatitude = ((msg6 & 0x03) << 15) | (msg7 << 7) | (msg8 >> 1)
	f.rawLongitude = ((msg8 & 0x01) << 16) | (msg9 << 8) | msg10
	f.cprFlagOddEven = int(msg6&0x04) >> 2
}

func (f *Frame) decodeAdsb() error {
	// Down Link Format 17 Message Types
	f.messageType = f.message[4] >> 3
	f.messageSubType = f.message[4] & 7

	switch f.messageType {
	case 1, 2, 3, 4:
		/* Aircraft Identification and Category */
		f.decodeFlightNumber()

		f.catType = 4 - f.messageType
		f.catSubType = f.message[4] & 7
		f.catValid = true
		f.messageSubType = 0
	case 5, 6, 7, 8:
		// surface position
		f.onGround = true
		f.validVerticalStatus = true
		f.messageSubType = 0

		f.decodeAdsbLatLon()
		f.decodeSurfaceMovementField()

		// if the 4th bit is set, the heading is valid
		if f.message[5]&0x08 != 0 {
			f.heading = float64(((f.message[5]<<4)|(f.message[6]>>4))&0x007F) * 360.0 / 128.0
			f.validHeading = true
		}

	case 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 20, 21, 22:
		f.isGnssAlt = f.messageType >= 20
		/* Airborne position Message */
		f.messageSubType = 0
		f.timeFlag = int(f.message[6] & (1 << 3))
		f.onGround = false
		f.validVerticalStatus = true
		f.surveillanceStatus = (f.message[4] & 0x06) >> 1
		f.nicSupplementB = f.message[4] & 0x01

		field := ((int32(f.message[5]) << 4) | (int32(f.message[6]) >> 4)) & 0x0FFF
		if f.isGnssAlt {
			// decimal value
			if f.unit == modesUnitFeet {
				f.altitude = int32(float64(field) * 3.28084)
			} else {
				f.altitude = field
			}
		} else {
			f.altitude = f.decodeAC12Field(field)
		}
		f.validAltitude = f.altitude != 0
		f.decodeAdsbLatLon()

	case 19:
		/* Airborne velocity Message */
		f.onGround = false
		f.validVerticalStatus = true

		f.intentChange = (f.message[5] & 0x80) >> 7
		f.ifrCapability = (f.message[5] & 0x40) >> 6
		f.validNacV = true
		f.nacV = (f.message[5] & 0x38) >> 3

		var verticalRateSign = int((f.message[8] & 0x8) >> 3)
		f.verticalRate = (int(f.message[8]&7) << 6) | (int(f.message[9]&0xfc) >> 2)
		if f.verticalRate != 0 {
			f.verticalRate--
			if verticalRateSign != 0 {
				f.verticalRate = 0 - f.verticalRate
			}
			f.verticalRate *= 64
			f.validVerticalRate = true
		}

		if f.messageSubType == 1 || f.messageSubType == 2 {

			// speed over Ground Message
			f.eastWestDirection = int(f.message[5]&4) >> 2
			f.northSouthDirection = int((f.message[7] & 0x80) >> 7)

			// Source bit for vertical rate
			//   0: GNSS, 1: Barometer
			f.verticalRateSource = int((f.message[8] & 0x10) >> 4)

			// Sub-type 1: Speed = Decimal value - 1
			f.eastWestVelocity = ((int(f.message[5]&3) << 8) | int(f.message[6])) - 1
			f.northSouthVelocity = ((int(f.message[7]&0x7f) << 3) | (int(f.message[8]&0xe0) >> 5)) - 1

			/* Compute velocity and angle from the two speed components. */

			// Sub-type 2: Speed = 4 x (Decimal value - 1)
			if f.messageSubType == 2 {
				// supersonic - unit is 4 knots.

				// for unsigned integers or non-negative values, `<<= 2` is equivalent to `*= 4` but faster.
				f.eastWestVelocity <<= 2
				f.northSouthVelocity <<= 2

				f.superSonic = true
			}

			f.velocity = math.Sqrt(float64((f.northSouthVelocity * f.northSouthVelocity) + (f.eastWestVelocity * f.eastWestVelocity)))
			f.validVelocity = true

			if f.velocity != 0 {
				var heading float64
				f.eastWestVelocity--
				f.northSouthVelocity--
				if f.eastWestDirection != 0 {
					// GO WEST! (0=east, 1=west)
					f.eastWestVelocity *= -1
				}
				if f.northSouthDirection != 0 {
					// Going Down South! (0=north, 1=south)
					f.northSouthVelocity *= -1
				}
				heading = math.Atan2(float64(f.eastWestVelocity), float64(f.northSouthVelocity))
				/* Convert to degrees. */
				f.heading = heading * 360 / (math.Pi * 2)
				/* We don't want negative values but a 0-360 scale. */
				if f.heading < 0 {
					f.heading += 360
				}
				f.validHeading = true
			} else {
				f.heading = 0
			}
		} else if f.messageSubType == 3 || f.messageSubType == 4 {
			// Air Speed -- ground speed not available
			var airspeed = int(((f.message[7] & 0x7f) << 3) | (f.message[8] >> 5))
			if airspeed != 0 {
				airspeed--
				if f.messageSubType == 4 {
					// If (supersonic) unit is 4 kts
					f.superSonic = true
					airspeed <<= 2
				}
				f.velocity = float64(airspeed)
				f.validVelocity = true
			}

			if f.message[5]&4 != 0 {
				f.heading = (360.0 / 128.0) * float64(((f.message[5]&3)<<5)|(f.message[6]>>3))
				f.validHeading = true
			}
		}

		if f.message[10] > 0 {
			f.validHae = true
			f.haeDirection = (f.message[10] & 0x80) >> 7
			var multiplier = -25
			if f.haeDirection == 0 {
				multiplier = 25
			}
			f.haeDelta = multiplier * int((f.message[10]&0x7f)-1)
		}
	case 23:
		switch f.messageSubType {
		case 0: // test message
		case 1, 2, 3, 4, 5, 6: // Reserved
		case 7: // Allocated for national use
			// TEST MESSAGE with  squawk - decode it!
			return f.decodeSquawkIdentity(5, 6)
		}

	case 24:
	// Surface System Status Messages
	// NoOp
	// subType=1 is for Multilateration System Status (Allocated for national use)
	// this is a per system manufacturer message
	case 25, 26:
		// RESERVED
	case 27:
		// ADS-B Messages with TYPE Code=27 are Reserved for future expansion of these MOPS to specify Trajectory Change Message formats.
	case 28:
		switch f.messageSubType {
		case 0: // reserved
		case 1: // Emergency/priority status (§B.2.3.8)
			// EMERGENCY (or priority), EMERGENCY, THERE'S AN EMERGENCY GOING ON
			// ME bits 9,10,11 contain the emergency code (1-8 are TC/SUB) then EID bits (this)
			// ME starts at byte 5 (TC/SUB), EID is first 3 bits of byte 6
			f.emergencyID = int((f.message[5] & 0b1110_0000) >> 5)
			f.alert = f.emergencyID != 0
			f.emergency = emergencyStateTable[f.emergencyID]

			f.decodeSquawkIdentity(5, 6)

			// can get the Mode A Address too
			// mode_a_code = (short) (msg[2]|((msg[1]&0x1F)<<8));
		case 2:
		// ACAS RA broadcast
		case 3, 4, 5, 6, 7: // RESERVED
		}
	case 29:
		// Target State and Status Message
		// DO-260 - unused
		// DO-260A = Target State and Status Information Message
		// DO-260B =
		f.messageSubType = f.message[4] & 6 >> 1

		if f.messageSubType == 0 {
			// DO-260A
			// bit 39-41  Vertical Data Available / Source Indicator
			// bit 41-42  Target Altitude Type
			// bit 42-43  Backward Compatibility Flag - must be 0
			// bit 43-45  Target Altitude Capability
			// bit 45-47  Vertical Mode Indicator
			// bit 47-57  Target Altitude
			// bit 57-59  Horizontal Data Available
			// bit 59-68  Target Heading / Track Angle
			// bit 68-69  Target Heading / Track Indicator
			// bit 69-71  Horizontal Mode Indicator
			// bit 71-75  Navigation Accuracy Category — Position (NACP)
			// bit 75-76  Navigation Integrity Category — Baro (NICBARO)
			// bit 76-78  Surveillance Integrity Level (SIL)
			// bit 78-83  Reserved
			// bit 83-85  Capability / Mode Codes
			// bit 85-88  Emergency / Priority Status
			f.emergencyID = int(f.message[10] & 0x7)
			f.alert = f.emergencyID != 0
			f.emergency = emergencyStateTable[f.emergencyID]

		} else if f.messageSubType == 1 {
			// DO-260B
			// bit 40    SIL supplement (SIL Per Hour or Per Sample)
			// bit 41    Selected Alt Type
			// bit 42-52 MCP/FCU Selected Altitude OR FMS Selected Altitude
			// bit 53-61 Barometric Pressure Setting (minus 800 millibars)
			// bit 62    Selected Heading Status
			// bit 63    Selected Heading Sign
			// bit 64-71 Selected Heading
			// bit 72-75 NACp (Navigation Accuracy Category_Position)
			// bit 76    NICbaro (Navigation Integrity Category_Baro)
			// bit 77-78 SIL (Source Integrity Level)
			// bit 79    MCP/FPU Status
			// bit 80    Autopilot Engaged
			// bit 81    VNAV Mode Engaged
			// bit 82    Altitude Hold Mode
			// bit 83    Reserved for ADS-R Flag
			// bit 84    Approach Mode
			// bit 85    TCAS Operational
			// bit 86-88 Reserved
		}
	case 30:
	// NoOp
	case 31:
		// Operational status Message
		// TODO: Finish this off - it is not in a good working state

		// bool pointer helper
		bp := func(b bool) *bool { return &b }

		if f.messageSubType == 0 {
			// on the ground!
			f.validVerticalStatus = true
			f.onGround = false

			f.compatibilityClass = int(f.message[5])<<8 | int(f.message[6])
			if f.compatibilityClass&0xC000 == 0 {
				f.cccHasOperationalTcas = bp((f.compatibilityClass & 0x2000) != 0)
				f.cccHasAirRefVel = bp((f.compatibilityClass & 0x200) != 0)
				f.cccHasTargetStateRpt = bp((f.compatibilityClass & 0x100) != 0)

				changeRpt := f.compatibilityClass & 0xC0
				f.cccHasTargetChangeRpt = bp(changeRpt == 1 || changeRpt == 2)
				f.cccHasUATReceiver = (f.compatibilityClass & 0x20) != 0
			}

		} else if f.messageSubType == 1 {
			f.validVerticalStatus = true
			f.onGround = true
			f.compatibilityClass = int(f.message[5])<<4 | int(f.message[6]&0xF0)>>4
			f.airframeWidthLen = f.message[6] & 0x0F

			if f.compatibilityClass&0xC000 == 0 {
				f.cccHasLowTxPower = bp((f.compatibilityClass & 0x200) != 0)
				f.cccHasUATReceiver = (f.compatibilityClass & 0x100) != 0
				f.validNacV = true
				f.nacV = byte((f.compatibilityClass & 0xE0) >> 5)
				f.nicSupplementC = byte((f.compatibilityClass & 0x10) >> 4)
			}
		}
		if f.compatibilityClass&0xC000 == 0 {
			f.validCompatibilityClass = true
			f.cccHas1090EsIn = (f.compatibilityClass & 0x1000) != 0
		}

		f.operationalModeCode = int(f.message[7])<<8 | int(f.message[8])
		f.adsbVersion = (f.message[9] & 0xe0) >> 5
		f.nicSupplementA = f.message[9] & 0x10 >> 4

		f.nacP = f.message[9] & 0x0F
		f.geoVertAccuracy = f.message[10] & 0xC0 >> 6
		f.sil = f.message[10] & 0x30 >> 4
		f.nicCrossCheck = f.message[10] & 0x08 >> 3
		f.northReference = f.message[10] & 0x04 >> 2
	}

	return nil
}

func (f *Frame) bitString() string {
	var bits string
	for _, msg := range f.message {
		bits += fmt.Sprintf("%08b", msg)
	}
	return bits
}

func (f *Frame) decodeInto(bitStream string, t interface{}) error {
	valParentPtr := reflect.ValueOf(t)
	if reflect.Ptr != valParentPtr.Kind() || valParentPtr.IsNil() {
		return errors.New("you need to pass in a pointer to your struct")
	}
	valParent := valParentPtr.Elem()
	if !valParent.CanAddr() || !valParent.IsValid() || !valParent.CanSet() {
		return errors.New("cannot edit struct")
	}
	tType := reflect.TypeOf(t).Elem()
	if tType.Kind() != reflect.Struct {
		return errors.New("you need to pass in a struct with all the struct tags (bits,name,desc)")
	}

	for i := 0; i < tType.NumField(); i++ {
		field := tType.Field(i)

		bits := field.Tag.Get("bits")

		splitBits := strings.SplitN(bits, "-", 2)
		if len(splitBits) != 2 {
			println("Incorrect Struct Tag `bits`")
		}
		low, err := strconv.ParseUint(splitBits[0], 10, 8)
		if nil != err {
			return fmt.Errorf("incorrect Struct Tag `bits` low (%s) needs to be a number", splitBits[0])
		}
		high, err := strconv.ParseUint(splitBits[1], 10, 8)
		if nil != err {
			return fmt.Errorf("incorrect Struct Tag `bits` high (%s) needs to be a number", splitBits[1])
		}
		fieldBits := bitStream[low:high]
		ii, err := strconv.ParseUint(fieldBits, 2, 64)

		valField := valParent.Field(i)
		if !valField.IsValid() || !valField.CanSet() {
			err = fmt.Errorf("cannot Set Field %s", field.Name)
		}
		valField.SetUint(ii)
	}
	return nil
}

func (f *Frame) decodeAdsb2() (*extendedSquitter, error) {

	// Down Link Format 17 Message Types
	f.messageType = f.message[4] >> 3
	f.messageSubType = f.message[4] & 7
	bitStream := f.bitString()

	var err error
	var es extendedSquitter

	err = f.decodeInto(bitStream, &es)

	switch f.messageType {
	case 1, 2, 3, 4:
		err = f.decodeInto(bitStream, &es.df17me1)
	case 19:
		switch f.messageSubType {
		case 1, 2:
			err = f.decodeInto(bitStream, &es.df17me19t1)
		case 3, 4:
			err = f.decodeInto(bitStream, &es.df17me19t3)
		}
	case 28:
		switch f.messageSubType {
		case 1:
			err = f.decodeInto(bitStream, &es.df17me28t1)
		case 2:
			err = f.decodeInto(bitStream, &es.df17me28t2)
		}
	case 29:
		switch f.messageSubType {
		case 0:
			// DO-260A
			err = f.decodeInto(bitStream, &es.df17me29t0)
		case 1:
			// DO-260B
			err = f.decodeInto(bitStream, &es.df17me29t1)
		default:
			err = f.decodeInto(bitStream, &es.df17me29t2)
		}
	case 31:
		switch f.messageSubType {
		case 0:
			err = f.decodeInto(bitStream, &es.df17me31t0)
		case 1:
			err = f.decodeInto(bitStream, &es.df17me31t1)
		}
	default:
	}
	return &es, err
}

// =========================================================================
//
// Decode the 7 bit ground movement field PWL exponential style scale
func (f *Frame) decodeSurfaceMovementField() {
	movement := uint64(((f.message[4] << 4) | (f.message[5] >> 4)) & 0x007F)

	f.velocity, f.validVelocity = calcSurfaceSpeed(movement)
}

func calcSurfaceSpeed(value uint64) (float64, bool) {
	var gSpeed float64
	var validVelocity bool
	if (value > 0) && (value < 125) {
		validVelocity = true
		if value > 123 {
			gSpeed = 175 // > 175kt
		} else if value > 108 { // 109-123 - 5 kt steps
			gSpeed = float64((value-109)*5) + 100.0

		} else if value > 93 { // 94 - 108 | 70kt - <100kt
			gSpeed = float64((value-94)*2) + 70

		} else if value > 38 { // 39 - 93 | 15kt - <70kt | 1kt step
			gSpeed = float64(value-39) + 15

		} else if value > 12 { // 13-38 |  2 kt - <15kt | 0.5 kt steps
			gSpeed = float64(value-13)*0.5 + 2

		} else if value > 8 { // 9-12 | 1kt - < 2kt | 0.25 kt steps
			gSpeed = float64(value-9)*0.25 + 1

		} else if value > 1 {
			gSpeed = float64(value-1) * 0.125

		} else if value == 1 {
			// stopped
			gSpeed = 0
		} else {
			gSpeed = 0
			validVelocity = false
		}
	}
	return gSpeed, validVelocity
}
