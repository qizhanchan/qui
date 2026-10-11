package av1

// frame is a decoded picture with the state later frames reference.
type frame struct{}

func (d *Decoder) startFrame(h *FrameHeader) error { return nil }

func (d *Decoder) decodeTile(tn int, b []byte) error { return nil }

func (d *Decoder) finishFrame() error {
	d.updateRefs(d.hdr)
	return nil
}

func (d *Decoder) showExisting(h *FrameHeader) error {
	if h.FrameType == keyFrame {
		// The shown key frame becomes the new reference set (spec 7.21).
		slot := d.ref[h.FrameToShow]
		h.refreshFlags = (1 << numRefFrames) - 1
		for i := range d.ref {
			d.ref[i] = slot
		}
	}
	return nil
}

// updateRefs is the reference frame update process (spec 7.20).
func (d *Decoder) updateRefs(h *FrameHeader) {
	for i := 0; i < numRefFrames; i++ {
		if h.refreshFlags>>i&1 == 0 {
			continue
		}
		d.ref[i] = refSlot{
			valid:          true,
			frameID:        h.currentFrameID,
			upscaledWidth:  h.UpscaledWidth,
			frameWidth:     h.Width,
			frameHeight:    h.Height,
			renderWidth:    h.RenderWidth,
			renderHeight:   h.RenderHeight,
			miCols:         h.miCols,
			miRows:         h.miRows,
			frameType:      h.FrameType,
			orderHint:      h.OrderHint,
			savedOrderHint: h.orderHints,
			gmParams:       h.gmParams,
			lf:             h.lf,
			seg:            h.seg,
			grain:          h.grain,
			showable:       h.Showable,
			bitDepth:       d.seq.BitDepth,
		}
	}
}
