package jsjson

import (
	"math"
	"math/big"
	"strings"
)

// ToFixed is Number.prototype.toFixed(digits) for a finite x under 1e21:
// a tie rounds away from zero, judged on x's exact binary value, where
// strconv's 'f' rounds a tie to even.
func ToFixed(x float64, digits int) string {
	if math.IsNaN(x) || math.IsInf(x, 0) || math.Abs(x) >= 1e21 {
		return FormatNumber(x)
	}
	neg := x < 0
	scaled := new(big.Float).SetPrec(512).SetFloat64(math.Abs(x))
	scaled.Mul(scaled, new(big.Float).SetPrec(512).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(digits)), nil)))
	n, _ := scaled.Int(nil)
	frac := new(big.Float).SetPrec(512).Sub(scaled, new(big.Float).SetPrec(512).SetInt(n))
	if frac.Cmp(big.NewFloat(0.5)) >= 0 {
		n.Add(n, big.NewInt(1))
	}
	s := n.String()
	if digits > 0 {
		if len(s) <= digits {
			s = strings.Repeat("0", digits-len(s)+1) + s
		}
		s = s[:len(s)-digits] + "." + s[len(s)-digits:]
	}
	if neg {
		s = "-" + s
	}
	return s
}
