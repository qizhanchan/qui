package vp9

// Motion vector prediction (libvpx vp9_mvref_common.c find_mv_refs_idx,
// vp9_append_sub8x8_mvs_for_idx).

const mvBorder = 16 << 3

var idxNColumnToSubblock = [4][2]int{{1, 2}, {1, 3}, {3, 2}, {3, 3}}

func clampMV(m *mv, minC, maxC, minR, maxR int) {
	c := min(max(int(m.col), minC), maxC)
	r := min(max(int(m.row), minR), maxR)
	m.col, m.row = int16(c), int16(r)
}

func (t *tileState) clampMVRef(m *mv) {
	clampMV(m, t.toLeft-mvBorder, t.toRight+mvBorder, t.toTop-mvBorder, t.toBottom+mvBorder)
}

func (t *tileState) clampMV2(m *mv) {
	const margin = (160 - 4) << 3
	clampMV(m, t.toLeft-margin, t.toRight+margin, t.toTop-margin, t.toBottom+margin)
}

type mvList struct {
	l [2]mv
	n int
}

// add appends m; it reports true when the list is complete.
func (l *mvList) add(m mv) bool {
	if l.n > 0 {
		if m != l.l[0] {
			l.l[1] = m
			l.n = 2
			return true
		}
		return false
	}
	l.l[0] = m
	l.n = 1
	return false
}

// findMVRefs fills out with the candidate list for refFrame (block −1 =
// the whole block, else a sub8x8 index) and returns the mode context.
func (t *tileState) findMVRefs(refFrame int, out *[2]mv, block int) int {
	d := t.d
	h := d.hdr
	b := t.bi
	search := &mvRefBlocks[b.bsize]
	var list mvList
	ctxCounter := 0
	differentRef := false
	rf := int8(refFrame)
	inside := func(i int) *blockInfo {
		// mv_ref_blocks are {row, col} pairs.
		r := t.miRow + int(search[i][0])
		c := t.miCol + int(search[i][1])
		if r < 0 || c < t.miColStart || r >= d.miRows || c >= t.miColEnd {
			return nil
		}
		return d.mi[r*d.miStride+c]
	}
	done := false
	for i := 0; i < 2 && !done; i++ {
		cand := inside(i)
		if cand == nil {
			continue
		}
		ctxCounter += int(mode2Counter[cand.mode])
		differentRef = true
		for w := 0; w < 2; w++ {
			if cand.ref[w] == rf {
				m := cand.mv[w]
				if block >= 0 && cand.bsize < block8x8 {
					col0 := 0
					if search[i][1] == 0 {
						col0 = 1
					}
					m = cand.bmv[idxNColumnToSubblock[block][col0]][w]
				}
				done = list.add(m)
				break
			}
		}
	}
	for i := 2; i < 8 && !done; i++ {
		cand := inside(i)
		if cand == nil {
			continue
		}
		differentRef = true
		for w := 0; w < 2; w++ {
			if cand.ref[w] == rf {
				done = list.add(cand.mv[w])
				break
			}
		}
	}
	var prev *mvRef
	if d.usePrevMVs {
		prev = &d.prev.mvs[t.miRow*d.miCols+t.miCol]
	}
	if !done && prev != nil {
		if prev.ref[0] == rf {
			done = list.add(prev.mv[0])
		} else if prev.ref[1] == rf {
			done = list.add(prev.mv[1])
		}
	}
	if !done && differentRef {
		for i := 0; i < 8 && !done; i++ {
			cand := inside(i)
			if cand == nil || !cand.isInter {
				continue
			}
			if cand.ref[0] != rf {
				m := cand.mv[0]
				if h.signBias[cand.ref[0]] != h.signBias[refFrame] {
					m.row, m.col = -m.row, -m.col
				}
				if done = list.add(m); done {
					break
				}
			}
			if cand.hasSecond() && cand.ref[1] != rf && cand.mv[1] != cand.mv[0] {
				m := cand.mv[1]
				if h.signBias[cand.ref[1]] != h.signBias[refFrame] {
					m.row, m.col = -m.row, -m.col
				}
				done = list.add(m)
			}
		}
	}
	if !done && prev != nil {
		if prev.ref[0] != rf && prev.ref[0] > intraFrame {
			m := prev.mv[0]
			if h.signBias[prev.ref[0]] != h.signBias[refFrame] {
				m.row, m.col = -m.row, -m.col
			}
			done = list.add(m)
		}
		if !done && prev.ref[1] > intraFrame && prev.ref[1] != rf && prev.mv[1] != prev.mv[0] {
			m := prev.mv[1]
			if h.signBias[prev.ref[1]] != h.signBias[refFrame] {
				m.row, m.col = -m.row, -m.col
			}
			list.add(m)
		}
	}
	*out = list.l
	for i := range out {
		t.clampMVRef(&out[i])
	}
	return int(counterToContext[ctxCounter])
}

// appendSub8x8 returns the nearest and near candidates of sub-block j.
func (t *tileState) appendSub8x8(j, r int) (mv, mv) {
	b := t.bi
	var l [2]mv
	t.findMVRefs(int(b.ref[r]), &l, j)
	var nearest, near mv
	switch j {
	case 0:
		nearest, near = l[0], l[1]
	case 1, 2:
		nearest = b.bmv[0][r]
		for _, c := range l {
			if c != nearest {
				near = c
				break
			}
		}
	case 3:
		cands := [4]mv{b.bmv[1][r], b.bmv[0][r], l[0], l[1]}
		nearest = b.bmv[2][r]
		for _, c := range cands {
			if c != nearest {
				near = c
				break
			}
		}
	}
	return nearest, near
}
