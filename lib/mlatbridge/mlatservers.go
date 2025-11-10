package mlatbridge

import "plane.watch/lib/icaoregion"

func MLATServer(r icaoregion.Region) string {
	switch r {
	case icaoregion.AFI:
		return "mlat-server-afi:12346"
	case icaoregion.ASIA:
		return "mlat-server-asia:12346"
	case icaoregion.CAR:
		return "mlat-server-car:12346"
	case icaoregion.EUR:
		return "mlat-server-eur:12346"
	case icaoregion.MID:
		return "mlat-server-mid:12346"
	case icaoregion.NAM:
		return "mlat-server-nam:12346"
	case icaoregion.NAT:
		return "mlat-server-nat:12346"
	case icaoregion.SAM:
		return "mlat-server-sam:12346"
	default:
		return "mlat-server-unknown:12346"
	}
}
