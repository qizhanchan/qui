# RFC 0004：渲染流水线现代化——显示列表、工作线程光栅化、合成线程

状态：R1–R5 已实现（2026-11-20，未提交），见第 7 节
关联：RFC 0003（WebGPU 呈现）、`widgets/anim_layer*.go`、`internal/render`

## 1. 问题

qui 目前是“单线程即时光栅化”：每个 `Window.Step` 在主线程上遍历组件树，直接把像素画进 CPU 帧，再交给 GPU 呈现。性能工作已经把常见情况压得很低（react.dev 静置约 0.03 core、走马灯约 0.12 core），但剩下的问题都源于这一点，不是再做局部优化能解决的：

| 现象（react.dev，2026-11-19/20 实测） | 根因 |
|---|---|
| 走马灯每隔几秒卡一下（一次 60–230ms） | 新卡片进入预光栅化范围时，要在主线程上光栅化：每张 20–30ms，一次 3–15 张 |
| 任何主线程工作超过约 10ms（JS、样式、布局、光栅化）都会掉帧 | 动画、滚动、合成都在同一个线程上，没有和主线程解耦 |
| 首次滚动、缩放、悬停时的大面积重绘慢 | 光栅化只能单核、同步完成，没有分块，也没有提前光栅化 |
| 被遮挡、分层、视频等情况要在 `widgets` 里写特例 | 没有“层树”这一层抽象，合成规则散落在 ScrollView 缓存、动画层、retained draw 里 |

Chromium 的同类问题由 cc（Chrome Compositor）的分层架构解决。qui 的长期目标是现代、高性能的 UI 框架，所以按这套架构演进，而不是继续在单线程模型上打补丁。

## 2. 参照：Chromium 的渲染流水线

| 阶段 | Chromium | 线程 |
|---|---|---|
| 样式、布局、绘制**录制** | Blink 产出 `PaintRecord` / `DisplayItemList`（绘制操作列表，不含像素） | 主线程 |
| 提交 | `LayerTreeHost` → `LayerTreeHostImpl`（commit 后两边各持一份层树） | 主线程 → 合成线程 |
| 分块与光栅化 | `TileManager` 把层切成瓦片，按优先级（可见 > 即将可见）排队；`RasterWorkerPool` 在多个工作线程上回放显示列表得到像素；`ImageDecodeCache` 按目标尺寸解码图片 | 工作线程 |
| 合成与动画 | 合成线程按 BeginFrame（vsync 时间戳）推进 transform/opacity 动画和滚动，生成合成帧；主线程忙也不影响 | 合成线程 |
| 呈现 | Viz / GPU 进程 | GPU |

几个关键性质：
- **录制和光栅化分离**：主线程只产出不可变的显示列表，所以工作线程可以安全地并行回放，不碰组件树。
- **瓦片和优先级**：光栅化按需、提前进行，滚动和动画进入新区域时像素多半已经备好。
- **合成线程独立**：主线程卡住时动画和滚动仍然顺滑（“impl-side”动画、滚动）。

## 3. qui 的目标架构

```
主线程（UI）                         工作线程池                 合成（独立 goroutine，锁 OS 线程）
────────────                         ──────────                 ───────────────────────────────
事件 / JS / 样式 / 布局
绘制 → 录制 Picture（显示列表）  ──▶  按瓦片回放 Picture       层树快照 + 瓦片纹理
构建层树（内容层、动画层、             生成像素 → 上传纹理  ──▶  BeginFrame：推进动画/滚动偏移
fixed/sticky、视频、GPU 内容）                                   合成 → WebGPU 呈现
提交（commit）层树快照         ─────────────────────────────▶
```

### 3.1 显示列表 `Picture`（R1）
- `gfx.PictureRecorder` 实现完整的 `Canvas` 契约**以及所有可选能力接口**（`ClipAware`、变换绘图、着色器、`SaveLayer`、路径裁剪、字形串、`BackdropFilter`……），把调用记成不可变的操作列表；`Picture.Playback(Canvas)` 回放。
- 操作引用的数据必须是快照或不可变的（路径、字形串、图片）；可变对象在录制时拷贝。
- 验收：把现有渲染一致性测试（`render_conformance_test`、htmlcss 快照、Chrome 金标准）都改为“录制 → 回放”再跑一遍，像素逐字节一致；录制本身的开销不超过直接光栅化的 15%。

