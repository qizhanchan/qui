# RFC 0005：页面线程——把网页的 DOM、样式、布局和 JS 移出窗口主线程

状态：草案（2026-11-20），第 8 节已按 Chromium 源码调研定稿，待评审
关联：RFC 0004（显示列表、工作线程光栅化、合成线程）、q-browser `internal/browser/tab.go`、`ui_thread.go`

## 1. 问题

在 q-browser 里打开一个新页面时，整个窗口会卡住：鼠标转圈，标签栏、地址栏和其他标签页都没有响应。下面是 2026-11-20 在 github.com 首页点进 `/pricing` 的实测（1280 宽，真实窗口，`QUI_DEBUG_IDLE=1`）：

| 阶段 | 所在线程 | 耗时 |
|---|---|---|
| 网络请求 | 后台 goroutine | 0.5–1.9s |
| 准备文档：解析 HTML，拉取 CSS 和字体 | 后台 goroutine | 1.2–2.2s |
| `htmlcss.RenderDoc`：建元素树、首次样式计算 | **窗口 UI 线程** | 1.3s |
| 首次布局 | **窗口 UI 线程** | 10.1s（其中 8s 是容器查询在布局中反复重算子树） |
| JS：脚本执行、React 水合、定时器、观察器回调 | **窗口 UI 线程** | 一直占用（单次 0.1–0.9s 的任务很常见） |

UI 线程上最长的一步卡了 **11.7 秒**。原因不是某一个函数慢，而是架构本身：

1. **页面的所有工作都在窗口的 UI 线程上做。** `El` 本身就是 qui 控件，直接挂在窗口的控件树里。页面的样式、布局、绘制、命中测试、焦点和 JS 回调，与浏览器自己的界面（标签栏、地址栏、菜单）共用同一个线程、同一棵树。
2. **所有标签页共用这条线程。** 一个重页面会拖慢每个标签页，后台标签页的 JS 也会抢前台的帧。
3. **导航时没有“保留旧页面”。** `Tab.render` 会立刻用新文档替换旧文档。新页面在首次布局完成之前，什么也画不出来。

局部优化能缓解某个具体页面（例如容器查询的 10 秒，见第 7 节 P0），但只要页面工作还在 UI 线程上，下一个重页面照样会卡住整个窗口。

## 2. 参照：Chromium 的线程与进程划分

| Chromium | 负责 | 页面卡住时 |
|---|---|---|
| 浏览器进程 UI 线程 | 浏览器界面（标签栏、地址栏、菜单）、把输入路由给页面、导航决策 | 不受影响 |
| 浏览器进程 IO 线程、网络服务 | 网络、IPC | 不受影响 |
| renderer 进程**主线程**（每个站点一个） | DOM、样式、布局、绘制录制（`PaintRecord`）、JS、命中测试 | 只有该页面卡住 |
| renderer 合成线程 | 层树提交、滚动、transform/opacity 动画 | 继续滚动、继续动画 |
| 光栅化工作线程、GPU（Viz）进程 | 瓦片光栅化、合成、呈现 | 不受影响 |

几个关键做法：

- **浏览器界面线程从不运行页面代码。** 页面卡死时，标签栏和地址栏照常响应；超过阈值后弹出“页面无响应”，用户可以等待或关闭。
- **页面以“提交（commit）一帧”的方式与外界交互。** 主线程产出不可变的显示列表和层树，交给合成器；之后合成器只用这份快照，不再回头读 DOM。
- **滚动由合成器处理（impl-side scrolling）。** 主线程忙时滚动照样流畅，滚动偏移再异步通知主线程（`scroll` 事件晚到一些）。
- **导航时保留旧页面（paint holding）。** 新文档提交并完成首次有内容绘制之前，一直显示旧页面，只有标签上的图标转圈。
- **输入事件异步投递。** 浏览器把事件发给 renderer，renderer 自己做命中测试；鼠标指针形状、提示框等由 renderer 回传。

## 3. 目标架构

```
窗口 UI 线程（OS 主线程）                         页面线程（每个标签页 / 文档一个 goroutine）
─────────────────────────                         ─────────────────────────────────────
浏览器界面：标签栏、地址栏、菜单                  PageHost：自己的任务队列、帧时钟、rAF
PageView 控件（每个标签页一个）                    htmlcss 引擎 + 元素树、样式、布局
  · 显示最近一次提交的帧                           JS 运行时（sobek，本来就是单线程模型）
  · 滚动、缩放在本线程处理（不等页面）            命中测试、焦点、选区、表单控件状态
  · 把输入事件转发给页面线程           ──────▶     处理事件 → 样式/布局 → 录制 Picture
  · 执行页面请求的系统服务             ◀──────     请求：光标、提示框、IME 光标位置、下拉框、
    （光标、IME、弹层、剪贴板、对话框）             右键菜单、剪贴板、文件选择、标题、URL
合成：内容瓦片 + 动画层 + 视频        ◀──────     提交 PageFrame（不可变快照）
（RFC 0004 的光栅化工作线程 / 合成线程）
```

