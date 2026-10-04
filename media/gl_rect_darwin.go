//go:build darwin && cgo

package media

import (
	"fmt"
	"strings"
	"unsafe"

	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/qizhanchan/qui"
)

// CVOpenGLTextureCache on macOS hands back GL_TEXTURE_RECTANGLE
// textures (0x84F5) — not GL_TEXTURE_2D — because IOSurface-backed
// textures use non-normalized texel coords. The renderer's stock
// texQuad shader samples sampler2D, so we cannot route these
// frames through GLRenderer.DrawTexture. This file defines a tiny
// sampler2DRect shader + VAO that lives ENTIRELY inside the media
// package; it's invoked from VideoView.Draw's QueueGLDraw closure
// to blit one rect frame per Draw.

const rectVertexShaderSrc = `#version 330 core
layout(location = 0) in vec2 aPos;
layout(location = 1) in vec2 aUV;
out vec2 vUV;
void main() {
    gl_Position = vec4(aPos, 0.0, 1.0);
    vUV = aUV;
}` + "\x00"

const rectFragmentShaderSrc = `#version 330 core
in vec2 vUV;
out vec4 fragColor;
uniform sampler2DRect uTex;
void main() {
    fragColor = texture(uTex, vUV);
}` + "\x00"

type rectDrawer struct {
	program uint32
	vao     uint32
	vbo     uint32
	uTexLoc int32
	ready   bool
	initErr error
}

var gRectDrawer rectDrawer

func (d *rectDrawer) ensureInit() bool {
	if d.ready {
		return true
	}
	if d.initErr != nil {
		return false
	}
	vs, err := compileShader(rectVertexShaderSrc, gl.VERTEX_SHADER)
	if err != nil {
		d.initErr = fmt.Errorf("rect vertex: %w", err)
		debugf("rectDrawer init: %v", d.initErr)
		return false
	}
	fs, err := compileShader(rectFragmentShaderSrc, gl.FRAGMENT_SHADER)
	if err != nil {
		gl.DeleteShader(vs)
		d.initErr = fmt.Errorf("rect fragment: %w", err)
		debugf("rectDrawer init: %v", d.initErr)
		return false
	}
	prog := gl.CreateProgram()
	gl.AttachShader(prog, vs)
	gl.AttachShader(prog, fs)
	gl.LinkProgram(prog)
	gl.DeleteShader(vs)
	gl.DeleteShader(fs)

	var status int32
	gl.GetProgramiv(prog, gl.LINK_STATUS, &status)
	if status == gl.FALSE {
		var logLen int32
		gl.GetProgramiv(prog, gl.INFO_LOG_LENGTH, &logLen)
		buf := strings.Repeat("\x00", int(logLen+1))
		gl.GetProgramInfoLog(prog, logLen, nil, gl.Str(buf))
		gl.DeleteProgram(prog)
		d.initErr = fmt.Errorf("rect link: %s", buf)
		debugf("rectDrawer init: %v", d.initErr)
		return false
	}
	d.program = prog

	gl.GenVertexArrays(1, &d.vao)
	gl.GenBuffers(1, &d.vbo)
	gl.BindVertexArray(d.vao)
	gl.BindBuffer(gl.ARRAY_BUFFER, d.vbo)
	// Six vertices × 4 floats (x, y, u, v). Allocated dynamic;
	// updated per draw via BufferSubData.
	gl.BufferData(gl.ARRAY_BUFFER, 6*4*4, nil, gl.DYNAMIC_DRAW)
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointer(0, 2, gl.FLOAT, false, 4*4, gl.PtrOffset(0))
	gl.EnableVertexAttribArray(1)
	gl.VertexAttribPointer(1, 2, gl.FLOAT, false, 4*4, gl.PtrOffset(2*4))
	gl.BindBuffer(gl.ARRAY_BUFFER, 0)
	gl.BindVertexArray(0)

	d.uTexLoc = gl.GetUniformLocation(prog, gl.Str("uTex\x00"))
	d.ready = true
	debugf("rectDrawer ready prog=%d vao=%d vbo=%d uTex=%d", prog, d.vao, d.vbo, d.uTexLoc)
	return true
}

