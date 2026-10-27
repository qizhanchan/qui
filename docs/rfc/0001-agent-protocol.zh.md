# RFC 0001：qui 调试与操作协议 v2（CDP 形态）

| | |
|---|---|
| 状态 | 已实施 M1–M7（2026-10-27），偏差与未完成项见 §16 |
| 日期 | 2026-10-27 |
| 涉及 | `agent`、`cmd/qui-agent`、root 内省原语（`accessibility.go` / `selector.go` / `actions.go` / `wait.go` / `recording.go`）、`htmlcss/inspect.go`、`reactive` 的 Inspect |
| 已定 | ① 不保留现有 REST 接口，一次性切换；② 节点 id 采用全局单调递增；③ 不内嵌 JS 运行时 |

---

## 1. 背景

qui 的 agent 协议（`agent/llm.txt`）已经覆盖了 agent 驱动 UI 的大部分场景：AX 树、DOM 视图、样式级联、Reactive 组件树、动作、等待、截图、事件流。但它是随功能一路长出来的 REST 端点集合，存在几个会限制长期发展的结构性问题（见 §3）。

Chrome DevTools Protocol（CDP）是这个领域最成熟的协议，也是 coding agent 最熟悉的协议之一：`DOM.querySelector`、`CSS.getMatchedStylesForNode`、`Input.dispatchMouseEvent`、`Page.captureScreenshot` 这些名字几乎不需要解释。本 RFC 提议把 qui 的协议重构成 **CDP 形态**——借鉴它的结构（域、方法、事件、节点句柄、Target 模型、机器可读 schema），在语义吻合处直接使用 CDP 的方法名，同时保留 qui 已经比 CDP 更适合 agent 的部分（动作后的富结果、语义化错误码、auto-wait、只读声明式）。

## 2. 目标与非目标

**目标**

1. 一个统一的身份空间：widget、DOM 元素、AX 节点、Reactive 组件共享全局 `nodeId`，可以"解析一次、多次操作"，失效时显式报错。
2. 一个统一的消息格式：CDP 的 `{id, method, params, sessionId}`，WebSocket 和 HTTP 一次性调用共用同一套方法。
3. App 级入口 + 多窗口：一个 server 服务整个 App，窗口是 target。
4. 按域订阅的事件：DOM 变更、日志、panic、帧生命周期、Inspect 选点，而不只是输入事件。
5. 运行时可控：今天的 `QUI_DEBUG_*` 环境变量、locale、尺寸、动画速度都能在运行时切换，无需重启。
6. 机器可读 schema：方法从 Go 类型声明生成 `protocol.json`，`llm.txt` 的参考部分、CLI、MCP 工具定义都从它派生。
7. 可扩展：子包（htmlcss、reactive、graphs/diagram、webview……）可以注册自己的域，`agent` 包只负责传输。
8. Chrome DevTools 前端能连上来做只读检查（Elements / Styles / Computed / Accessibility / 投屏）——作为分阶段目标，见 §10。

**非目标**

- `Runtime.evaluate` 或任何形式的 JS 执行（见 §10.2）。
- 与 Puppeteer / Playwright `connectOverCDP` 兼容。它们的定位器、点击前的滚动、ElementHandle 全部靠 `Runtime.callFunctionOn` 注入 JS 实现，没有 JS 引擎时第一个 ElementHandle 就会失败。对这类用户，qui 提供 MCP 层（§11）。
- Network、Storage、ServiceWorker、Fetch 等与原生 GUI 无关的 CDP 域。
- 照搬 CDP 的 DOM 推送镜像模型作为主路径（见 §8.2）。

## 3. 现状问题

| # | 问题 | 证据 | 后果 |
|---|---|---|---|
| P1 | 节点没有稳定身份 | `AXNode.Path` 是位置串 `…/Box[2]/Button[0]`（`accessibility.go:299`）；`DOMNode.Path` 同理（`htmlcss/inspect.go`） | 前面插入兄弟节点 path 就漂移；每次 `/act` 重新解析 selector，读与写之间目标可能已变（TOCTOU）；无法表达"这个节点已经没了" |
| P2 | 三棵树三套寻址 | `/tree` + `/act`、`/dom` + `/dom/act`、`/reactive` 各自独立；`/act` 在 widget 语法解析失败时"改当 CSS 再试"（`llm.txt:195`） | agent 要先判断该用哪套；启发式回退让错误信息变模糊；Reactive 节点和 widget 只能靠 `widgetId` 字符串手工关联 |
| P3 | 消息格式不统一 | 有的错误是 HTTP 400，`/reactive` 无内容时是 204，`/act` 是 200 + `ok:false` + `errorCode` | 客户端要为每个端点写不同的错误处理；schema 无从生成 |
| P4 | 事件模型单薄 | `/events` 只推输入/焦点/重绘事件（`agent/server.go` 的 listener） | 拿不到应用日志、panic、DOM 变更、"界面何时真正更新了" |
| P5 | 一个 server 只服务一个窗口 | `Bind(window, …)`；`examples/multiwindow-sessions` 只绑定了第一个窗口；socket 按 pid 命名，`llm.txt` 自己警告"找最新 socket"会连错 app | 多窗口应用的第二个窗口不可调试；发现机制脆弱 |
| P6 | 调试开关只能靠环境变量 | `QUI_DEBUG_PAINT`、`QUI_DEBUG_LAYOUT`、`QUI_DEBUG_LAYOUT_CACHE`…… | 每切一次要重启进程，丢失现场 |
| P7 | 没有机器可读 schema | `llm.txt` 550 行手写；`cmd/qui-agent` 1100 行手写 | 文档、CLI、实现三者漂移；无法自动生成 MCP 工具定义 |
| P8 | 依赖方向与文档不符 | `agent/doc.go` 称"只依赖标准库和 root"，实际 `agent/dom.go` 导入了 `htmlcss` 和 `reactive` | 每加一个可检查的子包，`agent` 就要多一条依赖；graphs/diagram、webview 无法接入 |

