# qui —— 功能

[English](features.md) · **中文**

这里列出当前已经具备的能力。想了解它是怎么实现的，见 [architecture.zh.md](architecture.zh.md)；权威的 HTML/CSS 矩阵见 [../htmlcss/COVERAGE.zh.md](../htmlcss/COVERAGE.zh.md)。

## 引擎（根 `qui`）

**窗口与生命周期**

- `App` 拥有平台后端；`App.NewWindow` / `App.NewOverlayPanel`；`App.Run` 处理 SIGINT/SIGTERM 关闭；`App.SetKeepAlive`（窗口全关后仍运行的托盘应用）与 `App.Quit`。
- 关闭否决：`Window.OnCloseRequest`（返回 false 保留窗口 —— “保存更改？”流程）、`RequestClose`，以及不可否决的 `Close`。
- 生命周期回调：`OnActivate`、`OnMove`、`OnMinimize`、`OnFullscreenChange`、`OnClose`（都返回注销函数）；附属子窗口（`SetOwner`）与 sheet（`ShowAsSheet`，macOS）。
- 每个应用多个窗口；游离窗口可以在其他线程构造。
- 每个窗口的渲染器选择、设备像素比处理和内容视口缩放。
- 窗口模式：普通、全屏、浮层面板（不激活、透明、始终置顶 —— cocoa 后端）。
- 客户端标题栏：`TitlebarOverlay`、`TitlebarInsets` 与窗口管理器拖拽。
- 显示器枚举与几何持久化辅助。

**事件分发**

- DOM 风格三阶段分发（捕获 → 目标 → 冒泡），支持 `StopPropagation`。
- 鼠标捕获、合成 hover 进入 / 离开、点击聚焦 + Tab 循环、4px 死区拖放。
- 每个控件自己的坐标空间，配 `WindowPointToLocal` / `LocalPointToWindow` / `InteractionBoundsOf`。
- 修饰键抽象（`IsCommandMod`）以及命名键 + 文本 / 字符事件。
- 手势：捏合、旋转、双指双击；精确的滚轮阶段与修饰键。
- 模态浮层困住焦点，自上而下命中测试，并且隔离键盘：按键和窗口快捷键都到不了 modal 背后的界面。
- `Window.AddEventFilter`：在分发前看到（并可吞掉）每个事件 —— 命令面板、模态按键层、宏录制。
- 应用自定义事件：`CustomEvent` + `DispatchCustomEvent`，像输入事件一样走捕获 → 目标 → 冒泡。
- `MouseEvent.Clicks`（双击 / 三击计数）、`MouseEvent.ReleasedOver`（在控件上按下、拖到别处松开即取消点击），以及每个 `BaseWidget` 都有的 `OnFocus` / `OnBlur` / `OnKeyDown` 钩子，都返回注销函数。`OnKeyDown` 先于焦点导航收到 Tab。
- 快捷键：`CmdOrCtrl` 记号、`Bind` / `Unregister`、条件绑定（`BindIf`，不可用时跳过，按键继续传递）、按控件作用域绑定（`RegisterScoped`，优先于窗口级绑定，并在包含它的 modal 内保持有效）；后注册者优先。`MenuBar.BindAccelerators` 把菜单快捷键并入已有注册表并保持同步；禁用项不触发。
- 拖放：带类型的载荷（`DragData`、`DragDataProvider`）、`DragEnter` / `DragLeave`、`DropAcceptor` 接受 / 拒绝、拖拽图像，Esc 取消拖放，系统文件拖放优先交给 `Droppable` 控件。
- 可选的焦点策略：`ClickFocusPolicy`（可以 Tab 到达，但点击不转移焦点）与 `TabStopper`（可以点击聚焦，但 Tab 跳过，用于可选中的文字）。
- 浮层内容变化时会自行重新布局（`OverlayLayouter`）；`TickWidget` 会穿过未实现 Tickable 的容器继续传递帧 tick。
- 浮层分层（`PushOverlayLayer`：toast 始终在之后打开的 modal 之上，tooltip 在最上）与退出动画（`OverlayExiter`）。
- 可选控件约定都有导出的接口名：`ChildLister`、`ModalOverlay`、`FocusVisibleAware`、`WindowAware`、`LayoutDirtyMarker`；`QUI_DEBUG_WIDGETS=1` 会报告漏调的 `SetSelf`。

**布局**

