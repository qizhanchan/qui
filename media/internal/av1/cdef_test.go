package av1

import "testing"

// TestCdefFilter pins cdefFilter to cdefFilterRef, the spec's process
// (7.15.2) as written, over random blocks at frame edges and inside, all
// strengths, dampings and directions, 8- and 10-bit.
func TestCdefFilter(t *testing.T) {
	rng := uint32(7)
	rnd := func(n int) int {
		rng = rng*1664525 + 1013904223
		return int(rng>>8) % n
	}
	const W, H = 40, 24
	for iter := 0; iter < 20000; iter++ {
		bd := 8 + 2*rnd(2)
		src, a, b := &frame{bitDepth: bd, numPlanes: 3}, &frame{}, &frame{}
		for p := 0; p < 3; p++ {
			w, h := W>>min(p, 1), H>>min(p, 1)
			pb := planeBuf{stride: w, w: w, h: h}
			smooth := rnd(2) == 0
			base := rnd(1 << bd)
			for k := 0; k < w*h; k++ {
				v := rnd(1 << bd)
				if smooth {
					v = min(max(base+rnd(64)-32, 0), 1<<bd-1)
				}
				if bd > 8 {
					pb.pix16 = append(pb.pix16, uint16(v))
				} else {
					pb.pix8 = append(pb.pix8, uint8(v))
				}
			}
			src.planes[p] = pb
		}
		clone := func(dst *frame) {
			*dst = *src
			for p := range dst.planes {
				dst.planes[p].pix8 = append([]uint8(nil), src.planes[p].pix8...)
				dst.planes[p].pix16 = append([]uint16(nil), src.planes[p].pix16...)
			}
		}
		clone(a)
		clone(b)
		fs := &frameState{miCols: W / 4, miRows: H / 4}
		cs := uint(bd - 8)
		pri := int32(rnd(16)) << cs
		sec := int32([4]int{0, 1, 2, 4}[rnd(4)]) << cs
		if rnd(4) == 0 {
			pri = 0
		} else if rnd(4) == 0 {
			sec = 0
		}
		damping := 3 + rnd(4) + int(cs) - rnd(2)
		dir := rnd(8)
		plane := rnd(3)
		r, c := 2*rnd(H/8), 2*rnd(W/8)
		cdefFilter(fs, src, a, plane, r, c, pri, sec, damping, dir)
		cdefFilterRef(fs, src, b, plane, r, c, pri, sec, damping, dir)
		pa, pb := a.planes[plane], b.planes[plane]
		for k := range pa.pix8 {
			if pa.pix8[k] != pb.pix8[k] {
				t.Fatalf("iter %d bd %d plane %d (%d,%d) pri %d sec %d damping %d dir %d: sample %d = %d, want %d",
					iter, bd, plane, r, c, pri, sec, damping, dir, k, pa.pix8[k], pb.pix8[k])
			}
		}
		for k := range pa.pix16 {
			if pa.pix16[k] != pb.pix16[k] {
				t.Fatalf("iter %d bd %d plane %d (%d,%d) pri %d sec %d damping %d dir %d: sample %d = %d, want %d",
					iter, bd, plane, r, c, pri, sec, damping, dir, k, pa.pix16[k], pb.pix16[k])
			}
		}
	}
}

// cdefRefNA marks a tap outside the frame (CdefAvailable false).
const cdefRefNA = int32(1) << 24

func cdefFilterRef(f *frameState, src, dst *frame, plane, r, c int, priStr, secStr int32, damping, dir int) {
	if priStr == 0 && secStr == 0 {
		// The sum is zero and the sample is its own clamp.
		return
	}
	ss := min(plane, 1)
	x0, y0 := (c*4)>>ss, (r*4)>>ss
	w, h := 8>>ss, 8>>ss
	sp, dp := &src.planes[plane], &dst.planes[plane]
	maxX, maxY := (f.miCols*4)>>ss, (f.miRows*4)>>ss
	cs := uint(src.bitDepth - 8)
	pt := &cdefPriTaps[(priStr>>cs)&1]
	st := &cdefSecTaps[(priStr>>cs)&1]
	// The block with a two-sample border, stride 12.
	const bs = 12
	var blk [bs * bs]int32
	for i := -2; i < h+2; i++ {
		yy := y0 + i
		row := blk[(i+2)*bs : (i+2)*bs+w+4]
		if yy < 0 || yy >= maxY {
			for j := range row {
				row[j] = cdefRefNA
			}
			continue
		}
		o := yy * sp.stride
		for j := range row {
			xx := x0 + j - 2
			switch {
			case xx < 0 || xx >= maxX:
				row[j] = cdefRefNA
			case sp.pix16 != nil:
				row[j] = int32(sp.pix16[o+xx])
			default:
				row[j] = int32(sp.pix8[o+xx])
			}
		}
	}
	var priOff [2][2]int
	for k := 0; k < 2; k++ {
		d := &cdefDirections[dir][k]
		priOff[k] = [2]int{d[0]*bs + d[1], -(d[0]*bs + d[1])}
	}
	var secDirs = [2]int{(dir + 2) & 7, (dir - 2) & 7}
	var secOffs [2][4]int
	for k := 0; k < 2; k++ {
		n := 0
		for _, d2 := range secDirs {
			d := &cdefDirections[d2][k]
			o := d[0]*bs + d[1]
			secOffs[k][n], secOffs[k][n+1] = o, -o
			n += 2
		}
	}
	var priAdj, secAdj uint
	if priStr != 0 {
		priAdj = uint(max(0, damping-floorLog2(int(priStr))))
	}
	if secStr != 0 {
		secAdj = uint(max(0, damping-floorLog2(int(secStr))))
	}
	for i := 0; i < h; i++ {
		for j := 0; j < w; j++ {
			ci := (i+2)*bs + j + 2
			x := blk[ci]
			sum := int32(0)
			mx, mn := x, x
			for k := 0; k < 2; k++ {
				for _, o := range priOff[k] {
					p := blk[ci+o]
					if p == cdefRefNA {
						continue
					}
					if priStr != 0 {
						sum += pt[k] * constrainAdj(p-x, priStr, priAdj)
					}
					mx, mn = max(p, mx), min(p, mn)
				}
				for _, o := range secOffs[k] {
					p := blk[ci+o]
					if p == cdefRefNA {
						continue
					}
					if secStr != 0 {
						sum += st[k] * constrainAdj(p-x, secStr, secAdj)
					}
					mx, mn = max(p, mx), min(p, mn)
				}
			}
			neg := int32(0)
			if sum < 0 {
				neg = 1
			}
			v := min(max(x+((8+sum-neg)>>4), mn), mx)
			o := (y0+i)*dp.stride + x0 + j
			if dp.pix16 != nil {
				dp.pix16[o] = uint16(v)
			} else {
				dp.pix8[o] = uint8(v)
			}
		}
	}
}
