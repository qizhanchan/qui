# 扩展性与完备性审计

[English](extensibility-audit.md) · **中文**

这份文档记录：一个在独立 Go 模块里基于 qui 开发的应用，在哪些地方会被卡住。被卡住指的是只能 fork 控件、复制未导出的代码，或者接受改不了的行为。

审计覆盖三块：引擎扩展点（根包）、原生 `widgets`，以及应用开发层（`htmlcss` + `reactive` + `reactive/html`）。设计上明确不做的事情不在范围内，包括非 macOS 平台的原生集成，以及 [htmlcss/COVERAGE.md](../htmlcss/COVERAGE.md) 里列出的非目标。

审计的起因很具体：在另一个项目里自定义 dialog 按钮太费劲。下面的第一轮修掉了这个问题，也修掉了排查过程中发现的 bug；路线图是剩下的问题，按优先级排列。引用一律写成「文件 + 符号」，不写行号，这样代码修改后引用仍然有效。

## 第一轮：已完成

### 引擎（根包）

| 问题 | 修复 |
|---|---|
| 一个容器只要自己实现了 `Tickable`，就只会 tick 那些同样实现了 `Tickable` 的直接子节点。Dialog 内容里的 `ScrollView`，或任何第三方包装控件，会把其下的光标闪烁和 hover 过渡全部冻结 | 导出 `qui.TickWidget`；`Dialog`、`ListView`、`TableView` 改为通过它递归 |
| overlay 从不重新布局：显示期间内容变大，仍沿用 show 时的尺寸；切换主题或语言也传不到已打开的弹层 | 新增 `OverlayLayouter`。`Step` 会对布局变脏的 overlay 重新布局，`Window.InvalidateLayout` 也会把 overlay 标脏。`Dialog` 和 `Popup` 都实现了这个接口 |
| overlay 的绘制剔除用的是 `Bounds()`，阴影晕圈会漏重绘 | 改用 `PaintBoundsOf`；`Popup` 会上报内容的晕圈范围 |
| 按键会穿透 modal：没有焦点时落到主树 root，窗口快捷键（如 Cmd+S）在确认框背后照样触发 | 目标落在顶层 modal 背后的按键，改投给该 modal；有 modal 时窗口快捷键暂停响应 |
| 点击可聚焦的按钮，会把焦点从它要操作的输入框抢走 | 新增 `ClickFocusPolicy`（`FocusOnClick() bool`） |
| 每个可选中的文字 Label 都是一个 Tab 停靠点 | 新增 `TabStopper`。`Label`、`InlineBox` 和 portal host 仍可点击聚焦，但 Tab 会跳过它们 |
| `font-family: sans-serif`（以及任何未注册的字体族）下，粗体渲染成常规字重 | 未知字体族回落到默认字体族，并保持请求的字重 |

### `widgets`

| 问题 | 修复 |
|---|---|
| Dialog 的问题集中在一处：`Dialog.Buttons []*Button` 只能放按钮；`AddButton` 点完必定关闭；标题只能是字符串；Esc 必定关闭；没有默认按钮；颜色是构造时拷贝的字面值；内容会盖住按钮 | 重新设计 `Dialog`：`Actions []Widget`、`AddAction`、`CanClose` 否决、`OnClose(reason)`、`DefaultAction`（Enter）、`InitialFocus`、`DismissOnBackdrop`、`ActionsAlign`、`Header` 槽位、`SetTitleKey` + `AccessibleNameKey`，以及导出的 `DialogMetrics`。颜色改为绘制时读主题 token，内容区域加裁剪 |
| `Input` 即使与 Enter、Esc 无关也会吞掉它们，导致焦点在输入框里时，对话框的默认动作和 Esc 都失效 | 没有设置 `OnSubmit` 时 Enter 继续冒泡（对应 HTML 的隐式提交）；没有选区可清除时 Esc 继续冒泡 |
| `Slider` 没有用代码设置值的方法 | 新增 `Slider.SetValue` |

### `htmlcss` / `reactive` / `reactive/html`

