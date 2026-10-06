# qui —— 架构

[English](architecture.md) · **中文**

本文描述引擎内部的运作方式。高层概览见 [overview.zh.md](overview.zh.md)；功能清单见 [features.zh.md](features.zh.md)。

## 分层与依赖方向

`qui` 是一个 module，依赖图有着严格的分层。

```
htmlcss ──► widgets ──► 根 qui ◄── 其他所有包
reactive/html ──► reactive ──► 根 qui
```

子包 import 根引擎；根引擎绝不 import 子包。根↔子包的耦合通过根包定义的接口进行（`qui.Animator`、`qui.IMEClient`、`qui.VectorSource`、`qui.Translator`，以及 focusable/tickable 约定）。`widgets` 除根包外什么都不 import；`reactive` 只 import 根包，与后端无关。允许的跨子包边很少且固定（`scene3d`→`widgets`，`htmlcss`→`widgets`+`svg`，`reactive/html`→`reactive`+`htmlcss`+`widgets`，`icons`→`svg`）。如果根包看起来需要某个子包符号，解法是把符号移过去，或在根包定义接口。

## 帧循环

引擎运行一个绑定在操作系统主线程上的单线程循环（`runtime.LockOSThread`）。每次 `Window.Step()`：

1. 排空 `PostJob` 队列；
2. 对每个 `Tickable` 调用 `Tick(now)`（返回的矩形并入 `dirtyRegion`）；
3. 若 `root.IsLayoutDirty()` 则重新布局；
4. **若 `dirtyRegion` 为空，直接返回、不绘制**；
5. 只清除并重绘脏区域；
6. 只把重绘过的像素上传到 GPU（`GLRenderer.SetDamage`），然后 swap buffers。

帧是按需调度的（`idle.go`），而不是由固定时钟驱动。每轮迭代之间，`App.Run` 询问每个窗口距离下一次 `Step` 还有多久，并在平台的事件等待中恰好阻塞这么长时间：

- **正在变化**——上一帧有绘制，或存在动画器、待处理 job、脏绘制区域或脏布局：按显示节奏（1/60 秒）唤醒。由于一次绘制会换来下一帧，靠每帧返回脏矩形来做动画的 `Tickable` 无需额外代码就能持续运行，并在其 `Tick` 不再报告变化时停止；
- **等待某个时刻**——在一段静默后才变化的 `Tickable`（光标闪烁）在 `Tick` 中调用 `Window.RequestTickAt(t)`；tooltip 的延迟也按同样方式跟踪：在最早的那个时刻唤醒；
- **空闲**——一直阻塞，直到操作系统事件或跨 goroutine 唤醒（`PostJob`、`WakeEventLoop`、菜单动作）。空闲的应用不消耗 CPU。

AppKit 可能在等待期间、没有任何输入事件的情况下投递的平台回调（Dock 最小化、全屏过渡结束、程序化移动）会提前结束等待，从而轮询类状态（`OnMove`、`OnMinimize`、全屏）依然能被察觉。`App.SetMaxIdleWait(d)` 为早于 `RequestTickAt` 的第三方 `Tickable` 限制最长睡眠时间；更推荐直接修正该 `Tickable`。

控件树、焦点、浮层、布局元数据、渲染状态和直接内省都属于这个 UI goroutine。跨 goroutine 的代码必须使用 `Window.PostJob` / `TryPostJob` / `PostPriorityJob`；agent 的读取侧代码使用 `*Synced` API。`QUI_DEBUG_THREAD=1` 为生产窗口打开快速失败线程归属检查（`Window.EnableUIThreadChecks` 可在测试或自定义宿主中选择性开启）。游离的控件可以在其他线程构造，但一旦挂载，其状态就归 UI 线程所有。

进程拥有恰好一个 `App` 和一个平台后端。第二次 `NewApp` 返回 `ErrAppAlreadyExists`。后端会保持初始化直到进程退出；`App.Run` 会销毁应用拥有的窗口和状态项，但绝不终止进程级的平台状态。同一个 `App` 在上一次 run 关闭所有窗口后，可以再创建一批窗口并再次运行。

