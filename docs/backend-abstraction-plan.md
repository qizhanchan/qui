# Backend 抽象 + 每平台现代技术栈 —— 可行性与方案

> 前提（已定）：长期要支持多平台；GLFW 不作为长期选择；抽 backend；每个平台上保持技术栈先进。

## 结论

**可以，方向是对的。** 但必须先接受一件事：你要抽的**不是一个 seam，是两个**，而且成本差一个量级。

| Seam | 范围 | 面积 | 成本 |
|---|---|---|---|
| **S1 平台层** | 开窗 / 事件泵 / 输入 / 光标 / 显示器 | **35 个调用，10 个文件** | 低 |
| **S2 呈现 + GPU 层** | present、光栅化、GPU 逃生舱 | **约 405 个 `gl.*` 调用点**，且已是公共 API 承诺 | 高 |

"技术栈先进" 的战场几乎全在 S2。好消息是 S2 可以再切成两级，**第一级就能兑现绝大部分收益**（见第三节）。

---

## 一、好消息：seam 其实已经半成品

1. **`Renderer` 接口只有两个方法**（[render.go:128](render.go:128)）：
   ```go
   type Renderer interface {
       Begin(windowSize Size) Canvas
       End()
   }
   ```
   `Window.SetRenderer` 可以随便换实现（[window.go:922](window.go:922)）。这是极其干净的切口——S2 的上半部分不用新建，只需要清理。

2. **平台调用面积小得反常**：只有 10 个文件 import glfw，实际调用约 35 个（清单见 [glfw-dependency-research.md](docs/glfw-dependency-research.md)）。

3. **"nil 后端"这条路全套测试一直在跑**：[testhelpers.go:319](testhelpers.go:319) 的 `NewTestWindow` 是 `&Window{lastSize: size}`，`handle == nil`，测试从不创建真窗口。也就是说 `Window` 的每个方法早已容忍"没有平台句柄"——**换后端最大的风险（静默回归）已经有一整套回归网。**

4. **CPU raster 是纯 Go 的参考实现**，GPU 只是 opt-in（`QUI_GPU_RASTER=1`）。这是让"每平台现代化"可负担的结构性前提。

## 二、坏消息：GL 已经渗进公共 API，不只是内部实现

这是最容易低估的部分。`gl.*` 实际调用点分布（已剔除 `gl` 作为局部变量名的假阳性）：

| 位置 | 调用点 | 性质 |
|---|---|---|
| [renderer_gl.go](renderer_gl.go) | 109 | 内部，可换 |
| [backend_gpu.go](backend_gpu.go) + [backend_gpu_shaders.go](backend_gpu_shaders.go) | 119 | 内部，GLSL |
| [scene3d/](scene3d/) (viewport/mesh/shader/lines) | 98 | **子包直接写 GL** |
| [media/gl_rect_darwin.go](media/gl_rect_darwin.go) | 51 | **子包直接写 GL** |
| [examples/customgl/](examples/customgl/) | 26 | **文档化的用户特性** |
| [webview/](webview/) | 经 `QueueGLDraw` | **子包依赖逃生舱** |

更硬的三处约束：

1. **`RasterBackend` 接口自己就漏了 GL**：[backend.go:56](backend.go:56) `CompositeToDefault(r *GLRenderer)` —— 现有的"backend 抽象"并不是 GPU-API 中立的，它是 GL 形状的。
2. **每个 app 都手写 `qui.NewGLRenderer()`**：所有 example 加上 [reactive/html/html.go:664](reactive/html/html.go:664) 的 `h.Mount`。GPU 选择被烘进了应用代码。
3. **逃生舱是公开承诺**：`GPUCanvas.QueueGLDraw` / `GLState` / `PhysicalScissor` / `ActiveGLRenderer().DrawTexture`（[gl_hooks.go](gl_hooks.go)），scene3d / media / webview / customgl 都在用。

所以 S2 的第一步不是"上 Metal"，而是**去 GL 化命名与抽象**：
- `NewGLRenderer()` → `qui.NewRenderer()`（按平台选最佳实现），旧名保留为 deprecated 别名；
- `RasterBackend.CompositeToDefault(*GLRenderer)` → `CompositeToDefault(Compositor)`；
- `QueueGLDraw(func(GLState))` → `QueueDeviceDraw(func(DeviceState))`，`PhysicalScissor` 改成设备中立；
- scene3d / media 的 GL 代码收进"GL 后端专属"文件，非 GL 后端上要么有对应实现、要么明确降级。