| 问题 | 修复 |
|---|---|
| 每个对话框都要手写标题和按钮行 | 新增 `h.Dialog(DialogProps)`：头部（标题 + 可选的 ×）、正文、按钮行，类名固定为 `.q-dialog-*`。Esc、点遮罩、点 × 都会关闭；没人处理的 Enter 触发确认 |
| `h.ModalPortal` 的遮罩和关闭方式写死在代码里 | 新增 `h.ModalPortalWith(ModalOptions)`，可配置遮罩、遮罩点击 / Esc / Enter 回调和无障碍名称。`reactive.PortalOptions` 新增 `OnEnter`、`Role`、`Label` |
| 框架组件的默认外观没有地方放，放了应用又覆盖不掉 | 新增 `htmlcss.RegisterFrameworkCSS`：以类名为键的规则，层级与浏览器默认样式（UA）相同（层叠 tier 0），任何作者规则都能覆盖 |
| portal 在捕获阶段（事件从外往里传、尚未到达目标时）就吃掉 Esc；El 的键盘回调也在捕获阶段执行，结果祖先节点抢在获得焦点的元素之前处理了它的按键 | 两者都只在目标阶段和冒泡阶段处理，与 DOM 的顺序一致 |
| `<button>` 无法获得焦点，Enter / Space 也按不下去 | 推按钮可以用 Tab 到达，Enter / Space 能按下；鼠标点击不会转移焦点 |
| 没有用代码设置焦点的方式，也不支持 `autofocus` | 新增 `El.RequestFocus()`、`autofocus` 属性和 `Builder.Autofocus()` |
| portal 内容自成样式根：`.app.dark .dialog` 匹配不到，自定义属性也传不进去，暗色模式下对话框仍是浅色 | 新增 `El.SetStyleParent`，把 portal 内容挂到声明它的元素下面。这是一个仅用于样式的父节点：祖先选择器和继承都生效，兄弟选择器把它当作唯一子节点，`:root` 不再匹配它。由 reconciler 自动建立这层关联 |
| 居中显示的 portal 内容比窗口高时，标题会被推出屏幕 | 把内容的左上角限制在屏幕内 |

## 路线图

优先级说明：**P1** 会阻断常见的应用写法；**P2** 是确实存在的缺口，但有绕过办法。

### 引擎（根包）

- **P1 自定义布局无法上报固有尺寸**：`Layout` 接口只有 `Apply`。`Container.Measure` 只对三个内置布局引擎做按值的类型断言，所以连传 `&FlexLayout{}` 都会漏掉，其他引擎一律按「占满全部可用空间」测量。后果是自定义瀑布流放进 `ScrollView` 或 `Popup` 时，尺寸等于视口。建议为 `Layout` 增加可选的 `Measurer` 接口。（`layout.go` `Layout`；`container.go` `Container.Measure`）
- **P1 没有窗口级的事件拦截**：`AddEventListener` 只能观察，不能消费；事件目标在 overlay 里时，root 的捕获阶段处理器看不到它。命令面板、Vim 模式、宏录制都无处下手。建议增加 `Window.SetEventFilter(func(Event) bool)`，在分发之前执行。（`window.go` `dispatch`；`recording.go`）
- **P1 快捷键**：没有 `Unregister`；多个注册命中时先注册的生效；不区分作用域（面板、文档）；没有 `CmdOrCtrl` 这样的跨平台写法。（`accelerator.go`）
- **P1 无法否决窗口关闭**：`Window.OnClose` 的注释让人调用一个并不存在的 `SetShouldClose`；`runCloseHandlers` 执行完立即销毁窗口。（`window.go`、`app.go`）
- **P2 事件类型是封闭的**：`Event` 带未导出方法，`EventType` 是枚举，应用无法定义自己的冒泡事件。（`event.go`）
- **P2 拖放**：
  - 拖拽本身没有数据载荷和 MIME 类型，没有 DragEnter / DragLeave，不能接受或拒绝投放，也没有拖拽预览图。
  - 系统文件拖入只有窗口级回调，不会路由到实现了 `Droppable` 的控件。
  - 位置：`event.go` `DragEvent`；`window.go` 拖拽部分；`filedrop.go`。
