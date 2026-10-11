//go:build !purego

#include "textflag.h"

// func avgRows(dst *byte, ds int, a *byte, as int, b *byte, bs int, w, h int)
// PAVGB computes (a+b+1)>>1 per byte (SSE2, always present on amd64).
TEXT ·avgRows(SB), NOSPLIT, $0-64
	MOVQ dst+0(FP), DI
	MOVQ ds+8(FP), R8
	MOVQ a+16(FP), SI
	MOVQ as+24(FP), R9
	MOVQ b+32(FP), DX
	MOVQ bs+40(FP), R10
	MOVQ w+48(FP), AX
	MOVQ h+56(FP), CX
	CMPQ AX, $16
	JNE  w8

loop16:
	MOVOU (SI), X0
	MOVOU (DX), X1
	PAVGB X1, X0
	MOVOU X0, (DI)
	ADDQ  R8, DI
	ADDQ  R9, SI
	ADDQ  R10, DX
	DECQ  CX
	JNZ   loop16
	RET

w8:
	MOVQ  (SI), X0
	MOVQ  (DX), X1
	PAVGB X1, X0
	MOVQ  X0, (DI)
	ADDQ  R8, DI
	ADDQ  R9, SI
	ADDQ  R10, DX
	DECQ  CX
	JNZ   w8
	RET
