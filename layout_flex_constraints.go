package qui

import "math"

// Resolve a max-constrained flex line using unclamped bases. CSS Flexbox
// §9.7 freezes min/max violations and redistributes the remaining space;
// a max-clamped base must not be mistaken for the item's content size.
func (l FlexLayout) resolveClampedBases(items []flexItem, available float32) {
	available -= l.Gap * float32(len(items)-1)
	var hypothetical float32
	for _, it := range items {
		hypothetical += it.basis
	}
	grow := hypothetical < available
	frozen := make([]bool, len(items))
	factor := make([]float32, len(items))
	base := make([]float32, len(items))
	weight := make([]float32, len(items))
	for i := range items {
		it := &items[i]
		base[i] = it.flexBase + it.marginMainSum
		factor[i] = it.flex.effectiveShrink()
		weight[i] = factor[i] * max(0, it.flexBase-it.w.Style().ContentInsets().Horizontal())
		if l.Direction == Vertical {
			weight[i] = factor[i] * max(0, it.flexBase-it.w.Style().ContentInsets().Vertical())
		}
		if grow {
			factor[i], weight[i] = it.flex.Grow, it.flex.Grow
		}
		frozen[i] = factor[i] == 0 || (grow && base[i] > it.basis) || (!grow && base[i] < it.basis)
		it.size = it.basis
	}
	remaining := func() float32 {
		free := available
		for i, it := range items {
			if frozen[i] {
				free -= it.size
			} else {
				free -= base[i]
			}
		}
		return free
	}
	initialFree := remaining()
	for step := 0; step <= len(items); step++ {
		var sum, weights float32
		for i := range items {
			if !frozen[i] {
				sum += factor[i]
				weights += weight[i]
			}
		}
		if sum == 0 {
			break
		}
		free := remaining()
		if sum < 1 && math.Abs(float64(initialFree*sum)) < math.Abs(float64(free)) {
			free = initialFree * sum
		}
		violations := make([]float32, len(items))
		var total float32
		for i := range items {
			if frozen[i] {
				continue
			}
			target := base[i]
			if weights > 0 {
				target += free * weight[i] / weights
			}
			items[i].size = max(items[i].minMain, target)
			if items[i].maxMain > 0 && items[i].size > items[i].maxMain {
				items[i].size = max(items[i].minMain, items[i].maxMain)
			}
			violations[i] = items[i].size - target
			total += violations[i]
		}
		for i := range items {
			if math.Abs(float64(total)) < .001 || total > 0 && violations[i] > 0 || total < 0 && violations[i] < 0 {
				frozen[i] = true
			}
		}
	}
}
