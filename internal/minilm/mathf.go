package minilm

import "math"

// expf is e^x in float32: range reduction to r = x - n·ln2 with |r| ≤ ln2/2
// and a degree-6 polynomial (Cephes expf), within about 1 ulp. math.Exp in
// float64 is several times slower, and this runs for every attention score.
func expf(x float32) float32 {
	switch {
	case x > 88.72283:
		return float32(math.Inf(1))
	case x < -87.33654: // below the smallest normal: flush to zero
		return 0
	}
	n := float32(math.Floor(float64(x*1.442695041 + 0.5)))
	r := x - n*0.693359375 - n*-2.12194440e-4 // ln 2 in two parts, for precision
	p := float32(1.9875691500e-4)
	p = p*r + 1.3981999507e-3
	p = p*r + 8.3334519073e-3
	p = p*r + 4.1665795894e-2
	p = p*r + 1.6666665459e-1
	p = p*r + 5.0000001201e-1
	p = p*r*r + r + 1
	return p * math.Float32frombits(uint32(int32(n)+127)<<23)
}

// erff is erf(x) in float32, by Abramowitz and Stegun 7.1.26: an absolute
// error of 1.5e-7, below 5e-7 with float32 rounding.
func erff(x float32) float32 {
	sign := float32(1)
	if x < 0 {
		sign, x = -1, -x
	}
	if x > 4 { // erf(4) = 1 - 1.5e-8
		return sign
	}
	t := 1 / (1 + 0.3275911*x)
	p := float32(1.061405429)
	p = p*t - 1.453152027
	p = p*t + 1.421413741
	p = p*t - 0.284496736
	p = p*t + 0.254829592
	return sign * (1 - p*t*expf(-x*x))
}

// gelu is the exact (erf) GELU that BERT's "gelu" activation is.
func gelu(x float32) float32 {
	return 0.5 * x * (1 + erff(x*(1/math.Sqrt2)))
}
