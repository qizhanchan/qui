# media 底层：自有的纯 Go 音视频栈

状态：**阶段 1–4 已落地（2026-10-11）。** 编解码器**由我们自己用纯 Go 实现**；
容器层基于 `go-mp4`；只有音频**输出**保留一层很薄的 cgo。H.264 + AAC-LC 的 MP4
已能在纯 Go 后端上端到端播放（视频、音频、音画同步、seek、拖动预览），不依赖
ffmpeg，也不依赖平台编解码框架：

```bash
QUI_MEDIA_BACKEND=golang go run ./examples/media-video ~/video.mp4
```

本文记录该决策背后的证据、已建成的架构以及剩余事项。最初的探针在
[`tools/media-poc/`](../tools/media-poc/README.md)。

## 实现进度

| 部分 | 位置 | 状态 |
|---|---|---|
| 容器（ISO-BMFF） | `media/internal/mp4x` | ✅ 渐进式与分片（`moof/traf/trun`）MP4 共用一张 sample 表。逐 sample 的 DTS、**合成偏移（`ctts`）**与 **edit list 偏移**，因此 `PTS` 即呈现时间；`stss` 同步表，缺失时回退为扫描 IDR；按 PTS 的 `SeekKeyframe` / `SampleAtTime`（二分查找）；逐轨道的 `avcC` 与 `esds`（AudioSpecificConfig） |
| H.264 码流（H0） | `media/internal/h264`（`bits`、`nal`、`params`、`slice`） | ✅ Annex-B 与长度前缀 NAL、avcC、SPS（含 VUI：SAR、色彩描述、时序、`max_num_reorder_frames`）/ PPS、完整 slice 头（参考列表修改、加权表、MMCO） |
| 熵编码（H1/H6） | `cavlc.go`、`cabac.go` | ✅ 全部语法元素的 **CAVLC 与 CABAC**；CABAC 算术引擎与规范逐位一致（I_PCM 重新对齐可用） |
| 帧内 + 变换（H1/H2/H7） | `intra.go`、`transform.go`、`recon.go` | ✅ I_PCM、I16×16、I4×4、**I8×8**（含参考样本滤波）、4×4 与 **8×8 变换**、**自定义缩放矩阵**（SPS + PPS 列表、回退规则 A 与 B、flat/默认列表） |
| 帧间（H4/H5） | `mvpred.go`、`mc.go`、`recon.go` | ✅ P 与 **B** 片：所有划分与子划分、`P_Skip`、`B_Skip`/`B_Direct`（**空域与时域 direct**、`direct_8x8_inference`）、中值 MV 预测、带边缘扩展的 1/4 亮度 / 1/8 色度运动补偿、**显式与隐式加权预测** |
| DPB + 输出 | `picture.go`、`decoder.go` | ✅ POC 类型 0/1/2、滑动窗口与 MMCO 1–6、`frame_num` 间隙填充、参考列表初始化 + 修改、按 VUI 重排深度（否则 `MaxDpbFrames`）以显示顺序输出、输出帧池（`h264.Recycle`） |
| 去块（H3） | `deblock.go` | ✅ 由帧内/系数/运动推导 bS（比较参考图像本身而非索引）、alpha/beta/tC0、`disable_deblocking_filter_idc` 0/1/2、色度 QP 偏移 |
| 健壮性（H8） | `decoder.go`、`deblock.go` | ✅ 多 slice 图像、任意 slice 顺序（`TestArbitrarySliceOrder` 将每条 3-slice 图像内的 slice 逆序，验证平面逐位相同）、对未被任何 slice 覆盖的宏块做隐藏（从最近参考帧复制，否则填灰）、缺失参考回退、slice 解码外层 `recover()` —— 损坏码流不会 panic 或卡死（`robust_test.go`）。当 `ref_pic_list_modification` 指向 DPB 中不存在的图像时（参考帧丢失，或无 IDR 的 `frame_num` 重置），改为**隐藏**——保留该位置原有的参考——而不是中止整个解码（计入 `concealedRefs`）；OpenH264 的错误隐藏测试集正需要此行为 |
| AAC-LC（A0–A4） | `media/internal/aac` | ✅ ASC/ADTS、ICS + section/比例因子/频谱 Huffman（基于 ISO 14496-3 码本表的两级查找表）、pulse data、**TNS**、**M/S**、**强度立体声**、**PNS**、反量化、IMDCT（基于 N/4 点 FFT 的 DCT-IV）、正弦 + KBD 窗、全部四种窗序列、重叠相加；单声道 / 立体声，全部 LC 采样率。TNS 由两条入库向量覆盖（`lc_44k_apple`、`lc_44k_is_ms`）；PNS 按**能量**验证（`TestPNSEnegry`）——其样本值属实现细节 |
| 音频输出 | `media/internal/audioout` | ✅ 单一 `Sink` 接口、三个后端：**AudioQueue**（macOS）、**ALSA**（Linux，经 `dlopen` 加载，无需 ALSA 头文件）、**WASAPI**（Windows，纯 COM syscall，不用 cgo）。有界非阻塞 `Write`、`Buffered`、`Flush`、`SetRate`（macOS 保持音高；Linux/Windows 为重采样，音高随速率变化），以及经分段环把设备 sample 时间映射成的**内容时钟**，`HostTime` 即为正在听到的位置。无 cgo 构建（或缺少运行时库的机器）返回 `ErrNotSupported` |
| YUV → RGB | `media/internal/yuv`、`media/gl_yuv.go` | ✅ CPU I420→RGBA（BT.601/709，full/limited range）以及 **GPU 路径**：三张 R8 平面 + 矩阵着色器（BT.601/709/2020，full/limited）直接绘入 VideoView 帧缓冲 |
| 纯 Go 后端 | `media/backend_golang.go`、`backend_golang_audio.go`、`video_frame_golang.go` | ✅ `Backend` / `Source` / `ScrubDecoder` / `AudioRenderer`：按呈现时间输出视频帧（显示顺序，seek 后丢弃早于目标的帧）、精确与关键帧 seek、独立的拖动预览解码器（在同一 GOP 内向前续解）、AAC 渲染器（seek 时预滚 + 裁剪）、音频播完后的时钟外推、`OnEnded` |
| 运行时后端选择 | `media/backend.go` | ✅ `QUI_MEDIA_BACKEND=native\|golang\|auto`（默认 `auto`：有 native 用 native，否则 `golang`） |

