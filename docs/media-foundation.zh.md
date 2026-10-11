# media 底层：自有的纯 Go 音视频栈

状态：**验证已完成（2026-10-11）；开发进行中。** 方向已定：编解码器**由我们自己用纯 Go 实现**；
容器层基于 `go-mp4`；只有音频**输出**保留一层很薄的 cgo。

本文记录该决策背后的证据、目标架构与里程碑计划。可运行的探针在
[`tools/media-poc/`](../tools/media-poc/README.md)。

## 实现进度

| 部分 | 位置 | 状态 |
|---|---|---|
| 容器（ISO-BMFF） | `media/internal/mp4x` | ✅ 轨道、sample 时间线、sample 读取、关键帧、关键帧 seek、avcC/ASC、fMP4 探测 —— 已测 |
| H.264 **H0** 码流 | `media/internal/h264`（bits, nal, sps, pps） | ✅ 指数哥伦布、RBSP/EBSP、Annex-B 拆分、SPS/PPS（含 High profile 与 frame cropping）—— 用真实 SPS 字节测过 |
| H.264 **H1** 帧内 | `media/internal/h264`（frame, transform, slice, cavlc, intra, decoder） | ✅ **可解码真实关键帧** —— CAVLC 残差、帧内预测（**I16x16 与 I4x4，均在真实内容上与 OpenH264 逐字节一致**）、I_PCM、4:2:0 色度、slice-data 循环 —— 在 6 条预言机码流上验证。待做：I8x8 |
| H.264 **H3** 去块 | `media/internal/h264`（deblock） | ✅ 帧内去块（宏块边强滤波 + 变换块边普通滤波）、slice alpha/beta 偏移、`disable_deblocking_filter_idc` 0/1/2 —— 开启或关闭去块均**与参考逐字节一致** |
| H.264 **H4** 帧间 | `media/internal/h264`（inter, mc, decoder） | ✅ **P 片帧间预测** —— 参考列表 L0 + DPB、中值 MV 预测、1/4 亮度（6 抽头）与 1/8 色度运动补偿、`P_Skip` 与 `P_16x16/16x8/8x16/8x8` 划分、帧间残差、加权预测、帧间去块边界强度 —— 在 2 条 Baseline 多帧预言机码流以及**一帧 1280×544 High profile P 帧（亮度 0/696320，每个色度平面 0/174080）上与 OpenH264 逐字节一致**。待做：B 片、参考列表重排 |
| H.264 **H5** B 片 | `media/internal/h264`（bslice, poc） | 🚧 已实现：POC（类型 0/1/2）、DPB、L0/L1 构建、双向预测、空域 + 时域 direct、`B_16x16/16x8/8x16/8x8` 与 `B_Skip`。**预言机验证待完成** —— OpenH264 的 Main profile CAVLC B 一致性码流 POC/参考结构非标准；干净的 I/P/B 一致性码流是 High profile。该码流的 P 片**不同步已修复**：帧间 `transform_size_8x8_flag` 在 coded_block_pattern **之后**读取（而非 `sub_mb_type` 之前，与 FFmpeg 一致），所有 P 宏块现已正确解析；并新增加权预测解析。该码流的 P 帧现已**与 OpenH264 100% 逐像素一致**：修复了 2D 半像素滤波（`j`：先垂直后水平、中间为不裁剪的 16-bit）、16x8/8x16 的划分专用 MV 预测，以及帧间去块边界强度。剩余：该码流的 B 片 MB 层仍不同步（解析问题），随后进行预言机验证 |
| H.264 **H7** High profile | `media/internal/h264`（transform8x8, intra, decoder） | 🚧 已实现 I_8x8（9 种模式、8×8 反变换 + 反量化）与帧间 `transform_size_8x8_flag` —— **真实 1280×544 High profile CAVLC 码流的 I_8x8 IDR 与 P 帧均与 OpenH264 逐像素一致（0/696320）**。待做：自定义缩放矩阵；上述 B 片解析 |
| YUV → RGBA | `media/internal/yuv` | ✅ I420→RGBA（BT.601/BT.709，逐平面 stride）+ I420 GLSL 上屏着色器 —— 已测 |
| AAC-LC **A0** 码流 | `media/internal/aac` | ✅ AudioSpecificConfig（MP4 `esds`）与 ADTS 头解析、采样率表、裸 access unit 拆分 —— 已测 |

