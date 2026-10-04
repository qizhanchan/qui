package media

import "github.com/qizhanchan/qui"

// Accessibility hooks. VideoView is treated as an opaque leaf — the
// agent layer surfaces its role + playback state but does not walk
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
