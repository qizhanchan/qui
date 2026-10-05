# reactive

在 qui 的**保留模式（retained-mode）**控件树之上，架一层**声明式 UI 运行时**。目标是用尽可能少的代码写出复杂业务界面——用 React 已经验证过的心智模型，但不继承 React 引擎的那些缺陷。

一句话概括本包的定位：**API 是 React 的形状，引擎是 Solid 的形状。**

[English](README.md) · **中文**

---

## 1. 为什么需要它，为什么长这样

qui 底层是保留树 + 脏区域重绘：控件是稳定的 Go 对象，不失效就不重画，改一个 Label 只重画一个矩形。这套底层其实比浏览器 DOM 更适合做细粒度响应式——控件身份天然稳定，setter 就是方法调用，失效就是 `InvalidateRect`。

但"手写控件树 + 手动增删子节点"写业务界面太啰嗦。React 的声明式范式（`UI = f(state)`）解决了这个啰嗦，代价是"任何状态变化都从根部重跑 render 再 diff"。直接照搬会和 qui 的底层设计打架：上层用全量重算，去驱动一个为精确失效而生的底层。

所以本包分成**两套引擎，各管一件事**：

| 引擎 | 管什么 | 触发方式 | 开销 |
|------|--------|----------|------|
| **Reconcile 引擎** | 结构变化（组件挂载/卸载、列表增删） | `UseState` / 组件 setState | 一次作用域内的树 diff |
| **Signal 引擎** | 高频值变化 + 结构变化的快路径 | `Signal.Set` | 直接调 widget setter / 局部 diff，**不跑 render** |

写界面时：骨架和条件结构用 Reconcile 引擎（熟悉的 React 写法），高频更新（时钟、进度、拖拽数值、大列表）用 Signal 引擎绕开 VDOM。

---

## 2. 分层

```
reactive/            引擎：Element / Reconciler / Runtime / hooks / signal（只依赖 root qui，后端无关）
reactive/html/       h DSL：HTML 元素 builder，降解为 htmlcss.El 承载的 Element，样式走纯 CSS
```

- `reactive` 引擎不依赖任何后端——host 的 Create/Update/SetChildren 钩子由上层提供。
  `reactive/html` 是当前唯一的官方后端（htmlcss 活元素）；后端自身的状态（style engine）
  通过 `Runtime.SetHostData` / `reactive.CurrentRuntime()` 按 runtime 解析，支持多窗口。
- 严格遵守 qui 的导入方向：root 永不反向依赖。

一个最小程序（`import h "github.com/qizhanchan/qui/reactive/html"`）：

```go
rt := h.Mount(window, `.title { font-size: 20px }`, func() h.Node {
    n, set := reactive.UseState(0)
    return h.Div(
        h.H1("Counter").Class("title"),
        h.Button(fmt.Sprintf("count: %d", n)).OnClick(func() { set(n + 1) }),
    )
})
```

---

## 3. Reconcile 引擎

### 3.1 Element 与 instance

`Element` 是**声明**（一次性的、值类型的描述），`instance` 是**保留状态**（挂载后长期存在，持有真实 widget、hook 槽、子节点）。`render` 函数每次产出一棵新的 Element 树，Reconciler 拿它和上一棵 instance 树对比，做最小改动。

Element 有若干种 kind，除了普通 host（对应一个真实 widget），还有几种"无自有 widget"的特殊节点，它们的子节点会**拼接（splice）进最近的 host 祖先**：

| kind | 作用 |
|------|------|
| host | 一个真实控件（`Node`/`Leaf` 构造） |
| component | `Component[P](name, key, props, render)`，有独立 hook 状态 |
| `Fragment(...)` | 多个兄弟节点，无容器包裹（React `<>...</>`） |
| provider | `ctx.Provide(value, child)`，Context 注入 |
| portal | `Portal(child)`，挂到 window overlay 栈 |
| boundary | `ErrorBoundary(...)`，捕获子树 render panic |
| bound | `Show`/`For`，signal 驱动的结构（见 §4.3） |
| `Empty()` | 条件占位槽 |

### 3.2 keyed diff 与条件渲染