## 4. 总体设计

```
┌──────────────────────────────────────────────────────────────┐
│ 客户端：qui-agent CLI · MCP server · DevTools 前端 · 自定义    │
└──────────────┬───────────────────────────────┬───────────────┘
               │ WebSocket（会话 + 事件）        │ HTTP POST /rpc（一次性）
┌──────────────▼───────────────────────────────▼───────────────┐
│ agent：传输层（UDS / TCP、鉴权、会话、事件队列、发现）          │
│        —— 不认识任何具体的域                                    │
└──────────────┬───────────────────────────────────────────────┘
               │ qui.Inspector 注册表（root 定义的接口）
┌──────────────▼───────────────────────────────────────────────┐
│ 域提供者                                                       │
│  root:      Target Page Input Accessibility Overlay Emulation  │
│             Animation Log Performance Debug Agent App           │
│             + DOM / CSS 的 widget 树兜底实现                     │
│  htmlcss:   DOM CSS（注册后覆盖兜底实现）                        │
│  reactive:  Reactive                                           │
│  将来：     Diagram（graphs/diagram）、Scene3D、WebView 子 target │
└──────────────────────────────────────────────────────────────┘
```

层次划分：

- **原生协议层**（§5–§9）：CDP 形态的域/方法/事件。语义吻合处直接用 CDP 方法名和参数名。
- **Agent 层**（§9）：`Agent.*` 域，提供 Playwright 风格的高层动作（selector 或 ref、auto-wait、动作后富结果）和紧凑文本快照。这是 LLM 的主要入口。
- **兼容层**（§10、§11）：DevTools 前端适配、MCP 封装。都只是原生协议之上的薄适配。

## 5. 传输与消息格式

### 5.1 消息

与 CDP 完全一致的外形：

```jsonc
// 请求
{"id": 7, "method": "DOM.querySelector", "params": {"nodeId": 1, "selector": ".toolbar .save"}, "sessionId": "S1"}
// 成功
{"id": 7, "result": {"nodeId": 418}, "sessionId": "S1"}
// 失败
{"id": 7, "error": {"code": -32000, "message": "selector matched nothing",
                    "data": {"reason": "no_match", "hint": "re-run Agent.snapshot"}}, "sessionId": "S1"}
// 事件
{"method": "Log.entryAdded", "params": {...}, "sessionId": "S1"}
```

### 5.2 两种传输，同一套方法

| 传输 | 路径 | 用途 |
|---|---|---|
| WebSocket | `/devtools/browser`（App 级）、`/devtools/page/<targetId>`（直连某窗口） | 会话、事件订阅、DevTools 前端、CLI 的 `events` 命令 |
| HTTP 一次性 | `POST /rpc`，body 为 `{method, params, target?}` | curl、无法保持长连接的 agent 运行环境 |

HTTP 调用无会话，`target` 省略时：App 只有一个窗口则隐式选中，否则报 `ambiguous_target`。需要 `enable` 状态的事件对 HTTP 客户端不适用，改由 §8.3 的事件环形缓冲轮询。

WebSocket 实现优先使用已有依赖 `golang.org/x/net/websocket`（不新增模块）。如果后续遇到它缺失的特性（如 permessage-deflate、细粒度背压），再评估替换，见 §14。

### 5.3 错误

外层 `code` 遵循 JSON-RPC：`-32700` 解析错误、`-32600` 非法请求、`-32601` 方法不存在、`-32602` 参数错误、`-32000` 业务错误。`data.reason` 保留 qui 的语义化错误码，这是 agent 能自主恢复的关键：

