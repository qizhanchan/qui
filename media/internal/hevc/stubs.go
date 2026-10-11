package hevc

// Temporary stubs (inter prediction, loop filters).

func (s *sliceDec) predictionUnit(x0, y0, w, h, partIdx int, skip bool) error { return errUnsupported }
func (s *sliceDec) parsePUs(x0, y0, n int) error                              { return errUnsupported }
func (d *Decoder) deblock(p *picture)                                         {}
func (d *Decoder) applySAO(p *picture)                                        {}