- **P2 窗口生命周期**：没有激活、失活、移动、最小化回调；没有 sheet、子窗口或从属窗口；托盘应用无法在没有任何窗口时继续运行。（`window_overlay.go` `WindowKind`；`app.go`）
- **P2 主题**：
  - 主题是进程级全局的，没有供缓存使用的 `ThemeGeneration()`。
  - `SubscribeTheme` 与控件的生命周期无关，订阅切片只增不减。
  - 很多控件在构造时就拷贝了解析后的颜色（`DefaultStyle`、`defaultButtonStates`），调用 `SetTheme` 后它们不会变化。
  - 位置：`theme.go`、`style.go`、`widgets/basic.go`。
- **P2 焦点监听与 overlay 层级**：`AddFocusChangeListener` 注册后无法移除。overlay 只是一个栈，没有分层（后打开的 modal 会盖住 toast），`RemoveOverlay` 之前也没有退场动画钩子。（`focus.go`、`window.go`）
- **P2 可选钩子只能靠鸭子类型满足**：
  - `Modal()`、`SetFocusVisible()`、`ChildList()` 只要方法签名对得上就生效，但公开文档里没有任何说明。
  - `markLayoutDirty` 未导出。
  - 漏调 `SetSelf` 会悄无声息地让 hit-test 失效，建议在 `QUI_DEBUG_*` 调试模式下加断言。
  - 位置：`focus.go`、`widget.go`、`container.go`。

### `widgets`

- **P1 `Select` 的选项只能是 `[]string`**：
  - 没有 value 和 label 之分。
  - 没有图标、分组、自定义渲染，也没有打开 / 关闭事件。
  - `SetText` 按翻译后的显示文本匹配，切换语言后会对不上。
  - 位置：`select.go`。
- **P1 `ListView` / `TableView` 的颜色写死为深色**：表头、选中、hover、斑马纹、滚动条的颜色都是 `Draw` 和构造函数里的字面值，应改为读主题 token 或导出字段。（`listview.go`、`tableview.go`、根包 `scrollbar.go`）
- **P1 `TableView` 单元格只能显示纯文本**：没有单元格渲染器、列对齐，也不能点表头排序。widget-row 模式没有虚拟化，所有行控件都要一次性创建。（`tableview.go`、`listview.go` `SetRowWidgets`）
- **P1 `TabView`**：
  - 标签只有标题，不能加图标、关闭按钮、禁用状态或角标。
  - 标签宽度强制均分，标签多了没有溢出或滚动处理。
  - 标签栏高度是私有常量。
  - 切换标签无法否决。
  - 位置：`tabs.go`。
- **P1 菜单**：
  - `menuActivatable` 的方法未导出，自定义的菜单行无法参与键盘导航；Panel 行会被方向键直接跳过。
  - `MenuItem` 没有图标字段，也没有 i18n key。
  - 在子菜单里按 Left 会关闭整条菜单链；顶层菜单之间也不能用 Left / Right 切换。
  - 位置：`menu.go`。
- **P1 `Select` / `MenuBar` 在构造时就记下了 `*Window`**：如果先搭好控件树、之后才挂到窗口上，它们会静默打不开。建议回退到 `Window()` 取窗口。（`select.go`、`menu.go`）
- **P2 `MenuBar` 文档引用了不存在的 API**：注释里的 `mb.Menus()[i].Trigger()` 并不存在，trigger 按钮的样式只在 `AddMenu` 时拷贝一次。（`menu.go` `AddMenu`）
- **P2 `Popup`**：
  - 关闭时不带原因，也不能否决。
  - 点击外部的那次点击总会被吞掉，不能穿透到下层。
  - `ShowAt` 不把焦点移进内容，也不做窗口边缘夹取。
  - 位置：`popup.go`。
- **P2 `MouseEvent` 没有点击次数**：`ListView` / `TableView` 的 `OnActivate` 只响应 Enter，要实现「双击打开」只能自己按时间判断。（根包 `event.go`）
- **P2 原生控件没有 `OnFocus` / `OnBlur` / `OnKeyDown`**：htmlcss 的 `El` 有这些回调。要给 `Input` 做自动补全，只能嵌入它再重写 `Handle`。（`input.go`）
- **P2 i18n 缺口**（对照 CLAUDE.md 里关于 `TextKey` 的规则）：以下字段没有对应的 key：`Tab.Title`、`TableColumn.Title`、`MenuItem.Label` / `AddMenu`、`RadioButton.Label`、`Input.Label` / `Select.Label`、`FieldSet` 标题、`SetTooltip`。
- **P2 无障碍**：
  - `Input` / `Select` 的 `AccessibleName` 忽略了 `Label`，也没有覆盖名称的入口。
  - 标签页和模型模式下的行都没有通过 `AccessibleChildren` 暴露出来。
  - Tab 导航不会把获得焦点的控件滚动到可视区。
  - `ScrollView.ContentSize` 需要手动维护。
  - 位置：`accessibility.go`、`scroll.go`。
