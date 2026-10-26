package minilm

import "golang.org/x/sys/cpu"

// hasKernel reports whether the CPU has AVX2 and FMA, which the kernel
// needs. It is a variable so that tests can run the plain-Go path.
var hasKernel = cpu.X86.HasAVX2 && cpu.X86.HasFMA

// kernelCols is the width of the kernel's tile.
const kernelCols = 24

// kernel computes a 4×24 tile of x·w; see matmul_amd64.s.
//
//go:noescape
func kernel(k int, x *float32, xStride int, w *float32, wStride int, dst *float32, dstStride int)