## 验证

- **H.264 与 FFmpeg 逐像素一致**（按显示顺序比对每一帧的每个平面），覆盖：19 种 x264
  配置（Baseline/Main/High、CAVLC 与 CABAC、B 金字塔、1–16 个参考帧、显式与隐式加权
  预测、时域与空域 direct、8×8 变换、自定义矩阵、多 slice、奇数尺寸与裁剪）；5 条
  VideoToolbox（`h264_videotoolbox`）码流；一段 1080p High profile 片段；以及沿用的
  OpenH264 向量。
- **OpenH264 一致性码流集（独立复核）：** OpenH264 一致性集里的 49 条码流中，**46 条与
  FFmpeg 逐像素一致**。不一致的 3 条都是刻意构造的**错误隐藏**码流
  （`BA_MW_D_P_LOST`、`Error_I_P`、`MR2_TANDBERG_E`）：现在都能解出与 FFmpeg 相同的帧数
  （99 / 6 / 300）且不再致命报错，但被隐藏的像素不同——隐藏策略属实现细节，本就不期望
  完全一致。`MR2_TANDBERG_E` 正是发现上面 H8 缺口的那条（此前在第 55 个 NAL 处失败）。
- **I+P 与 OpenH264 预言机：** 对一条 Main profile 的 I+P 码流，每一帧的每个平面都与
  OpenH264 2.4.1（`oh264-hashes`）逐字节一致，而不只是与 FFmpeg 一致。在含 B 帧的码流上，
  OpenH264 2.4.1 自身在 B 图上就与 FFmpeg（以及我们）不一致——FFmpeg 与本解码器一致，
  OpenH264 是异类——因此 B 帧的实际预言机取 FFmpeg。
- **AAC-LC 与 FFmpeg 误差在 ±1 LSB 内**（SNR 约 137 dB），包括 FFmpeg 与 Apple 编码器
  产出的码流，含强度立体声与 M/S。
- **后端：** e2e 测试解码一份提交入库的 B 帧 + AAC MP4，帧哈希与 PCM（从头及 seek 后）
  均与 FFmpeg 一致。GUI 播放用 `qui-agent` 在 1080p 文件上检查过：实时时钟、seek、
  重播、音画同步。

提交入库的向量（离线、无需原生库）：

| 路径 | 内容 |
|---|---|
| `media/internal/h264/testdata/vectors.txt` | `cabac_high_bpyr`、`cavlc_main_b`、`cabac_temporal_wb`、`cabac_cqm`、`cavlc_slices`、`crop_offedge`、`vt_main_p8x8` 的名称、帧数与解码帧 SHA-256 |
| `media/internal/h264/testdata/*` | 以 OpenH264 为预言机的 Baseline P / 帧内 / POC 类型 2 向量，外加 `conceal_missing_ref.264`（截取的错误隐藏码流，其 `ref_pic_list_modification` 指向 DPB 中缺失的图像——用于固定 H8 隐藏路径） |
| `media/internal/aac/testdata/*.aac` + `.s16` | 五条 LC 码流（22.05/44.1/48 kHz、单/双声道、Apple 编码器、IS + M/S）及 FFmpeg PCM，均在 ±1 LSB 内 |
| `media/internal/aac/testdata/pns/*` | 一条重度使用 PNS 的 LC 码流（111 个噪声频带 / 23 帧）及 FFmpeg PCM；按能量检查（`TestPNSEnegry`），因为 PNS 样本值属实现细节 |
| `media/testdata/bframes_aac_160x90.mp4` + `.ref.s16` | 后端 e2e 片段 |

与 FFmpeg 的语料比对（需手动开启，不在 `go test ./...` 中）：

```bash
AVC_CORPUS=/path/to/dir go test -run TestCorpus ./media/internal/h264   # *.264 + ffmpeg
AVC_BIG=/path/to/1080p.264 go test -run TestBigFile ./media/internal/h264
AAC_CORPUS=/path/to/dir go test -run TestADTSCorpus ./media/internal/aac   # *.aac + *.f32
```

## 性能

解码一段 1080p30 High profile CABAC 纯 P 帧录屏，在 Apple Silicon 性能核上每帧约耗
**5.7 ms CPU**（单线程约 175 fps；最初的纯 Go 解码器约 107 fps，即 **CPU 降低 39%**）。
去块流水化把这些开销分摊到两个核上。在完整播放器上实测
（`examples/media-video`，`QUI_MEDIA_BACKEND=golang`，统计 20 秒 CPU 时间）：

| 播放 | CPU |
|---|---|
| 1080p30，1× | **约 0.44 核**（原约 0.6 核；`-tags purego` 约 0.49 核） |
| 1080p30，2× | **约 0.63 核**（原约 0.8 核；`-tags purego` 约 0.79 核），分摊在多个线程上 —— 没有线程接近占满 |

已落地的优化：

