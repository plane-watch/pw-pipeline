package main

import "testing"

func TestSBSPosition(t *testing.T) {
	cases := []struct {
		name     string
		line     string
		lat, lon float64
		ok       bool
	}{
		{"mlat-server line", "MSG,3,1,1,7C6DB8,1,2026/10/25,01:02:03.456,2026/10/25,01:02:04.000,,3400,,,-31.950000,115.860000,,,,,,", -31.95, 115.86, true},
		{"mux test line", "MSG,3,49,2425,7503DF,748059,2016/06/03,08:00:00.489,2016/06/03,08:00:00.568,,2650,,,-32.09214,115.92078,,,0,0,0,0", -32.09214, 115.92078, true},
		{"empty position", "MSG,3,1,1,7C6DB8,1,2026/10/25,01:02:03.456,2026/10/25,01:02:04.000,,3400,,,,,,,,,,", 0, 0, false},
		{"zero zero", "MSG,3,1,1,7C6DB8,1,2026/10/25,01:02:03.456,2026/10/25,01:02:04.000,,3400,,,0,0,,,,,,", 0, 0, false},
		{"out of range", "MSG,3,1,1,7C6DB8,1,2026/10/25,01:02:03.456,2026/10/25,01:02:04.000,,3400,,,-91,115.86,,,,,,", 0, 0, false},
		{"garbage", "MSG,3,1,1,7C6DB8,1,2026/10/25,01:02:03.456,2026/10/25,01:02:04.000,,3400,,,abc,115.86,,,,,,", 0, 0, false},
		{"too few fields", "MSG,3,1,1,7C6DB8,1,2026/10/25,01:02:03.456", 0, 0, false},
		{"lat only", "MSG,3,1,1,7C6DB8,1,2026/10/25,01:02:03.456,2026/10/25,01:02:04.000,,3400,,,-31.95", 0, 0, false},
		{"no trailing fields", "MSG,3,1,1,7C6DB8,1,2026/10/25,01:02:03.456,2026/10/25,01:02:04.000,,3400,,,-31.95,115.86", -31.95, 115.86, true},
		{"empty", "", 0, 0, false},
	}
	for _, c := range cases {
		lat, lon, ok := sbsPosition([]byte(c.line))
		if ok != c.ok || lat != c.lat || lon != c.lon {
			t.Errorf("%s: got %v %v %v, want %v %v %v", c.name, lat, lon, ok, c.lat, c.lon, c.ok)
		}
	}
}
