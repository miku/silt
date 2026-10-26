//go:build !amd64 && !arm64

package minilm

// Elsewhere, matmul runs in plain Go.
const (
	hasKernel  = false
	kernelCols = 1
)

func kernel(k int, x *float32, xStride int, w *float32, wStride int, dst *float32, dstStride int) {
	panic("unreachable")
}