### 3.1 PageHost：可以由任意线程驱动的“嵌入式窗口”

不另造一套页面运行时，而是**复用 `qui.Window` 的全部机制**：焦点、弹层、提示框、选区、任务队列、动画器、帧时钟、`RequestAnimationFrame`、`AfterLayout`、agent 自省都在里面。做法是新增一个窗口后端 `internal/platform/embedded`（与 `headless` 并列），由它实现 `contract`：

- **尺寸与缩放**来自宿主 `PageView`（`SetViewport(w, h, scale)` 消息）。
- **事件循环**：一个 goroutine 运行 `PageHost.Run()`。它在有任务、到期的 rAF 或定时器、或有输入时调用 `Window.Step`，其余时间阻塞在通道上。窗口的 `uiThread.owner` 绑定到这个 goroutine 锁定的 OS 线程上（`runtime.LockOSThread`），`assertUIThread` 的语义保持不变：“控件只能由拥有它的窗口线程访问”。
- **呈现**：`Step` 结束时不呈现像素，而是生成一个 `PageFrame`（3.2 节）交给宿主。
- **系统服务**：光标、IME、剪贴板、弹层窗口、对话框等原本直接调 OS 的地方，在 embedded 后端里改成发消息给 UI 线程（3.4 节）。

`ui_thread.go` 已经是按窗口绑定线程的（`Window.uiThread.owner`），这是现成的基础；`PostJob` / `*Synced` 系列是现有的跨线程接口，保持不变。

### 3.2 提交：PageFrame

页面线程每完成一帧，就产出一个不可变快照，通过“单槽邮箱”交给 UI 线程（新的覆盖未取走的旧的；UI 线程总是取最新的一份）：

| 字段 | 内容 | 来源 |
|---|---|---|
| `Content` | 文档内容的显示列表，文档坐标，不含滚动 | RFC 0004 R1 的 `Picture`（在 RasterBackend 层录制） |
| `Damage` | 相对上一帧变化的文档区域 | 现有的脏区域 |
| `Extent` / `ScrollOffset` | 文档尺寸、页面线程认为的滚动位置（JS `scrollTo` 之后才变） | ScrollView |
| `Layers` | fixed/sticky 层、动画层（曲线）、视频与 GPU 内容的 retained draw、遮挡关系 | RFC 0004 R4 的层集合提交快照 |
| `ScrollTree` | 每个可滚动节点：id、父节点、元素 id、容器矩形、内容尺寸、当前偏移、两个方向是否可滚动、`overscroll-behavior`、是否“快速”及原因（8.2 节） | 页面布局 |
| `HitRegions` | 只用于“不等页面也能决定”的事，按绘制顺序：快速滚动区（元素 id）、需要页面命中测试的区域（非快速或嵌套重叠的滚动区、滚动条）、阻塞型 wheel 监听区、`touch-action` 区、链接区域（悬停时在状态栏显示 URL） | 页面布局 |
| `Meta` | 标题、URL、favicon、主题色、光标、加载状态、提交序号 | Tab |

UI 线程的 `PageView`：
- 用 R3 的瓦片缓存显示 `Content`。瓦片从提交的 `Picture` 在光栅化工作线程上回放，**不再碰元素树**。`Damage` 让相交的瓦片失效。
- 用 `Layers` 驱动 fixed/sticky 和 R4 的合成线程动画。
- 页面线程卡住时，`PageView` 继续显示上一帧，滚动、缩放、动画都正常。

### 3.3 滚动与缩放在 UI 线程处理

（细节见 8.2 节的调研结论。）

- 滚轮和触控板滚动由 `PageView` 立即应用到提交帧上，再异步把新偏移发给页面线程。页面线程在下一帧开始、执行脚本和布局之前应用这些偏移，每帧最多派发一次 `scroll` 事件，并驱动 IntersectionObserver 和懒加载。
- 页面可能阻止滚动的情况：落在**非 passive** 的 `wheel` / `touch` 监听器区域内。这时只有手势的第一个 wheel 事件需要等页面线程回复，同一手势的后续事件一律非阻塞，与 Chromium 一致（没有超时，页面卡死由 3.6 节的无响应检测兜底）。挂在 window、document、`<html>`、`<body>` 上的监听器默认 passive。
- 嵌套的可滚动元素：符合“快速”条件的，第一期就做成独立的合成层，由 UI 线程滚动；其余的交给页面线程滚动（8.2 节）。
- 页面缩放（Cmd +/−）在 UI 线程先按位图缩放显示，等页面线程按新的 zoom 重新布局并提交后再替换（与 Chrome 的做法一致）。

### 3.4 输入与系统服务：消息通道

