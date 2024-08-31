package main

import (
	"testing"
)

func TestAvrFrame_Checksum(t *testing.T) {
	type fields struct {
		a   uint64
		b   uint64
		len int
	}
	tests := []struct {
		name   string
		fields fields
		want   uint64
	}{
		{
			// checksum ^ CRC is the ICAO
			name: "Valid DF00",
			fields: fields{
				a:   0x00_05_03_19_AB_8C_22_00,
				b:   0,
				len: 7,
			},
			want: 0xD7F778,
		},
		{
			// checksum ^ CRC is the ICAO
			name: "Valid DF04",
			fields: fields{
				a:   0x20_00_06_A2_DE_8B_1C_00,
				b:   0,
				len: 7,
			},
			want: 0xA29034,
		},
		{
			// checksum ^ CRC is the ICAO
			name: "Valid DF05",
			fields: fields{
				a:   0x29_00_1B_3A_F4_7E_76_00,
				b:   0,
				len: 7,
			},
			want: 0x886A02,
		},
		{
			// ICAO field is explicit, checksum ^ CRC should be 0
			name: "Valid DF11",
			fields: fields{
				a:   0x5F_7C_7B_5A_BB_4F_87_00,
				b:   0,
				len: 7,
			},
			want: 0xBB4F87,
		},
		{
			// checksum ^ CRC is the ICAO
			name: "Valid DF16",
			fields: fields{
				a:   0x80_61_94_20_58_A2_0A_A1,
				b:   0x0C_3A_1E_6E_E7_CD_00_00,
				len: 14,
			},
			want: 0x12A4D2,
		},
		{
			name: "Valid DF17",
			fields: fields{
				a:   0x8D_76_CE_88_20_4C_90_72,
				b:   0xCB_48_20_9A_50_4D_00_00,
				len: 14,
			},
			want: 0x9A504D,
		},
		{
			name: "Valid DF18",
			fields: fields{
				a:   0x90_7C_F7_C6_10_40_84_98,
				b:   0xC8_18_20_00_67_90_00_00,
				len: 14,
			},
			want: 0x6790,
		},
		{
			name: "Valid DF20",
			fields: fields{
				a:   0xA0_00_01_9D_10_00_08_00,
				b:   0xF0_00_00_46_35_C0_00_00,
				len: 14,
			},
			want: 0x3A4ACD,
		},
		{
			name: "Valid DF24",
			fields: fields{
				a:   0xC3_62_FB_6B_7F_17_23_D2,
				b:   0xD7_12_92_A2_82_D1_00_00,
				len: 14,
			},
			want: 0xDE9E0D,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := AvrFrame{
				a:   tt.fields.a,
				b:   tt.fields.b,
				len: tt.fields.len,
			}
			got := a.Checksum()
			icao := got ^ a.messageCrc()
			t.Logf("%s - checksum %X, CRC %X, ICAO %X\n", tt.name, got, a.messageCrc(), icao)
			if got := a.Checksum(); got != tt.want {
				t.Errorf("Checksum() = %X, want %X", got, tt.want)
			}
		})
	}
}