子节点按 `(kind, key)` 匹配复用。列表项给稳定 `Key`，增删重排就能复用 widget 实例而不是重建。

条件渲染 `h.If(cond, node)` 在 false 时降级为 `h.Nothing()`（即 `Empty()`）。**关键**：Reconciler 把 `Empty()` 保留成一个 nil 占位槽留在兄弟序列里，所以切换条件只挂载/卸载那一个子树，前后的无 key 兄弟不会错位（React null-child 语义）。

```go
h.Div(
    header,
    h.If(expanded, detailPanel), // 折叠时留 nil 占位
    footer,                      // expanded 切换时不会被重挂载
)
```

### 3.3 组件默认不 memo；memo 必须显式比较

`Component` 默认在父级 render 时重跑，因此每次都会安装新生成的回调闭包，不会把 render-local 状态卡在旧 handler 里。只有组件自身的 `setState` 仍然是局部更新。

需要优化时使用 `MemoComponent`，并提供**类型安全的比较器**：

```go
reactive.MemoComponent("Row", key, props,
    func(previous, next RowProps) bool {
        return previous.ID == next.ID && previous.Title == next.Title
    },
    renderRow,
)
```

比较器就是 memo 合约：它必须涵盖会影响渲染和事件处理器语义的所有数据。Go 函数值没有可用的闭包相等性——代码地址不包含捕获值——因此框架不会再默认推断它。`valuesEqual` 供 Signal、hook deps 和 Context 使用，遵循安全规则：非 nil 函数不相等。

### 3.4 作用域重渲染：childDirty + visitDirty

组件 props 没变且自身不 dirty 时会 bailout（跳过重渲染）。但如果它**下面**有个组件 setState 了，直接返回旧子树会把那个 dirty 组件冻住。

解法：每个 fiber 带 `parent` 链和 `childDirty` 标记。setState 时沿 parent 链把 `childDirty` 一路标到根；bailout 遇到 `childDirty` 就走 `visitDirty` 遍历 instance 子树，只重渲染真正 dirty 的 fiber，并沿途重新同步 host 的子控件列表。（标记走链时**绝不提前返回**——上面一个被清过的祖先会切断后续 pass 的链路。）

### 3.5 子节点同步按 widget 列表，不按 instance 指针

`syncChildWidgets` 收集一个 host 节点最终贡献的 widget 列表（穿透 component / fragment / provider 展开），只在这个**扁平 widget 列表真的变了**时才调 `SetChildren`。这样组件实例复用但换了根 widget（render 换了根 kind）也能正确传播给父容器。

---

## 4. Signal 引擎（"Solid 引擎"）

### 4.1 Signal

```go
s := reactive.NewSignal(0)
s.Get()              // 读（自动追踪时会登记依赖）
s.Peek()             // 读，不登记依赖
s.Set(1)             // 写，等值不通知
s.Update(func(v int) int { return v + 1 })
unsub := s.Subscribe(func() { ... })
```

等值 `Set` 不触发通知（用 `valuesEqual` 判断）。

### 4.2 属性直绑：`BindWidget`

```go
count := reactive.NewSignal(0)
text  := reactive.Map(count, func(n int) string { return fmt.Sprintf("n=%d", n) })

h.Span("").BindText(text)     // signal 变 → 直接 SetTextContent，不跑 render，不跑 reconcile
```

`BindWidget(widget, signal, apply)` 是核心：signal 变化 → 通过 `PostJob` 编组到 window 主 goroutine → 直接调 widget setter（burst 合并，一帧只应用最新值）→ 吃 qui 原生的脏区域重绘。**完全不经过 VDOM。**

h 层已封装：`.BindText`（元素文本）、`.BindClass`（CSS 类切换，含 restyle）。退订通过 `Element.Destroy`（mount 时锁定的卸载钩子）自动接线。

**铁律**：signal 身份在 widget 生命周期内必须稳定。要么在 render 外创建，要么用 `UseSignal`（每 fiber 记忆化；对它 `Set` 不会触发组件重渲染）。**不要**在 render 里每次新建 `Map(...)` 链——会泄漏订阅。

### 4.3 结构直绑：`Show` / `For`（Solid 的 `<Show>` / `<For>`）