UI 线程 → 页面线程（有序队列，连续的鼠标移动和滚轮合并）：
- 鼠标、键盘、滚轮、触控板手势、IME 提交和预编辑文本、拖放进入/离开/放下、窗口焦点变化、可见性（标签页切到后台）、视口尺寸和缩放、系统外观（深色模式）。

页面线程 → UI 线程（请求；需要回复的带 id）：

| 请求 | UI 线程的处理 |
|---|---|
| 光标形状、提示框、链接悬停 URL | 直接应用 |
| IME：输入框位置（`CaretRect`）、开始或结束输入 | 更新 `IMEClient` 代理 |
| `<select>` 下拉、右键菜单、`<input type=date/color>` 选择器 | 在 UI 线程弹出原生弹层，结果回传（异步，HTML 本来就是异步的） |
| 剪贴板写入；剪贴板读取（异步 API） | 执行，读取的结果回传 |
| 文件选择、下载、`window.open`、导航、标题和 URL 变化 | 交给浏览器层 |
| 全屏、指针锁定、权限请求 | 交给浏览器层 |

原则：**页面线程永远不同步等待 UI 线程**（否则 UI 线程同步等页面时会死锁），需要回复的都做成异步回调。这与 Web 平台 API 的异步语义天然一致。

### 3.5 线程安全的地基

现在“同一时刻只有一个线程在做样式和布局”是隐含前提。多个页面线程加上 UI 线程并发之前，必须先清理包级可变状态（2026-11-20 盘点，不完整，第 P1 阶段要用 `-race` 全面审计）：

| 位置 | 状态 | 处理 |
|---|---|---|
| `htmlcss/viewport_units.go` `viewportUnits` | 样式计算时由 `useViewport` 临时改写 | 放进 `StyleEngine`，计算时通过参数或计算上下文传递 |
| `htmlcss/values.go` `remPx` | `interpret` 每次写入 | 同上 |
| `htmlcss/media.go` `devicePixelRatio`、`ua.go` `webCompat` | 全局开关 | 放进 `StyleEngine` 选项 |
| `htmlcss/click_propagation.go` `curClick` | 点击派发的进行中状态 | 放进引擎或窗口 |
| `layout.go` `pctIndefinite`（`IntrinsicWidthMeasurement()`） | 测量时递增 | 放进布局上下文（按根，或经由 `Widget.Window()` 取窗口级状态） |
| `layout_measure_cache.go` `layoutEpoch` 等 | 全局失效纪元 | 原子变量或窗口级 |
| `internal/typeset` 字体注册表、回退链、塑形缓存 | R2 已审计光栅化路径，塑形和测量路径还没有 | 加锁或做成每线程缓存 |
| 图片解码缓存、`frameworkCSS` | 已有锁或只读 | 核对 |

验收：两个页面线程和 UI 线程同时持续重样式、重布局 60 秒，在 `-race` 下零报告，渲染结果与串行执行逐字节一致。

### 3.6 导航与“页面无响应”

- **保留旧页面**：`Tab.load` 新建一个 PageHost 加载新文档，旧 PageHost 继续显示、继续接收输入，直到新页面提交第一帧有内容的 `PageFrame`（或超时 3s）才切换。期间只有标签图标转圈，地址栏显示新 URL，可以随时停止或后退。
- **卡死检测**：沿用 Chromium 的参数：UI 线程发出的输入事件超过 15s 没有回执，就向页面线程发一次 ping，再等 1s；仍无回应且标签页可见时，显示“页面无响应：等待 / 关闭页面”，列出共用这条页面线程的所有标签页（8.1 节）。关闭时中断 JS（`vm.Interrupt`）并丢弃这个 PageHost；“等待”则重新计时。后台标签页不检测。
- **崩溃隔离**：页面线程里的 panic 只终结这个 PageHost，标签页显示崩溃页（现有的 `recoverCrash`），其他标签页和浏览器界面不受影响。注意 Go 无法从 `fatal error`（并发写 map 等）中恢复，所以真正的隔离要靠第 P6 阶段的多进程。

### 3.7 后台标签页

节流按页面而不是按线程（同一条页面线程上可能有多个页面），参数取 Chromium 的默认值：

| 规则 | 参数 |
|---|---|
| 不可见页面不提交帧，rAF 暂停 | — |
| 定时器唤醒对齐 | 1s（`internal/js/throttle.go` 已有，移到 PageHost） |
| CPU 预算节流 | 隐藏 10s 后启用；预算为 1% CPU，初始 1s，上限 3s |
| 强化唤醒节流 | 每分钟一次；宽限期为加载中 5 分钟、已加载 60 秒；标题或 favicon 刚变化的 3 秒内不启用 |
| 冻结 | 后台 1 分钟后冻结页面（任务队列暂停） |

另外，最多 N 个（默认 = CPU 核数）页面线程同时可运行，其余排队，避免标签页很多时把 CPU 抢光。

## 4. 已有、并沿用到目标架构的部分

