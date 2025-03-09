package tracker

import (
	"fmt"
	"testing"
	"time"
)

func TestFunkyLatLon(t *testing.T) {
	var plane *Plane
	var err error
	trk := NewTracker()
	plane = trk.GetPlane(7777)

	_ = plane.setCprEvenLocation(92095, 39846, time.Now())
	_, err = plane.cprLocation.decodeGlobalAir()
	if nil == err {
		t.Error("We should fail CPR decode with only an even location set")
	}
	plane.zeroCpr()

	_ = plane.setCprOddLocation(88385, 125818, time.Now())
	_, err = plane.cprLocation.decodeGlobalAir()
	if nil == err {
		t.Error("We should fail CPR decode with only an odd location set")
	}
	plane.zeroCpr()

	plane = trk.GetPlane(7777)
	_ = plane.setCprEvenLocation(92095, 39846, time.Now())
	_ = plane.setCprOddLocation(88385, 125818, time.Now())

	_, err = plane.cprLocation.decodeGlobalAir()
	if nil != err {
		t.Error("We should be able to decode with both odd and even CPR locations")
	}
}

func TestGetPlane(t *testing.T) {
	// fmt.Println("TestGetPlane")

	var plane *Plane
	var err error
	trk := NewTracker()
	planeListLen := trk.numPlanes()
	plane = trk.GetPlane(1234)

	if planeListLen == trk.numPlanes() {
		t.Error("Plane List should be longer")
	}

	if plane.IcaoIdentifier() != 1234 {
		t.Errorf("Expected planes ICAO identifier to be moo, got %d", plane.IcaoIdentifier())
	}

	plane = trk.GetPlane(1234)
	err = plane.setCprOddLocation(88385, 125818, time.Now())
	if nil != err {
		// there was an error
		t.Errorf("Unexpected error When decoding CPR: %s", err)
	}

	if plane.cprLocation.oddLat != 88385 {
		t.Errorf("Even Lat not recorded properly. expected 88385, got: %0.2f", plane.cprLocation.oddLat)
	}

	if plane.cprLocation.oddLon != 125818 {
		t.Errorf("Even Lon not recorded properly. expected 125818, got: %0.2f", plane.cprLocation.oddLon)
	}

	err = plane.setCprEvenLocation(92095, 39846, time.Now())
	if nil != err {
		// there was an error
		t.Errorf("Unexpected error When decoding CPR: %s", err)
	}

	if plane.cprLocation.evenLat != 92095 {
		t.Errorf("Even Lat not recorded properly. expected 92095, got: %0.2f", plane.cprLocation.evenLat)
	}

	if plane.cprLocation.evenLon != 39846 {
		t.Errorf("Even Lon not recorded properly. expected 39846, got: %0.2f", plane.cprLocation.evenLon)
	}

	plane = trk.GetPlane(1234)
	location, err := plane.cprLocation.decodeGlobalAir()

	// ensure the intermediary calculations are correct

	if plane.cprLocation.latitudeIndex != 1 {
		t.Errorf("Incorrect latitude index, expected 1 got %d", plane.cprLocation.latitudeIndex)
	}

	if fmt.Sprintf("%0.13f", plane.cprLocation.rlat0) != "10.2157745361328" {
		t.Errorf("Incorrect RLAT(0) calc, expected 10.2157745361328 - got: %0.13f", plane.cprLocation.rlat0)
	}

	if fmt.Sprintf("%0.13f", plane.cprLocation.rlat1) != "10.2162144547802" {
		t.Errorf("Incorrect RLAT(1) calc, expected 10.2162144547802 - got: %0.13f", plane.cprLocation.rlat1)
	}

	if nil != err {
		// there was an error
		t.Errorf("Unexpected error When decoding CPR: %s", err)
	}

	if fmt.Sprintf("%0.12f", location.longitude) != "123.889128586342" {
		t.Errorf("longitude Calculation was incorrect: expected 123.889128586342, got %0.12f", location.longitude)
	}
	if fmt.Sprintf("%0.13f", location.latitude) != "10.2162144547802" {
		t.Errorf("latitude Calculation was incorrect: expected 10.2162144547802, got %0.13f", location.latitude)
	}

	_ = plane.addLatLong(location.latitude, location.longitude, time.Now(), true)
}

