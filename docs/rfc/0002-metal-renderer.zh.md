# RFC 0002：macOS 上的 Metal 渲染器

状态：M1、M3（FrameView）已实现，M2 暂缓（2026-11-09）

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
- `End` 把**损坏区域**上传到一张常驻的 `MTLTexture`（`replaceRegion`），然后对 `CAMetalLayer` 的下一个 drawable 画一个全屏四边形并 present。
  - 纹理常驻、每帧整屏重画 drawable：drawable 内容是否保留无关紧要，从结构上消除「旧缓冲被合成出来」这一类问题。
  - 写纹理前等待上一帧的 command buffer 完成，避免 CPU 改写正在被 GPU 采样的纹理。
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

## 阶段

| 阶段 | 内容 | 验收 |
|---|---|---|
| M1 | MetalRenderer + cocoa Metal 模式 + 选择 / 环境变量 + GL 自动回退 | go-code 在 Metal 下日常使用正常；截图、弹窗、透明窗口、改尺寸、多显示器；scene3d 示例自动回退 GL |
| M2 | Metal GPU 光栅后端（暂缓） | `QUI_GPU_RASTER=1` 下与 CPU 栅格像素一致（容差内），render 一致性测试通过 |
| M3 | 保留式 Metal 绘制 + FrameView / VideoView | 视频、远程桌面画面在 Metal 窗口里不回退 GL |
| 后续 | 滚动平移（`Window.ScrollPixels`，GPU 端移动纹理）、多矩形脏区、帧统计（`Performance.getFrames`） | go-code 1720×1080 滚动帧 p95 10.3 → 2.3 ms |

## 环境变量

- `QUI_RENDERER=metal|gl|cpu`：覆盖渲染器选择（cocoa 默认 metal）。
- `QUI_DEBUG_RENDERER=1`：每个窗口打印一次实际使用的呈现方式，以及回退到 GL 的时刻。
