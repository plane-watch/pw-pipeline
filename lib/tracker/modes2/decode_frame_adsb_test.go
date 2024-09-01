package main

import (
	"fmt"
	"reflect"
	"strconv"
	"testing"
)

func TestAvrFrame_DecodeADSB(t *testing.T) {
	tests := []struct {
		name    string
		fields  []byte
		want    DFADSB
		wantErr bool
	}{
		{
			name:   "DF17 / 03 / 00",
			fields: []byte{0x8C, 0x11, 0x2C, 0xCB, 0x1E, 0x48, 0x1D, 0xF7, 0xC3, 0x0C, 0xE0, 0x5C, 0xCD, 0x99},
			want: DFADSB{
				FlightNumber:    "RA77003 ",
				ICAO:            0x112CCB,
				MessageType:     3,
				MessageSubType:  0,
				Interrogatable:  true,
				CategoryValid:   true,
				CategoryType:    1,
				CategorySubType: 6,
			},
			wantErr: false,
		},
		{
			name:   "DF17 / 04 / 00",
			fields: []byte{0x8D, 0x3C, 0x64, 0x67, 0x20, 0x0C, 0x61, 0xF9, 0x60, 0xB8, 0x20, 0x28, 0x4E, 0xBF},
			want: DFADSB{
				FlightNumber:   "CFG9XK  ",
				ICAO:           0x3C6467,
				MessageType:    4,
				MessageSubType: 0,
				Interrogatable: true,
				CategoryValid:  true,
			},
			wantErr: false,
		},
		{
			name:   "DF17 / 07 / Even",
			fields: []byte{0x8F, 0xA4, 0xE8, 0x76, 0x39, 0x58, 0xA3, 0x08, 0xC4, 0xA0, 0x70, 0xFB, 0x82, 0x00},
			want: DFADSB{
				ICAO:           0xA4E876,
				MessageType:    7,
				Interrogatable: true,
				ValidVertical:  true,
				ValidHeading:   true,
				ValidVelocity:  true,
				OnGround:       true,
				Velocity:       6,
				Heading:        28.125,
				CprOddEven:     0,
				CprLat:         0b1100_0010_0011_0001_0,
				CprLon:         0b0101_0000_0011_1000_0,
			},
			wantErr: false,
		},
		{
			name:   "DF17 / 07 / Odd",
			fields: []byte{0x8F, 0xA4, 0xE8, 0x76, 0x38, 0xEE, 0x05, 0x1D, 0xB8, 0x9C, 0xA8, 0x72, 0x95, 0x6D},
			want: DFADSB{
				ICAO:           0xA4E876,
				MessageType:    7,
				Interrogatable: true,
				ValidVertical:  true,
				ValidHeading:   true,
				ValidVelocity:  true,
				OnGround:       true,
				Velocity:       2.50,
				Heading:        270,
				CprOddEven:     1,
				CprLat:         0b01000111011011100,
				CprLon:         0b01001110010101000,
			},
			wantErr: false,
		},
		{
			name:   "DF17 / 10 / 00",
			fields: []byte{0x8D, 0xAC, 0xBC, 0x0C, 0x50, 0xBD, 0xF1, 0xEF, 0x47, 0x77, 0x7C, 0x81, 0x4F, 0x62},
			want: DFADSB{
				ICAO:           0xACBC0C,
				MessageType:    10,
				Interrogatable: true,
				Altitude:       36975,
				AltUnit:        AltitudeUnitFeet,
				ValidAltitude:  true,
				ValidVertical:  true,
				CprLat:         0b01111011110100011,
				CprLon:         0b10111011101111100,
				CprOddEven:     0,
			},
			wantErr: false,
		},
		{
			name:   "DF17 / 11 / 00",
			fields: []byte{0x8D, 0x00, 0x81, 0x26, 0x58, 0x49, 0xC6, 0xD2, 0xF6, 0x13, 0x91, 0x04, 0xD6, 0xA8},
			want: DFADSB{
				ICAO:           0x008126,
				MessageType:    11,
				Interrogatable: true,
				Altitude:       13700,
				AltUnit:        AltitudeUnitFeet,
				ValidAltitude:  true,
				ValidVertical:  true,
				CprLat:         0b10110100101111011,
				CprLon:         0b00001001110010001,
				CprOddEven:     1,
			},
			wantErr: false,
		},
		{
			name:   "DF17 / 12 / 00",
			fields: []byte{0x8D, 0x4D, 0x23, 0xFB, 0x60, 0x35, 0xA6, 0x70, 0x10, 0x0A, 0xD4, 0xF0, 0x98, 0xEA},
			want: DFADSB{
				ICAO:           0x4D23FB,
				MessageType:    12,
				Interrogatable: true,
				Altitude:       9650,
				AltUnit:        AltitudeUnitFeet,
				ValidAltitude:  true,
				ValidVertical:  true,
				CprLat:         0b10011100000001000,
				CprLon:         0b00000101011010100,
				CprOddEven:     1,
			},
			wantErr: false,
		},
		{
			name:   "DF17 / 13 / 00",
			fields: []byte{0x8D, 0x7C, 0x79, 0xB5, 0x69, 0x09, 0xD5, 0x35, 0x56, 0xF8, 0x3E, 0xE4, 0xAA, 0xC6},
			want: DFADSB{
				ICAO:           0x7C79B5,
				MessageType:    13,
				Interrogatable: true,
				Altitude:       925,
				AltUnit:        AltitudeUnitFeet,
				ValidAltitude:  true,
				ValidVertical:  true,
				CprLat:         0b01001101010101011,
				CprLon:         0b01111100000111110,
				CprOddEven:     1,
				NicSupplementB: 1,
			},
			wantErr: false,
		},
		{
			name:   "DF17 / 14 / 00",
			fields: []byte{0x8D, 0x06, 0xA3, 0x64, 0x70, 0xC3, 0x84, 0xA8, 0x03, 0x34, 0x56, 0x65, 0x3C, 0xD1},
			want: DFADSB{
				ICAO:           0x06A364,
				MessageType:    14,
				Interrogatable: true,
				Altitude:       38000,
				AltUnit:        AltitudeUnitFeet,
				ValidAltitude:  true,
				ValidVertical:  true,
				CprLat:         0b00101010000000001,
				CprLon:         0b10011010001010110,
				CprOddEven:     1,
			},
			wantErr: false,
		},
		{
			name:   "DF17 / 14 / 00 - UTC Time Sync",
			fields: []byte{0x8D, 0x7C, 0x43, 0x1A, 0x70, 0xA6, 0x0F, 0x1C, 0x25, 0x93, 0x7C, 0xD7, 0x3C, 0xD6},
			want: DFADSB{
				ICAO:           0x7C431A,
				MessageType:    14,
				Interrogatable: true,
				Altitude:       3100,
				AltUnit:        AltitudeUnitFeet,
				ValidAltitude:  true,
				ValidVertical:  true,
				CprLat:         0b11000111000010010,
				CprLon:         0b11001001101111100,
				CprOddEven:     1,
				UTCTimeSync:    true,
			},
			wantErr: false,
		},
		{
			name:   "DF17 / 15 / 00",
			fields: []byte{0x8D, 0x89, 0x63, 0xF5, 0x78, 0xCD, 0x80, 0x31, 0x87, 0x7F, 0x1A, 0x2B, 0x7A, 0x7F},
			want: DFADSB{
				ICAO:           0x8963F5,
				MessageType:    15,
				Interrogatable: true,
				Altitude:       40000,
				AltUnit:        AltitudeUnitFeet,
				ValidAltitude:  true,
				ValidVertical:  true,
				CprLat:         0b00001100011000011,
				CprLon:         0b10111111100011010,
				CprOddEven:     0,
			},
			wantErr: false,
		},
		{
			name:   "DF17 / 18 / 00",
			fields: []byte{0x8D, 0x01, 0x02, 0x3D, 0x90, 0xB9, 0x43, 0x90, 0x89, 0xB4, 0x05, 0xCB, 0x27, 0xB0},
			want: DFADSB{
				ICAO:           0x01023D,
				MessageType:    18,
				Interrogatable: true,
				Altitude:       35900,
				AltUnit:        AltitudeUnitFeet,
				ValidAltitude:  true,
				ValidVertical:  true,
				CprLat:         0b11100100001000100,
				CprLon:         0b11011010000000101,
				CprOddEven:     0,
			},
			wantErr: false,
		},
		{
			name:   "DF17 / 19 / 01 (1)",
			fields: []byte{0x8D, 0x00, 0x81, 0x26, 0x99, 0x0D, 0x23, 0x9A, 0x90, 0x78, 0x28, 0x28, 0xF2, 0x11},
			want: DFADSB{
				ICAO:                  0x008126,
				MessageType:           19,
				MessageSubType:        1,
				Interrogatable:        true,
				ValidVertical:         true,
				VerticalRate:          1856,
				ValidHeading:          true,
				Heading:               233.96,
				ValidVelocity:         true,
				Velocity:              360.03,
				ValidHAE:              true,
				HeightAboveEllipsoid:  975,
				ValidNacV:             true,
				NavigationalAccuracyV: 1,
				VerticalRateSource:    VerticalRateSourceGNSS,
			},
			wantErr: false,
		},
		{
			name:   "DF17 / 19 / 01 (2)",
			fields: []byte{0x8D, 0x7C, 0x1B, 0x26, 0x99, 0x44, 0x5D, 0xA6, 0xA8, 0x5C, 0x30, 0x34, 0x00, 0xC7},
			want: DFADSB{
				ICAO:                  0x7C1B26,
				MessageType:           19,
				MessageSubType:        1,
				Interrogatable:        true,
				ValidVertical:         true,
				VerticalRate:          -1408,
				ValidHeading:          true,
				Heading:               196.63,
				ValidVelocity:         true,
				Velocity:              322.69,
				ValidHAE:              true,
				HeightAboveEllipsoid:  1175,
				ValidNacV:             true,
				NavigationalAccuracyV: 0,
				IFRCapable:            true,
				VerticalRateSource:    VerticalRateSourceBarometric,
			},
			wantErr: false,
		},
		{
			name:   "DF17 / 19 / 03",
			fields: []byte{0x8D, 0x01, 0x02, 0x08, 0x9B, 0x07, 0x0A, 0x25, 0x78, 0x08, 0x00, 0x09, 0xE0, 0x30},
			want: DFADSB{
				ICAO:               0x010208,
				MessageType:        19,
				MessageSubType:     3,
				Interrogatable:     true,
				ValidVertical:      true,
				VerticalRate:       -64,
				ValidHeading:       true,
				Heading:            272.81,
				ValidVelocity:      true,
				Velocity:           298,
				ValidNacV:          true,
				VerticalRateSource: VerticalRateSourceBarometric,
			},
			wantErr: false,
		},
		{
			name:   "DF17 / 23 / 07",
			fields: []byte{0x8D, 0x7C, 0x62, 0x73, 0xBF, 0xF1, 0xF8, 0x00, 0x00, 0x00, 0x00, 0x2E, 0x44, 0xEE},
			want: DFADSB{
				ICAO:           0x7C6273,
				MessageType:    23,
				MessageSubType: 07,
				Interrogatable: true,
			},
			wantErr: false,
		},
		{
			name:   "DF17 / 28 / 01",
			fields: []byte{0x8D, 0x00, 0x81, 0x26, 0xE1, 0x00, 0x06, 0x00, 0x00, 0x00, 0x00, 0x28, 0x97, 0xA6},
			want: DFADSB{
				ICAO:           0x008126,
				MessageType:    28,
				MessageSubType: 01,
				Interrogatable: true,
			},
			wantErr: false,
		},
		{
			name:   "DF17 / 28 / 02",
			fields: []byte{0x8D, 0x3B, 0x77, 0x5F, 0xE2, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x54, 0x1D, 0x4D},
			want: DFADSB{
				ICAO:           0x3B775F,
				MessageType:    28,
				MessageSubType: 02,
				Interrogatable: true,
			},
			wantErr: false,
		},
		{
			name:   "DF17 / 29 / 00",
			fields: []byte{0x8D, 0x7C, 0x6A, 0x54, 0xE8, 0x00, 0x00, 0x2F, 0xD1, 0x38, 0x10, 0x40, 0xC9, 0xA3},
			want: DFADSB{
				ICAO:           0x7C6A54,
				MessageType:    29,
				MessageSubType: 0,
				Interrogatable: true,
			},
			wantErr: false,
		},
		{
			name:   "DF17 / 29 / 01",
			fields: []byte{0x8D, 0x7C, 0x6A, 0x54, 0xE9, 0x98, 0x14, 0x2F, 0xD1, 0x38, 0x10, 0xCA, 0xD2, 0x2F},
			want: DFADSB{
				ICAO:           0x7C6A54,
				MessageType:    29,
				MessageSubType: 1,
				Interrogatable: true,
			},
			wantErr: false,
		},
		{
			name:   "DF17 / 29 / 02",
			fields: []byte{0x8D, 0x00, 0x81, 0x26, 0xEA, 0x3E, 0x98, 0x58, 0x01, 0x3C, 0x08, 0x12, 0x5A, 0x45},
			want: DFADSB{
				ICAO:           0x008126,
				MessageType:    29,
				MessageSubType: 2,
				Interrogatable: true,
			},
			wantErr: false,
		},
		{
			name:   "DF17 / 29 / 03",
			fields: []byte{0x8D, 0x4B, 0xA8, 0x69, 0xEB, 0x4E, 0x38, 0x60, 0x00, 0x10, 0x08, 0xA8, 0x57, 0x65},
			want: DFADSB{
				ICAO:           0x4BA869,
				MessageType:    29,
				MessageSubType: 3,
				Interrogatable: true,
			},
			wantErr: false,
		},
		{
			name:   "DF17 / 31 / 00",
			fields: []byte{0x8D, 0x00, 0x81, 0x26, 0xF8, 0x23, 0x00, 0x02, 0x00, 0x49, 0xB8, 0x32, 0x9F, 0x55},
			want: DFADSB{
				ICAO:           0x008126,
				MessageType:    31,
				MessageSubType: 0,
				Interrogatable: true,
			},
			wantErr: false,
		},
		{
			name:   "DF17 / 31 / 1",
			fields: []byte{0x8C, 0x4A, 0x91, 0xF9, 0xF9, 0x00, 0x26, 0x02, 0x83, 0x49, 0x38, 0x9D, 0xE8, 0x16},
			want: DFADSB{
				ICAO:           0x4A91F9,
				MessageType:    31,
				MessageSubType: 1,
				Interrogatable: true,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := FromBytes112(tt.fields)

			got, err := a.DecodeADSB()

			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeDF17() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Did not Interpret %s correctly", a.String())
				format := " %22s | %-17s | %-17s\n"
				t.Errorf("DecodeDF17()")
				t.Errorf(format, "Field", "GOT", "WANT")
				printed := false

				r := reflect.TypeOf(got)
				for i := 0; i < r.NumField(); i++ {
					vGot := reflect.ValueOf(got).Field(i)
					vWant := reflect.ValueOf(tt.want).Field(i)

					// if vGot.Equal(vWant) {
					// 	continue
					// }
					var vGotVal, vWantVal string
					switch vGot.Kind() {
					case reflect.String:
						vGotVal = vGot.String()
						vWantVal = vWant.String()
					case reflect.Bool:
						vGotVal = strconv.FormatBool(vGot.Bool())
						vWantVal = strconv.FormatBool(vWant.Bool())
					case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
						vGotVal = strconv.FormatInt(vGot.Int(), 10)
						vWantVal = strconv.FormatInt(vWant.Int(), 10)
					case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
						vGotVal = strconv.FormatUint(vGot.Uint(), 10)
						vWantVal = strconv.FormatUint(vWant.Uint(), 10)
					case reflect.Float64, reflect.Float32:
						vGotVal = fmt.Sprintf("%f", vGot.Float())
						vWantVal = fmt.Sprintf("%f", vWant.Float())
					default:
						vGotVal = "Unknown Got Type: " + vGot.Kind().String()
						vWantVal = "Unknown Want Type: " + vWant.Kind().String()
					}

					if vGotVal == vWantVal {
						continue
					}
					printed = true

					t.Errorf(format, r.Field(i).Name, vGotVal, vWantVal)
				}
				t.Errorf("\n\n")

				if !printed {
					t.Errorf("DecodeDF17()\n got = %+v\nwant = %+v\n", got, tt.want)
				}
			}
		})
	}
}

func BenchmarkAvrFrame_DecodeADSB_04(b *testing.B) {
	for i := 0; i < b.N; i++ {
		f := FromBytes112([]byte{0x8D, 0x3C, 0x64, 0x67, 0x20, 0x0C, 0x61, 0xF9, 0x60, 0xB8, 0x20, 0x28, 0x4E, 0xBF})
		_, err := f.DecodeADSB()
		if err != nil {
			b.Error(err)
		}
	}
}

func BenchmarkAvrFrame_DecodeADSB_07(b *testing.B) {
	for i := 0; i < b.N; i++ {
		f := FromBytes112([]byte{0x8F, 0xA4, 0xE8, 0x76, 0x38, 0xEE, 0x05, 0x1D, 0xB8, 0x9C, 0xA8, 0x72, 0x95, 0x6D})
		_, err := f.DecodeADSB()
		if err != nil {
			b.Error(err)
		}
	}
}
