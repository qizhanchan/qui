package av1

import (
	"fmt"
	"os"
	"testing"
)

func blkStr(t *tile) string {
	b := &t.b
	return fmt.Sprintf("(%d,%d) sz%d inter=%v mode=%d ref=%v mv=%v skip=%v sm=%v interp=%v tx=%d", b.miRow, b.miCol, b.size, b.isInter, b.yMode, b.refFrame, b.mv, b.skip, b.skipMode, b.interpFilter, b.txSize)
}

// TestEncodeBlockDiff (opt-in: AV1_ENC_BLOCKDIFF=1) encodes a clip and
// compares every committed block's decisions with what the decoder parses
// from the stream, reporting the first difference: the place a writer
// and its parser disagree.
func TestEncodeBlockDiff(t *testing.T) {
	if os.Getenv("AV1_ENC_BLOCKDIFF") == "" {
		t.Skip("set AV1_ENC_BLOCKDIFF")
	}
	src := testSource(t, "ra_p4", 12)
	e, _ := NewEncoder(EncoderConfig{Width: 320, Height: 180, QIndex: 120, MiniGOP: 4, Refs: 2, Preset: 1})
	var encBlocks [][]string
	cur := -1
	encHook = func(tl *tile) {
		if cur < 0 {
			return
		}
		encBlocks[cur] = append(encBlocks[cur], blkStr(tl))
	}
	encFrameHook = func() { encBlocks = append(encBlocks, nil); cur = len(encBlocks) - 1 }
	pkts := encodeAll(t, e, src)
	encHook, encFrameHook = nil, nil
	var decBlocks [][]string
	debugBlock = func(tl *tile) { decBlocks[len(decBlocks)-1] = append(decBlocks[len(decBlocks)-1], blkStr(tl)) }
	debugFrame = func() { decBlocks = append(decBlocks, nil) }
	d := New()
	for _, p := range pkts {
		d.Decode(p.Data)
		for d.Next() != nil {
		}
	}
	debugBlock, debugFrame = nil, nil
	for f := range min(len(encBlocks), len(decBlocks)) {
		for i := range min(len(encBlocks[f]), len(decBlocks[f])) {
			if encBlocks[f][i] != decBlocks[f][i] {
				t.Logf("coded frame %d block %d:\n enc %s\n dec %s", f, i, encBlocks[f][i], decBlocks[f][i])
				if i > 0 {
					t.Logf(" prev %s", encBlocks[f][i-1])
				}
				t.Fail()
				return
			}
		}
	}
	t.Log("no block differs", len(encBlocks), len(decBlocks))
}