后续每个解码里程碑仍以 OpenH264 工具作为正确性预言机。

---

## 1. 现状

`media/` 目前只有 **AVFoundation 后端**，仅在 macOS 可用
（`media/backend_darwin.go`，`//go:build darwin && cgo`）。其它平台由
`media/backend_other.go` 不注册后端，所有入口返回 `ErrNotSupported`。
好消息是包结构很适合替换：整个平台栈都藏在一个内部接口
（`media/backend.go`）后面：

- `Backend` — `OpenSource(path)`、`Capabilities()`
- `Source` — `VideoTrack`、`AudioTrack`、`NextVideoFrame`、`SeekVideo`、
  `OpenScrubDecoder`、`AudioRenderer`、`Close`
- `ScrubDecoder`、`AudioRenderer`

外加 `VideoFrame`（`media/types.go`）与 A/V 同步时钟（`media/clock.go`）。
渲染路径也已存在：`media/frame_view.go` 的 `PushRGBA → uploadAndDraw`
把 CPU 帧经 GL 合成到帧缓冲。

**目标：** 新增第二个后端 `backend_golang.go`，既不依赖 ffmpeg，也不依赖
macOS，从而让同一套 `VideoView` / `AudioPlayer` 在 Linux、Windows 上也能工作。

---

## 2. 已验证结论（可运行代码，真实文件）

四个探针都在 `tools/media-poc/` 下对真实 MP4 运行，解码路径均无 cgo。

| 环节 | 方案 | 结果 | 许可 |
|---|---|---|---|
| Demux | `github.com/abema/go-mp4` | H.264 + AAC 轨道、chunk/sample 表、IDR 定位、`esds`/`avcC` 配置全部可提取 | MIT |
| 视频·全帧 | Cisco **OpenH264**，经 `github.com/y9o/go-openh264`（purego `dlopen`） | 1920×1080，7931 帧 26.6s = **298 fps（约 9× 实时）**，3.4 ms/帧；I/P/B 输出肉眼正确 | 绑定 MIT，二进制见 §3 |
| 视频·关键帧 | `github.com/Eyevinn/hi264`（纯 Go） | IDR 解码约 24 ms/帧@1080p；**只解 IDR，不能播放** | MIT |
| 音频 | `github.com/skrashevich/go-aac`（纯 Go） | 11389 个 AAC-LC 帧 → 264.45s 立体声 PCM，时长与容器完全吻合，RMS ≈ −6.7 dBFS | **LGPL-3.0** |
| 上屏 + 同步 | 现有 `frame_view.go` / `clock.go` | 就绪，无需改引擎 | — |
| 音频输出 | CoreAudio / WASAPI / ALSA | **纯 Go 做不到**；需要一层薄 cgo | — |

**要点**

1. 容器层很便宜。`go-mp4.Probe` 已组装好 sample timeline；我们只需补 chunk→偏移
   映射（`mp4util` 里几十行）。工期是**天级**，不是周级。
2. Go 里的全帧 H.264 **能跑而且很快**——但只能靠 OpenH264 二进制。纯 Go 解码器目前
   没有可用实现（`hi264` 只解关键帧；`go-264` 仍是实验品）。
3. 纯 Go 的 AAC-LC 解码是已解决的现成问题——代价是 LGPL。
4. 音频**输出**是唯一纯 Go 无法拥有的一层，必须调 OS。

---

## 3. 真正的约束是许可与专利，不是性能