### 失效与重绘作用域

核心不变量是：**没有失效就不绘制**。

- `Invalidate` / `InvalidateRect` 标记纯视觉变化。
- `InvalidateLayout` 标记影响尺寸的变化。它只把调用者的绘制范围标脏（而非整个窗口），并向根冒泡一个标志。布局过程随后仅当某个控件的包围盒真的改变时才提升为全量重绘（`BaseWidget.Layout` → `noteBoundsChange`）。`boundsEpsilon` 以下的亚像素抖动保持作用域内；在布局过程之外调用 `Layout` 会直接标脏新旧范围。
- 一次测量结果不变的文字 tick 只重绘自己的矩形，因此窗口在两次更新之间真正空闲，`WaitIdle` 也能对持续更新的应用工作。设 `QUI_DEBUG_PAINT=1` 可记录全量重绘提升以及第一个移动的控件。

文字测量是带缓存的（`text.go` 中的 `BuildTextLayout` 缓存，在字体注册表变化时失效），因此任何布局失效触发的全树重新布局，对未变化的文字而言只是 map 查找，而不是重新测量。

## 控件树与 `BaseWidget.self`

所有控件都实现 `Widget`（`widget.go`）；`BaseWidget` 提供默认实现，被到处内嵌。Go 的内嵌没有虚派发，因此 `BaseWidget` 带一个 `self Widget` 字段，在构造后立刻通过 `SetSelf(outer)` 设置 —— **构造 `Container` 子类后一定要调用 `SetSelf`**，否则事件分发会漏掉祖先。`Container` 是唯一的树组合类型；子节点通过 `ChildList()` 暴露给框架遍历。

## 事件分发

分发是 DOM 风格的三阶段：捕获（根→目标的父节点）、目标、冒泡（父节点→根），并支持 `StopPropagation`。`Handle` 返回 `true` 即隐式停止。额外规则：

- `MouseDown` 时捕获鼠标 —— `MouseMove`/`MouseUp` 路由到被捕获的控件；
- 通过对比分发路径合成 hover 进入 / 离开；
- `MouseDown` 与 Tab 循环更新焦点；
- 拖放以 4px 死区合成；
- 浮层自上而下命中测试，`Modal()` 浮层会困住焦点。

### 坐标空间

每个控件收到的鼠标、手势、拖拽事件都位于**它自己的坐标空间** —— 即其 `Bounds()` 所在的空间 —— 因此它总能拿事件坐标与自己的几何比较（`event.go` 中的 `eventInWidgetSpace`；未变换的树中是恒等）。

当某个祖先变换控件时，控件自己的空间与窗口空间不同：

- CSS `transform`（`InteractionTransformer`）移动的是控件自己的盒子；
- 滚动容器（`ChildInteractionTransformer`）移动后代但不移动容器自身。`widgets.ScrollView` 把内容**只布局一次**在视口原点，然后通过 transform 滚动，因此滚动是 O(1)，子控件的包围盒与滚动无关。

用 `WindowPointToLocal` / `LocalPointToWindow` / `InteractionBoundsOf`（`widget.go`）跨越边界。任何把几何交给「位于窗口空间」的东西的代码 —— 浮层 / 弹出锚点、IME caret 矩形、tooltip 锚点、AX 与 agent 包围盒 —— 都必须使用屏幕上的形式，而不是裸的 `Bounds()`。后代失效通过 `ChildPaintTransformer` 映射出去，并被 `PaintClipper` 裁剪，因此滚出视口的控件不会标脏任何东西。

## 渲染

渲染是 Skia 形状的，分为两层。

**Canvas 前端**（`render.go`、`canvas_state.go`、`canvas_paint.go`、`canvas_layer.go`、`path.go`）：Save/Restore 状态栈（裁剪 + 2×3 仿射矩阵），`DrawShape(Shape, Paint)` 是唯一入口（便捷方法是包装），`Path` 支持二次 / 三次贝塞尔、抗锯齿的 winding/even-odd 填充、描边 cap/join/miter，`SaveLayer` 支持混合模式与 `Paint.Alpha`，`ClipPath`（在 Restore 时遮罩），`Paint.Shader`（线性 / 径向渐变），`ColorFilter`/`ImageFilter`（投影、模糊），以及用于 elevaton 阴影的 `DrawShadow`。

