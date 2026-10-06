# qui

[English](README.md) · **中文**

`qui` 是一个保留模式（retained-mode，而非即时模式）的 Go GUI 框架，基于 GLFW 3.3 + OpenGL 3.3 Core，并自带光栅化器 —— 一个 CPU 参考后端，以及通过 `QUI_GPU_RASTER=1` 开启的可选 GPU 光栅后端。它是单一的 Go module：`github.com/qizhanchan/qui`，根目录的 `package qui` 就是引擎。

**战略方向：用（轻量 HTML + CSS）+ Go 来开发应用。** 支撑它的技术栈是：

- `htmlcss` —— 一个小型 HTML5 + CSS 引擎，把标记与样式表转换成 qui 控件树，并提供可保留、可重样式的活动元素模式（`El` + `StyleEngine`）；
- `reactive` —— 声明式运行时（「React 形状的 API，Solid 形状的引擎」）：结构变化走 reconciler + hooks，细粒度更新走 signal，完全跳过 render 过程；
- `reactive/html` —— 把两者粘合起来的 DSL：组件返回 `h.Div(...)` / `h.Button(...)` 控件树，用普通 CSS class 描述样式。

`go run ./examples/reactive-html` 是这个方向的范例。原生 `widgets` 包在这套界面之下越来越多地扮演渲染 / 控件原语层（见下面的 backing-primitives 约定），同时仍然可以直接使用。

## 文档

| 文档 | 内容 |
|---|---|
| [docs/overview.zh.md](docs/overview.zh.md) | qui 是什么、它解决的问题、设计原则、项目状态 |
| [docs/architecture.zh.md](docs/architecture.zh.md) | 引擎内部：帧循环、控件树、事件分发、布局、渲染、文字、主题、i18n、平台接缝、内省 |
| [docs/features.zh.md](docs/features.zh.md) | 引擎、widgets、htmlcss、reactive、i18n 以及各辅助包的功能清单 |
| [reactive/README.zh.md](reactive/README.zh.md) | reactive 运行时深入解析 |
| [htmlcss/COVERAGE.zh.md](htmlcss/COVERAGE.zh.md) | 权威的 HTML/CSS 能力矩阵 |
| [agent/llm.txt](agent/llm.txt) | agent 线缆规范（运行中的应用也会在 `/llm.txt` 提供） |

顶层文档都同时提供英文版；中文版与英文版并列，文件名以 `.zh.md` 结尾。

## 项目状态

- **Module：** `github.com/qizhanchan/qui`（Go 1.25）。
- **平台：** macOS 是完整支持的平台。Linux / Windows 可以编译并运行核心功能，但原生对话框、IME、emoji 与系统托盘目前仅支持 macOS。
- **渲染：** CPU 光栅器是默认实现，且是纯 Go；GPU 光栅后端（`QUI_GPU_RASTER=1`）为可选开启。两者位于同一套 `Canvas` / `RasterBackend` 接缝之后。
- **窗口：** 默认后端是 GLFW；macOS 上还编译进了原生 AppKit（`cocoa`）后端，在运行时选择。
- **它不是一个完整的浏览器，也不是一套完整的设计系统。** `htmlcss` 是有意为之的子集，qui 也只提供一套浅色的设计 token 主题 —— 真正「有设计感」的样式属于样式表。

## 快速开始

```go
package main

import (
	"log"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

func main() {
	app, err := qui.NewApp()
	if err != nil {
		log.Fatal(err)
	}
	window, err := app.NewWindow("Hello", 480, 320)
	if err != nil {
		log.Fatal(err)
	}
	window.SetRenderer(qui.NewGLRenderer())

	root := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Vertical, Gap: 12},
		widgets.NewLabel("Hello, qui."),
		widgets.NewButton("Quit", func() { app.CloseWindow(window) }),
	)
	window.SetRoot(root)

	app.Run()
}
```

