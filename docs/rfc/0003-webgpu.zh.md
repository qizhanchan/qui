# RFC 0003：以 WebGPU（Dawn）取代 OpenGL

状态：W0–W5 已实现（2026-11-10）；Windows / Linux 上的 WebGPU 呈现未实测

## 背景

RFC 0002 让 macOS 默认走 Metal 呈现，但 OpenGL 仍然在 qui 里扎着根：

| 用到 GL 的地方 | 作用 |
|---|---|
| `GLRenderer`（`internal/render/renderer_gl.go`） | 非 Metal 窗口的呈现（Windows / `qui_glfw`、需要原生 GL 的 macOS 窗口） |
| `backend_gpu.go`（`QUI_GPU_RASTER=1`） | 可选的 GL 光栅后端（只加速矩形类图元） |
| `GPUCanvas.QueueGLDraw` / `DrawTexture(uint32)`、`RetainedGLCanvas`、`Window.SetPreRender/SetPostRender(GLState)`、`CompileShader`、`PhysicalScissor` | **公开 API**：应用和组件直接写 GL |
| `scene3d` | GLSL 3.3 + FBO 的 3D 视口 |
| `media`（FrameView / VideoView 的 GL 路径） | 视频帧上传与 YUV 转换 |
| cocoa 的 `NSOpenGLContext`、glfw 的 GL context | 窗口的 GL 呈现 |

问题不在 2D：qui 的 2D 是 CPU 光栅，GPU 只负责把帧送上屏幕。问题在于**公开 API 把应用绑死在 GL 上**——要做 3D / 游戏 / 自定义着色器，只能写 GL；而 macOS 已经废弃 GL，Metal 窗口遇到这类组件只能退回 GL。为每个平台各写一套原生 API（Metal / D3D12 / Vulkan）又维护不起。

WebGPU 正好是这个缺口的答案：一套 API、一种着色语言（WGSL），由实现映射到各平台原生 API。Dawn 是 Chromium 的 WebGPU 实现，后端覆盖 Metal、D3D12、Vulkan（以及 GL/GLES 作兜底）；`webgpu.h` 的核心部分自 2025-09（Chrome 141）起稳定。浏览器（`GOOS=js`）里的 `navigator.gpu` 是同一套语义。

## 目标

- **qui 不再包含任何 OpenGL**：删除 `GLRenderer`、GL 光栅后端、GL 钩子与 `go-gl/gl` 依赖；cocoa 不再创建 `NSOpenGLContext`；glfw 窗口不再带 GL context（`go-gl/glfw` 只作为 Windows 的窗口层保留）。
- **GPU 内容的公开 API 改为 WebGPU**：组件 / 应用拿到 `wgpu` 设备、命令编码器和帧目标，用 WGSL 写着色器；scene3d、media、自定义着色器示例全部迁移。
- **2D 呈现不退步**：macOS 默认仍是 RFC 0002 的 `MetalRenderer`（IOSurface 零拷贝，已调优）；WebGPU 渲染器同样做到 IOSurface 零拷贝，在需要 GPU 内容的窗口上接手。
- **跨平台**：同一份 WebGPU 代码在 macOS（Metal）、Windows（D3D12）、Linux（Vulkan）上运行。

非目标：GPU 光栅（RFC 0002 已测得瓶颈不在此，继续暂缓，将来以 WGSL 实现）；合成层树。

## 设计

### 1. Dawn 的获取与加载

- **运行时加载，不在构建期链接**。`internal/wgpu` 编入 Dawn 的 `webgpu.h`（`dawn_webgpu.h`，BSD-3；与 Go 文件放在同一目录，`go mod vendor` 才会一并复制），每个 `wgpu*` 入口由生成的转发函数（`procs_gen.c`，`go generate`）经函数指针调用；`wgpu.Load()` 在运行时 `dlopen` Dawn 共享库并解析全部 276 个入口。
  - 好处：没装 Dawn 的机器照样 `go build` / `go test`（WebGPU 不可用，窗口保持 CPU / Metal 呈现）；交叉编译不需要 Dawn；go module 缓存里的 qui 不需要附带二进制。
