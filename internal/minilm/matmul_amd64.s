#include "textflag.h"

// func kernel(k int, x *float32, xStride int, w *float32, wStride int, dst *float32, dstStride int)
//
// dst[r][0:24] = Σ_{i<k} x[r][i] · w[i][0:24] for the rows r = 0..3, with
// the strides in bytes. Twelve accumulators, rows 0..3 × three vectors of 8:
// enough independent chains to hide the FMA latency (eight, in a 4×16 tile,
// measured 10% slower).
TEXT ·kernel(SB), NOSPLIT, $0-56
	MOVQ k+0(FP), CX
	MOVQ x+8(FP), SI
	MOVQ xStride+16(FP), R8
	MOVQ w+24(FP), DI
	MOVQ wStride+32(FP), R9
	MOVQ dst+40(FP), DX
	MOVQ dstStride+48(FP), R10

	LEAQ (SI)(R8*1), R11  // x row 1
	LEAQ (R11)(R8*1), R12 // x row 2
	LEAQ (R12)(R8*1), R13 // x row 3

	VXORPS Y0, Y0, Y0
	VXORPS Y1, Y1, Y1
	VXORPS Y2, Y2, Y2
	VXORPS Y3, Y3, Y3
	VXORPS Y4, Y4, Y4
	VXORPS Y5, Y5, Y5
	VXORPS Y6, Y6, Y6
	VXORPS Y7, Y7, Y7
	VXORPS Y8, Y8, Y8
	VXORPS Y9, Y9, Y9
	VXORPS Y10, Y10, Y10
	VXORPS Y11, Y11, Y11

	TESTQ CX, CX
	JZ    store24

loop24:
	VMOVUPS      (DI), Y12
	VMOVUPS      32(DI), Y13
	VMOVUPS      64(DI), Y14
	VBROADCASTSS (SI), Y15
	VFMADD231PS  Y12, Y15, Y0
	VFMADD231PS  Y13, Y15, Y1
	VFMADD231PS  Y14, Y15, Y2
	VBROADCASTSS (R11), Y15
	VFMADD231PS  Y12, Y15, Y3
	VFMADD231PS  Y13, Y15, Y4
	VFMADD231PS  Y14, Y15, Y5
	VBROADCASTSS (R12), Y15
	VFMADD231PS  Y12, Y15, Y6
	VFMADD231PS  Y13, Y15, Y7
	VFMADD231PS  Y14, Y15, Y8
	VBROADCASTSS (R13), Y15
	VFMADD231PS  Y12, Y15, Y9
	VFMADD231PS  Y13, Y15, Y10
	VFMADD231PS  Y14, Y15, Y11
	ADDQ         $4, SI
	ADDQ         $4, R11
	ADDQ         $4, R12
	ADDQ         $4, R13
	ADDQ         R9, DI
	DECQ         CX
	JNZ          loop24

store24:
	VMOVUPS Y0, (DX)
	VMOVUPS Y1, 32(DX)
	VMOVUPS Y2, 64(DX)
	ADDQ    R10, DX
	VMOVUPS Y3, (DX)
	VMOVUPS Y4, 32(DX)
	VMOVUPS Y5, 64(DX)
	ADDQ    R10, DX
	VMOVUPS Y6, (DX)
	VMOVUPS Y7, 32(DX)
	VMOVUPS Y8, 64(DX)
	ADDQ    R10, DX
	VMOVUPS Y9, (DX)
	VMOVUPS Y10, 32(DX)
	VMOVUPS Y11, 64(DX)
	VZEROUPPER
	RET