HTML/CSS 这套界面大致长这样（摘自 `examples/reactive-html`）：

```go
rt := h.Mount(window, css, func() h.Node {
	n, set := reactive.UseState(0)
	return h.Div(
		h.H1("Counter"),
		h.Button(fmt.Sprintf("count: %d", n)).OnClick(func() { set(n + 1) }),
	)
})
```

## 包地图

| 包 | 角色 |
|---|---|
| 根 `qui` | 引擎：窗口 / 帧循环、三阶段事件分发、布局引擎（Flex/Grid/Flow/Absolute）、Canvas + RasterBackend 渲染、Path/Paint/Shape、SaveLayer/混合/着色器/滤镜、主题 token、Unicode 整形文字 + 行内流（IFC）、跨控件文字选择，以及始终开启的 AI 内省界面（AX 树、选择器、截图、动作、等待、事件录制、主线程任务队列） |
| `widgets` | 原生控件：Box、Label、InlineBox、RichText、Anchor、Button、Input、TextArea、CheckBox、RadioButton/Group、Switch、Slider、Progress、Select、ScrollView、ListView、TableView、TabView、Popup、Dialog、MenuBar/ContextMenu、Image、Rule、FieldSet。另有 `undo.go`（文本控件撤销历史） |
| `htmlcss` | HTML + CSS 引擎（详见下文） |
| `reactive`、`reactive/html` | 声明式运行时 + 类 Web DSL（详见下文） |
| `icons` | 可选图标集：约 135 个精选 24px 描边字形（Material Symbols，Apache 2.0），解析为 `*svg.Document`，可传给任何接受 `qui.VectorSource` 的位置。纯资源 —— 根包与 `widgets` 都不依赖它 |
| `agent` + `cmd/qui-agent` | 基于根包内省原语的 HTTP/SSE 线缆层（默认 UDS，可选 TCP + bearer）以及配套命令行客户端。`agent` 保持轻薄 —— 所有原语都在根包 |
| `scene3d` | 3D 数学 + 场景图 + `Viewport` 控件（离屏 FBO，经 `QueueGLDraw` 合成）；`Vec3Field` 与 widgets 集成，因此 `scene3d` 可以 import `widgets` |
| `anim` | `Tween[T]` / `Spring` / `Timeline` + 缓动目录；都满足 `qui.Animator` |
| `graphs` | 2D 图表：Line/Scatter/Bar/Area/Pie 系列、ValueAxis/CategoryAxis、Legend、平移 / 缩放 |
| `svg` | SVG 1.1 子集：解析 / 程序化构建 / 光栅化（可染色）/ 序列化；`Document` 满足 `qui.VectorSource` |
| `media` | `AudioPlayer`（服务）+ `VideoView`（控件），不依赖 ffmpeg；平台后端隐藏在内部接口之后（目前为 macOS，其他平台返回 `ErrNotSupported`） |
| `webview` | 基于 CEF 的内嵌浏览器控件，受 `webview_cef` build tag + 拉取的 SDK 门控；默认构建不包含 CEF。JS↔Go 桥只传 JSON。.app 打包流程见 `webview/scripts/` |
| `physics`、`physics/p2`、`physics/p3` | 游戏物理：与维度无关的词汇在根包；2D 与 3D 引擎 API 对称（扫掠 AABB、空间哈希、接触标志、单向平台、传感器）。零依赖 —— 连根 `qui` 都不依赖 |
| `i18n` + `cmd/qui-i18n` | 消息目录（JSON，CLDR 复数）+ 区域敏感格式化（数字 / 货币 / 日期 / 相对时间 / 列表 / 排序），通过 `qui.Translator` 接缝接入根包。CLI 可从源码提取 key、检查目录、生成用于布局测试的伪语言 |
| `fonts/jetbrainsmono` | 内嵌字体；`jetbrainsmono.Use()` 替换全局默认字族（所有字重） |

