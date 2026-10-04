package qui

import (
	"github.com/go-gl/gl/v3.3-core/gl"
)

// backend_gpu_shaders.go isolates the GL shader source strings + the
// one-time program compilation for gpuBackend's shape family. Kept
// separate from backend_gpu.go so the algorithmic code stays readable
// and the shader source can be edited without scrolling past the
// backend struct.
//
// # Vertex layout
//
// A single static unit quad in shapeVBO (six vertices, 2×float per
// vertex, values in [0,1]²). The vertex shader mixes each corner
// with uRect (pixel-space {x0, y0, x1, y1}) then projects to NDC using
// uFBSize. Y is flipped because qui uses a top-down pixel coordinate
// system while OpenGL NDC is bottom-up.
//
// # Fragment logic
//
// Branchless via uMode + uRadius / uStroke / uSoftness. Every mode
// emits PREMULTIPLIED RGBA to match the FBO's premul convention
// (gpuBackend.Begin sets blend to gl.ONE / gl.ONE_MINUS_SRC_ALPHA).
//
// - mode 0 (SolidRect)     : uniform color across the full quad
// - mode 1 (FillRounded)   : SDF sdRoundedBox, smoothstep 1-px AA
// - mode 2 (StrokeRect)    : abs(sdBox) - uStroke, smoothstep AA
// - mode 3 (StrokeRounded) : abs(sdRoundedBox) - uStroke, smoothstep AA
// - mode 4 (Line)          : SDF sdCapsule between two points encoded
//                            in uRect (see comment in the fragment
//                            source)
//
// # AA softness
//
// smoothstep window width is 1 physical pixel by default (a
// half-pixel each side of the ideal edge). Callers can bump uSoftness
// to widen the transition band for e.g. subtly-blurred silhouettes.

// shapeVertexSource — shared vertex shader for every shape draw.
// Position input is a fixed unit quad; uRect selects the destination
// rectangle in physical pixels; uFBSize is the FBO's physical size.
// vLocal carries the corner's (0..1) coord so the fragment shader can
// reconstruct pixel-space coordinates without a per-vertex output.
const shapeVertexSource = `#version 330 core
layout(location = 0) in vec2 aPos;
uniform vec4 uRect;
uniform vec2 uFBSize;
out vec2 vLocal;
out vec2 vPixel;
void main() {
    vLocal = aPos;
    vec2 pix = mix(uRect.xy, uRect.zw, aPos);
    vPixel = pix;
    vec2 ndc = (pix / uFBSize) * 2.0 - 1.0;
    ndc.y = -ndc.y;
    gl_Position = vec4(ndc, 0.0, 1.0);
}
`

// shapeFragmentSource — routes on uMode to the right SDF/fill path.
// Signed-distance helpers (sdBox, sdRoundedBox, sdCapsule) all follow
// the standard convention: negative inside, zero on the boundary,
// positive outside. Coverage is a smoothstep across a 1-px band
// centered on the ideal edge.
const shapeFragmentSource = `#version 330 core
in vec2 vLocal;
in vec2 vPixel;
out vec4 fragColor;

uniform vec4 uRect;      // (x0, y0, x1, y1) in pixels — quad bbox and SDF-reference rect for modes 0-3
uniform vec4 uLine;      // (ax, ay, bx, by) capsule endpoints for mode 4
uniform vec4 uColor;     // unpremul RGBA
uniform float uRadius;   // corner radius (px) for rounded modes
uniform float uStroke;   // half stroke width (px); >0 for stroke modes
uniform int   uMode;
uniform float uSoftness; // AA transition width in px (default 1.0)

// SDF: axis-aligned box centered at halfSize with p in local
// centered coords.
float sdBox(vec2 p, vec2 halfSize) {
    vec2 d = abs(p) - halfSize;
    return length(max(d, vec2(0.0))) + min(max(d.x, d.y), 0.0);
}

// SDF: rounded box. Same as sdBox on the shrunken halfSize+radius,
// then subtract the radius.
float sdRoundedBox(vec2 p, vec2 halfSize, float r) {
    vec2 d = abs(p) - halfSize + vec2(r);
    return length(max(d, vec2(0.0))) + min(max(d.x, d.y), 0.0) - r;
}

// SDF: line segment from a to b, evaluated at p.
float sdCapsule(vec2 p, vec2 a, vec2 b) {
    vec2 pa = p - a;
    vec2 ba = b - a;
    float h = clamp(dot(pa, ba) / max(dot(ba, ba), 1e-6), 0.0, 1.0);
    return length(pa - ba * h);
}

void main() {
    float soft = max(uSoftness, 0.0001);
    float coverage = 1.0;

    if (uMode == 0) {
        // Solid rect — full coverage; caller has already clipped the
        // quad to the intended pixel bounds. No SDF needed.
        coverage = 1.0;
    } else if (uMode == 1) {
        // Filled rounded rect.
        vec2 center = 0.5 * (uRect.xy + uRect.zw);
        vec2 halfSize = 0.5 * (uRect.zw - uRect.xy);
        float d = sdRoundedBox(vPixel - center, halfSize, uRadius);
        // 1-px band: inside (d<-0.5) full, outside (d>0.5) zero.
        coverage = 1.0 - smoothstep(-0.5 * soft, 0.5 * soft, d);
    } else if (uMode == 2) {
        // Stroked square-cornered rect.
        vec2 center = 0.5 * (uRect.xy + uRect.zw);
        vec2 halfSize = 0.5 * (uRect.zw - uRect.xy);
        float d = sdBox(vPixel - center, halfSize);
        float sd = abs(d) - uStroke;
        coverage = 1.0 - smoothstep(-0.5 * soft, 0.5 * soft, sd);
    } else if (uMode == 3) {
        // Stroked rounded rect.
        vec2 center = 0.5 * (uRect.xy + uRect.zw);
        vec2 halfSize = 0.5 * (uRect.zw - uRect.xy);
        float d = sdRoundedBox(vPixel - center, halfSize, uRadius);
        float sd = abs(d) - uStroke;
        coverage = 1.0 - smoothstep(-0.5 * soft, 0.5 * soft, sd);
    } else if (uMode == 4) {
        // Capsule (line with round caps). uLine carries the actual
        // endpoints; uRect is just the padded axis-aligned bbox used
        // by the vertex shader for the quad geometry. Half-width is
        // uStroke.
        vec2 a = uLine.xy;
        vec2 b = uLine.zw;
        float d = sdCapsule(vPixel, a, b) - uStroke;
        coverage = 1.0 - smoothstep(-0.5 * soft, 0.5 * soft, d);
    }

    float a = uColor.a * coverage;
    // Premultiplied output — FBO blend is ONE / ONE_MINUS_SRC_ALPHA.
    fragColor = vec4(uColor.rgb * a, a);
}
`