func Test_headingInfo_getCompassLabel(t *testing.T) {
	type args struct {
		heading float64
	}
	tests := []struct {
		name string
		want string
		hi   headingInfo
		args args
	}{
		{name: "T0", hi: headingLookup, args: args{heading: 0}, want: "N"},
		{name: "T1", hi: headingLookup, args: args{heading: 1}, want: "N"},
		{name: "T2", hi: headingLookup, args: args{heading: 2}, want: "N"},
		{name: "T3", hi: headingLookup, args: args{heading: 3}, want: "N"},
		{name: "T4", hi: headingLookup, args: args{heading: 4}, want: "N"},
		{name: "T5", hi: headingLookup, args: args{heading: 5}, want: "N"},
		{name: "T6", hi: headingLookup, args: args{heading: 6}, want: "N"},
		{name: "T7", hi: headingLookup, args: args{heading: 7}, want: "N"},
		{name: "T8", hi: headingLookup, args: args{heading: 8}, want: "N"},
		{name: "T9", hi: headingLookup, args: args{heading: 9}, want: "N"},
		{name: "T10", hi: headingLookup, args: args{heading: 10}, want: "N"},
		{name: "T11", hi: headingLookup, args: args{heading: 11}, want: "N"},
		{name: "T12", hi: headingLookup, args: args{heading: 12}, want: "NNE"},
		{name: "T13", hi: headingLookup, args: args{heading: 13}, want: "NNE"},
		{name: "T14", hi: headingLookup, args: args{heading: 14}, want: "NNE"},
		{name: "T15", hi: headingLookup, args: args{heading: 15}, want: "NNE"},
		{name: "T16", hi: headingLookup, args: args{heading: 16}, want: "NNE"},
		{name: "T17", hi: headingLookup, args: args{heading: 17}, want: "NNE"},
		{name: "T18", hi: headingLookup, args: args{heading: 18}, want: "NNE"},
		{name: "T19", hi: headingLookup, args: args{heading: 19}, want: "NNE"},
		{name: "T20", hi: headingLookup, args: args{heading: 20}, want: "NNE"},
		{name: "T21", hi: headingLookup, args: args{heading: 21}, want: "NNE"},
		{name: "T22", hi: headingLookup, args: args{heading: 22}, want: "NNE"},
		{name: "T23", hi: headingLookup, args: args{heading: 23}, want: "NNE"},
		{name: "T24", hi: headingLookup, args: args{heading: 24}, want: "NNE"},
		{name: "T25", hi: headingLookup, args: args{heading: 25}, want: "NNE"},
		{name: "T26", hi: headingLookup, args: args{heading: 26}, want: "NNE"},
		{name: "T27", hi: headingLookup, args: args{heading: 27}, want: "NNE"},
		{name: "T28", hi: headingLookup, args: args{heading: 28}, want: "NNE"},
		{name: "T29", hi: headingLookup, args: args{heading: 29}, want: "NNE"},
		{name: "T30", hi: headingLookup, args: args{heading: 30}, want: "NNE"},
		{name: "T31", hi: headingLookup, args: args{heading: 31}, want: "NNE"},
		{name: "T32", hi: headingLookup, args: args{heading: 32}, want: "NNE"},
		{name: "T33", hi: headingLookup, args: args{heading: 33}, want: "NNE"},
		{name: "T34", hi: headingLookup, args: args{heading: 34}, want: "NE"},
		{name: "T35", hi: headingLookup, args: args{heading: 35}, want: "NE"},
		{name: "T36", hi: headingLookup, args: args{heading: 36}, want: "NE"},
		{name: "T37", hi: headingLookup, args: args{heading: 37}, want: "NE"},
		{name: "T38", hi: headingLookup, args: args{heading: 38}, want: "NE"},
		{name: "T39", hi: headingLookup, args: args{heading: 39}, want: "NE"},
		{name: "T40", hi: headingLookup, args: args{heading: 40}, want: "NE"},
		{name: "T41", hi: headingLookup, args: args{heading: 41}, want: "NE"},
		{name: "T42", hi: headingLookup, args: args{heading: 42}, want: "NE"},
		{name: "T43", hi: headingLookup, args: args{heading: 43}, want: "NE"},
		{name: "T44", hi: headingLookup, args: args{heading: 44}, want: "NE"},
		{name: "T45", hi: headingLookup, args: args{heading: 45}, want: "NE"},
		{name: "T46", hi: headingLookup, args: args{heading: 46}, want: "NE"},
		{name: "T47", hi: headingLookup, args: args{heading: 47}, want: "NE"},
		{name: "T48", hi: headingLookup, args: args{heading: 48}, want: "NE"},
		{name: "T49", hi: headingLookup, args: args{heading: 49}, want: "NE"},
		{name: "T50", hi: headingLookup, args: args{heading: 50}, want: "NE"},
		{name: "T51", hi: headingLookup, args: args{heading: 51}, want: "NE"},
		{name: "T52", hi: headingLookup, args: args{heading: 52}, want: "NE"},
		{name: "T53", hi: headingLookup, args: args{heading: 53}, want: "NE"},
		{name: "T54", hi: headingLookup, args: args{heading: 54}, want: "NE"},
		{name: "T55", hi: headingLookup, args: args{heading: 55}, want: "NE"},
		{name: "T56", hi: headingLookup, args: args{heading: 56}, want: "NE"},
		{name: "T57", hi: headingLookup, args: args{heading: 57}, want: "ENE"},
		{name: "T58", hi: headingLookup, args: args{heading: 58}, want: "ENE"},
		{name: "T59", hi: headingLookup, args: args{heading: 59}, want: "ENE"},
		{name: "T60", hi: headingLookup, args: args{heading: 60}, want: "ENE"},
		{name: "T61", hi: headingLookup, args: args{heading: 61}, want: "ENE"},
		{name: "T62", hi: headingLookup, args: args{heading: 62}, want: "ENE"},
		{name: "T63", hi: headingLookup, args: args{heading: 63}, want: "ENE"},
		{name: "T64", hi: headingLookup, args: args{heading: 64}, want: "ENE"},
		{name: "T65", hi: headingLookup, args: args{heading: 65}, want: "ENE"},
		{name: "T66", hi: headingLookup, args: args{heading: 66}, want: "ENE"},
		{name: "T67", hi: headingLookup, args: args{heading: 67}, want: "ENE"},
		{name: "T68", hi: headingLookup, args: args{heading: 68}, want: "ENE"},
		{name: "T69", hi: headingLookup, args: args{heading: 69}, want: "ENE"},
		{name: "T70", hi: headingLookup, args: args{heading: 70}, want: "ENE"},
		{name: "T71", hi: headingLookup, args: args{heading: 71}, want: "ENE"},
		{name: "T72", hi: headingLookup, args: args{heading: 72}, want: "ENE"},
		{name: "T73", hi: headingLookup, args: args{heading: 73}, want: "ENE"},
		{name: "T74", hi: headingLookup, args: args{heading: 74}, want: "ENE"},
		{name: "T75", hi: headingLookup, args: args{heading: 75}, want: "ENE"},
		{name: "T76", hi: headingLookup, args: args{heading: 76}, want: "ENE"},
		{name: "T77", hi: headingLookup, args: args{heading: 77}, want: "ENE"},
		{name: "T78", hi: headingLookup, args: args{heading: 78}, want: "ENE"},
		{name: "T79", hi: headingLookup, args: args{heading: 79}, want: "E"},
		{name: "T80", hi: headingLookup, args: args{heading: 80}, want: "E"},
		{name: "T81", hi: headingLookup, args: args{heading: 81}, want: "E"},
		{name: "T82", hi: headingLookup, args: args{heading: 82}, want: "E"},
		{name: "T83", hi: headingLookup, args: args{heading: 83}, want: "E"},
		{name: "T84", hi: headingLookup, args: args{heading: 84}, want: "E"},
		{name: "T85", hi: headingLookup, args: args{heading: 85}, want: "E"},
		{name: "T86", hi: headingLookup, args: args{heading: 86}, want: "E"},
		{name: "T87", hi: headingLookup, args: args{heading: 87}, want: "E"},
		{name: "T88", hi: headingLookup, args: args{heading: 88}, want: "E"},
		{name: "T89", hi: headingLookup, args: args{heading: 89}, want: "E"},
		{name: "T90", hi: headingLookup, args: args{heading: 90}, want: "E"},
		{name: "T91", hi: headingLookup, args: args{heading: 91}, want: "E"},
		{name: "T92", hi: headingLookup, args: args{heading: 92}, want: "E"},
		{name: "T93", hi: headingLookup, args: args{heading: 93}, want: "E"},
		{name: "T94", hi: headingLookup, args: args{heading: 94}, want: "E"},
		{name: "T95", hi: headingLookup, args: args{heading: 95}, want: "E"},
		{name: "T96", hi: headingLookup, args: args{heading: 96}, want: "E"},
		{name: "T97", hi: headingLookup, args: args{heading: 97}, want: "E"},
		{name: "T98", hi: headingLookup, args: args{heading: 98}, want: "E"},
		{name: "T99", hi: headingLookup, args: args{heading: 99}, want: "E"},
		{name: "T100", hi: headingLookup, args: args{heading: 100}, want: "E"},
		{name: "T101", hi: headingLookup, args: args{heading: 101}, want: "E"},
		{name: "T102", hi: headingLookup, args: args{heading: 102}, want: "ESE"},
		{name: "T103", hi: headingLookup, args: args{heading: 103}, want: "ESE"},
		{name: "T104", hi: headingLookup, args: args{heading: 104}, want: "ESE"},
		{name: "T105", hi: headingLookup, args: args{heading: 105}, want: "ESE"},
		{name: "T106", hi: headingLookup, args: args{heading: 106}, want: "ESE"},
		{name: "T107", hi: headingLookup, args: args{heading: 107}, want: "ESE"},
		{name: "T108", hi: headingLookup, args: args{heading: 108}, want: "ESE"},
		{name: "T109", hi: headingLookup, args: args{heading: 109}, want: "ESE"},
		{name: "T110", hi: headingLookup, args: args{heading: 110}, want: "ESE"},
		{name: "T111", hi: headingLookup, args: args{heading: 111}, want: "ESE"},
		{name: "T112", hi: headingLookup, args: args{heading: 112}, want: "ESE"},
		{name: "T113", hi: headingLookup, args: args{heading: 113}, want: "ESE"},
		{name: "T114", hi: headingLookup, args: args{heading: 114}, want: "ESE"},
		{name: "T115", hi: headingLookup, args: args{heading: 115}, want: "ESE"},
		{name: "T116", hi: headingLookup, args: args{heading: 116}, want: "ESE"},
		{name: "T117", hi: headingLookup, args: args{heading: 117}, want: "ESE"},
		{name: "T118", hi: headingLookup, args: args{heading: 118}, want: "ESE"},
		{name: "T119", hi: headingLookup, args: args{heading: 119}, want: "ESE"},
		{name: "T120", hi: headingLookup, args: args{heading: 120}, want: "ESE"},
		{name: "T121", hi: headingLookup, args: args{heading: 121}, want: "ESE"},
		{name: "T122", hi: headingLookup, args: args{heading: 122}, want: "ESE"},
		{name: "T123", hi: headingLookup, args: args{heading: 123}, want: "ESE"},
		{name: "T124", hi: headingLookup, args: args{heading: 124}, want: "SE"},
		{name: "T125", hi: headingLookup, args: args{heading: 125}, want: "SE"},
		{name: "T126", hi: headingLookup, args: args{heading: 126}, want: "SE"},
		{name: "T127", hi: headingLookup, args: args{heading: 127}, want: "SE"},
		{name: "T128", hi: headingLookup, args: args{heading: 128}, want: "SE"},
		{name: "T129", hi: headingLookup, args: args{heading: 129}, want: "SE"},
		{name: "T130", hi: headingLookup, args: args{heading: 130}, want: "SE"},
		{name: "T131", hi: headingLookup, args: args{heading: 131}, want: "SE"},
		{name: "T132", hi: headingLookup, args: args{heading: 132}, want: "SE"},
		{name: "T133", hi: headingLookup, args: args{heading: 133}, want: "SE"},
		{name: "T134", hi: headingLookup, args: args{heading: 134}, want: "SE"},
		{name: "T135", hi: headingLookup, args: args{heading: 135}, want: "SE"},
		{name: "T136", hi: headingLookup, args: args{heading: 136}, want: "SE"},
		{name: "T137", hi: headingLookup, args: args{heading: 137}, want: "SE"},
		{name: "T138", hi: headingLookup, args: args{heading: 138}, want: "SE"},
		{name: "T139", hi: headingLookup, args: args{heading: 139}, want: "SE"},
		{name: "T140", hi: headingLookup, args: args{heading: 140}, want: "SE"},
		{name: "T141", hi: headingLookup, args: args{heading: 141}, want: "SE"},
		{name: "T142", hi: headingLookup, args: args{heading: 142}, want: "SE"},
		{name: "T143", hi: headingLookup, args: args{heading: 143}, want: "SE"},
		{name: "T144", hi: headingLookup, args: args{heading: 144}, want: "SE"},
		{name: "T145", hi: headingLookup, args: args{heading: 145}, want: "SE"},
		{name: "T146", hi: headingLookup, args: args{heading: 146}, want: "SE"},
		{name: "T147", hi: headingLookup, args: args{heading: 147}, want: "SSE"},
		{name: "T148", hi: headingLookup, args: args{heading: 148}, want: "SSE"},
		{name: "T149", hi: headingLookup, args: args{heading: 149}, want: "SSE"},
		{name: "T150", hi: headingLookup, args: args{heading: 150}, want: "SSE"},
		{name: "T151", hi: headingLookup, args: args{heading: 151}, want: "SSE"},
		{name: "T152", hi: headingLookup, args: args{heading: 152}, want: "SSE"},
		{name: "T153", hi: headingLookup, args: args{heading: 153}, want: "SSE"},
		{name: "T154", hi: headingLookup, args: args{heading: 154}, want: "SSE"},
		{name: "T155", hi: headingLookup, args: args{heading: 155}, want: "SSE"},
		{name: "T156", hi: headingLookup, args: args{heading: 156}, want: "SSE"},
		{name: "T157", hi: headingLookup, args: args{heading: 157}, want: "SSE"},
		{name: "T158", hi: headingLookup, args: args{heading: 158}, want: "SSE"},
		{name: "T159", hi: headingLookup, args: args{heading: 159}, want: "SSE"},
		{name: "T160", hi: headingLookup, args: args{heading: 160}, want: "SSE"},
		{name: "T161", hi: headingLookup, args: args{heading: 161}, want: "SSE"},
		{name: "T162", hi: headingLookup, args: args{heading: 162}, want: "SSE"},
		{name: "T163", hi: headingLookup, args: args{heading: 163}, want: "SSE"},
		{name: "T164", hi: headingLookup, args: args{heading: 164}, want: "SSE"},
		{name: "T165", hi: headingLookup, args: args{heading: 165}, want: "SSE"},
		{name: "T166", hi: headingLookup, args: args{heading: 166}, want: "SSE"},
		{name: "T167", hi: headingLookup, args: args{heading: 167}, want: "SSE"},
		{name: "T168", hi: headingLookup, args: args{heading: 168}, want: "SSE"},
		{name: "T169", hi: headingLookup, args: args{heading: 169}, want: "S"},
		{name: "T170", hi: headingLookup, args: args{heading: 170}, want: "S"},
		{name: "T171", hi: headingLookup, args: args{heading: 171}, want: "S"},
		{name: "T172", hi: headingLookup, args: args{heading: 172}, want: "S"},
		{name: "T173", hi: headingLookup, args: args{heading: 173}, want: "S"},
		{name: "T174", hi: headingLookup, args: args{heading: 174}, want: "S"},
		{name: "T175", hi: headingLookup, args: args{heading: 175}, want: "S"},
		{name: "T176", hi: headingLookup, args: args{heading: 176}, want: "S"},
		{name: "T177", hi: headingLookup, args: args{heading: 177}, want: "S"},
		{name: "T178", hi: headingLookup, args: args{heading: 178}, want: "S"},
		{name: "T179", hi: headingLookup, args: args{heading: 179}, want: "S"},
		{name: "T180", hi: headingLookup, args: args{heading: 180}, want: "S"},
		{name: "T181", hi: headingLookup, args: args{heading: 181}, want: "S"},
		{name: "T182", hi: headingLookup, args: args{heading: 182}, want: "S"},
		{name: "T183", hi: headingLookup, args: args{heading: 183}, want: "S"},
		{name: "T184", hi: headingLookup, args: args{heading: 184}, want: "S"},
		{name: "T185", hi: headingLookup, args: args{heading: 185}, want: "S"},
		{name: "T186", hi: headingLookup, args: args{heading: 186}, want: "S"},
		{name: "T187", hi: headingLookup, args: args{heading: 187}, want: "S"},
		{name: "T188", hi: headingLookup, args: args{heading: 188}, want: "S"},
		{name: "T189", hi: headingLookup, args: args{heading: 189}, want: "S"},
		{name: "T190", hi: headingLookup, args: args{heading: 190}, want: "S"},
		{name: "T191", hi: headingLookup, args: args{heading: 191}, want: "S"},
		{name: "T192", hi: headingLookup, args: args{heading: 192}, want: "SSW"},
		{name: "T193", hi: headingLookup, args: args{heading: 193}, want: "SSW"},
		{name: "T194", hi: headingLookup, args: args{heading: 194}, want: "SSW"},
		{name: "T195", hi: headingLookup, args: args{heading: 195}, want: "SSW"},
		{name: "T196", hi: headingLookup, args: args{heading: 196}, want: "SSW"},
		{name: "T197", hi: headingLookup, args: args{heading: 197}, want: "SSW"},
		{name: "T198", hi: headingLookup, args: args{heading: 198}, want: "SSW"},
		{name: "T199", hi: headingLookup, args: args{heading: 199}, want: "SSW"},
		{name: "T200", hi: headingLookup, args: args{heading: 200}, want: "SSW"},
		{name: "T201", hi: headingLookup, args: args{heading: 201}, want: "SSW"},
		{name: "T202", hi: headingLookup, args: args{heading: 202}, want: "SSW"},
		{name: "T203", hi: headingLookup, args: args{heading: 203}, want: "SSW"},
		{name: "T204", hi: headingLookup, args: args{heading: 204}, want: "SSW"},
		{name: "T205", hi: headingLookup, args: args{heading: 205}, want: "SSW"},
		{name: "T206", hi: headingLookup, args: args{heading: 206}, want: "SSW"},
		{name: "T207", hi: headingLookup, args: args{heading: 207}, want: "SSW"},
		{name: "T208", hi: headingLookup, args: args{heading: 208}, want: "SSW"},
		{name: "T209", hi: headingLookup, args: args{heading: 209}, want: "SSW"},
		{name: "T210", hi: headingLookup, args: args{heading: 210}, want: "SSW"},
		{name: "T211", hi: headingLookup, args: args{heading: 211}, want: "SSW"},
		{name: "T212", hi: headingLookup, args: args{heading: 212}, want: "SSW"},
		{name: "T213", hi: headingLookup, args: args{heading: 213}, want: "SSW"},
		{name: "T214", hi: headingLookup, args: args{heading: 214}, want: "SW"},
		{name: "T215", hi: headingLookup, args: args{heading: 215}, want: "SW"},
		{name: "T216", hi: headingLookup, args: args{heading: 216}, want: "SW"},
		{name: "T217", hi: headingLookup, args: args{heading: 217}, want: "SW"},
		{name: "T218", hi: headingLookup, args: args{heading: 218}, want: "SW"},
		{name: "T219", hi: headingLookup, args: args{heading: 219}, want: "SW"},
		{name: "T220", hi: headingLookup, args: args{heading: 220}, want: "SW"},
		{name: "T221", hi: headingLookup, args: args{heading: 221}, want: "SW"},
		{name: "T222", hi: headingLookup, args: args{heading: 222}, want: "SW"},
		{name: "T223", hi: headingLookup, args: args{heading: 223}, want: "SW"},
		{name: "T224", hi: headingLookup, args: args{heading: 224}, want: "SW"},
		{name: "T225", hi: headingLookup, args: args{heading: 225}, want: "SW"},
		{name: "T226", hi: headingLookup, args: args{heading: 226}, want: "SW"},
		{name: "T227", hi: headingLookup, args: args{heading: 227}, want: "SW"},
		{name: "T228", hi: headingLookup, args: args{heading: 228}, want: "SW"},
		{name: "T229", hi: headingLookup, args: args{heading: 229}, want: "SW"},
		{name: "T230", hi: headingLookup, args: args{heading: 230}, want: "SW"},
		{name: "T231", hi: headingLookup, args: args{heading: 231}, want: "SW"},
		{name: "T232", hi: headingLookup, args: args{heading: 232}, want: "SW"},
		{name: "T233", hi: headingLookup, args: args{heading: 233}, want: "SW"},
		{name: "T234", hi: headingLookup, args: args{heading: 234}, want: "SW"},
		{name: "T235", hi: headingLookup, args: args{heading: 235}, want: "SW"},
		{name: "T236", hi: headingLookup, args: args{heading: 236}, want: "SW"},
		{name: "T237", hi: headingLookup, args: args{heading: 237}, want: "WSW"},
		{name: "T238", hi: headingLookup, args: args{heading: 238}, want: "WSW"},
		{name: "T239", hi: headingLookup, args: args{heading: 239}, want: "WSW"},
		{name: "T240", hi: headingLookup, args: args{heading: 240}, want: "WSW"},
		{name: "T241", hi: headingLookup, args: args{heading: 241}, want: "WSW"},
		{name: "T242", hi: headingLookup, args: args{heading: 242}, want: "WSW"},
		{name: "T243", hi: headingLookup, args: args{heading: 243}, want: "WSW"},
		{name: "T244", hi: headingLookup, args: args{heading: 244}, want: "WSW"},
		{name: "T245", hi: headingLookup, args: args{heading: 245}, want: "WSW"},
		{name: "T246", hi: headingLookup, args: args{heading: 246}, want: "WSW"},
		{name: "T247", hi: headingLookup, args: args{heading: 247}, want: "WSW"},
		{name: "T248", hi: headingLookup, args: args{heading: 248}, want: "WSW"},
		{name: "T249", hi: headingLookup, args: args{heading: 249}, want: "WSW"},
		{name: "T250", hi: headingLookup, args: args{heading: 250}, want: "WSW"},
		{name: "T251", hi: headingLookup, args: args{heading: 251}, want: "WSW"},
		{name: "T252", hi: headingLookup, args: args{heading: 252}, want: "WSW"},
		{name: "T253", hi: headingLookup, args: args{heading: 253}, want: "WSW"},
		{name: "T254", hi: headingLookup, args: args{heading: 254}, want: "WSW"},
		{name: "T255", hi: headingLookup, args: args{heading: 255}, want: "WSW"},
		{name: "T256", hi: headingLookup, args: args{heading: 256}, want: "WSW"},
		{name: "T257", hi: headingLookup, args: args{heading: 257}, want: "WSW"},
		{name: "T258", hi: headingLookup, args: args{heading: 258}, want: "WSW"},
		{name: "T259", hi: headingLookup, args: args{heading: 259}, want: "W"},
		{name: "T260", hi: headingLookup, args: args{heading: 260}, want: "W"},
		{name: "T261", hi: headingLookup, args: args{heading: 261}, want: "W"},
		{name: "T262", hi: headingLookup, args: args{heading: 262}, want: "W"},
		{name: "T263", hi: headingLookup, args: args{heading: 263}, want: "W"},
		{name: "T264", hi: headingLookup, args: args{heading: 264}, want: "W"},
		{name: "T265", hi: headingLookup, args: args{heading: 265}, want: "W"},
		{name: "T266", hi: headingLookup, args: args{heading: 266}, want: "W"},
		{name: "T267", hi: headingLookup, args: args{heading: 267}, want: "W"},
		{name: "T268", hi: headingLookup, args: args{heading: 268}, want: "W"},
		{name: "T269", hi: headingLookup, args: args{heading: 269}, want: "W"},
		{name: "T270", hi: headingLookup, args: args{heading: 270}, want: "W"},
		{name: "T271", hi: headingLookup, args: args{heading: 271}, want: "W"},
		{name: "T272", hi: headingLookup, args: args{heading: 272}, want: "W"},
		{name: "T273", hi: headingLookup, args: args{heading: 273}, want: "W"},
		{name: "T274", hi: headingLookup, args: args{heading: 274}, want: "W"},
		{name: "T275", hi: headingLookup, args: args{heading: 275}, want: "W"},
		{name: "T276", hi: headingLookup, args: args{heading: 276}, want: "W"},
		{name: "T277", hi: headingLookup, args: args{heading: 277}, want: "W"},
		{name: "T278", hi: headingLookup, args: args{heading: 278}, want: "W"},
		{name: "T279", hi: headingLookup, args: args{heading: 279}, want: "W"},
		{name: "T280", hi: headingLookup, args: args{heading: 280}, want: "W"},
		{name: "T281", hi: headingLookup, args: args{heading: 281}, want: "W"},
		{name: "T282", hi: headingLookup, args: args{heading: 282}, want: "WNW"},
		{name: "T283", hi: headingLookup, args: args{heading: 283}, want: "WNW"},
		{name: "T284", hi: headingLookup, args: args{heading: 284}, want: "WNW"},
		{name: "T285", hi: headingLookup, args: args{heading: 285}, want: "WNW"},
		{name: "T286", hi: headingLookup, args: args{heading: 286}, want: "WNW"},
		{name: "T287", hi: headingLookup, args: args{heading: 287}, want: "WNW"},
		{name: "T288", hi: headingLookup, args: args{heading: 288}, want: "WNW"},
		{name: "T289", hi: headingLookup, args: args{heading: 289}, want: "WNW"},
		{name: "T290", hi: headingLookup, args: args{heading: 290}, want: "WNW"},
		{name: "T291", hi: headingLookup, args: args{heading: 291}, want: "WNW"},
		{name: "T292", hi: headingLookup, args: args{heading: 292}, want: "WNW"},
		{name: "T293", hi: headingLookup, args: args{heading: 293}, want: "WNW"},
		{name: "T294", hi: headingLookup, args: args{heading: 294}, want: "WNW"},
		{name: "T295", hi: headingLookup, args: args{heading: 295}, want: "WNW"},
		{name: "T296", hi: headingLookup, args: args{heading: 296}, want: "WNW"},
		{name: "T297", hi: headingLookup, args: args{heading: 297}, want: "WNW"},
		{name: "T298", hi: headingLookup, args: args{heading: 298}, want: "WNW"},
		{name: "T299", hi: headingLookup, args: args{heading: 299}, want: "WNW"},
		{name: "T300", hi: headingLookup, args: args{heading: 300}, want: "WNW"},
		{name: "T301", hi: headingLookup, args: args{heading: 301}, want: "WNW"},
		{name: "T302", hi: headingLookup, args: args{heading: 302}, want: "WNW"},
		{name: "T303", hi: headingLookup, args: args{heading: 303}, want: "WNW"},
		{name: "T304", hi: headingLookup, args: args{heading: 304}, want: "NW"},
		{name: "T305", hi: headingLookup, args: args{heading: 305}, want: "NW"},
		{name: "T306", hi: headingLookup, args: args{heading: 306}, want: "NW"},
		{name: "T307", hi: headingLookup, args: args{heading: 307}, want: "NW"},
		{name: "T308", hi: headingLookup, args: args{heading: 308}, want: "NW"},
		{name: "T309", hi: headingLookup, args: args{heading: 309}, want: "NW"},
		{name: "T310", hi: headingLookup, args: args{heading: 310}, want: "NW"},
		{name: "T311", hi: headingLookup, args: args{heading: 311}, want: "NW"},
		{name: "T312", hi: headingLookup, args: args{heading: 312}, want: "NW"},
		{name: "T313", hi: headingLookup, args: args{heading: 313}, want: "NW"},
		{name: "T314", hi: headingLookup, args: args{heading: 314}, want: "NW"},
		{name: "T315", hi: headingLookup, args: args{heading: 315}, want: "NW"},
		{name: "T316", hi: headingLookup, args: args{heading: 316}, want: "NW"},
		{name: "T317", hi: headingLookup, args: args{heading: 317}, want: "NW"},
		{name: "T318", hi: headingLookup, args: args{heading: 318}, want: "NW"},
		{name: "T319", hi: headingLookup, args: args{heading: 319}, want: "NW"},
		{name: "T320", hi: headingLookup, args: args{heading: 320}, want: "NW"},
		{name: "T321", hi: headingLookup, args: args{heading: 321}, want: "NW"},
		{name: "T322", hi: headingLookup, args: args{heading: 322}, want: "NW"},
		{name: "T323", hi: headingLookup, args: args{heading: 323}, want: "NW"},
		{name: "T324", hi: headingLookup, args: args{heading: 324}, want: "NW"},
		{name: "T325", hi: headingLookup, args: args{heading: 325}, want: "NW"},
		{name: "T326", hi: headingLookup, args: args{heading: 326}, want: "NW"},
		{name: "T327", hi: headingLookup, args: args{heading: 327}, want: "NNW"},
		{name: "T328", hi: headingLookup, args: args{heading: 328}, want: "NNW"},
		{name: "T329", hi: headingLookup, args: args{heading: 329}, want: "NNW"},
		{name: "T330", hi: headingLookup, args: args{heading: 330}, want: "NNW"},
		{name: "T331", hi: headingLookup, args: args{heading: 331}, want: "NNW"},
		{name: "T332", hi: headingLookup, args: args{heading: 332}, want: "NNW"},
		{name: "T333", hi: headingLookup, args: args{heading: 333}, want: "NNW"},
		{name: "T334", hi: headingLookup, args: args{heading: 334}, want: "NNW"},
		{name: "T335", hi: headingLookup, args: args{heading: 335}, want: "NNW"},
		{name: "T336", hi: headingLookup, args: args{heading: 336}, want: "NNW"},
		{name: "T337", hi: headingLookup, args: args{heading: 337}, want: "NNW"},
		{name: "T338", hi: headingLookup, args: args{heading: 338}, want: "NNW"},
		{name: "T339", hi: headingLookup, args: args{heading: 339}, want: "NNW"},
		{name: "T340", hi: headingLookup, args: args{heading: 340}, want: "NNW"},
		{name: "T341", hi: headingLookup, args: args{heading: 341}, want: "NNW"},
		{name: "T342", hi: headingLookup, args: args{heading: 342}, want: "NNW"},
		{name: "T343", hi: headingLookup, args: args{heading: 343}, want: "NNW"},
		{name: "T344", hi: headingLookup, args: args{heading: 344}, want: "NNW"},
		{name: "T345", hi: headingLookup, args: args{heading: 345}, want: "NNW"},
		{name: "T346", hi: headingLookup, args: args{heading: 346}, want: "NNW"},
		{name: "T347", hi: headingLookup, args: args{heading: 347}, want: "NNW"},
		{name: "T348", hi: headingLookup, args: args{heading: 348}, want: "NNW"},
		{name: "T349", hi: headingLookup, args: args{heading: 349}, want: "N"},
		{name: "T350", hi: headingLookup, args: args{heading: 350}, want: "N"},
		{name: "T351", hi: headingLookup, args: args{heading: 351}, want: "N"},
		{name: "T352", hi: headingLookup, args: args{heading: 352}, want: "N"},
		{name: "T353", hi: headingLookup, args: args{heading: 353}, want: "N"},
		{name: "T354", hi: headingLookup, args: args{heading: 354}, want: "N"},
		{name: "T355", hi: headingLookup, args: args{heading: 355}, want: "N"},
		{name: "T356", hi: headingLookup, args: args{heading: 356}, want: "N"},
		{name: "T357", hi: headingLookup, args: args{heading: 357}, want: "N"},
		{name: "T358", hi: headingLookup, args: args{heading: 358}, want: "N"},
		{name: "T359", hi: headingLookup, args: args{heading: 359}, want: "N"},
		{name: "T360", hi: headingLookup, args: args{heading: 360}, want: "N"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.hi.getCompassLabel(tt.args.heading); got != tt.want {
				t.Errorf("getCompassLabel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlane_setGroundStatus(t *testing.T) {
	trk := NewTracker()
	plane := trk.GetPlane(7778)
	n := time.Now()
	plane.setGroundStatus(false, n)
	if plane.OnGround() {
		t.Errorf("Failed to set on ground status to false")
	}

	plane.setVerticalRate(10, n)
	if plane.VerticalRate() != 10 {
		t.Errorf("Failed to set veritical rate correctly")
	}

	plane.setGroundStatus(true, n)
	if !plane.OnGround() {
		t.Errorf("plane should be on ground")
	}
	if plane.VerticalRate() != 0 {
		t.Errorf("Vertical rate should have been 0")
	}
}

func TestPlane_Altitude(t *testing.T) {
	trk := NewTracker()
	plane := trk.GetPlane(7778)

	if plane.HasAltitude() {
		t.Errorf("new aircraft without altitude set should not have HasAltitude=true")
	}
	n := time.Now()

	plane.setAltitude(1545, "donkeys", n)
	if !plane.HasAltitude() {
		t.Errorf("Should have an altitude after setting it")
	}

	if 1545 != plane.Altitude() {
		t.Errorf("incorrect altitude set")
	}

	if "donkeys" != plane.AltitudeUnits() {
		t.Errorf("incorrect altitude units")
	}
}