- **P2 几何尺寸写死**：`FieldSet` 标题的高度和字体是固定的；内置 tooltip 的内边距、字体、延迟都不可配置。（`frame.go`、根包 `tooltip.go`）

### `htmlcss` / `reactive` / `reactive/html`

- **P1 用原生控件实现的弹层，CSS 改不到外观**：`<select>` 下拉、`title` 提示框、滚动条、datalist 弹层、颜色面板读的都是进程级的 Go 主题，`SetCSS` 和 `:root` 变量都影响不到，暗色样式表下它们仍是浅色。（`widgets/select.go`、根包 `tooltip.go`、`widgets/scroll.go`、`datalist.go`、`el.go` 颜色面板）
- **P1 DSL 没有 pointer down / move / up 事件**（COVERAGE 里已列为已知缺口），分割条、自绘滑块、框选都做不了。`OnClick` 不带坐标和按键信息。没有 `preventDefault` 来阻止内置行为，比如 submit 按钮会在作者的 onClick 之外同时提交表单。
- **P1 Effect 在布局之前执行**：`UseEffect` 里读到的 `Bounds()` 是旧值或零。定位弹层、滚动到新增的行、测量尺寸都需要一个 `UseLayoutEffect`。
- **P1 弹层定位方式太少**：`PortalAlign` 只有 Center / Fill / AtPosition，没有锚定到某个元素、跟随窗口变化、空间不够时自动翻转的 popover；目前只有原生菜单有 `AnchorMenu`。（`reactive/portal.go`）
- **P2 `Ref` 只在元素创建时触发一次**：通过 Ref 装上的回调永远停留在首次渲染的闭包，也没有卸载时的 detach 回调。`OnFormSubmit` 和 canvas 绘制都没有对应的 Builder 方法。（`reactive/html/html.go`）
- **P2 `h.Leaf`（嵌入原生控件）不进节点镜像**：class、margin、flex 对它都不生效，`:nth-child` 和 `+` 选择器会把它跳过。也没有 `h.Canvas`。（`reactive/html/html.go`）
- **P2 内联文字里的链接直接调用 `qui.OpenURL`**：应用拦截不了站内路由（如 `#/settings`）；内容不可信时还有安全隐患。（`el.go` 点击处理）
- **P2 不支持扩展**：不能注册自定义标签，也不能把自定义 CSS 属性映射到原生控件的 setter。（`htmlcss`）
- **P2 自绘内容读不到计算后的样式**：`SetCanvasDraw` 里拿不到元素的 `color` 和 `--变量`。
- **P2 缺少 `UseId` 和异步资源原语**：异步资源原语要能管理 loading / error 状态，并在组件卸载后安全地处理迟到的结果。
- **P2 `:focus-visible` 被当作 `:focus` 处理**：作者写的焦点环在鼠标点击后也会出现；给推按钮写了 `:focus` 规则后，它又会变回「点击即聚焦」。（`css.go`）
- **P2 文件拖入和粘贴只有窗口级入口**：没有元素级的放置区，元素上也没有 paste 事件。

## 第一轮的验证方式

- **单元测试**：每项修复都有对应测试，分别在 `overlay_modal_test.go`、`widgets/dialog_actions_test.go`、`reactive/html/dialog_test.go`、`font_test.go`。`go test ./...` 全部通过。
- **在真实应用里验证**：用 `cmd/qui-agent` 驱动 `examples/dialog` 和 `examples/reactive-html`，检查了以下几项：
  - 校验失败时对话框保持打开；
  - 焦点在输入框时按 Enter 能保存，按 Esc 能关闭；
  - Tab 在 × → Cancel → Delete 之间循环；
  - 对话框跟随 `.app.dark` 切换为暗色。
- **下游回归**：q-office 基于当前代码可以构建，`cmd/q-excel` 的测试通过。
