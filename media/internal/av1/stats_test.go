package av1

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestToolStats (opt-in: AV1_STATS=dir) decodes every *.ivf in a
// directory and counts how often each coding tool occurs, to show what
// the test streams cover.
func TestToolStats(t *testing.T) {
	dir := os.Getenv("AV1_STATS")
	if dir == "" {
		t.Skip("set AV1_STATS")
	}
	cnt := map[string]int{}
	inc := func(f string, a ...any) { cnt[fmt.Sprintf(f, a...)]++ }
	debugBlock = func(tl *tile) {
		b := &tl.b
		h := tl.h
		switch {
		case b.useIntraBC:
			inc("block intrabc")
		case b.isInter:
			inc("block inter motion=%d", b.motionMode)
			inc("block inter compound=%d", b.compoundType)
			if b.interIntra {
				inc("block interintra mode=%d wedge=%v", b.interIntraMode, b.wedgeInterIntra)
			}
			if b.skipMode {
				inc("block skipMode")
			}
			if b.refFrame[1] > intraFrame {
				inc("block compound refs %d-%d", b.refFrame[0], b.refFrame[1])
			}
			if b.interpFilter[0] != b.interpFilter[1] {
				inc("block dual filter")
			}
			inc("block interp=%d", b.interpFilter[0])
			if b.yMode == globalMV || b.yMode == globalGlobalMV {
				inc("block globalmv gmtype=%d", h.gmType[b.refFrame[0]])
			}
		default:
			inc("block intra")
			if b.paletteSizeY > 0 {
				inc("block palette y")
			}
			if b.paletteSizeUV > 0 {
				inc("block palette uv")
			}
			if b.useFilterIntra {
				inc("block filter intra")
			}
			if b.uvMode == uvCflPred {
				inc("block cfl")
			}
			if b.angleDeltaY != 0 {
				inc("block angle delta")
			}
		}
		if b.segID != 0 {
			inc("block segid!=0")
		}
		if b.lossless {
			inc("block lossless")
		}
	}
	debugTx = func(tl *tile, plane, x, y, tx, typ, eob int) {
		inc("tx type %d", typ)
		inc("tx size %d", tx)
	}
	defer func() { debugBlock, debugTx = nil, nil }()
	files, _ := filepath.Glob(filepath.Join(dir, "*.ivf"))
	for _, f := range files {
		data, _ := os.ReadFile(f)
		tus, _, _, _ := ReadIVF(data)
		d := New()
		for _, tu := range tus {
			if err := d.DecodeTemporalUnit(tu); err != nil {
				t.Errorf("%s: %v", f, err)
				break
			}
			if h := d.hdr; h != nil && !h.ShowExistingFrame {
				if h.seg.enabled {
					inc("hdr segmentation (temporal=%v)", h.seg.temporalUpdate)
				}
				if h.deltaQPresent {
					inc("hdr delta q")
				}
				if h.deltaLFPresent {
					inc("hdr delta lf multi=%v", h.deltaLFMulti)
				}
				for r := lastFrame; r <= altrefFrame; r++ {
					if h.gmType[r] != gmIdentity {
						inc("hdr gm type %d", h.gmType[r])
					}
				}
				if h.usingQMatrix {
					inc("hdr qmatrix")
				}
				if h.superresDenom != 8 {
					inc("hdr superres")
				}
				for p := 0; p < 3; p++ {
					if h.lrType[p] != restoreNone {
						inc("hdr lr plane%d type %d", p, h.lrType[p])
					}
				}
				if h.grain.apply {
					inc("hdr film grain")
				}
				if h.useRefFrameMVs {
					inc("hdr ref frame mvs")
				}
				if h.reducedTxSet {
					inc("hdr reduced tx set")
				}
				if h.allowWarpedMotion {
					inc("hdr warped motion")
				}
				inc("hdr frame type %d", h.FrameType)
			}
			d.out = d.out[:0]
		}
	}
	keys := make([]string, 0, len(cnt))
	for k := range cnt {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Logf("%-40s %d", k, cnt[k])
	}
}