- `FlexLayout`（与 CSS 对齐的 flexbox）、`GridLayout`、`FlowLayout`、`AbsoluteLayout`。
- flex / 容器测量中的 CSS margin-box 语义。
- 受控的逐子节点元数据：`SetFlexItem` / `SetGridItem` / `SetAbsolutePosition`。
- 自定义布局引擎通过 `LayoutMeasurer` 报告固有尺寸；`Window.AfterLayout` 在绘制前基于最终几何执行代码。
- shrink-to-fit 测量；grid 跨行；溢出调试浮层（`QUI_DEBUG_LAYOUT=1`）。

**渲染**

- `Canvas` 前端：Save/Restore 状态栈（裁剪 + 2×3 仿射矩阵）、`Path` 支持二次 / 三次贝塞尔、抗锯齿 winding/even-odd 填充、描边 cap/join/miter。
- `SaveLayer` 支持混合模式与 `Paint.Alpha`；`ClipPath`；线性 / 径向渐变着色器；`ColorFilter` / `ImageFilter`（投影、模糊）；`DrawShadow`。
- `RasterBackend` 接缝：纯 Go 的 CPU 参考实现（`backend_cpu.go`）与可选 GPU 后端（`QUI_GPU_RASTER=1`，`backend_gpu.go`）。
- 脏区域绘制，`Invalidate` / `InvalidateRect` / `InvalidateLayout` 及包围盒变化提升。
- 离屏渲染与回读；PDF 画布后端；SVG 画布辅助。
- GL 逃生舱：`GPUCanvas.QueueGLDraw`、`GLState`、`ActiveGLRenderer().DrawTexture`、`PhysicalScissor`。

**文字**

- `go-text/typesetting` 字形串整形：OpenType GSUB/GPOS、Unicode bidi、UAX #14 换行、UAX #29 字素簇边界。
- 测量、绘制、PDF、换行、命中测试、选择和编辑器 caret 共享同一套整形簇。
- 行内流（`inline.go`、`widgets.InlineBox`）：文字与原子行内盒在共享行上按基线对齐。
- 富样式 run（`text_rich.go`）、跨控件文字选择、CJK 换行与 emoji。

**主题**

- 全局设计 token `Theme`：表面阶梯、文字角色、强调色、边框、语义色、高度阴影、状态不透明度、间距 / 圆角 / 字体 / 过渡比例尺。
- `SetTheme` 失效；实时读取 `CurrentTheme()`；供缓存使用的 `ThemeGeneration()`；可退订的 `SubscribeTheme`。用基线颜色构建的原生控件会跟随主题切换。只有一套浅色配色；深色界面用 CSS。
- 每个窗口可配置 tooltip（`SetTooltipStyle`：颜色、字体、内边距、宽度、显示延迟、宽限期）。

**国际化接缝**

- `Locale`、`Direction`、`Translate`、`LocaleGeneration`、`SetDefaultLocale`、`SubscribeLocale`。
- 区域感知的文字测量缓存 key；`Translator` 接口接入 `i18n` 包。

**线程与任务**

- `PostJob` / `TryPostJob` / `PostPriorityJob` 主线程队列；`IdleState`。
- `QUI_DEBUG_THREAD=1` 快速失败 UI 线程归属检查。
- `Tickable` / `Animator` / `Focusable` / `IMEClient` 约定；`Window.RequestTickAt` 供需要唤醒空闲循环的 Tickable 使用（按需帧调度，`App.SetMaxIdleWait`）；注册后长期不产生绘制的动画器会被降为 4 Hz 轮询并记录日志。

**AI 原生内省**

- 无障碍树，含角色、名称、值、包围盒与消息 key。
- CSS 属性风格的选择器语法（`#id`、`[role=]`、`[name*=]`、`[key=]`、`:nth`、`:visible`、`:focused`、`:layer(modal)`）。
- 按选择器定位的动作（点击、输入、拖拽、滚动……），遵守模态阻塞与滚动到可见。
- 缩放 / 区域 / 带标注的 PNG 截图，通过主线程任务队列。
- `WaitIdle` / `WaitOverlay`；事件录制；`Cmd/Ctrl+Shift+A` 打开窗口内 agent 叠加层。

## `widgets`

