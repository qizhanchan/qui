//go:build cgo

package media

import (
	"fmt"
	"strings"
	"unsafe"

	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/qizhanchan/qui"
)

// GPU colour conversion for the pure-Go backend: the three I420 planes are
// uploaded as R8 textures and converted to RGB in a fragment shader, so no
// per-pixel YUV→RGB work happens on the CPU.

const yuvVertexSrc = `#version 330 core
layout(location = 0) in vec2 aPos;
layout(location = 1) in vec2 aUV;
out vec2 vUV;
void main() {
    gl_Position = vec4(aPos, 0.0, 1.0);
    vUV = aUV;
}` + "\x00"

const yuvFragmentSrc = `#version 330 core
in vec2 vUV;
out vec4 fragColor;
uniform sampler2D uY;
uniform sampler2D uU;
uniform sampler2D uV;
uniform mat3 uM;      // YCbCr (range-expanded) -> RGB
uniform vec3 uOff;    // subtracted before uScale
uniform vec3 uScale;  // range expansion
void main() {
    vec3 yuv = vec3(texture(uY, vUV).r, texture(uU, vUV).r, texture(uV, vUV).r);
    yuv = (yuv - uOff) * uScale;
    fragColor = vec4(clamp(uM * yuv, 0.0, 1.0), 1.0);
}` + "\x00"

type yuvRenderer struct {
	ready, failed bool
	program       uint32
	vao, vbo      uint32
	tex           [3]uint32
	texW, texH    [3]int32
	uY, uU, uV    int32
	uM, uOff, uSc int32

	last  *golangVideoFrame // frame currently held in the textures
	lastG uint64

	// verts is what vbo holds (vertsOK once written). The quad rarely
	// changes, and rewriting a buffer the GPU may still be reading stalls
	// the driver, so identical quads are not re-uploaded and a changed one
	// orphans the old storage first.
	verts   [24]float32
	vertsOK bool
}

var gYUV yuvRenderer

// pendingTexDeletes holds GL textures released off the GL thread; they are
// deleted on the next GL call.
var pendingTexDeletes = make(chan uint32, 1024)

func drainTexDeletes() {
	for {
		select {
		case t := <-pendingTexDeletes:
			gl.DeleteTextures(1, &t)
		default:
			return
		}
	}
}

func yuvCompile(src string, kind uint32) (uint32, error) {
	sh := gl.CreateShader(kind)
	cstr, free := gl.Strs(src)
	defer free()
	n := int32(len(src) - 1)
	gl.ShaderSource(sh, 1, cstr, &n)
	gl.CompileShader(sh)
	var ok int32
	gl.GetShaderiv(sh, gl.COMPILE_STATUS, &ok)
	if ok == gl.FALSE {
		var l int32
		gl.GetShaderiv(sh, gl.INFO_LOG_LENGTH, &l)
		buf := strings.Repeat("\x00", int(l+1))
		gl.GetShaderInfoLog(sh, l, nil, gl.Str(buf))
		gl.DeleteShader(sh)
		return 0, fmt.Errorf("yuv shader: %s", buf)
	}
	return sh, nil
}

func (r *yuvRenderer) init() bool {
	if r.ready {
		return true
	}
	if r.failed {
		return false
	}
	vs, err := yuvCompile(yuvVertexSrc, gl.VERTEX_SHADER)
	if err != nil {
		r.failed = true
		vvDebugf("%v", err)
		return false
	}
	fs, err := yuvCompile(yuvFragmentSrc, gl.FRAGMENT_SHADER)
	if err != nil {
		gl.DeleteShader(vs)
		r.failed = true
		vvDebugf("%v", err)
		return false
	}
	p := gl.CreateProgram()
	gl.AttachShader(p, vs)
	gl.AttachShader(p, fs)
	gl.LinkProgram(p)
	gl.DeleteShader(vs)
	gl.DeleteShader(fs)
	var ok int32
	gl.GetProgramiv(p, gl.LINK_STATUS, &ok)
	if ok == gl.FALSE {
		gl.DeleteProgram(p)
		r.failed = true
		return false
	}
	r.program = p
	r.uY = gl.GetUniformLocation(p, gl.Str("uY\x00"))
	r.uU = gl.GetUniformLocation(p, gl.Str("uU\x00"))
	r.uV = gl.GetUniformLocation(p, gl.Str("uV\x00"))
	r.uM = gl.GetUniformLocation(p, gl.Str("uM\x00"))
	r.uOff = gl.GetUniformLocation(p, gl.Str("uOff\x00"))
	r.uSc = gl.GetUniformLocation(p, gl.Str("uScale\x00"))

	gl.GenVertexArrays(1, &r.vao)
	gl.GenBuffers(1, &r.vbo)
	gl.BindVertexArray(r.vao)
	gl.BindBuffer(gl.ARRAY_BUFFER, r.vbo)
	gl.BufferData(gl.ARRAY_BUFFER, 6*4*4, nil, gl.DYNAMIC_DRAW)
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointer(0, 2, gl.FLOAT, false, 16, gl.PtrOffset(0))
	gl.EnableVertexAttribArray(1)
	gl.VertexAttribPointer(1, 2, gl.FLOAT, false, 16, gl.PtrOffset(8))
	gl.BindBuffer(gl.ARRAY_BUFFER, 0)
	gl.BindVertexArray(0)

	gl.GenTextures(3, &r.tex[0])
	for _, t := range r.tex {
		gl.BindTexture(gl.TEXTURE_2D, t)
		gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
		gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
		gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
		gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	}
	gl.BindTexture(gl.TEXTURE_2D, 0)
	r.ready = true
	return true
}

