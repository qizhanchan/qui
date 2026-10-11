package h264

import (
	"fmt"
	"os"
	"strconv"
	"testing"
)

// TestDebugMB dumps macroblocks of picture $AVC_POC in $AVC_FILE that differ
// from the reference (pre-deblocking comparison; use no-deblock streams).
func TestDebugMB(t *testing.T) {
	file := os.Getenv("AVC_FILE")
	if file == "" {
		t.Skip()
	}
	poc, _ := strconv.Atoi(os.Getenv("AVC_POC"))
	debugNoDeblock = os.Getenv("AVC_NODBK") != ""
	defer func() { debugNoDeblock = false }()
	data, _ := os.ReadFile(file + ".264")
	refName := file + ".yuv"
	if debugNoDeblock {
		refName = file + ".nodbk.yuv"
	}
	ref, _ := os.ReadFile(refName)
	frames, _ := DecodeAnnexB(data)
	w, h := frames[0].Width, frames[0].Height
	fs := w * h * 3 / 2
	// AVC_POC is the display index; valid for streams without reordering.
	idx := poc
	_ = frames
	r := ref[idx*fs:]
	cnt := -1
	debugPicture = func(p *picture, pc *picCtx) {
		cnt++
		if cnt != idx {
			return
		}
		for a := range pc.mbs {
			mx, my := a%pc.mbW, a/pc.mbW
			n := 0
			for y := 0; y < 16; y++ {
				for x := 0; x < 16; x++ {
					if p.y[(my*16+y)*p.stride+mx*16+x] != r[(my*16+y)*w+mx*16+x] {
						n++
					}
				}
			}
			if n == 0 {
				continue
			}
			mb := &pc.mbs[a]
			fmt.Printf("mb %d (%d,%d) typ=%#x cbp=%#x qp=%d t8=%v diff=%d\n", a, mx, my, mb.typ, mb.cbp, mb.qp, mb.t8x8, n)
			for l := 0; l < 2; l++ {
				fmt.Printf("  L%d:", l)
				for by := 0; by < 4; by++ {
					for bx := 0; bx < 4; bx++ {
						i := (my*4+by)*pc.mbW*4 + mx*4 + bx
						fmt.Printf(" %d:(%d,%d)", p.ref[l][i], p.mv[l][2*i], p.mv[l][2*i+1])
					}
					fmt.Printf(" |")
				}
				fmt.Println()
			}
			for y := 0; y < 16; y++ {
				for x := 0; x < 16; x++ {
					fmt.Printf("%4d", int(p.y[(my*16+y)*p.stride+mx*16+x])-int(r[(my*16+y)*w+mx*16+x]))
				}
				fmt.Println()
			}
		}
	}
	DecodeAnnexB(data)
	debugPicture = nil
}

func TestDebugErr(t *testing.T) {
	file := os.Getenv("AVC_FILE")
	if file == "" {
		t.Skip()
	}
	data, _ := os.ReadFile(file + ".264")
	n := 0
	debugErr = func(s *sliceCtx, err error) {
		if n < 5 {
			fmt.Printf("poc %d slice type %d mb %d (%d,%d): %v typ=%#x\n", s.pic.poc, s.h.sliceType, s.mbAddr, s.mbX, s.mbY, err, s.typ)
		}
		n++
	}
	DecodeAnnexB(data)
	debugErr = nil
}

func TestDumpHeaders(t *testing.T) {
	file := os.Getenv("AVC_FILE")
	if file == "" {
		t.Skip()
	}
	data, _ := os.ReadFile(file + ".264")
	var spsT [32]*SPS
	var ppsT [256]*PPS
	n := 0
	for _, nal := range SplitAnnexB(data) {
		switch nalType(nal[0]) {
		case nalSPS:
			s, _ := ParseSPS(nal)
			spsT[s.ID] = s
			fmt.Printf("SPS %+v\n", *s)
		case nalPPS:
			p, _ := ParsePPS(nal, func(i int) *SPS { return spsT[i] })
			ppsT[p.ID] = p
			fmt.Printf("PPS id=%d cabac=%v nref=%d/%d wp=%v wb=%d t8=%v qp=%d cqp=%d/%d dbk=%v ci=%v\n", p.ID, p.EntropyCodingCABAC, p.NumRefIdxL0Default, p.NumRefIdxL1Default, p.WeightedPred, p.WeightedBipredIDC, p.Transform8x8Mode, p.PicInitQP, p.ChromaQPIndexOffset, p.SecondChromaQPOffset, p.DeblockingFilterCtl, p.ConstrainedIntraPred)
		case nalSlice, nalIDR:
			if n > 12 {
				continue
			}
			n++
			r := newBitReader(unescapeRBSP(nil, nal[1:]))
			h, err := parseSliceHeader(r, nal[0], spsT[:], ppsT[:])
			if err != nil {
				fmt.Println("err", err)
				continue
			}
			fmt.Printf("slice nal=%d ref=%d first=%d type=%d fn=%d poc=%d nref=%v mod=%v mmco=%v adapt=%v lt=%v dbk=%d a=%d b=%d qpd=%d w=%v idc=%d\n", h.nalType, h.nalRefIDC, h.firstMB, h.sliceType, h.frameNum, h.pocLsb, h.numRefIdx, h.refMod, h.mmco, h.adaptiveMarking, h.longTermReference, h.disableDeblock, h.alphaOffset, h.betaOffset, h.qpDelta, h.hasWeights, h.cabacInitIDC)
		default:
			fmt.Printf("nal type %d len %d\n", nalType(nal[0]), len(nal))
		}
	}
}

func TestDumpYUV(t *testing.T) {
	file := os.Getenv("AVC_FILE")
	if file == "" {
		t.Skip()
	}
	data, _ := os.ReadFile(file + ".264")
	debugNoDeblock = os.Getenv("AVC_NODBK") != ""
	defer func() { debugNoDeblock = false }()
	frames, err := DecodeAnnexB(data)
	fmt.Println("err", err, len(frames))
	out, _ := os.Create(file + ".ours.yuv")
	defer out.Close()
	for _, f := range frames {
		out.Write(f.Y.Pix)
		out.Write(f.Cb.Pix)
		out.Write(f.Cr.Pix)
	}
}

func TestResidualCmp(t *testing.T) {
	file := os.Getenv("AVC_FILE")
	if file == "" {
		t.Skip()
	}
	ref, _ := os.ReadFile(file + ".nodbk.yuv")
	ours, _ := os.ReadFile(file + ".ours.yuv")
	w := 320
	fs := w * 240 * 3 / 2
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			fmt.Printf("%4d", int(ref[fs+y*w+x])-int(ref[y*w+x]))
		}
		fmt.Print("   |")
		for x := 0; x < 8; x++ {
			fmt.Printf("%4d", int(ours[fs+y*w+x])-int(ours[y*w+x]))
		}
		fmt.Println()
	}
}