- **固定版本**：`wgpu.DawnRevision`（Dawn 提交）与 `wgpu.DawnRelease`（github.com/google/dawn 的 nightly 标签）。`webgpu.h` 的扩展结构体在不同 Dawn 版本之间不保证 ABI 兼容，所以头文件、库、安装目录都以这个修订号为准；升级 Dawn = 一次显式的换头文件 + 换哈希的提交。
- **`cmd/qui-dawn`**：下载固定版本的官方预编译包，核对 SHA-256（写死在源码里），取出单体静态库 `libwebgpu_dawn.a`，链接成只导出 `wgpu*` 的共享库（macOS arm64 约 8 MB），装到 `<用户缓存>/qui/dawn/<修订>/<os>-<arch>/`。`-o DIR` 顺带复制一份（打包 .app 时放进 `Contents/Frameworks`）。
- **查找顺序**（`wgpu.LibraryCandidates`）：`$QUI_DAWN_LIB` → 可执行文件同目录 → `../Frameworks`（.app）→ 用户缓存 → 系统搜索路径。`QUI_WEBGPU=0` 关闭 WebGPU。
- 平台：darwin/arm64、darwin/amd64 有固定哈希；linux/amd64 有链接配方（未验证）；Windows 的发布包是 MSVC 静态库，需要另行构建 DLL，后续阶段处理。

### 2. `internal/wgpu`：Go 绑定

只覆盖 qui 与其 GPU 组件用到的部分：设备、纹理与视图、缓冲、采样器、WGSL 着色器模块（编译错误经 error scope 返回）、渲染管线（顶点布局、深度、混合、MSAA）、绑定组（自动布局）、命令编码器 / 渲染通道、纹理拷贝与回读、Surface（CAMetalLayer / HWND / Xlib）、IOSurface 导入（`SharedTextureMemory`）。

- **进程内只有一个设备**（`wgpu.Shared()`）：所有窗口、离屏目标、组件共享，纹理可以跨窗口使用。
- 所有异步调用（请求适配器 / 设备、`mapAsync`、`onSubmittedWorkDone`、`popErrorScope`）都以 `WaitAnyOnly` 模式 + `wgpuInstanceWaitAny` 同步等待，回调在调用线程上执行，不会有 C 线程回调 Go。唯一的例外是未捕获错误 / 设备丢失回调，转发给 `wgpu.SetErrorHandler`（默认写日志；测试里设为失败）。
- 描述符在 C 内存里构造（cgo 不允许传含 Go 指针的 Go 内存），每次调用一个 arena。
- 对象需要显式 `Release`（不用 finalizer：finalizer 跑在别的 goroutine，而所有 GPU 调用都在 UI 线程）。
- 公开包 **`gpu`** 用类型别名重新导出这套 API，供 scene3d 与应用使用（与 `pdf`/`svg` 直接触达内部层的做法一致，archtest 增加对应的边）。

### 3. 渲染器

| 渲染器 | 光栅 | 呈现 | 在哪里 |
|---|---|---|---|
| `CPURenderer` | CPU | 平台合成器（CALayer contents / XPutImage / canvas） | x11、web、headless、无 GPU 时 |
| `MetalRenderer` | CPU | CAMetalLayer，IOSurface 零拷贝 | macOS 默认（RFC 0002） |
| `WebGPURenderer`（新） | CPU | WebGPU Surface | 需要 GPU 内容的 macOS 窗口；Windows（glfw）；`QUI_RENDERER=webgpu` |

`WebGPURenderer` 的结构与 `MetalRenderer` 一一对应：

