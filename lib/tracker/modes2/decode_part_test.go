package main

import "testing"

func Test_decodeID13(t *testing.T) {
	type args struct {
		id13 int16
	}
	tests := []struct {
		name    string
		args    args
		want    int16
		wantErr bool
	}{
		{
			name: "squawk 0664",
			args: args{
				id13: 0b00101_00001011,
			},
			want:    664,
			wantErr: false,
		},
		{
			name: "zero bit set",
			args: args{
				id13: 0b00000_01000000,
			},
			want:    0,
			wantErr: true,
		},
		{
			name: "squawk 7777",
			args: args{
				id13: 0b11111_10111111,
			},
			want:    7777,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeID13Field(tt.args.id13)
			if (err != nil) != tt.wantErr {
				t.Errorf("decodeID13Field() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("decodeID13Field() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Benchmark_decodeID13Field(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if x, _ := decodeID13Field(0b00011111_10111111); x != 7777 {
			b.Errorf("incorrect decode - got %d, want 7777", x)
		}
	}
}

func Benchmark_decodeAC13Field(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if x := decodeAC13Field(0b00011111_10111111); x != 30583 {
			b.Errorf("incorrect decode - got %d, want 30583", x)
		}
	}
}
