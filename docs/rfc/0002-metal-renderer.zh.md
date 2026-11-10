# RFC 0002：macOS 上的 Metal 渲染器

状态：M1、M3（FrameView）已实现，M2 暂缓（2026-11-09）

> **2026-11-10：`MetalRenderer` 已移除，由 WebGPU（Dawn）取代**——原因与测量见 [RFC 0003](0003-webgpu.zh.md)「移除 MetalRenderer」。下文仅作历史记录，其中的 `MetalRenderer`、`NewMetalRenderer`、`qui_nometal`、`QUI_DEBUG_METAL_VERIFY` 均已不存在。

> 2026-11-10：OpenGL 已从 qui 移除（RFC 0003）。下文的「GL 回退」改为「切到 WebGPURenderer」：需要 GPU 内容的窗口在同一个 CAMetalLayer 上换成 Dawn；`QUI_RENDERER` 的取值变为 `metal|webgpu|cpu`，`QUI_GPU_RASTER` 已删除。

## 背景

macOS 从 10.14 起废弃 OpenGL（冻结在 4.1，Apple Silicon 上是跑在 Metal 之上的转换层）。qui 在 cocoa 后端用 `NSOpenGLContext` 呈现：`GLRenderer` 把 CPU 光栅化的整帧上传成 GL 纹理再 `flushBuffer`。这条路有两个问题：

1. **不是系统主推的路径**：AppKit / Core Animation 与 NSOpenGLContext 的合成配合越来越差。2026-11-09 的 go-code「hover 后画面回退一帧」就是这一类——子窗口挂上时 AppKit 用了没保留内容的 GL 后台缓冲重新合成（临时修复：`NSOpenGLPFABackingStore`）。
2. **跟不上系统能力**：ProMotion / 可变刷新率、CAMetalLayer 的呈现时序、IOSurface 零拷贝（视频帧）都要求 Metal。

## 目标

- macOS 上默认用 **Metal** 呈现和（可选的）GPU 光栅化；GL 只在确实需要原生 GL 的窗口（scene3d、CEF webview、自定义 GL）上使用。
- 对应用**零改动**：调用 `SetRenderer(NewGLRenderer())` 或不设置渲染器的应用自动得到 Metal；用到原生 GL 的窗口自动退回 GL，不出现空白内容。
- 不破坏分层：`internal/render` 仍只依赖 `gfx` / `typeset`；平台对象经 `internal/platform/contract` 传递。

非目标：Linux / Windows 仍是 GL（或 CPU 呈现）；scene3d 不移植到 Metal。

## 设计

### 1. MetalRenderer（`internal/render`，darwin + cgo）

- 光栅化与 `CPURenderer` 相同：`ImageCanvas` + `CPUBackend` 写进 `*image.RGBA`。
- **帧缓冲是一块 IOSurface**（统一内存的机器上）：CPU 直接光栅化进去，GPU 用同一块内存上的 `MTLTexture` 采样，没有上传这一步。`End` 只对 `CAMetalLayer` 的下一个 drawable 画一个全屏四边形并 present。
  - 纹理常驻、每帧整屏重画 drawable：drawable 内容是否保留无关紧要，从结构上消除「旧缓冲被合成出来」这一类问题。
  - CPU 开始画一帧前 `IOSurfaceLock`，并等上一帧的 command buffer 完成（GPU 可能还在采样这块内存），`End` 时解锁。
  - 滚动平移（`ScrollPixels`）在 GPU 上对这块内存经临时纹理做 blit，提交并等它完成（约 0.2 ms），CPU 再画新露出的部分。
  - 拿不到 IOSurface 时（独显机器等）退回私有纹理：每个损坏矩形经 staging buffer 上传，平移作为 GPU blit 排在上传之前。
  - 渲染器关闭或换 GL 前，帧内容复制回 Go 内存，`Snapshot` 不会悬空。
- 渲染器自己完成 present（`SelfPresentingRenderer` 能力），`Window.present` 不再调 `surface.present()`。
- 透明窗口（弹窗、overlay panel）：layer `opaque = NO`，清成透明，四边形直接写预乘颜色（不混合）。
- 色彩空间：layer 设为 sRGB，与 CPU 栅格（sRGB）及原 CGImage 路径一致。
- `Snapshot` / `FrameImage`：CPU 图像就在内存里，直接复制。

### 2. cocoa 的 Metal 模式

