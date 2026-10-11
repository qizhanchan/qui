package h264

// Plane is a single 8-bit image plane.
type Plane struct {
	Pix    []byte
	Stride int
	W, H   int
}

func newPlane(w, h int) *Plane {
	return &Plane{Pix: make([]byte, w*h), Stride: w, W: w, H: h}
}

// At returns the sample at (x, y).
func (p *Plane) At(x, y int) byte { return p.Pix[y*p.Stride+x] }

// Set stores v at (x, y).
func (p *Plane) Set(x, y int, v byte) { p.Pix[y*p.Stride+x] = v }

// Frame is a decoded picture in 4:2:0 (I420) layout.
type Frame struct {
	Y, Cb, Cr     *Plane
	Width, Height int
}

// NewFrame allocates a 4:2:0 frame of the given luma dimensions.
func NewFrame(width, height int) *Frame {
	cw, ch := (width+1)/2, (height+1)/2
	return &Frame{
		Y:      newPlane(width, height),
		Cb:     newPlane(cw, ch),
		Cr:     newPlane(cw, ch),
		Width:  width,
		Height: height,
	}
}

// chromaSize returns the 4:2:0 chroma plane dimensions for a luma size.
func chromaSize(luma int) int { return (luma + 1) / 2 }