| reason | 含义 | agent 应对 |
|---|---|---|
| `bad_selector` | selector 解析失败 | 改 selector，重试无用 |
| `no_match` | selector 没匹配到 | 重新快照 |
| `stale_node` | nodeId 指向的节点已不在任何窗口的树里 | 重新解析 |
| `modal_blocked` | 目标被模态层挡住；`data.blockingNodeId` | 先处理模态框 |
| `not_text_target` | `type` 打在了不接受文本的节点上，未做任何修改 | 换目标 |
| `timeout` | 等待或主线程调度超时 | 判断 app 是否卡死 |
| `ambiguous_target` | 多窗口时未指定 target | 带上 target |
| `target_closed` | 窗口已关闭 | 重新 `Target.getTargets` |
| `unsupported` | 已识别但 qui 不支持的 CDP 方法（DevTools 前端兼容桩） | 忽略 |

## 6. 发现与 Target 模型

### 6.1 App 级 server

`agent.BindEnv` 改为绑定 `*qui.App`（保留按窗口绑定的便捷包装，内部转为 App 级）。窗口创建/关闭时自动注册/注销 target，并发出 `Target.targetCreated` / `Target.targetDestroyed`。

Target 类型：

| type | 对象 | 备注 |
|---|---|---|
| `app` | 整个进程 | `/devtools/browser` 的会话对象 |
| `window` | `*qui.Window` | DevTools 兼容层对外报告为 `page` |
| `webview` | `webview` 包里的 CEF 浏览器 | 将来：直接代理 CEF 自己的 CDP 作为子 target，网页内容获得完整的 Chrome DevTools 能力 |

方法：`Target.getTargets`、`Target.attachToTarget{targetId, flatten:true}` → `sessionId`、`Target.detachFromTarget`、`Target.setDiscoverTargets`、`Target.activateTarget`（窗口置前）、`Target.closeTarget`。

### 6.2 发现

- `GET /json/version`、`GET /json/list`、`GET /json/protocol`：形状与 Chrome 一致（`webSocketDebuggerUrl`、`devtoolsFrontendUrl` 等），DevTools 和现有工具可以直接用。
- 发现文件：`$TMPDIR/qui-agent/<appName>.json`，内容为 `{pid, socket, tcp, startedAt}`，对应 Chrome 的 `DevToolsActivePort`。`qui-agent -app <name>` 按名字连接，不再猜"最新的 socket"。`QUI_AGENT_SOCK` 保留，用于显式固定路径。

## 7. 节点身份

### 7.1 规则

- root 维护一个进程级 `atomic.Uint64` 计数器，`nodeId` 从 1 开始单调递增，**永不复用**。
- `BaseWidget` 增加 `nodeID uint64` 字段，**首次被读取时懒分配**（`Widget.NodeID()`）。懒分配同时规避两个问题：用结构体字面量构造、未调用 `NewBaseWidget` 的 widget；以及 `BaseWidget` 按值拷贝时 id 被复制。
- 同一个身份空间覆盖：
  - **widget**：自身的 `nodeId`。
  - **DOM 元素**：就是承载该 `El` 的宿主 widget 的 `nodeId`。因此 DOM 节点与对应 AX 节点天然同 id，不需要 CDP 的 `backendDOMNodeId` 映射。
  - **AX 节点**：widget 的 `nodeId`。
  - **Reactive 组件实例**：从同一计数器分配（组件不是 widget）；host 节点带 `hostNodeId` 指向其 widget。
  - **DOM 文本节点**：htmlcss 把内联文本折叠进 `InlineBox` 的 run，没有独立对象。为 `#text` 节点在 El 侧懒分配 id，生命周期跟随所属 El 的文本内容版本。见 §14 待决问题。
- 节点被移动（例如 keyed 列表重排、reparent）时 id 不变——身份跟随对象，而不是位置。
- 节点不可从任何窗口的 root 或 overlay 到达时，对它的任何操作都返回 `stale_node`。detach 后又重新挂回的 widget 恢复可用。

### 7.2 文本形式与寻址

- JSON 中一律是整数：`"nodeId": 418`。
- 文本快照和 selector 中写作 `@418`。`@` 在 CSS 和 widget selector 语法里都不合法，不会产生歧义。
- 所有接受目标的方法，`target` 参数既可以是 selector，也可以是 `@418`；也可以直接用整数 `nodeId` 参数。
- `path` 字段保留，作为可读的位置描述，但明确不再承诺稳定。

### 7.3 统一 selector

一个 selector 字符串，一个解析器，不再按端点区分、不再"失败后换语法重试"：

- CSS：tag、`.class`、`#id`、属性、组合器、`:nth-child`、`:not`……（htmlcss 的引擎）
- Playwright 定位器：`:has-text("…")`
- AX 伪类与属性：`[role=button]`、`[name="Save"]`、`[key=qui.save]`、`:focused`、`:visible`、`:layer(modal)`、`:nth(0)`
- 节点引用：`@418`、`@418 .row`（以该节点为作用域）

`#id` 统一为"DOM id 属性或 widget ID"。原生 widget 应用中，tag 就是 widget 类型名的小写形式（`button`、`listview`）。

## 8. 域