func (r *yuvRenderer) upload(i int, pix []byte, w, h, stride int) {
	gl.BindTexture(gl.TEXTURE_2D, r.tex[i])
	gl.PixelStorei(gl.UNPACK_ALIGNMENT, 1)
	gl.PixelStorei(gl.UNPACK_ROW_LENGTH, int32(stride))
	if r.texW[i] != int32(w) || r.texH[i] != int32(h) {
		gl.TexImage2D(gl.TEXTURE_2D, 0, gl.R8, int32(w), int32(h), 0, gl.RED, gl.UNSIGNED_BYTE, unsafe.Pointer(&pix[0]))
		r.texW[i], r.texH[i] = int32(w), int32(h)
	} else {
		gl.TexSubImage2D(gl.TEXTURE_2D, 0, 0, 0, int32(w), int32(h), gl.RED, gl.UNSIGNED_BYTE, unsafe.Pointer(&pix[0]))
	}
	gl.PixelStorei(gl.UNPACK_ROW_LENGTH, 0)
}

// colorMatrix returns the YCbCr→RGB matrix (column-major for GL) plus the
// range offset/scale for the frame's VUI colour description.
func colorMatrix(matrix int, fullRange bool, height int) (m [9]float32, off, scale [3]float32) {
	kr, kb := float32(0.299), float32(0.114) // BT.601
	switch matrix {
	case 1: // BT.709
		kr, kb = 0.2126, 0.0722
	case 9, 10: // BT.2020
		kr, kb = 0.2627, 0.0593
	case 5, 6:
	default:
		if height >= 720 {
			kr, kb = 0.2126, 0.0722
		}
	}
	kg := 1 - kr - kb
	// R = Y + 2(1-kr) Cr; B = Y + 2(1-kb) Cb; G = (Y - kr R - kb B)/kg.
	rv := 2 * (1 - kr)
	bu := 2 * (1 - kb)
	gu := -bu * kb / kg
	gv := -rv * kr / kg
	// Columns: Y, Cb, Cr.
	m = [9]float32{1, 1, 1, 0, gu, bu, rv, gv, 0}
	if fullRange {
		off = [3]float32{0, 128.0 / 255, 128.0 / 255}
		scale = [3]float32{1, 1, 1}
	} else {
		off = [3]float32{16.0 / 255, 128.0 / 255, 128.0 / 255}
		scale = [3]float32{255.0 / 219, 255.0 / 224, 255.0 / 224}
	}
	return
}

// draw renders frame f into physDst of the current framebuffer.
func (r *yuvRenderer) draw(f *golangVideoFrame, physDst qui.Rect, fb qui.Size, flipY bool) bool {
	drainTexDeletes()
	if !r.init() || fb.W <= 0 || fb.H <= 0 || physDst.W <= 0 || physDst.H <= 0 {
		return false
	}
	fr := f.frame
	if fr == nil {
		return false
	}
	if r.last != f || f.gen != r.lastG {
		r.upload(0, fr.Y.Pix, fr.Y.W, fr.Y.H, fr.Y.Stride)
		r.upload(1, fr.Cb.Pix, fr.Cb.W, fr.Cb.H, fr.Cb.Stride)
		r.upload(2, fr.Cr.Pix, fr.Cr.W, fr.Cr.H, fr.Cr.Stride)
		r.last, r.lastG = f, f.gen
	}
	x0 := physDst.X/fb.W*2 - 1
	y0 := 1 - physDst.Y/fb.H*2
	x1 := (physDst.X+physDst.W)/fb.W*2 - 1
	y1 := 1 - (physDst.Y+physDst.H)/fb.H*2
	vTop, vBot := float32(0), float32(1)
	if !flipY {
		vTop, vBot = 1, 0
	}
	verts := [...]float32{
		x0, y0, 0, vTop,
		x0, y1, 0, vBot,
		x1, y1, 1, vBot,
		x0, y0, 0, vTop,
		x1, y1, 1, vBot,
		x1, y0, 1, vTop,
	}
	m, off, sc := colorMatrix(f.matrix, f.fullRange, fr.Height)

	gl.Disable(gl.BLEND)
	gl.Disable(gl.DEPTH_TEST)
	gl.Disable(gl.SCISSOR_TEST)
	gl.Disable(gl.CULL_FACE)
	gl.UseProgram(r.program)
	for i := 0; i < 3; i++ {
		gl.ActiveTexture(gl.TEXTURE0 + uint32(i))
		gl.BindTexture(gl.TEXTURE_2D, r.tex[i])
	}
	gl.Uniform1i(r.uY, 0)
	gl.Uniform1i(r.uU, 1)
	gl.Uniform1i(r.uV, 2)
	gl.UniformMatrix3fv(r.uM, 1, false, &m[0])
	gl.Uniform3fv(r.uOff, 1, &off[0])
	gl.Uniform3fv(r.uSc, 1, &sc[0])
	gl.BindVertexArray(r.vao)
	gl.BindBuffer(gl.ARRAY_BUFFER, r.vbo)
	if !r.vertsOK || verts != r.verts {
		gl.BufferData(gl.ARRAY_BUFFER, len(verts)*4, nil, gl.DYNAMIC_DRAW)
		gl.BufferSubData(gl.ARRAY_BUFFER, 0, len(verts)*4, unsafe.Pointer(&verts[0]))
		r.verts, r.vertsOK = verts, true
	}
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
	for i := 2; i >= 0; i-- {
		gl.ActiveTexture(gl.TEXTURE0 + uint32(i))
		gl.BindTexture(gl.TEXTURE_2D, 0)
	}
	gl.UseProgram(0)
	gl.Enable(gl.BLEND)
	return true
}