## 三、关键：把"先进"切成两级，成本立刻可控

### Level 1 — 只现代化 **present**（推荐先做，性价比极高）

CPU 光栅化不动（纯 Go，跨平台白拿），只把"把一张 `image.RGBA` 送上屏"这件事换成各平台原生：

| 平台 | present 方案 |
|---|---|
| macOS | `CAMetalLayer` + `IOSurface`（或 `CALayer.contents = CGImage`） |
| Windows | DXGI flip-model swapchain，或 `DirectComposition` |
| Linux | Wayland `wl_shm` / dmabuf；X11 fallback 走 XShm / Present 扩展 |

为什么值得先做：

- **默认路径彻底不需要 OpenGL 了。** 今天即使 CPU raster 也必须过 GL：[renderer_gl.go:145](renderer_gl.go:145) 画进 `image.RGBA`，[renderer_gl.go:188](renderer_gl.go:188) 再上传成纹理 blit——**每帧一次全屏纹理上传，只为把位图搬上屏**。Level 1 直接省掉。
- 拿到原生 present 语义：自适应刷新（ProMotion）、无 tearing、正确的 occlusion/节流。
- macOS 摆脱废弃的 `NSOpenGLContext`（GL 自 10.14 起废弃）。
- **每平台只要约 200–400 行**——你只是在传一张位图，没有 shader、没有资源模型、没有管线状态。

一句话：**Level 1 就能兑现 "每平台技术栈先进" 的绝大部分体感，而它的成本只是 Level 2 的零头。**

### Level 2 — GPU 光栅化每平台原生（贵，且是 opt-in，可无限期延后）

这才是 405 个调用点 + 三套 shader 语言（MSL / HLSL / SPIR-V）+ 三套资源模型的地方。两条路：

| 路线 | 说明 | 取舍 |
|---|---|---|
| 各写原生 | Metal / D3D12 / Vulkan 三套 | 最"先进"、最可控；三倍工作量与维护 |
| 中间层 `wgpu` | 一套 WGSL 打到 Metal/D3D12/Vulkan（wgpu-native 走 cgo） | 一套 shader 三家通吃；代价是引入预编译原生库、交叉编译变复杂、Go 绑定成熟度**需要先验证**（`cogentcore/webgpu` 等候选） |

因为 GPU raster 是 `QUI_GPU_RASTER=1` 的 opt-in，Level 2 可以：只做 1–2 个平台、或者干脆等 wgpu 路线验证完再一次性做。**不要把它排进多平台的关键路径。**

## 四、Seam 接口草图

放在 `internal/platform`，**必须是纯叶子包**（不 import root `qui`，自带 `Size`/`Key`/`Cursor` 等类型或用基本类型）——这样 root import 它不会成环，符合 CLAUDE.md "root 绝不 import 子包" 的规则精神（那条规则防的是循环，不是分层）。

```go
// internal/platform —— 不 import qui
type App interface {
    NewWindow(cfg WindowConfig) (Window, error)
    PumpEvents(timeout time.Duration) // 对应 WaitEventsTimeout / PollEvents
    Wake()                            // 对应 PostEmptyEvent（跨 goroutine 唤醒）
    Monitors() []Monitor
}

type Window interface {
    // 几何
    Size() (w, h int)            // 逻辑
    FramebufferSize() (w, h int) // 物理
    Scale() (x, y float32)       // 允许非整数（Wayland 分数缩放 / Windows 任意 %）
    SetTitle(string); SetSizeLimits(minW, minH, maxW, maxH int)
    Iconify(); Maximize(); Restore(); Focus()
    ShouldClose() bool; SetShouldClose(bool); Destroy()

    // 位置是"可选能力"——Wayland 没有全局坐标。ok=false 表示平台不支持，
    // 调用方必须有不依赖绝对坐标的退路（见 5.2）。
    Pos() (x, y int, ok bool)
    SetPos(x, y int) bool

    Caps() Caps // 装饰归属、是否支持绝对定位、剪贴板是否需要 serial …

    // 输入：一个 Handler 接口取代 10 个 callback setter
    SetHandler(Handler)
    CursorPos() (x, y float64)
    SetCursor(CursorShape)

    // 呈现：S2 的接口
    Surface() Surface

    // 逃生舱：NSWindow / HWND / wl_surface
    NativeHandle() unsafe.Pointer
}

// Surface 的关键设计：呈现是"异步取帧许可 + 提交"，不是同步 SwapBuffers。
// macOS 用 CADisplayLink、Windows 用 waitable swapchain、Wayland 用
// wl_surface.frame 回调都能自然实现；反过来（同步 Swap）在 Wayland 上是错的。
type Surface interface {
    // Ready 在"该画下一帧了"时收到一次信号。主循环 select 它而不是死等 60Hz。
    Ready() <-chan struct{}
    Begin(fbW, fbH int)
    PresentCPU(img *image.RGBA) // Level 1：提交一张位图
    End()
}

type Handler interface {
    OnMouseMove(x, y float64, mods Mods)
    OnMouseButton(btn Button, down bool, mods Mods)
    OnScroll(dx, dy float64, mods Mods, phase Phase) // ← mods + phase 从第一天就在契约里
    OnGesture(g Gesture)                             // ← 捏合/旋转，契约先行
    OnKey(k Key, scancode int, down, repeat bool, mods Mods)
    OnChar(r rune)
    OnFocus(bool)
    OnResize(w, h int)
    OnFramebufferResize(w, h int)
    OnRefresh()
    OnDrop(paths []string)
    OnClose()
}
```