```go
visible := reactive.NewSignal(false)
h.Show("hint", visible, func() h.Node { return h.Span("现在你看见我了") })

todos := reactive.NewSignal([]todo{...})
h.For("rows", todos, func(i int, t todo) h.Node {
    return h.Li(t.Text).Key(strconv.Itoa(t.ID))
})
// 容器布局选项走 h.ForWith(key, reactive.BoundLayout{...}, todos, render)
```

signal 变 → 合并成一个主线程 job → **只对这个节点的子树做一次局部 reconcile**（复用 keyed diff）——根 render 函数和树的其余部分完全不跑。

这里有两个决定成败的细节：

1. **行内组件 setState 不能死**：bound 节点在 mount 时捕获 `parentFiber`，局部 pass 以它为栈底——行 fiber 的 childDirty 链才能接回真实祖先，全量 pass 的 `visitDirty` 才找得到它。
2. **行内 `Context.Use` 不能失效**：局部 sync 不是从根下降的，provider 栈是空的。所以 mount / 每次常规 pass 都快照 provider 栈，局部 sync 时复原。

局部 sync 的节点数计入 profiler 总账，但**不增加 render pass 计数**——用 `rt.Profile().Renders` 恒定来验证"零 reconcile"。

### 4.4 派生：`Map` / `Computed`

```go
double := reactive.Map(base, func(n int) int { return n * 2 })

// 零 deps = 自动追踪：fn 里每个 Get 自动登记为依赖
total := reactive.Computed(func() string {
    return fmt.Sprintf("%d/%d", done.Get(), all.Get())
})
```

`Computed(fn)` 不传 deps 时走**自动依赖追踪**（Solid 模式）：fn 执行期间的每个 `Signal.Get` 自动登记，且**每次重算重新收集**——藏在 `if` 分支里的 computed 只监听实际走到的分支，掉线的依赖自动退订。`Signal.Peek` 可在 fn 里读而不登记依赖。传了显式 deps 则走固定订阅、不做 Get 拦截。

`Map` 和 `Computed` 返回的仍是普通 `*Signal[T]`，但带有 `Dispose()`：它会解除所有上游订阅（自动追踪的动态依赖也包括在内），可重复调用。为临时页面、动态组件或切换数据源创建的派生 signal，应在其拥有者结束时调用 `Dispose()`；应用全生命周期的派生值通常无需处理。

实现上：全局 atomic tracker 槽，无追踪时 `Get` 只多一次 atomic load（快路径无锁）；追踪期间的收集有锁保护——无关 goroutine 并发 `Get` 最坏产生一个良性冗余依赖，不会 race。

---

## 5. Hooks

positional per-fiber（调用顺序必须稳定，否则 panic——和 React 一样，别放 `if` 里）：

| Hook | 说明 |
|------|------|
| `UseState(init)` | `(value, set)` |
| `UseStateFn(init)` | `(value, set, update)`；`update(func(prev) next)` 基于最新值，burst 安全 |
| `UseReducer(reducer, init)` | `(state, dispatch)`；dispatch 基于最新 state |
| `UseRef(init)` | 稳定 `*T`，改它不触发渲染 |
| `UseMemo(fn, deps...)` | 按 deps 记忆化 |
| `UseCallback(fn, deps...)` | 稳定函数身份 |
| `UseEffect(fn, deps...)` | commit 后跑副作用，返回 cleanup |
| `UseEffectOnce(fn)` | 仅挂载跑一次，卸载 cleanup |
| `UseSignal(init)` | 每 fiber 记忆化的 Signal，满足直绑的身份稳定要求；`Set` 不触发重渲染 |

Context：

```go
var ThemeCtx = reactive.NewContext("theme", defaultTheme)
ThemeCtx.Provide(dark, subtree)   // 高处提供
theme := ThemeCtx.Use()           // 任意深度消费（订阅，值变时标脏消费者）
```

---

## 6. 其他能力

