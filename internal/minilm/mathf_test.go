package minilm

import (
	"math"
	"math/rand/v2"
	"testing"
)

func TestExpfIsWithinAFewULPs(t *testing.T) {
	for x := float32(-87); x <= 88; x += 0.0137 {
		want := math.Exp(float64(x))
		if rel := math.Abs(float64(expf(x))-want) / want; rel > 3e-7 {
			t.Fatalf("expf(%v) = %v, want %v (relative error %.1e)", x, expf(x), want, rel)
		}
	}
	if expf(-100) != 0 || !math.IsInf(float64(expf(100)), 1) || expf(0) != 1 {
		t.Error("expf edge cases")
	}
}

func TestErffIsWithinTheApproximationError(t *testing.T) {
	// 1.5e-7 is the approximation's own error; computing 1 - p·t·e^(-x²) in
	// float32 cancels for small |x| and adds about as much again. What matters
	// is the end result, TestEmbeddingsMatchPatent, which this leaves at 7e-7.
	worst := 0.0
	for x := float32(-6); x <= 6; x += 0.00031 {
		want := math.Erf(float64(x))
		d := math.Abs(float64(erff(x)) - want)
		worst = max(worst, d)
		if d > 5e-7 {
			t.Fatalf("erff(%v) = %v, want %v (error %.1e)", x, erff(x), want, d)
		}
	}
	t.Logf("worst error: %.2e", worst)
	if erff(0) != 0 {
		t.Errorf("erff(0) = %v", erff(0))
	}
}

func TestMatmulKernelMatchesGo(t *testing.T) {
	rnd := rand.New(rand.NewPCG(1, 2))
	fill := func(n int) []float32 {
		s := make([]float32, n)
		for i := range s {
			s[i] = rnd.Float32()*2 - 1
		}
		return s
	}
	for _, shape := range [][3]int{{4, 384, 384}, {8, 384, 1536}, {20, 1536, 384}, {4, 1, 24}, {4, 3, 16}, {4, 5, 48}, {7, 384, 384}, {4, 384, 40}} {
		n, in, out := shape[0], shape[1], shape[2]
		x, w := fill(n*in), fill(in*out)
		got, want := make([]float32, n*out), make([]float32, n*out)
		matmul(got, x, w, n, in, out)
		matmulGo(want, x, w, n, in, out)
		for i := range got {
			if d := math.Abs(float64(got[i] - want[i])); d > 1e-4*math.Sqrt(float64(in)) {
				t.Fatalf("%v: element %d is %v, want %v", shape, i, got[i], want[i])
			}
		}
	}
}