**问题不是 ffmpeg，而是 H.264 与 AAC 的专利**——任何实现（包括我们自己写）都绕不开。
我们能规避的是**束缚分发方式的许可**。

**OpenH264 二进制。** Cisco 的代付 AVC/H.264 专利费**仅在全部满足**时适用：

1. 该二进制由**最终用户设备单独下载**——不能预打包进第三方软件；
2. 最终用户能**控制（启用/禁用）**它；
3. 第三方软件必须**展示** `"OpenH264 Video Codec provided by Cisco Systems, Inc."`，
   并在 EULA 中复述。

把 dylib 打进 qui 应用会失去该覆盖，专利费落到我们头上。这正是本方案把 OpenH264
当作**参考/预言机**、而非出货编解码器的原因。

**LGPL 的 AAC。** 成熟的纯 Go AAC 解码器都源自 FFmpeg/FAAD2，是 LGPL。Go 静态链接成
单一二进制，使 LGPL 的"可重链接"义务很别扭。自研可绕开。

**自研并不能消除专利。** 它消除的是**二进制依赖**与**LGPL**，并让我们完全掌控解码路径
（这对框架本身是好事）。专利是另一个独立的商业决策，我们显式跟踪。

---

## 4. 目标架构

```
media/
  backend.go            # 现有 seam；增加运行时后端选择
  backend_darwin.go     # 保留：VideoToolbox 硬件解码（可选加速）
  backend_golang.go     # 新增：跨平台纯 Go 流水线
  internal/
    mp4x/               # 容器：timeline、seek、fragmented MP4（基于 go-mp4）
    h264/               # 新增：自研 H.264 解码器
    aac/                # 新增：自研 AAC-LC 解码器
    yuv/                # planar YUV -> RGBA，以及 I420 GL 上传 shader
    audioout/           # 薄 cgo 输出：CoreAudio / WASAPI / ALSA
```

遵守的规则：

- `media/internal/*` 为内部子包，公共 API 不膨胀。
- 解码器是纯 Go，仅依赖标准库。
- `audioout` 是唯一的 cgo 孤岛，藏在小接口后面，使其余部分在 `CGO_ENABLED=0`
  下仍可构建（返回 `ErrNotSupported`）。
- `backend_golang.go` 实现现有的 `Backend`/`Source`/`AudioRenderer` 接口，
  `VideoView` 与 `AudioPlayer` 保持不变。

**需要一处引擎改动：** `media/backend.go` 的 `setBackend` 是 init 唯一注册
（backend.go:134）。要让原生与纯 Go 后端并存、可对比，选择必须改为运行时驱动
（build tag 或 `QUI_MEDIA_BACKEND=native|golang|auto`）。

---

## 5. 解码器计划

### 5.1 H.264/AVC（`internal/h264`）

按依赖排序；每个里程碑都对 OpenH264 预言机验证（同片输入、逐平面 diff 输出）。

| # | 里程碑 | 内容 |
|---|---|---|
| H0 | 码流 | NAL 解析、EBSP→RBSP、指数哥伦布、SPS/PPS、slice header |
| H1 | 帧内 | I_PCM、I16×16（全部预测模式）、I4×4（9 种）、色度、4:2:0 |
| H2 | 变换 | 反扫描、4×4 整数 IDCT、反量化、默认 scaling |
| H3 | 去块滤波 | 边界强度、亮度/色度滤波 |
| H4 | P 帧 | DPB/参考列表、MV 预测、亚像素插值、加权预测 |
| H5 | B 帧 | 双向预测、direct 模式、重排序/输出 |
| H6 | CABAC | 上下文建模——main/high profile |
| H7 | High profile | 8×8 变换、自定义 scaling 矩阵 |
| H8 | 健壮性 | 多 slice、ASO、错误隐藏 |

H0–H5 即 **Baseline/Main**——足以覆盖绝大多数网页 MP4。H6 打开 High profile
（手机默认录制）。

### 5.2 AAC-LC（`internal/aac`）