平台 App 是进程级对象，由 qui 首次使用时初始化并常驻到进程退出；普通
`qui.App.Run` 只管理窗口和 App 自己的资源，不能终止全局 backend。

### 排序上的关键洞察

**手势（上一轮定的 M0/M1）应该作为 seam 的输入契约先行，而不是给 GLFW 打的补丁。**

`Handler` 从第一天就带 `OnGesture` 和带 mods/phase 的 `OnScroll`：
- **GLFW 后端"尽力实现"**——scroll 的 mods/phase 靠 swizzle `scrollWheel:`，捏合靠 `magnifyWithEvent:`（[ime_darwin.m](ime_darwin.m) 已证明这条路可行）；
- **原生后端天然完整实现**——因为那本来就是 NSEvent / WM_POINTER / `zwp_pointer_gestures_v1` 直接给的东西。

这样 M0/M1 的产出不会在换后端时被丢掉，顺序就对了。

## 五、决策：macOS 优先，Windows/Linux 只保接口

> 已定：Windows/Linux 暂不投入，只要求预留接口足够健壮；CI 暂不做；先在 macOS 把引擎做稳做全。

这个取舍是对的。但它带来一个具体风险，必须用具体手段对冲：

**只有一个实现的接口，一定会悄悄长成那个实现的形状。** 你不会在设计时发现，会在两年后写第二个后端时发现——那时接口已经有几十个调用方。

下面三条是"不写 Windows/Linux 也能保证接口健壮"的实际做法。

### 5.1 用 headless 后端当第二个实现（最重要的一条）

不做 CI、不做 Windows，但**做一个 `platform_headless`**：内存位图 + 脚本化事件注入，零系统依赖。

这是唯一能立刻暴露"接口里只有 Cocoa 才有的假设"的手段——**任何被 macOS 污染的方法，在 headless 上会立刻显得荒谬或无法实现**。而它顺带的收益大到本身就值得做：

- [testhelpers.go:319](testhelpers.go:319) 的 `NewTestWindow`（现在是 `&Window{lastSize: size}`，`handle == nil`）可以升级成**真后端**，测试从"容忍 nil 句柄"变成"跑完整管线"；
- 截图回归 / `QUI_GOLDEN=1` / htmlcss snapshot 测试不再需要真窗口；
- qui-agent 可以驱动一个真正 headless 的 app（对 AI-native 方向直接加分）；
- 将来真要做 CI，它已经就绪。

**成本极低**（present 就是"把位图存起来"，事件就是"注入"），**收益是接口设计的正确性**。这条建议替代原先的 "P0 = CI"。

### 5.2 接口按平台语义的**并集**设计，而不是按 Cocoa 设计

以下是三平台真正会打架的地方。它们不是"以后适配一下"，而是**现在就会强迫接口变形**——写的时候不知道，后面改起来很贵：

