//go:build cgo && !js

package wgpu

/*
#include <stdlib.h>
#include <string.h>
#include "helpers.h"

static WGPUStringView quiStr(const char* s, size_t n) { WGPUStringView v = {s, n}; return v; }
*/
import "C"

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"
)

// arena holds C memory for one call's descriptors (cgo forbids passing Go
// memory that itself holds pointers).
type arena struct{ ptrs []unsafe.Pointer }

func (a *arena) alloc(n uintptr) unsafe.Pointer {
	if n == 0 {
		n = 1
	}
	p := C.calloc(1, C.size_t(n))
	a.ptrs = append(a.ptrs, p)
	return p
}

func (a *arena) str(s string) C.WGPUStringView {
	if s == "" {
		return C.quiStr(nil, 0)
	}
	p := a.alloc(uintptr(len(s)))
	C.memcpy(p, unsafe.Pointer(unsafe.StringData(s)), C.size_t(len(s)))
	return C.quiStr((*C.char)(p), C.size_t(len(s)))
}

func (a *arena) free() {
	for _, p := range a.ptrs {
		C.free(p)
	}
	a.ptrs = nil
}

func allocN[T any](a *arena, n int) []T {
	if n == 0 {
		return nil
	}
	var z T
	p := a.alloc(unsafe.Sizeof(z) * uintptr(n))
	return unsafe.Slice((*T)(p), n)
}

func cbool(b bool) C.WGPUBool {
	if b {
		return 1
	}
	return 0
}

// Device is the process-wide WebGPU device (Shared).
type Device struct {
	inst    C.WGPUInstance
	adapter C.WGPUAdapter
	c       C.WGPUDevice
	queue   C.WGPUQueue
	name    string
	// ioSurface reports that IOSurfaces can be imported (macOS).
	ioSurface bool
}

var (
	sharedOnce sync.Once
	shared     *Device
	sharedErr  error
)

// Shared returns the process's device, creating it on first use (which
// loads Dawn). Every window, offscreen target and GPU widget uses it.
func Shared() (*Device, error) {
	sharedOnce.Do(func() {
		if err := Load(); err != nil {
			sharedErr = err
			return
		}
		shared, sharedErr = newDevice()
	})
	return shared, sharedErr
}

func newDevice() (*Device, error) {
	inst := C.quiWGPUCreateInstance()
	if inst == nil {
		return nil, errors.New("wgpu: cannot create instance")
	}
	var buf [512]C.char
	adapter := C.quiWGPURequestAdapter(inst, nil, 1, &buf[0], C.size_t(len(buf)))
	if adapter == nil {
		C.wgpuInstanceRelease(inst)
		return nil, fmt.Errorf("wgpu: %s", C.GoString(&buf[0]))
	}
	var want []C.WGPUFeatureName
	io := false
	if C.wgpuAdapterHasFeature(adapter, C.WGPUFeatureName_SharedTextureMemoryIOSurface) != 0 &&
		C.wgpuAdapterHasFeature(adapter, C.WGPUFeatureName_SharedFenceMTLSharedEvent) != 0 {
		want = append(want, C.WGPUFeatureName_SharedTextureMemoryIOSurface, C.WGPUFeatureName_SharedFenceMTLSharedEvent)
		io = true
	}
	var a arena
	defer a.free()
	feats := allocN[C.WGPUFeatureName](&a, len(want))
	copy(feats, want)
	var fp *C.WGPUFeatureName
	if len(feats) > 0 {
		fp = &feats[0]
	}
	dev := C.quiWGPURequestDevice(inst, adapter, fp, C.size_t(len(want)), &buf[0], C.size_t(len(buf)))
	if dev == nil {
		C.wgpuAdapterRelease(adapter)
		C.wgpuInstanceRelease(inst)
		return nil, fmt.Errorf("wgpu: %s", C.GoString(&buf[0]))
	}
	C.quiWGPUAdapterName(adapter, &buf[0], C.size_t(len(buf)))
	return &Device{
		inst: inst, adapter: adapter, c: dev, queue: C.wgpuDeviceGetQueue(dev),
		name: C.GoString(&buf[0]), ioSurface: io,
	}, nil
}

// AdapterName describes the GPU and backend, e.g. "Apple M3 (Metal)".
func (d *Device) AdapterName() string { return d.name }

// CanImportIOSurface reports whether ImportIOSurface works (macOS).
func (d *Device) CanImportIOSurface() bool { return d.ioSurface }

// Handle is the raw WGPUDevice, for code that calls webgpu.h directly.
func (d *Device) Handle() unsafe.Pointer { return unsafe.Pointer(d.c) }

// Tick processes pending callbacks and device work.
func (d *Device) Tick() { C.wgpuInstanceProcessEvents(d.inst) }

// ---- enums ----

type TextureFormat uint32