- **帧缓冲**：macOS 统一内存机器上是一块 IOSurface，经 `SharedTextureMemory` 导入成纹理——CPU 直接光栅化进去，GPU 采样同一块内存，没有上传。CPU 开始画之前等上一帧的 GPU 工作完成并 `IOSurfaceLock`；每帧 `BeginAccess(initialized)` / `EndAccess`。其它情况用普通纹理，每个损坏矩形 `queue.writeTexture` 上传。
- **滚动平移**（`ScrollPixels`）：`copyTextureToTexture` 经临时纹理（WebGPU 不允许同一纹理自拷贝），与 Metal 版相同。
- **一帧的组成**：先运行本帧的 GPU 绘制回调（它们可以先渲染到自己的离屏纹理）→ 打开 surface 纹理上的主通道，清屏 → 依次执行合成操作（视频帧、纹理、直接画进目标的通道回调）→ 以预乘 src-over 画 2D 层（按像素坐标 `textureLoad`，ClearRect 打出的洞露出下面的 GPU 内容）→ 提交、present。
- 透明窗口：surface 用 `Premultiplied` alpha 模式，清成透明。
- 实时改尺寸：cocoa 在 live resize 期间设置 `presentsWithTransaction`；present 前等待本帧 GPU 工作完成，让新尺寸的帧与窗口边框同一事务提交。
- `Snapshot`：CPU 帧就在内存里，直接复制；测试另有读回 surface 实际内容的路径。

**选择规则**（取代 GL 回退）：

- 应用调用 `SetRenderer(NewGLRenderer())`（保留为 deprecated，等价于新的 `NewAutoRenderer()`）或不设置渲染器时，窗口取「自动」：cocoa 上是 `MetalRenderer`，glfw 上是 `WebGPURenderer`，无 GPU 的表面上是 `CPURenderer`。
- `MetalRenderer` 的画布实现 `GPUCanvas` 只是为了**探测**：第一次有组件请求 WebGPU 绘制时记下「需要 WebGPU」并丢弃本次调用，帧末窗口换成 `WebGPURenderer`（同一个 CAMetalLayer，Dawn 接管）并整窗重画——和过去切 GL 的流程相同，只是方向换成 WebGPU。
- Dawn 不可用时，需要 GPU 内容的组件画不出来（记一次日志），其余界面照常：组件本来就要处理画布不支持 `GPUCanvas` 的情况。
- `QUI_RENDERER=metal|webgpu|cpu` 覆盖选择；`QUI_DEBUG_RENDERER=1` 打印每个窗口的实际呈现方式与切换时刻。

### 4. 组件与应用的 GPU API

取代所有 GL 接口：

```go
// 视频类内容：Metal 与 WebGPU 渲染器都支持，不触发切换。
type Compositor interface {            // 原 MetalCompositor
    DrawPixelBuffer(pb unsafe.Pointer, src, dst Rect) bool   // macOS CVPixelBuffer，零拷贝
    DrawPixels(key any, pix unsafe.Pointer, stride, w, h int, bgra bool, src, dst Rect) bool
    DrawYUV420(key any, planes [3]YUVPlane, m [9]float32, off, scale [3]float32, src, dst Rect) bool
    FramebufferSize() Size
}
type RetainedCompositorCanvas interface {   // 原 RetainedMetalCanvas
    QueueRetainedDraw(key any, physBounds Rect, fn func(Compositor))
}

// 任意 GPU 内容（3D、自定义着色器）：只有 WebGPU 渲染器支持。
type GPUCanvas interface {
    QueueGPUDraw(fn func(*GPUFrame))                                  // 本帧
    QueueRetainedGPUDraw(key any, physBounds Rect, fn func(*GPUFrame)) // 保留式，存活规则同 RFC 0002
}
type GPUFrame struct {
    Device  *gpu.Device
    Encoder *gpu.CommandEncoder // 本帧的编码器：离屏通道在这里开
    Format  gpu.TextureFormat   // 目标格式（管线的颜色目标要用它）
    Size    Size                // 物理像素
    // 方法：
    //   DrawTexture(view, src, dst, TextureOpts)       把纹理合成到 dst（预乘 src-over）
    //   RenderPass(func(*gpu.RenderPass))               直接在帧目标上画（load，不清屏）
    //   Compositor()                                    视频类合成
}
```