- **Fragment**（`reactive.Fragment` / `h.Frag`）：组件返回多个兄弟，直接参与父布局，无容器包裹。
- **Portal**（`h.Portal` / `h.ModalPortal(onDismiss, node)` / `h.ModalPortalWith(opts, node)`）：声明式 overlay。
  - `h.If(open, h.ModalPortal(...))` 就是一个带 scrim、点击/Esc 关闭、焦点陷阱的模态框。
  - `ModalPortalWith` 可以配置 scrim、哪些手势会关闭，以及由 `OnEnter` 触发的默认动作。
  - Esc 和 Enter 在冒泡阶段处理，获得焦点的内容先拿到这两个键，例如 textarea 换行、行内编辑取消。
  - portal 内容从声明它的位置继承样式：虽然它挂在 overlay 栈上，但能继承自定义属性，也能匹配 `.app.dark .x` 这类祖先选择器。
  - overlay 的失效在 portal 本地处理，不冒泡到主树。
- **Dialog**（`h.Dialog(h.DialogProps{Title, Body, Actions, OnDismiss, OnConfirm, CloseButton, Class})`）：基于 `ModalPortalWith` 的现成对话框外壳。
  - 结构：`div.q-dialog > header.q-dialog-header(.q-dialog-title, .q-dialog-close) + .q-dialog-body + footer.q-dialog-actions`。
  - 外观来自框架层 CSS，任何作者规则都能覆盖；也可以通过 `--q-dialog-bg/-fg/-radius/-shadow` 换肤。
  - 按钮可以 Tab 到达，Enter/Space 按下；打开时要聚焦的输入框加上 `.Autofocus()`。
- **ErrorBoundary**（`reactive.ErrorBoundary(key, child, fallback)`）：捕获子树的 **render/reconcile panic**（Go GUI 里这会杀进程），切换到 fallback + retry。fallback 自身 panic 会向上传播；事件处理器/effect 里的 panic 不在覆盖范围。

---

## 7. 快速参考

**h DSL**（`reactive/html`，import 别名 `h`）

```
容器    Div / Section / Header / Footer / Nav / Main / Ul / Ol / Form
文本    Span / P / H1..H6 / Li / Button / Label / A(text, href)
表单    Input() / Checkbox() / Textarea() / Select(items...)
          → .Value/.OnInput/.OnSubmit/.Checked/.OnToggle/.Options/.Selected/.OnSelect
通用    .Class/.ID/.Key/.Attr/.Text/.Children/.OnClick/.OnContextMenu/.Icon
拖拽    .Draggable(key)/.DragHandle/.OnDrop/.OnDragOver/.OnDragEnd
条件    If / Nothing / Frag / ForEach / El(rawElement)
信号    .BindText/.BindClass；Show(key, sig, build) / For(key, sig, render) / ForWith
浮层    Portal / ModalPortal(onDismiss, node) / ModalPortalWith(opts, node) / Dialog(props)
        ContextMenu(window, x, y, items)
焦点    .Autofocus()；El.RequestFocus()
```

**样式规则**：全部走 CSS class（`h.Mount` 传入的 stylesheet），状态驱动的样式用 `.BindClass` 切换类名或 setState 换 `.Class`——没有内联样式 prop。

---

## 8. 心智模型小结

- **写骨架/条件结构**：普通 render + hooks，React 的写法。
- **高频值更新**：`Signal` + `.BindText`/`.BindClass`/`BindWidget`，绕开 VDOM。
- **高频结构更新**（大列表、频繁增删）：`For`/`Show`，局部 diff，绕开根 render。
- **组件 memo**：默认不启用；确有必要时用 `MemoComponent`，并在比较器中完整表达渲染与回调所依赖的数据。
- **派生 signal**：临时的 `Map`/`Computed` 在拥有者结束时调用 `Dispose()`，释放上游订阅。
- **直绑的 signal**：身份要稳定——render 外建，或 `UseSignal`。

reconciler 在这套设计里从"每次更新的税"退化成"按需工具"：只有真正的结构变化才付 diff 的钱，值变化和列表变化都有各自的快路径。

---

## 9. 示例

```
examples/reactive-html   signals + For/Show + BindClass、拖拽重排、右键菜单、
                         模态对话框、CSS grid 卡片墙、明暗主题切换、跨包组件
```

更细的实现约定见仓库根 README 的 "App-dev surface" 一节。