const (
	FormatUndefined           TextureFormat = C.WGPUTextureFormat_Undefined
	FormatR8Unorm             TextureFormat = C.WGPUTextureFormat_R8Unorm
	FormatRG8Unorm            TextureFormat = C.WGPUTextureFormat_RG8Unorm
	FormatRGBA8Unorm          TextureFormat = C.WGPUTextureFormat_RGBA8Unorm
	FormatRGBA8UnormSrgb      TextureFormat = C.WGPUTextureFormat_RGBA8UnormSrgb
	FormatBGRA8Unorm          TextureFormat = C.WGPUTextureFormat_BGRA8Unorm
	FormatBGRA8UnormSrgb      TextureFormat = C.WGPUTextureFormat_BGRA8UnormSrgb
	FormatRGBA16Float         TextureFormat = C.WGPUTextureFormat_RGBA16Float
	FormatR32Float            TextureFormat = C.WGPUTextureFormat_R32Float
	FormatDepth24Plus         TextureFormat = C.WGPUTextureFormat_Depth24Plus
	FormatDepth32Float        TextureFormat = C.WGPUTextureFormat_Depth32Float
	FormatDepth24PlusStencil8 TextureFormat = C.WGPUTextureFormat_Depth24PlusStencil8
)

type TextureUsage uint64

const (
	UsageCopySrc          TextureUsage = C.WGPUTextureUsage_CopySrc
	UsageCopyDst          TextureUsage = C.WGPUTextureUsage_CopyDst
	UsageTextureBinding   TextureUsage = C.WGPUTextureUsage_TextureBinding
	UsageStorageBinding   TextureUsage = C.WGPUTextureUsage_StorageBinding
	UsageRenderAttachment TextureUsage = C.WGPUTextureUsage_RenderAttachment
)

type BufferUsage uint64

const (
	BufferMapRead  BufferUsage = C.WGPUBufferUsage_MapRead
	BufferMapWrite BufferUsage = C.WGPUBufferUsage_MapWrite
	BufferCopySrc  BufferUsage = C.WGPUBufferUsage_CopySrc
	BufferCopyDst  BufferUsage = C.WGPUBufferUsage_CopyDst
	BufferIndex    BufferUsage = C.WGPUBufferUsage_Index
	BufferVertex   BufferUsage = C.WGPUBufferUsage_Vertex
	BufferUniform  BufferUsage = C.WGPUBufferUsage_Uniform
	BufferStorage  BufferUsage = C.WGPUBufferUsage_Storage
)

type VertexFormat uint32

const (
	VertexFloat32   VertexFormat = C.WGPUVertexFormat_Float32
	VertexFloat32x2 VertexFormat = C.WGPUVertexFormat_Float32x2
	VertexFloat32x3 VertexFormat = C.WGPUVertexFormat_Float32x3
	VertexFloat32x4 VertexFormat = C.WGPUVertexFormat_Float32x4
	VertexUint32    VertexFormat = C.WGPUVertexFormat_Uint32
	VertexUnorm8x4  VertexFormat = C.WGPUVertexFormat_Unorm8x4
)

type VertexStepMode uint32

const (
	StepVertex   VertexStepMode = C.WGPUVertexStepMode_Vertex
	StepInstance VertexStepMode = C.WGPUVertexStepMode_Instance
)

type PrimitiveTopology uint32

const (
	TopologyTriangleList  PrimitiveTopology = C.WGPUPrimitiveTopology_TriangleList
	TopologyTriangleStrip PrimitiveTopology = C.WGPUPrimitiveTopology_TriangleStrip
	TopologyLineList      PrimitiveTopology = C.WGPUPrimitiveTopology_LineList
	TopologyLineStrip     PrimitiveTopology = C.WGPUPrimitiveTopology_LineStrip
	TopologyPointList     PrimitiveTopology = C.WGPUPrimitiveTopology_PointList
)

type CullMode uint32

const (
	CullNone  CullMode = C.WGPUCullMode_None
	CullFront CullMode = C.WGPUCullMode_Front
	CullBack  CullMode = C.WGPUCullMode_Back
)

type FrontFace uint32

const (
	FrontCCW FrontFace = C.WGPUFrontFace_CCW
	FrontCW  FrontFace = C.WGPUFrontFace_CW
)

type IndexFormat uint32

const (
	IndexUint16 IndexFormat = C.WGPUIndexFormat_Uint16
	IndexUint32 IndexFormat = C.WGPUIndexFormat_Uint32
)

type CompareFunction uint32

const (
	CompareNever        CompareFunction = C.WGPUCompareFunction_Never
	CompareLess         CompareFunction = C.WGPUCompareFunction_Less
	CompareLessEqual    CompareFunction = C.WGPUCompareFunction_LessEqual
	CompareGreater      CompareFunction = C.WGPUCompareFunction_Greater
	CompareGreaterEqual CompareFunction = C.WGPUCompareFunction_GreaterEqual
	CompareEqual        CompareFunction = C.WGPUCompareFunction_Equal
	CompareAlways       CompareFunction = C.WGPUCompareFunction_Always
)

type BlendFactor uint32

