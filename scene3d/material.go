package scene3d

import "github.com/qizhanchan/qui"

// Materials are WGSL shaders plus per-object parameters. The baseline
// set — Unlit + Lit — covers most design / creative-tool needs without
// pulling in full PBR (BRDF + IBL). Custom materials (PBR, cel, …) plug in
// through CustomMaterial with their own WGSL.
//
// Every material's shader is written against Preamble (prepended
// automatically), which declares the bindings the viewport fills:
//
//	struct Frame  { view, proj: mat4x4f; camera_pos, light_dir, light_color, ambient: vec4f }
//	struct Object { model: mat4x4f; color, params: vec4f; user: array<vec4f, 4> }
//	@group(0) @binding(0) var<uniform> frame: Frame;
//	@group(0) @binding(1) var<uniform> obj: Object;
//	fn clip_from_world(world: vec4f) -> vec4f
//
// and must define vs_main / fs_main. Meshes feed @location(0) position:
// vec3f, @location(1) normal: vec3f, @location(2) uv: vec2f (zero for
// line meshes). fs_main returns PREMULTIPLIED color: the pipeline blends
// one / one-minus-src-alpha. light_dir points
// from the surface TOWARD the light; light_color is premultiplied by the
// intensity. Shaders compile on the first frame that draws them.

// Material is a node's surface: its shader and per-object parameters.
type Material interface {
	// WGSL is the shader source (without Preamble). Materials that share
	// a source share one pipeline.
	WGSL() string
	// Params fills the per-object block (obj.color / params / user).
	Params(scene *Scene) MaterialParams
	// Destroy releases what the material owns (built-ins own nothing:
	// pipelines are cached per shader).
	Destroy()
}

// MaterialParams is a material's per-object data.
type MaterialParams struct {
	Color  qui.Color
	Params [4]float32
	User   [16]float32
}

// Preamble is the WGSL every material shader is compiled after.
const Preamble = `
struct Frame {
    view: mat4x4f,
    proj: mat4x4f,
    camera_pos: vec4f,
    light_dir: vec4f,
    light_color: vec4f,
    ambient: vec4f,
};
struct Object {
    model: mat4x4f,
    color: vec4f,
    params: vec4f,
    user: array<vec4f, 4>,
};
@group(0) @binding(0) var<uniform> frame: Frame;
@group(0) @binding(1) var<uniform> obj: Object;

// clip_from_world projects a world position. The camera matrices follow
// the OpenGL convention (depth -1..1); WebGPU's clip depth is 0..1.
fn clip_from_world(world: vec4f) -> vec4f {
    var p = frame.proj * frame.view * world;
    p.z = 0.5 * (p.z + p.w);
    return p;
}
`

// ---- UnlitMaterial ----

// UnlitMaterial is a flat-colored material. No lighting, no shading —
// pure albedo. Cheapest option; also the default for line meshes.
type UnlitMaterial struct {
	BaseColor qui.Color
}

// NewUnlitMaterial creates an unlit material with the given color.
func NewUnlitMaterial(c qui.Color) *UnlitMaterial {
	return &UnlitMaterial{BaseColor: c}
}

func (m *UnlitMaterial) WGSL() string { return unlitWGSL }

func (m *UnlitMaterial) Params(*Scene) MaterialParams { return MaterialParams{Color: m.BaseColor} }

func (m *UnlitMaterial) Destroy() {}

const unlitWGSL = `
@vertex fn vs_main(@location(0) position: vec3f) -> @builtin(position) vec4f {
    return clip_from_world(obj.model * vec4f(position, 1.0));
}
@fragment fn fs_main() -> @location(0) vec4f {
    return vec4f(obj.color.rgb * obj.color.a, obj.color.a);
}
`

// ---- LitMaterial ----

// LitMaterial is Blinn-Phong with one directional light (the scene's
// first) plus ambient — good enough for previews and editors.
type LitMaterial struct {
	BaseColor qui.Color
	Roughness float32 // used only as a weak specular modulation
}

// NewLitMaterial creates a lit material with the given base color.
func NewLitMaterial(c qui.Color) *LitMaterial {
	return &LitMaterial{BaseColor: c, Roughness: 0.5}
}

func (m *LitMaterial) WGSL() string { return litWGSL }

func (m *LitMaterial) Params(*Scene) MaterialParams {
	return MaterialParams{Color: m.BaseColor, Params: [4]float32{m.Roughness}}
}

func (m *LitMaterial) Destroy() {}

const litWGSL = `
struct VOut {
    @builtin(position) pos: vec4f,
    @location(0) normal: vec3f,
    @location(1) world: vec3f,
};
@vertex fn vs_main(@location(0) position: vec3f, @location(1) normal: vec3f,
                   @location(2) uv: vec2f) -> VOut {
    let world = obj.model * vec4f(position, 1.0);
    var o: VOut;
    o.pos = clip_from_world(world);
    o.normal = (obj.model * vec4f(normal, 0.0)).xyz;
    o.world = world.xyz;
    return o;
}
@fragment fn fs_main(in: VOut) -> @location(0) vec4f {
    let roughness = obj.params.x;
    let n = normalize(in.normal);
    let l = normalize(frame.light_dir.xyz);
    let v = normalize(frame.camera_pos.xyz - in.world);
    let h = normalize(l + v);
    let ndotl = max(dot(n, l), 0.0);
    let ndoth = max(dot(n, h), 0.0);
    let shininess = mix(64.0, 4.0, roughness);
    let spec = pow(ndoth, shininess) * (1.0 - roughness) * 0.5;
    let base = obj.color.rgb;
    let rgb = base * frame.ambient.rgb + base * ndotl * frame.light_color.rgb
            + frame.light_color.rgb * spec;
    return vec4f(rgb * obj.color.a, obj.color.a);
}
`

// ---- CustomMaterial ----

// CustomMaterial is a user shader: Source is WGSL written against
// Preamble (vs_main / fs_main), OnParams fills its per-object block.
type CustomMaterial struct {
	Source   string
	OnParams func(scene *Scene) MaterialParams
}

func (m *CustomMaterial) WGSL() string { return m.Source }

func (m *CustomMaterial) Params(scene *Scene) MaterialParams {
	if m.OnParams == nil {
		return MaterialParams{Color: qui.Color{R: 1, G: 1, B: 1, A: 1}}
	}
	return m.OnParams(scene)
}

func (m *CustomMaterial) Destroy() {}
