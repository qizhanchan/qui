package media

import (
	"fmt"

	"github.com/qizhanchan/qui"
)

// Accessibility hooks. VideoView and FrameView are opaque leaves — the
// agent layer surfaces their role and the frame's size, but does not walk
// into per-frame data.

func (v *VideoView) Role() string { return qui.RoleVideo }

func (v *VideoView) AccessibleValue() string {
	if v == nil {
		return ""
	}
	return v.State().String()
}

func (v *VideoView) AccessibleState() qui.AccessibleState {
	if v == nil {
		return 0
	}
	if v.State() == StatePlaying {
		return qui.AXStateBusy
	}
	return 0
}

// FrameView shows pushed frames; its value is the newest frame's size.

func (v *FrameView) Role() string { return qui.RoleVideo }

func (v *FrameView) AccessibleValue() string {
	if v == nil {
		return ""
	}
	w, h := v.FrameSize()
	if w <= 0 || h <= 0 {
		return "no frame"
	}
	return fmt.Sprintf("%dx%d", w, h)
}
