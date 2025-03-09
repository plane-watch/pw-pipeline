package modes2

import "fmt"

func decodeExample() {
	frame, _ := FromHexString("00050319AB8C22")

	switch frame.DF {
	case DF00ShortAirToAir:
		df0, _ := frame.DecodeDF0()
		fmt.Printf("Aircraft [icao] %06X, onGround=%t altitude=%d\n", df0.ICAO, df0.OnGround, df0.Altitude)
	case DF04SurveillanceAltitudeReply:
	case DF05SurveillanceIdentReply:
	case DF11ModeSAllCallReply:
	case DF16LongAirToAir:
	case DF17ADSBExtendedSquitter:
	case DF18ADSBSupplementary:
	case DF19ADSBMilitary:
	case DF20CommB:
	case DF21CommB:
	case DF22Military:
	case DF24CommD:
	}
}