const (
	BlendZero             BlendFactor = C.WGPUBlendFactor_Zero
	BlendOne              BlendFactor = C.WGPUBlendFactor_One
	BlendSrcAlpha         BlendFactor = C.WGPUBlendFactor_SrcAlpha
	BlendOneMinusSrcAlpha BlendFactor = C.WGPUBlendFactor_OneMinusSrcAlpha
	BlendDstAlpha         BlendFactor = C.WGPUBlendFactor_DstAlpha
	BlendOneMinusDstAlpha BlendFactor = C.WGPUBlendFactor_OneMinusDstAlpha
)

type BlendOperation uint32

const (
	BlendAdd      BlendOperation = C.WGPUBlendOperation_Add
	BlendSubtract BlendOperation = C.WGPUBlendOperation_Subtract
	BlendMin      BlendOperation = C.WGPUBlendOperation_Min
	BlendMax      BlendOperation = C.WGPUBlendOperation_Max
)

type LoadOp uint32

const (
	LoadOpLoad  LoadOp = C.WGPULoadOp_Load
	LoadOpClear LoadOp = C.WGPULoadOp_Clear
)

type StoreOp uint32

const (
	StoreOpStore   StoreOp = C.WGPUStoreOp_Store
	StoreOpDiscard StoreOp = C.WGPUStoreOp_Discard
)

type FilterMode uint32

const (
	FilterNearest FilterMode = C.WGPUFilterMode_Nearest
	FilterLinear  FilterMode = C.WGPUFilterMode_Linear
)

type AddressMode uint32

const (
	AddressClampToEdge AddressMode = C.WGPUAddressMode_ClampToEdge
	AddressRepeat      AddressMode = C.WGPUAddressMode_Repeat
	AddressMirror      AddressMode = C.WGPUAddressMode_MirrorRepeat
)

// BlendPremultiplied is source-over for premultiplied colors — what qui's
// 2D layer, video frames and CAMetalLayer compositing all use.
var BlendPremultiplied = &BlendState{
	Color: BlendComponent{Op: BlendAdd, Src: BlendOne, Dst: BlendOneMinusSrcAlpha},
	Alpha: BlendComponent{Op: BlendAdd, Src: BlendOne, Dst: BlendOneMinusSrcAlpha},
}

// BlendAlpha is source-over for straight (non-premultiplied) colors.
var BlendAlpha = &BlendState{
	Color: BlendComponent{Op: BlendAdd, Src: BlendSrcAlpha, Dst: BlendOneMinusSrcAlpha},
	Alpha: BlendComponent{Op: BlendAdd, Src: BlendOne, Dst: BlendOneMinusSrcAlpha},
}

// ---- textures ----

type Texture struct {
	c      C.WGPUTexture
	w, h   int
	format TextureFormat
}

type TextureDescriptor struct {
	Label         string
	Width, Height int
	Format        TextureFormat
	Usage         TextureUsage
	SampleCount   int // 0 = 1
}

func (d *Device) CreateTexture(desc TextureDescriptor) *Texture {
	var a arena
	defer a.free()
	td := (*C.WGPUTextureDescriptor)(a.alloc(C.sizeof_WGPUTextureDescriptor))
	td.label = a.str(desc.Label)
	td.usage = C.WGPUTextureUsage(desc.Usage)
	td.dimension = C.WGPUTextureDimension_2D
	td.size = C.WGPUExtent3D{C.uint32_t(desc.Width), C.uint32_t(desc.Height), 1}
	td.format = C.WGPUTextureFormat(desc.Format)
	td.mipLevelCount = 1
	td.sampleCount = 1
	if desc.SampleCount > 1 {
		td.sampleCount = C.uint32_t(desc.SampleCount)
	}
	t := C.wgpuDeviceCreateTexture(d.c, td)
	if t == nil {
		return nil
	}
	return &Texture{c: t, w: desc.Width, h: desc.Height, format: desc.Format}
}

func (t *Texture) Width() int            { return t.w }
func (t *Texture) Height() int           { return t.h }
func (t *Texture) Format() TextureFormat { return t.format }

// CreateView makes the default view of the texture.
func (t *Texture) CreateView() *TextureView {
	return &TextureView{c: C.wgpuTextureCreateView(t.c, nil), tex: t}
}

// Release drops the reference; Destroy also frees the GPU memory now.
func (t *Texture) Release() {
	if t != nil && t.c != nil {
		C.wgpuTextureRelease(t.c)
		t.c = nil
	}
}

func (t *Texture) Destroy() {
	if t != nil && t.c != nil {
		C.wgpuTextureDestroy(t.c)
		C.wgpuTextureRelease(t.c)
		t.c = nil
	}
}

type TextureView struct {
	c   C.WGPUTextureView
	tex *Texture
}

// Texture is the texture the view was made from (nil for surface views
// created elsewhere).
func (v *TextureView) Texture() *Texture { return v.tex }

func (v *TextureView) Release() {
	if v != nil && v.c != nil {
		C.wgpuTextureViewRelease(v.c)
		v.c = nil
	}
}

