//go:build !purego

#include "textflag.h"

// func avgRows(dst *byte, ds int, a *byte, as int, b *byte, bs int, w, h int)
// Go's assembler has no URHADD, so it is emitted as a raw word.
TEXT ·avgRows(SB), NOSPLIT, $0-64
	MOVD dst+0(FP), R0
	MOVD ds+8(FP), R1
	MOVD a+16(FP), R2
	MOVD as+24(FP), R3
	MOVD b+32(FP), R4
	MOVD bs+40(FP), R5
	MOVD w+48(FP), R6
	MOVD h+56(FP), R7
	CMP  $16, R6
	BNE  w8

loop16:
	VLD1 (R2), [V0.B16]
	VLD1 (R4), [V1.B16]
	WORD $0x6E211402 // URHADD V2.16B, V0.16B, V1.16B
	VST1 [V2.B16], (R0)
	ADD  R1, R0
	ADD  R3, R2
	ADD  R5, R4
	SUBS $1, R7
	BNE  loop16
	RET

w8:
	FMOVD (R2), F0
	FMOVD (R4), F1
	WORD  $0x2E211402 // URHADD V2.8B, V0.8B, V1.8B
	FMOVD F2, (R0)
	ADD   R1, R0
	ADD   R3, R2
	ADD   R5, R4
	SUBS  $1, R7
	BNE   w8
	RET
