package av1

import "fmt"

const (
	selectScreenContentTools = 2
	selectIntegerMV          = 2
)

// SequenceHeader is a parsed sequence header OBU (spec 5.5).
type SequenceHeader struct {
	Profile            int
	StillPicture       bool
	ReducedStillHeader bool

	TimingInfoPresent    bool
	NumUnitsInTick       uint32
	TimeScale            uint32
	EqualPictureInterval bool
	DecoderModelPresent  bool
	bufDelayLen          int // buffer_delay_length_minus_1 + 1
	bufferRemovalLen     int // buffer_removal_time_length_minus_1 + 1
	presentationTimeLen  int // frame_presentation_time_length_minus_1 + 1

	OperatingPoints   int
	opIdc             [32]int
	opLevel           [32]int
	opTier            [32]int
	opDecoderModel    [32]bool
	OperatingPointIdc int // of the chosen operating point (0)

	frameWidthBits, frameHeightBits int
	MaxWidth, MaxHeight             int

	FrameIDNumbersPresent bool
	deltaFrameIDLen       int // delta_frame_id_length_minus_2 + 2
	frameIDLen            int // additional + delta + 3

	Use128x128           bool
	EnableFilterIntra    bool
	EnableIntraEdge      bool
	EnableInterIntra     bool
	EnableMaskedCompound bool
	EnableWarpedMotion   bool
	EnableDualFilter     bool
	EnableOrderHint      bool
	EnableJntComp        bool
	EnableRefFrameMVs    bool
	ForceScreenContent   int
	ForceIntegerMV       int
	OrderHintBits        int
	EnableSuperres       bool
	EnableCDEF           bool
	EnableRestoration    bool

	// Colour config.
	BitDepth         int
	MonoChrome       bool
	ColorPrimaries   int
	TransferChar     int
	MatrixCoeffs     int
	FullRange        bool
	SubX, SubY       int
	ChromaSamplePos  int
	SeparateUVDeltaQ bool
	FilmGrainPresent bool
}

// NumPlanes is 1 for monochrome streams, else 3.
func (s *SequenceHeader) NumPlanes() int {
	if s.MonoChrome {
		return 1
	}
	return 3
}

// FPS returns the frame rate from the timing info, or 0.
func (s *SequenceHeader) FPS() float64 {
	if !s.TimingInfoPresent || s.NumUnitsInTick == 0 {
		return 0
	}
	return float64(s.TimeScale) / float64(s.NumUnitsInTick)
}

// ParseSequenceHeader parses a sequence header OBU payload.
func ParseSequenceHeader(b []byte) (*SequenceHeader, error) {
	return parseSequenceHeader(&bitReader{data: b})
}