func compileShader(src string, shaderType uint32) (uint32, error) {
	sh := gl.CreateShader(shaderType)
	cstr, free := gl.Strs(src)
	defer free()
	length := int32(len(src) - 1)
	gl.ShaderSource(sh, 1, cstr, &length)
	gl.CompileShader(sh)
	var status int32
	gl.GetShaderiv(sh, gl.COMPILE_STATUS, &status)
	if status == gl.FALSE {
		var logLen int32
		gl.GetShaderiv(sh, gl.INFO_LOG_LENGTH, &logLen)
		buf := strings.Repeat("\x00", int(logLen+1))
		gl.GetShaderInfoLog(sh, logLen, nil, gl.Str(buf))
		gl.DeleteShader(sh)
		return 0, fmt.Errorf("compile: %s", buf)
	}
	return sh, nil
}

// drawRectTexture blits a GL_TEXTURE_RECTANGLE texture into the
// current framebuffer. physDst is in framebuffer pixels. fb is the
// framebuffer size. texSize is the texture's pixel dimensions
// (needed because RECTANGLE uses non-normalized UVs). flipY=true
// samples the texture top-down (matches CVPixelBuffer's image
// convention so the picture renders right-side-up under qui's
// y-down 2D coord system).
func drawRectTexture(texName uint32, physDst qui.Rect, fb qui.Size, texSize qui.Size, flipY bool) {
	if !gRectDrawer.ensureInit() {
		return
	}
	if fb.W <= 0 || fb.H <= 0 || physDst.W <= 0 || physDst.H <= 0 {
		return
	}
	// NDC quad — same Y-flip convention as GLRenderer.DrawTexture
	// (y=0 logical → y=1 NDC).
	x0 := physDst.X/fb.W*2 - 1
	y0 := 1 - physDst.Y/fb.H*2
	x1 := (physDst.X+physDst.W)/fb.W*2 - 1
	y1 := 1 - (physDst.Y+physDst.H)/fb.H*2

	// RECTANGLE UVs in texel space.
	tw, th := texSize.W, texSize.H
	var uTop, uBot, uLeft, uRight float32
	uLeft, uRight = 0, tw
	if flipY {
		// Image is stored top-down; top of dst → V=0.
		uTop, uBot = 0, th
	} else {
		uTop, uBot = th, 0
	}

	verts := [...]float32{
		// pos       // uv
		x0, y0, uLeft, uTop, // top-left
		x0, y1, uLeft, uBot, // bottom-left
		x1, y1, uRight, uBot, // bottom-right
		x0, y0, uLeft, uTop, // top-left
		x1, y1, uRight, uBot, // bottom-right
		x1, y0, uRight, uTop, // top-right
	}

	// Save / restore is overkill — End() re-establishes blit state
	// afterwards, so we just set what we need.
	gl.Enable(gl.BLEND)
	gl.BlendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA)
	gl.Disable(gl.DEPTH_TEST)
	gl.Disable(gl.SCISSOR_TEST)
	gl.Disable(gl.CULL_FACE)

	gl.UseProgram(gRectDrawer.program)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_RECTANGLE, texName)
	gl.Uniform1i(gRectDrawer.uTexLoc, 0)

	gl.BindVertexArray(gRectDrawer.vao)
	gl.BindBuffer(gl.ARRAY_BUFFER, gRectDrawer.vbo)
	gl.BufferSubData(gl.ARRAY_BUFFER, 0, len(verts)*4, unsafe.Pointer(&verts[0]))
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
	gl.BindTexture(gl.TEXTURE_RECTANGLE, 0)
	gl.UseProgram(0)
}