// ---- buffers ----

type Buffer struct {
	c    C.WGPUBuffer
	size uint64
	dev  *Device
}

func (d *Device) CreateBuffer(label string, usage BufferUsage, size uint64) *Buffer {
	var a arena
	defer a.free()
	bd := (*C.WGPUBufferDescriptor)(a.alloc(C.sizeof_WGPUBufferDescriptor))
	bd.label = a.str(label)
	bd.usage = C.WGPUBufferUsage(usage)
	bd.size = C.uint64_t((size + 3) &^ 3)
	b := C.wgpuDeviceCreateBuffer(d.c, bd)
	if b == nil {
		return nil
	}
	return &Buffer{c: b, size: uint64(bd.size), dev: d}
}

// CreateBufferInit creates a buffer holding data (CopyDst is added).
func (d *Device) CreateBufferInit(label string, usage BufferUsage, data []byte) *Buffer {
	b := d.CreateBuffer(label, usage|BufferCopyDst, uint64(len(data)))
	if b != nil && len(data) > 0 {
		d.WriteBuffer(b, 0, data)
	}
	return b
}

func (b *Buffer) Size() uint64 { return b.size }

func (b *Buffer) Release() {
	if b != nil && b.c != nil {
		C.wgpuBufferDestroy(b.c)
		C.wgpuBufferRelease(b.c)
		b.c = nil
	}
}

// ---- queue ----

// WriteBuffer schedules data to be copied into b at off (len and off
// multiples of 4; a shorter tail is padded).
func (d *Device) WriteBuffer(b *Buffer, off uint64, data []byte) {
	if len(data) == 0 {
		return
	}
	if len(data)%4 != 0 {
		pad := make([]byte, (len(data)+3)&^3)
		copy(pad, data)
		data = pad
	}
	C.wgpuQueueWriteBuffer(d.queue, b.c, C.uint64_t(off), unsafe.Pointer(&data[0]), C.size_t(len(data)))
}

// WriteTexture uploads w×h texels at (x, y) from pix, whose rows are
// stride bytes apart.
func (d *Device) WriteTexture(t *Texture, x, y, w, h int, pix unsafe.Pointer, stride int) {
	if w <= 0 || h <= 0 || pix == nil {
		return
	}
	dst := C.WGPUTexelCopyTextureInfo{texture: t.c, origin: C.WGPUOrigin3D{C.uint32_t(x), C.uint32_t(y), 0}, aspect: C.WGPUTextureAspect_All}
	layout := C.WGPUTexelCopyBufferLayout{bytesPerRow: C.uint32_t(stride), rowsPerImage: C.uint32_t(h)}
	size := C.WGPUExtent3D{C.uint32_t(w), C.uint32_t(h), 1}
	n := stride*(h-1) + w*bytesPerTexel(t.format)
	C.wgpuQueueWriteTexture(d.queue, &dst, pix, C.size_t(n), &layout, &size)
}

func bytesPerTexel(f TextureFormat) int {
	switch f {
	case FormatR8Unorm:
		return 1
	case FormatRG8Unorm:
		return 2
	case FormatRGBA16Float:
		return 8
	}
	return 4
}

// Submit runs the command buffers, in order.
func (d *Device) Submit(cbs ...*CommandBuffer) {
	if len(cbs) == 0 {
		return
	}
	var a arena
	defer a.free()
	arr := allocN[C.WGPUCommandBuffer](&a, len(cbs))
	for i, cb := range cbs {
		arr[i] = cb.c
	}
	C.wgpuQueueSubmit(d.queue, C.size_t(len(cbs)), &arr[0])
	for _, cb := range cbs {
		C.wgpuCommandBufferRelease(cb.c)
		cb.c = nil
	}
}

// WaitIdle blocks until all submitted work has completed.
func (d *Device) WaitIdle() bool { return C.quiWGPUWaitQueue(d.inst, d.queue) != 0 }