| 改动 | 位置 | 效果 |
|---|---|---|
| 保留式 GL 绘制 | `video_view.go` | VideoView 注册 `qui.RetainedGLCanvas` 绘制，仅在提升新帧时请求 `Window.RequestGLRedraw`，并在下一帧到期时再 Tick。此前它每个 Tick 都把自己标脏，窗口即使暂停也按显示器刷新率重新光栅化、上传并交换缓冲 |
| 去块流水化 | `pipeline.go` | 第 *n* 帧在独立 goroutine 上去块，同时解码第 *n+1* 帧；运动补偿只等待它读取的参考行（逐帧行进度），输出在去块完成后再拷贝，解码器对 `picCtx` 双缓冲。解码速度 +34%，CPU 不变；`TestPipelinedDeblockMatchesInPlace` 固定其与原地去块结果一致 |
| 预解码环 | `backend_golang.go`（`decodeAhead`） | 解码器在独立 goroutine 上领先约 4 帧，解码与显示重叠 |
| NEON 内核 | `neon_arm64.s`（由 `neon_gen.go` 生成） | 亮度 6 抽头水平 / 垂直 / 中心与色度双线性运动补偿（亮度 16/8 宽、色度 8/4 宽）、亮度环路滤波（普通与 bS 4，垂直边在寄存器内转置）、色度环路滤波（Cb 与 Cr 一次 16 通道处理，TBL 收集/散射）、4×4 IDCT。Go 汇编器缺少大部分 NEON 运算指令，生成器用 clang 汇编标准 arm64 语法并输出带注释的 `WORD`。每个内核都有随机化测试（`neon_test.go`）固定与 Go 参考实现一致；`-tags purego` 或非 arm64 构建使用 Go 代码 |
| `avg2` 汇编 | `avg_arm64.s`、`avg_amd64.s` | 1/4 像素与双向预测平均使用 NEON `URHADD` / SSE2 `PAVGB`；保留标量 `purego` 回退，`TestAvg2MatchesScalar` 固定两者一致 |
| CABAC 引擎 | `cabac.go` | 用 64 位比特缓存取代每次重归一化的 `peek32`；合并为 256 项状态表（无边界检查、无 MPS 翻转分支） |
| 单列表直接运动补偿 | `recon.go` | 无加权的单列表块直接预测到图像中（不再经临时缓冲拷贝） |
| YUV 四边形 | `gl_yuv.go` | 顶点缓冲只在四边形变化时重写（每帧重写会让 GL 驱动同步等待） |
| 按行切片的 6 抽头滤波 | `mc.go` | `lumaV` / `lumaHV` 去掉逐抽头边界检查（内核快约 17–25%） |
| 感知划分的 bS | `deblock.go`（`samePartition`） | 位于同一运动划分内部的边跳过运动比较 |
| 输出帧池 | `picture.go`（`Recycle`） | 1080p 每帧不再分配 3 MB |

**波前（块行）并行去块试过后已移除：** 结果逐位一致，但以约 57% 的额外总 CPU
（600 帧 9.1 s 对 5.8 s）只换来约 14% 的解码速度 —— 开销在调度器的自旋与
goroutine 交接上。对播放器而言，每帧 CPU 比峰值速度更重要。

剩余热点：CABAC `decision`（约占解码 19%，本质上是串行的）、bS 推导与运动信息记录
（`deblockMB`、`writeBackMotion`）、残差解析。播放器中最大的非解码开销是帧呈现
（`glfwSwapBuffers`）。去掉边界检查的标量环路滤波和三缓冲 YUV 纹理都实测无收益，已放弃。

## 已知限制

- **H.264：** 仅支持逐行帧 —— 隔行（PAFF 场、MBAFF）码流返回 `ErrUnsupported`；仅
  4:2:0 8 bit（不支持 4:2:2 / 4:4:4 / High 10）；不支持 SP/SI slice；不支持 FMO slice group。
- **AAC：** 仅 LC。HE-AAC（SBR/PS）码流只解其 LC 核心（半采样率）；不解码 CCE / LFE /
  两个以上声道以及 960 样本帧。**存在一处已知偏差，并已用入库向量复现：** 一条 32 kHz
  立体声码流中，某个短窗（`eightShort`）帧的左声道解码有误（`[448,960)` 内 512 个样本中
  有 505 个与 FFmpeg 不一致，最大 549）。复现码流与定位见
  `media/internal/aac/testdata/known_bug/` 与 `short_window_test.go`（未修复前 Skip）；
  该码流被排除在 ±1 LSB 向量集之外。
- **音频输出** 已覆盖 macOS（AudioQueue）、Linux（ALSA，经 `dlopen` 加载，无需 ALSA
  头文件）与 Windows（WASAPI，纯 syscall，不用 cgo）。缺少运行时库的机器返回
  `ErrNotSupported`。Linux 输出为 cgo（`-ldl`），Windows 输出不是。
- **cgo：** 纯 Go 后端通过 `go-gl` 上传，故为 `//go:build cgo`。解码器本身可在
  `CGO_ENABLED=0` 下构建。

---

## 路线图：后续编解码器

H.264 + AAC 播放栈已完成。后续工作沿用同一套做法 —— 自研纯 Go 编解码器、按 profile
需要用 NEON 加速、以 FFmpeg 为正确性预言机 —— 扩展到编码和更新的格式。按顺序：

### R1. H.264 编码器（`media/internal/h264enc`）

目标：把 I420 帧编码为 H.264 基本流和任何播放器都能解码的 MP4；在单个性能核上以
“fast” 预设实时编码 1080p30。

| # | 里程碑 | 内容 |
|---|---|---|
| E0 | 码流 | 比特写入、指数哥伦布、防竞争字节、SPS/PPS/slice 头、Annex-B 与长度前缀输出 |
| E1 | 帧内 | I16x16 / I4x4 模式决策（SAD → SATD）、4×4 整数正变换 + 量化（flat 矩阵）、CAVLC 残差编码、复用解码器预测 / IDCT / 去块的重建环 |
| E2 | P 帧 | 整像素运动搜索（菱形 / 六边形，预测候选）、用解码器 MC 内核做 1/4 像素细化、P_Skip / 16x16 / 16x8 / 8x16 / 8x8 决策、单参考帧 |
| E3 | 码率控制 | 先恒定 QP，再做带 VBV 模型的 ABR / 类 CRF；关键帧间隔；`max_num_reorder_frames = 0` |
| E4 | CABAC + High | CABAC 编码引擎与上下文建模、8×8 变换、B 帧（稍后）、多参考帧 |
| E5 | 容器 | `mp4x` 写入：`avcC`、`stts`/`stsz`/`stco`/`stss`/`ctts`，流式用的分片输出；AAC 编码器另列、稍后 |
| E6 | 性能 | NEON SAD/SATD、正向 DCT、量化；slice 或帧级并行编码 |

验证：