**严格的依赖方向：** 子包 import 根 `qui`；根包绝不 import 任何子包。根↔子包的耦合通过根包定义的接口进行（`qui.Animator`、`qui.IMEClient`、`qui.VectorSource`、`qui.Translator`，以及 focusable/tickable 约定）。允许的跨子包边：`scene3d`→`widgets`，`htmlcss`→`widgets`+`svg`，`reactive/html`→`reactive`+`htmlcss`+`widgets`，`icons`→`svg`。`widgets` 除根包外什么都不 import —— 菜单里的对勾 / 折角是几何绘制，而不是从 `icons` 拉取。（`i18n` 只 import 根包 + `golang.org/x/text`；其他包一律通过 `qui.Translate` 拿译文，绝不 import `i18n`。）**`reactive` 自身只 import 根包 —— 它后端无关；请保持这一点。** 其余包保持隔离。如果根包看起来需要某个子包符号，就把符号移过去，或在根包定义接口。

## 常用命令

GLFW 需要原生头文件（macOS 上 `brew install glfw`；Linux 上需要 X11/Wayland 开发包）。

```bash
go build ./...                              # 编译全部
go test ./...                               # 全部测试
go test ./htmlcss ./reactive/...            # 单个包
go test -run TestName ./widgets             # 单个测试
go vet ./...

# 应用开发界面
go run ./examples/reactive-html             # reactive + htmlcss + h DSL（范例）
go run ./examples/html-css                  # 一次性 htmlcss.Render 展示
go run ./examples/chrome-tabs               # 标题栏内的浏览器风格标签条
go run ./examples/i18n                      # 运行时切换 7 种语言、CLDR 复数、RTL

QUI_PLATFORM=cocoa go run ./examples/overlay-panel   # 不激活、透明的浮层

# 其他示例：3dviewer animation customgl debug-layout dialog filedialog
# flowchart graphs layouts listview media-audio media-video multiwindow
# multiwindow-sessions popup svg systray text textarea textfield theme
# webview（webview 需要 CEF 构建 —— 见 webview/scripts）

# 从外部驱动运行中的应用（AI 原生可操作性）
QUI_AGENT=1 go run ./examples/reactive-html &
go run ./cmd/qui-agent tree                 # 自动发现 socket；另有：
go run ./cmd/qui-agent click '[role=button][name="New"]'
go run ./cmd/qui-agent shot /tmp/ui.png
# 或者直接：curl --unix-socket $TMPDIR/qui-agent-*.sock http://./llm.txt
```

常用环境变量：`QUI_PLATFORM=glfw|cocoa`（窗口后端 —— 见下）、`QUI_GPU_RASTER=1`（GPU 光栅后端）、`QUI_AGENT=1` / `QUI_AGENT_TCP=:port` / `QUI_AGENT_TOKEN` / `QUI_AGENT_SOCK`（agent 服务）、`QUI_DEBUG_LAYOUT=1`（flex 溢出日志）、`QUI_DEBUG_PAINT=1`（记录全量重绘提升 + 第一个移动的控件）、`QUI_DEBUG_LAYOUT_CACHE=1`（每次命中测量缓存都重新测量，并记录未对自身调用 `InvalidateLayout` 就改变了尺寸的控件）、`QUI_DEBUG_THREAD=1`（UI 线程归属快速失败检查）、`QUI_DEBUG_GESTURE=1`（记录 macOS 桥装了哪些手势选择器，含是否保留 GLFW 的 scrollWheel:）、`QUI_I18N_STRICT=1`（未解析的 key 渲染为 ⟦key⟧ 并各记录一次）、`QUI_GOLDEN=1`（写入 golden 截图）、`QUI_HTMLCSS_SNAPSHOT=1`（htmlcss 快照 PNG）。

**窗口后端（`QUI_PLATFORM`）。** 操作系统窗口层位于平台接缝（`platform.go`）之后，编译进了多个实现，在*运行时*选择：