// ReadTexture copies an RGBA8/BGRA8 texture's (x, y, w, h) part back to
// memory, tightly packed rows, waiting for the GPU.
func (d *Device) ReadTexture(t *Texture, x, y, w, h int) ([]byte, error) {
	if w <= 0 || h <= 0 {
		return nil, nil
	}
	bpp := bytesPerTexel(t.format)
	row := (w*bpp + 255) &^ 255
	size := uint64(row * h)
	buf := d.CreateBuffer("qui readback", BufferMapRead|BufferCopyDst, size)
	if buf == nil {
		return nil, errors.New("wgpu: readback buffer")
	}
	defer buf.Release()
	enc := d.CreateCommandEncoder()
	src := C.WGPUTexelCopyTextureInfo{texture: t.c, origin: C.WGPUOrigin3D{C.uint32_t(x), C.uint32_t(y), 0}, aspect: C.WGPUTextureAspect_All}
	dst := C.WGPUTexelCopyBufferInfo{buffer: buf.c}
	dst.layout = C.WGPUTexelCopyBufferLayout{bytesPerRow: C.uint32_t(row), rowsPerImage: C.uint32_t(h)}
	ext := C.WGPUExtent3D{C.uint32_t(w), C.uint32_t(h), 1}
	C.wgpuCommandEncoderCopyTextureToBuffer(enc.c, &src, &dst, &ext)
	d.Submit(enc.Finish())
	if C.quiWGPUMapRead(d.inst, buf.c, 0, C.size_t(size)) == 0 {
		return nil, errors.New("wgpu: map failed")
	}
	p := C.wgpuBufferGetConstMappedRange(buf.c, 0, C.size_t(size))
	if p == nil {
		C.wgpuBufferUnmap(buf.c)
		return nil, errors.New("wgpu: mapped range")
	}
	src8 := unsafe.Slice((*byte)(p), size)
	out := make([]byte, w*bpp*h)
	for j := 0; j < h; j++ {
		copy(out[j*w*bpp:(j+1)*w*bpp], src8[j*row:])
	}
	C.wgpuBufferUnmap(buf.c)
	return out, nil
}

// ---- samplers, shaders, pipelines, bind groups ----

type Sampler struct{ c C.WGPUSampler }

func (d *Device) CreateSampler(filter FilterMode, address AddressMode) *Sampler {
	sd := C.WGPUSamplerDescriptor{
		addressModeU: C.WGPUAddressMode(address), addressModeV: C.WGPUAddressMode(address),
		addressModeW: C.WGPUAddressMode(address),
		magFilter:    C.WGPUFilterMode(filter), minFilter: C.WGPUFilterMode(filter),
		mipmapFilter: C.WGPUMipmapFilterMode_Nearest,
		lodMaxClamp:  32, maxAnisotropy: 1,
	}
	return &Sampler{c: C.wgpuDeviceCreateSampler(d.c, &sd)}
}

func (s *Sampler) Release() {
	if s != nil && s.c != nil {
		C.wgpuSamplerRelease(s.c)
		s.c = nil
	}
}

type ShaderModule struct{ c C.WGPUShaderModule }

// CreateShaderModule compiles WGSL source, returning the compiler's
// message on error.
func (d *Device) CreateShaderModule(label, wgsl string) (*ShaderModule, error) {
	var a arena
	defer a.free()
	src := (*C.WGPUShaderSourceWGSL)(a.alloc(C.sizeof_WGPUShaderSourceWGSL))
	src.chain.sType = C.WGPUSType_ShaderSourceWGSL
	src.code = a.str(wgsl)
	md := (*C.WGPUShaderModuleDescriptor)(a.alloc(C.sizeof_WGPUShaderModuleDescriptor))
	md.nextInChain = &src.chain
	md.label = a.str(label)
	C.wgpuDevicePushErrorScope(d.c, C.WGPUErrorFilter_Validation)
	m := C.wgpuDeviceCreateShaderModule(d.c, md)
	if err := d.popScope(label); err != nil {
		if m != nil {
			C.wgpuShaderModuleRelease(m)
		}
		return nil, err
	}
	return &ShaderModule{c: m}, nil
}

func (d *Device) popScope(what string) error {
	var buf [2048]C.char
	if C.quiWGPUPopErrorScope(d.inst, d.c, &buf[0], C.size_t(len(buf))) == 0 {
		return fmt.Errorf("wgpu: %s: %s", what, C.GoString(&buf[0]))
	}
	return nil
}

func (m *ShaderModule) Release() {
	if m != nil && m.c != nil {
		C.wgpuShaderModuleRelease(m.c)
		m.c = nil
	}
}

type VertexAttribute struct {
	Format   VertexFormat
	Offset   uint64
	Location uint32
}

type VertexBufferLayout struct {
	Stride     uint64
	StepMode   VertexStepMode // zero: per vertex
	Attributes []VertexAttribute
}

type BlendComponent struct {
	Op       BlendOperation
	Src, Dst BlendFactor
}

type BlendState struct{ Color, Alpha BlendComponent }

type ColorTarget struct {
	Format TextureFormat
	Blend  *BlendState // nil: replace
}

type DepthStencil struct {
	Format     TextureFormat
	DepthWrite bool
	Compare    CompareFunction
	// DepthBias / SlopeScale offset depth (line overlays on surfaces).
	DepthBias      int32
	DepthBiasSlope float32
}

type RenderPipelineDescriptor struct {
	Label string
	// Layout is the explicit pipeline layout; nil derives one from the
	// shader (bind groups must then be made from BindGroupLayout(i)).
	Layout        *PipelineLayout
	Module        *ShaderModule
	VertexEntry   string // default "vs_main"
	FragmentEntry string // default "fs_main"
	Buffers       []VertexBufferLayout
	Topology      PrimitiveTopology // zero: triangle list
	StripIndex    IndexFormat       // for strip topologies with an index buffer
	Cull          CullMode
	FrontFace     FrontFace
	DepthStencil  *DepthStencil
	SampleCount   int
	Targets       []ColorTarget
}