- **自洽：** 编码器的重建结果必须与我们的解码器和 FFmpeg 解码其码流的结果逐字节相同
  （不一致就是编码器 bug，不存在“差不多”）。
- **一致性：** `ffmpeg -v error -i out.264 -f null -` 不报任何错误；提交的向量固定码流哈希。
- **质量：** 在现有测试素材（`export 1080.mp4` 与已提交片段）上，与同码率的
  `libx264 -preset ultrafast/veryfast` 比较 PSNR / SSIM；CABAC 之前目标与 `ultrafast` 相差
  约 1 dB 以内，E4 之后与 `veryfast` 相差约 0.5 dB 以内。

**状态：E0–E6 已完成（2026-10-11）。** 编码器通过 `h264/enckit.go` 复用解码器的帧内预测、
IDCT、反量化、运动补偿、CAVLC/CABAC 表与去块，重建即解码。已实现：码流与参数集（E0）、
I16×16/I4×4 帧内（E1）、P 帧全部分割 + 六边形/亚像素搜索 + 多参考（E2）、CQP/CRF/ABR/VBV
码率控制（E3）、CABAC + High 8×8 + B 帧空间直接模式（E4）、渐进式与分片 MP4 写入（E5）、
NEON SAD/SATD/变换量化 + 半像素平面 + 早退跳过探测 + 条带并行（E6）。

证据：22 种配置在两个片段上与我们的解码器及 FFmpeg 逐字节一致；MP4 往返一致；
`testdata/streams.txt` 固定码流哈希（NEON 与 purego 一致）。同码率 BD-PSNR：fast 预设比
`x264 ultrafast` 高 +4.1 dB（360p）/ +3.2 dB（1080p）；High+CABAC+B 比
`x264 veryfast -tune psnr` 高 +0.27 / +0.12 dB。1080p 单核：fast 12.0 ms/帧（约 83 fps），
medium 18.8 ms，High+CABAC+B 22.5 ms；4 条带约快一倍。ABR 误差 ±6% 以内，VBV 不下溢。

待办：AQ/psy、B_8x8 与 B 的 16×8/8×16 分割、加权预测、帧级并行、AAC 编码器。

### R2. H.265 / HEVC 解码器，然后编码器（`media/internal/hevc`）

解码器范围：Main profile（8 bit 4:2:0）、逐行；随后做 Main 10。

| # | 里程碑 | 内容 |
|---|---|---|
| V0 | 码流 | NAL 头（2 字节）、VPS/SPS/PPS（含 VUI）、短期 / 长期 RPS、slice segment 头；`mp4x` 支持 `hvcC` |
| V1 | 熵编码 | HEVC CABAC（新的上下文表、大量旁路二值化）、编码四叉树（CTU 16–64）、变换树 |
| V2 | 帧内 | 35 种模式（planar、DC、33 种角度）含参考平滑、4×4 DST、4–32 反 DCT、缩放列表、transform skip |
| V3 | 帧间 | merge（空域 / 时域 / 组合双向 / 零候选）、AMVP、亮度 8 抽头 / 色度 4 抽头 MC、加权预测、TMVP |
| V4 | 环内滤波 | 去块（8×8 网格）与 SAO（band / edge） |
| V5 | DPB | 基于 RPS 的参考标记、POC、按 `sps_max_num_reorder_pics` 输出；CRA / RASL / BLA 随机访问 |
| V6 | 并行工具 | tiles 与 WPP 入口点（解析；WPP 可用行并行解码）、dependent slice |
| V7 | 性能 | NEON 8 抽头 MC、变换、SAO / 去块；与 H.264 相同的图像级流水化 |

测试向量（全部从 `export 1080.mp4` 与已提交片段本地生成；输出与 FFmpeg `hevc` 解码器逐平面比对）：

```bash
ffmpeg -i "export 1080.mp4" -t 10 -c:v libx265 -preset medium -x265-params keyint=60 -an hevc_main.mp4
ffmpeg -i "export 1080.mp4" -t 10 -c:v libx265 -x265-params "bframes=4:ref=4:weightb=1:sao=1" -an hevc_b.mp4
ffmpeg -i "export 1080.mp4" -t 10 -c:v libx265 -x265-params "wpp=1:ctu=32" -an hevc_wpp.mp4
ffmpeg -i "export 1080.mp4" -t 2 -c:v libx265 -x265-params lossless=1 -an hevc_lossless.mp4
ffmpeg -i "export 1080.mp4" -t 10 -c:v libx265 -x265-params scaling-list=default -an hevc_sl.mp4
ffmpeg -i "export 1080.mp4" -t 10 -c:v hevc_videotoolbox -b:v 4M -an hevc_vt.mp4   # Apple 编码器
ffmpeg -i in.mp4 -c:v libx265 -pix_fmt yuv420p10le ...                            # Main 10，稍后
```

与 H.264 一样，小片段（≤ 160×90，数帧）连同帧哈希放入 `testdata`；1080p 片段保留为需手动开启的语料比对。

**解码器状态：V0–V7 已完成（2026-10-11）。** tiles 与 dependent slice 已实现，但手头的编码器
（x265、VideoToolbox）都不产生这类码流；下面的 HEVC 编码器会产生它们，并以测试向量固定。

