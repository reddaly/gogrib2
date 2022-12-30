package grib1

import "testing"

func Test_parse2ByteInt(t *testing.T) {
	tests := []struct {
		name string
		arg  []byte
		want int32
	}{
		{
			"positive number",
			[]byte{0, 16},
			16,
		},
		{
			"negative 3",
			[]byte{0b10000001, 3},
			-3,
		},
		{
			"negative 257",
			[]byte{0b10000001, 1},
			-(1 + 256),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parse2ByteInt(tt.arg[0], tt.arg[1]); got != tt.want {
				t.Errorf("parse2ByteInt() = %v, want %v", got, tt.want)
			}
		})
	}
}
