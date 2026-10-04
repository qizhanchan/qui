package scene3d

import "github.com/qizhanchan/qui"

// Materials bind shader programs + uniforms for a draw call. The
// baseline set — Unlit + Lit — covers most design/creative-tool
// needs without pulling in full PBR (BRDF + IBL). Custom materials
// (PBR, subsurface, cel, etc.) plug in via CustomMaterial.
//
// Materials are lazy: their shader compiles on first Bind in the
// active GL context, so constructing a material outside a GL
// context is fine.

// Material binds its program + per-object uniforms before a Mesh.Draw.
// model is the node's world matrix; scene provides camera + lights.
type Material interface {
	Bind(scene *Scene, aspect float32, model Mat4)
	Destroy()
}

// ---- UnlitMaterial ----

// UnlitMaterial is a flat-colored material. No lighting, no shading —
// pure albedo (optionally multiplied by a texture). Cheapest option.
type UnlitMaterial struct {
	BaseColor qui.Color
	program   *ShaderProgram
}

// NewUnlitMaterial creates an unlit material with the given color.
func NewUnlitMaterial(c qui.Color) *UnlitMaterial {
	return &UnlitMaterial{BaseColor: c}
}

func (m *UnlitMaterial) Bind(scene *Scene, aspect float32, model Mat4) {
	if m.program == nil {
		p, err := NewShaderProgram(unlitVertexShader, unlitFragmentShader)
		if err != nil {
			return
		}
		m.program = p
	}
	m.program.Use()
	proj := scene.Camera.ProjectionMatrix(aspect)
	view := scene.Camera.ViewMatrix()
	m.program.SetMat4("uModel", &model)
	m.program.SetMat4("uView", &view)
	m.program.SetMat4("uProjection", &proj)
	m.program.SetColor("uColor", m.BaseColor)
}

func (m *UnlitMaterial) Destroy() {
	if m == nil {
		return
	}
	m.program.Destroy()
	m.program = nil
}

const unlitVertexShader = `#version 330 core
layout(location = 0) in vec3 aPos;
uniform mat4 uModel;
uniform mat4 uView;
uniform mat4 uProjection;
void main() {
    gl_Position = uProjection * uView * uModel * vec4(aPos, 1.0);
}
` + "\x00"

const unlitFragmentShader = `#version 330 core
uniform vec4 uColor;
out vec4 FragColor;
void main() {
    FragColor = uColor;
}
` + "\x00"

// ---- LitMaterial ----

// LitMaterial is a simple Lambert + half-Lambert material with one
// directional light + ambient. Sufficient for the common case
// "object in a scene under one sun". For PBR / image-based lighting,
// write a CustomMaterial.
type LitMaterial struct {
	BaseColor qui.Color
	Roughness float32 // used only as a weak specular modulation
	program   *ShaderProgram
}

// NewLitMaterial creates a Lambert-shaded material.
func NewLitMaterial(c qui.Color) *LitMaterial {
	return &LitMaterial{BaseColor: c, Roughness: 0.5}
}

func (m *LitMaterial) Bind(scene *Scene, aspect float32, model Mat4) {
	if m.program == nil {
		p, err := NewShaderProgram(litVertexShader, litFragmentShader)
		if err != nil {
			return
		}
		m.program = p
	}
	m.program.Use()
	proj := scene.Camera.ProjectionMatrix(aspect)
	view := scene.Camera.ViewMatrix()
	dir := scene.firstDirectional()
	m.program.SetMat4("uModel", &model)
	m.program.SetMat4("uView", &view)
	m.program.SetMat4("uProjection", &proj)
	m.program.SetColor("uBaseColor", m.BaseColor)
	m.program.SetColor("uAmbient", scene.Ambient)
	// Shader expects light-to-surface direction NEGATED (i.e. direction
	// *from* surface *to* light) for the standard N·L convention.
	lightDir := dir.Direction.Mul(-1).Normalize()
	m.program.SetVec3("uLightDir", lightDir)
	m.program.SetColor("uLightColor", dir.Color)
	m.program.SetFloat("uLightIntensity", dir.Intensity)
	m.program.SetVec3("uCameraPos", scene.Camera.Position)
	m.program.SetFloat("uRoughness", m.Roughness)
}

func (m *LitMaterial) Destroy() {
	if m == nil {
		return
	}
	m.program.Destroy()
	m.program = nil
}

const litVertexShader = `#version 330 core
layout(location = 0) in vec3 aPos;
layout(location = 1) in vec3 aNormal;
layout(location = 2) in vec2 aUV;
out vec3 vNormal;
out vec3 vWorldPos;
out vec2 vUV;
uniform mat4 uModel;
uniform mat4 uView;
uniform mat4 uProjection;
void main() {
    vec4 world = uModel * vec4(aPos, 1.0);
    vWorldPos = world.xyz;
    // Transpose(inverse(upper-left 3x3 of uModel)) — but the scene
    // graph forbids non-uniform scale for lit meshes in the first
    // cut, so we can treat uModel's upper-left as the normal matrix
    // directly. Good enough for uniform scales.
    vNormal = mat3(uModel) * aNormal;
    vUV = aUV;
    gl_Position = uProjection * uView * world;
}
` + "\x00"

const litFragmentShader = `#version 330 core
in vec3 vNormal;
in vec3 vWorldPos;
in vec2 vUV;
out vec4 FragColor;
uniform vec4 uBaseColor;
uniform vec4 uAmbient;
uniform vec3 uLightDir;       // direction from surface TOWARD light (unit)
uniform vec4 uLightColor;
uniform float uLightIntensity;
uniform vec3 uCameraPos;
uniform float uRoughness;
void main() {
    vec3 N = normalize(vNormal);
    vec3 L = normalize(uLightDir);
    vec3 V = normalize(uCameraPos - vWorldPos);
    vec3 H = normalize(L + V);
    float NdotL = max(dot(N, L), 0.0);
    float NdotH = max(dot(N, H), 0.0);
    // Weak specular — falls off with roughness. Not physically based;
    // just enough to suggest material.
    float shininess = mix(64.0, 4.0, uRoughness);
    float spec = pow(NdotH, shininess) * (1.0 - uRoughness) * 0.5;
    vec3 diffuse = uBaseColor.rgb * NdotL * uLightColor.rgb * uLightIntensity;
    vec3 ambient = uBaseColor.rgb * uAmbient.rgb;
    vec3 specular = uLightColor.rgb * spec * uLightIntensity;
    FragColor = vec4(ambient + diffuse + specular, uBaseColor.a);
}
` + "\x00"

// ---- CustomMaterial ----

// CustomMaterial wraps a user-provided ShaderProgram. The Bind
// callback lets callers set arbitrary uniforms before each draw.
// Useful for PBR, cel shading, or any effect the built-in materials
// don't cover.
type CustomMaterial struct {
	Program *ShaderProgram
	OnBind  func(p *ShaderProgram, scene *Scene, aspect float32, model Mat4)
}

func (m *CustomMaterial) Bind(scene *Scene, aspect float32, model Mat4) {
	if m.Program == nil {
		return
	}
	m.Program.Use()
	if m.OnBind != nil {
		m.OnBind(m.Program, scene, aspect, model)
	}
}

func (m *CustomMaterial) Destroy() {
	if m == nil {
		return
	}
	m.Program.Destroy()
	m.Program = nil
}