| 部分 | 位置 | 状态 |
|---|---|---|
| 码流（V0） | `bits.go`、`params.go`、`slice.go`、`scaling.go`；`mp4x` | VPS/SPS/PPS（含 VUI）、带帧间 RPS 预测的短期 RPS、长期参考、slice segment 头、缩放列表；`mp4x` 读写 `hvc1`/`hev1` + `hvcC`，纯 Go 后端可播放 |
| 熵编码（V1） | `cabac.go`、`ctu.go`、`residual.go` | 使用 HEVC 初始化表的 CABAC、编码四叉树（CTU 16–64，图像边缘隐式划分）、变换树、含符号隐藏与 Rice 自适应的残差编码、cu_qp_delta 量化组、PCM |
| 帧内（V2） | `intra.go`、`transform.go` | 35 种模式（参考替换 / 平滑 / 强平滑与边界滤波）、4×4 DST、只累加非零输入的 4–32 反 DCT、transform skip、无损（transquant bypass）、受限帧内 |
| 帧间（V3） | `inter.go`、`mc.go` | merge（空域、时域、组合双向、零）、带缩放的 AMVP、TMVP、8 / 4 抽头插值、显式加权预测、AMP |
| 环内滤波（V4） | `deblock.go`、`sao.go` | 含 slice / tile 边界规则的去块、按 CTB 判定邻居可用性的 SAO band / edge |
| DPB（V5） | `decoder.go` | POC、RPS 标记、C.5.2.2 输出（重排与延迟限制）、缺失参考生成；CRA / BLA 随机访问丢弃 RASL 图像 |
| 并行工具（V6） | `ctu.go`、`params.go` | tiles（地址映射、每 tile 上下文重置）、WPP 上下文存储 / 同步与子流重启、dependent slice segment |
| 性能（V7） | `neon_arm64.s`（由 `neon_gen.go` 生成）、`pipeline.go` | NEON 插值（8 / 4 抽头，两级）、单向 / 双向 / 加权输出、SAO edge；整数 MV 直接拷贝与平均；去块 + SAO 按 CTB 行在独立 goroutine 上流水化 |

证据：

- **与 FFmpeg 逐平面一致：** 16 个已提交向量（`testdata/vectors.txt`：AMP、transform skip、
  4 参考 B 金字塔、加权预测、无损与 cu 级无损、缩放列表、slices + WPP、CTU 16 / 32 与深 TU 树、
  开放 GOP 的 CRA、受限帧内、8×8 量化组，以及从 CRA / BLA 起解码并丢弃 RASL）、416×240 上 24 种
  x265 配置的压力语料，以及 1080p 语料（x265 medium、B + 4 参考 + 加权、WPP + CTU 32、无损、
  缩放列表、两段 `hevc_videotoolbox` 码流）。HEVC + AAC 的 MP4 经纯 Go 后端播放，帧与 FFmpeg
  一致（`TestGolangBackendMatchesFFmpeg`）。
- **第二个参照：** VideoToolbox 硬件解码器在全部 42 个小语料码流上与我们一致。在 FFmpeg 软件
  解码器与 VideoToolbox 不一致的两个 CTU-16 码流上（CTB 角附近一个 SAO 样本），我们与
  VideoToolbox 一致；`ctu16_sao_vt` 固定了这一情形。
- **速度**（1080p，Apple 芯片一个性能核加滤波 goroutine，`BenchmarkDecodeFile`）：高运动 x265
  medium **6.5 ms/帧**（FFmpeg `-threads 1`：3.8 ms）；典型屏幕内容 1.9–2.5 ms（FFmpeg 1.2–1.3 ms）。
  NEON 与 `-tags purego` 构建解码结果相同；流水化滤波与原地滤波结果相同
  （`TestPipelinedFilterMatchesInPlace`，race 检测干净）。

未完成项：tiles / dependent slice 向量（来自我们的编码器）、Main 10、NEON 去块与反变换、WPP 行并行解码。

HEVC **编码器**在解码器之后，里程碑结构与 R1 相同，验证方式也相同（重建 == 我们与 FFmpeg 的解码结果；质量对比 libx265）：

| # | 里程碑 | 内容 |
|---|---|---|
| H0 | 码流 | VPS/SPS/PPS、slice segment 头（显式短期 RPS）、Annex-B 与长度前缀输出 |
| H1 | 帧内 | CABAC 编码器 + 比特估计器、按 RD 的编码四叉树、35 模式帧内搜索、NxN、正向 DST/DCT、死区量化、符号隐藏 |
| H2 | 帧间 | skip / merge（解码器自己的候选推导）、整数 + 1/4 像素运动搜索的 AMVP、多参考 |
| H3 | 码率控制 | CQP、CRF、ABR、VBV（溢出时重编码） |
| H4 | B 图像、环内 | 非参考 B 图像的 mini-GOP、双向预测搜索、去块、SAO 决策 |
| H5 | 容器 | `mp4x` 的 `hvc1`/`hvcC`，普通与分片 MP4 |
| H6 | 性能 | NEON SATD/SAD/正向变换（加上解码器的 MC）、快速决策、按 CTU 行波前并行、并行去块 / SAO 统计 |

**编码器状态：H0–H6 已完成（2026-10-11）。**

| 部分 | 位置 | 状态 |
|---|---|---|
| 重建 | 与解码器同包的 `enc*.go` | 内嵌一个 `Decoder` 保存状态：参数集与 slice 头由解码器自己的解析器读回，CU 用其帧内预测 / 反量化 / 反变换 / MC / 状态设置函数重建，图像经过其 DPB 与环内滤波 |
| 码流（H0） | `enc_bits.go`、`enc_params.go` | VPS/SPS/PPS（VUI 颜色 + 时序）、slice 头、WPP / 均匀 tiles / slices / dependent slice segment，入口点按转义后的负载计数 |
| 帧内（H1） | `enc_cabac.go`、`enc_syntax.go`、`enc_intra.go`、`enc_tq.go` | 算术编码器与估计器共用 `binSink` 接口；语法写入函数逐元素对应解码器的解析函数；自底向上的四叉树（快照 / 恢复）；SATD 排序后 RD；通过奇偶调整实现符号隐藏 |
| 帧间（H2） | `enc_inter.go` | 解码器推导的 merge 候选分别按 skip 与带残差 merge 尝试；AMVP 2Nx2N 菱形 SAD 搜索加半 / 1/4 像素 SATD 细化；遵守推断 cbf 规则（全零 merge 变为 skip） |
| 码率控制（H3） | `enc_rc.go` | H.264 编码器的模型：带 I/B 偏移的 CQP、CRF、ABR（I/B 跟随 P 的 QP）、带预测器的 VBV；溢出的图像只重置其帧上下文后重编码 |
| B 图像、环内（H4） | `enc.go`、`enc_sao.go` | 每个 mini-GOP 最多 7 个 B 图像（RPS 保留锚点，重排深度 1）；图像分三遍编码——决策、去块 + SAO 决策（band / 4 种 edge 类 / merge，基于去块后与源图的统计）、熵编码 |
| 容器（H5） | `mp4x` | `Packet.DTS` = 编码序号 − `Reorder()`，配合编辑列表复用 |
| 性能（H6） | `neon_arm64.s`、`enc.go` | 提前 skip、P/B 中按条件尝试帧内、preset 0 帧间 CU ≥ 16×16；第一遍按 CTB 行波前并行（任意核数结果一致） |