### 3.2 工作线程光栅化（R2）
- 动画层的子元素位图先迁移到这条路：主线程录制 Picture，工作线程回放成位图；位图没好之前，回放时沿用旧位图或跳过（与 Chromium 的“棋盘格”策略不同，我们先选“沿用 + 提前量”）。
- 前置：`internal/typeset` 的字形缓存、`render` 的 pix 池、图片解码缓存都要能并发访问（逐个审计并加锁或改为每线程缓存）。
- 验收：react.dev 走马灯 60 秒内不再出现超过 20ms 的帧间隔（`QUI_DEBUG_GPU_LAYERS` 统计），主线程没有光栅化耗时。

### 3.3 分块的内容层（R3）
- ScrollView 的内容缓存改为瓦片（例如 256×256 设备像素），从 Picture 在工作线程光栅化；视口外按滚动方向提前光栅化。
- 内容变化时只让相交的瓦片失效、重新录制对应区域。
- 验收：react.dev、go.dev 首屏后快速滚动，p95 帧间隔 ≤ 17ms；主线程每帧不做光栅化。

### 3.4 层树与合成线程（R4）
- 把现在分散的合成规则（ScrollView 缓存、动画层、fixed/sticky、retained draw、视频）统一为显式的层树：每层有 transform、clip、opacity、z 顺序和内容（瓦片 / 纹理 / GPU 绘制）。遮挡关系由层的 z 顺序天然表达，不再需要“遮挡物”特例。
- 合成在独立 goroutine（锁 OS 线程）上运行，按 vsync 推进 transform/opacity 动画和滚动偏移；主线程只在提交时交换层树快照。
- 验收：在 JS 里人为制造 100ms 的主线程阻塞，走马灯和滚动仍然 60fps。

### 3.5 GPU 光栅化（R5，长期）
- 在 WebGPU 上实现路径填充、渐变、文字（SDF 或字形图集）等，作为 Picture 的第二个回放后端；CPU 光栅化保留为参考实现和回退路径。

## 4. 已经完成、并沿用到目标架构的部分

| 已有 | 在目标架构里的位置 |
|---|---|
| GPU 合成动画层（`widgets/anim_layer_gpu.go`）、2D 帧之上的 retained 合成绘制 | R4 层树的雏形，合成规则会迁移进层树 |
| 帧时钟（`window_frameclock.go`，动画时间按刷新周期推进） | R4 的 BeginFrame 时间 |
| 按绘制尺寸缓存解码图片（`internal/render/image_scale_cache.go`） | R2 的 ImageDecodeCache，需改为并发安全 |
| 在预算内保留层的子元素位图 | R3 的瓦片保留策略 |
| `drawOverRGBA` 快速合成、空闲调度、定时器对齐 | 继续有效 |

## 5. 风险

- **Canvas 能力面大**：可选接口多，漏录一个就会在某些页面上画错。用“录制 → 回放”跑全部现有测试兜底，并在 `QUI_DEBUG_DRAW` 下比对。
- **线程安全**：字形缓存、字体回退、图片解码、`pix` 池目前默认在主线程上用。R2 之前必须先审计。
- **内存**：显示列表 + 瓦片 + 纹理会增加占用，需要统一的预算和淘汰策略（参照 Chromium 的 tile memory budget）。
- **调试复杂度**：多线程后问题更难复现。每阶段都要带上可观测性（每帧阶段耗时、瓦片命中率、光栅化队列长度）。

## 6. 推进顺序

R1 → R2 → R3 → R4 → R5，每阶段单独提交并在 react.dev / go.dev 上用 `tools/probe/anim-smooth.sh` 和滚动基准验收。R1 是后续所有阶段的地基，建议先做。

## 7. 实现记录（2026-11-20）

