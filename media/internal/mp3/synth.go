package mp3

// synthState is one channel's polyphase synthesis filterbank
// (ISO/IEC 11172-3 Annex A, Figure A.2): the 1024-entry V FIFO kept as a
// ring whose write offset moves back 64 entries per subband sample slot.
type synthState struct {
	v   [2048]float64 // V mirrored twice so windows never wrap
	off int
}

// run consumes 32 subband samples and produces 32 PCM samples.
func (s *synthState) run(in *[32]float64, out []float64) {
	s.off = (s.off - 64) & 1023
	o := s.off
	// Matrixing: V[i] = sum_k cos((16+i)(2k+1)pi/64) S[k]. A direct
	// 64x32 product keeps it simple and exact; decode cost is still under
	// 1% of real time.
	for i := 0; i < 64; i++ {
		c := &synthCos[i]
		var sum float64
		for k := 0; k < 32; k++ {
			sum += c[k] * in[k]
		}
		s.v[o+i] = sum
		s.v[o+i+1024] = sum
	}
	v := s.v[o : o+1024]
	for j := 0; j < 32; j++ {
		var sum float64
		for i := 0; i < 8; i++ {
			sum += v[i*128+j]*synthWin[i*64+j] + v[i*128+96+j]*synthWin[i*64+32+j]
		}
		out[j] = sum
	}
}

func (s *synthState) reset() {
	s.v = [2048]float64{}
	s.off = 0
}
