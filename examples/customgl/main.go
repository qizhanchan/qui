package main

// Demonstrates Window.SetPreRender — a raw-OpenGL hook that lets
// the caller draw directly into the framebuffer before the widget
// tree paints on top. Here we render an animated full-screen shader
// background (Shadertoy-style), and 2D widgets render over it.
//
// The widget tree itself is regular — just a Label and Button that
// sit on top of the GPU-rendered background. The background shows
// through because the CPU-side 2D layer is transparent wherever no
// widget paints (blend func = SRC_ALPHA / ONE_MINUS_SRC_ALPHA).

import (
	"fmt"
	"log"
	"time"
	"unsafe"

	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

const vsSrc = `#version 330 core
out vec2 vUV;
void main() {
    vec2 quad[6] = vec2[6](
        vec2(-1,-1), vec2(1,-1), vec2(-1,1),
        vec2(1,-1), vec2(1,1), vec2(-1,1)
    );
    vec2 p = quad[gl_VertexID];
    vUV = p * 0.5 + 0.5;
    gl_Position = vec4(p, 0, 1);
}
` + "\x00"

const fsSrc = `#version 330 core
in vec2 vUV;
out vec4 FragColor;
uniform float uTime;
uniform vec2 uRes;
void main() {
    vec2 uv = (vUV * 2.0 - 1.0);
    uv.x *= uRes.x / uRes.y;
    float t = uTime * 0.35;
    float d = 0.0;
    for (int i = 0; i < 4; i++) {
        float fi = float(i);
        d += sin(uv.x * (3.0 + fi) + t + fi) * cos(uv.y * (3.0 + fi) + t);
    }
    vec3 col = 0.5 + 0.5 * cos(vec3(0,2,4) + d + t);
    col *= smoothstep(1.4, 0.2, length(uv));
    FragColor = vec4(col, 1.0);
}
` + "\x00"

var (
	program   uint32
	vao       uint32
	uTimeLoc  int32
	uResLoc   int32
	startTime = time.Now()
)

func initShader() {
	vs := compile(gl.VERTEX_SHADER, vsSrc)
	fs := compile(gl.FRAGMENT_SHADER, fsSrc)
	program = gl.CreateProgram()
	gl.AttachShader(program, vs)
	gl.AttachShader(program, fs)
	gl.LinkProgram(program)
	gl.DeleteShader(vs)
	gl.DeleteShader(fs)
	uTimeLoc = gl.GetUniformLocation(program, gl.Str("uTime\x00"))
	uResLoc = gl.GetUniformLocation(program, gl.Str("uRes\x00"))
	gl.GenVertexArrays(1, &vao)
}

func compile(kind uint32, src string) uint32 {
	s := gl.CreateShader(kind)
	csrc, free := gl.Strs(src)
	defer free()
	gl.ShaderSource(s, 1, csrc, nil)
	gl.CompileShader(s)
	var ok int32
	gl.GetShaderiv(s, gl.COMPILE_STATUS, &ok)
	if ok == 0 {
		var logLen int32
		gl.GetShaderiv(s, gl.INFO_LOG_LENGTH, &logLen)
		buf := make([]byte, logLen)
		gl.GetShaderInfoLog(s, logLen, nil, (*uint8)(unsafe.Pointer(&buf[0])))
		log.Fatalf("shader compile failed: %s", string(buf))
	}
	return s
}

func main() {
	app, err := qui.NewApp()
	if err != nil {
		log.Fatal(err)
	}
	window, err := app.NewWindow("qui — custom GL background", 800, 500)
	if err != nil {
		log.Fatal(err)
	}
	renderer := qui.NewGLRenderer()
	window.SetRenderer(renderer)

	var clicks int
	label := widgets.NewLabel("Shader-driven background. 2D UI paints on top.")
	label.Style().Foreground = qui.ColorWhite
	label.Style().Font.Size = 20
	label.Style().Padding = qui.Insets{Top: 10, Right: 16, Bottom: 10, Left: 16}
	label.Style().Background = qui.Color{R: 0, G: 0, B: 0, A: 0.4}
	label.Style().Radius = 6

	countLabel := widgets.NewLabel("clicks: 0")
	countLabel.Style().Foreground = qui.ColorWhite
	countLabel.Style().Font.Size = 16

	btn := widgets.NewButton("Tick up", func() {
		clicks++
		countLabel.SetText(fmt.Sprintf("clicks: %d", clicks))
		window.Invalidate()
	})

	root := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical, Gap: 16, AlignItems: qui.AlignCenter, Justify: qui.JustifyCenter}, label, countLabel, btn)
	window.SetRoot(root)

	// Drive continuous animation: invalidate every frame so the shader
	// re-renders. On a real app this would be gated on "shader is
	// dirty" but for a shadertoy background we want 60 fps.
	animator := tickEveryFrame{window: window}
	window.RegisterAnimator(&animator)

	// The pre-render hook paints under everything. First call lazily
	// compiles the shader (we need a current GL context, which we
	// have here since the window's Step has made its context current
	// before the hook fires).
	window.SetPreRender(func(state qui.GLState) {
		if program == 0 {
			initShader()
		}
		elapsed := float32(time.Since(startTime).Seconds())
		gl.UseProgram(program)
		gl.Uniform1f(uTimeLoc, elapsed)
		gl.Uniform2f(uResLoc, state.FramebufferSize.W, state.FramebufferSize.H)
		gl.BindVertexArray(vao)
		gl.Disable(gl.BLEND)
		gl.DrawArrays(gl.TRIANGLES, 0, 6)
		gl.BindVertexArray(0)
		gl.UseProgram(0)
	})

	app.Run()
}

// tickEveryFrame is an Animator that never finishes — it just
// asks the window to redraw every tick so the shader uniform
// advances in time.
type tickEveryFrame struct {
	window *qui.Window
}

func (t *tickEveryFrame) Tick(now time.Time) (qui.Rect, bool) {
	return t.window.Bounds(), false
}
func (t *tickEveryFrame) Stop() {}