- 结构：`Box`、`Container` 布局、`Rule`、`FieldSet`、`Anchor`、`ScrollView`（内容尺寸自动测量）、`ListView`（虚拟化；工厂行只在可见时构建）、`TableView`（单元格渲染器、列对齐、点击表头排序且经 `RowKeyModel` 保持选中记录、工厂行）、`TabView`（图标、徽标、可关闭与禁用的标签、按内容宽度的标签与可滚动溢出、切换否决）。列表与表格的颜色取自主题，可逐字段覆盖（`RowColors`；`NoStripe` / `NoHover` 关闭对应装饰）。零配置构造的控件（Input、TextArea、Select、TabView、MenuBar 等）跟随 `SetTheme`。
- 文字：`Label`、`RichText`、`InlineBox`；文字选择与复制。
- 输入：`Input`、`TextArea`（均支持剪贴板、IME 与撤销 / 重做）、`CheckBox`、`RadioButton` / `RadioGroup`、`Switch`、`Slider`、`Select`（值 / 标签分离的选项，支持图标与分组、自定义选项行、打开 / 关闭事件）。
- 反馈：`Progress`、`Tooltip`。
- 浮层：`Popup`（关闭原因 + 否决、点击穿透、自动聚焦、贴边约束；被外部移出浮层栈后能自行复位）、`Dialog`、`MenuBar`（左右键切换菜单）、`ContextMenu`、`MenuItem` 图标 / ID / 自定义内容，可键盘导航的面板行（`MenuActivatable`）。`Dialog` 的按钮行可以放任意控件，支持 `CanClose` 否决关闭、`OnClose(reason)`、Enter 触发 `DefaultAction`、`InitialFocus`、自定义 `Header`，颜色取自主题 token。
- 媒体：`Image`（光栅 + 矢量）。
- i18n：所有带文字的控件（标签页、列、菜单项、标签、fieldset 标题、tooltip）都有在 Measure/Draw 中解析的 `TextKey` 式 key，以及 `AccessibleNameKey()`。
- 无障碍：名称考虑 label、`SetAccessibleName` 覆盖、自绘的标签页 / 列表行 / 表格单元格作为 AX 子节点发布、Tab 时把获焦控件滚动到可见。
- 文本控件撤销历史（`undo.go`）与可复用的选择模型。

## `htmlcss`

**HTML 标签：** 结构 / 语义容器（`div`、`section`、`article`、`header`、`footer`、`nav`、`main`、`aside`、`p`、标题、列表、`table`/`tr`/`td`/`th`/`caption`/`colgroup`、`pre`、`code`、`blockquote`、`hr`、`br`、`span`、`b`、`em`、`strong`、`i`、`u`、`a`、`img`、`svg`、`canvas`、各种 `input`、`textarea`、`select`/`option`、`button`、`label`、`fieldset`、`datalist`、`template`）。不支持的标签会被解析并跳过。

**CSS：** 完整选择器集，含交互状态（`:hover`、`:focus`、`:focus-visible`、`:checked`、`:disabled`、`:enabled`、`:required`、`:optional`、`:read-only`、`:read-write`）、`var()` + `:root`、简写属性、逐边边框的盒模型、背景色 / 渐变、box-shadow、opacity、transform、`position:relative`、overflow（`auto`/`scroll` 由 `ScrollView` 托管）、`display:flex`/`grid`、列表标记、text-decoration/transform/overflow、white-space、`overflow-wrap`/`word-break`、`scrollbar-color`。原生弹出层（select、datalist、调色板、tooltip）跟随 `--popup-*` / `--tooltip-*` 自定义属性。

**扩展点：** `RegisterElement`（由原生控件承载的自定义标签）、`RegisterProperty` + `ComputedStyle.Property` / `Var`（支持 CSS 全局关键字）、`El.SetOnStyle`、感知样式的画布绘制（`SetCanvasPaint`）、`StyleEngine.SetLinkHandler`（能拿到被点击的 `<a>`，折叠进行内文本的也一样）。`h.Mount` 的首轮渲染在其 effect 执行前就已完成样式计算。

**事件与交互：** 单击 / 双击（带位置与 `PreventDefault`）、带捕获的指针按下 / 移动 / 松开、hover、焦点（`El.RequestFocus`、`autofocus`；`<button>` 可以 Tab 到达，Enter/Space 按下）、键盘（作者回调只在目标 / 冒泡阶段执行）、`pointer-events:none`、滚轮、拖拽重排（`Draggable`/`DragHandle`/`OnDrop`/`OnDragOver`）、元素级文件拖放与粘贴钩子、`<a>` 链接激活、`app-region: drag|no-drag`。

**两个入口，一个装配体：** 活动元素 `El` + `StyleEngine`（保留式、子树作用域重样式），以及一次性的 `Render` / `RenderDoc`（把解析后的 DOM 编译成静态但仍可变的 `El` 树）。

**已知缺口：** CSS 过渡 / 动画、静态 `Render` 下运行时切换 `display:none`、原生日期 / 时间选择器、真正的校验伪类、`::placeholder`。

## `reactive` 与 `reactive/html`

