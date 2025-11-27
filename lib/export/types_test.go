package export

import (
	"github.com/rs/zerolog"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	zerolog.SetGlobalLevel(zerolog.Disabled)
	m.Run()
}

func TestIsLocationPossible(t *testing.T) {
	msg1t := time.Date(2023, time.January, 9, 19, 0, 0, 0, time.Local)
	msg2t := time.Date(2023, time.January, 9, 19, 0, 1, 0, time.Local)

	if msg2t.Before(msg1t) {
		t.Error("I don't understand time")
	}

	pos1 := PlaneLocation{Lat: -31.942017, Lon: 115.964594, Heading: 14.116942, HasLocation: true, HasHeading: true, LastMsg: msg1t}
	pos2 := PlaneLocation{Lat: -31.940887, Lon: 115.964897, Heading: 14.116942, HasLocation: true, HasHeading: true, LastMsg: msg2t}

	if !IsLocationPossible(pos1, pos2) {
		t.Error("Pos1 -> Pos2 is possible")
	}

	if IsLocationPossible(pos2, pos1) {
		t.Error("Pos2 -> Pos1 is not possible")
	}
}

func TestMergeCallSign(t *testing.T) {
	type args struct {
		prev PlaneLocation
		next PlaneLocation
	}
	tests := []struct {
		name    string
		args    args
		want    PlaneLocation
		wantErr bool
	}{
		{
			name: "both-filled",
			args: args{
				prev: PlaneLocation{CallSign: ptr("ONE")},
				next: PlaneLocation{CallSign: ptr("TWO")},
			},
			want:    PlaneLocation{CallSign: ptr("TWO")},
			wantErr: false,
		},
		{
			name: "One-Blank",
			args: args{
				prev: PlaneLocation{CallSign: ptr("")},
				next: PlaneLocation{CallSign: ptr("TWO")},
			},
			want:    PlaneLocation{CallSign: ptr("TWO")},
			wantErr: false,
		},
		{
			name: "Two-Blank",
			args: args{
				prev: PlaneLocation{CallSign: ptr("ONE")},
				next: PlaneLocation{CallSign: ptr("")},
			},
			want:    PlaneLocation{CallSign: ptr("ONE")},
			wantErr: false,
		},
		{
			name: "One-nil",
			args: args{
				prev: PlaneLocation{CallSign: nil},
				next: PlaneLocation{CallSign: ptr("")},
			},
			want:    PlaneLocation{CallSign: ptr("")},
			wantErr: false,
		},
		{
			name: "Two-nil",
			args: args{
				prev: PlaneLocation{CallSign: ptr("ONE")},
				next: PlaneLocation{CallSign: nil},
			},
			want:    PlaneLocation{CallSign: ptr("ONE")},
			wantErr: false,
		},
		{
			name: "both-nil",
			args: args{
				prev: PlaneLocation{CallSign: nil},
				next: PlaneLocation{CallSign: nil},
			},
			want:    PlaneLocation{CallSign: nil},
			wantErr: false,
		},
		{
			name: "both-blank",
			args: args{
				prev: PlaneLocation{CallSign: ptr("")},
				next: PlaneLocation{CallSign: ptr("")},
			},
			want:    PlaneLocation{CallSign: ptr("")},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MergePlaneLocations(tt.args.prev, tt.args.next)
			if (err != nil) != tt.wantErr {
				t.Errorf("MergePlaneLocations() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if nil != tt.want.CallSign {
				if unPtr(got.CallSign) != unPtr(tt.want.CallSign) {
					t.Errorf("MergePlaneLocations() got = %s, want %s", unPtr(got.CallSign), unPtr(tt.want.CallSign))
				}
			} else {
				if got.CallSign != nil {
					t.Errorf("Expected a nil callsign, got %s", unPtr(got.CallSign))
				}
			}
		})
	}
}

func TestPlaneLocation_PrepareSourceTags(t *testing.T) {
	tests := []struct {
		name   string
		fields string
		want   map[string]uint32
	}{
		{
			name:   "feeder key with icao and id decodes correctly",
			fields: "YPPH-0001",
			want: map[string]uint32{
				"0001": 1,
			},
		},
		{
			name:   "feeder key with icao and id decodes correctly (lower)",
			fields: "ypph-0001",
			want: map[string]uint32{
				"0001": 1,
			},
		},
		{
			name:   "feeder key with long id works",
			fields: "long-0000001",
			want: map[string]uint32{
				"0000001": 1,
			},
		},
		{
			name:   "feeder key that's an API Key",
			fields: "550e8400-e29b-41d4-a716-446655440000",
			want: map[string]uint32{
				"550e8400-e29b-41d4-a716-446655440000": 1,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := make(map[string]uint32, 1)
			pl := &PlaneLocation{
				SourceTags: map[string]uint32{
					tt.fields: 1,
				},
				SourceTagsMutex: &sync.RWMutex{},
			}
			if got := pl.PrepareSourceTags(m); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("PrepareSourceTags() = %v, want %v", got, tt.want)
			}
		})
	}
}