| 分歧点 | macOS | Windows | Wayland | 对接口的要求 |
|---|---|---|---|---|
| **全局窗口坐标** | 有 | 有 | **完全没有**（client 无法知道/设置自己的绝对位置） | `Pos()/SetPos()` **不能是必需能力**，必须可失败/可缺失。现在它被用在全屏保存恢复和多窗口定位上，需要改成"相对/由 WM 决定" |
| **呈现节奏** | CAMetalLayer + CADisplayLink | DXGI waitable swapchain | **必须等 `wl_surface.frame` 回调才画下一帧** | 呈现接口需要一个**异步 "可以画下一帧" 信号**，而不是同步 `SwapBuffers`。今天的 `SwapBuffers` + `WaitEventsTimeout(1/60)` 模型在 Wayland 上是错的——这是最该现在就修正的一条 |
| **DPI** | `backingScaleFactor`，基本是 1/2/3 | per-monitor v2，任意 100–350%，`WM_DPICHANGED` 还会**建议新窗口矩形** | `wp_fractional_scale_v1`，1/120 为单位的分数缩放 | 缩放必须是 `float32` 且允许**非整数**；DPI 变化事件要能携带"建议的新几何" |
| **子窗口 / popup** | 自由定位 | 自由定位 | 必须用 `xdg_positioner` **相对父窗口**锚定 | popup-as-window 方向要按"锚点 + 对齐 + 翻转规则"表达，不能按绝对屏幕坐标 |
| **剪贴板写入** | 随时可写 | 随时可写 | **需要输入事件的 serial**（无焦点写不了） | 写剪贴板必须**可返回错误**，且允许"必须由输入事件驱动" |
| **窗口装饰** | 系统提供 | 系统提供 | 可能没有服务端装饰（GNOME 就没有），要自绘或用 libdecor | 需要"谁画标题栏"的能力位 |
| **事件泵** | NSApp run loop，需主线程 | 有 modal loop（`WM_ENTERSIZEMOVE`、菜单跟踪） | fd 驱动，可 poll | `PumpEvents(timeout)` 这个签名是对的（Wayland 最自然，另两家用 timer 实现）；但"modal loop 中仍要能重绘"必须写进契约 |
| **DnD** | NSDragging | OLE | `wl_data_device` | 拖放要按"进入/悬停/放下 + 数据类型协商"抽象，不能只有 GLFW 那种 `drop(paths []string)` |

这张表就是"接口足够健壮"的验收标准：**每一条都能在接口上表达出来，而不是被 macOS 的做法挤掉。**

### 5.3 用能力查询取代 `ErrNotSupported` stub

现在 media / print / webview 都是 darwin-only + 编译 stub（[README.md:31](README.md:31)）。单平台阶段这没问题，但接口层要预留可查询的 capability，让上层能**提前**降级而不是调用后吃错误。否则将来每个功能点都要重新发明一次降级逻辑。

## 六、分期路线

按 "macOS 优先、只保接口" 的决定重排：

> **进度**：M0–M2（手势）、P1（seam）、P3b 接口半部（呈现 seam + 显示节奏）、**P3a（Cocoa 原生后端）**
> 已完成并实机验证。剩 **P3b 实现**（CALayer 直呈 + CADisplayLink）。
>
> 后端是**运行时**可选而非编译期二选一：`QUI_PLATFORM=glfw|cocoa`，默认仍是 glfw（见
> `platform_select.go`）。这样"新后端有没有回归"是一个 env var + 重跑，而不是重新编译；
> 两个实现都编译进去，谁也不会腐烂；用户机器上出问题时回退是一句支持指令而不是发版。
>
> 一处与本文档的偏离：seam 落在 **root 的未导出接口**（`platform.go` + `platform_glfw*.go`）而非
> `internal/platform` 叶子包。原因是叶子包要重复定义 `Key`(60+ 常量)/`Modifiers`/`MouseButton`/
> `CursorShape` 并双向翻译，约 150 行纯样板且每加一个键要改两处；而"接口够不够健壮"取决于**形状**
> （fallible `pos`、非整数 scale、异步取帧），不取决于它在哪个包。形状稳定之后再搬到
> `internal/platform` 是机械操作，先搬反而要改两次。
> 硬边界由两个替代物提供：`platform_containment_test.go`（扫描 root，禁止 `platform_glfw*` 之外
> import 窗口库）和 `platform_test.go` 的 `fakePlatformWindow`（第二实现，含 Wayland 形状与分数缩放用例）。

