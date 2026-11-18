package vp9

import (
	"fmt"
	"sync"
)

// decodeStats counts stream features (coverage checks in tests).
type decodeStats struct {
	mu                                                    sync.Mutex
	frames, hidden, showExisting, intraOnly, keyframes    int
	errorRes, switchable, segmentation, compoundFrames    int
	compoundBlocks, sub8x8Inter, sub8x8Intra, intraBlocks int
	interBlocks, skipped                                  int
	tx                                                    [4]int
	refScaled                                             int
}

func (s *decodeStats) frame(h *frameHeader) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.frames++
	if h.showExisting {
		s.showExisting++
		return
	}
	if !h.showFrame {
		s.hidden++
	}
	if h.intraOnlyFlag {
		s.intraOnly++
	}
	if h.frameType == keyFrame {
		s.keyframes++
	}
	if h.errorRes {
		s.errorRes++
	}
	if h.interpFilter == filterSwitchable && !h.intraOnly() {
		s.switchable++
	}
}

func (s *decodeStats) block(b *blockInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b.isInter {
		s.interBlocks++
		if b.hasSecond() {
			s.compoundBlocks++
		}
		if b.bsize < block8x8 {
			s.sub8x8Inter++
		}
	} else {
		s.intraBlocks++
		if b.bsize < block8x8 {
			s.sub8x8Intra++
		}
	}
	if b.skip {
		s.skipped++
	}
	s.tx[b.tx]++
}

func (s *decodeStats) String() string {
	return fmt.Sprintf("frames %d key %d hidden %d showExisting %d intraOnly %d errRes %d switchable %d | blocks intra %d (sub8 %d) inter %d (sub8 %d, compound %d) skip %d tx %v",
		s.frames, s.keyframes, s.hidden, s.showExisting, s.intraOnly, s.errorRes, s.switchable,
		s.intraBlocks, s.sub8x8Intra, s.interBlocks, s.sub8x8Inter, s.compoundBlocks, s.skipped, s.tx)
}