证据：

- **自洽 + 一致性：** 15 种配置（帧内 / P / B，CTU 16 / 32 / 64，1–3 参考，WPP，tiles，带 dependent segment 的 slices，SAO 开 / 关，符号隐藏开 / 关，强制重编码的 VBV）在我们的解码器与 FFmpeg 中都精确解出重建结果（`TestEncoderReconMatchesDecoders`；在 FFmpeg 软件解码器出现其 SAO 偏差的 CTU-16 码流上，VideoToolbox 一致）。这些码流同时也是解码器 tiles 与 dependent slice 的覆盖。MP4 往返（普通 / 分片，含 B 图像）在我们的栈与 FFmpeg 中解码一致（`TestMP4RoundTrip`）。`testdata/encstreams.txt` 固定了每个码流；NEON、`-tags purego` 以及 1 核与 10 核产生的码流完全相同。
- **质量**（等码率 BD-PSNR，需手动开启的 `TestQualityVsX265`，3 个 B 图像，x265 使用 `-tune psnr`），合成噪声 416×240 / 屏幕内容 640×360：preset 0 对 `ultrafast` **+0.17 / +6.8 dB**，对 `veryfast` −0.26 / −1.6 dB；preset 1 对 `veryfast` **−0.04 / +3.6 dB**，对 `medium` −0.18 / +4.9 dB。
- **速度**（1080p，Apple 芯片，10 核，`BenchmarkEncodeFile`）：preset 0 **38 ms/帧**（屏幕内容）/ **47 ms/帧**（高运动）；preset 1 带 B 图像 52 / 68 ms；preset 0 单核 170 ms。
- **码率控制：** ABR 在三种码率下误差 15% 以内，VBV 从不下溢，CRF 单调（`enc_rc_test.go`）。

未完成项：RQT（变换划分）与矩形 / AMP 划分、RDOQ、B 金字塔、自适应量化、NEON 帧内预测。

### R3. AV1 解码器，然后编码器（`media/internal/av1`）

解码器范围：Main profile（8 / 10 bit 4:2:0）。

| # | 里程碑 | 内容 |
|---|---|---|
| A0 | 码流 | OBU、sequence / frame 头、tile 信息、`av1C` 及 `mp4x` 中的 ISOBMFF AV1 绑定 |
| A1 | 熵编码 | 自适应 CDF 的多符号算术解码器、系数编码 |
| A2 | 帧内 | 方向预测（角度偏移、边缘滤波 / 上采样）、smooth、Paeth、CfL、调色板、intra block copy |
| A3 | 帧间 | 单 / 复合预测、OBMC、warped 与全局运动、8 抽头滤波、wedge / diff-weighted 掩码、参考缩放 |
| A4 | 变换 | DCT / ADST / flipADST / identity，4×4 – 64×64 |
| A5 | 环路滤波 | 去块、CDEF、loop restoration（Wiener / self-guided）、superres |
| A6 | 胶片颗粒 | 按规范合成（作用于输出，不进入参考帧） |
| A7 | 性能 | NEON 内核、tile 与图像级并行 |

预言机：FFmpeg 搭配 **libdav1d**；向量用 **libsvtav1** 从同一批源片段本地编码（预设覆盖各工具，
胶片颗粒开 / 关）。AV1 **编码器**放在最后，以 SVT-AV1 的快速预设为质量参照。

向量用 FFmpeg 的 libsvtav1 从原始片段生成，例如：

```bash
ffmpeg -f rawvideo -pix_fmt yuv420p -s 320x180 -i clip.yuv -frames:v 12 \
  -c:v libsvtav1 -preset 4 -svtav1-params keyint=12:tile-columns=1 ra_p4.ivf
```

并与 `dav1d` 命令行比对（libdav1d，不经过 FFmpeg 在分辨率变化时的缩放；`--outputinvisible 1`
连从不显示的帧也一并比对，`--filmgrain 0/1` 分别比对无颗粒的参考与最终输出）。

**解码器状态：A0–A7 已完成（2026-10-11）。**

| 部分 | 位置 | 状态 |
|---|---|---|
| 码流（A0） | `obu.go`、`seq.go`、`header.go`、`ivf.go`、`config.go`；`mp4x` | OBU、sequence 头（operating point、时序 / 解码器模型、颜色配置）、完整的 uncompressed header（frame refs short signaling、tile 信息、量化、分割、delta q / lf、环路滤波、CDEF、LR、skip mode、全局运动、胶片颗粒参数）、IVF；`mp4x` 读写 `av01` + `av1C`，纯 Go 后端可播放 |
| 熵编码（A1） | `msac.go`、`cdf.go`、`cdf_tables.go`、`residual.go` | 带自适应的窗口化多符号算术解码器、66 张默认 CDF 表、含全部上下文与扫描顺序的系数编码 |
| 帧内（A2） | `intra.go`、`block.go` | 带角度偏移与边缘滤波 / 上采样的方向预测、smooth（H/V）、Paeth、递归 filter intra、CfL、调色板（含颜色缓存）、intra block copy |
| 帧间（A3） | `inter.go`、`mvpred.go`、`mfmv.go`、`mc.go` | MV 预测栈（空域、由投影运动场得到的时域、扩展搜索）、单 / 复合预测及 wedge、差值加权、距离加权与 inter-intra 掩码、OBMC、局部 warp 与全局运动、8 抽头滤波（含双滤波器）、参考缩放、skip mode、S 帧 |
| 变换（A4） | `transform.go`、`recon.go` | DCT 4–64、ADST 4/8/16、flipADST、identity、无损用 WHT；量化矩阵 |
| 环路滤波（A5） | `lf.go`、`cdef.go`、`restoration.go`、`lr.go` | 去块、CDEF、superres 上采样、loop restoration（Wiener、self-guided） |
| 胶片颗粒（A6） | `filmgrain.go` | 在输出上合成颗粒（8 / 10 bit、色度随亮度、重叠、裁剪），不进入参考帧 |
| 性能（A7） | `mc_fast.go`、`neon_arm64.s`（由 `neon_gen.go` 生成）、`parallel.go` | 无缩放 MC 直接读取参考帧并使用 NEON 8 抽头内核（taps 减半，使 8 bit 水平级在 int16 中精确）；按 stripe 缓冲的 loop restoration；CDEF 在每块邻域副本上计算；**tiles 并行解码**；去块（行 / 列带）、CDEF（64×64 行）与 LR（stripe）在所有核上运行；**单 tile 帧的环路滤波以 64 行为带、在独立 goroutine 上紧跟 tile 解码进行**（`pipeline.go`）；展开的逆 DCT（`idct_unrolled.go`，由 `idct_gen.go` 生成）、无分支 CDEF 抽头、带 padding 的系数上下文；帧与临时缓冲区复用 |