✅ 表示首期实现，○ 表示后续阶段。方法名与 CDP 相同的，参数和结果尽量取 CDP 的子集；qui 扩展字段用 `qui` 前缀或放在独立域中。

### 8.1 方法一览

| 域 | 方法 | 事件 | 阶段 | 说明 |
|---|---|---|---|---|
| **Target** | `getTargets` `attachToTarget` `detachFromTarget` `setDiscoverTargets` `activateTarget` `closeTarget` | `targetCreated` `targetDestroyed` `targetInfoChanged` | ✅ | §6 |
| **DOM** | `enable` `getDocument{depth}` `querySelector` `querySelectorAll` `describeNode` `getBoxModel` `getNodeForLocation{x,y}` `scrollIntoViewIfNeeded` `focus` | `documentUpdated` `childNodeInserted` `childNodeRemoved` `attributeModified` `characterDataModified` | ✅ 方法 / ○ 事件 | 没有 htmlcss 的应用，由 root 用 widget 树兜底 |
| **CSS** | `getMatchedStylesForNode` `getComputedStyleForNode` | — | ✅ | 对应今天的 `/dom/styles` |
| | `setStyleTexts` `forcePseudoState` | `styleSheetAdded` | ○ | 实时改样式；`forcePseudoState` 让 `:hover`/`:focus` 可检查（今天明确匹配不到） |
| **Accessibility** | `getFullAXTree{depth}` `queryAXTree{role,name}` `getPartialAXTree{nodeId}` | — | ✅ | 对应今天的 `/tree` |
| **Input** | `dispatchMouseEvent` `dispatchKeyEvent` `insertText` `imeSetComposition` `synthesizePinchGesture` `synthesizeScrollGesture` `dispatchDragEvent` | — | ✅ | 坐标级原始输入；今天散落在 `/act` 里的 `x,y` 入口迁到这里 |
| **Page** | `captureScreenshot{format,clip,scale,quiAnnotate}` `getLayoutMetrics` `bringToFront` | `frameCommitted`(qui) `windowResized` | ✅ | `quiAnnotate` 对应今天的 `annotate=1` |
| | `startScreencast` `stopScreencast` `screencastFrameAck` | `screencastFrame` | ○ | 用 qui 自带的脏区域只推变化部分 |
| **Overlay** | `highlightNode` `hideHighlight` `setInspectMode` | `inspectNodeRequested` `nodeHighlightRequested` | ✅ | 复用 Cmd+Shift+A 覆盖层；`setInspectMode` 后用户在窗口里点一下，agent 收到对应 `nodeId` |
| **Emulation** | `setDeviceMetricsOverride` `setLocaleOverride` `setEmulatedMedia` | — | ○ | 尺寸/缩放、i18n 切换、配色方案 |
| | `setVirtualTimePolicy` | `virtualTimeBudgetExpired` | ○ | 冻结/推进帧时钟，截图可复现 |
| **Animation** | `getPlaybackRate` `setPlaybackRate` `setPaused` `seekAnimations` | `animationStarted` | ○ | 对接 `qui.Animator` |
| **Log** | `enable` `clear` | `entryAdded` | ✅ | 应用日志。root 目前不使用 `slog`；新增 `qui.LogHandler()` 返回一个 `slog.Handler`，应用自行接入（`slog.SetDefault` 或包装已有 handler），框架不劫持全局 `log` 输出 |
| **Runtime** | `enable` | `exceptionThrown` | ✅ | 事件分发、job、Draw 中的 panic，带 Go 栈。今天帧循环**不** recover panic（只有 `safeCallListener` 吞掉监听器的 panic），所以需要新增捕获点，策略见 §15 |
| **App** | `listCommands` `invoke{name,args}` | — | ○ | 应用自注册的具名命令（JSON 进 JSON 出），替代 eval。不使用 `Runtime.evaluate` 的名字，避免让 agent 误以为能执行 JS |
| **Performance** | `enable` `getMetrics` | `metrics` | ○ | 帧耗时、layout 次数、重绘面积、glyph cache 命中、job 队列深度 |
| **Debug**(qui) | `getFlags` `setFlags{paint,layout,layoutCache,draw,thread,widgets,i18nStrict}` `getDiagnostics` | `diagnostic` | ✅ | 取代 `QUI_DEBUG_*` 环境变量（环境变量仍作为初始值）；`getDiagnostics` 对应今天的 `/diagnostics` |
| **Reactive**(qui) | `getComponentTree` `getHooks{nodeId}` | `componentUpdated` | ✅ | 对应今天的 `/reactive` |
| | `setHookValue` `highlightUpdates` | | ○ | 类 React DevTools：改 state、高亮重渲染 |
| **Agent**(qui) | 见 §9 | `inputRecorded` `valueChanged` `submitted` | ✅ | 高层入口 |

### 8.2 读取模型：以拉取为主