**RasterBackend**（`backend.go`）：前端之下产出像素的抽象。所有坐标都已是物理坐标，每个方法都收到一个它必须遵守的裁剪。`backend_cpu.go` 是参考实现；`backend_gpu.go`（FBO + 着色器，`QUI_GPU_RASTER=1`）接入同一个位置，并按图元与 CPU 路径交替。`renderer_gl.go` 负责 GL 窗口 blit。

### 文字

文字使用 `go-text/typesetting` 的字形串整形（`text_shaping.go`），支持 OpenType GSUB/GPOS、Unicode bidi、UAX #14 换行和 UAX #29 字素簇边界。测量、CPU/GL 绘制、PDF 输出、换行、命中测试、选区和编辑器 caret 共享同一套整形簇。`RuneAdvance` 仅作为独立 rune 工具的兼容助手保留。带样式的 run 在仅绘制边界的跨越中保持整形上下文。

行内流 —— 文字与原子行内盒在同一行上按基线对齐共享 —— 位于 `inline.go`，并以 `widgets.InlineBox` 的形式暴露。

GL 控件通过 `GPUCanvas.QueueGLDraw` + `ActiveGLRenderer().DrawTexture` 合成（`scene3d` 与 `media` 使用）；`qui.PhysicalScissor` 把逻辑裁剪转换成 GL scissor 盒。

## 布局

引擎：`FlexLayout`（与 CSS 对齐的 flexbox）、`GridLayout`、`FlowLayout`、`AbsoluteLayout`。有两条与 CSS 一致、但不知道就会踩坑的规则：

1. **`Grow > 0` 且未设 `Basis` 时隐含 `Basis = 0`**（CSS `flex: <grow>`）；否则按内容全量测量的子节点会饿死固定尺寸的兄弟。
2. **`Shrink` 默认是 1**；用 `NoShrink: true` 关闭。

用 `w.SetFlex(grow)` 表示「占满剩余空间」。`Style.Margin` 在 `FlexLayout` + `Container.Measure` 中具有 CSS margin-box 语义（Grid 与 Absolute 忽略它）。用 `QUI_DEBUG_LAYOUT=1` 或调试浮层排查溢出。

每个子节点的布局元数据是受控状态：用 `FlexItemValue` 配合 `SetFlexItem`/`UpdateFlexItem`、`GridItemValue` 配合 `SetGridItem`/`UpdateGridItem`、`AbsolutePositionValue` 配合 `SetAbsolutePosition`/`UpdateAbsolutePosition`。旧式的指针 getter 仍为兼容保留，但直接改它们不会触发布局失效。

## 视口缩放

浏览器风格的页面缩放：`Window.SetZoom` / `ZoomIn` / `ZoomOut` / `ResetZoom` / `SmartZoom`。缩放会缩小**内容视口**（`windowSize / zoom`）并按同一系数缩放画布，因此控件树会**重新排版** —— 文字重新换行、flex 重新分配、百分比按更小的视口解析 —— 和浏览器里的 Cmd+/Cmd− 一样，而不像那种缩放成品像素、需要平移的放大镜。

要知道两件事：

1. **`Window.Size()` 是内容视口**，不是操作系统窗口。每个控件侧的调用者（对话框居中、弹出测量、菜单夹紧、CSS 的 `vw` 单位）想要的是它所在的空间。只有当你指的是窗口本身时（把它放到某台显示器、持久化几何）才用 `WindowSize()`。100% 时两者相等。`DevicePixelRatio()` 仍是显示器的真实比例；`EffectiveScale()` 是 `dpr × zoom`，负责把控件坐标转换成像素。
2. **缩放是可选开启的**，用 `SetViewportZoomEnabled(true)` 打开 Cmd/Ctrl+`=`/`-`/`0`、触控板捏合和双指双击。默认关闭，因为已经自行解释捏合的应用（图表缩放某个轴、画布应用缩放内容）否则会得到第二个互相竞争的缩放。消费掉手势的控件获胜；只有无人认领的捏合才缩放页面。