type RenderPipeline struct{ c C.WGPURenderPipeline }

func (d *Device) CreateRenderPipeline(desc RenderPipelineDescriptor) (*RenderPipeline, error) {
	var a arena
	defer a.free()
	pd := (*C.WGPURenderPipelineDescriptor)(a.alloc(C.sizeof_WGPURenderPipelineDescriptor))
	pd.label = a.str(desc.Label)
	if desc.Layout != nil {
		pd.layout = desc.Layout.c
	}
	ve, fe := desc.VertexEntry, desc.FragmentEntry
	if ve == "" {
		ve = "vs_main"
	}
	if fe == "" {
		fe = "fs_main"
	}
	pd.vertex.module = desc.Module.c
	pd.vertex.entryPoint = a.str(ve)
	if n := len(desc.Buffers); n > 0 {
		bufs := allocN[C.WGPUVertexBufferLayout](&a, n)
		for i, b := range desc.Buffers {
			bufs[i].arrayStride = C.uint64_t(b.Stride)
			bufs[i].stepMode = C.WGPUVertexStepMode_Vertex
			if b.StepMode != 0 {
				bufs[i].stepMode = C.WGPUVertexStepMode(b.StepMode)
			}
			attrs := allocN[C.WGPUVertexAttribute](&a, len(b.Attributes))
			for j, at := range b.Attributes {
				attrs[j].format = C.WGPUVertexFormat(at.Format)
				attrs[j].offset = C.uint64_t(at.Offset)
				attrs[j].shaderLocation = C.uint32_t(at.Location)
			}
			bufs[i].attributeCount = C.size_t(len(attrs))
			if len(attrs) > 0 {
				bufs[i].attributes = &attrs[0]
			}
		}
		pd.vertex.bufferCount = C.size_t(n)
		pd.vertex.buffers = &bufs[0]
	}
	pd.primitive.topology = C.WGPUPrimitiveTopology_TriangleList
	if desc.Topology != 0 {
		pd.primitive.topology = C.WGPUPrimitiveTopology(desc.Topology)
	}
	pd.primitive.stripIndexFormat = C.WGPUIndexFormat(desc.StripIndex)
	pd.primitive.cullMode = C.WGPUCullMode_None
	if desc.Cull != 0 {
		pd.primitive.cullMode = C.WGPUCullMode(desc.Cull)
	}
	pd.primitive.frontFace = C.WGPUFrontFace_CCW
	if desc.FrontFace != 0 {
		pd.primitive.frontFace = C.WGPUFrontFace(desc.FrontFace)
	}
	if ds := desc.DepthStencil; ds != nil {
		cds := (*C.WGPUDepthStencilState)(a.alloc(C.sizeof_WGPUDepthStencilState))
		cds.format = C.WGPUTextureFormat(ds.Format)
		cds.depthWriteEnabled = C.WGPUOptionalBool_False
		if ds.DepthWrite {
			cds.depthWriteEnabled = C.WGPUOptionalBool_True
		}
		cds.depthCompare = C.WGPUCompareFunction(ds.Compare)
		if ds.Compare == 0 {
			cds.depthCompare = C.WGPUCompareFunction_Less
		}
		for _, f := range []*C.WGPUStencilFaceState{&cds.stencilFront, &cds.stencilBack} {
			f.compare = C.WGPUCompareFunction_Always
			f.failOp = C.WGPUStencilOperation_Keep
			f.depthFailOp = C.WGPUStencilOperation_Keep
			f.passOp = C.WGPUStencilOperation_Keep
		}
		cds.stencilReadMask = 0xFFFFFFFF
		cds.stencilWriteMask = 0xFFFFFFFF
		cds.depthBias = C.int32_t(ds.DepthBias)
		cds.depthBiasSlopeScale = C.float(ds.DepthBiasSlope)
		pd.depthStencil = cds
	}
	pd.multisample.count = 1
	if desc.SampleCount > 1 {
		pd.multisample.count = C.uint32_t(desc.SampleCount)
	}
	pd.multisample.mask = 0xFFFFFFFF
	if len(desc.Targets) > 0 {
		fs := (*C.WGPUFragmentState)(a.alloc(C.sizeof_WGPUFragmentState))
		fs.module = desc.Module.c
		fs.entryPoint = a.str(fe)
		ts := allocN[C.WGPUColorTargetState](&a, len(desc.Targets))
		for i, t := range desc.Targets {
			ts[i].format = C.WGPUTextureFormat(t.Format)
			ts[i].writeMask = C.WGPUColorWriteMask_All
			if t.Blend != nil {
				bs := (*C.WGPUBlendState)(a.alloc(C.sizeof_WGPUBlendState))
				bs.color = C.WGPUBlendComponent{C.WGPUBlendOperation(t.Blend.Color.Op), C.WGPUBlendFactor(t.Blend.Color.Src), C.WGPUBlendFactor(t.Blend.Color.Dst)}
				bs.alpha = C.WGPUBlendComponent{C.WGPUBlendOperation(t.Blend.Alpha.Op), C.WGPUBlendFactor(t.Blend.Alpha.Src), C.WGPUBlendFactor(t.Blend.Alpha.Dst)}
				ts[i].blend = bs
			}
		}
		fs.targetCount = C.size_t(len(ts))
		fs.targets = &ts[0]
		pd.fragment = fs
	}
	C.wgpuDevicePushErrorScope(d.c, C.WGPUErrorFilter_Validation)
	p := C.wgpuDeviceCreateRenderPipeline(d.c, pd)
	if err := d.popScope("pipeline " + desc.Label); err != nil {
		if p != nil {
			C.wgpuRenderPipelineRelease(p)
		}
		return nil, err
	}
	return &RenderPipeline{c: p}, nil
}

