package scene3d

import (
	"fmt"

	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/qizhanchan/qui"
)

// ShaderProgram is a compiled + linked GL program with cached uniform
// locations. Build via NewShaderProgram; the returned object is
// valid only as long as its creating GL context is current.
//
// Usage pattern inside a widget/material:
//
//	if s.program == nil { s.program, _ = scene3d.NewShaderProgram(vsSrc, fsSrc) }
//	s.program.Use()
//	s.program.SetMat4("uProjection", &proj)
type ShaderProgram struct {
	ID       uint32
	uniforms map[string]int32
}

// NewShaderProgram compiles a vertex + fragment shader pair into a
// linked program. Both sources must be null-terminated (append "\x00").
func NewShaderProgram(vertexSrc, fragmentSrc string) (*ShaderProgram, error) {
	vs, err := qui.CompileShader(vertexSrc, gl.VERTEX_SHADER)
	if err != nil {
		return nil, fmt.Errorf("vertex shader: %w", err)
	}
	defer gl.DeleteShader(vs)
	fs, err := qui.CompileShader(fragmentSrc, gl.FRAGMENT_SHADER)
	if err != nil {
		return nil, fmt.Errorf("fragment shader: %w", err)
	}
	defer gl.DeleteShader(fs)

	prog := gl.CreateProgram()
	gl.AttachShader(prog, vs)
	gl.AttachShader(prog, fs)
	gl.LinkProgram(prog)
	var status int32
	gl.GetProgramiv(prog, gl.LINK_STATUS, &status)
	if status == 0 {
		var logLen int32
		gl.GetProgramiv(prog, gl.INFO_LOG_LENGTH, &logLen)
		logBuf := make([]byte, logLen+1)
		gl.GetProgramInfoLog(prog, logLen, nil, &logBuf[0])
		gl.DeleteProgram(prog)
		return nil, fmt.Errorf("program link: %s", string(logBuf))
	}
	return &ShaderProgram{ID: prog, uniforms: map[string]int32{}}, nil
}

// Use binds the program.
func (s *ShaderProgram) Use() {
	if s == nil {
		return
	}
	gl.UseProgram(s.ID)
}

// Destroy releases the GL program object. Must be called on a
// goroutine with the program's context current.
func (s *ShaderProgram) Destroy() {
	if s == nil || s.ID == 0 {
		return
	}
	gl.DeleteProgram(s.ID)
	s.ID = 0
}

// Loc returns (and caches) the uniform location by name.
func (s *ShaderProgram) Loc(name string) int32 {
	if s == nil {
		return -1
	}
	if loc, ok := s.uniforms[name]; ok {
		return loc
	}
	cName := name + "\x00"
	loc := gl.GetUniformLocation(s.ID, gl.Str(cName))
	s.uniforms[name] = loc
	return loc
}

// SetMat4 uploads a Mat4 uniform (column-major, no transpose).
func (s *ShaderProgram) SetMat4(name string, m *Mat4) {
	loc := s.Loc(name)
	if loc < 0 {
		return
	}
	gl.UniformMatrix4fv(loc, 1, false, &m[0])
}

func (s *ShaderProgram) SetVec3(name string, v Vec3) {
	loc := s.Loc(name)
	if loc < 0 {
		return
	}
	gl.Uniform3f(loc, v.X, v.Y, v.Z)
}

func (s *ShaderProgram) SetVec4(name string, v Vec4) {
	loc := s.Loc(name)
	if loc < 0 {
		return
	}
	gl.Uniform4f(loc, v.X, v.Y, v.Z, v.W)
}

func (s *ShaderProgram) SetColor(name string, c qui.Color) {
	s.SetVec4(name, Vec4{c.R, c.G, c.B, c.A})
}

func (s *ShaderProgram) SetFloat(name string, f float32) {
	loc := s.Loc(name)
	if loc < 0 {
		return
	}
	gl.Uniform1f(loc, f)
}

func (s *ShaderProgram) SetInt(name string, i int32) {
	loc := s.Loc(name)
	if loc < 0 {
		return
	}
	gl.Uniform1i(loc, i)
}
