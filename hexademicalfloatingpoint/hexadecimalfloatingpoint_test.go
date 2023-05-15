package hexademicalfloatingpoint

import (
	"testing"
)

func TestParse32(t *testing.T) {
	for _, tt := range []struct {
		hdf  []byte // ibm format
		want float64
	}{
		// {
		// 	[]byte{0b1000_0000 | (64*4 - 24), 0, 0, 5},
		// 	-5,
		// },
		{
			// example from https://en.wikipedia.org/wiki/IBM_hexadecimal_floating-point
			[]byte{0b1100_0010, 0b0111_0110, 0b1010_0000, 0b0000_0000},
			-118.625,
		},
		{
			// example from https://en.wikipedia.org/wiki/IBM_hexadecimal_floating-point
			[]byte{0b1100_0010, 0b0111_0110, 0b1010_0000, 0b0000_0000},
			-118.625,
		},
		{
			// example from https://en.wikipedia.org/wiki/IBM_hexadecimal_floating-point
			[]byte{0, 0, 0, 0},
			0,
		},
	} {
		got := Parse32(tt.hdf)
		if got != tt.want {
			t.Errorf("decoded %f, wanted %f (delta = %f)", got, tt.want, got-tt.want)
		}
	}
}