| 阶段 | 内容 | 验收 |
|---|---|---|
| **P1** ✅ | S1 seam 落地（按 5.2 的并集设计）+ `platform_glfw` 实现；`Handler` 含 gesture / scroll-mods 契约 | 行为零变化，全套测试绿；GLFW 从 10 个文件收缩到 3 个 |
| **P1.5** 部分 | 第二实现目前是测试内的 `fakePlatformWindow`（含 Wayland / 分数缩放形状）。完整的 `platform_headless`（`NewTestWindow` 改走它、截图回归无需真窗口）仍待做 | 接口已被证明没有 Cocoa 假设 |
| **P2** ✅ | M0/M1 手势：GLFW 后端用 swizzle 实现 `onGesture` + scroll mods/phase；testhelpers 注入口 + agent `pinch` action | q-word / q-excel 捏合缩放可用（实机验证）；qui-agent 能无头驱动 |
| **P3b-接口** ✅ | 呈现 seam：`platformSurface{ready, presentCPU, present}`、`CPUFrameSource`、`Window.present` 优先 CPU 路径、`frameDue` 按显示信号节奏、`wake` 下沉到 per-window | 行为零变化；顺带修掉 `PostJob` 会从任意 goroutine 懒初始化窗口库的崩溃 |
| **P3a** ✅ | **darwin 原生后端**（`platform_cocoa_darwin.go`/`.m`，~1050 行）：NSApplication + 手写事件泵、NSWindow/QuiView、macOS 虚拟键码表、光标、NSScreen 枚举、NSPasteboard、文件拖放、**原生手势响应方法（不再 swizzle）**、live resize / `windowDidChangeBackingProperties` | `QUI_PLATFORM=cocoa` 下 q-excel / reactive-html / 3dviewer 均正常；几何数值与 GLFW **逐字节一致**（含多显示器 top-left 换算、maximize/restore、DPR、剪贴板往返）；手势与菜单经 qui-agent 无头验证 |
| **P3b-实现** 待做 | CALayer/IOSurface 直呈 + CADisplayLink 驱动 `ready()`；GL 消费者（scene3d/media/webview/`QUI_GPU_RASTER`）保留 NSOpenGLContext 后备 surface | macOS 默认路径不再有 GLFW、不再有 OpenGL；每帧省一次全屏纹理上传 |

### P3a 的实现札记

- **窗口用整数 handle 寻址，不传指针**。cgo 禁止把 Go 指针存进 C，ARC 下手写 bridging cast 又是负债，
  所以 C 侧用一个 `NSMutableDictionary` 持有 AppKit 对象，两侧只交换 `uintptr_t`。唯一例外是
  `quiCocoaNSWindow`（IME 和原生全屏桥要真的 `NSWindow*`）。
- **`QuiView` 采用 `NSTextInputClient`，方法集与 `keyDown:` 顺序刻意对齐 GLFW 的 content view**
  （先发 key 回调，再 `interpretKeyEvents:`）。因为 `ime_darwin.m` 是按 `[window contentView]` 的
  **实际类**去 `class_addMethod` 的——形状对上了，IME 桥一行都不用改就继续生效。
- **键码表放在 Go 侧**（`cocoaKeyCodes`）而不是 C 里。它是纯数据，放 Go 就能直接单测；放 C 就只能靠
  跑起来一个窗口去观察。表里是**物理按键位置**（kVK_*），Dvorak / AZERTY 下 `0x00` 仍是 `KeyA`——
  这正是快捷键想要的语义。
- **`windowShouldClose:` 返回 NO 并只记标志**。让 AppKit 真的关窗会在 qui 跑 OnClose 回调期间把 GL
  context 抽走；qui 的主循环轮询 `shouldClose`、在窗口还活着时跑完 handler，再显式 destroy。
- **`platformSurface` 暂时两项都拒绝**（`ready()` 返回 nil、`presentCPU` 返回 false），present 就是
  `[ctx flushBuffer]`——与 GLFW 完全等价。打开它们是 P3b 实现的事，刻意分开做，这样这一步的回归只能
  归因于窗口/输入重写本身。
- **containment 从两侧守**：`platform_containment_test.go` 除了禁止 `platform_glfw*` 之外 import
  窗口库，还新增禁止 `platform_cocoa*` 之外调用 `C.quiCocoa*`。后者是必要的——原生后端没有 import
  路径可查，它的 C 入口对包内每个文件都可见，"就在这儿调一下 `quiCocoaNSWindow`" 是最容易的抄近路。
  注意这条比"backend 外不许用 Cocoa"窄：IME / 菜单 / 对话框 / 托盘 / 剪贴板 / 打印都合法地用 AppKit，
  它们通过 seam 的 `nativeWindow` 拿窗口，那是正门。