| 取值 | 后端 |
|---|---|
| `glfw`（默认） | GLFW 3.3 + 一个 macOS 桥，用 swizzle 接入手势和精确的滚轮修饰键 |
| `cocoa`（darwin） | 原生 AppKit —— 我们自己的 NSApplication 事件泵、NSWindow/NSView，手势是真正的 responder 方法 |

两者的几何结果完全一致（经过数值验证，包括多显示器坐标、最大化和 DPR），因此切换后端是一次对比而非迁移：

```bash
QUI_PLATFORM=cocoa go run ./examples/reactive-html
```

默认停留在最有验证的后端，而不是最新的后端。无法识别的名字会报错并列出可用项 —— 打错字不应表现为「什么都没变」。

**shell 驱动的测试中的进程生命周期：** `go run` 不会把信号转发给子进程 —— 需要 `kill` 真正的 GLFW 宿主时，请始终用 `go build -o /tmp/app ./... && /tmp/app &`。`App.Run` 通过正常的关闭路径处理 SIGINT/SIGTERM（卸载 agent socket、运行 `OnClose` 回调）。

## 引擎架构

单线程循环，绑定在操作系统主线程上。每次 `Window.Step()` 会：排空任务队列 → tick 动画器 → 若有布局脏则重新布局 → **仅当脏区域非空时**才清除并重绘该区域。控件树、焦点、浮层、布局元数据和渲染状态都属于这个 UI goroutine；跨 goroutine 的代码走 `Window.PostJob` / `TryPostJob` / `PostPriorityJob`，agent 的读取侧代码走 `*Synced` API。

渲染是 Skia 形状的两层结构：上层的 `Canvas` 前端（状态栈、`Path`、`Paint`、着色器、滤镜、`SaveLayer`）位于下层产出像素的 `RasterBackend` 之上（`backend_cpu.go` 是参考实现，`backend_gpu.go` 可选）。文字使用 `go-text/typesetting` 的字形串整形，支持 OpenType GSUB/GPOS、Unicode bidi、UAX #14 换行和 UAX #29 字素簇边界；测量、CPU/GL 绘制、PDF 输出、换行、命中测试、选区和编辑器 caret 共享同一套整形簇。

布局引擎有 `FlexLayout`、`GridLayout`、`FlowLayout`、`AbsoluteLayout`。有两条与 CSS 一致、但不知道就会踩坑的规则：`Grow > 0` 且未设 `Basis` 时隐含 `Basis = 0`；`Shrink` 默认是 1（用 `NoShrink: true` 关闭）。

完整内容见 [docs/architecture.zh.md](docs/architecture.zh.md)：帧循环、事件分发与坐标空间、视口缩放、主题、i18n、平台桥，以及 AI 原生内省界面。

## 应用开发界面：htmlcss + reactive

`htmlcss` 通过唯一的装配体 —— 活动元素 `El` —— 把 HTML + CSS 解析成控件树。它支持完整的选择器集（含交互状态）、`var()` + `:root`、简写属性、逐边边框的盒模型、阴影 / 渐变 / 透明 / transform、`position:relative`、overflow、`display:flex/grid`、列表标记、text-decoration/transform/overflow 与 white-space。重样式是子树作用域的。行内文字折进 `widgets.InlineBox` 里的 Unicode 整形 run，因此换行后的文字仍可参与跨控件选择。

`reactive` 在一个运行时上跑两套引擎：负责结构的 **reconciler**（`UseState`、带 key 的列表）和负责高频值的 **signal** 引擎（`Signal.Set` 直接调用控件 setter，跳过 render 过程）。`reactive/html` 是流式 DSL —— 每个标签都是 `h.Tag(...any) *Builder`，组件也可以写成 HTML 片段，用 `h.MustParse` 编译后通过扁平的 `Scope` 每次渲染填充。`h.Mount(window, css, app)` 为每个窗口接好样式引擎。