// ensureShapeProgram compiles the shape shader program the first time
// a GPU-native primitive is issued. Failure sets shapeFailed so
// subsequent calls fall through to the CPU path without retrying.
func (b *gpuBackend) ensureShapeProgram() bool {
	if b.shapeReady {
		return true
	}
	if b.shapeFailed {
		return false
	}
	prog, err := newProgram(shapeVertexSource, shapeFragmentSource)
	if err != nil {
		b.shapeFailed = true
		return false
	}
	// Static unit-quad VBO (six vertices — two triangles covering
	// [0,1]²). Vertex layout: one attribute (aPos, vec2).
	quad := []float32{
		0, 0,
		1, 0,
		1, 1,
		0, 0,
		1, 1,
		0, 1,
	}
	gl.GenVertexArrays(1, &b.shapeVAO)
	gl.GenBuffers(1, &b.shapeVBO)
	gl.BindVertexArray(b.shapeVAO)
	gl.BindBuffer(gl.ARRAY_BUFFER, b.shapeVBO)
	gl.BufferData(gl.ARRAY_BUFFER, len(quad)*4, gl.Ptr(quad), gl.STATIC_DRAW)
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointer(0, 2, gl.FLOAT, false, 2*4, gl.PtrOffset(0))
	gl.BindBuffer(gl.ARRAY_BUFFER, 0)
	gl.BindVertexArray(0)

	b.shapeProg = prog
	b.uShapeRect = gl.GetUniformLocation(prog, gl.Str("uRect\x00"))
	b.uShapeFBSize = gl.GetUniformLocation(prog, gl.Str("uFBSize\x00"))
	b.uShapeColor = gl.GetUniformLocation(prog, gl.Str("uColor\x00"))
	b.uShapeRadius = gl.GetUniformLocation(prog, gl.Str("uRadius\x00"))
	b.uShapeStroke = gl.GetUniformLocation(prog, gl.Str("uStroke\x00"))
	b.uShapeMode = gl.GetUniformLocation(prog, gl.Str("uMode\x00"))
	b.uShapeSoftness = gl.GetUniformLocation(prog, gl.Str("uSoftness\x00"))
	b.uShapeLine = gl.GetUniformLocation(prog, gl.Str("uLine\x00"))
	b.shapeReady = true
	return true
}

// bindShapeProgram binds the shape program + VAO and sets the frame-
// constant uFBSize uniform. Individual draw helpers then set the
// per-call uniforms and issue DrawArrays.
func (b *gpuBackend) bindShapeProgram() {
	gl.UseProgram(b.shapeProg)
	gl.BindVertexArray(b.shapeVAO)
	gl.Uniform2f(b.uShapeFBSize, b.fboSize.W, b.fboSize.H)
	gl.Uniform1f(b.uShapeSoftness, 1.0)
}

// emitShapeQuad issues one shape draw. Callers set mode-specific
// uniforms first (uRect / uColor / uRadius / uStroke / uMode). Kept
// as a helper so all shape draws use the same DrawArrays pattern.
func (b *gpuBackend) emitShapeQuad() {
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
}