- 新增可选接口 `contract.MetalSurface`（`MetalLayer()` / `ExitMetal()`），只有 cocoa 实现，其它后端不受影响。cocoa 首次调用时进入 Metal 模式：GL context `clearDrawable`，视图换成 `CAMetalLayer`（topLeft 重力，`drawableSize` 跟随 backing 尺寸）。
- **不启动 display link**：`nextDrawable` 在所有 drawable 都在队列里时会阻塞，本身就把渲染节奏拉到显示器的刷新率上（和 GL 的 swap interval 一样）；再叠一个不同步的 vsync 标志，会让 60 fps 的视频源在两次 tick 之间的帧被丢掉（实测丢帧 29/300 → 与 GL 持平）。
- 实时拖拽改尺寸时用 `presentsWithTransaction`，让新尺寸的帧与窗口边框同一事务提交，避免内容抖动。
- **可回退**：Metal 模式可以切回 GL 模式（恢复 `setView:` 到 GL context），供第 3 点使用。

### 3. 渲染器选择与 GL 自动回退

- `QUI_RENDERER=metal|gl|cpu` 覆盖选择；cocoa 上默认 `metal`。
- cocoa 上 `SetRenderer(*GLRenderer)` 与默认渲染器都换成 `MetalRenderer`，除非设置了 `QUI_RENDERER=gl`。显式 `SetRenderer(NewMetalRenderer())` 不受影响。
- MetalRenderer 的 canvas 实现 `GPUCanvas` / `RetainedGLCanvas`：第一次被调用（某个 widget 需要原生 GL）时记下「需要 GL」并丢弃这次调用；`Window` 在帧末把该窗口换成 `GLRenderer`（surface 切回 GL 模式）并整窗重画。每个窗口最多切一次，记日志。
- 弹窗窗口（`NewPopupWindow`）同样走这套选择。

### 4. Metal GPU 光栅后端（`QUI_GPU_RASTER=1`）—— 暂缓

暂不实现：GL 版本身是可选的混合后端，只加速矩形一类图元，文字 / 路径 / 图片都走 CPU 再上传，收益未经证实。设置了 `QUI_GPU_RASTER=1` 的窗口继续用 GL。下面是将来要做时的方案：


移植现有 GL 的「flushing hybrid」：矩形 / 圆角矩形 / 描边 / 线段用 MSL 着色器画进离屏 `MTLTexture`，其余图元走 CPU 回退并在需要时上传合成；`ReadbackCPU` 用 `getBytes` 读回给截图。与 GL 版共享图元判定逻辑与测试。

### 5. 保留式 Metal 绘制（`RetainedMetalCanvas`）

与保留式 GL 绘制同一套存活规则（`RetainedSet[F]` 泛型共用）：`QueueRetainedMetalDraw(key, physBounds, fn)`，`Window.RequestGLRedraw` 对 Metal 窗口同样有效（只重放保留绘制、不重新光栅化）。绘制拿到的不是原始 API，而是渲染器提供的 `MetalCompositor`：`DrawPixelBuffer`（CVPixelBuffer → `CVMetalTextureCache`，零拷贝）与 `DrawPixels`（RGBA / BGRA 上传到按 key 缓存的纹理）。合成顺序与 GL 相同：先清屏、画保留绘制，再以预乘 src-over 叠 2D 层，ClearRect 打出的洞露出下面的视频。

- media 的 **FrameView** 已接入（RGBA 推帧、CVPixelBuffer 推帧都不再回退 GL）。
- **VideoView** 已接入：AVFoundation 后端的帧是 IOSurface 上的 BGRA，走 `DrawPixelBuffer` 零拷贝；纯 Go 后端的 I420 走新增的 `DrawYUV420`（三个 R8 纹理，着色器里按与 GL 相同的系数转 RGB）。

### 6. 测量结论与暂缓项

go-code（`cmd/go-code/tools/scrollbench`，整窗重画同法：`Debug.invalidate` 触发整窗重画，`Performance.getFrames` 取帧统计）：

| 场景 | 改动前 | IOSurface | + 延迟填充 |
|---|---|---|---|
| 1720×1080 滚动帧 total p50 / p95 | 8.41 / 10.31 ms | 2.18 / 2.55 ms | 2.10 / 2.46 ms |
| 整窗重画 1280×800 total p50（present） | 5.97 ms（1.78） | 4.44 ms（0.09） | 3.26 ms（0.10） |
| 整窗重画 1720×1080 total p50（present） | 9.86 ms（3.08） | 6.66 ms（0.08） | 4.31 ms（0.10） |
| 整窗重画 2560×1400 total p50（present） | 15.57 ms（5.12） | 10.25 ms（0.09） | 6.09 ms（0.11） |

