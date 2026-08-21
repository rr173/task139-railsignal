// Package fixed provides integer rounding helpers used across the engine so
// that no float values leak into the public API. All public quantities are
// integers (meters, seconds, deci-meters/second, counts). A few internal
// proportional conversions (e.g. speed km/h -> dm/s) round half-up.
package fixed

// RoundHalfUp returns q rounded half-up: it computes round(a/b) with ties going
// away from zero, for non-negative inputs. b must be > 0.
func RoundHalfUp(a, b int) int {
	if b == 0 {
		panic("fixed.RoundHalfUp: zero divisor")
	}
	if a < 0 {
		// keep half-up semantics symmetric for negatives
		return -roundHalfUpPos(-a, b)
	}
	return roundHalfUpPos(a, b)
}

func roundHalfUpPos(a, b int) int {
	q := a / b
	r := a % b
	// half of b, rounded up, is the threshold for rounding up
	if 2*r >= b {
		return q + 1
	}
	return q
}

// Ceil returns ceil(a/b) for a >= 0, b > 0.
func Ceil(a, b int) int {
	if b == 0 {
		panic("fixed.Ceil: zero divisor")
	}
	q := a / b
	if a%b == 0 {
		return q
	}
	if a < 0 {
		return q // towards zero is the ceiling for negative numerators when remainder 0, else q is already the ceil
	}
	return q + 1
}

// Max returns the larger of two ints.
func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Min returns the smaller of two ints.
func Min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// KmhToDms converts kilometres-per-hour to deci-metres/second (0.1 m/s steps),
// rounded half-up. 1 km/h = 1000/3600 m/s = 10/36 dm/s.
func KmhToDms(kmh int) int {
	// kmh * 1000 / 3600 * 10 = kmh * 10000 / 3600 = kmh * 100 / 36
	return RoundHalfUp(kmh*100, 36)
}

// Clamp returns v clamped to [lo, hi]. Assumes lo <= hi.
func Clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