func (p *RenderPipeline) Release() {
	if p != nil && p.c != nil {
		C.wgpuRenderPipelineRelease(p.c)
		p.c = nil
	}
}

// BindGroupLayout returns the pipeline's (automatic) layout of group i.
func (p *RenderPipeline) BindGroupLayout(i int) *BindGroupLayout {
	return &BindGroupLayout{c: C.wgpuRenderPipelineGetBindGroupLayout(p.c, C.uint32_t(i))}
}

type BindGroupLayout struct{ c C.WGPUBindGroupLayout }

func (l *BindGroupLayout) Release() {
	if l != nil && l.c != nil {
		C.wgpuBindGroupLayoutRelease(l.c)
		l.c = nil
	}
}

type BindGroupEntry struct {
	Binding     uint32
	Buffer      *Buffer
	Offset      uint64
	Size        uint64 // 0: the rest of the buffer
	Sampler     *Sampler
	TextureView *TextureView
}

type BindGroup struct{ c C.WGPUBindGroup }

func (d *Device) CreateBindGroup(layout *BindGroupLayout, entries ...BindGroupEntry) *BindGroup {
	var a arena
	defer a.free()
	es := allocN[C.WGPUBindGroupEntry](&a, len(entries))
	for i, e := range entries {
		es[i].binding = C.uint32_t(e.Binding)
		if e.Buffer != nil {
			es[i].buffer = e.Buffer.c
			es[i].offset = C.uint64_t(e.Offset)
			es[i].size = C.uint64_t(e.Size)
			if e.Size == 0 {
				es[i].size = C.WGPU_WHOLE_SIZE
			}
		}
		if e.Sampler != nil {
			es[i].sampler = e.Sampler.c
		}
		if e.TextureView != nil {
			es[i].textureView = e.TextureView.c
		}
	}
	bd := C.WGPUBindGroupDescriptor{layout: layout.c, entryCount: C.size_t(len(es))}
	if len(es) > 0 {
		bd.entries = &es[0]
	}
	return &BindGroup{c: C.wgpuDeviceCreateBindGroup(d.c, &bd)}
}

func (g *BindGroup) Release() {
	if g != nil && g.c != nil {
		C.wgpuBindGroupRelease(g.c)
		g.c = nil
	}
}

// ---- commands ----

type CommandEncoder struct{ c C.WGPUCommandEncoder }
type CommandBuffer struct{ c C.WGPUCommandBuffer }

func (d *Device) CreateCommandEncoder() *CommandEncoder {
	return &CommandEncoder{c: C.wgpuDeviceCreateCommandEncoder(d.c, nil)}
}

func (e *CommandEncoder) Finish() *CommandBuffer {
	cb := C.wgpuCommandEncoderFinish(e.c, nil)
	C.wgpuCommandEncoderRelease(e.c)
	e.c = nil
	return &CommandBuffer{c: cb}
}

// CopyTexture copies a w×h block from (sx, sy) of src to (dx, dy) of dst.
// src and dst must be different textures.
func (e *CommandEncoder) CopyTexture(src *Texture, sx, sy int, dst *Texture, dx, dy, w, h int) {
	s := C.WGPUTexelCopyTextureInfo{texture: src.c, origin: C.WGPUOrigin3D{C.uint32_t(sx), C.uint32_t(sy), 0}, aspect: C.WGPUTextureAspect_All}
	t := C.WGPUTexelCopyTextureInfo{texture: dst.c, origin: C.WGPUOrigin3D{C.uint32_t(dx), C.uint32_t(dy), 0}, aspect: C.WGPUTextureAspect_All}
	ext := C.WGPUExtent3D{C.uint32_t(w), C.uint32_t(h), 1}
	C.wgpuCommandEncoderCopyTextureToTexture(e.c, &s, &t, &ext)
}

type Color struct{ R, G, B, A float64 }

type ColorAttachment struct {
	View    *TextureView
	Resolve *TextureView // MSAA resolve target
	Load    LoadOp       // zero: clear
	Store   StoreOp      // zero: store
	Clear   Color
}

