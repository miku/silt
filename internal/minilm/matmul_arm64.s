#include "textflag.h"

// func kernel(k int, x *float32, xStride int, w *float32, wStride int, dst *float32, dstStride int)
//
// dst[r][0:16] = Σ_{i<k} x[r][i] · w[i][0:16] for the rows r = 0..3, with
// the strides in bytes. Sixteen NEON accumulators, rows 0..3 × four vectors
// of 4, so that four FMA pipes with a latency of 4 have work in flight.
TEXT ·kernel(SB), NOSPLIT, $0-56
	MOVD k+0(FP), R0
	MOVD x+8(FP), R1
	MOVD xStride+16(FP), R2
	MOVD w+24(FP), R3
	MOVD wStride+32(FP), R4
	MOVD dst+40(FP), R5
	MOVD dstStride+48(FP), R6

	ADD R2, R1, R7 // x row 1
	ADD R2, R7, R8 // x row 2
	ADD R2, R8, R9 // x row 3

	VEOR V0.B16, V0.B16, V0.B16
	VEOR V1.B16, V1.B16, V1.B16
	VEOR V2.B16, V2.B16, V2.B16
	VEOR V3.B16, V3.B16, V3.B16
	VEOR V4.B16, V4.B16, V4.B16
	VEOR V5.B16, V5.B16, V5.B16
	VEOR V6.B16, V6.B16, V6.B16
	VEOR V7.B16, V7.B16, V7.B16
	VEOR V8.B16, V8.B16, V8.B16
	VEOR V9.B16, V9.B16, V9.B16
	VEOR V10.B16, V10.B16, V10.B16
	VEOR V11.B16, V11.B16, V11.B16
	VEOR V12.B16, V12.B16, V12.B16
	VEOR V13.B16, V13.B16, V13.B16
	VEOR V14.B16, V14.B16, V14.B16
	VEOR V15.B16, V15.B16, V15.B16

	CBZ R0, store

loop:
	VLD1    (R3), [V16.S4, V17.S4, V18.S4, V19.S4]
	ADD     R4, R3, R3
	VLD1R.P 4(R1), [V20.S4]
	VLD1R.P 4(R7), [V21.S4]
	VLD1R.P 4(R8), [V22.S4]
	VLD1R.P 4(R9), [V23.S4]
	VFMLA   V16.S4, V20.S4, V0.S4
	VFMLA   V17.S4, V20.S4, V1.S4
	VFMLA   V18.S4, V20.S4, V2.S4
	VFMLA   V19.S4, V20.S4, V3.S4
	VFMLA   V16.S4, V21.S4, V4.S4
	VFMLA   V17.S4, V21.S4, V5.S4
	VFMLA   V18.S4, V21.S4, V6.S4
	VFMLA   V19.S4, V21.S4, V7.S4
	VFMLA   V16.S4, V22.S4, V8.S4
	VFMLA   V17.S4, V22.S4, V9.S4
	VFMLA   V18.S4, V22.S4, V10.S4
	VFMLA   V19.S4, V22.S4, V11.S4
	VFMLA   V16.S4, V23.S4, V12.S4
	VFMLA   V17.S4, V23.S4, V13.S4
	VFMLA   V18.S4, V23.S4, V14.S4
	VFMLA   V19.S4, V23.S4, V15.S4
	SUB     $1, R0, R0
	CBNZ    R0, loop

store:
	VST1 [V0.S4, V1.S4, V2.S4, V3.S4], (R5)
	ADD  R6, R5, R5
	VST1 [V4.S4, V5.S4, V6.S4, V7.S4], (R5)
	ADD  R6, R5, R5
	VST1 [V8.S4, V9.S4, V10.S4, V11.S4], (R5)
	ADD  R6, R5, R5
	VST1 [V12.S4, V13.S4, V14.S4, V15.S4], (R5)
	RET