- `Window.SetPreRender/SetPostRender(func(*GPUFrame))`：整窗的 GPU 钩子（着色器背景、后处理叠加）。
- `Window.RequestGPURedraw` / `ForgetRetainedDraw`（原 `RequestGLRedraw` / `ForgetRetainedGLDraw`，旧名保留为 deprecated 别名）。
- 删除：`GLState`、`TextureOpts.FlipY` 的 GL 含义、`PhysicalScissor`、`CompileShader`、`SetGPURaster`、`ActiveGLRenderer`、`GLRenderer`。
- **scene3d** 改为 WGSL：`Viewport` 在 `QueueRetainedGPUDraw` 里把场景画进自己的离屏颜色 + 深度纹理（可选 MSAA），再 `DrawTexture` 到组件区域；网格 / 线段的顶点缓冲、每物体的模型矩阵走实例缓冲。
- **media**：FrameView / VideoView 只保留合成器路径，GL 路径删除。
- **webview**：目前绘制是空实现，去掉 `GLState` 参数。

### 5. 平台层

- **cocoa**：删除 `NSOpenGLContext`。窗口要么是 CPU 模式（CALayer contents + CVDisplayLink），要么是 Metal 模式（CAMetalLayer，`MetalRenderer` 与 `WebGPURenderer` 共用）。`contract.MetalSurface.ExitMetal` 删除；`Caps.GPUContext` 改为表示「能提供 GPU 呈现表面」。
- **glfw**：`GLFW_CLIENT_API = NO_API`，不建 GL context；新增 `contract.WebGPUSurface`，用原生窗口句柄建 surface（Windows HWND、macOS CAMetalLayer、X11 窗口）。
- **x11**：仍是 XPutImage 呈现 CPU 帧；WebGPU（Vulkan + Xlib surface）作为后续阶段。
- **web**：不变（CPU + canvas）；浏览器 WebGPU 后续单独评估。

## 阶段

| 阶段 | 内容 | 验收 |
|---|---|---|
| W0 | `internal/wgpu` + `cmd/qui-dawn`：运行时加载、设备、离屏绘制、着色器报错、纹理往返 | `go test ./internal/wgpu` 在 M 系列 Mac 上通过（Apple M5，Metal） |
| W1 | `WebGPURenderer`（IOSurface 零拷贝 / 上传、ScrollPixels、合成器、透明窗口、live resize）+ 渲染器选择 | 离屏测试与 CPU 参考帧逐像素一致；`QUI_RENDERER=webgpu` 下 go-code 日常使用正常；scrollbench 对比 Metal |
| W2 | GPU API（`GPUCanvas` / `GPUFrame` / 钩子）+ Metal→WebGPU 自动切换；scene3d 移植 WGSL；customgl 示例改写 | 3dviewer / 自定义着色器示例在 Metal 窗口里自动切到 WebGPU 并正确显示 |
| W3 | media 只留合成器路径；webview 清理 | FrameView / VideoView 在 Metal 与 WebGPU 窗口下都正确 |
| W4 | cocoa 删除 NSOpenGLContext；glfw 改 NO_API + WebGPU surface | `-tags qui_glfw` 在 macOS 上用 WebGPU 呈现 |
| W5 | 删除 `GLRenderer`、GL 光栅后端、`go-gl/gl`；文档、archtest；下游（go-code、q-remote-desktop 的 `SetPostRender` 截图）更新 | `go list -deps ./... | grep go-gl/gl` 为空；全部测试通过 |

## 实测（2026-11-10，Apple M5，macOS 26.7）