**尚未验证（需要人在键盘前）**：真实键盘输入与中文 IME 组词、拖窗口边缘的 live resize、把窗口拖到
另一块不同 DPI 的显示器、触控板捏合/旋转在 cocoa 后端下的手感。前三项的代码路径都写了，但都只能
交互式确认。用 `QUI_PLATFORM=cocoa` 跑任意例子对比 `QUI_PLATFORM=glfw` 即可。
| **P4** | 引擎在 macOS 上做稳做全（手势语义 B1 视口缩放、`Window.SetZoom`、其余引擎待办） | —— 这是当前主战场 |
| **—— 以下按需，暂不排期 ——** | | |
| P5 | Windows 后端（Win32 + DXGI）、Linux（Wayland + libdecor，X11 fallback） | GLFW 可整体删除 |
| P6 | Level 2：GPU raster 现代化（各写原生 or wgpu 一套打三家） | `QUI_GPU_RASTER=1` 在目标平台可用 |

P1 + P1.5 是"保证预留接口健壮"的全部内容，成本约一周，**做完之后 Windows/Linux 可以放心地无限期延后**——接口已经被第二个实现验证过，且 5.2 那张表的每一条都在契约里表达了。

P2 之后 GLFW 挡路的现实痛点（手势）解决；P3 之后 macOS 这个主平台完全现代化（无 GLFW、无 OpenGL、无废弃 API）。

## 七、隐藏成本清单（自研后端必须提前认账）

1. **IME 在非 macOS 上是真活**：Windows 要 TSF（或至少 `ImmGetCompositionString`），Linux/Wayland 要 `text-input-v3` + ibus/fcitx 协调。GLFW 在这块也不好用（macOS 已经靠 swizzle 绕过），所以自研不是"额外成本"，是"迟早要付"——但要知道它的量。
2. **live resize**：macOS 拖窗口边缘会进入嵌套事件跟踪循环。现在靠 [window.go:1251](window.go:1251) 在 `SetSizeCallback` 里直接 `Step()` 兜着；Windows 的 `WM_ENTERSIZEMOVE` 同类问题。
3. **HiDPI 动态变化**：窗口跨 Retina / 非 Retina 移动（`didChangeBackingProperties`）、Windows per-monitor DPI awareness v2、Wayland `wp_fractional_scale`。DPR 是渲染管线核心（[window.go:1120](window.go:1120)）。
4. **Wayland 的窗口装饰**：没有服务端装饰时要自己画（libdecor 或自绘标题栏）。
5. **键盘布局**：macOS virtual keycode 便宜（约 80 行表）；X11/Wayland 要处理 xkb。
6. **cgo 交叉编译**：三套原生后端意味着 CI 必须在各平台原生构建（不能靠交叉编译），这也是 P0 的一部分。
7. **睡眠唤醒 / run loop 节流**：仓库里 `QUI_SHUTDOWN_DEBUG`（[app.go:22](app.go:22)）就是为查这类问题存在的，自研会重新踩。

## 八、隐藏成本清单的调整

第七节那 7 条里，按 macOS 优先重新排优先级：

- **现在就要面对**：live resize（第 2 条）、HiDPI 动态变化（第 3 条）、睡眠唤醒（第 7 条）—— 这三条是 macOS 自研后端 P3 的核心难点。
- **接口要预留、实现可延后**：IME 的 Windows TSF / Linux text-input-v3（第 1 条）、Wayland 装饰（第 4 条）、xkb 键盘布局（第 5 条）。已有的 `IMEClient` 接口是对的抽象方向，只要别让它长出 Cocoa 专属方法。
- **暂时不用管**：cgo 交叉编译（第 6 条）—— 单平台阶段无关。

## 九、待拍板

1. **P1.5 的 headless 后端做吗？** 我建议做——它是"只有一个实现也能保证接口健壮"的唯一实际手段，而且顺带把 `NewTestWindow` 升级成真后端、让截图回归和 agent 无头驱动都受益。成本比 CI 低，收益比 CI 直接。
2. **`internal/platform` 作为纯叶子包这个约定接受吗？** 它是让 root 能 import 而不破坏 CLAUDE.md 分层规则的关键。
3. **`Surface.Ready()` 这个异步取帧模型接受吗？** 它会改动主循环（[app.go:162](app.go:162) `RunStep` 现在是 `WaitEventsTimeout(1/60)`）。现在改是顺手；等 macOS 后端写完再改就要动两处。
4. Level 2（GPU raster 现代化）走各写原生还是 wgpu —— **暂不需要决定**，等 macOS 引擎做稳、真有 GPU raster 需求时再 spike。
