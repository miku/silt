package minilm

// matmul computes dst[n×out] = x[n×in]·w[in×out], all row-major. Where there
// is an assembly kernel (AVX2 and FMA on amd64, NEON on arm64), it needs n to
// be a multiple of 4 and out one of kernelCols (24 or 16, of which 384 and
// 1536 are both); callers pad their rows to make the first hold.
func matmul(dst, x, w []float32, n, in, out int) {
	if len(dst) < n*out || len(x) < n*in || len(w) < in*out {
		panic("minilm: matmul: slices too short")
	}
	if hasKernel && n%4 == 0 && out%kernelCols == 0 && in > 0 {
		// Column panels outermost: a panel of w (in × kernelCols floats)
		// stays in L1 while every block of 4 rows passes over it.
		for j := 0; j < out; j += kernelCols {
			for i := 0; i < n; i += 4 {
				kernel(in, &x[i*in], 4*in, &w[j], 4*out, &dst[i*out+j], 4*out)
			}
		}
		return
	}
	matmulGo(dst, x, w, n, in, out)
}

// matmulGo is matmul in plain Go. Four rows of x are processed together, so
// that each row of w, once loaded, updates four rows of dst.
func matmulGo(dst, x, w []float32, n, in, out int) {
	clear(dst[:n*out])
	i := 0
	for ; i+4 <= n; i += 4 {
		d0 := dst[(i+0)*out : (i+1)*out]
		d1 := dst[(i+1)*out : (i+2)*out]
		d2 := dst[(i+2)*out : (i+3)*out]
		d3 := dst[(i+3)*out : (i+4)*out]
		for k := range in {
			a0, a1, a2, a3 := x[(i+0)*in+k], x[(i+1)*in+k], x[(i+2)*in+k], x[(i+3)*in+k]
			wk := w[k*out : (k+1)*out]
			d0, d1, d2, d3 := d0[:len(wk)], d1[:len(wk)], d2[:len(wk)], d3[:len(wk)]
			for j, wv := range wk {
				d0[j] += a0 * wv
				d1[j] += a1 * wv
				d2[j] += a2 * wv
				d3[j] += a3 * wv
			}
		}
	}
	for ; i < n; i++ {
		d := dst[i*out : (i+1)*out]
		for k := range in {
			a := x[i*in+k]
			wk := w[k*out : (k+1)*out]
			d := d[:len(wk)]
			for j, wv := range wk {
				d[j] += a * wv
			}
		}
	}
}