IOSurface 让 present 从「随面积线性增长的 memcpy」变成常数；滚动帧比私有纹理方案（1.81 ms）略高，多出的是同步等 GPU 平移完成的时间，换来的是整窗重画省下 3–5 ms。

IOSurface 之后剩下的时间几乎全在 CPU 光栅（2560×1400 下 draw ≈ 10 ms），其中最大的一块是不透明背景的重复填充：窗口背景、各级容器背景、编辑器背景层层铺满，一次整窗重画每个像素被写约 4.8 次。

**延迟填充（overdraw 消除，`internal/render/backend_cpu_defer.go`）**：CPU 后端把基础帧缓冲上「只写不读」的写入（不透明 FillRect、Clear、ClearRect，以及圆角矩形的不透明中心部分）先记下来不画；后来的这类写入把自己的矩形从已记录的里减掉（那些像素反正要被覆盖），所以记录表始终两两不相交、顺序无关；其它任何绘制先把自己区域里的记录画出来，`End` 画出剩余的。结果与立即绘制逐像素相同，只是省掉了会被覆盖的写。整窗重画的不透明写入从约 4.8 倍降到约 1 倍。只有调用 `End` 的渲染器（CPU / GL 的 CPU 路径 / Metal）打开；`QUI_DEFER_FILLS=0` 关闭以便二分问题。验证：随机操作序列（填充、半透明、圆角、描边、路径、覆盖率、图层）与立即绘制逐像素对比，并用破坏性修改确认测试能抓到漏刷、刷的区域过小；go-code 开 / 关延迟填充的整窗截图逐字节相同。

内存分配：排除 gopls 的干扰后，绘制路径每帧分配约 100 KB（文字 mask、装饰收集），已不是瓶颈，暂不处理。

- **GPU 光栅（第 4 点）继续暂缓**：瓶颈不是文字 / 路径这类 GPU 才能解决的问题；消除 overdraw 后整窗重画在 2560×1400 下约 6 ms。
- **合成层树（compositing layers）只做设计不实现**：把滚动内容、浮层各自放一张纹理、由 GPU 合成，能让滚动和动画完全不重光栅，但要改 widget 树的绘制契约（哪些子树成层、层失效规则），目前单层 + `ScrollPixels` 已把滚动压到 ~2 ms，收益要等测到瓶颈再说。
- **通用 ScrollView / htmlcss 容器的 `ScrollPixels` 暂缓**：只有调用方能保证「区域内除内容外没有不随内容移动的东西」时平移才正确。htmlcss 里 `background-attachment` 式的固定背景、重叠/定位元素、粘性元素、圆角裁剪都可能违反这一点。将来要做时的安全条件：容器不透明背景随内容滚动、无子元素越出视口、视口上方无兄弟元素覆盖、无 sticky 子元素；不满足就退回整块重画。

## 阶段

| 阶段 | 内容 | 验收 |
|---|---|---|
| M1 | MetalRenderer + cocoa Metal 模式 + 选择 / 环境变量 + GL 自动回退 | go-code 在 Metal 下日常使用正常；截图、弹窗、透明窗口、改尺寸、多显示器；scene3d 示例自动回退 GL |
| M2 | Metal GPU 光栅后端（暂缓） | `QUI_GPU_RASTER=1` 下与 CPU 栅格像素一致（容差内），render 一致性测试通过 |
| M3 | 保留式 Metal 绘制 + FrameView / VideoView | 视频、远程桌面画面在 Metal 窗口里不回退 GL |
| 后续 | 滚动平移（`Window.ScrollPixels`，GPU 端移动纹理）、多矩形脏区、帧统计（`Performance.getFrames`） | go-code 1720×1080 滚动帧 p95 10.3 → 2.3 ms |
| 后续 | IOSurface 帧缓冲（零拷贝 present） | 2560×1400 整窗重画 present 5.1 → 0.09 ms |

## 环境变量

- `QUI_RENDERER=metal|gl|cpu`：覆盖渲染器选择（cocoa 默认 metal）。
- `QUI_DEBUG_RENDERER=1`：每个窗口打印一次实际使用的呈现方式，以及回退到 GL 的时刻。
- `QUI_DEBUG_METAL_VERIFY=1`：每帧把纹理读回与 CPU 帧比较，打印不一致。IOSurface 模式下两者是同一块内存，这项检查只对私有纹理路径有意义；IOSurface 的正确性由离屏测试（与从头绘制的参考帧比较）保证。