CDP 的 DOM 域是为 DevTools 前端设计的推送镜像：`getDocument` 只给浅层，之后通过 `setChildNodes` / `childNodeInserted` 维护客户端副本。这对 LLM 来说交互频繁、token 开销大。qui 的约定是：

- **主路径是拉取快照**：`getDocument{depth:-1}`、`getFullAXTree`、`Agent.snapshot` 一次返回完整子树，配合 `depth`、`nodeId` 作用域、`interestingOnly` 控制体积。
- **变更事件只发给 `DOM.enable` 过的会话**，用于 DevTools 前端和需要增量同步的工具。

### 8.3 事件缓冲

每个 target 为每个域维护一个带序号的环形缓冲。`Target.getEvents{since, domains, limit}` 让 HTTP 客户端轮询，取代今天的 `/console?since=`。WebSocket 会话事件队列满时丢弃最旧事件，并发出一条 `Target.eventsDropped{count}`，不阻塞 UI 线程。

## 9. Agent 层

这是给 LLM 的主入口，相当于把 Playwright MCP 和今天的 `/act` 合在一起。

### 9.1 快照

`Agent.snapshot{target?, root?, format: "text"|"json", maxDepth?, interestingOnly?}`

`text` 格式对齐 Playwright 的 aria snapshot，agent 普遍熟悉，token 开销比 JSON 小一个数量级：

```
- window "Untitled — Editor" [ref=@1]
  - toolbar [ref=@12]
    - button "New" [ref=@13] key=qui.new
    - button "Save" [ref=@14] [disabled]
  - textbox "Search" [ref=@20]: hello|  caret=5
  - combobox "Method" [ref=@31]: GET  options=[GET*, POST, PUT]
- dialog "Confirm" [ref=@88] layer=modal
  - button "OK" [ref=@90] [focused]
```

`json` 格式是统一节点结构：`{nodeId, tag, role, name, nameKey, value, state, classes, attrs, bounds, visible, layer, textState?, options?, children}`——今天 `/tree` 和 `/dom` 两种节点结构合一。

### 9.2 动作

`Agent.click` `doubleClick` `rightClick` `hover` `focus` `blur` `type` `fill` `selectOption` `press` `scroll` `drag` `pinch`

- `target`：selector 或 `@ref`。
- **auto-wait** 默认开启：等目标出现、可见、可操作；`timeoutMs: 0` 关闭。
- **单次解析**：动作开始时把 selector 解析为 `nodeId`，之后只认这个 id。
- `type` 与 `fill` 拆开：`fill` 是今天的快速路径（一次 `SetText`，一个 undo 条目）；`type` 逐字派发键盘事件（今天的 `raw:true`）。名字和 Playwright 一致，agent 不用读文档就会用。
- `selectOption{target, label|value}` 取代"对 combobox 用 type 选项"这个需要读文档才知道的技巧。

结果保留 qui 现在比 CDP 更好的部分：

```jsonc
{"matched": {"nodeId": 14, "selector": "button.save"},
 "treeHash": "ax-91c2…",
 "focusChange": {"from": 20, "to": 14},
 "newOverlays": [{"nodeId": 88, "layer": "modal[0]"}],
 "diagnostics": [],
 "idleState": {"dirtyRegion": false, "layoutDirty": false, "activeAnimators": 0, "pendingJobs": 0}}
```

### 9.3 等待

`Agent.waitFor{selector, state: "visible"|"hidden"|"attached"|"detached", timeoutMs}`、`Agent.waitForIdle`、`Agent.waitForStable`、`Agent.waitForOverlay{pattern, state}`。语义沿用 `wait.go`，`state` 名称对齐 Playwright。

## 10. Chrome DevTools 前端兼容

### 10.1 不需要 JS 运行时

DevTools 前端只是一个 CDP 客户端。Elements、Styles、Computed、Accessibility、投屏这些面板完全由 `DOM.*`、`CSS.*`、`Overlay.*`、`Accessibility.*`、`Page.*` 驱动，不需要页面里有 JS。需要 JS 的只有 Console 求值、Sources/断点、Event Listeners 面板（要求 `Runtime.resolveNode` 返回 JS 对象句柄）、Memory、Performance 录制这几类。

**不建议挂一个旁路 JS 运行时（例如 goja）**。原因：

- JS 运行时看不到 Go 的对象。要让 `$0.textContent` 有意义，就得把 widget / El / Reactive 的 API 整体绑定进 JS——这是一个巨大的、要长期维护的第二套 API。
- 它会打破"协议只提供声明式操作"的约定。今天 `llm.txt` 明确写着没有 `/eval`，这是安全性和可预测性的基础。
- 绑定不完整时，agent 会写出看似可行、实际报错的 JS，比"明确不支持"更糟糕。
- 新增一个大型第三方依赖，与仓库的依赖策略冲突。

Console 面板的替代方案（○ 阶段）：`Runtime.evaluate` 不执行 JS，而是把输入当作一个小型命令语言解释——`$$('.card')` 等价 `DOM.querySelectorAll`，`$0` 是当前选中节点，`qui.invoke('name', …)` 等价 `App.invoke`。其他输入返回明确的 `unsupported`。