func parseSequenceHeader(r *bitReader) (*SequenceHeader, error) {
	s := &SequenceHeader{}
	s.Profile = r.f(3)
	s.StillPicture = r.flag()
	s.ReducedStillHeader = r.flag()
	if s.ReducedStillHeader {
		s.OperatingPoints = 1
		s.opLevel[0] = r.f(5)
	} else {
		s.TimingInfoPresent = r.flag()
		if s.TimingInfoPresent {
			s.NumUnitsInTick = uint32(r.f(32))
			s.TimeScale = uint32(r.f(32))
			s.EqualPictureInterval = r.flag()
			if s.EqualPictureInterval {
				r.uvlc()
			}
			s.DecoderModelPresent = r.flag()
			if s.DecoderModelPresent {
				s.bufDelayLen = r.f(5) + 1
				r.f(32) // num_units_in_decoding_tick
				s.bufferRemovalLen = r.f(5) + 1
				s.presentationTimeLen = r.f(5) + 1
			}
		}
		initialDisplayDelay := r.flag()
		s.OperatingPoints = r.f(5) + 1
		for i := 0; i < s.OperatingPoints; i++ {
			s.opIdc[i] = r.f(12)
			s.opLevel[i] = r.f(5)
			if s.opLevel[i] > 7 {
				s.opTier[i] = r.f(1)
			}
			if s.DecoderModelPresent {
				s.opDecoderModel[i] = r.flag()
				if s.opDecoderModel[i] {
					r.f(s.bufDelayLen) // decoder_buffer_delay
					r.f(s.bufDelayLen) // encoder_buffer_delay
					r.f(1)             // low_delay_mode_flag
				}
			}
			if initialDisplayDelay {
				if r.flag() {
					r.f(4)
				}
			}
		}
	}
	s.OperatingPointIdc = s.opIdc[0]
	s.frameWidthBits = r.f(4) + 1
	s.frameHeightBits = r.f(4) + 1
	s.MaxWidth = r.f(s.frameWidthBits) + 1
	s.MaxHeight = r.f(s.frameHeightBits) + 1
	if !s.ReducedStillHeader {
		s.FrameIDNumbersPresent = r.flag()
	}
	if s.FrameIDNumbersPresent {
		s.deltaFrameIDLen = r.f(4) + 2
		s.frameIDLen = r.f(3) + 1 + s.deltaFrameIDLen
	}
	s.Use128x128 = r.flag()
	s.EnableFilterIntra = r.flag()
	s.EnableIntraEdge = r.flag()
	if s.ReducedStillHeader {
		s.ForceScreenContent = selectScreenContentTools
		s.ForceIntegerMV = selectIntegerMV
	} else {
		s.EnableInterIntra = r.flag()
		s.EnableMaskedCompound = r.flag()
		s.EnableWarpedMotion = r.flag()
		s.EnableDualFilter = r.flag()
		s.EnableOrderHint = r.flag()
		if s.EnableOrderHint {
			s.EnableJntComp = r.flag()
			s.EnableRefFrameMVs = r.flag()
		}
		if r.flag() {
			s.ForceScreenContent = selectScreenContentTools
		} else {
			s.ForceScreenContent = r.f(1)
		}
		s.ForceIntegerMV = selectIntegerMV
		if s.ForceScreenContent > 0 {
			if !r.flag() {
				s.ForceIntegerMV = r.f(1)
			}
		}
		if s.EnableOrderHint {
			s.OrderHintBits = r.f(3) + 1
		}
	}
	s.EnableSuperres = r.flag()
	s.EnableCDEF = r.flag()
	s.EnableRestoration = r.flag()
	if err := s.colorConfig(r); err != nil {
		return nil, err
	}
	s.FilmGrainPresent = r.flag()
	if r.overrun() {
		return nil, fmt.Errorf("%w: truncated sequence header", ErrInvalid)
	}
	return s, nil
}

func (s *SequenceHeader) colorConfig(r *bitReader) error {
	high := r.flag()
	s.BitDepth = 8
	if s.Profile == 2 && high {
		if r.flag() {
			s.BitDepth = 12
		} else {
			s.BitDepth = 10
		}
	} else if s.Profile <= 2 && high {
		s.BitDepth = 10
	}
	if s.Profile != 1 {
		s.MonoChrome = r.flag()
	}
	s.ColorPrimaries, s.TransferChar, s.MatrixCoeffs = 2, 2, 2
	if r.flag() {
		s.ColorPrimaries = r.f(8)
		s.TransferChar = r.f(8)
		s.MatrixCoeffs = r.f(8)
	}
	switch {
	case s.MonoChrome:
		s.FullRange = r.flag()
		s.SubX, s.SubY = 1, 1
		return s.supported()
	case s.ColorPrimaries == 1 && s.TransferChar == 13 && s.MatrixCoeffs == 0:
		s.FullRange = true
	default:
		s.FullRange = r.flag()
		switch s.Profile {
		case 0:
			s.SubX, s.SubY = 1, 1
		case 1:
		default:
			if s.BitDepth == 12 {
				s.SubX = r.f(1)
				if s.SubX != 0 {
					s.SubY = r.f(1)
				}
			} else {
				s.SubX = 1
			}
		}
		if s.SubX != 0 && s.SubY != 0 {
			s.ChromaSamplePos = r.f(2)
		}
	}
	s.SeparateUVDeltaQ = r.flag()
	return s.supported()
}

// supported rejects what the decoder does not implement: anything but
// 4:2:0 / monochrome at 8 or 10 bits (Main profile).
func (s *SequenceHeader) supported() error {
	if s.SubX != 1 || s.SubY != 1 || s.BitDepth > 10 {
		return fmt.Errorf("%w: profile %d, %d-bit, subsampling %d,%d", ErrUnsupported, s.Profile, s.BitDepth, s.SubX, s.SubY)
	}
	return nil
}