| 阶段 | 代码 | 关键点 | 实测（react.dev，1000×672 窗口） |
|---|---|---|---|
| R1 显示列表 | `internal/render/picture.go`、`picture_record.go` | 在 RasterBackend 层录制（物理像素），前端能力全部自动覆盖；ClipPath 遮罩延迟到回放时生成；`QUI_RECORD_FRAMES=1` 让所有窗口帧走“录制→回放” | 录制开销约为光栅化的 8%；整幅回放逐字节一致；全量测试在录制模式下无新增失败；单张卡片录制 3.15ms→15µs |
| R2 工作线程光栅化 | `internal/render/raster_pool.go`、`widgets/anim_layer.go` | 优先级队列，`Wait` 可窃取未开始的任务；可见且无位图的子元素并行光栅化后等待，其余完成后补进 GPU 纹理槽位，只触发 GPU 重绘；`<canvas>` 位图登记为易变（`gfx.MarkVolatile`），录制时拷贝 | 层捕获帧最大间隔 17.1ms（原 60–230ms） |
| R3 分块内容层 | `widgets/scroll_cache.go` | 256px 瓦片，内容设备坐标（与滚动无关），亚像素相位作键；视口上下各一屏预光栅化；小损坏原地修补、大损坏重光栅化；滤镜附近加 128px 边距；滚动露出条带改为“仅合成”损坏；另加模糊阴影遮罩缓存、帧驱动的 rAF（`Window.RequestAnimationFrame`） | 20px/帧滚动稳定 ~60fps（之前 rAF 只有 31Hz）；最慢帧 160–200ms→≤40ms |
| R4 合成线程 | `compositor_thread.go`、`renderer_webgpu.go`、`widgets/anim_layer_gpu.go`、`htmlcss/anim.go` | 层集合发布不可变提交快照；htmlcss 给出纯函数动画曲线（与主线程同一套插值）；合成线程按自己的帧时钟逐 vsync 呈现，主线程只在内容变化时出帧；Dawn 开启 ImplicitDeviceSynchronization；渲染器锁：UI 线程持有 Begin…End | JS 每 500ms 阻塞 100ms 时走马灯仍 60–62 帧/秒，p90 ≈ 17ms |
| R5 GPU 光栅化 | `internal/render/gpu_raster.go` | 实心/圆角矩形（SDF）、图片（含旋转）、文字覆盖遮罩、图层+路径遮罩在 GPU 上；路径/阴影/线条等逐操作 CPU 画成小纹理；backdrop/图层滤镜/非 SrcOver 整张回退 CPU；图片准备（转格式、缩放）在后台，未就绪时本次回退工作线程 | 与 CPU 结果仅抗锯齿边缘不同；悬停缩放过渡的逐帧重光栅化不再占 CPU |

开关：`QUI_RECORD_FRAMES=1`、`QUI_RASTER_THREADS=N`、`QUI_CONTENT_TILES=0`、`QUI_COMPOSITOR_THREAD=0`、`QUI_GPU_RASTER=0`、`QUI_SHADOW_CACHE=0`；诊断：`QUI_DEBUG_TILES=1`、`QUI_DEBUG_GPU_LAYERS=1`、`QUI_DEBUG_IDLE=1`（含帧间隔分位数、合成线程帧数、job 来源）。

尚未做 / 与原计划的差异：
- 滚动仍在 UI 线程：输入事件和 JS 都在 UI 线程上，合成线程无法独立滚动；内容瓦片仍合成进 CPU 2D 帧（未作为 GPU 层）。
- 合成线程只驱动“纯曲线”的变换动画层，且层上没有遮挡物；有遮挡物、opacity 动画、过渡叠加时仍由 UI 线程驱动。
- GPU 光栅化目前只用于 GPU 合成层的子元素位图，内容瓦片仍是 CPU 工作线程光栅化。

Review（2026-11-20，一轮）后修正：曲线被清除到重新捕获之间，合成线程只用发布时冻结的偏移，不再读组件状态；视口外（含窗口外、未绘制帧）的内容损坏会转交给下一帧并使对应瓦片失效；瓦片原地修补改为写时复制；渲染器所有 UI 线程 setter 加锁，合成线程在渲染器损坏或有待绘制的像素滚动时不出帧；UI 线程在出帧时合成线程让出；易变图片（`<canvas>` 及每次录制的拷贝）不进图片缓存；GPU 图片缓存在提交后才淘汰；动画元素被移除时释放其子位图和纹理；rAF 按墙钟周期限频并等到下一周期（原先被 job 唤醒的中间 step 抢占帧时间，滚动只有 ~43fps）。

遗留：合成线程与 UI 线程各有一个帧时钟（交替出帧时可能有不到一个周期的偏差）；目前 WebGPU present 不一定按 vsync 阻塞，滚动时一帧内可能画两次（页面滚动事件引起的重绘）；内容瓦片仍走 CPU 光栅化并合成进 2D 帧。