### 10.2 难度评估：中等，难点不在 JS

| 工作项 | 难度 | 说明 |
|---|---|---|
| 启动握手打桩 | 低 | 前端连接时会依次调用 `Target.setAutoAttach`、`Page.enable`、`Page.getResourceTree`、`Runtime.enable`（需回一个虚拟的 `executionContextCreated`）、`Network.enable`、`Debugger.enable`（需回 `debuggerId`）、`Log.enable`、`Profiler.enable` 等。大部分回空结果或 `unsupported` 即可。准确清单需要 spike 时抓包确定 |
| DOM 节点格式 | 中 | CDP 的 `Node` 要求 `nodeType`、`nodeName`、`localName`、扁平的 `attributes` 数组、`childNodeCount`、独立的 `#text` 子节点。qui 的文本在 `InlineBox` run 里，需要投影出文本节点（与 §7.1 相同的问题） |
| `CSS.getMatchedStylesForNode` | 中高 | 结构复杂：`matchedCSSRules[].rule.style.cssProperties[]`、`inherited[]`、`pseudoElements[]`。只读展示不需要源码区间：不带 `styleSheetId` 的规则会显示为不可编辑。**要支持在 Styles 面板里直接改样式，`htmlcss.Rule` 需要记录源码位置**——今天的 `Rule{Selectors, Declarations}` 没有这个信息 |
| Overlay 高亮 | 低 | 复用现有覆盖层；`highlightNode` 的 config（盒模型颜色）可以先忽略 |
| 投屏 | 低中 | `startScreencast` 推 JPEG/PNG 帧；输入回传走 `Input.*`，已有 |
| 前端版本漂移 | 持续成本 | DevTools 前端跟随 Chrome 每月更新，会调新方法、改期望。需要固定一个前端版本做回归，并让未知方法一律返回 `unsupported` 而不是断连 |
| 鉴权 | 中 | DevTools 前端无法发送 `Authorization` 头。不能为此放宽 TCP 鉴权。方案：DevTools 兼容需显式开启（`QUI_AGENT_DEVTOOLS=1`），只绑定 loopback，WebSocket 路径中带一次性能力令牌（`/devtools/<token>/page/<id>`），并校验 `Origin`（只允许 `devtools://` 和配置的来源）。UDS 上的连接不受影响 |

前端获取方式：Chrome 自带 `devtools://devtools/bundled/inspector.html?ws=127.0.0.1:<port>/devtools/<token>/page/<id>`，也能通过 `chrome://inspect` 的"Discover network targets"发现。qui 不需要打包前端。

### 10.3 范围建议

首期只做到"能连上 + Elements/Styles/Computed 只读 + 高亮 + 投屏"。可编辑样式依赖 htmlcss 记录源码位置，列为单独的后续项。

## 11. MCP 层

`qui-agent mcp`：一个 stdio MCP server，工具名对齐 chrome-devtools-mcp / Playwright MCP，让已经会用这两者的 agent 零学习成本：

| 工具 | 映射 |
|---|---|
| `take_snapshot` | `Agent.snapshot{format:"text"}` |
| `click` / `hover` / `fill` / `type` / `select_option` / `press_key` / `drag` | `Agent.*`，参数 `uid` 即 `@ref` |
| `take_screenshot` | `Page.captureScreenshot` |
| `wait_for` | `Agent.waitFor` |
| `list_console_messages` | `Target.getEvents{domains:["Log","Runtime"]}` |
| `get_styles` | `CSS.getMatchedStylesForNode` |
| `list_targets` / `select_target` | `Target.*` |

工具定义从 `protocol.json` 生成，不手写。

## 12. Schema 与代码生成

方法在 Go 中声明，参数和结果是普通结构体：

```go
// 在 htmlcss 的 init 中注册
qui.RegisterDomain(qui.Domain{
	Name: "CSS",
	Commands: []qui.Command{
		qui.Cmd("getMatchedStylesForNode", "Rules that matched a node, in cascade order.",
			func(ctx qui.InspectContext, p GetMatchedStylesParams) (GetMatchedStylesResult, error) { … }),
	},
	Events: []qui.Event{ … },
})
```

- 注册表通过反射生成 `protocol.json`，形状对齐 CDP 的 `browser_protocol.json`（`domains[].commands[].parameters[]`），可被现有 CDP 工具链读取。
- `llm.txt` 拆为两部分：手写的使用指南（概念、选择器优先级、常见流程、陷阱），以及从 schema 生成的方法参考。
- `cmd/qui-agent` 的子命令与 MCP 工具定义同样从 schema 派生。
- CI 检查：生成物与提交内容一致，否则失败。

## 13. 包结构与线程