规范与参考解码器在细节上有出入之处（反变换蝶形的符号与中间裁剪、warp 采样步长、interintra
上下文、partition gather 顺序、量化矩阵只用于 `IDTX` 以下的类型、小尺寸色度的 OBMC、superres
覆盖整个宽度），解码器遵循 libaom / libdav1d，这是一致性码流所要求的。反变换还逐个一维地与
SVT-AV1 自身的实现比对过（`AV1_TX_VECTORS`）。

证据：

- **与 libdav1d 逐位一致：** 14 个已提交向量（`testdata/vectors.txt`，320×180：帧内与屏幕内容、
  preset 0 与 4 的随机访问（含 tiles）、低延迟 S 帧、superres、参考缩放、8 / 10 bit 胶片颗粒、
  量化矩阵加分割、无损、10 bit、全局运动），以及包括不可见帧在内的语料比对：preset 0–8 的
  帧内与随机访问、低延迟、去块 / CDEF / LR / superres 的各种组合、10 bit、2–8 个 tile、带 intra
  block copy 的屏幕内容、分辨率变化、S 帧、分割（AQ 与 variance boost）、overlay、无损、量化
  矩阵、胶片颗粒、ROTZOOM 全局运动，以及 1080p。AV1 + AAC 的 MP4 经纯 Go 后端播放，帧与
  libdav1d 一致，精确 seek 与拖动预览正常（`TestGolangBackendMatchesFFmpeg`、
  `TestGolangBackendPreciseSeek`）。
- **健壮性：** 损坏的 temporal unit 返回错误而不会 panic（`TestDecodeCorrupt`；`Decode` 会
  recover，tile / 滤波 worker 中的 panic 会转交给它）。race 检测干净。
- **速度**（1080p 随机访问，SVT-AV1 preset 8，Apple 芯片，`BenchmarkDecodeFile`）：单 tile
  10 核 **15.1 ms/帧**，单核 21.4 ms（libdav1d 单线程：1.9 ms）；4 个 tile **6.5 ms**，8 个 tile
  5.5 ms。经后端播放 1080p MP4 为 63 fps（单 tile）/ 157 fps（4 tile）。NEON 与 `-tags purego`
  构建解码结果相同（`TestMcNEON` 对每种滤波器、相位与宽度检查内核）。
- **速度，噪声内容**（1080p `testsrc2` + 噪声，SVT-AV1 preset 8 CRF 35，单 tile，M5，
  2026-10-17）：单核 40.6 ms/帧（变换 / CDEF / 上下文优化前为 67.5），**10 核 24.0 ms**
  （原 46.3；滤波流水线化前 27.2），已等于串行 tile 解码时间。流水线与原地滤波结果
  逐位一致（`TestPipelinedFilterMatchesInPlace`，race detector 通过）。

未完成项：本地编码器都不产生的工具（色度调色板、距离加权复合、双滤波器、delta 环路滤波、
intra-only 帧）已实现但没有向量固定；12 bit 与 4:2:2 / 4:4:4（High / Professional）会被拒绝；
后端把 10 bit 输出舍入为 8 bit；帧级并行解码（像 libdav1d 那样重叠多帧），以及 CDEF、LR、
变换与 10 bit MC 的 NEON 版本。

AV1 **编码器**沿用 HEVC 编码器的结构：内嵌一个 `Decoder` 保存状态（头部由它的解析器读回；块用它的
预测、变换与滤波重建），语法写入函数逐一对应它的解析函数，重建结果必须等于我们的解码器与 libdav1d
从码流解出的结果。质量以 SVT-AV1 的快速预设为参照。

| # | 里程碑 | 内容 |
|---|---|---|
| AE0 | 码流 | sequence / frame 头与 tile group OBU、使用解码器 CDF 自适应的多符号区间编码器、temporal unit、IVF |
| AE1 | 帧内 | 符号代价估计、RD 划分搜索、帧内模式（带角度偏移的方向预测、smooth、Paeth、CfL）、带类型与尺寸搜索的正向变换、量化、系数编码 |
| AE2 | 帧间 | 基于解码器 MV 栈的 NEAREST / NEAR / GLOBAL / NEW、运动搜索（整数 + 亚像素）、skip、多参考 |
| AE3 | 码率控制 | qindex 尺度上的 CQP、CRF、ABR、VBV（H.264 / HEVC 的模型） |
| AE4 | 层级、环内 | 用 `show_existing_frame` 显示的隐藏 ALTREF 帧、复合预测、去块强度、CDEF 与 loop restoration 搜索 |
| AE5 | 容器 | `mp4x` 的 `av01` / `av1C`，普通与分片 MP4 |
| AE6 | 性能 | NEON SAD / SATD / 正向变换、tile 与超级块行并行 |

**编码器状态：AE0–AE5 已完成，AE6 部分完成（2026-10-11）。** 编码器尚未接入任何公开 API，目前只能从 `media/internal/av1` 的测试中调用。

