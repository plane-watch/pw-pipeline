package modes2

import "testing"

var (
	df0bytes  = []byte{0x00, 0x05, 0x03, 0x19, 0xAB, 0x8C, 0x22}
	df4bytes  = []byte{0x21, 0x00, 0x00, 0x99, 0x2F, 0x8C, 0x48}
	df5bytes  = []byte{0x28, 0x00, 0x1B, 0x1F, 0x21, 0x81, 0xF6}
	df11bytes = []byte{0x5F, 0x7C, 0x7B, 0x5A, 0xBB, 0x4F, 0x87}
	df16bytes = []byte{0x80, 0x81, 0x82, 0xBF, 0x58, 0x17, 0xF2, 0x92, 0xF2, 0x31, 0x5E, 0xDF, 0xCA, 0x2E}
	df17bytes = []byte{0x8D, 0x7C, 0x77, 0x94, 0x78, 0x65, 0x87, 0x83, 0xF6, 0xEF, 0x13, 0x89, 0xB3, 0x9E}
	df18bytes = []byte{0x90, 0x7C, 0xF7, 0xC6, 0xC1, 0x04, 0x00, 0x00, 0x00, 0x20, 0x04, 0x6E, 0xFB, 0xAC}
	df20bytes = []byte{0xA0, 0x00, 0x03, 0x36, 0x10, 0x02, 0x0A, 0x80, 0xF0, 0x00, 0x00, 0x27, 0x0B, 0xAA}
	df21bytes = []byte{0xA8, 0x00, 0x11, 0x89, 0x20, 0x58, 0xF6, 0xB9, 0xC3, 0x8D, 0xA0, 0x9C, 0x6D, 0x38}
	df24bytes = []byte{0xC3, 0x62, 0xFB, 0x6B, 0x7F, 0x17, 0x23, 0xD2, 0xD7, 0x12, 0x92, 0xA2, 0x82, 0xD1}
)

func BenchmarkFromBytes112(b *testing.B) {
	inputs := [][]byte{
		df16bytes,
		df17bytes,
		df18bytes,
		df20bytes,
		df21bytes,
	}

	for n := 0; n < b.N; n++ {
		FromBytes112(inputs[0])
		FromBytes112(inputs[1])
		FromBytes112(inputs[2])
		FromBytes112(inputs[3])
		FromBytes112(inputs[4])
	}
}

func BenchmarkFromBytes112Unsafe(b *testing.B) {
	inputs := [][]byte{
		df16bytes,
		df17bytes,
		df18bytes,
		df20bytes,
		df21bytes,
		df24bytes,
	}

	for n := 0; n < b.N; n++ {
		fromBytes112Unsafe(inputs[0])
		fromBytes112Unsafe(inputs[1])
		fromBytes112Unsafe(inputs[2])
		fromBytes112Unsafe(inputs[3])
		fromBytes112Unsafe(inputs[4])
		fromBytes112Unsafe(inputs[5])
	}
}

func BenchmarkFromBytes56(b *testing.B) {
	inputs := [][]byte{
		df0bytes,
		df4bytes,
		df5bytes,
		df11bytes,
	}

	for n := 0; n < b.N; n++ {
		FromBytes56(inputs[0])
		FromBytes56(inputs[1])
		FromBytes56(inputs[2])
		FromBytes56(inputs[3])
	}
}

func BenchmarkAvrFrame_DecodeDF0(b *testing.B) {
	for i := 0; i < b.N; i++ {
		f := FromBytes56(df0bytes)
		df0, err := f.DecodeDF0()
		if err != nil {
			b.Error(err)
		}
		if df0.OnGround {
			b.Error("on ground?")
		}
	}
}

func BenchmarkAvrFrame_DecodeDF4(b *testing.B) {
	for i := 0; i < b.N; i++ {
		f := FromBytes56(df4bytes)
		df4, err := f.DecodeDF4()
		if err != nil {
			b.Error(err)
		}
		if !df4.OnGround {
			b.Error("in air?")
		}
	}
}
func BenchmarkAvrFrame_DecodeDF5(b *testing.B) {
	for i := 0; i < b.N; i++ {
		f := FromBytes56(df5bytes)
		df5, err := f.DecodeDF5()
		if err != nil {
			b.Error(err)
		}
		if df5.OnGround {
			b.Error("on ground?")
		}
	}
}
func BenchmarkAvrFrame_DecodeDF11(b *testing.B) {
	for i := 0; i < b.N; i++ {
		f := FromBytes56(df11bytes)
		df11, err := f.DecodeDF11()
		if err != nil {
			b.Error(err)
		}
		if df11.ICAO == 0 {
			b.Error("NO ICAO?")
		}
	}
}
func BenchmarkAvrFrame_DecodeDF16(b *testing.B) {
	for i := 0; i < b.N; i++ {
		f := FromBytes112(df16bytes)
		df16, err := f.DecodeDF16()
		if err != nil {
			b.Error(err)
		}
		if df16.ICAO == 0 {
			b.Error("NO ICAO?")
		}
	}
}
func BenchmarkAvrFrame_DecodeDF17(b *testing.B) {
	for i := 0; i < b.N; i++ {
		f := FromBytes112(df17bytes)
		df17, err := f.DecodeADSB()
		if err != nil {
			b.Error(err)
		}
		if df17.ICAO == 0 {
			b.Error("NO ICAO?")
		}
	}
}
func BenchmarkAvrFrame_DecodeDF18(b *testing.B) {
	for i := 0; i < b.N; i++ {
		f := FromBytes112(df18bytes)
		df18, err := f.DecodeDF18()
		if err != nil {
			b.Error(err)
		}
		if df18.ICAO == 0 {
			b.Error("NO ICAO?")
		}
	}
}
func BenchmarkAvrFrame_DecodeDF20(b *testing.B) {
	for i := 0; i < b.N; i++ {
		f := FromBytes112(df20bytes)
		df20, err := f.DecodeDF20()
		if err != nil {
			b.Error(err)
		}
		if df20.OnGround || df20.Altitude != 4350 {
			b.Error("On Ground or wrong altitude?")
		}
	}
}
func BenchmarkAvrFrame_DecodeDF21(b *testing.B) {
	for i := 0; i < b.N; i++ {
		f := FromBytes112(df21bytes)
		df21, err := f.DecodeDF21()
		if err != nil {
			b.Error(err)
		}
		if df21.OnGround {
			b.Error("On Ground?")
		}
	}
}

func BenchmarkAvrFrame_DecodeDF24(b *testing.B) {
	b.Skipf("DF24 decoding not implemented")
	for i := 0; i < b.N; i++ {
		f := FromBytes112(df24bytes)
		df24, err := f.DecodeDF24()
		if err != nil {
			b.Error(err)
		}
		if df24.ICAO == 0 {
			b.Error("no icao?")
		}
	}
}