- **root** 定义 `Domain` / `Command` / `Event` / `InspectContext` 和注册表，实现与引擎直接相关的域，并提供兜底的 DOM/CSS（基于 widget 树）。
- **htmlcss**、**reactive** 在 `init` 中注册各自的域，覆盖兜底实现。应用只要导入了 htmlcss，DOM/CSS 就是 HTML 级别的；纯原生 widget 应用仍然有可用的 DOM。这和 `database/sql` 的驱动注册是同一个模式，符合"root 不导入子包"的规则。
- **agent** 只依赖 root：传输、鉴权、会话、事件队列、发现，以及 DevTools 兼容桩。§3 P8 的反向依赖随之消失。`CLAUDE.md` 的依赖规则不需要为 agent 增加例外。
- **线程**：命令默认通过 `Window.RunSynced` 在 UI 线程执行；注册时可声明 `ReadOnlySnapshot`，读取已有快照的命令不占用 UI 线程。事件在 UI 线程产生，非阻塞地写入各会话队列（§8.3）。

## 14. 迁移计划

不保留旧 REST 接口。按依赖顺序分阶段推进，每阶段完成后仓库处于可用状态。

| 阶段 | 内容 | 完成标准 |
|---|---|---|
| M1 节点身份 | root 加全局 `nodeId` 和懒分配；AXNode / DOMNode / Reactive 节点带 `nodeId`；统一 selector 解析器支持 `@id`；`stale_node` | 现有测试通过；新增"插入兄弟节点后 nodeId 不变""detach 后报 stale_node"测试 |
| M2 协议骨架 | root 域注册表；agent 改为 WebSocket + `POST /rpc`；JSON-RPC 格式与错误码；迁移 Accessibility、DOM、CSS、Input、Page.captureScreenshot、Agent、Debug.getDiagnostics、Reactive.getComponentTree；**删除全部旧 REST 端点**；`cmd/qui-agent` 基于新协议重写 | 旧 `agent_test.go` / `dom_test.go` 的场景全部用新协议重写并通过；`protocol.json` 生成且有 CI 检查 |
| M3 App 级与 Target | `BindEnv(app)`；多窗口 target；`/json/*`；发现文件 | `multiwindow-sessions` 两个窗口都可驱动 |
| M4 事件 | Log、Runtime.exceptionThrown、Page.frameCommitted、Overlay inspect、DOM 变更事件、`Target.getEvents` | 能捕获一次 panic 和一条 slog 日志 |
| M5 运行时控制 | Debug.setFlags、Emulation、Animation | `QUI_DEBUG_PAINT` 能在运行时开关 |
| M6 llm.txt 与 MCP | 拆分 llm.txt；`qui-agent mcp` | 用 MCP 在 `examples/reactive-html` 跑通一个完整任务 |
| M7 DevTools spike | §10.3 的范围 | 用 Chrome 自带 DevTools 打开 `examples/html-css`，Elements + Styles 只读可用 |

同步更新：`README.md` / `README.zh.md`、`docs/features*.md`、`CLAUDE.md` 中的 agent 命令示例，以及 `examples/*` 里调用 `agent.BindEnv` 的 14 处。

## 15. 待决问题

1. **文本节点身份**：`#text` 节点在 El 侧懒分配 id，文本变化时保留还是换新？CDP 是保留（`characterDataModified`）。倾向保留。
2. **WebSocket 库**：`golang.org/x/net/websocket`（已是依赖，功能够用）还是 `github.com/coder/websocket`（维护更积极，需新增依赖）。倾向前者，必要时再换。
3. **快照中的 widget 管线节点**：今天 `/dom` 会折叠 InlineBox、ScrollView、backing control 等管线节点，`/tree` 不折叠。统一后的 `Agent.snapshot` 默认折叠，用 `interestingOnly:false` 展开——是否足够？
4. **`App.invoke` 的安全边界**：应用注册的命令可能有副作用。是否需要按命令声明 `readOnly`，并让 MCP 层默认只暴露只读命令？
5. **DevTools 能力令牌**：一次性令牌放在 URL 路径里，是否接受它可能出现在本地日志中的风险？替代方案是只允许 UDS + 一个本地转发器。
6. **panic 捕获策略**：捕获后上报 `Runtime.exceptionThrown`，然后是继续运行（丢弃这次事件/job，界面可能处于半更新状态），还是上报后重新 panic（与不接 agent 时行为一致）？倾向默认重新 panic，用 `Debug.setFlags{survivePanics:true}` 显式切换为继续运行，方便 agent 在现场继续检查。
7. **CSS 源码位置**：为了可编辑的 Styles 面板，`htmlcss` 的解析器要记录规则和声明的源码区间——这会让 `Rule` 变大，是否只在启用 agent 时记录？

## 16. 实施记录（2026-10-27）

M1–M7 已全部落地，旧 REST 端点已删除。以下是与上文设计的偏差，以及留待后续的事项。

### 16.1 与设计的偏差

