package main

import (
	"math"
	"strconv"
)

const (
	sbsLatField = 14
	sbsLonField = 15
)

// sbsPosition reads the aircraft position from an SBS line: fields 14 and 15,
// counting from 0. It does not trust the mux's parse, which turns an empty or
// unparsable field into 0: a line with either field empty, unparsable, out of range
// or exactly 0, 0 has no usable position.
func sbsPosition(line []byte) (lat, lon float64, ok bool) {
	var latS, lonS string
	field, start := 0, 0
	for i := 0; i <= len(line); i++ {
		if i < len(line) && line[i] != ',' {
			continue
		}
		switch field {
		case sbsLatField:
			latS = string(line[start:i])
		case sbsLonField:
			lonS = string(line[start:i])
		}
		if field == sbsLonField {
			break
		}
		field++
		start = i + 1
	}
	if latS == "" || lonS == "" {
		return 0, 0, false
	}
	var err error
	if lat, err = strconv.ParseFloat(latS, 64); err != nil {
		return 0, 0, false
	}
	if lon, err = strconv.ParseFloat(lonS, 64); err != nil {
		return 0, 0, false
	}
	if math.IsNaN(lat) || math.IsNaN(lon) || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return 0, 0, false
	}
	if lat == 0 && lon == 0 {
		return 0, 0, false
	}
	return lat, lon, true
}