| # | 里程碑 | 内容 |
|---|---|---|
| A0 | 码流 | 裸 access unit + AudioSpecificConfig（MP4）与 ADTS |
| A1 | 频谱 | ICS、scalefactor、Huffman 码本、量化 |
| A2 | 滤波器组 | 反量化、IMDCT、加窗、重叠相加 |
| A3 | 工具 | TNS、M/S 立体声、强度立体声、PNS |
| A4 | 覆盖 | 最高到立体声、44.1/48 kHz |

LC 之外的版本/特性（HE-AAC/SBR/PS）初期明确不做。

### 5.3 性能

纯 Go H.264 是风险点。朴素的 Go 解码器达不到 1080p30。方案：

- 用 goroutine 做块行并行（I 帧与 P/B 去块在 slice/MB 行上可干净并行），吃满多核；
- 对最热的两处内核（IDCT 与亚像素运动补偿）按 profile 手写汇编；
- 解码环形缓冲与 UI 线程解耦（复用 `video_view.go` 里现有的 pump 结构）。

目标：现代笔记本上 ≤ 4 核即可 1080p30 解码并留有余量。

---

## 6. 测试策略

- **预言机 diff。** `cmd/openh264-play`（视频）与 `cmd/aac-to-wav`（音频）产出参考；
  自研解码器必须逐平面一致 / PCM 在容差内。这是主要正确性闸门。
- **提交向量。** 少量极小 MP4 + 期望帧/PCM 哈希，入库 `testdata`，测试无需网络、
  无需原生库。
- **一致性码流。** 覆盖各 profile/工具的片段做回归。

---

## 7. 里程碑与工期

每个阶段都交付可用的东西，不必等到解码器全部完成。

| 阶段 | 交付物 | 大致工期 |
|---|---|---|
| 1 | 容器层（`internal/mp4x`）+ `backend_golang.go` 骨架；**关键帧/缩略图**（hi264） | 2–3 周 |
| 2 | H.264 H0–H5（Baseline/Main）+ GL 上屏；**跨平台 H.264 播放，无 cgo** | 6–10 周 |
| 3 | AAC-LC A0–A4 + `audioout` cgo 输出 + A/V 同步；**完整播放** | 5–8 周 |
| 4 | H6–H8（CABAC/High profile）+ 性能优化 | 6–10 周 |
| 5 | 扩展：HEVC/AV1 评估、硬件解码路径 | 持续 |

一个可信、完全自有的栈约需 **6–9 个月**；可用的跨平台播放器（阶段 1–3）约在
**3–4 个月**内落地。

---

## 8. 风险

| 风险 | 严重度 | 应对 |
|---|---|---|
| H.264 CABAC 复杂度 | 高 | 放到阶段 4；先交付 Baseline/Main |
| 纯 Go 解码过于慢 | 高 | 并行 + 定向汇编；把 OpenH264 作为同接口下的回退后端 |
| 专利（H.264/AAC） | 高，非技术 | 显式商业决策；自研并不能消除 |
| fragmented MP4 / fMP4 播放 | 中 | `go-mp4` 支持 `moof/traf`；扩展 `mp4x` |
| 音频输出可移植性 | 中 | 隔离在 `audioout`；复用现有 darwin 音频代码 |

---

## 9. 跑一遍探针

```bash
cd tools/media-poc

# 纯 Go，无需原生库
go run ./cmd/keyframe -idr 5   ~/video.mp4 out.png

# 全帧证明（先取 Cisco 二进制）
curl -L -o libopenh264.dylib.bz2 \
  https://ciscobinary.openh264.org/libopenh264-2.6.0-mac-arm64.dylib.bz2
bunzip2 libopenh264.dylib.bz2
go run ./cmd/openh264-play -lib ./libopenh264.dylib -out frames ~/video.mp4

# 音频
go run ./cmd/aac-to-wav ~/video.mp4 out.wav
```

hevc: https://github.com/gen2brain/h265
av1: https://github.com/gen2brain/gav1d