输入会在 `window_handler.go` 中一次性转换成视口空间，因此分发、命中测试、`actions.go` 和 agent 都看不到缩放。缩放吸附到 2.5% 的步长 —— 文字测量按字号缓存，连续的捏合否则会每帧错过缓存。

## 主题

一个全局的设计 token `Theme`，刻意与具体设计系统无关：五级表面阶梯（`Surface` / `SurfaceSunken` / `SurfaceRaised` / `SurfaceStrong` / `SurfaceOverlay`）、`Text` / `TextMuted` / `TextSelection`、`Accent` 家族、`Border` / `BorderStrong` / `BorderFocus`、语义色 `Error` / `Warning` / `Success`、六级 `Elevation` 阴影阶梯、hover/focus/pressed/dragged 覆盖不透明度，以及间距 / 圆角 / 字体 / 过渡比例尺。`ThemeFont(role)` 在该字体比例尺上解析六种中性文字角色（`TextBody`、`TextLabel`、`TextHeading` 等）。`SetTheme` 会使订阅的窗口失效；控件在 `Draw` 里实时读取 `CurrentTheme()`。

**qui 只有一套配色：浅色。** 引擎和 `widgets` 中都没有深色主题，也没有任何明暗分支 —— 深色界面是应用层的事，在 htmlcss 层用 CSS 表达（见 `examples/reactive-html`，它切换 `.dark` class）。同样地，也不捆绑设计系统：给 `Theme` 重新着色就是换一套品牌色，而「有设计感」的外观（Material、Fluent、你自己的）是一张样式表，不是 token 包。

## 国际化

根包拥有一个很小的接缝；目录与 CLDR 机制位于 `i18n`，根包从不 import 它 —— 与 `qui.Animator` / `IMEClient` / `VectorSource` 的布置相同。`widgets`、`htmlcss` 和 `reactive` 都通过 `qui.Translate` 拿到译文，因此不产生新的 import 边。

```go
i18n.Install()                       // 载入内建 qui.* 字符串，注册 Translator
i18n.Load(myLocales, "locales")      // 添加应用的目录（go:embed）
qui.SetDefaultLocale(qui.SystemLocale())
```

`qui.SetDefaultLocale` 就是整个语言切换：它递增 `LocaleGeneration()`、丢弃文字布局 memo，并触发每个窗口和样式引擎的订阅。**不会重建任何东西** —— 控件在 Measure/Draw 中解析消息 key，htmlcss 在重样式时重新解析 `data-i18n`，因此切换的代价是一次重新布局，而不是一次 reconcile。

需要内化的就是「延迟解析」这一条。qui 是保留模式，所以构造时捕获的字符串（`NewButton(i18n.T("save"), …)`）会冻结在当时的语言。请改成设置 KEY：

```go
btn.SetTextKey("qui.save")                       // 原生控件
h.Button("Save").T("qui.save")                   // reactive/html DSL
<button data-i18n="qui.save">Save</button>       <!-- htmlcss 标记 -->
```

字面量保留为回退（也作为代码内文档）：目录里缺条目时渲染 "Save"，而不是 "qui.save"。`QUI_I18N_STRICT=1` 会把它翻转成可见的 `⟦qui.save⟧`，让缺口在开发时无处遁形。

查找走三层 —— 请求 locale 的回退链、当前默认 locale 的、再到 `FallbackLocale()`（字符串撰写时所用的 locale，默认 "en"）。复数使用 `x/text` 的真实 CLDR 类别，所以俄语有四种形式、阿拉伯语有六种；`if n == 1` 在大多数语言里都是错的。

`i18n.Printer`（来自 `i18n.In(loc)`）格式化数字、百分比、货币、日期、相对时间和列表，并给出 `collate.Collator` —— 任何面向用户的排序都该用它，因为按字节序在德语里会把 "Ä" 排到 "Z" 之后。