| 已有 | 在目标架构里的位置 |
|---|---|
| 按窗口绑定的 UI 线程所有权（`ui_thread.go`）、`PostJob` / `*Synced` | PageHost 的线程模型，无需新概念 |
| R1 `Picture` 显示列表 | `PageFrame.Content` |
| R2 光栅化工作线程、R3 内容瓦片 | `PageView` 从提交的 Picture 光栅化，不碰元素树 |
| R4 层集合提交快照、合成线程动画 | `PageFrame.Layers` |
| q-browser 后台 goroutine 中的取文档与准备 | 不变，结果改为投递到 PageHost 而不是窗口 |
| `internal/js` 定时器对齐、后台节流、帧驱动 rAF | 移到 PageHost 的帧时钟上 |
| `headless` 窗口后端 | `embedded` 后端的模板 |

## 5. 考虑过、没有采用的方案

- **在 UI 线程上切片（React 式让出）**：布局是递归的整体计算，JS 也无法在任意位置暂停，切片只能把 11 秒切成很多段，窗口仍然一卡一卡，而且每个功能都要写让出点。否决。
- **只把样式和布局放到后台、JS 留在 UI 线程**：JS 随时同步读取几何信息（`getBoundingClientRect`、`offsetWidth`），这会变成跨线程同步等待，而 JS 本身在 react.dev、GitHub 上就是主要耗时之一。否决。
- **一开始就做多进程（每站点一个 renderer 进程）**：崩溃隔离和安全沙箱最好，但需要跨进程传输显示列表和位图（共享内存）、跨进程 IME 和弹层、进程管理等，工程量是线程方案的数倍。线程方案的接口（PageFrame 提交 + 消息通道）本身就是可序列化的形状，以后拆进程可以复用。列为 P6。

## 6. 风险

- **接口面大**：`El` 和 `widgets` 里有大量直接调用窗口和系统服务的地方（`assertUIThread` 约 219 处）。embedded 后端必须覆盖 `contract` 的每一个能力，漏掉一个就会在某个交互上静默失效。对策：`contract` 一致性测试同时跑 headless 和 embedded；`QUI_DEBUG_THREAD=1` 在 q-browser 中默认打开。
- **跨线程选区和查找**：页面内选区、页内查找（`find.go`）、复制都要通过消息完成；跨越浏览器界面和页面的选区不再支持（Chrome 也不支持）。
- **agent / DevTools 自省**：协议请求要路由到对应 PageHost 的线程执行（现有的 `*Synced` 接口改为按窗口投递），节点 id 仍然全局唯一。
- **内存**：每个提交帧带一份 Picture；只保留“当前显示”和“最新提交”两份。iframe 第一期与父文档同线程。
- **测试**：大量测试用 `NewTestWindow` 在当前 goroutine 同步运行。embedded 后端需要提供同样的同步测试模式（`DrainJobsForTest` 等），保证现有测试不用改。

## 7. 推进顺序与验收

| 阶段 | 内容 | 验收 |
|---|---|---|
| **P0** 先止血 | 修容器查询的重算风暴：布局试量时不触发子树重算，或按宽度缓存每个容器的样式结果；UI 线程长任务统计（`QUI_DEBUG_IDLE` 加“>50ms 任务”的来源） | `/pricing` 首次布局从 10s 降到 1s 以内 |
| **P1** 线程安全 | 3.5 节的全局状态全部移除；塑形和测量路径的并发审计 | 3.5 节的 `-race` 并发验收 |
| **P2** PageHost | `internal/platform/embedded` + `PageHost.Run`；PageFrame 提交；同步测试模式 | 现有 htmlcss / q-browser 测试在 embedded 后端下全部通过 |
| **P3** PageView | 从提交帧显示、瓦片光栅化、UI 线程滚动和缩放、层与合成线程衔接 | JS 里人为制造 2s 死循环时，滚动和走马灯仍保持 60fps，标签栏可以切换 |
| **P4** 输入与服务 | 3.4 节的消息通道全覆盖：IME、弹层、剪贴板、拖放、光标、提示框 | 中文输入、`<select>`、右键菜单、拖拽上传在 react.dev / GitHub / MDN 上与现在一致 |
| **P5** q-browser 接入 | Tab 改为每个文档一个 PageHost；JS 运行时迁移；保留旧页面；无响应检测；后台调度；agent 路由 | 打开 `/pricing` 时，UI 线程 p99 帧间隔 ≤ 20ms，旧页面在新页面提交前保持可滚动；20 站点 sitecheck 无回退 |
| **P6**（可选） | 多进程：PageFrame 和消息通道走共享内存 + IPC | 页面里的 `fatal error` 不再杀死浏览器 |

P0 与架构无关，可以立刻做。P1 是后面所有阶段的地基，也能顺带让 RFC 0004 的光栅化工作线程更安全。P2 到 P5 依次进行，每阶段单独提交。

