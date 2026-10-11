package h264enc

// Temporary stubs for milestones not yet implemented (E3/E4).

type cabacEnc struct{}

func (c *cabacEnc) init(w *bitWriter, typ, initIdc, qp int) {}
func (c *cabacEnc) encodeTerminate(last bool)               {}
func (c *cabacEnc) finish()                                 {}
func (s *sliceEnc) writeMBCABAC()                           {}
func (s *sliceEnc) cabacSkip(skip bool)                     {}
func (s *sliceEnc) encodeB()                                {}
func (s *sliceEnc) writeBPredCAVLC()                        {}
func (s *sliceEnc) analyseI8() int                          { return 1 << 30 }
func (s *sliceEnc) codeI8Final()                            {}
func (s *sliceEnc) choose8x8Inter() bool                    { return false }
func (s *sliceEnc) codeInter8()                             {}
func (e *Encoder) setupBLists()                             {}
func (e *Encoder) queueB(src *srcPic, pts int64) []Packet   { return nil }
func (e *Encoder) flushB() []Packet                         { return nil }