**agent 选择器与 i18n：** 翻译过的标题会破坏 `[name="Save"]`。每个带 key 的控件都会把消息 key 发布为 AX 节点的 `nameKey`，选择器语法用 `[key=qui.save]` 匹配它 —— 与 locale 无关，因此一个脚本能在所有语言里驱动应用。任何必须经受语言切换的东西，优先用 `#id` 或 `[key=]` 而不是 `[name=]`。

工具：`go run ./cmd/qui-i18n extract` 扫描 Go 源码（`i18n.T`、`SetTextKey`、`h.T`、标记里的 `data-i18n`）并更新源目录，且绝不覆盖已有译文；`lint` 报告缺失 / 未使用 key、占位符漂移和复数形状错误；`pseudo` 生成一个带重音、长 40% 的 locale，在译者介入之前就暴露硬编码字符串和裁切。

**RTL 现状：** 文字完全 bidi —— 整形、caret、选区、`TextAlignStart/End` 都按段落方向解析，`Locale.Direction()` 报告方向。**布局镜像尚未实现** —— flex 主轴、grid 列序、滚动条一侧、弹出锚定和对话框按钮顺序仍是物理的。`examples/i18n` 切到阿拉伯语能清楚看出哪些可用、哪些不可用。

## 平台桥

平台代码按 build tag 拆分为 `*_darwin.go`（+ `.m`/`.cc`）与 `*_other.go` —— IME（`NSTextInputClient` → `IMEClient` 控件）、原生对话框、emoji（Core Text 位图）、剪贴板、系统托盘、原生菜单。各平台 API 必须一致；非 darwin 得到可编译的 stub。`IsCommandMod` 抽象 Cmd 与 Ctrl 的区别。

**客户端标题栏**（`window_titlebar*.go`）：`Window.SetTitlebarStyle(TitlebarOverlay)` 把内容区延伸到透明的系统标题栏之下（macOS `FullSizeContentView`，因此缩放边缘 / 红绿灯按钮 / 全屏都保持原生）。`TitlebarInsets()` 报告系统窗口按钮所需的留白，`BeginWindowDrag()` 把进行中的按下交给窗口管理器（绝不要用鼠标位移来移动窗口 —— 那会丢失吸附，在 Wayland 上也不可能）。在不支持处各自返回 false / 零值，因此应用会保留原生标题栏而不是崩掉。htmlcss 把 CSS `app-region: drag|no-drag` 映射到它上面；`examples/chrome-tabs` 就建立在两者之上。

**浮层面板**（`window_overlay.go`）：`App.NewOverlayPanel(w, h)` 创建一个 `WindowOverlayPanel` —— 无边框、透明、始终置顶、且**永不获取键盘焦点**的操作系统窗口。这是一种独立的 `WindowKind`，不是关掉系统装饰的普通窗口，因为三个属性共同承担关键作用：不激活（点击它不得让用户正在输入的应用失去激活）、透明帧缓冲（面板画自己的圆角卡片，其余部分透出合成）、以及在所有 Space（包括全屏应用）之上置顶。面板初始隐藏；`ShowAt(x, y)` 先定位**再**显示（反过来的顺序会在旧位置闪一帧），`Hide()` 只是 ordered out 而不销毁 —— IME 候选条每敲一个键就切换一次，而这个频率下重建 OS 窗口加 GPU 上下文太慢。窗口的 kind 驱动 `Window.Step` 中的透明清除和 `GLRenderer.SetTransparent`（后者还切换到 `BlendFuncSeparate`，避免直通 alpha 在合成时被平方）。只有 `cocoa` 后端实现它（`NSPanel` + `NSWindowStyleMaskNonactivatingPanel`）；GLFW 返回 `ErrOverlayPanelUnsupported`，而不是悄悄交回一个会抢焦点的窗口。`examples/overlay-panel` 是示例。

## AI 原生内省与可操作性

根包文件：

