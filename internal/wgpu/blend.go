package wgpu

// BlendPremultiplied is source-over for premultiplied colors — what qui's
// 2D layer, video frames and CAMetalLayer compositing all use. Each call
// returns a new state, so a caller changing it affects only its own
// pipelines.
func BlendPremultiplied() *BlendState {
	return &BlendState{
		Color: BlendComponent{Op: BlendAdd, Src: BlendOne, Dst: BlendOneMinusSrcAlpha},
		Alpha: BlendComponent{Op: BlendAdd, Src: BlendOne, Dst: BlendOneMinusSrcAlpha},
	}
}

// BlendAlpha is source-over for straight (non-premultiplied) colors; a new
// state per call, like BlendPremultiplied.
func BlendAlpha() *BlendState {
	return &BlendState{
		Color: BlendComponent{Op: BlendAdd, Src: BlendSrcAlpha, Dst: BlendOneMinusSrcAlpha},
		Alpha: BlendComponent{Op: BlendAdd, Src: BlendOne, Dst: BlendOneMinusSrcAlpha},
	}
}
