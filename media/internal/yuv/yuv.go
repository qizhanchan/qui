// Package yuv converts planar YUV frames (I420 / 4:2:0) into packed RGBA for
// display, and provides the GLSL source for uploading I420 planes directly on
// the GPU. It is pure Go and has no dependency on the rest of the media stack.
package yuv

import "image"

// Colorspace selects the luma/chroma matrix used to convert YUV to RGB.
type Colorspace int

const (
	// BT601 is the SD/HD legacy matrix (default for most MP4 video).
	BT601 Colorspace = iota
	// BT709 is the HD matrix used by most 1080p/4K content.
	BT709
)

// clipByte clamps an int to [0, 255].
func clipByte(v int) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

// I420ToRGBA converts a 4:2:0 (I420) frame into dst (RGBA, 8 bits per channel).
//
// The Y, Cb and Cr planes are given with their own strides (bytes per row).
// The destination is written starting at dst.Pix[0] with dst.Stride; dst must
// be at least (width x height) with matching stride. The conversion uses the
// limited-range (video) BT.601 matrix, which is what H.264 streams carry by
// default.
func I420ToRGBA(dst *image.RGBA, y, cb, cr []byte, width, height, strideY, strideC int) {
	I420ToRGBAColor(dst, y, cb, cr, width, height, strideY, strideC, BT601)
}

// I420ToRGBAColor is I420ToRGBA with an explicit colorspace.
func I420ToRGBAColor(dst *image.RGBA, y, cb, cr []byte, width, height, strideY, strideC int, cs Colorspace) {
	// Limited-range coefficients, scaled by 256.
	//   BT.601: R = 1.164(Y-16) + 1.596(V-128)
	//           G = 1.164(Y-16) - 0.391(U-128) - 0.813(V-128)
	//           B = 1.164(Y-16) + 2.018(U-128)
	//   BT.709: R = 1.164(Y-16) + 1.793(V-128)
	//           G = 1.164(Y-16) - 0.213(U-128) - 0.533(V-128)
	//           B = 1.164(Y-16) + 2.112(U-128)
	var yc, rv, gu, gv, bu int
	switch cs {
	case BT709:
		yc, rv, gu, gv, bu = 298, 459, -55, -136, 541
	default:
		yc, rv, gu, gv, bu = 298, 409, -100, -208, 516
	}

	for row := 0; row < height; row++ {
		yRow := row * strideY
		cRow := (row / 2) * strideC
		dRow := row * dst.Stride
		for col := 0; col < width; col++ {
			yy := int(y[yRow+col]) - 16
			if yy < 0 {
				yy = 0
			}
			ci := col / 2
			uu := int(cb[cRow+ci]) - 128
			vv := int(cr[cRow+ci]) - 128

			luma := yc * yy
			r := (luma + rv*vv + 128) >> 8
			g := (luma + gu*uu + gv*vv + 128) >> 8
			b := (luma + bu*uu + 128) >> 8

			o := dRow + col*4
			dst.Pix[o+0] = clipByte(r)
			dst.Pix[o+1] = clipByte(g)
			dst.Pix[o+2] = clipByte(b)
			dst.Pix[o+3] = 255
		}
	}
}

// I420ToRGBAInto is a convenience wrapper that converts into a caller-provided
// tightly-packed RGBA buffer (stride = width*4).
func I420ToRGBAInto(dst []byte, y, cb, cr []byte, width, height int) {
	img := &image.RGBA{Pix: dst, Stride: width * 4, Rect: image.Rect(0, 0, width, height)}
	I420ToRGBA(img, y, cb, cr, width, height, width, (width+1)/2)
}

// I420ShaderVertex is a trivial full-screen pass-through vertex shader.
const I420ShaderVertex = `#version 330 core
layout(location = 0) in vec2 aPos;
layout(location = 1) in vec2 aUV;
out vec2 vUV;
void main() {
    vUV = aUV;
    gl_Position = vec4(aPos, 0.0, 1.0);
}
`

// I420ShaderFragment samples the three I420 planes (uploaded as R8 textures)
// and converts to RGB with the limited-range BT.601 matrix. uTexY/uTexU/uTexV
// are texture units; uScale accounts for the chroma subsampling so the chroma
// coordinates map onto the half-resolution planes.
const I420ShaderFragment = `#version 330 core
in vec2 vUV;
out vec4 fragColor;
uniform sampler2D uTexY;
uniform sampler2D uTexU;
uniform sampler2D uTexV;
void main() {
    float y = texture(uTexY, vUV).r;
    float u = texture(uTexU, vUV).r;
    float v = texture(uTexV, vUV).r;

    // Limited range: expand to full range.
    y = 1.1643 * (y - 0.0625);
    u = u - 0.5;
    v = v - 0.5;

    float r = y + 1.5958 * v;
    float g = y - 0.39173 * u - 0.81290 * v;
    float b = y + 2.017 * u;
    fragColor = vec4(r, g, b, 1.0);
}
`

// I420ToRGBAColorRange converts into a tightly packed RGBA buffer (stride
// width*4) with an explicit matrix and range (full = JPEG/PC range).
func I420ToRGBAColorRange(dst []byte, y, cb, cr []byte, width, height, strideY, strideC int, cs Colorspace, full bool) {
	if !full {
		img := &image.RGBA{Pix: dst, Stride: width * 4, Rect: image.Rect(0, 0, width, height)}
		I420ToRGBAColor(img, y, cb, cr, width, height, strideY, strideC, cs)
		return
	}
	// Full-range coefficients, scaled by 256.
	rv, gu, gv, bu := 359, -88, -183, 454
	if cs == BT709 {
		rv, gu, gv, bu = 403, -48, -120, 475
	}
	for row := 0; row < height; row++ {
		yRow := row * strideY
		cRow := (row / 2) * strideC
		d := dst[row*width*4:]
		for col := 0; col < width; col++ {
			luma := int(y[yRow+col]) << 8
			uu := int(cb[cRow+col/2]) - 128
			vv := int(cr[cRow+col/2]) - 128
			o := col * 4
			d[o+0] = clipByte((luma + rv*vv + 128) >> 8)
			d[o+1] = clipByte((luma + gu*uu + gv*vv + 128) >> 8)
			d[o+2] = clipByte((luma + bu*uu + 128) >> 8)
			d[o+3] = 255
		}
	}
}
