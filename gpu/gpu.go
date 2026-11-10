//go:build cgo && !js

// Package gpu is qui's WebGPU API: the process-wide device, textures,
// buffers, WGSL shader modules, render pipelines and passes. It is a thin
// re-export of the internal binding to Dawn (docs/rfc/0003-webgpu.zh.md);
// a widget gets a frame to draw into from qui.GPUCanvas, an app a whole
// window from Window.SetPreRender / SetPostRender.
//
// Every call must be made on the UI thread. Objects are released
// explicitly (Release / Destroy).
package gpu

import "github.com/qizhanchan/qui/internal/wgpu"

type (
	ShaderStage              = wgpu.ShaderStage
	BindingType              = wgpu.BindingType
	BindGroupLayoutEntry     = wgpu.BindGroupLayoutEntry
	PipelineLayout           = wgpu.PipelineLayout
	Surface                  = wgpu.Surface
	SharedMemory             = wgpu.SharedMemory
	Device                   = wgpu.Device
	TextureFormat            = wgpu.TextureFormat
	TextureUsage             = wgpu.TextureUsage
	BufferUsage              = wgpu.BufferUsage
	VertexFormat             = wgpu.VertexFormat
	VertexStepMode           = wgpu.VertexStepMode
	PrimitiveTopology        = wgpu.PrimitiveTopology
	CullMode                 = wgpu.CullMode
	FrontFace                = wgpu.FrontFace
	IndexFormat              = wgpu.IndexFormat
	CompareFunction          = wgpu.CompareFunction
	BlendFactor              = wgpu.BlendFactor
	BlendOperation           = wgpu.BlendOperation
	LoadOp                   = wgpu.LoadOp
	StoreOp                  = wgpu.StoreOp
	FilterMode               = wgpu.FilterMode
	AddressMode              = wgpu.AddressMode
	Texture                  = wgpu.Texture
	TextureDescriptor        = wgpu.TextureDescriptor
	TextureView              = wgpu.TextureView
	Buffer                   = wgpu.Buffer
	Sampler                  = wgpu.Sampler
	ShaderModule             = wgpu.ShaderModule
	VertexAttribute          = wgpu.VertexAttribute
	VertexBufferLayout       = wgpu.VertexBufferLayout
	BlendComponent           = wgpu.BlendComponent
	BlendState               = wgpu.BlendState
	ColorTarget              = wgpu.ColorTarget
	DepthStencil             = wgpu.DepthStencil
	RenderPipelineDescriptor = wgpu.RenderPipelineDescriptor
	RenderPipeline           = wgpu.RenderPipeline
	BindGroupLayout          = wgpu.BindGroupLayout
	BindGroupEntry           = wgpu.BindGroupEntry
	BindGroup                = wgpu.BindGroup
	CommandEncoder           = wgpu.CommandEncoder
	CommandBuffer            = wgpu.CommandBuffer
	Color                    = wgpu.Color
	ColorAttachment          = wgpu.ColorAttachment
	DepthAttachment          = wgpu.DepthAttachment
	RenderPassDescriptor     = wgpu.RenderPassDescriptor
	RenderPass               = wgpu.RenderPass
)