## 8. 调研结论：待定问题的答案（Chromium 源码，`/opt/cpp_work/chromium/src`，2026-11-20）

### 8.1 页面线程的粒度：按“相关浏览上下文组 + 站点”，不是按文档

**Chromium 的做法**

- **BrowsingInstance**（`content/browser/browsing_instance.h:34-56`）是一组可以互相持有脚本引用的标签页和框架（通过 opener、frames 等）。同一 BrowsingInstance 内同一站点的文档“可能同步互相访问脚本，因此必须在同一个进程里”。在 Blink 里对应 agent cluster（`window_agent_factory.h:22-60`）：同步访问 `opener.document` 是直接访问堆，必须共享一个事件循环。
- **站点** = scheme + eTLD+1（`site_info.cc:1329-1335`，忽略端口和子域）。跨站的窗口和框架之间只有 `postMessage` 和受限的 WindowProxy，所以可以分属不同进程。`noopener` 和 COOP 会切断关联，新建一个 BrowsingInstance。
- **一个 renderer 进程只有一个 Blink 主线程**。进程里的所有页面和框架共享它，通过 AgentGroupScheduler → PageScheduler → FrameScheduler 分成独立的任务队列和节流策略（`main_thread_scheduler_impl.cc:2365`、`page_scheduler_impl.cc`）。
- **进程复用**（`process_reuse_policy.h:23-55`，`render_process_host_impl.cc:5471-5630`）：
  - 桌面上默认开启 `kProcessPerSiteUpToMainFrameThreshold`：同站点最多 2 个主框架共用一个进程，总内存上限 2GiB（`content_features.cc:1104-1134`）。
  - 进程数软上限为 物理内存 MiB ÷ 2 ÷ 85MB，最少 3 个（`render_process_host_impl.cc:1531-1604`）。超过上限后复用一个随机的合适进程。
  - 预备一个空闲的 spare renderer，用来掩盖进程启动时间。
- **无响应**（`components/input/input_constants.h`）：桌面 15s（Android 5s）没有输入回执，再 ping 1s。弹窗列出共用这个进程的所有页面，“关闭页面”会结束整个进程（`hung_renderer_view.cc`）。

**我们的决定**

1. 页面线程的键是 **（相关浏览上下文组，站点）**。q-browser 里，用 `window.open`（不带 `noopener`）打开的同站点窗口，以及同站点的 iframe，与父文档共用一条页面线程。这样同步脚本访问不需要跨线程。
2. 无关的标签页即使同站点，默认也各用一条线程，这是比 Chromium 更强的隔离。只有页面线程数超过上限时，才把同站点的新标签页合并到已有线程上，对应 Chromium 的 MainFrameThreshold；上限的初始值取 CPU 核数 × 2。
3. 跨站 iframe 第一期仍与父文档同线程。等 `postMessage` 和 WindowProxy 代理就绪后，再按站点拆开（对应 Chromium 的 OOPIF）。
4. 一条页面线程上可以有多个页面，所以 PageHost 内部按页面和框架维护任务队列与节流状态，与 Blink 的调度层级一致。
5. 预备一个空闲的 PageHost（spare）：引擎和 JS 运行时提前建好，导航时直接取用。

### 8.2 嵌套滚动区域：快速滚动区第一期就做成合成层

**Chromium 的做法**

- **决策在绘制之后、分层时做**（`paint_artifact_compositor.cc:306-353`）。不可由用户滚动的（`overflow:hidden`）永远不合成。在 HiDPI（设备缩放 ≥ 1.5）下 LCD 文本偏好为“忽略”，于是**所有可由用户滚动的区域都会合成**（`widget_base.cc:2072-2092`）。`<select>`、文本输入框、canvas 子树倾向于不合成（`paint_layer_scrollable_area.cc:2771-2789`）。
- **不合成的滚动区现在也能在合成线程上滚动**：`RasterInducingScroll` 已是 stable，做法是按新的偏移重新光栅化同一份显示列表（`property_tree.cc:1877-1890`）。超出 Blink 绘制时的 cull rect 才需要主线程重绘。
- **主线程原因**拆成了三组（`cc/input/main_thread_scrolling_reason.h`）：
  - 必须由主线程重绘：`background-attachment: fixed`、滚动时背景需重绘、不透明度和 LCD 文本条件不满足、偏好不合成。
  - 必须由主线程做命中测试：滚动条、主线程命中区域、命中不可靠。
  - 仅用于统计：wheel 和 touch 监听区域。
  - 此外，滚动内容里有 touch-action 区、wheel 监听区、滚动条或选区时，也强制主线程重绘（`paint_artifact_compositor.cc:382-427`）。