功能清单见 [docs/features.zh.md](docs/features.zh.md)，reactive 运行时深入解析见 [reactive/README.zh.md](reactive/README.zh.md)。

## AI 原生内省 + 可操作性

每个 qui 应用都内置一套无障碍 + 可操作的界面：读出控件树（角色 / 名称 / 值 / 包围盒），并按选择器派发真实的点击 / 输入 / 滚动事件 —— 既可以在进程内调用，也可以通过 `agent.BindEnv(window)` 走 HTTP + SSE。`agent` 服务默认使用 UDS（可选 TCP + bearer），并在 `/llm.txt` 提供完整线缆规范。

```go
import "github.com/qizhanchan/qui/agent"

if srv, err := agent.BindEnv(window); err == nil && srv != nil {
	window.OnClose(func() { _ = srv.Stop() })
}
```

带 `QUI_AGENT=1` 运行，用 `cmd/qui-agent` 驱动（`tree`、`click`、`type`、`wait`、`shot` 等）。在任意 qui 应用中按 `Cmd/Ctrl+Shift+A` 可切换窗口内叠加层，显示每个控件的 ID / 角色 / 名称，无需改代码。

## 约定

- **新代码放哪里：** 引擎改动 → 根包（仅当它影响引擎而非单个控件时）。新控件 → `widgets/`（文件以控件命名）。CSS 特性 → `htmlcss`（在共享 applier 中实现，让两条路径都受益；同时补一个引擎测试，视觉相关时再补快照测试）。
- **面向用户的字符串：** 绝不在 `widgets/`、`htmlcss` 或引擎中硬编码。用 `qui.TOr(key, literal)`，这样没有目录的应用不受影响；给任何带文字的控件加一个在 Measure/Draw 时解析的 `TextKey` 式字段（见 `widgets/i18n.go`）。标题来自 key 的控件必须实现 `AccessibleNameKey()` —— 否则 `[key=]` 选择器会悄悄漏掉它。
- **依赖 locale 的缓存：** 任何缓存测量 / 整形结果的代码，key 里都要带上 `LocaleGeneration()`，就像 `FontRegistryGeneration()` 那样。reactive 运行时 → `reactive`；DSL 界面 → `reactive/html`。AX 覆写 → 各子包的 `accessibility.go`。HTTP 端点 → `agent/`（原语本身放根包）。平台代码遵循 `_darwin.go`/`_other.go` 拆分 —— 绝不把 darwin 代码散落到带 tag 和通用的文件里。
- `widgets/` 文件使用 `import . "github.com/qizhanchan/qui"`（点 import）—— 这是唯一被允许的位置，其他位置一律正常限定包名。点 import 不会绕过未导出字段：用访问器（`Bounds()`、`Style()`、`Enabled()`）。
- Container 子类构造后必须调用 `SetSelf`。
- 测试使用根包 `testhelpers.go` 的 fixture：`NewTestWindow`、`NewMouseEvent`/`NewKeyEvent`/`NewCharEvent`/`NewScrollEvent`、`RecordingCanvas`、`NewImageCanvas`、`DrainJobsForTest`。测试模式窗口内联执行动作（无需帧泵）。
- 做动画的控件实现 `Tickable`；文本输入实现 `IMEClient`；可聚焦控件实现 `Focusable() bool` + `SetFocused(bool)`。
- 依赖：`go-gl/gl`、`go-gl/glfw`、`go-text/typesetting`、`srwiley/rasterx`、`golang.org/x/{image,net,sys,text}`。

`CLAUDE.md` 和 `AGENTS.md` 是指向本文件的符号链接，因此编码 agent 读到的是同一个首页。

## 许可证

基于 MIT 许可证发布 —— 详见 [LICENSE](LICENSE)。

```text
MIT License

Copyright (c) 2026 qizhanchan

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```