const (
	DawnRevision              = wgpu.DawnRevision
	DawnRelease               = wgpu.DawnRelease
	StageVertex               = wgpu.StageVertex
	StageFragment             = wgpu.StageFragment
	StageCompute              = wgpu.StageCompute
	BindUniformBuffer         = wgpu.BindUniformBuffer
	BindStorageBuffer         = wgpu.BindStorageBuffer
	BindReadOnlyStorageBuffer = wgpu.BindReadOnlyStorageBuffer
	BindTexture               = wgpu.BindTexture
	BindSampler               = wgpu.BindSampler
	BindUnfilterableTexture   = wgpu.BindUnfilterableTexture
	FormatUndefined           = wgpu.FormatUndefined
	FormatR8Unorm             = wgpu.FormatR8Unorm
	FormatRG8Unorm            = wgpu.FormatRG8Unorm
	FormatRGBA8Unorm          = wgpu.FormatRGBA8Unorm
	FormatRGBA8UnormSrgb      = wgpu.FormatRGBA8UnormSrgb
	FormatBGRA8Unorm          = wgpu.FormatBGRA8Unorm
	FormatBGRA8UnormSrgb      = wgpu.FormatBGRA8UnormSrgb
	FormatRGBA16Float         = wgpu.FormatRGBA16Float
	FormatR32Float            = wgpu.FormatR32Float
	FormatDepth24Plus         = wgpu.FormatDepth24Plus
	FormatDepth32Float        = wgpu.FormatDepth32Float
	FormatDepth24PlusStencil8 = wgpu.FormatDepth24PlusStencil8
	UsageCopySrc              = wgpu.UsageCopySrc
	UsageCopyDst              = wgpu.UsageCopyDst
	UsageTextureBinding       = wgpu.UsageTextureBinding
	UsageStorageBinding       = wgpu.UsageStorageBinding
	UsageRenderAttachment     = wgpu.UsageRenderAttachment
	BufferMapRead             = wgpu.BufferMapRead
	BufferMapWrite            = wgpu.BufferMapWrite
	BufferCopySrc             = wgpu.BufferCopySrc
	BufferCopyDst             = wgpu.BufferCopyDst
	BufferIndex               = wgpu.BufferIndex
	BufferVertex              = wgpu.BufferVertex
	BufferUniform             = wgpu.BufferUniform
	BufferStorage             = wgpu.BufferStorage
	VertexFloat32             = wgpu.VertexFloat32
	VertexFloat32x2           = wgpu.VertexFloat32x2
	VertexFloat32x3           = wgpu.VertexFloat32x3
	VertexFloat32x4           = wgpu.VertexFloat32x4
	VertexUint32              = wgpu.VertexUint32
	VertexUnorm8x4            = wgpu.VertexUnorm8x4
	StepVertex                = wgpu.StepVertex
	StepInstance              = wgpu.StepInstance
	TopologyTriangleList      = wgpu.TopologyTriangleList
	TopologyTriangleStrip     = wgpu.TopologyTriangleStrip
	TopologyLineList          = wgpu.TopologyLineList
	TopologyLineStrip         = wgpu.TopologyLineStrip
	TopologyPointList         = wgpu.TopologyPointList
	CullNone                  = wgpu.CullNone
	CullFront                 = wgpu.CullFront
	CullBack                  = wgpu.CullBack
	FrontCCW                  = wgpu.FrontCCW
	FrontCW                   = wgpu.FrontCW
	IndexUint16               = wgpu.IndexUint16
	IndexUint32               = wgpu.IndexUint32
	CompareNever              = wgpu.CompareNever
	CompareLess               = wgpu.CompareLess
	CompareLessEqual          = wgpu.CompareLessEqual
	CompareGreater            = wgpu.CompareGreater
	CompareGreaterEqual       = wgpu.CompareGreaterEqual
	CompareEqual              = wgpu.CompareEqual
	CompareAlways             = wgpu.CompareAlways
	BlendZero                 = wgpu.BlendZero
	BlendOne                  = wgpu.BlendOne
	BlendSrcAlpha             = wgpu.BlendSrcAlpha
	BlendOneMinusSrcAlpha     = wgpu.BlendOneMinusSrcAlpha
	BlendDstAlpha             = wgpu.BlendDstAlpha
	BlendOneMinusDstAlpha     = wgpu.BlendOneMinusDstAlpha
	BlendAdd                  = wgpu.BlendAdd
	BlendSubtract             = wgpu.BlendSubtract
	BlendMin                  = wgpu.BlendMin
	BlendMax                  = wgpu.BlendMax
	LoadOpLoad                = wgpu.LoadOpLoad
	LoadOpClear               = wgpu.LoadOpClear
	StoreOpStore              = wgpu.StoreOpStore
	StoreOpDiscard            = wgpu.StoreOpDiscard
	FilterNearest             = wgpu.FilterNearest
	FilterLinear              = wgpu.FilterLinear
	AddressClampToEdge        = wgpu.AddressClampToEdge
	AddressRepeat             = wgpu.AddressRepeat
	AddressMirror             = wgpu.AddressMirror
)

var (
	ErrUnavailable     = wgpu.ErrUnavailable
	BlendPremultiplied = wgpu.BlendPremultiplied
	BlendAlpha         = wgpu.BlendAlpha
)

// LibraryName is the Dawn shared library's file name on this OS.
func LibraryName() string { return wgpu.LibraryName() }

// CacheDir is where cmd/qui-dawn installs Dawn.
func CacheDir() string { return wgpu.CacheDir() }

// LibraryCandidates lists where Load looks for Dawn.
func LibraryCandidates() []string { return wgpu.LibraryCandidates() }

// Load opens the Dawn library (once) and reports why it could not.
func Load() error { return wgpu.Load() }

// LibraryPath is the Dawn library Load opened.
func LibraryPath() string { return wgpu.LibraryPath() }

// SetErrorHandler replaces the default handler (a log line) of
// uncaptured device errors.
func SetErrorHandler(fn func(kind int, msg string)) { wgpu.SetErrorHandler(fn) }

// Shared returns the process's WebGPU device, loading Dawn on first use.
func Shared() (*Device, error) { return wgpu.Shared() }