| 位置 | 设计 | 实际 | 原因 |
|---|---|---|---|
| §5.2 WebSocket | 用 `golang.org/x/net/websocket` | 自写的最小 RFC 6455 实现（`agent/internal/ws`，约 300 行，仅标准库） | 仓库的 `vendor/` 被 git 忽略且不含该包；自写实现也能精确控制 Origin 校验和帧大小上限 |
| §6.1 绑定方式 | `BindEnv(app)` | 保留 `BindEnv(window)` 的签名，但服务进程内全部窗口（`qui.LiveWindows`） | 14 个示例零改动；多窗口应用只需一次调用 |
| §7.3 统一选择器 | 一个合并的解析器 | 一个入口、两套语法、确定性规则：控件语法能解析就按控件语法，否则交给注册的 CSS 引擎（`qui.RegisterSelectorEngine`）。`Window.Find/Click/WaitFor` 也因此直接支持 CSS | 两套语法在 `[name=]` 上语义冲突（无障碍名称 vs HTML 属性），合并会破坏现有脚本；确定性规则消除了旧实现“失败后换语法重试”的不可预测性。CSS 中的 HTML `name` 属性需加标签限定：`input[name=email]` |
| §7.3 `@418 .row` 作用域写法 | 支持 | 未实现；用 `DOM.querySelector {nodeId, selector}` 做子树查询 | 使用频率低 |
| §7.1 文本节点 | El 侧懒分配 | htmlcss 的文本段本身就是 El，天然有 id；只有“叶子元素自带的文本”需要合成节点，其 id 跟随元素生命周期，文本变化时保留（回答了 §15 问题 1） | — |
| §12 schema 生成物 | 提交生成文件 + CI 检查 | `/json/protocol` 和 `/llm.txt` 的方法参考都在运行时由注册表生成，没有需要同步的生成文件 | 从结构上杜绝漂移，不需要 CI 检查 |
| §11 MCP 工具 | 从 schema 生成 | `Agent.*` 命令自动成为工具（输入 schema 由协议 schema 转换），另有 10 个手写工具（截图、日志、HTML、样式、窗口选择、拾取、高亮、组件树、通用调用） | 截图等需要特殊的内容类型 |
| Debug 开关 | — | 原 `QUI_DEBUG_*` 的包级 `bool` 改为 `atomic.Bool`，环境变量仍是初始值 | 运行时切换需要并发安全 |

### 16.2 未实现（列入后续）

- `Performance` 域（帧耗时、layout 次数、重绘面积、glyph cache 命中）。
- `App.listCommands` / `App.invoke`（应用自注册命令）。
- `CSS.setStyleTexts`（需要 htmlcss 记录源码位置，§15 问题 7）和 `CSS.forcePseudoState`。
- `Reactive.setHookValue` / `highlightUpdates`。
- 细粒度 DOM 变更事件（`childNodeInserted` 等）：目前只有粗粒度的 `DOM.documentUpdated`（基于 AX 树哈希，仅在有会话启用 DOM 时计算）。
- `Emulation.setEmulatedMedia`：qui 只有一套浅色主题，没有可切换的配色方案。
- webview 子 target（代理 CEF 自身的 CDP）。

### 16.3 未验证（需要人工）

- **真实的 Chrome DevTools 前端尚未连接验证。** 已用测试按 DevTools 的启动顺序回放了一遍调用（`agent/devtools_test.go`），并在 headless 应用上验证了投屏帧和 ack 流控；但 DevTools 前端版本众多，实际打开 `devtools://devtools/bundled/inspector.html?ws=127.0.0.1:<port>/devtools/t/<token>/page/window-1` 时可能还会遇到未桩化的方法。启动方式：`QUI_AGENT_TCP=127.0.0.1:9222 QUI_AGENT_TOKEN=<token> QUI_AGENT_DEVTOOLS=1 ./app`。
- `chrome://inspect` 的自动发现不可用：它请求 `/json/list` 时无法携带 token。需要直接打开上面的 URL。

### 16.4 §15 待决问题的结论

1. 文本节点 id：保留（见 16.1）。
2. WebSocket 库：自写（见 16.1）。
3. 快照折叠：默认折叠无名称的管线节点，以及只重复父节点文本的子节点；`all:true` 展开全部。
4. `App.invoke` 安全边界：功能未实现，问题保留。
5. DevTools 令牌：放在 URL 路径中，仅在显式开启 `QUI_AGENT_DEVTOOLS=1` 且配置了 TCP + token 时生效；服务端日志里打印的 URL 用 `$QUI_AGENT_TOKEN` 占位，不输出真实令牌；另外拒绝任何带非 `devtools://` Origin 的 WebSocket 请求。
6. panic 策略：默认上报后重新 panic（保留原始栈），`Debug.setFlags {survivePanics:true}` 切换为继续运行。没有 agent 订阅 Runtime 事件时完全不 recover，行为与之前一致。
7. CSS 源码位置：未做，随 `CSS.setStyleTexts` 一起处理。