- `roles.go` —— 角色常量，以及可选的 `Roled`/`Named`/`Valued` 接口；
- `accessibility.go` —— AX 树（浮层是平级的顶层子树）；
- `selector.go` —— CSS 属性风格的选择器语法（`#id`、`[role=]`、`[name*=]`、`:nth`、`:visible`、`:focused`、`:layer(modal)`）；
- `snapshot*.go` —— 缩放与带标注的 PNG；`SnapshotScaled/Region/Annotated` 通过主线程任务队列串行化，因此 agent goroutine 的截图绝不会与进行中的帧竞争（不要在活动窗口的主 goroutine 里调用它们）；
- `actions.go` —— 按选择器定位的同步 Click/Type/Drag/…，遵守模态阻塞与滚动到可见；
- `wait.go` —— `WaitIdle`、`WaitOverlay`；
- `recording.go` —— 事件监听器；
- `jobs.go` —— `PostJob` 主线程队列。

在任意 qui 应用中按 `Cmd/Ctrl+Shift+A` 可切换 agent 叠加层，无需改代码。`SetRoot` 之后调用 `agent.BindEnv(window)`，当 `QUI_AGENT=1` 时启动 HTTP 服务；`GET /llm.txt` 教会 agent 完整的线缆界面。新的控件 AX 覆写放在各子包的 `accessibility.go` 中。

## 应用开发界面：htmlcss 与 reactive

### htmlcss

CSS 引擎（解析 → 层叠 → `ComputedStyle`）和唯一的控件装配体 —— 活动元素 `El` —— 位于两个入口之后。

- **`El` + `StyleEngine`**（`el.go` / `engine_live.go`）：保留式元素控件。每个 `El` 内嵌 `widgets.Box`，同步维护一个 `*Node` 镜像（使选择器层叠能匹配），并在 class/attr/text/children 变化时就地重算 CSS。表单标签通过后备控件渲染；`overflow:auto/scroll` 托管一个 `ScrollView`；`<img>` 从 `src` 载入光栅 / svg（BaseDir 选项）；ul/ol 里的 `<li>` 渲染成悬挂的 `[marker | content]` 行，遵循 `list-style-type`；`<a>` 在点击时打开 href（元素或折进的 span，经 `InlineBox.LinkSourceAt`，它同时给出折进的 `<a>`）；图标叶子渲染 `qui.VectorSource`；拖拽重排内建。样式 applier（`applyBox`/`applyCommon`/`applyTextStyle`/`chooseLayout`/`gridLayout`）是 `apply.go` 中的自由函数。
- **重样式是子树作用域的**（`engine_live.go`）：每次变更记录自己的元素；合并后的 flush（每帧一次，经 `PostJob`）从每个脏元素的父节点向下重样式 —— 这就是完整的波及范围，因为选择器语法没有面向父节点的选择器（父节点覆盖自身、后代，以及 `~`/`+` 兄弟的连带影响）。嵌套作用域会被剪枝；portal/dialog 根独立重样式。`Restyle()` 仍是挂载时使用的全文档过程。
- **行内格式化**（`El.buildFlow`）：连续的行内级子节点归组为匿名 run —— 每个 run 是一个 `widgets.InlineBox`，其中文字片段和纯文字行内元素（span/b/em/a/…）折成带样式的 Unicode 整形 run（UAX #14 换行、bidi 视觉序、共享基线、href span），而需要真实控件行为的子节点（处理器、拖拽、`#id`、视觉盒子、图标、表单控件、`inline-block`）作为仍能命中测试的原子行内盒同行。块级子节点堆叠在 run 之间（CSS 匿名块行为）。`InlineBox` 实现 `qui.TextSelectable`，因此折进的文字参与跨控件选择。`El.AccessibleName` 包含折进 span 的文字。
- **一次性：** `Render(html, css, opts)` / `RenderDoc` 把解析后的 DOM 编译成静态 `El` 树（`static.go`）并重样式一次。`RenderResult` 按 id/class 索引每个元素 —— 结果像任何 `El` 树一样可变、可重样式。

后备原语契约：htmlcss 通过 `widgets.Box`、`Label`、`InlineBox`、`Rule`、`Input`、`TextArea`、`CheckBox`、`Select`、`ScrollView`、`Image` 渲染（`reactive/html` 还会用 `MenuItem`/`ShowContextMenu`）。把这些控件的 API 当作稳定契约 —— 改动会悄悄破坏 CSS 渲染；改动后运行 `go test ./htmlcss ./reactive/...`。

