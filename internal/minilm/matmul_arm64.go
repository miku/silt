package minilm

// NEON is part of every arm64 CPU, so the kernel always runs.
const (
	hasKernel  = true
	kernelCols = 16
)

// kernel computes a 4×16 tile of x·w; see matmul_arm64.s.
//
//go:noescape
func kernel(k int, x *float32, xStride int, w *float32, wStride int, dst *float32, dstStride int)
