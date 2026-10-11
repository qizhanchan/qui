//go:build ignore

// neon_gen writes neon_arm64.s (the HEVC decoder's kernels). Go's arm64 assembler lacks most NEON
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
		// 8-tap horizontal filter of bytes into int16 plus off. x0 dst, x1 dst stride (bytes),
		// x2 src (3 samples left of the block), x3 src stride, x4 w (multiple of 4), x5 h,
		// x6 taps (8 int16), x7 off. Reads up to 16 bytes past the row start of each 8 outputs.
		name: "neonFilter8H",
		args: []string{"dst", "ds", "src", "ss", "w", "h", "f", "off"},
		body: `
	ld1 {v15.8h}, [x6]
	dup v29.8h, w7
1:
	mov x8, x2
	mov x9, x0
	mov x10, x4
2:
	ld1 {v0.16b}, [x8]
	add x8, x8, #8
	uxtl v1.8h, v0.8b
	uxtl2 v2.8h, v0.16b
	mov v16.16b, v29.16b
	mla v16.8h, v1.8h, v15.h[0]
	ext v3.16b, v1.16b, v2.16b, #2
	mla v16.8h, v3.8h, v15.h[1]
	ext v3.16b, v1.16b, v2.16b, #4
	mla v16.8h, v3.8h, v15.h[2]
	ext v3.16b, v1.16b, v2.16b, #6
	mla v16.8h, v3.8h, v15.h[3]
	ext v3.16b, v1.16b, v2.16b, #8
	mla v16.8h, v3.8h, v15.h[4]
	ext v3.16b, v1.16b, v2.16b, #10
	mla v16.8h, v3.8h, v15.h[5]
	ext v3.16b, v1.16b, v2.16b, #12
	mla v16.8h, v3.8h, v15.h[6]
	ext v3.16b, v1.16b, v2.16b, #14
	mla v16.8h, v3.8h, v15.h[7]
	cmp x10, #8
	b.lt 3f
	st1 {v16.8h}, [x9], #16
	subs x10, x10, #8
	b.ne 2b
	b 4f
3:
	st1 {v16.4h}, [x9]
4:
	add x2, x2, x3
	add x0, x0, x1
	subs x5, x5, #1
	b.ne 1b
`,
	},
	{
		// 4-tap horizontal filter (chroma); as neonFilter8H with 4 taps, src 1 sample left.
		name: "neonFilter4H",
		args: []string{"dst", "ds", "src", "ss", "w", "h", "f", "off"},
		body: `
	ld1 {v15.4h}, [x6]
	dup v29.8h, w7
1:
	mov x8, x2
	mov x9, x0
	mov x10, x4
2:
	ld1 {v0.16b}, [x8]
	add x8, x8, #8
	uxtl v1.8h, v0.8b
	uxtl2 v2.8h, v0.16b
	mov v16.16b, v29.16b
	mla v16.8h, v1.8h, v15.h[0]
	ext v3.16b, v1.16b, v2.16b, #2
	mla v16.8h, v3.8h, v15.h[1]
	ext v3.16b, v1.16b, v2.16b, #4
	mla v16.8h, v3.8h, v15.h[2]
	ext v3.16b, v1.16b, v2.16b, #6
	mla v16.8h, v3.8h, v15.h[3]
	cmp x10, #8
	b.lt 3f
	st1 {v16.8h}, [x9], #16
	subs x10, x10, #8
	b.ne 2b
	b 4f
3:
	st1 {v16.4h}, [x9]
4:
	add x2, x2, x3
	add x0, x0, x1
	subs x5, x5, #1
	b.ne 1b
`,
	},
	{
		// 8-tap vertical filter of bytes into int16 plus off; src is 3 rows above.
		name: "neonFilter8V",
		args: []string{"dst", "ds", "src", "ss", "w", "h", "f", "off"},
		body: `
	ld1 {v15.8h}, [x6]
	dup v29.8h, w7
1:
	mov x8, x2
	mov x9, x0
	mov x10, x4
2:
	mov x11, x8
	ld1 {v0.8b}, [x11], x3
	ld1 {v1.8b}, [x11], x3
	ld1 {v2.8b}, [x11], x3
	ld1 {v3.8b}, [x11], x3
	ld1 {v4.8b}, [x11], x3
	ld1 {v5.8b}, [x11], x3
	ld1 {v6.8b}, [x11], x3
	ld1 {v7.8b}, [x11], x3
	uxtl v0.8h, v0.8b
	uxtl v1.8h, v1.8b
	uxtl v2.8h, v2.8b
	uxtl v3.8h, v3.8b
	uxtl v4.8h, v4.8b
	uxtl v5.8h, v5.8b
	uxtl v6.8h, v6.8b
	uxtl v7.8h, v7.8b
	mov v16.16b, v29.16b
	mla v16.8h, v0.8h, v15.h[0]
	mla v16.8h, v1.8h, v15.h[1]
	mla v16.8h, v2.8h, v15.h[2]
	mla v16.8h, v3.8h, v15.h[3]
	mla v16.8h, v4.8h, v15.h[4]
	mla v16.8h, v5.8h, v15.h[5]
	mla v16.8h, v6.8h, v15.h[6]
	mla v16.8h, v7.8h, v15.h[7]
	cmp x10, #8
	b.lt 3f
	st1 {v16.8h}, [x9], #16
	add x8, x8, #8
	subs x10, x10, #8
	b.ne 2b
	b 4f
3:
	st1 {v16.4h}, [x9]
4:
	add x2, x2, x3
	add x0, x0, x1
	subs x5, x5, #1
	b.ne 1b
`,
	},
	{
		// 4-tap vertical filter of bytes; src is 1 row above.
		name: "neonFilter4V",
		args: []string{"dst", "ds", "src", "ss", "w", "h", "f", "off"},
		body: `
	ld1 {v15.4h}, [x6]
	dup v29.8h, w7
1:
	mov x8, x2
	mov x9, x0
	mov x10, x4
2:
	mov x11, x8
	ld1 {v0.8b}, [x11], x3
	ld1 {v1.8b}, [x11], x3
	ld1 {v2.8b}, [x11], x3
	ld1 {v3.8b}, [x11], x3
	uxtl v0.8h, v0.8b
	uxtl v1.8h, v1.8b
	uxtl v2.8h, v2.8b
	uxtl v3.8h, v3.8b
	mov v16.16b, v29.16b
	mla v16.8h, v0.8h, v15.h[0]
	mla v16.8h, v1.8h, v15.h[1]
	mla v16.8h, v2.8h, v15.h[2]
	mla v16.8h, v3.8h, v15.h[3]
	cmp x10, #8
	b.lt 3f
	st1 {v16.8h}, [x9], #16
	add x8, x8, #8
	subs x10, x10, #8
	b.ne 2b
	b 4f
3:
	st1 {v16.4h}, [x9]
4:
	add x2, x2, x3
	add x0, x0, x1
	subs x5, x5, #1
	b.ne 1b
`,
	},
	{
		// 8-tap second stage over int16 first-stage sums with 32-bit
		// accumulation: (sum >> 6) - 8192. x3 is the source stride in bytes.
		name: "neonFilter8V16",
		args: []string{"dst", "ds", "src", "ss", "w", "h", "f"},
		body: `
	ld1 {v15.8h}, [x6]
	movz w12, #0xfff8, lsl #16
	dup v30.4s, w12
1:
	mov x8, x2
	mov x9, x0
	mov x10, x4
2:
	mov x11, x8
	ld1 {v0.8h}, [x11], x3
	ld1 {v1.8h}, [x11], x3
	ld1 {v2.8h}, [x11], x3
	ld1 {v3.8h}, [x11], x3
	ld1 {v4.8h}, [x11], x3
	ld1 {v5.8h}, [x11], x3
	ld1 {v6.8h}, [x11], x3
	ld1 {v7.8h}, [x11], x3
	mov v16.16b, v30.16b
	mov v17.16b, v30.16b
	smlal v16.4s, v0.4h, v15.h[0]
	smlal2 v17.4s, v0.8h, v15.h[0]
	smlal v16.4s, v1.4h, v15.h[1]
	smlal2 v17.4s, v1.8h, v15.h[1]
	smlal v16.4s, v2.4h, v15.h[2]
	smlal2 v17.4s, v2.8h, v15.h[2]
	smlal v16.4s, v3.4h, v15.h[3]
	smlal2 v17.4s, v3.8h, v15.h[3]
	smlal v16.4s, v4.4h, v15.h[4]
	smlal2 v17.4s, v4.8h, v15.h[4]
	smlal v16.4s, v5.4h, v15.h[5]
	smlal2 v17.4s, v5.8h, v15.h[5]
	smlal v16.4s, v6.4h, v15.h[6]
	smlal2 v17.4s, v6.8h, v15.h[6]
	smlal v16.4s, v7.4h, v15.h[7]
	smlal2 v17.4s, v7.8h, v15.h[7]
	shrn v18.4h, v16.4s, #6
	shrn2 v18.8h, v17.4s, #6
	cmp x10, #8
	b.lt 3f
	st1 {v18.8h}, [x9], #16
	add x8, x8, #16
	subs x10, x10, #8
	b.ne 2b
	b 4f
3:
	st1 {v18.4h}, [x9]
4:
	add x2, x2, x3
	add x0, x0, x1
	subs x5, x5, #1
	b.ne 1b
`,
	},
	{
		// 4-tap second stage over int16 first-stage sums.
		name: "neonFilter4V16",
		args: []string{"dst", "ds", "src", "ss", "w", "h", "f"},
		body: `
	ld1 {v15.4h}, [x6]
	movz w12, #0xfff8, lsl #16
	dup v30.4s, w12
1:
	mov x8, x2
	mov x9, x0
	mov x10, x4
2:
	mov x11, x8
	ld1 {v0.8h}, [x11], x3
	ld1 {v1.8h}, [x11], x3
	ld1 {v2.8h}, [x11], x3
	ld1 {v3.8h}, [x11], x3
	mov v16.16b, v30.16b
	mov v17.16b, v30.16b
	smlal v16.4s, v0.4h, v15.h[0]
	smlal2 v17.4s, v0.8h, v15.h[0]
	smlal v16.4s, v1.4h, v15.h[1]
	smlal2 v17.4s, v1.8h, v15.h[1]
	smlal v16.4s, v2.4h, v15.h[2]
	smlal2 v17.4s, v2.8h, v15.h[2]
	smlal v16.4s, v3.4h, v15.h[3]
	smlal2 v17.4s, v3.8h, v15.h[3]
	shrn v18.4h, v16.4s, #6
	shrn2 v18.8h, v17.4s, #6
	cmp x10, #8
	b.lt 3f
	st1 {v18.8h}, [x9], #16
	add x8, x8, #16
	subs x10, x10, #8
	b.ne 2b
	b 4f
3:
	st1 {v18.4h}, [x9]
4:
	add x2, x2, x3
	add x0, x0, x1
	subs x5, x5, #1
	b.ne 1b
`,
	},
	{
		// Uni-prediction output: clip((p + 8192 + 32) >> 6), computed as
		// sat(((p >> 1) + 4112) >> 5) to stay in 16 bits. x0 dst, x1 stride, x2 src, x3 w, x4 h.
		name: "neonPutUni",
		args: []string{"dst", "ds", "src", "w", "h"},
		body: `
	mov w12, #4112
	dup v30.8h, w12
1:
	mov x9, x0
	mov x10, x3
2:
	cmp x10, #8
	b.lt 3f
	ld1 {v0.8h}, [x2], #16
	sshr v0.8h, v0.8h, #1
	add v0.8h, v0.8h, v30.8h
	sqshrun v0.8b, v0.8h, #5
	st1 {v0.8b}, [x9], #8
	subs x10, x10, #8
	b.ne 2b
	b 4f
3:
	ld1 {v0.4h}, [x2], #8
	sshr v0.4h, v0.4h, #1
	add v0.4h, v0.4h, v30.4h
	sqshrun v0.8b, v0.8h, #5
	st1 {v0.s}[0], [x9]
4:
	add x0, x0, x1
	subs x4, x4, #1
	b.ne 1b
`,
	},
	{
		// Bi-prediction output: clip((a + b + 16384 + 64) >> 7) via a halving add.
		name: "neonPutBi",
		args: []string{"dst", "ds", "a", "b", "w", "h"},
		body: `
	mov w12, #4112
	dup v30.8h, w12
1:
	mov x9, x0
	mov x10, x4
2:
	cmp x10, #8
	b.lt 3f
	ld1 {v0.8h}, [x2], #16
	ld1 {v1.8h}, [x3], #16
	shadd v0.8h, v0.8h, v1.8h
	sshr v0.8h, v0.8h, #1
	add v0.8h, v0.8h, v30.8h
	sqshrun v0.8b, v0.8h, #5
	st1 {v0.8b}, [x9], #8
	subs x10, x10, #8
	b.ne 2b
	b 4f
3:
	ld1 {v0.4h}, [x2], #8
	ld1 {v1.4h}, [x3], #8
	shadd v0.4h, v0.4h, v1.4h
	sshr v0.4h, v0.4h, #1
	add v0.4h, v0.4h, v30.4h
	sqshrun v0.8b, v0.8h, #5
	st1 {v0.s}[0], [x9]
4:
	add x0, x0, x1
	subs x5, x5, #1
	b.ne 1b
`,
	},
	{
		// SAO edge offset over a w x h rectangle (w >= 16): dst = sat(src +
		// tbl[2 + sign(src-a) + sign(src-b)]) where a and b are the samples
		// at byte offsets aoff / boff from src. The last vector of a row
		// overlaps the previous one; src is the unmodified copy, so the
		// overlap recomputes identical values. x0 dst, x1 src, x2 stride,
		// x3 aoff, x4 boff, x5 w, x6 h, x7 tbl (16 int8).
		name: "neonSAOEdge",
		args: []string{"dst", "src", "stride", "aoff", "boff", "w", "h", "tbl"},
		body: `
	ld1 {v31.16b}, [x7]
	movi v30.16b, #2
1:
	mov x8, x1
	mov x9, x0
	mov x10, x5
2:
	cmp x10, #16
	b.ge 3f
	sub x11, x10, #16
	add x8, x8, x11
	add x9, x9, x11
	mov x10, #16
3:
	ld1 {v0.16b}, [x8]
	add x12, x8, x3
	ld1 {v1.16b}, [x12]
	add x12, x8, x4
	ld1 {v2.16b}, [x12]
	cmhi v3.16b, v0.16b, v1.16b
	cmhi v4.16b, v1.16b, v0.16b
	sub v5.16b, v4.16b, v3.16b
	cmhi v3.16b, v0.16b, v2.16b
	cmhi v4.16b, v2.16b, v0.16b
	sub v6.16b, v4.16b, v3.16b
	add v5.16b, v5.16b, v6.16b
	add v5.16b, v5.16b, v30.16b
	tbl v5.16b, {v31.16b}, v5.16b
	usqadd v0.16b, v5.16b
	st1 {v0.16b}, [x9]
	add x8, x8, #16
	add x9, x9, #16
	subs x10, x10, #16
	b.ne 2b
	add x1, x1, x2
	add x0, x0, x2
	subs x6, x6, #1
	b.ne 1b
`,
	},
	{
		// Explicit weighted uni-prediction: dst = clip(((p*wt + rnd) >>
		// shift) + off). Saturating narrowing only acts far outside
		// [0, 255], so the final clip is exact. x0 dst, x1 stride, x2 src,
		// x3 w, x4 h, x5 wt, x6 rnd, x7 shift, x8 off.
		name: "neonPutWUni",
		args: []string{"dst", "ds", "src", "w", "h", "wt", "rnd", "shift", "off"},
		body: `
	dup v28.8h, w5
	dup v29.4s, w6
	neg x7, x7
	dup v30.4s, w7
	dup v31.8h, w8
1:
	mov x9, x0
	mov x10, x3
2:
	cmp x10, #8
	b.lt 3f
	ld1 {v0.8h}, [x2], #16
	smull v1.4s, v0.4h, v28.4h
	smull2 v2.4s, v0.8h, v28.8h
	add v1.4s, v1.4s, v29.4s
	add v2.4s, v2.4s, v29.4s
	sshl v1.4s, v1.4s, v30.4s
	sshl v2.4s, v2.4s, v30.4s
	sqxtn v3.4h, v1.4s
	sqxtn2 v3.8h, v2.4s
	sqadd v3.8h, v3.8h, v31.8h
	sqxtun v3.8b, v3.8h
	st1 {v3.8b}, [x9], #8
	subs x10, x10, #8
	b.ne 2b
	b 4f
3:
	ld1 {v0.4h}, [x2], #8
	smull v1.4s, v0.4h, v28.4h
	add v1.4s, v1.4s, v29.4s
	sshl v1.4s, v1.4s, v30.4s
	sqxtn v3.4h, v1.4s
	sqadd v3.4h, v3.4h, v31.4h
	sqxtun v3.8b, v3.8h
	st1 {v3.s}[0], [x9]
4:
	add x0, x0, x1
	subs x4, x4, #1
	b.ne 1b
`,
	},
	{
		// Explicit weighted bi-prediction: dst = clip((a*w0 + b*w1 + add)
		// >> shift). x0 dst, x1 stride, x2 a, x3 b, x4 w, x5 h, x6 w0,
		// x7 w1, x8 add, x9 shift.
		name: "neonPutWBi",
		args: []string{"dst", "ds", "a", "b", "w", "h", "w0", "w1", "add", "shift"},
		body: `
	dup v26.8h, w6
	dup v27.8h, w7
	dup v29.4s, w8
	neg x9, x9
	dup v30.4s, w9
1:
	mov x10, x0
	mov x11, x4
2:
	cmp x11, #8
	b.lt 3f
	ld1 {v0.8h}, [x2], #16
	ld1 {v1.8h}, [x3], #16
	smull v2.4s, v0.4h, v26.4h
	smull2 v3.4s, v0.8h, v26.8h
	smlal v2.4s, v1.4h, v27.4h
	smlal2 v3.4s, v1.8h, v27.8h
	add v2.4s, v2.4s, v29.4s
	add v3.4s, v3.4s, v29.4s
	sshl v2.4s, v2.4s, v30.4s
	sshl v3.4s, v3.4s, v30.4s
	sqxtn v4.4h, v2.4s
	sqxtn2 v4.8h, v3.4s
	sqxtun v4.8b, v4.8h
	st1 {v4.8b}, [x10], #8
	subs x11, x11, #8
	b.ne 2b
	b 4f
3:
	ld1 {v0.4h}, [x2], #8
	ld1 {v1.4h}, [x3], #8
	smull v2.4s, v0.4h, v26.4h
	smlal v2.4s, v1.4h, v27.4h
	add v2.4s, v2.4s, v29.4s
	sshl v2.4s, v2.4s, v30.4s
	sqxtn v4.4h, v2.4s
	sqxtun v4.8b, v4.8h
	st1 {v4.s}[0], [x10]
4:
	add x0, x0, x1
	subs x5, x5, #1
	b.ne 1b
`,
	},
	{
		// Integer-position prediction: dst = (src << 6) - 8192 as int16.
		// x0 dst (stride w), x1 src, x2 src stride, x3 w, x4 h.
		name: "neonCopyPred",
		args: []string{"dst", "src", "ss", "w", "h"},
		body: `
	mov w12, #8192
	dup v31.8h, w12
1:
	mov x9, x1
	mov x10, x3
2:
	cmp x10, #8
	b.lt 3f
	ld1 {v0.8b}, [x9], #8
	ushll v0.8h, v0.8b, #6
	sub v0.8h, v0.8h, v31.8h
	st1 {v0.8h}, [x0], #16
	subs x10, x10, #8
	b.ne 2b
	b 4f
3:
	ld1 {v0.s}[0], [x9]
	ushll v0.8h, v0.8b, #6
	sub v0.8h, v0.8h, v31.8h
	st1 {v0.4h}, [x0], #8
4:
	add x1, x1, x2
	subs x4, x4, #1
	b.ne 1b
`,
	},
	{
		// Sum of absolute differences of a w x h block, w = 16 or 8,
		// h <= 16. x0 a, x1 a stride, x2 b, x3 b stride, x4 w, x5 h,
		// x6 *int result.
		name: "neonSAD",
		args: []string{"a", "as", "b", "bs", "w", "h", "out"},
		body: `
	movi v16.8h, #0
	movi v17.8h, #0
	cmp x4, #16
	b.ne 8f
1:
	ld1 {v0.16b}, [x0], x1
	ld1 {v1.16b}, [x2], x3
	uabal v16.8h, v0.8b, v1.8b
	uabal2 v17.8h, v0.16b, v1.16b
	subs x5, x5, #1
	b.ne 1b
	b 9f
8:
	ld1 {v0.8b}, [x0], x1
	ld1 {v1.8b}, [x2], x3
	uabal v16.8h, v0.8b, v1.8b
	subs x5, x5, #1
	b.ne 8b
9:
	add v16.8h, v16.8h, v17.8h
	uaddlv s0, v16.8h
	str s0, [x6]
	str wzr, [x6, #4]
	ret
`,
	},
	{
		// Raw SATD (sum of |4x4 Hadamard| of a - b over every 4x4) of a
		// w x h block, w = 4, 8 or 16 (16 = two 8-wide strips), h a
		// multiple of 4. Same arguments as neonSAD.
		name: "neonSATD",
		args: []string{"a", "as", "b", "bs", "w", "h", "out"},
		body: `
	movi v20.4s, #0
	mov x9, x0
	mov x10, x2
	mov x11, x5
	cmp x4, #4
	b.eq 40f
2:
	mov x0, x9
	mov x2, x10
	mov x5, x11
1:
	ld1 {v0.8b}, [x0], x1
	ld1 {v4.8b}, [x2], x3
	ld1 {v1.8b}, [x0], x1
	ld1 {v5.8b}, [x2], x3
	ld1 {v2.8b}, [x0], x1
	ld1 {v6.8b}, [x2], x3
	ld1 {v3.8b}, [x0], x1
	ld1 {v7.8b}, [x2], x3
	bl 50f
	subs x5, x5, #4
	b.ne 1b
	subs x4, x4, #8
	b.eq 9f
	add x9, x9, #8
	add x10, x10, #8
	b 2b
40:
	ldr s0, [x0]
	add x0, x0, x1
	ldr s4, [x2]
	add x2, x2, x3
	ldr s1, [x0]
	add x0, x0, x1
	ldr s5, [x2]
	add x2, x2, x3
	ldr s2, [x0]
	add x0, x0, x1
	ldr s6, [x2]
	add x2, x2, x3
	ldr s3, [x0]
	add x0, x0, x1
	ldr s7, [x2]
	add x2, x2, x3
	bl 50f
	subs x5, x5, #4
	b.ne 40b
9:
	addv s0, v20.4s
	str s0, [x6]
	str wzr, [x6, #4]
	b 99f
50:
	usubl v0.8h, v0.8b, v4.8b
	usubl v1.8h, v1.8b, v5.8b
	usubl v2.8h, v2.8b, v6.8b
	usubl v3.8h, v3.8b, v7.8b
	add v4.8h, v0.8h, v1.8h
	sub v5.8h, v0.8h, v1.8h
	add v6.8h, v2.8h, v3.8h
	sub v7.8h, v2.8h, v3.8h
	add v0.8h, v4.8h, v6.8h
	sub v1.8h, v4.8h, v6.8h
	add v2.8h, v5.8h, v7.8h
	sub v3.8h, v5.8h, v7.8h
	rev32 v4.8h, v0.8h
	rev32 v5.8h, v1.8h
	rev32 v6.8h, v2.8h
	rev32 v7.8h, v3.8h
	add v16.8h, v0.8h, v4.8h
	sub v0.8h, v0.8h, v4.8h
	add v17.8h, v1.8h, v5.8h
	sub v1.8h, v1.8h, v5.8h
	add v18.8h, v2.8h, v6.8h
	sub v2.8h, v2.8h, v6.8h
	add v19.8h, v3.8h, v7.8h
	sub v3.8h, v3.8h, v7.8h
	trn1 v0.8h, v16.8h, v0.8h
	trn1 v1.8h, v17.8h, v1.8h
	trn1 v2.8h, v18.8h, v2.8h
	trn1 v3.8h, v19.8h, v3.8h
	rev64 v4.4s, v0.4s
	rev64 v5.4s, v1.4s
	rev64 v6.4s, v2.4s
	rev64 v7.4s, v3.4s
	add v16.8h, v0.8h, v4.8h
	sub v0.8h, v0.8h, v4.8h
	add v17.8h, v1.8h, v5.8h
	sub v1.8h, v1.8h, v5.8h
	add v18.8h, v2.8h, v6.8h
	sub v2.8h, v2.8h, v6.8h
	add v19.8h, v3.8h, v7.8h
	sub v3.8h, v3.8h, v7.8h
	trn1 v0.4s, v16.4s, v0.4s
	trn1 v1.4s, v17.4s, v1.4s
	trn1 v2.4s, v18.4s, v2.4s
	trn1 v3.4s, v19.4s, v3.4s
	abs v0.8h, v0.8h
	abs v1.8h, v1.8h
	abs v2.8h, v2.8h
	abs v3.8h, v3.8h
	add v0.8h, v0.8h, v1.8h
	add v2.8h, v2.8h, v3.8h
	uadalp v20.4s, v0.8h
	uadalp v20.4s, v2.8h
	ret
99:
`,
	},
	{
		// Column transform of an n x n int32 block (n = 4, 8, 16 or 32):
		// dst[j][k] = (sum_y m[j][y] * src[y][k] + (1 << (shift-1))) >>
		// shift, vectorised over 16 (n >= 16) or 4 columns. x0 dst, x1 src,
		// x2 m (n x n int32), x3 n, x4 shift.
		name: "neonColTransform",
		args: []string{"dst", "src", "m", "n", "shift"},
		body: `
	lsl x9, x3, #2
	neg x10, x4
	dup v31.4s, w10
	mov w11, #1
	sub x12, x4, #1
	lsl w11, w11, w12
	dup v30.4s, w11
	mov x5, #0
1:
	mul x13, x5, x9
	add x14, x2, x13
	add x15, x0, x13
	mov x6, #0
	cmp x3, #16
	b.lt 5f
2:
	mov v16.16b, v30.16b
	mov v17.16b, v30.16b
	mov v18.16b, v30.16b
	mov v19.16b, v30.16b
	add x7, x1, x6
	mov x8, #0
3:
	ldr w12, [x14, x8, lsl #2]
	dup v1.4s, w12
	ld1 {v2.4s, v3.4s, v4.4s, v5.4s}, [x7], x9
	mla v16.4s, v2.4s, v1.4s
	mla v17.4s, v3.4s, v1.4s
	mla v18.4s, v4.4s, v1.4s
	mla v19.4s, v5.4s, v1.4s
	add x8, x8, #1
	cmp x8, x3
	b.lt 3b
	sshl v16.4s, v16.4s, v31.4s
	sshl v17.4s, v17.4s, v31.4s
	sshl v18.4s, v18.4s, v31.4s
	sshl v19.4s, v19.4s, v31.4s
	st1 {v16.4s, v17.4s, v18.4s, v19.4s}, [x15], #64
	add x6, x6, #64
	cmp x6, x9
	b.lt 2b
	b 8f
5:
	mov v16.16b, v30.16b
	add x7, x1, x6
	mov x8, #0
6:
	ldr w12, [x14, x8, lsl #2]
	dup v1.4s, w12
	ld1 {v2.4s}, [x7], x9
	mla v16.4s, v2.4s, v1.4s
	add x8, x8, #1
	cmp x8, x3
	b.lt 6b
	sshl v16.4s, v16.4s, v31.4s
	st1 {v16.4s}, [x15], #16
	add x6, x6, #16
	cmp x6, x9
	b.lt 5b
8:
	add x5, x5, #1
	cmp x5, x3
	b.lt 1b
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
		// A kernel may call a local subroutine (bl), which clobbers the
		// link register: it is saved in R16 here and restored by the
		// epilogue, which such kernels reach by branching to a final
		// "99:" label instead of returning directly.
		fmt.Fprintf(&out, "TEXT ·%s(SB), NOSPLIT|NOFRAME, $0-%d\n", k.name, 8*len(k.args))
		for i, a := range k.args {
			fmt.Fprintf(&out, "\tMOVD %s+%d(FP), R%d\n", a, 8*i, i)
		}
		out.WriteString("\tMOVD R30, R16\n")
		for i, w := range words {
			fmt.Fprintf(&out, "\tWORD $0x%08x // %s\n", w, lines[i])
		}
		out.WriteString("\tMOVD R16, R30\n\tRET\n")
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
