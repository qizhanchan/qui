package av1

import (
	"fmt"
	"os"
	"testing"
)

// TestDebugBlock (opt-in: AV1_DEBUG="file frame x y") prints the block
// covering luma sample (x, y) of the given decoded-frame index.
func TestDebugBlock(t *testing.T) {
	spec := os.Getenv("AV1_DEBUG")
	if spec == "" {
		t.Skip("set AV1_DEBUG")
	}
	var file string
	var frame, x, y int
	fmt.Sscan(spec, &file, &frame, &x, &y)
	xx, yy := x, y
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	debugBlock = func(tl *tile) {
		b := &tl.b
		if frame >= 0 && n != frame {
			return
		}
		if x>>2 >= b.miCol && x>>2 < b.miCol+b.bw4 && y>>2 >= b.miRow && y>>2 < b.miRow+b.bh4 {
			t.Logf("frame %d (decode #%d, type %d, show %v): block %d at mi (%d,%d) size %d inter=%v mode=%d ref=%v mv=%v interp=%v motion=%d compound=%d ii=%v/%d wedge=%v/%d/%d skip=%v skipMode=%v tx=%d palette=%d filterIntra=%v",
				frame, n, tl.h.FrameType, tl.h.ShowFrame, 0, b.miRow, b.miCol, b.size, b.isInter, b.yMode, b.refFrame, b.mv, b.interpFilter, b.motionMode,
				b.compoundType, b.interIntra, b.interIntraMode, b.wedgeInterIntra, b.wedgeIndex, b.wedgeSign, b.skip, b.skipMode, b.txSize, b.paletteSizeY, b.useFilterIntra)
		}
	}
	debugTx = func(tl *tile, plane, x, y, tx, typ, eob int) {
		if n == frame && plane == 0 && x <= xx && xx < x+txW[tx] && y <= yy && yy < y+txH[tx] {
			t.Logf("  tx plane %d at (%d,%d) size %d type %d eob %d", plane, x, y, tx, typ, eob)
		}
	}
	defer func() { debugBlock, debugTx = nil, nil }()
	tus, _, _, _ := ReadIVF(data)
	d := New()
	for _, tu := range tus {
		before := len(d.out)
		_ = before
		obus, _ := splitOBUs(tu)
		for i := range obus {
			if err := d.decodeOBU(&obus[i]); err != nil {
				t.Fatal(err)
			}
			if obus[i].typ == obuFrame || obus[i].typ == obuTileGroup {
				if !d.seenFrameHeader {
					n++
				}
			}
		}
	}
}
