//go:build ignore

// neon_gen writes neon_arm64.s for the encoder, like the decoder's
// h264/neon_gen.go: kernels in standard arm64 syntax, assembled with the
// system clang and emitted as annotated WORDs, arguments loaded from the
// Go (ABI0) frame into R0, R1, ... by a generated prologue.
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
	args []string
	body string
}

var kernels = []kernel{
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
		// Forward 4x4 transforms and quantisation of the four 4x4 blocks of
		// an 8x8 residual (src - pred). x0 src, x1 src stride, x2 pred,
		// x3 pred stride, x4 *[4][8]uint16 multipliers (row r: mf(r, 0..3)
		// twice), x5 bias, x6 qbits, x7 *[64]int32 levels (block k raster
		// at 16k; k = 0 top-left, 1 top-right, 2 bottom-left, 3
		// bottom-right), x8 *int non-zero block mask.
		name: "neonDCTQuant8",
		args: []string{"src", "ss", "pred", "ps", "mf", "bias", "qbits", "out", "mask"},
		body: `
	ld1 {v24.8h, v25.8h, v26.8h, v27.8h}, [x4]
	dup v28.4s, w5
	neg x6, x6
	dup v29.4s, w6
	mov x9, #0
	mov x10, #2
1:
	ld1 {v0.8b}, [x0], x1
	ld1 {v4.8b}, [x2], x3
	ld1 {v1.8b}, [x0], x1
	ld1 {v5.8b}, [x2], x3
	ld1 {v2.8b}, [x0], x1
	ld1 {v6.8b}, [x2], x3
	ld1 {v3.8b}, [x0], x1
	ld1 {v7.8b}, [x2], x3
	usubl v0.8h, v0.8b, v4.8b
	usubl v1.8h, v1.8b, v5.8b
	usubl v2.8h, v2.8b, v6.8b
	usubl v3.8h, v3.8b, v7.8b
	add v4.8h, v0.8h, v3.8h
	sub v5.8h, v0.8h, v3.8h
	add v6.8h, v1.8h, v2.8h
	sub v7.8h, v1.8h, v2.8h
	add v0.8h, v4.8h, v6.8h
	sub v2.8h, v4.8h, v6.8h
	shl v16.8h, v5.8h, #1
	add v1.8h, v16.8h, v7.8h
	shl v17.8h, v7.8h, #1
	sub v3.8h, v5.8h, v17.8h
	trn1 v4.8h, v0.8h, v1.8h
	trn2 v5.8h, v0.8h, v1.8h
	trn1 v6.8h, v2.8h, v3.8h
	trn2 v7.8h, v2.8h, v3.8h
	trn1 v0.4s, v4.4s, v6.4s
	trn2 v2.4s, v4.4s, v6.4s
	trn1 v1.4s, v5.4s, v7.4s
	trn2 v3.4s, v5.4s, v7.4s
	add v4.8h, v0.8h, v3.8h
	sub v5.8h, v0.8h, v3.8h
	add v6.8h, v1.8h, v2.8h
	sub v7.8h, v1.8h, v2.8h
	add v0.8h, v4.8h, v6.8h
	sub v2.8h, v4.8h, v6.8h
	shl v16.8h, v5.8h, #1
	add v1.8h, v16.8h, v7.8h
	shl v17.8h, v7.8h, #1
	sub v3.8h, v5.8h, v17.8h
	trn1 v4.8h, v0.8h, v1.8h
	trn2 v5.8h, v0.8h, v1.8h
	trn1 v6.8h, v2.8h, v3.8h
	trn2 v7.8h, v2.8h, v3.8h
	trn1 v0.4s, v4.4s, v6.4s
	trn2 v2.4s, v4.4s, v6.4s
	trn1 v1.4s, v5.4s, v7.4s
	trn2 v3.4s, v5.4s, v7.4s
	movi v30.16b, #0
	movi v31.16b, #0
	add x11, x7, #64
	mov v8.16b, v0.16b
	mov v9.16b, v24.16b
	bl 50f
	mov v8.16b, v1.16b
	mov v9.16b, v25.16b
	bl 50f
	mov v8.16b, v2.16b
	mov v9.16b, v26.16b
	bl 50f
	mov v8.16b, v3.16b
	mov v9.16b, v27.16b
	bl 50f
	umaxv s16, v30.4s
	fmov w12, s16
	cmp w12, #0
	cset w12, ne
	lsl w12, w12, w9
	umaxv s17, v31.4s
	fmov w13, s17
	cmp w13, #0
	cset w13, ne
	add w14, w9, #1
	lsl w13, w13, w14
	orr w12, w12, w13
	ldr x15, [x8]
	orr x15, x15, x12
	str x15, [x8]
	add x7, x7, #64
	add x9, x9, #2
	subs x10, x10, #1
	b.ne 1b
	b 99f
50:
	abs v16.8h, v8.8h
	umull v18.4s, v16.4h, v9.4h
	umull2 v19.4s, v16.8h, v9.8h
	add v18.4s, v18.4s, v28.4s
	add v19.4s, v19.4s, v28.4s
	ushl v18.4s, v18.4s, v29.4s
	ushl v19.4s, v19.4s, v29.4s
	cmlt v20.8h, v8.8h, #0
	sxtl v21.4s, v20.4h
	sxtl2 v22.4s, v20.8h
	eor v18.16b, v18.16b, v21.16b
	sub v18.4s, v18.4s, v21.4s
	eor v19.16b, v19.16b, v22.16b
	sub v19.4s, v19.4s, v22.4s
	orr v30.16b, v30.16b, v18.16b
	orr v31.16b, v31.16b, v19.16b
	st1 {v18.4s}, [x7], #16
	st1 {v19.4s}, [x11], #16
	ret
99:
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
	data, err := f.Section("__text").Data()
	if err != nil {
		log.Fatal(err)
	}
	words := make([]uint32, len(data)/4)
	for i := range words {
		words[i] = binary.LittleEndian.Uint32(data[4*i:])
	}
	return words
}