### reactive

一个运行时，两套引擎（深入解析见 [reactive/README.zh.md](../reactive/README.zh.md)）：

| 引擎 | 管什么 | 触发方式 | 开销 |
|---|---|---|---|
| Reconcile | 结构（挂载 / 卸载、带 key 的列表） | `UseState` / setState | 作用域内的树 diff |
| Signal | 高频值 + 结构快路径 | `Signal.Set` | 直接调控件 setter / 局部子节点同步 —— **不跑 render** |

- 元素种类：host、component（`Component[P]`）、`Fragment`、context provider、`Portal`/`PortalWith`（`ModalPortal` = 对话框外壳）、`ErrorBoundary`、`Empty()`、绑定节点（`Show`/`For`）。
- Hooks：`UseState(Fn)` / `UseRef` / `UseMemo` / `UseCallback` / `UseReducer` / `UseEffect(Once)` / `UseSignal` —— 按 fiber 位置确定，顺序必须稳定。
- Signals：`Signal[T]`、`Map`/`Computed`（通过拦截 Get 自动追踪；`Peek` 不追踪）、`BindWidget`（signal → setter，突发合并，不 reconcile）。signal 的标识必须在控件整个生命周期内稳定。
- `Show`/`For` 直接从 signal 挂载 / 卸载子树，其 reconcile 只作用于该节点。`For` 的行需要稳定的 key。
- Props 相等性是 `valuesEqual`（函数按代码指针比较），因此处理器必须通过函数式更新器或 props 读取最新状态，绝不能捕获 render 时的局部变量。
- **宿主层状态搭载在 Runtime 上：** `Runtime.SetHostData`/`HostData` + `reactive.CurrentRuntime()`（在每次 render/reconcile/scoped-sync 过程中设置）。元素 Create 钩子就是这样解析每个 runtime 的后端 —— 绝不用包级全局（会破坏多窗口）。

### reactive/html

流式 HTML 元素 builder 降解为 `htmlcss.El` 承载的 reactive Element。这套界面刻意保持规整：

- **每个标签都是 `h.Tag(...any) *Builder`。** 参数按类型分类：`string` 是文字，`Node` 是子节点，这些的切片原地摊开，数字格式化为文字，`qui.VectorSource` 是图标，`nil` 被丢弃，其他任何东西都会 panic 并指明标签、参数下标和类型。与元素子节点混排的文字降解为匿名 `#text` 段，从而折进一个行内 run；纯文字内容则保持为元素自身的文字。四个不承载文字的标签把字符串当作自己的载荷读取：`img`→src、`input`/`textarea`→value、`select`→options。
- **没有降解仪式。** `reactive.Element` 满足 `h.Node`，因此组件就是普通的 `func(...) h.Node`。`h.Component` / `h.Leaf` 包装 reactive 构造函数。
- **组件可以写成 HTML。** `h.MustParse` 一次性编译标记片段（通常用 `//go:embed`）；`Template.Bind(h.Scope{…})` 每次渲染填充它。这里刻意没有表达式语言：一个洞只对应扁平 `Scope` 里的一个 key。`{name}` 插值；`:if` `:key` `:class` `:text` `:value` `:checked` `:selected` `:disabled` `:options` `:draggable` `:icon` `:ref` 各取一个 Scope key；`@click` `@input` `@commit` `@toggle` `@select` `@contextmenu` `@keydown` `@drop` `@dragover` `@dragend`（及其余）取一个处理器。Scope 值若是 signal 则绑定而非插值（`BindText`、`BindClass`、`Show`），因此更新跳过 render。结构规则与 htmlcss 的静态构建共享，因此一个模板和同样标记经 `htmlcss.Render` 会产出相同的树。错误携带模板行号加元素路径；`Template.Names()` 列出模板需要的 Scope key。
- `h.Mount(window, css, app)` 创建样式引擎、存到 Runtime 上、渲染并接好引擎根 —— 每个窗口拥有自己的引擎；多窗口绝不共享样式状态。