- **命中与锁定**：
  - 合成线程按层的滚动命中区找目标（`input_handler.cc:1835-1905`）。嵌套的非合成滚动区、或与其他内容重叠的，标为“需要主线程命中测试”：Blink 算出目标元素 id 后回注，**之后的滚动仍在合成线程**（`input_handler_proxy.cc:526-575`）。
  - 每个手势只锁定一次目标，沿父链找第一个还能滚动的节点；`overscroll-behavior` 会截断滚动链（`input_handler.cc:2341-2425`）。
- **wheel**：只有手势的第一个事件可以被取消，没有超时，后续事件都是非阻塞的（`mouse_wheel_event_queue.cc:249-311`）。document 级监听器默认 passive（`event_target.cc:455-520`）。
- **偏移回传**：合成线程把滚动增量放进下一次 BeginMainFrame。Blink 在脚本和布局之前应用这些增量，在渲染步骤里每帧派发一次 `scroll` 事件，在 rAF 之前（`layer_tree_host.cc:1256-1290`、`page_animator.cc:267-272`）。两边的偏移用“基准值 + 增量”同步（SyncedProperty）。

**我们的决定**

1. **第一期**：UI 线程滚动根视口，以及页面线程标为“快速”的嵌套滚动区。“快速”的条件是：
   - 可由用户滚动；
   - 没有 `background-attachment: fixed`，滚动时背景不需重绘；
   - 不是 `<select>`、文本输入框或 canvas 子树；
   - 子树里没有非 passive 的 wheel/touch 监听器、没有滚动条命中区、没有选区。
2. **快速嵌套滚动区第一期就做成独立合成层**：一张比可见区大的预绘制内容纹理，复用 R3 瓦片，对应 Chromium 的 `is_composited` 路径。我们的屏幕基本都是 HiDPI，这也是 Chromium 在 HiDPI 上的实际行为。“重新光栅化滚动”（RasterInducingScroll）留到第二期，因为它需要 cull rect 未覆盖时回退页面重绘的机制。
3. **其余滚动区交给页面线程**，但命中测试也可以学 Chromium：命中有歧义时向页面线程要目标元素 id，拿到后由 UI 线程滚动。前提是目标恰好是快速滚动区。
4. **每帧发布** `ScrollTree` 和按绘制顺序的 `HitRegions`（3.2 节表格）。偏移以“基准 + 增量”同步，页面线程在下一帧开始时应用增量，每帧最多派发一次 `scroll`。

### 8.3 普通 qui 应用：提供给“嵌入式 HTML 面板”，但原生界面保持单线程

**Chromium 的做法**

- **浏览器自己的 HTML 界面都在 renderer 进程里**，从不在 UI 线程上运行：WebUI、侧边栏、扩展弹窗、标签搜索气泡、DevTools。`views::WebView`（`ui/views/controls/webview/webview.h:45-59`）在 Views 树里只放一个占位层，经由 `SetShowSurface` 引用 renderer 的 viz surface（`delegated_frame_host.cc:291,383`）。
  - renderer 慢的时候，浏览器继续显示该 surface 的上一帧，自己的帧不受影响。
  - 输入经 `RenderWidgetHostInputEventRouter` 异步转发，UI 线程从不等待回复。
  - 焦点和无障碍需要手工桥接（`webview.cc:379,459-492`）。
- **掩盖延迟的手段，分层使用**：
  - `TopChromeWebUIConfig`：受信任的 top-chrome 页面默认共用一个进程（`top_chrome_webui_config.h:28-39`）。
  - `WebUIContentsPreloadManager`：全局只预加载一个最可能用到的页面；内存紧张时不预加载；当前页面首次绘制后（或 3s 后）再补充（`webui_contents_preload_manager.h`）。
  - “准备好再显示”：页面自己调用 `ShowUI()` 之后气泡才出现，不会出现空白气泡（`webui_bubble_dialog_view.cc:194-199`）。
  - 关闭后保活 30s，再次打开直接复用（`webui_bubble_manager.cc:20`）。
- **嵌入式界面的无响应处理不弹窗**：工具栏的 WebUI 直接结束并重启 renderer（`webui_toolbar_web_view.cc:270-286`），气泡在 renderer 结束时关闭。
- **原生 Views 界面仍是单线程的**：`ui::Compositor` 的 `LayerTreeHost` 是 `CreateSingleThreaded`，跑在 UI 线程上（`ui/compositor/compositor.cc:249`）。只有光栅化在工作线程，最终合成在 viz/GPU 进程。

**我们的决定**

1. qui 提供公开的 `qui.PageHost` + `PageView`。任何应用都可以把一棵 htmlcss/reactive 子树放到独立的页面线程上，例如 go-code 的 Markdown/HTML 预览、q-browser 的 DevTools 面板。接口与 q-browser 用的是同一套。
2. 原生控件和应用自己的界面**不**提供“放到其他线程”的选项。与 Chromium 一致，只隔离 HTML 子树；原生界面靠 RFC 0004 的光栅化工作线程和合成线程保持流畅。
3. 一并提供 Chromium 的延迟掩盖手段：
   - `PageHost` 预热池：默认 1 个，内存紧张时不预热；
   - “准备好再显示”：页面首次有内容提交前，`PageView` 显示上一帧或占位；
   - 关闭后保活：默认 30s。