- 负责结构的 reconciler：`UseState`、`UseReducer`、`UseRef`、`UseMemo`、`UseCallback`、`UseEffect(Once)`、`UseLayoutEffect`（布局后、绘制前）、`UseId`、`UseResource`（可取消的异步加载）、`UseSignal`、`UseContext`；带 key 的列表；错误边界；portal，包括锚定的 popover（`PortalAnchored`）。
- 负责高频更新的 signal 引擎：`Signal[T]`、`Map`/`Computed`、`BindWidget`、`Show`/`For` 绑定节点 —— 全部跳过 render 过程。
- `reactive/html`：几乎所有常用标签的流式 builder，以 HTML 名字命名的链式 prop，`h.If`/`h.Show`/`h.For`/`h.Each`/`h.Frag`，浮层（`h.Portal`/`h.ModalPortal`/`h.ModalPortalWith`/`h.Popover`/`h.ContextMenu`），跟随最新一次渲染的 ref（`.Ref`、`.RefTo`）、`h.Canvas`、`h.Leaf`（承载在可样式化元素里的原生控件）、指针 / 点击 / 文件拖放 / 粘贴 / 表单提交 builder，`h.Dialog` 对话框外壳（头部 / 正文 / 按钮行，类名为 `.q-dialog-*`，默认外观由框架层 CSS 提供），`h.Mount`。portal 内容从声明它的位置继承样式：`.app.dark .q-dialog` 能匹配，自定义属性也能继承。
- HTML 组件：`h.MustParse` / `MustParseSet` 编译标记片段；`Template.Bind(Scope)` 填充它们；signal 值的洞绑定而非插值；错误携带模板行号。

## `i18n` + `cmd/qui-i18n`

- 消息目录（JSON），通过 `golang.org/x/text` 使用 CLDR 复数类别。
- 区域敏感格式化：数字、百分比、货币、日期、相对时间、列表连接、排序。
- `i18n.Install`（内建 `qui.*` 字符串）、`i18n.Load`（应用目录）、`i18n.In(loc)` 打印器。
- CLI：`extract`（扫描源码、更新目录且不覆盖已有译文）、`lint`（缺失 / 未使用 key、占位符漂移、复数形状）、`pseudo`（带重音、长 40% 的伪 locale）。

## `agent` + `cmd/qui-agent`

- 基于引擎内省原语的 HTTP + SSE 服务；默认 Unix socket，可选 TCP + bearer token。
- 端点：`GET /tree`、`/screenshot`、`/diagnostics`、`/health`、`/wait`、`/console`、`/events`（SSE）、`POST /act`，以及 `/llm.txt` 和 DOM/reactive 界面（`/dom`、`/dom/styles`、`/dom/act`、`/reactive`）。
- `agent.BindEnv(window)` 由环境变量门控；`cmd/qui-agent` CLI（`tree`、`click`、`rightclick`、`type`、`wait`、`pinch`、`shot`、`raw`）。

## 辅助包

| 包 | 亮点 |
|---|---|
| `anim` | `Tween[T]`、`Spring`、`Timeline`；缓动目录；满足 `qui.Animator` |
| `graphs` | Line / Scatter / Bar / Area / Pie 系列，数值 + 类别轴，图例、tooltip、平移 / 缩放、主题调色板 |
| `svg` | SVG 1.1 子集：解析 / 构建 / 光栅化（可染色）/ 序列化；`Document` 满足 `qui.VectorSource` |
| `scene3d` | 场景图（节点、mesh、材质、灯光、相机）、`Viewport` FBO 控件、轨道控制、拾取、`Vec3Field` |
| `media` | 不依赖 ffmpeg 的 `AudioPlayer`（服务）与 `VideoView`（控件）；macOS 使用 AVFoundation 后端 |
| `webview` | 基于 CEF 的内嵌浏览器控件，受 `webview_cef` 门控；CPU 帧路径可用，GL/IOSurface 路径待实现；JS↔Go 桥只传 JSON |
| `physics` / `p2` / `p3` | 与维度无关的词汇，以及 API 对称的 2D/3D 引擎：扫掠 AABB、空间哈希、接触标志、单向平台、传感器 |
| `icons` | 约 135 个 Material Symbols 描边字形，为 `*svg.Document` |
| `fonts/jetbrainsmono` | 内嵌 JetBrains Mono；`Use()` 替换全局默认字族 |

## 工具

- `cmd/qui-agent` —— agent 命令行客户端。
- `cmd/qui-i18n` —— 目录提取、检查与伪 locale 生成。
- `examples/` —— 可运行的示例，包括范例 `reactive-html`、htmlcss 展示（`html-css`）、i18n、chrome-tabs、overlay-panel、graphs、svg、media、scene3d 等。
