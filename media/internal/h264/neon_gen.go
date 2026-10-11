//go:build ignore

// neon_gen writes neon_arm64.s. Go's arm64 assembler lacks most NEON
// arithmetic (UADDL, MLA, SQRSHRUN, UMULL, ...), so the kernels are written
// in standard arm64 syntax below, assembled with the system clang, and
// emitted as WORDs — each annotated with its instruction. The kernels are
// leaf routines using only R0–R15 and V0–V31, with arguments loaded from
// the Go (ABI0) frame by a generated prologue.
//
//	go run neon_gen.go
package main

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type kernel struct {
	name string
	args []string // Go argument names, each 8 bytes, loaded into R0, R1, ...
	body string
}

var kernels = []kernel{
	{
		// 6-tap horizontal half-sample filter, w = 16 or 8.
		// x0 dst, x1 dst stride, x2 src (2 samples left of the block),
		// x3 src stride, x4 w, x5 h.
		name: "neonLumaH",
		args: []string{"dst", "ds", "src", "ss", "w", "h"},
		body: `
	movi v30.8h, #20
	movi v31.8h, #5
	cmp x4, #16
	b.ne 8f
1:
	ld1 {v0.16b, v1.16b}, [x2], x3
	ext v2.16b, v0.16b, v1.16b, #1
	ext v3.16b, v0.16b, v1.16b, #2
	ext v4.16b, v0.16b, v1.16b, #3
	ext v5.16b, v0.16b, v1.16b, #4
	ext v6.16b, v0.16b, v1.16b, #5
	uaddl v16.8h, v0.8b, v6.8b
	uaddl2 v17.8h, v0.16b, v6.16b
	uaddl v18.8h, v3.8b, v4.8b
	uaddl2 v19.8h, v3.16b, v4.16b
	uaddl v20.8h, v2.8b, v5.8b
	uaddl2 v21.8h, v2.16b, v5.16b
	mla v16.8h, v18.8h, v30.8h
	mla v17.8h, v19.8h, v30.8h
	mls v16.8h, v20.8h, v31.8h
	mls v17.8h, v21.8h, v31.8h
	sqrshrun v0.8b, v16.8h, #5
	sqrshrun2 v0.16b, v17.8h, #5
	st1 {v0.16b}, [x0], x1
	subs x5, x5, #1
	b.ne 1b
	ret
8:
	ld1 {v0.16b}, [x2], x3
	ext v2.16b, v0.16b, v0.16b, #1
	ext v3.16b, v0.16b, v0.16b, #2
	ext v4.16b, v0.16b, v0.16b, #3
	ext v5.16b, v0.16b, v0.16b, #4
	ext v6.16b, v0.16b, v0.16b, #5
	uaddl v16.8h, v0.8b, v6.8b
	uaddl v18.8h, v3.8b, v4.8b
	uaddl v20.8h, v2.8b, v5.8b
	mla v16.8h, v18.8h, v30.8h
	mls v16.8h, v20.8h, v31.8h
	sqrshrun v0.8b, v16.8h, #5
	st1 {v0.8b}, [x0], x1
	subs x5, x5, #1
	b.ne 8b
	ret
`,
	},
	{
		// 6-tap vertical half-sample filter, w = 16 or 8.
		// x2 src points two rows above the block.
		name: "neonLumaV",
		args: []string{"dst", "ds", "src", "ss", "w", "h"},
		body: `
	movi v30.8h, #20
	movi v31.8h, #5
	cmp x4, #16
	b.ne 8f
1:
	mov x6, x2
	ld1 {v0.16b}, [x6], x3
	ld1 {v1.16b}, [x6], x3
	ld1 {v2.16b}, [x6], x3
	ld1 {v3.16b}, [x6], x3
	ld1 {v4.16b}, [x6], x3
	ld1 {v5.16b}, [x6]
	uaddl v16.8h, v0.8b, v5.8b
	uaddl2 v17.8h, v0.16b, v5.16b
	uaddl v18.8h, v2.8b, v3.8b
	uaddl2 v19.8h, v2.16b, v3.16b
	uaddl v20.8h, v1.8b, v4.8b
	uaddl2 v21.8h, v1.16b, v4.16b
	mla v16.8h, v18.8h, v30.8h
	mla v17.8h, v19.8h, v30.8h
	mls v16.8h, v20.8h, v31.8h
	mls v17.8h, v21.8h, v31.8h
	sqrshrun v0.8b, v16.8h, #5
	sqrshrun2 v0.16b, v17.8h, #5
	st1 {v0.16b}, [x0], x1
	add x2, x2, x3
	subs x5, x5, #1
	b.ne 1b
	ret
8:
	mov x6, x2
	ld1 {v0.8b}, [x6], x3
	ld1 {v1.8b}, [x6], x3
	ld1 {v2.8b}, [x6], x3
	ld1 {v3.8b}, [x6], x3
	ld1 {v4.8b}, [x6], x3
	ld1 {v5.8b}, [x6]
	uaddl v16.8h, v0.8b, v5.8b
	uaddl v18.8h, v2.8b, v3.8b
	uaddl v20.8h, v1.8b, v4.8b
	mla v16.8h, v18.8h, v30.8h
	mls v16.8h, v20.8h, v31.8h
	sqrshrun v0.8b, v16.8h, #5
	st1 {v0.8b}, [x0], x1
	add x2, x2, x3
	subs x5, x5, #1
	b.ne 8b
	ret
`,
	},
	{
		// Centre half-sample position j: a vertical 6-tap pass over 24
		// columns into an int16 scratch (rows of 24 = 48 bytes), then a
		// horizontal 6-tap pass in 32-bit with (v+512)>>10 and clipping.
		// x2 src points two rows above and two samples left of the block;
		// x6 is the scratch (at least h*24 int16). w = 16 or 8.
		name: "neonLumaHV",
		args: []string{"dst", "ds", "src", "ss", "w", "h", "tmp"},
		body: `
	movi v30.8h, #20
	movi v31.8h, #5
	mov x7, x6
	mov x8, x5
1:
	mov x9, x2
	ldr q0, [x9]
	ldr d24, [x9, #16]
	add x9, x9, x3
	ldr q1, [x9]
	ldr d25, [x9, #16]
	add x9, x9, x3
	ldr q2, [x9]
	ldr d26, [x9, #16]
	add x9, x9, x3
	ldr q3, [x9]
	ldr d27, [x9, #16]
	add x9, x9, x3
	ldr q4, [x9]
	ldr d28, [x9, #16]
	add x9, x9, x3
	ldr q5, [x9]
	ldr d29, [x9, #16]
	uaddl v16.8h, v0.8b, v5.8b
	uaddl2 v17.8h, v0.16b, v5.16b
	uaddl v18.8h, v24.8b, v29.8b
	uaddl v19.8h, v2.8b, v3.8b
	uaddl2 v20.8h, v2.16b, v3.16b
	uaddl v21.8h, v26.8b, v27.8b
	uaddl v22.8h, v1.8b, v4.8b
	uaddl2 v23.8h, v1.16b, v4.16b
	uaddl v6.8h, v25.8b, v28.8b
	mla v16.8h, v19.8h, v30.8h
	mla v17.8h, v20.8h, v30.8h
	mla v18.8h, v21.8h, v30.8h
	mls v16.8h, v22.8h, v31.8h
	mls v17.8h, v23.8h, v31.8h
	mls v18.8h, v6.8h, v31.8h
	st1 {v16.8h, v17.8h, v18.8h}, [x7], #48
	add x2, x2, x3
	subs x8, x8, #1
	b.ne 1b

	movi v30.4s, #20
	movi v31.4s, #5
	mov x7, x6
2:
	ld1 {v0.8h, v1.8h, v2.8h}, [x7], #48
	ext v3.16b, v0.16b, v1.16b, #2
	ext v4.16b, v0.16b, v1.16b, #4
	ext v5.16b, v0.16b, v1.16b, #6
	ext v6.16b, v0.16b, v1.16b, #8
	ext v7.16b, v0.16b, v1.16b, #10
	saddl v16.4s, v0.4h, v7.4h
	saddl2 v17.4s, v0.8h, v7.8h
	saddl v18.4s, v4.4h, v5.4h
	saddl2 v19.4s, v4.8h, v5.8h
	saddl v20.4s, v3.4h, v6.4h
	saddl2 v21.4s, v3.8h, v6.8h
	mla v16.4s, v18.4s, v30.4s
	mla v17.4s, v19.4s, v30.4s
	mls v16.4s, v20.4s, v31.4s
	mls v17.4s, v21.4s, v31.4s
	sqrshrun v22.4h, v16.4s, #10
	sqrshrun2 v22.8h, v17.4s, #10
	uqxtn v23.8b, v22.8h
	cmp x4, #16
	b.ne 3f
	ext v3.16b, v1.16b, v2.16b, #2
	ext v4.16b, v1.16b, v2.16b, #4
	ext v5.16b, v1.16b, v2.16b, #6
	ext v6.16b, v1.16b, v2.16b, #8
	ext v7.16b, v1.16b, v2.16b, #10
	saddl v16.4s, v1.4h, v7.4h
	saddl2 v17.4s, v1.8h, v7.8h
	saddl v18.4s, v4.4h, v5.4h
	saddl2 v19.4s, v4.8h, v5.8h
	saddl v20.4s, v3.4h, v6.4h
	saddl2 v21.4s, v3.8h, v6.8h
	mla v16.4s, v18.4s, v30.4s
	mla v17.4s, v19.4s, v30.4s
	mls v16.4s, v20.4s, v31.4s
	mls v17.4s, v21.4s, v31.4s
	sqrshrun v22.4h, v16.4s, #10
	sqrshrun2 v22.8h, v17.4s, #10
	uqxtn2 v23.16b, v22.8h
	st1 {v23.16b}, [x0], x1
	b 4f
3:
	st1 {v23.8b}, [x0], x1
4:
	subs x5, x5, #1
	b.ne 2b
	ret
`,
	},
	{
		// Chroma eighth-sample bilinear prediction, w = 8 or 4:
		// (a*A + b*B + c*C + d*D + 32) >> 6.
		name: "neonChroma",
		args: []string{"dst", "ds", "src", "ss", "w", "h", "a", "b", "c", "d"},
		body: `
	dup v28.8b, w6
	dup v29.8b, w7
	dup v30.8b, w8
	dup v31.8b, w9
	cmp x4, #8
	b.ne 4f
	ld1 {v0.16b}, [x2], x3
1:
	ld1 {v1.16b}, [x2], x3
	ext v2.16b, v0.16b, v0.16b, #1
	ext v3.16b, v1.16b, v1.16b, #1
	umull v16.8h, v0.8b, v28.8b
	umlal v16.8h, v2.8b, v29.8b
	umlal v16.8h, v1.8b, v30.8b
	umlal v16.8h, v3.8b, v31.8b
	rshrn v16.8b, v16.8h, #6
	st1 {v16.8b}, [x0], x1
	mov v0.16b, v1.16b
	subs x5, x5, #1
	b.ne 1b
	ret
4:
	ld1 {v0.8b}, [x2], x3
5:
	ld1 {v1.8b}, [x2], x3
	ext v2.8b, v0.8b, v0.8b, #1
	ext v3.8b, v1.8b, v1.8b, #1
	umull v16.8h, v0.8b, v28.8b
	umlal v16.8h, v2.8b, v29.8b
	umlal v16.8h, v1.8b, v30.8b
	umlal v16.8h, v3.8b, v31.8b
	rshrn v16.8b, v16.8h, #6
	st1 {v16.s}[0], [x0], x1
	mov v0.16b, v1.16b
	subs x5, x5, #1
	b.ne 5b
	ret
`,
	},
	{
		// Luma deblocking of one 16-sample edge (spec 8.7.2). x0 points at
		// q0 of the first line, x1 is the stride, x2 = 1 for a vertical
		// edge (lines are rows: transposed in and out), x3/x4 alpha/beta.
		// Normal filter (x6 = 0): x5 points at 16 per-line tc0 values,
		// -1 for lines with bS 0. Strong filter (x6 = 1): bS 4 throughout.
		name: "neonLumaEdge",
		args: []string{"pix", "stride", "vert", "alpha", "beta", "tc0", "strong"},
		body: `
	dup v24.16b, w3
	dup v25.16b, w4
	cbnz x6, 1f
	ld1 {v26.16b}, [x5]
	b 2f
1:
	lsr w7, w3, #2
	add w7, w7, #2
	dup v26.16b, w7
2:
	cbnz x2, 3f
	lsl x9, x1, #2
	sub x8, x0, x9
	ld1 {v16.16b}, [x8], x1
	ld1 {v17.16b}, [x8], x1
	ld1 {v18.16b}, [x8], x1
	ld1 {v19.16b}, [x8], x1
	ld1 {v20.16b}, [x8], x1
	ld1 {v21.16b}, [x8], x1
	ld1 {v22.16b}, [x8], x1
	ld1 {v23.16b}, [x8], x1
	b 4f
3:
	sub x8, x0, #4
	ld1 {v16.d}[0], [x8], x1
	ld1 {v17.d}[0], [x8], x1
	ld1 {v18.d}[0], [x8], x1
	ld1 {v19.d}[0], [x8], x1
	ld1 {v20.d}[0], [x8], x1
	ld1 {v21.d}[0], [x8], x1
	ld1 {v22.d}[0], [x8], x1
	ld1 {v23.d}[0], [x8], x1
	ld1 {v16.d}[1], [x8], x1
	ld1 {v17.d}[1], [x8], x1
	ld1 {v18.d}[1], [x8], x1
	ld1 {v19.d}[1], [x8], x1
	ld1 {v20.d}[1], [x8], x1
	ld1 {v21.d}[1], [x8], x1
	ld1 {v22.d}[1], [x8], x1
	ld1 {v23.d}[1], [x8], x1
	trn1 v0.16b, v16.16b, v17.16b
	trn2 v1.16b, v16.16b, v17.16b
	trn1 v2.16b, v18.16b, v19.16b
	trn2 v3.16b, v18.16b, v19.16b
	trn1 v4.16b, v20.16b, v21.16b
	trn2 v5.16b, v20.16b, v21.16b
	trn1 v6.16b, v22.16b, v23.16b
	trn2 v7.16b, v22.16b, v23.16b
	trn1 v8.8h, v0.8h, v2.8h
	trn2 v10.8h, v0.8h, v2.8h
	trn1 v9.8h, v1.8h, v3.8h
	trn2 v11.8h, v1.8h, v3.8h
	trn1 v12.8h, v4.8h, v6.8h
	trn2 v14.8h, v4.8h, v6.8h
	trn1 v13.8h, v5.8h, v7.8h
	trn2 v15.8h, v5.8h, v7.8h
	trn1 v16.4s, v8.4s, v12.4s
	trn2 v20.4s, v8.4s, v12.4s
	trn1 v17.4s, v9.4s, v13.4s
	trn2 v21.4s, v9.4s, v13.4s
	trn1 v18.4s, v10.4s, v14.4s
	trn2 v22.4s, v10.4s, v14.4s
	trn1 v19.4s, v11.4s, v15.4s
	trn2 v23.4s, v11.4s, v15.4s
4:
	uabd v0.16b, v19.16b, v20.16b
	cmhi v0.16b, v24.16b, v0.16b
	uabd v1.16b, v18.16b, v19.16b
	cmhi v1.16b, v25.16b, v1.16b
	and v0.16b, v0.16b, v1.16b
	uabd v1.16b, v21.16b, v20.16b
	cmhi v1.16b, v25.16b, v1.16b
	and v0.16b, v0.16b, v1.16b
	uabd v2.16b, v17.16b, v19.16b
	cmhi v2.16b, v25.16b, v2.16b
	uabd v3.16b, v22.16b, v20.16b
	cmhi v3.16b, v25.16b, v3.16b
	cbnz x6, 5f

	cmge v1.16b, v26.16b, #0
	and v0.16b, v0.16b, v1.16b
	and v2.16b, v2.16b, v0.16b
	and v3.16b, v3.16b, v0.16b
	sub v4.16b, v26.16b, v2.16b
	sub v4.16b, v4.16b, v3.16b
	usubl v5.8h, v20.8b, v19.8b
	usubl2 v6.8h, v20.16b, v19.16b
	shl v5.8h, v5.8h, #2
	shl v6.8h, v6.8h, #2
	usubl v7.8h, v18.8b, v21.8b
	usubl2 v27.8h, v18.16b, v21.16b
	add v5.8h, v5.8h, v7.8h
	add v6.8h, v6.8h, v27.8h
	sqrshrn v5.8b, v5.8h, #3
	sqrshrn2 v5.16b, v6.8h, #3
	neg v6.16b, v4.16b
	smin v5.16b, v5.16b, v4.16b
	smax v5.16b, v5.16b, v6.16b
	and v5.16b, v5.16b, v0.16b
	urhadd v7.16b, v19.16b, v20.16b
	neg v31.16b, v26.16b
	uaddl v27.8h, v17.8b, v7.8b
	uaddl2 v28.8h, v17.16b, v7.16b
	ushll v29.8h, v18.8b, #1
	ushll2 v30.8h, v18.16b, #1
	sub v27.8h, v27.8h, v29.8h
	sub v28.8h, v28.8h, v30.8h
	sqshrn v27.8b, v27.8h, #1
	sqshrn2 v27.16b, v28.8h, #1
	smin v27.16b, v27.16b, v26.16b
	smax v27.16b, v27.16b, v31.16b
	and v27.16b, v27.16b, v2.16b
	add v18.16b, v18.16b, v27.16b
	uaddl v27.8h, v22.8b, v7.8b
	uaddl2 v28.8h, v22.16b, v7.16b
	ushll v29.8h, v21.8b, #1
	ushll2 v30.8h, v21.16b, #1
	sub v27.8h, v27.8h, v29.8h
	sub v28.8h, v28.8h, v30.8h
	sqshrn v27.8b, v27.8h, #1
	sqshrn2 v27.16b, v28.8h, #1
	smin v27.16b, v27.16b, v26.16b
	smax v27.16b, v27.16b, v31.16b
	and v27.16b, v27.16b, v3.16b
	add v21.16b, v21.16b, v27.16b
	usqadd v19.16b, v5.16b
	neg v5.16b, v5.16b
	usqadd v20.16b, v5.16b
	b 6f

5:
	uabd v1.16b, v19.16b, v20.16b
	cmhi v1.16b, v26.16b, v1.16b
	and v2.16b, v2.16b, v1.16b
	and v2.16b, v2.16b, v0.16b
	and v3.16b, v3.16b, v1.16b
	and v3.16b, v3.16b, v0.16b
	uaddl v4.8h, v17.8b, v18.8b
	uaddl2 v5.8h, v17.16b, v18.16b
	uaddl v6.8h, v19.8b, v20.8b
	uaddl2 v7.8h, v19.16b, v20.16b
	add v4.8h, v4.8h, v6.8h
	add v5.8h, v5.8h, v7.8h
	rshrn v8.8b, v4.8h, #2
	rshrn2 v8.16b, v5.8h, #2
	uaddl v27.8h, v18.8b, v21.8b
	uaddl2 v28.8h, v18.16b, v21.16b
	add v27.8h, v27.8h, v4.8h
	add v28.8h, v28.8h, v5.8h
	add v27.8h, v27.8h, v6.8h
	add v28.8h, v28.8h, v7.8h
	rshrn v9.8b, v27.8h, #3
	rshrn2 v9.16b, v28.8h, #3
	uaddl v27.8h, v16.8b, v17.8b
	uaddl2 v28.8h, v16.16b, v17.16b
	shl v27.8h, v27.8h, #1
	shl v28.8h, v28.8h, #1
	add v27.8h, v27.8h, v4.8h
	add v28.8h, v28.8h, v5.8h
	rshrn v10.8b, v27.8h, #3
	rshrn2 v10.16b, v28.8h, #3
	ushll v27.8h, v18.8b, #1
	ushll2 v28.8h, v18.16b, #1
	uaddw v27.8h, v27.8h, v19.8b
	uaddw2 v28.8h, v28.8h, v19.16b
	uaddw v27.8h, v27.8h, v21.8b
	uaddw2 v28.8h, v28.8h, v21.16b
	rshrn v11.8b, v27.8h, #2
	rshrn2 v11.16b, v28.8h, #2
	uaddl v4.8h, v22.8b, v21.8b
	uaddl2 v5.8h, v22.16b, v21.16b
	add v4.8h, v4.8h, v6.8h
	add v5.8h, v5.8h, v7.8h
	rshrn v12.8b, v4.8h, #2
	rshrn2 v12.16b, v5.8h, #2
	uaddl v27.8h, v21.8b, v18.8b
	uaddl2 v28.8h, v21.16b, v18.16b
	add v27.8h, v27.8h, v4.8h
	add v28.8h, v28.8h, v5.8h
	add v27.8h, v27.8h, v6.8h
	add v28.8h, v28.8h, v7.8h
	rshrn v13.8b, v27.8h, #3
	rshrn2 v13.16b, v28.8h, #3
	uaddl v27.8h, v23.8b, v22.8b
	uaddl2 v28.8h, v23.16b, v22.16b
	shl v27.8h, v27.8h, #1
	shl v28.8h, v28.8h, #1
	add v27.8h, v27.8h, v4.8h
	add v28.8h, v28.8h, v5.8h
	rshrn v14.8b, v27.8h, #3
	rshrn2 v14.16b, v28.8h, #3
	ushll v27.8h, v21.8b, #1
	ushll2 v28.8h, v21.16b, #1
	uaddw v27.8h, v27.8h, v20.8b
	uaddw2 v28.8h, v28.8h, v20.16b
	uaddw v27.8h, v27.8h, v18.8b
	uaddw2 v28.8h, v28.8h, v18.16b
	rshrn v15.8b, v27.8h, #2
	rshrn2 v15.16b, v28.8h, #2
	mov v27.16b, v2.16b
	bsl v27.16b, v9.16b, v11.16b
	mov v28.16b, v0.16b
	bsl v28.16b, v27.16b, v19.16b
	mov v19.16b, v28.16b
	mov v27.16b, v2.16b
	bsl v27.16b, v8.16b, v18.16b
	mov v18.16b, v27.16b
	mov v27.16b, v2.16b
	bsl v27.16b, v10.16b, v17.16b
	mov v17.16b, v27.16b
	mov v27.16b, v3.16b
	bsl v27.16b, v13.16b, v15.16b
	mov v28.16b, v0.16b
	bsl v28.16b, v27.16b, v20.16b
	mov v20.16b, v28.16b
	mov v27.16b, v3.16b
	bsl v27.16b, v12.16b, v21.16b
	mov v21.16b, v27.16b
	mov v27.16b, v3.16b
	bsl v27.16b, v14.16b, v22.16b
	mov v22.16b, v27.16b

6:
	cbnz x2, 7f
	add x9, x1, x1, lsl #1
	sub x8, x0, x9
	st1 {v17.16b}, [x8], x1
	st1 {v18.16b}, [x8], x1
	st1 {v19.16b}, [x8], x1
	st1 {v20.16b}, [x8], x1
	st1 {v21.16b}, [x8], x1
	st1 {v22.16b}, [x8], x1
	ret
7:
	trn1 v0.16b, v16.16b, v17.16b
	trn2 v1.16b, v16.16b, v17.16b
	trn1 v2.16b, v18.16b, v19.16b
	trn2 v3.16b, v18.16b, v19.16b
	trn1 v4.16b, v20.16b, v21.16b
	trn2 v5.16b, v20.16b, v21.16b
	trn1 v6.16b, v22.16b, v23.16b
	trn2 v7.16b, v22.16b, v23.16b
	trn1 v8.8h, v0.8h, v2.8h
	trn2 v10.8h, v0.8h, v2.8h
	trn1 v9.8h, v1.8h, v3.8h
	trn2 v11.8h, v1.8h, v3.8h
	trn1 v12.8h, v4.8h, v6.8h
	trn2 v14.8h, v4.8h, v6.8h
	trn1 v13.8h, v5.8h, v7.8h
	trn2 v15.8h, v5.8h, v7.8h
	trn1 v16.4s, v8.4s, v12.4s
	trn2 v20.4s, v8.4s, v12.4s
	trn1 v17.4s, v9.4s, v13.4s
	trn2 v21.4s, v9.4s, v13.4s
	trn1 v18.4s, v10.4s, v14.4s
	trn2 v22.4s, v10.4s, v14.4s
	trn1 v19.4s, v11.4s, v15.4s
	trn2 v23.4s, v11.4s, v15.4s
	sub x8, x0, #4
	st1 {v16.d}[0], [x8], x1
	st1 {v17.d}[0], [x8], x1
	st1 {v18.d}[0], [x8], x1
	st1 {v19.d}[0], [x8], x1
	st1 {v20.d}[0], [x8], x1
	st1 {v21.d}[0], [x8], x1
	st1 {v22.d}[0], [x8], x1
	st1 {v23.d}[0], [x8], x1
	st1 {v16.d}[1], [x8], x1
	st1 {v17.d}[1], [x8], x1
	st1 {v18.d}[1], [x8], x1
	st1 {v19.d}[1], [x8], x1
	st1 {v20.d}[1], [x8], x1
	st1 {v21.d}[1], [x8], x1
	st1 {v22.d}[1], [x8], x1
	st1 {v23.d}[1], [x8], x1
	ret
`,
	},
	{
		// Chroma deblocking of one 8-sample edge in Cb and Cr at once:
		// lanes 0-7 Cb, 8-15 Cr. x0/x1 point at q0 of the first line in
		// Cb/Cr, x2 is the stride, x3 = 1 for a vertical edge (rows are
		// gathered/scattered with TBL using the indices at x6/x7). x4 points
		// at 48 bytes: per-lane alpha, beta, tc (tc0+1, -1 for bS 0).
		// x5 = 1 selects the bS 4 filter.
		name: "neonChromaEdge",
		args: []string{"cb", "cr", "stride", "vert", "prm", "strong", "gather", "scatter"},
		body: `
	ld1 {v24.16b, v25.16b, v26.16b}, [x4]
	cbnz x3, 1f
	sub x8, x0, x2, lsl #1
	sub x9, x1, x2, lsl #1
	ld1 {v16.d}[0], [x8], x2
	ld1 {v16.d}[1], [x9], x2
	ld1 {v17.d}[0], [x8], x2
	ld1 {v17.d}[1], [x9], x2
	ld1 {v18.d}[0], [x8], x2
	ld1 {v18.d}[1], [x9], x2
	ld1 {v19.d}[0], [x8]
	ld1 {v19.d}[1], [x9]
	b 2f
1:
	ld1 {v28.16b, v29.16b, v30.16b, v31.16b}, [x6]
	sub x8, x0, #2
	sub x9, x1, #2
	ld1 {v0.s}[0], [x8], x2
	ld1 {v0.s}[1], [x8], x2
	ld1 {v0.s}[2], [x8], x2
	ld1 {v0.s}[3], [x8], x2
	ld1 {v1.s}[0], [x8], x2
	ld1 {v1.s}[1], [x8], x2
	ld1 {v1.s}[2], [x8], x2
	ld1 {v1.s}[3], [x8]
	ld1 {v2.s}[0], [x9], x2
	ld1 {v2.s}[1], [x9], x2
	ld1 {v2.s}[2], [x9], x2
	ld1 {v2.s}[3], [x9], x2
	ld1 {v3.s}[0], [x9], x2
	ld1 {v3.s}[1], [x9], x2
	ld1 {v3.s}[2], [x9], x2
	ld1 {v3.s}[3], [x9]
	tbl v16.16b, {v0.16b, v1.16b, v2.16b, v3.16b}, v28.16b
	tbl v17.16b, {v0.16b, v1.16b, v2.16b, v3.16b}, v29.16b
	tbl v18.16b, {v0.16b, v1.16b, v2.16b, v3.16b}, v30.16b
	tbl v19.16b, {v0.16b, v1.16b, v2.16b, v3.16b}, v31.16b
2:
	uabd v0.16b, v17.16b, v18.16b
	cmhi v0.16b, v24.16b, v0.16b
	uabd v1.16b, v16.16b, v17.16b
	cmhi v1.16b, v25.16b, v1.16b
	and v0.16b, v0.16b, v1.16b
	uabd v1.16b, v19.16b, v18.16b
	cmhi v1.16b, v25.16b, v1.16b
	and v0.16b, v0.16b, v1.16b
	cbnz x5, 3f
	cmge v1.16b, v26.16b, #0
	and v0.16b, v0.16b, v1.16b
	usubl v5.8h, v18.8b, v17.8b
	usubl2 v6.8h, v18.16b, v17.16b
	shl v5.8h, v5.8h, #2
	shl v6.8h, v6.8h, #2
	usubl v7.8h, v16.8b, v19.8b
	usubl2 v27.8h, v16.16b, v19.16b
	add v5.8h, v5.8h, v7.8h
	add v6.8h, v6.8h, v27.8h
	sqrshrn v5.8b, v5.8h, #3
	sqrshrn2 v5.16b, v6.8h, #3
	neg v6.16b, v26.16b
	smin v5.16b, v5.16b, v26.16b
	smax v5.16b, v5.16b, v6.16b
	and v5.16b, v5.16b, v0.16b
	usqadd v17.16b, v5.16b
	neg v5.16b, v5.16b
	usqadd v18.16b, v5.16b
	b 4f
3:
	ushll v5.8h, v16.8b, #1
	ushll2 v6.8h, v16.16b, #1
	uaddw v5.8h, v5.8h, v17.8b
	uaddw2 v6.8h, v6.8h, v17.16b
	uaddw v5.8h, v5.8h, v19.8b
	uaddw2 v6.8h, v6.8h, v19.16b
	rshrn v7.8b, v5.8h, #2
	rshrn2 v7.16b, v6.8h, #2
	ushll v5.8h, v19.8b, #1
	ushll2 v6.8h, v19.16b, #1
	uaddw v5.8h, v5.8h, v18.8b
	uaddw2 v6.8h, v6.8h, v18.16b
	uaddw v5.8h, v5.8h, v16.8b
	uaddw2 v6.8h, v6.8h, v16.16b
	rshrn v27.8b, v5.8h, #2
	rshrn2 v27.16b, v6.8h, #2
	bit v17.16b, v7.16b, v0.16b
	bit v18.16b, v27.16b, v0.16b
4:
	cbnz x3, 5f
	sub x8, x0, x2
	sub x9, x1, x2
	st1 {v17.d}[0], [x8], x2
	st1 {v17.d}[1], [x9], x2
	st1 {v18.d}[0], [x8]
	st1 {v18.d}[1], [x9]
	ret
5:
	ld1 {v28.16b, v29.16b, v30.16b, v31.16b}, [x7]
	tbl v0.16b, {v16.16b, v17.16b, v18.16b, v19.16b}, v28.16b
	tbl v1.16b, {v16.16b, v17.16b, v18.16b, v19.16b}, v29.16b
	tbl v2.16b, {v16.16b, v17.16b, v18.16b, v19.16b}, v30.16b
	tbl v3.16b, {v16.16b, v17.16b, v18.16b, v19.16b}, v31.16b
	sub x8, x0, #2
	sub x9, x1, #2
	st1 {v0.s}[0], [x8], x2
	st1 {v0.s}[1], [x8], x2
	st1 {v0.s}[2], [x8], x2
	st1 {v0.s}[3], [x8], x2
	st1 {v1.s}[0], [x8], x2
	st1 {v1.s}[1], [x8], x2
	st1 {v1.s}[2], [x8], x2
	st1 {v1.s}[3], [x8]
	st1 {v2.s}[0], [x9], x2
	st1 {v2.s}[1], [x9], x2
	st1 {v2.s}[2], [x9], x2
	st1 {v2.s}[3], [x9], x2
	st1 {v3.s}[0], [x9], x2
	st1 {v3.s}[1], [x9], x2
	st1 {v3.s}[2], [x9], x2
	st1 {v3.s}[3], [x9]
	ret
`,
	},
	{
		// 4x4 inverse transform added to the prediction (spec 8.5.12),
		// then the coefficient block is cleared. x0 dst, x1 stride, x2 the
		// 16 int32 coefficients (raster).
		name: "neonIdct4Add",
		args: []string{"dst", "stride", "blk"},
		body: `
	ld1 {v0.4s, v1.4s, v2.4s, v3.4s}, [x2]
	trn1 v4.4s, v0.4s, v1.4s
	trn2 v5.4s, v0.4s, v1.4s
	trn1 v6.4s, v2.4s, v3.4s
	trn2 v7.4s, v2.4s, v3.4s
	trn1 v0.2d, v4.2d, v6.2d
	trn2 v2.2d, v4.2d, v6.2d
	trn1 v1.2d, v5.2d, v7.2d
	trn2 v3.2d, v5.2d, v7.2d
	add v4.4s, v0.4s, v2.4s
	sub v5.4s, v0.4s, v2.4s
	sshr v6.4s, v1.4s, #1
	sub v6.4s, v6.4s, v3.4s
	sshr v7.4s, v3.4s, #1
	add v7.4s, v1.4s, v7.4s
	add v0.4s, v4.4s, v7.4s
	add v1.4s, v5.4s, v6.4s
	sub v2.4s, v5.4s, v6.4s
	sub v3.4s, v4.4s, v7.4s
	trn1 v4.4s, v0.4s, v1.4s
	trn2 v5.4s, v0.4s, v1.4s
	trn1 v6.4s, v2.4s, v3.4s
	trn2 v7.4s, v2.4s, v3.4s
	trn1 v0.2d, v4.2d, v6.2d
	trn2 v2.2d, v4.2d, v6.2d
	trn1 v1.2d, v5.2d, v7.2d
	trn2 v3.2d, v5.2d, v7.2d
	add v4.4s, v0.4s, v2.4s
	sub v5.4s, v0.4s, v2.4s
	sshr v6.4s, v1.4s, #1
	sub v6.4s, v6.4s, v3.4s
	sshr v7.4s, v3.4s, #1
	add v7.4s, v1.4s, v7.4s
	add v0.4s, v4.4s, v7.4s
	add v1.4s, v5.4s, v6.4s
	sub v2.4s, v5.4s, v6.4s
	sub v3.4s, v4.4s, v7.4s
	srshr v0.4s, v0.4s, #6
	srshr v1.4s, v1.4s, #6
	srshr v2.4s, v2.4s, #6
	srshr v3.4s, v3.4s, #6
	mov x8, x0
	ld1 {v20.s}[0], [x8], x1
	ld1 {v20.s}[1], [x8], x1
	ld1 {v21.s}[0], [x8], x1
	ld1 {v21.s}[1], [x8]
	uxtl v20.8h, v20.8b
	uxtl v21.8h, v21.8b
	uxtl v22.4s, v20.4h
	uxtl2 v23.4s, v20.8h
	uxtl v24.4s, v21.4h
	uxtl2 v25.4s, v21.8h
	add v0.4s, v0.4s, v22.4s
	add v1.4s, v1.4s, v23.4s
	add v2.4s, v2.4s, v24.4s
	add v3.4s, v3.4s, v25.4s
	sqxtn v0.4h, v0.4s
	sqxtn2 v0.8h, v1.4s
	sqxtn v2.4h, v2.4s
	sqxtn2 v2.8h, v3.4s
	sqxtun v0.8b, v0.8h
	sqxtun v2.8b, v2.8h
	st1 {v0.s}[0], [x0], x1
	st1 {v0.s}[1], [x0], x1
	st1 {v2.s}[0], [x0], x1
	st1 {v2.s}[1], [x0]
	movi v28.16b, #0
	movi v29.16b, #0
	movi v30.16b, #0
	movi v31.16b, #0
	st1 {v28.4s, v29.4s, v30.4s, v31.4s}, [x2]
	ret
`,
	},
}