4. 嵌入式面板的无响应策略可配置，默认“重启面板”而不是弹窗；q-browser 的标签页使用弹窗策略（3.6 节）。

### 8.4 对推进顺序的影响

- P2 的 PageHost 要按“一条线程、多个页面”的调度层级来设计，不能写成每个文档一个 goroutine。
- P3 的验收增加一条：react.dev 和 GitHub 上快速嵌套滚动区（代码块、侧边栏）在页面线程阻塞 2s 时仍然可以滚动。
- P5 增加 spare PageHost，以及 `window.open` 同站点共线程。
- 新增 P5b：公开 `qui.PageHost` 给普通应用，以 go-code 预览面板作为第一个使用者。

## 9. 实施记录

- **P0**（7b7bdcd）：容器查询按宽度签名缓存样式结果，宽度切换不再整棵子树重算；`QUI_DEBUG_IDLE` 记录 >50ms 的任务（`qui/longtask`）。`/pricing` 首次布局 10.1s → 0.64s。
- **P1**（53ceb85）：3.5 节的全局状态移入引擎或窗口（视口单位、rem、点击派发、`pctIndefinite`、`layoutEpoch`、DPR）。`htmlcss` 和 q-browser 语料的并发测试在 `-race` 下零报告。
- **P2**：
  - `page_host.go`：`PageThread`（一个锁定的 OS 线程驱动多个页面，即 8.1 节的调度层级）和 `PageHost`（嵌入式后端上的 `qui.Window`）。
  - `page_host_platform.go`：嵌入式后端。它放在根包而不是 `internal/platform/embedded`，因为它要直接实现引擎内部的接口并访问 `PageHost`。
  - `page_view.go`：宿主窗口里的 `PageView`，负责显示提交的帧、同步视口、转发输入和光标。
  - 宿主侧的调用都不等待页面线程。页面帧里的 panic 只停掉这一个页面（`Crashed` / `OnCrash`），同线程的其他页面照常运行。
  - 同步测试模式：`NewSyncPageThread` 加 `Pump` / `RunUntilIdle`。
  - 顺带修复的共享状态：
    - 绘制轮次 `PaintPass` 改为按窗口计数。原来是全局变量，两个窗口同时绘制时会互相覆盖。
    - 主题订阅表加锁，`ThemeGeneration` 改为原子变量。
    - 页面窗口的主题和语言变更改为以任务方式投递到页面线程。
    - 页面窗口不再接管全局剪贴板提供者。
  - 验收：
    - `htmlcss` 的 116 个 web-* 样例在 3 个页面线程上并发渲染，同时 UI 线程在步进宿主窗口。
    - q-browser 的全部冻结语料（`TestPageThreadCorpusPages`，`QB_CONCURRENT_PAGES=all`）同样在页面线程上渲染。
    - 两者在 `-race` 下都零报告，渲染结果与串行渲染一致。允许 ±2 级的抗锯齿差异：字形掩码缓存把亚像素位置分桶，并按最先请求者的精确位置光栅化，绘制顺序不同时文字边缘会差一两级，见 `qui.FramesClose`。
  - 已知未做（留给后续阶段）：
    - `SetTheme` 在页面线程绘制时直接改写 `currentTheme`（P5 前处理）。
    - 页面里的弹层窗口回退为窗口内浮层（P4）。
    - `reactive` 的 `popupPool` 仍是全局变量（P4）。
- **P3**：
  - 滚动层（`page_scroll_layer.go`、`widgets/scroll_layer.go`、`page_view_scroll.go`）：
    - 带内容缓存的 ScrollView（`PixelScroll` + `FixedViewport`，即文档滚动区）每次提交都附上它的内容瓦片。瓦片是写时复制的，显示过的不会再被改写，UI 线程可以直接持有。
    - 滚轮落在这种层上时，`PageView` 立即在 UI 线程移动瓦片，fixed/sticky 盒子和滚动条从帧里按原位置拷贝。随后用 `SetScrollLayerOffset` 把新偏移异步告诉页面。
    - 提交里的 `Applied` 表示页面已经追上；追上之前，UI 继续按自己的偏移显示。
    - 其他能滚动的滚动区作为“慢区域”提交，落在上面的滚轮交给页面处理。
  - 合成动画（`page_host_retained.go`、`internal/render/renderer_cpu_sink.go`）：
    - 页面窗口的 CPURenderer 在宿主能合成时以 PageHost 为“保留绘制接收者”，只接受可在合成线程运行的覆盖绘制（运动由时间曲线决定的动画层），并按 RetainedSet 的生存规则随帧提交。
    - `PageView` 把这些绘制平移后重新排到宿主画布上，宿主的合成线程据此继续推动 CSS 走马灯。
    - 没有曲线的动画层在页面窗口里退回 CPU 绘制（`CompositorOnly`）。
  - 顺带修复的共享状态：`animLayers` 原是全局表，改为按窗口保存（新增 `Window.LocalValue` / `SetLocalValue`）。
  - 验收：
    - `widgets` 的 `TestPageScrollLayerScrollsWhilePageBlocked`：页面线程卡住时，宿主滚轮滚动到位，页面恢复后追上同一偏移。
    - `htmlcss` 的 `TestMarqueeOnPageThreadKeepsMovingWhilePageBlocked`：页面线程卡住时，宿主合成线程 150ms 内呈现多帧，走马灯持续移动。
    - 以上测试在 `-race` 下零报告。
  - 留到后续：
    - 嵌套滚动区做成合成层（8.2 节）：需要元素背景色随层提交，并换算父层瓦片空间。
    - 页面缩放的位图预览：需要 P5 中 q-browser 的缩放路径。
    - 层里的视频保留绘制（`QueueRetainedDraw`）跨线程：需要先审计 `FrameView` 的线程安全。
