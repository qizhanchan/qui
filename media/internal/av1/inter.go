package av1

// interFrameModeInfo is inter_frame_mode_info (spec 5.11.18).
func (t *tile) interFrameModeInfo() error { return errNotImplemented }

// intraBCMode reads the motion vector of an intra block copy block.
func (t *tile) intraBCMode() error { return errNotImplemented }

// computePrediction runs inter prediction for inter blocks (intra
// prediction happens per transform block in residual).
func (t *tile) computePrediction() error {
	if t.b.isInter {
		return errNotImplemented
	}
	return nil
}
