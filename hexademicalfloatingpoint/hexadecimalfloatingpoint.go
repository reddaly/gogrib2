// Package hexademicalfloatingpoint decodes floating point numbers encoded in
// IBM hexidecimal floating point format.
//
// See https://en.wikipedia.org/wiki/IBM_hexadecimal_floating-point.
package hexademicalfloatingpoint

import (
	"math"
)

// Parse32 decodes a single precision hexidecimal floating point number into a
// float64.
func Parse32(bytes []byte) float64 {
	// See 92.6.4 in https://wmoomm.sharepoint.com/sites/wmocpdb/eve_activityarea/Forms/AllItems.aspx?id=%2Fsites%2Fwmocpdb%2Feve%5Factivityarea%2FWMO%20Codes%2FWMO306%5FvI2%2FPrevEDITIONS%2FGRIB1%2FWMO306%5FvI2%5FGRIB1%5Fen%2Epdf&parent=%2Fsites%2Fwmocpdb%2Feve%5Factivityarea%2FWMO%20Codes%2FWMO306%5FvI2%2FPrevEDITIONS%2FGRIB1&p=true&ga=1
	// R = (–1)^s × 2^(–24) × B x 16^(A–64)
	// =>
	// R = (–1)^s × B x (2^4)^(A–64) x 2^(–24)
	//   = (–1)^s × B x (2)^(4A–4*64) x 2^(–24)
	//   = (–1)^s × B x (2)^(4A–4*64 - 24)
	a := bytes[0] & 0b0111_1111
	exp := 4*(int(a)-64) - 24

	b := (int(bytes[1]) << 16) | (int(bytes[2]) << 8) | int(bytes[3])
	out := math.Ldexp(float64(b), exp)

	// 0 for positive, 1 for negative
	isNegative := (bytes[0] & 0b1000_0000) != 0
	if isNegative {
		return out * -1
	}
	return out
}