type DepthAttachment struct {
	View  *TextureView
	Load  LoadOp // zero: clear
	Store StoreOp
	Clear float32 // zero is treated as 1 (far) when Load clears
}

type RenderPassDescriptor struct {
	Label string
	Color []ColorAttachment
	Depth *DepthAttachment
}

type RenderPass struct{ c C.WGPURenderPassEncoder }

func (e *CommandEncoder) BeginRenderPass(desc RenderPassDescriptor) *RenderPass {
	var a arena
	defer a.free()
	rp := (*C.WGPURenderPassDescriptor)(a.alloc(C.sizeof_WGPURenderPassDescriptor))
	rp.label = a.str(desc.Label)
	cs := allocN[C.WGPURenderPassColorAttachment](&a, len(desc.Color))
	for i, c := range desc.Color {
		cs[i].view = c.View.c
		cs[i].depthSlice = C.WGPU_DEPTH_SLICE_UNDEFINED
		if c.Resolve != nil {
			cs[i].resolveTarget = c.Resolve.c
		}
		cs[i].loadOp = C.WGPULoadOp_Clear
		if c.Load != 0 {
			cs[i].loadOp = C.WGPULoadOp(c.Load)
		}
		cs[i].storeOp = C.WGPUStoreOp_Store
		if c.Store != 0 {
			cs[i].storeOp = C.WGPUStoreOp(c.Store)
		}
		cs[i].clearValue = C.WGPUColor{C.double(c.Clear.R), C.double(c.Clear.G), C.double(c.Clear.B), C.double(c.Clear.A)}
	}
	rp.colorAttachmentCount = C.size_t(len(cs))
	if len(cs) > 0 {
		rp.colorAttachments = &cs[0]
	}
	if d := desc.Depth; d != nil {
		da := (*C.WGPURenderPassDepthStencilAttachment)(a.alloc(C.sizeof_WGPURenderPassDepthStencilAttachment))
		da.view = d.View.c
		da.depthLoadOp = C.WGPULoadOp_Clear
		if d.Load != 0 {
			da.depthLoadOp = C.WGPULoadOp(d.Load)
		}
		da.depthStoreOp = C.WGPUStoreOp_Store
		if d.Store != 0 {
			da.depthStoreOp = C.WGPUStoreOp(d.Store)
		}
		da.depthClearValue = 1
		if d.Clear != 0 {
			da.depthClearValue = C.float(d.Clear)
		}
		rp.depthStencilAttachment = da
	}
	return &RenderPass{c: C.wgpuCommandEncoderBeginRenderPass(e.c, rp)}
}

func (p *RenderPass) SetPipeline(pl *RenderPipeline) { C.wgpuRenderPassEncoderSetPipeline(p.c, pl.c) }

func (p *RenderPass) SetBindGroup(i int, g *BindGroup) {
	C.wgpuRenderPassEncoderSetBindGroup(p.c, C.uint32_t(i), g.c, 0, nil)
}

func (p *RenderPass) SetVertexBuffer(slot int, b *Buffer, off, size uint64) {
	if size == 0 {
		size = C.WGPU_WHOLE_SIZE
	}
	C.wgpuRenderPassEncoderSetVertexBuffer(p.c, C.uint32_t(slot), b.c, C.uint64_t(off), C.uint64_t(size))
}

func (p *RenderPass) SetIndexBuffer(b *Buffer, f IndexFormat, off, size uint64) {
	if size == 0 {
		size = C.WGPU_WHOLE_SIZE
	}
	C.wgpuRenderPassEncoderSetIndexBuffer(p.c, b.c, C.WGPUIndexFormat(f), C.uint64_t(off), C.uint64_t(size))
}

func (p *RenderPass) Draw(vertices, instances, firstVertex, firstInstance int) {
	C.wgpuRenderPassEncoderDraw(p.c, C.uint32_t(vertices), C.uint32_t(instances), C.uint32_t(firstVertex), C.uint32_t(firstInstance))
}

func (p *RenderPass) DrawIndexed(indices, instances, firstIndex, baseVertex, firstInstance int) {
	C.wgpuRenderPassEncoderDrawIndexed(p.c, C.uint32_t(indices), C.uint32_t(instances), C.uint32_t(firstIndex),
		C.int32_t(baseVertex), C.uint32_t(firstInstance))
}

func (p *RenderPass) SetViewport(x, y, w, h, minDepth, maxDepth float32) {
	C.wgpuRenderPassEncoderSetViewport(p.c, C.float(x), C.float(y), C.float(w), C.float(h), C.float(minDepth), C.float(maxDepth))
}

func (p *RenderPass) SetScissorRect(x, y, w, h int) {
	C.wgpuRenderPassEncoderSetScissorRect(p.c, C.uint32_t(x), C.uint32_t(y), C.uint32_t(w), C.uint32_t(h))
}

func (p *RenderPass) End() {
	C.wgpuRenderPassEncoderEnd(p.c)
	C.wgpuRenderPassEncoderRelease(p.c)
	p.c = nil
}