func main() {
	dir, err := os.MkdirTemp("", "neongen")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	var out bytes.Buffer
	out.WriteString("// Code generated by neon_gen.go; DO NOT EDIT.\n\n//go:build !purego\n\n#include \"textflag.h\"\n")
	for _, k := range kernels {
		words := assemble(dir, k)
		lines := instructions(k.body)
		if len(lines) != len(words) {
			log.Fatalf("%s: %d instructions but %d words", k.name, len(lines), len(words))
		}
		fmt.Fprintf(&out, "\n// func %s(%s)\n", k.name, strings.Join(k.args, ", "))
		fmt.Fprintf(&out, "TEXT ·%s(SB), NOSPLIT, $0-%d\n", k.name, 8*len(k.args))
		for i, a := range k.args {
			fmt.Fprintf(&out, "\tMOVD %s+%d(FP), R%d\n", a, 8*i, i)
		}
		for i, w := range words {
			fmt.Fprintf(&out, "\tWORD $0x%08x // %s\n", w, lines[i])
		}
		out.WriteString("\tRET\n")
	}
	if err := os.WriteFile("neon_arm64.s", out.Bytes(), 0o644); err != nil {
		log.Fatal(err)
	}
}

// instructions returns the body's instruction lines (labels and blanks
// dropped), in order.
func instructions(body string) []string {
	var out []string
	for _, l := range strings.Split(body, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasSuffix(l, ":") {
			continue
		}
		out = append(out, strings.Join(strings.Fields(l), " "))
	}
	return out
}

func assemble(dir string, k kernel) []uint32 {
	src := filepath.Join(dir, k.name+".s")
	obj := filepath.Join(dir, k.name+".o")
	text := ".text\n.globl _k\n_k:\n" + k.body
	if err := os.WriteFile(src, []byte(text), 0o644); err != nil {
		log.Fatal(err)
	}
	cmd := exec.Command("clang", "-c", "-arch", "arm64", src, "-o", obj)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("%s: clang: %v", k.name, err)
	}
	f, err := macho.Open(obj)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	sec := f.Section("__text")
	data, err := sec.Data()
	if err != nil {
		log.Fatal(err)
	}
	words := make([]uint32, len(data)/4)
	for i := range words {
		words[i] = binary.LittleEndian.Uint32(data[4*i:])
	}
	return words
}