| 部分 | 位置 | 状态 |
|---|---|---|
| 码流（AE0） | `enc_bits.go`、`enc_msac.go` | 序列头和帧头由内嵌的 `Decoder` 回读解析。tile 数据由 libaom `od_ec` 区间编码器写出。符号在 pass A 中记录，确定 CDEF 和 LR 选择后再回放 |
| 帧内（AE1） | `enc_block.go`、`enc_search.go`、`enc_tq.go`、`enc_fdct.go` | 用快照做 RD 分块搜索，再做一次记录式提交。支持带角度偏移的方向模式、smooth、Paeth、CfL。搜索变换类型和深度。快速 DCT-II 按解码器的变换缩放 |
| 帧间（AE2） | `enc_inter.go` | 基于解码器 MV 栈的 NEAREST / NEAR / GLOBAL / NEW，整像素加亚像素搜索，skip mode，复合预测，插值滤波器搜索。候选先按 SATD 预排序 |
| 码率控制（AE3） | `enc_rc.go` | CQP、CRF（qindex ≈ 4·CRF）、ABR、VBV（溢出时重编） |
| 层级与环路滤波（AE4） | `enc_gop.go`、`enc_filter.go`、`enc_lr.go` | mini-GOP，隐藏 ALTREF 通过 `show_existing_frame` 显示。搜索去块级别、CDEF 强度集合，以及 Wiener / 自引导 LR |
| 容器（AE5） | `mp4x` | `av01` / `av1C` 封装，普通和分片两种（`TestEncMP4RoundTrip`） |
| 性能（AE6） | `enc.go` | 均匀 tile 并行搜索并并行回放（720p 起 2×2）。预设开关：帧间预排序、帧内跳过、无残差帧间块不再搜索分割、稀疏 CDEF 测量。环路滤波试算用满所有核 |

证据：

- 所有测试码流在本解码器和 libdav1d 中解出的结果都与编码器重建完全一致（`enc_test.go`）。
- 质量（`TestQualityVsSVT`，BD-PSNR），640×360：
  - 全帧内：对 SVT-AV1 preset 4 为 −0.01 dB；
  - 低延迟：对 preset 8 为 +0.02 dB；
  - 随机访问（MiniGOP 8）：对 preset 8 为 +0.14 dB；
  - preset 0 对 SVT preset 10：随机访问 −0.56 dB，低延迟 −0.85 dB。
- 速度（1080p，Apple M5 10 核，`BenchmarkEncodeFile`，qindex 128）：
  - preset 0：约 550 ms/帧，单核 1.36 s；
  - preset 1：6.8 s/帧；
  - preset 2：12 s/帧。
  - 1080p 下 4×2 tile 比 2×2 快 20%，质量损失 0.08 dB。

剩余工作：

1. **pass A 的超级块行波前。** 进行中，在分支 `wip/av1-sbrow-wavefront` 上。符号记录已改为按偏移引用 CDF，因此各行可以用各自的上下文副本搜索，再回放到 tile 唯一的上下文中。测试前还需要两处修复：
   - 每行的 `prev` 改为取该 tile 的上一个任务；
   - 复制上一行的 CDF 前，要等它完成 `min(2, cols)` 个超级块。

   之后验证与 libdav1d 逐位一致，以及 1 核和 10 核产出相同码流。1080p 下还有两个瓶颈：4 个 tile 只用到 10 核中的约 2.5 核，pass A 约占帧时间的 70%。
2. NEON SAD / SATD / 正向变换。现在 profile 已经很平（SATD 约占 5%），优先级低于第 1 项。
3. 提速 preset 1。它比 preset 0 慢 12 倍，主要来自矩形分块、LR Wiener 和变换类型搜索。
4. 像 H.264 / HEVC 那样，把编码器接入公开 API 或媒体管线。
5. 尚未实现：10-bit 编码、面向屏幕内容的调色板和帧内块复制、自适应量化。

### 顺序与成本

| 项目 | 大致工作量 | 备注 |
|---|---|---|
| R1 H.264 编码器 | 到 E4 约 2–3 个月 | 复用解码器的预测、变换、MC 与去块 |
| R2 HEVC 解码器 | 3–4 个月 | 最多复用 H.264 的设计；Main 10 另需约 3 周 |
| R2 HEVC 编码器 | 3–4 个月 | |
| R3 AV1 解码器 | 5–7 个月 | 工具最多；胶片颗粒与 loop restoration 最耗时 |
| R3 AV1 编码器 | 6 个月以上 | |

专利与许可（§3）因编解码器而异：HEVC 由多个专利池覆盖，AV1 由 AOMedia 免版税提供。这不改变
技术计划，但发布 HEVC 前值得先做决定。

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
*（已完成 —— 见[实现进度](#实现进度)。）*

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

**需要的那处引擎改动已完成：** 后端选择改为由 `QUI_MEDIA_BACKEND=native|golang|auto`
在运行时决定，原生与纯 Go 后端可以并存，并能在同一文件上对比。

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
**三项均已落地**：单核已约 59 fps（默认线程约 73 fps，见[性能](#性能)），块行并行（去块波前）
与 `avg2` 汇编内核已实测带来 +20% / +9%。

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
| H.264 CABAC 复杂度 | ~~高~~ **已完成** | CABAC 已实现且与 FFmpeg 逐像素一致 |
| 纯 Go 解码过于慢 | ~~高~~ **低** | 单核 1080p 约 59 fps、默认线程约 73 fps；块行并行 + `avg2` 汇编已落地（+20% / +9%）；把 OpenH264 作为同接口下的回退后端 |
| 专利（H.264/AAC） | 高，非技术 | 显式商业决策；自研并不能消除 |
| fragmented MP4 / fMP4 播放 | ~~中~~ **已完成** | `mp4x` 由 `moof/traf/trun` 组装 sample 表，fMP4 与渐进式一样播放 |
| 音频输出可移植性 | ~~中~~ **已完成** | `audioout` 已覆盖 macOS（AudioQueue）、Linux（ALSA via `dlopen`）、Windows（WASAPI via syscall） |

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