- **P4**：
  - 提交里新增 `PageMeta`：页面焦点控件是否接受输入法组合，以及它的光标矩形。`PageView` 据此作为宿主窗口的 `IMEClient` / `IMEActivity` 代理：预编辑和上屏文本转发给页面，候选窗按页面光标加视图偏移定位；页面焦点不接受组合时，按键直接作为字符送达。
  - `PageView` 是文件拖放目标，OS 拖进来的文件按页面坐标交给页面窗口。
  - 光标（P2）、剪贴板（全局提供者）、提示框（页面窗口内浮层）沿用已有路径。
  - 页面里的弹层（`<select>` 下拉、菜单）在嵌入式后端上建不出 OS 弹层窗口，回退为页面窗口内的浮层。
  - `reactive` 的 `popupPool` / `popupCapable` 原是全局表，改为按所属窗口保存（`LocalValue`）。
  - 验收：`TestPageViewIMEAndFileDrop`（候选窗位置、预编辑和上屏、拖放坐标），`-race` 零报告。
  - 留到 P5：浏览器右键菜单在 shell 里同步读页面命中信息，要改成向页面异步请求。
  - 未做：页面弹层越出页面视口（Chromium 的独立弹层窗口）。
- **P5**（q-browser 接入）：
  - `browser.TabHandle` 是 shell 一侧的句柄。`browser.Tab` 本身不动，整个跑在 `PageHost` 的窗口里，每个标签页一条页面线程；按（组, 站点）共享线程留到后续。
    - shell 读到的状态（标题、URL、加载中、历史、缩放、查找结果、悬停链接）是页面线程每次变化后发布的镜像。
    - 命令一律投递，不等待。
    - 原来 shell 同步读页面的地方（`HitAt`、`SelectedText`）改到页面一侧：页面窗口的事件过滤器根据同一批鼠标事件算出悬停链接、中键链接和右键目标，再报给 UI 线程（`OnContextMenu`）。
  - 无响应检测：每秒检查一次 `PageThread.Busy`，单个任务超过 15 秒就在状态栏提示。
  - 后台标签页：`PageHost.SetHidden` 把帧率降到每秒 4 帧，任务照常运行；JS 定时器的节流沿用 `js.SetVisible`。
  - 绘制保持：导航时 PageView 一直显示上一帧，直到新文档提交，这是这个架构自然带来的。
  - `matchMedia` 改用文档自己的视口（`MediaMatchesSize`）。
  - 用 `QB_PAGE_THREADS=0` 可以回到旧模式。DevTools 只在旧模式下可用，因为它从 shell 窗口直接读 DOM；页面线程模式下面板里给出说明。
  - 顺带修复：`position: fixed` 盒子位于 transform 等祖先之下时，内容瓦片缓存原来会拒绝整个文档（go.dev 因此从来没有用上瓦片）。现在这类盒子画进瓦片，它们的移动按内容损伤处理。
  - 验收（真实窗口）：
    - `QB_DEBUG_UILAT` 测 UI 线程排队延迟。打开 github.com/pricing 时，页面线程模式下除启动那一秒外每秒 p99 ≤ 20ms、最大约 31ms；旧模式下 UI 线程多次卡住 1–3.3 秒。
    - go.dev 页面线程卡住 4 秒期间，滚轮立即滚动一屏，页面恢复后追上同一位置。
    - `TestTabHandleThreaded` 在 `-race` 下零报告。
- **P5b**：
  - `qui.PageThread`、`PageHost`、`PageView` 已经是公开 API，示例见 `examples/pagehost`：一个页面线程上的 HTML 文档，可以一键阻塞页面线程，观察窗口仍然响应、可以滚动、走马灯仍在走。
  - go-code 预览面板的迁移留给 go-code 仓库（当前有未提交的其他改动）。