- `go test ./...` 全部通过；`go list -deps ./...` 不含 `go-gl/gl`；`CGO_ENABLED=0`、`GOOS=js`、`GOOS=windows/linux CGO_ENABLED=0`、`-tags qui_glfw` 均能编译。
- 离屏测试（`internal/render/renderer_webgpu_darwin_test.go`）：呈现帧与 CPU 帧逐像素一致（IOSurface / 上传两条路径，含行补齐的宽度），视频挖洞合成，`RequestGPURedraw` 帧，DrawPixels，ScrollPixels（含平移区内的重画），GPU 绘制 + 前后钩子，透明窗口。scene3d：立方体渲染、三角形绕序与背面剔除和 GL 约定一致。
- 真实窗口：3D 视口窗口从 Metal 自动切到 WebGPU 并正确合成；`-tags qui_glfw`（无 GL context，CAMetalLayer 上建 surface）同样正确；`examples/customgpu` 的 WGSL 背景；q-kart（scene3d 赛车游戏）画面 + 2D HUD 正确。
- go-code `scrollbench`（1720×1080，120 步滚轮）：

| 呈现 | total p50 / p95 | draw p50 | present p50 |
|---|---|---|---|
| MetalRenderer（IOSurface） | 2.35 / 5.02 ms | 0.89 ms | 0.08 ms |
| WebGPURenderer（IOSurface） | 2.63 / 4.41 ms | 0.99 ms | 0.13 ms |
| WebGPURenderer（上传，`QUI_WEBGPU_SHARED=0`） | 2.02 / 2.97 ms | 1.04 ms | 0.27 ms |

  `-verify 10`（每 10 步与整窗重画逐像素比对）在 WebGPU 下通过。IOSurface 路径的滚动帧略慢，原因与 Metal 版相同：平移要在 GPU 上同步完成后 CPU 才能画露出部分；上传路径把平移排在下一帧的命令里，不等。
- Dawn 冷启动：首个 WebGPU 窗口建设备约 0.3–0.9 s（只在需要 GPU 内容时发生）。

## 实现中的发现

- `go mod vendor` 只复制包目录里的文件，不复制没有 Go 文件的子目录：头文件必须与 Go 文件同目录（`dawn_webgpu.h`、`dawn_version.h`），否则 vendor 了 qui 的下游（q-remote-desktop）编译失败。
- Dawn 的 Metal 后端用 `[commandBuffer presentDrawable:]` 呈现，不看 `presentsWithTransaction`；live resize 期间 WebGPURenderer 在 present 前等本帧 GPU 工作完成，尽量让新尺寸的帧赶上窗口边框的事务。未在真实拖拽中验证。
- 设置了 pre-render 钩子的窗口，2D 层要清成透明（钩子就是背景）；原 GL 示例在主题背景加入后其实已被盖住。
- WebGPU 不允许纹理自拷贝，`queue.writeTexture` 在队列时间线上先于已编码的命令执行：滚动平移必须先单独提交，再上传损坏矩形，否则平移会盖掉新画的像素。

## 风险

1. **live resize 抖动**：WebGPU 没有 `presentsWithTransaction`。靠在 present 前等 GPU 完成 + 层属性实现；若不够，2D 窗口本来就在 MetalRenderer 上，只有 GPU 内容窗口受影响。
2. **体积**：Dawn 共享库约 8 MB（macOS arm64），只有分发了它的应用才付这个代价。
3. **Dawn 扩展 API 会变**：升级 Dawn 时同步更新 `dawn_webgpu.h` / `dawn_version.h`、哈希与必要的绑定改动；`procs_gen.c` 重新生成，缺失的入口会在 `Load` 时报出。
4. **Windows 未验证**：本机只有 macOS。glfw + HWND 的代码路径只做到能编译的程度，需要在 Windows 上实测并补充 DLL 的构建配方。
5. **冷启动**：Dawn 建设备约 1 s（首次，含 Metal 初始化）。设备只在第一个 WebGPU 窗口出现时创建，纯 2D 应用（Metal 呈现）不付这个代价。

## 环境变量

- `QUI_RENDERER=metal|webgpu|cpu`：覆盖渲染器选择。
- `QUI_WEBGPU=0`：不加载 Dawn（需要 GPU 内容的组件不显示）。
- `QUI_DAWN_LIB=/path/libwebgpu_dawn.dylib`：指定 Dawn 库。
- `QUI_DEBUG_RENDERER=1`：打印呈现方式、Dawn 适配器与切换。
