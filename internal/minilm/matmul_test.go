package minilm

import (
	"fmt"
	"testing"
)

// BenchmarkMatmul runs the encoder's three shapes of matrix product, for 64
// rows: query, key, value and attention output (384×384), intermediate
// (384×1536) and output (1536×384), with the kernel and in plain Go.
func BenchmarkMatmul(b *testing.B) {
	for _, shape := range [][3]int{{64, 384, 384}, {64, 384, 1536}, {64, 1536, 384}} {
		n, in, out := shape[0], shape[1], shape[2]
		x, w, dst := benchFloats(n*in), benchFloats(in*out), make([]float32, n*out)
		for _, impl := range []struct {
			name string
			f    func(dst, x, w []float32, n, in, out int)
		}{{"asm", matmul}, {"go", matmulGo}} {
			b.Run(fmt.Sprintf("%dx%dx%d/%s", n, in, out, impl.name), func(b *testing.B) {
				for b.Loop() {
					impl.f(dst, x, w, n, in, out)
				}
				reportFlops(b, n*in*out)
			})
		}
	}
}

// BenchmarkKernel runs the kernel alone on one tile, 4×384 by 384×kernelCols,
// which stays in L1: as close to the peak of the FMA pipes as it gets.
func BenchmarkKernel(b *testing.B) {
	if !hasKernel {
		b.Skip("no assembly kernel")
	}
	const k = 384
	x, w, dst := benchFloats(4*k), benchFloats(k*kernelCols), make([]float32, 4*kernelCols)
	for b.Loop() {
		kernel(k, &x[0], 4*k, &w[0], 4*kernelCols, &dst[0], 4*kernelCols)
	}
	reportFlops(b, 4*k*kernelCols)
}

func benchFloats(n int) []float32 {
	s := make([]float32, n)
	for i := range s {
		s[i] = float32(i%17-8) / 8
	}
	return s
}

// reportFlops reports fmas multiply-adds per op as GFLOP/s, two flops each.
func reportFlops(b *testing.B, fmas int) {
	b.ReportMetric(2*float64(fmas)*float64(b.N)/b.Elapsed().Seconds()/1e9, "GFLOP/s")
}
