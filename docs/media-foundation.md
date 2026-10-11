# Media foundation: a self-owned, pure-Go audio/video stack

Status: **stages 1–4 landed (2026-10-11).** The codecs are **written by us in
pure Go**; the container layer builds on `go-mp4`; only audio *output* stays
behind a thin cgo seam. An H.264 + AAC-LC MP4 plays end-to-end (video, audio,
A/V sync, seek, scrub) on the pure-Go backend with no ffmpeg and no platform
codec framework:

```bash
QUI_MEDIA_BACKEND=golang go run ./examples/media-video ~/video.mp4
```

This document records the evidence behind that decision, the architecture as
built, and what remains. The original probes live in
[`tools/media-poc/`](../tools/media-poc/README.md).

## Implementation progress

| Piece | Location | State |
|---|---|---|
| Container (ISO-BMFF) | `media/internal/mp4x` | ✅ progressive and fragmented (`moof/traf/trun`) MP4 share one sample table. Per-sample DTS, **composition offsets (`ctts`)** and the **edit-list shift**, so `PTS` is the presentation time; `stss` sync table with an IDR-scan fallback when it is missing; `SeekKeyframe` / `SampleAtTime` by PTS (binary search); per-track `avcC` and `esds` (AudioSpecificConfig) |
| H.264 bitstream (H0) | `media/internal/h264` (`bits`, `nal`, `params`, `slice`) | ✅ Annex-B and length-prefixed NALs, avcC, SPS (incl. VUI: SAR, colour description, timing, `max_num_reorder_frames`) / PPS, full slice header (ref-list modification, pred-weight table, MMCO) |
| Entropy (H1/H6) | `cavlc.go`, `cabac.go` | ✅ **CAVLC and CABAC** for every syntax element, a spec-exact CABAC arithmetic engine (I_PCM re-alignment works) |
| Intra + transform (H1/H2/H7) | `intra.go`, `transform.go`, `recon.go` | ✅ I_PCM, I16×16, I4×4, **I8×8** (with reference filtering), 4×4 and **8×8 transform**, **custom scaling matrices** (SPS + PPS lists, fall-back rules A and B, flat/default lists) |
| Inter (H4/H5) | `mvpred.go`, `mc.go`, `recon.go` | ✅ P and **B** slices: all partitions and sub-partitions, `P_Skip`, `B_Skip`/`B_Direct` (**spatial and temporal direct**, `direct_8x8_inference`), median MV prediction, quarter-pel luma / eighth-pel chroma MC with edge emulation, **explicit and implicit weighted prediction** |
| DPB + output | `picture.go`, `decoder.go` | ✅ POC types 0/1/2, sliding window and MMCO 1–6, `frame_num` gap filling, ref-list init + modification, display-order bumping by the VUI reorder depth (else `MaxDpbFrames`), output-frame pool (`h264.Recycle`) |
| Deblocking (H3) | `deblock.go` | ✅ bS from intra/coefficients/motion (reference identity, not index), alpha/beta/tC0, `disable_deblocking_filter_idc` 0/1/2, chroma QP offsets |
| Robustness (H8) | `decoder.go`, `deblock.go` | ✅ multi-slice pictures, slices in any order (`TestArbitrarySliceOrder` reverses the slices inside every picture of a 3-slice stream and checks the planes are bit-identical), concealment of macroblocks no slice covered (copy from the nearest reference, else grey), missing-reference fallback, `recover()` around slice decode — corrupted streams never panic or hang (`robust_test.go`). A `ref_pic_list_modification` that names a picture absent from the DPB (a lost reference, or a `frame_num` reset without an IDR) is **concealed** — the reference already at that position is kept — instead of aborting the decode (counted in `concealedRefs`); this is what the OpenH264 error-concealment set requires |
| AAC-LC (A0–A4) | `media/internal/aac` | ✅ ASC/ADTS, ICS + section/scalefactor/spectral Huffman (two-level LUTs over the ISO 14496-3 codebook tables), pulse data, **TNS**, **M/S**, **intensity stereo**, **PNS**, inverse quant, IMDCT (DCT-IV over an N/4 FFT), sine + KBD windows, all four window sequences, overlap-add; mono / stereo, every LC sample rate. TNS is exercised by two committed vectors (`lc_44k_apple`, `lc_44k_is_ms`); PNS is verified by **energy** (`TestPNSEnegry`) — its sample values are implementation-defined |
| Audio output | `media/internal/audioout` | ✅ one `Sink` interface, three backends: **AudioQueue** (macOS), **ALSA** (Linux, loaded with `dlopen` so no ALSA headers are needed) and **WASAPI** (Windows, plain COM syscalls, no cgo). Bounded non-blocking `Write`, `Buffered`, `Flush`, `SetRate` (pitch-preserving on macOS; a resampler — pitch follows rate — on Linux/Windows), and a **content clock** mapped from the device sample time through a segment ring, so `HostTime` is what is heard. A build without cgo (or a machine without the runtime library) returns `ErrNotSupported` |
| YUV → RGB | `media/internal/yuv`, `media/gl_yuv.go` | ✅ CPU I420→RGBA (BT.601/709, full/limited range) and a **GPU path**: three R8 planes + a matrix shader (BT.601/709/2020, full/limited) drawn straight into the VideoView framebuffer |
| Pure-Go backend | `media/backend_golang.go`, `backend_golang_audio.go`, `video_frame_golang.go` | ✅ `Backend` / `Source` / `ScrubDecoder` / `AudioRenderer`: presentation-time video frames (display order, early frames dropped after a seek), precise and keyframe seek, independent scrub decoder that continues forward inside a GOP, AAC renderer with preroll + trim on seek, drained-audio clock extrapolation, `OnEnded` |
| Runtime backend selection | `media/backend.go` | ✅ `QUI_MEDIA_BACKEND=native\|golang\|auto` (default `auto` = native when registered, else `golang`) |

## Verification

- **H.264 is pixel-exact against FFmpeg** (every plane of every frame, in
  display order) on: 19 x264 configurations covering Baseline/Main/High, CAVLC
  and CABAC, B-pyramids, 1–16 references, weighted prediction (explicit and
  implicit), temporal and spatial direct, 8×8 transform, custom matrices,
  multi-slice, interlace-free odd sizes and cropping; 5 VideoToolbox
  (`h264_videotoolbox`) streams; a 1080p High-profile clip; and the inherited
  OpenH264 vectors.
- **OpenH264 conformance sweep (independently re-checked):** 49 clips from the
  OpenH264 conformance set decode **pixel-exact against FFmpeg on 46**. The
  three that differ are the deliberate **error-concealment** streams
  (`BA_MW_D_P_LOST`, `Error_I_P`, `MR2_TANDBERG_E`): each now decodes to the
  same frame count as FFmpeg (99 / 6 / 300) with no fatal error, but the
  concealed pixels differ — concealment is implementation-defined, so exact
  agreement there is not expected. `MR2_TANDBERG_E` is the case that found the
  H8 gap above (it previously failed at NAL 55).
- **I+P vs the OpenH264 oracle:** on a Main-profile I+P stream every plane of
  every frame is byte-identical to OpenH264 2.4.1 (`oh264-hashes`), not just to
  FFmpeg. On B-frame streams OpenH264 2.4.1 itself diverges from FFmpeg (and
  from us) on the B pictures — FFmpeg and this decoder agree, OpenH264 is the
  outlier — so FFmpeg is the practical oracle for B.
- **AAC-LC matches FFmpeg within ±1 LSB** (~137 dB SNR) on FFmpeg- and
  Apple-encoded streams, including intensity and M/S stereo.
- **Backend:** the e2e test decodes a committed B-frame + AAC MP4 and matches
  FFmpeg's frame hashes and PCM from the start and after a seek. GUI playback
  was checked on a 1080p file through `qui-agent`: real-time clock, seek,
  replay, A/V sync.

Committed vectors (offline, no native library):

| Path | Contents |
|---|---|
| `media/internal/h264/testdata/vectors.txt` | name, frame count, SHA-256 of the decoded frames for `cabac_high_bpyr`, `cavlc_main_b`, `cabac_temporal_wb`, `cabac_cqm`, `cavlc_slices`, `crop_offedge`, `vt_main_p8x8` |
| `media/internal/h264/testdata/*` | OpenH264-oracle Baseline P / intra / POC-type-2 vectors, plus `conceal_missing_ref.264` (a trimmed error-concealment stream whose `ref_pic_list_modification` names a picture missing from the DPB — pins the H8 concealment path) |
| `media/internal/aac/testdata/*.aac` + `.s16` | five LC streams (22.05/44.1/48 kHz, mono/stereo, Apple encoder, IS + M/S) with FFmpeg PCM, all within ±1 LSB |
| `media/internal/aac/testdata/pns/*` | an LC stream with heavy PNS (111 noise bands / 23 frames) and FFmpeg PCM; checked by energy (`TestPNSEnegry`) because PNS samples are implementation-defined |
| `media/testdata/bframes_aac_160x90.mp4` + `.ref.s16` | the backend e2e clip |

Corpus runs against FFmpeg (opt-in, not part of `go test ./...`):

```bash
AVC_CORPUS=/path/to/dir go test -run TestCorpus ./media/internal/h264   # *.264 + ffmpeg
AVC_BIG=/path/to/1080p.264 go test -run TestBigFile ./media/internal/h264
AAC_CORPUS=/path/to/dir go test -run TestADTSCorpus ./media/internal/aac   # *.aac + *.f32
```

## Performance

Decoding a 1080p30 High-profile CABAC P-frame screen recording costs
**~5.7 ms of CPU per frame** on an Apple-silicon performance core (~175 fps
single-threaded; ~107 fps for the original pure-Go decoder, i.e. **−39%
CPU**). Pipelined deblocking spreads that over two cores. Measured on the full player
(`examples/media-video`, `QUI_MEDIA_BACKEND=golang`, CPU time over 20 s):

| Playback | CPU |
|---|---|
| 1080p30, 1× | **~0.44 core** (was ~0.6; ~0.49 with `-tags purego`) |
| 1080p30, 2× | **~0.63 core** (was ~0.8; ~0.79 with `-tags purego`), spread over threads — no thread is near saturation |

What is in place:

| Change | Where | Effect |
|---|---|---|
| Retained GL draw | `video_view.go` | VideoView registers a `qui.RetainedGLCanvas` draw and asks for `Window.RequestGLRedraw` only when a new frame is promoted, ticking at the next frame's due time. Previously it kept itself dirty every tick, so the window re-rasterized, uploaded and swapped at the display refresh rate even while paused |
| Pipelined deblocking | `pipeline.go` | picture *n* is deblocked on its own goroutine while picture *n+1* decodes; motion compensation waits only for the reference rows it reads (per-picture row progress), output is copied once deblocking is done, and the decoder double-buffers its `picCtx`. +34% decode speed at identical CPU; `TestPipelinedDeblockMatchesInPlace` pins it to the in-place result |
| Decode-ahead ring | `backend_golang.go` (`decodeAhead`) | the decoder runs ~4 frames ahead on its own goroutine, so decode time overlaps display |
| NEON kernels | `neon_arm64.s` (generated by `neon_gen.go`) | luma 6-tap H / V / centre and chroma bilinear MC (16/8-wide luma, 8/4-wide chroma), the luma loop filter (normal and bS 4, vertical edges transposed in registers), the chroma loop filter (Cb and Cr in one 16-lane pass, TBL gather/scatter), and the 4×4 IDCT. Go's assembler lacks most NEON arithmetic, so the generator assembles standard arm64 syntax with clang and emits annotated `WORD`s. Each kernel is pinned to its Go reference by randomized tests (`neon_test.go`); `-tags purego` or non-arm64 builds use the Go code |
| `avg2` assembly | `avg_arm64.s`, `avg_amd64.s` | NEON `URHADD` / SSE2 `PAVGB` for quarter-pel and bi-prediction averaging; scalar `purego` fallback, `TestAvg2MatchesScalar` pins equality |
| CABAC engine | `cabac.go` | 64-bit bit cache instead of a `peek32` per renormalization; merged 256-entry state tables (no bounds checks, no MPS-flip branch) |
| Direct single-list MC | `recon.go` | unweighted single-list blocks predict straight into the picture (no scratch copy) |
| YUV quad | `gl_yuv.go` | the vertex buffer is only rewritten when the quad changes (rewriting it each frame stalled the GL driver) |
| Row-sliced 6-tap filters | `mc.go` | `lumaV` / `lumaHV` without per-tap bounds checks (~17–25% faster kernels) |
| Partition-aware bS | `deblock.go` (`samePartition`) | interior edges inside one motion partition skip the motion comparison |
| Output-frame pool | `picture.go` (`Recycle`) | no 3 MB allocation per 1080p frame |

**Wavefront (block-row) deblocking was tried and removed:** it was
bit-exact, but it bought ~14% decode speed for ~57% more total CPU (9.1 s vs
5.8 s on 600 frames) — spinning and goroutine handoff in the scheduler. For a
player, CPU per frame matters more than peak speed.

Remaining hot spots: CABAC `decision` (~19% of decode, inherently serial),
bS derivation and motion bookkeeping (`deblockMB`, `writeBackMotion`), and
residual parsing. In the player, presenting frames (`glfwSwapBuffers`) is
the largest non-decode cost. A bounds-check-free scalar rewrite of the loop
filter and triple-buffered YUV textures were both measured and dropped (no
gain).

## Known limitations

- **H.264:** progressive frames only — interlaced (PAFF fields, MBAFF) streams
  report `ErrUnsupported`; 4:2:0 at 8 bits only (no 4:2:2 / 4:4:4 / High 10);
  no SP/SI slices; no FMO slice groups.
- **AAC:** LC only. HE-AAC (SBR/PS) streams decode their LC core at half rate;
  CCE / LFE / more than two channels and 960-sample frames are not decoded.
  **One known deviation, reproduced by a committed vector:** in a 32 kHz stereo
  stream, a single short-window (`eightShort`) frame decodes wrong on the left
  channel (505 of 512 samples in `[448,960)` differ from FFmpeg, max 549). The
  reproducer and its localisation are in
  `media/internal/aac/testdata/known_bug/` and `short_window_test.go` (skipped
  while open); the stream is kept out of the ±1 LSB vector glob.
- **Audio output** is implemented for macOS (AudioQueue), Linux (ALSA, loaded
  through `dlopen` so no ALSA headers are needed) and Windows (WASAPI through
  plain syscalls, no cgo). A machine without the runtime library reports
  `ErrNotSupported`. The Linux sink is cgo (`-ldl`); the Windows sink is not.
- **cgo:** the pure-Go backend uploads through `go-gl`, so it is
  `//go:build cgo`. The decoders themselves build with `CGO_ENABLED=0`.

---

## Roadmap: next codecs

The H.264 + AAC playback stack is done; the next work extends the same
approach — self-written pure-Go codecs, NEON where profiling demands it,
FFmpeg as the correctness oracle — to encoding and to newer formats. In
order:

### R1. H.264 encoder (`media/internal/h264enc`)

Goal: encode I420 frames to an H.264 elementary stream and an MP4 that any
player decodes, at 1080p30 in real time on one performance core with a
"fast" preset.

| # | Milestone | Contents |
|---|---|---|
| E0 | Bitstream | bit writer, Exp-Golomb, emulation prevention, SPS/PPS/slice header, Annex-B and length-prefixed output |
| E1 | Intra | I16x16 / I4x4 mode decision (SAD → SATD), forward 4×4 integer transform + quantization (flat matrices), CAVLC residual coding, reconstruction loop reusing the decoder's prediction, IDCT and deblocking |
| E2 | P frames | integer motion search (diamond / hexagon, predictor candidates), quarter-pel refinement with the decoder's MC kernels, P_Skip / 16x16 / 16x8 / 8x16 / 8x8 decisions, one reference |
| E3 | Rate control | constant QP, then ABR / CRF-style with a VBV model; keyframe interval; `max_num_reorder_frames = 0` |
| E4 | CABAC + High | CABAC encoding engine and context modeling, 8×8 transform, B frames (later), multiple references |
| E5 | Container | `mp4x` writer: `avcC`, `stts`/`stsz`/`stco`/`stss`/`ctts`, fragmented output for streaming; AAC encoder is a separate, later item |
| E6 | Performance | NEON SAD/SATD, forward DCT, quantization; slice- or frame-parallel encoding |

Verification:

- **Self-consistency:** the encoder's reconstruction must equal, byte for
  byte, what both our decoder and FFmpeg decode from its stream (a mismatch
  is an encoder bug, never "close enough").
- **Conformance:** `ffmpeg -v error -i out.264 -f null -` reports nothing;
  committed vectors pin stream hashes.
- **Quality:** PSNR / SSIM vs `libx264 -preset ultrafast/veryfast` at equal
  bitrate on the existing test clips (`export 1080.mp4`, the committed
  clips); target within ~1 dB of `ultrafast` before CABAC, within ~0.5 dB of
  `veryfast` after E4.

**Status: E0–E6 done (2026-10-11).**

| Piece | Location | State |
|---|---|---|
| Decoder kit | `h264/enckit.go` | the decoder's intra predictors, IDCT, dequant tables, MC, CAVLC/CABAC tables, 8×8 reference filter and deblocking, exported so the encoder reconstructs with exactly the decoding code |
| Bitstream (E0) | `bits.go`, `headers.go` | bit writer, ue/se/te, emulation prevention, SPS (level picked from size/rate/DPB, cropping, VUI colour + timing + `max_num_reorder_frames`), PPS, slice header; Annex-B and length-prefixed output |
| Intra (E1) | `intra.go`, `quant.go`, `cavlc.go` | I16×16 / I4×4 / chroma decisions on SATD + mode bits, forward 4×4 transform, deadzone quantisation, Hadamard DC paths, CAVLC |
| Inter (E2) | `inter.go`, `hpel.go` | spec MV prediction, P_Skip, 16×16 / 16×8 / 8×16 / 8×8, hexagon (or diamond) integer search from predictor/neighbour/co-located candidates, half- then quarter-sample SATD refinement on precomputed half-sample planes, multiple references, inter decimation |
| Rate control (E3) | `ratecontrol.go` | CQP with I/B offsets, CRF (calibrated against x264's QP mapping), ABR with refit rate factor + overflow correction, VBV leaky bucket with IDR look-ahead and re-encode on overflow |
| CABAC + High (E4) | `cabac.go`, `transform8.go`, `bframes.go` | CABAC engine and all syntax elements (contexts mirror the decoder), Intra8×8 and the inter 8×8 transform (SA8D decision), B pictures: mini-GOPs, spatial direct, B_Skip, L0/L1/bi |
| Container (E5) | `mp4x/writer.go` | progressive (`stts`/`ctts`/`stss`/`stsc`/`stsz`/`stco`/`co64`, edit list) and fragmented (`mvex`, `moof`/`traf`/`trun`) MP4 with `avc1`/`avcC` and `mp4a`/`esds` |
| Performance (E6) | `neon_arm64.s` (from `neon_gen.go`) | NEON SAD, SATD and a fused 8×8 forward-transform + quantise; an early-exit skip probe; slice-parallel coding and parallel half-sample planes |

Evidence:

- **Self-consistency + conformance:** 22 configurations (Baseline / Main /
  High, CAVLC / CABAC, B pictures, 1–3 references, slices, CRF, extreme QPs,
  deblocking offsets) on two committed sources (one with cropping)
  reconstruct byte-identically to both our decoder and FFmpeg, which reports
  no errors (`TestReconMatchesDecoder`, `TestFFmpegConformance`). MP4 round
  trips (progressive and fragmented, with B pictures) decode identically in
  our stack and in FFmpeg (`TestMP4RoundTrip`, `mp4x.TestRemux`).
  `testdata/streams.txt` pins every configuration's stream hash; NEON and
  `-tags purego` builds produce identical streams.
- **Quality** (BD-PSNR at equal bitrate, 30 frames, opt-in
  `TestQualityVsX264`): fast preset **+4.1 dB** (360p) / **+3.2 dB** (1080p)
  vs `x264 -preset ultrafast`; High + CABAC + 8×8 + B **+0.27 dB** /
  **+0.12 dB** vs `x264 -preset veryfast -tune psnr`.
- **Speed** (1080p, one Apple-silicon performance core, `BenchmarkEncode1080`):
  fast **12.0 ms/frame** (~83 fps), medium 18.8 ms, High + CABAC + B 22.5 ms;
  four slices roughly halve each.
- **Rate control:** ABR lands within ±6% of target on real footage, VBV never
  underflows, CRF is monotonic (`ratecontrol_test.go`).

Open items: AQ / psy tuning (they trade PSNR for subjective quality), B_8x8
and 16×8/8×16 B partitions, weighted prediction, frame-parallel encoding,
and an AAC encoder.

### R2. H.265 / HEVC decoder, then encoder (`media/internal/hevc`)

Decoder scope: Main profile (8-bit 4:2:0), progressive; Main 10 next.

| # | Milestone | Contents |
|---|---|---|
| V0 | Bitstream | NAL header (2 bytes), VPS/SPS/PPS incl. VUI, short-term / long-term RPS, slice segment header; `hvcC` in `mp4x` |
| V1 | Entropy | HEVC CABAC (new context tables, bypass-heavy binarizations), coding quadtree (CTU 16–64), transform tree |
| V2 | Intra | 35 modes (planar, DC, 33 angular) with reference smoothing, 4×4 DST, 4–32 inverse DCT, scaling lists, transform skip |
| V3 | Inter | merge (spatial / temporal / combined bi / zero candidates), AMVP, 8-tap luma / 4-tap chroma MC, weighted prediction, TMVP |
| V4 | In-loop | deblocking (8×8 grid) and SAO (band / edge) |
| V5 | DPB | RPS-driven reference marking, POC, bumping by `sps_max_num_reorder_pics`; CRA / RASL / BLA random access |
| V6 | Parallel tools | tiles and WPP entry points (parsed; WPP decoding can use row parallelism), dependent slices |
| V7 | Performance | NEON 8-tap MC, transforms, SAO / deblock; picture-level pipelining as in H.264 |

Vectors (all generated locally from `export 1080.mp4` and the committed
clips; outputs compared plane-exact against FFmpeg's `hevc` decoder):

```bash
ffmpeg -i "export 1080.mp4" -t 10 -c:v libx265 -preset medium -x265-params keyint=60 -an hevc_main.mp4
ffmpeg -i "export 1080.mp4" -t 10 -c:v libx265 -x265-params "bframes=4:ref=4:weightb=1:sao=1" -an hevc_b.mp4
ffmpeg -i "export 1080.mp4" -t 10 -c:v libx265 -x265-params "wpp=1:ctu=32" -an hevc_wpp.mp4
ffmpeg -i "export 1080.mp4" -t 2 -c:v libx265 -x265-params lossless=1 -an hevc_lossless.mp4
ffmpeg -i "export 1080.mp4" -t 10 -c:v libx265 -x265-params scaling-list=default -an hevc_sl.mp4
ffmpeg -i "export 1080.mp4" -t 10 -c:v hevc_videotoolbox -b:v 4M -an hevc_vt.mp4   # Apple encoder
ffmpeg -i in.mp4 -c:v libx265 -pix_fmt yuv420p10le ...                            # Main 10, later
```

Small clips (≤ 160×90, a few frames) go into `testdata` with frame hashes,
as for H.264; the 1080p clips stay opt-in corpus runs.

**Decoder status: V0–V7 done (2026-10-11).** Tiles and dependent slices
are implemented but no encoder at hand emits them (x265 and VideoToolbox do
not); the HEVC encoder below will, and will pin them with vectors.

| Piece | Location | State |
|---|---|---|
| Bitstream (V0) | `bits.go`, `params.go`, `slice.go`, `scaling.go`; `mp4x` | VPS/SPS/PPS with VUI, short-term RPS with inter-RPS prediction, long-term references, slice segment header, scaling lists; `hvc1`/`hev1` + `hvcC` read and written by `mp4x`, played by the pure-Go backend |
| Entropy (V1) | `cabac.go`, `ctu.go`, `residual.go` | CABAC with the HEVC init tables, coding quadtree (CTU 16–64, implicit splits at picture edges), transform tree, residual coding with sign hiding and Rice adaptation, cu_qp_delta quantisation groups, PCM |
| Intra (V2) | `intra.go`, `transform.go` | 35 modes with reference substitution / smoothing / strong smoothing and boundary filters, 4×4 DST, 4–32 inverse DCT accumulated over non-zero inputs, transform skip, lossless (transquant bypass), constrained intra |
| Inter (V3) | `inter.go`, `mc.go` | merge (spatial, temporal, combined bi, zero), AMVP with scaling, TMVP, 8-/4-tap interpolation, explicit weighted prediction, AMP |
| In-loop (V4) | `deblock.go`, `sao.go` | deblocking with slice / tile boundary rules, SAO band / edge with per-CTB neighbour validity |
| DPB (V5) | `decoder.go` | POC, RPS marking, C.5.2.2 bumping (reorder and latency limits), missing-reference generation; CRA / BLA random access drops RASL pictures |
| Parallel tools (V6) | `ctu.go`, `params.go` | tiles (address maps, per-tile context reset), WPP context storage / sync and substream restarts, dependent slice segments |
| Performance (V7) | `neon_arm64.s` (from `neon_gen.go`), `pipeline.go` | NEON interpolation (8-/4-tap, both stages), uni / bi / weighted output, SAO edge offset; integer-MV copies and averages; deblocking + SAO pipelined on their own goroutine by CTB rows |

Evidence:

- **Plane-exact vs FFmpeg:** 16 committed vectors (`testdata/vectors.txt`:
  AMP, transform skip, B pyramid with 4 references, weighted prediction,
  lossless and cu-lossless, scaling lists, slices + WPP, CTU 16 / 32 with
  deep TU trees, open-GOP CRA, constrained intra, 8×8 quantisation groups,
  and decoding from a CRA / BLA with RASL dropped), a 24-configuration x265
  stress corpus at 416×240, and 1080p corpus runs (x265 medium, B + 4 refs
  + weighted, WPP + CTU 32, lossless, scaling lists, and two
  `hevc_videotoolbox` streams). The HEVC + AAC MP4 plays through the
  pure-Go backend with frames matching FFmpeg (`TestGolangBackendMatchesFFmpeg`).
- **Second oracle:** VideoToolbox's hardware decoder agrees with us on all
  42 small corpus streams. On the two CTU-16 streams where FFmpeg's software
  decoder disagrees with VideoToolbox (one SAO sample next to a CTB corner),
  we match VideoToolbox; `ctu16_sao_vt` pins that case.
- **Speed** (1080p, Apple-silicon performance core plus the filter
  goroutine, `BenchmarkDecodeFile`): high-motion x265 medium **6.5 ms/frame**
  (FFmpeg `-threads 1`: 3.8 ms); typical screen content 1.9–2.5 ms
  (FFmpeg 1.2–1.3 ms). NEON and `-tags purego` builds decode identically;
  pipelined and in-place filtering are pinned equal
  (`TestPipelinedFilterMatchesInPlace`, race detector clean).

Open items: tile / dependent-slice vectors (from our encoder), Main 10,
NEON deblocking and inverse transform, WPP row-parallel decoding.

The HEVC **encoder** follows the decoder with the same milestone shape as
R1, verified the same way (reconstruction == decode by us and by FFmpeg;
quality vs libx265):

| # | Milestone | Contents |
|---|---|---|
| H0 | Bitstream | VPS/SPS/PPS, slice segment headers (explicit short-term RPS), Annex-B and length-prefixed output |
| H1 | Intra | CABAC coder + bit estimator, coding quadtree by RD, 35-mode intra search, NxN, forward DST/DCT, dead-zone quantisation, sign data hiding |
| H2 | Inter | skip / merge (the decoder's candidate derivation), AMVP with integer + quarter-sample motion search, multiple references |
| H3 | Rate control | CQP, CRF, ABR, VBV (re-encode on overflow) |
| H4 | B pictures, in-loop | mini-GOPs of non-reference B pictures, bi-prediction search, deblocking, SAO decisions |
| H5 | Container | `mp4x` `hvc1`/`hvcC`, progressive and fragmented |
| H6 | Performance | NEON SATD/SAD/forward transform (plus the decoder's MC), fast decisions, wavefront-parallel CTU rows, parallel deblocking / SAO statistics |

**Encoder status: H0–H6 done (2026-10-11).**

| Piece | Location | State |
|---|---|---|
| Reconstruction | `enc*.go` beside the decoder | an embedded `Decoder` holds the state: parameter sets and slice headers are parsed back by the decoder's parsers, CUs are reconstructed with its intra prediction / dequantisation / inverse transform / MC / state setters, pictures go through its DPB and loop filters |
| Bitstream (H0) | `enc_bits.go`, `enc_params.go` | VPS/SPS/PPS (VUI colour + timing), slice headers, WPP / uniform tiles / slices / dependent slice segments with entry points counted over the escaped payload |
| Intra (H1) | `enc_cabac.go`, `enc_syntax.go`, `enc_intra.go`, `enc_tq.go` | one `binSink` interface for the arithmetic coder and the estimator; syntax writers mirror the decoder's parsers; bottom-up quadtree with snapshot/restore; SATD-ranked modes then RD; sign data hiding by parity adjustment |
| Inter (H2) | `enc_inter.go` | merge candidates from the decoder's own derivation tried as skip and merge-with-residual; AMVP 2Nx2N with diamond SAD search and half/quarter-sample SATD refinement; inferred-cbf rules respected (all-zero merges become skips) |
| Rate control (H3) | `enc_rc.go` | the H.264 encoder's model: CQP with I/B offsets, CRF, ABR (I/B follow the P QP), VBV with predictors; an overflowing picture is re-coded after resetting only its frame context |
| B pictures, in-loop (H4) | `enc.go`, `enc_sao.go` | up to 7 B pictures per mini-GOP (RPS keeps the anchors, reorder 1); pictures coded in three passes — decide, deblock + SAO decision (band / 4 edge classes / merge, deblocked-vs-source statistics), entropy-code |
| Container (H5) | `mp4x` | `Packet.DTS` = coding index − `Reorder()`, muxed with an edit list |
| Performance (H6) | `neon_arm64.s`, `enc.go` | early skip, gated intra in P/B, preset 0 inter CUs ≥ 16×16; pass A as a wavefront over CTB rows (deterministic for any core count) |

Evidence:

- **Self-consistency + conformance:** 15 configurations (intra / P / B,
  CTU 16 / 32 / 64, 1–3 references, WPP, tiles, slices with dependent
  segments, SAO on/off, sign hiding on/off, a VBV that forces re-encodes)
  decode to exactly the reconstruction in our decoder and FFmpeg
  (`TestEncoderReconMatchesDecoders`; on CTU-16 streams where FFmpeg's
  software decoder shows its SAO deviation, VideoToolbox matches). These
  streams are also the decoder's tile and dependent-slice coverage. MP4
  round trips (progressive / fragmented, with B pictures) decode
  identically in our stack and FFmpeg (`TestMP4RoundTrip`).
  `testdata/encstreams.txt` pins every stream; NEON, `-tags purego` and 1
  vs 10 cores produce identical streams.
- **Quality** (BD-PSNR at equal bitrate, opt-in `TestQualityVsX265`, three B
  pictures, x265 with `-tune psnr`), synthetic noisy 416×240 / screen
  content 640×360: preset 0 **+0.17 / +6.8 dB** vs `ultrafast`, −0.26 /
  −1.6 dB vs `veryfast`; preset 1 **−0.04 / +3.6 dB** vs `veryfast`, −0.18 /
  +4.9 dB vs `medium`.
- **Speed** (1080p, Apple silicon, 10 cores, `BenchmarkEncodeFile`):
  preset 0 **38 ms/frame** (screen content) / **47 ms/frame** (high
  motion); preset 1 with B pictures 52 / 68 ms; preset 0 on one core
  170 ms.
- **Rate control:** ABR within 15% at three rates, VBV never underflows,
  CRF is monotonic (`enc_rc_test.go`).

Open items: RQT (transform splits) and rectangular / AMP partitions,
RDOQ, B-pyramids, adaptive quantisation, and NEON intra prediction.

### R3. AV1 decoder, then encoder (`media/internal/av1`)

Decoder scope: Main profile (8- and 10-bit 4:2:0).

| # | Milestone | Contents |
|---|---|---|
| A0 | Bitstream | OBUs, sequence / frame headers, tile info, `av1C` and the ISOBMFF AV1 binding in `mp4x` |
| A1 | Entropy | multi-symbol arithmetic decoder with adaptive CDFs, coefficient coding |
| A2 | Intra | directional (with angle deltas, edge filtering / upsampling), smooth, Paeth, CfL, palette, intra block copy |
| A3 | Inter | single / compound prediction, OBMC, warped and global motion, 8-tap filters, wedge / diff-weighted masks, reference scaling |
| A4 | Transforms | DCT / ADST / flipADST / identity, 4×4 – 64×64 |
| A5 | Loop filters | deblocking, CDEF, loop restoration (Wiener / self-guided), superres |
| A6 | Film grain | synthesis per the spec (applied on output, not in the reference) |
| A7 | Performance | NEON kernels, tile- and picture-level parallelism |

Oracle: FFmpeg with **libdav1d**; vectors encoded locally with
**libsvtav1** (presets spanning the toolset, film grain on and off) from the
same source clips. The AV1 **encoder** comes last, starting from SVT-AV1's
fast presets as the quality reference.

Vectors are made with FFmpeg's libsvtav1 from raw clips, for example:

```bash
ffmpeg -f rawvideo -pix_fmt yuv420p -s 320x180 -i clip.yuv -frames:v 12 \
  -c:v libsvtav1 -preset 4 -svtav1-params keyint=12:tile-columns=1 ra_p4.ivf
```

and compared against the `dav1d` CLI (libdav1d without FFmpeg's scaling on
resolution changes; `--outputinvisible 1` also checks the frames that are
never shown, `--filmgrain 0/1` the grain-free reference and the output).

**Decoder status: A0–A7 done (2026-10-11).**

| Piece | Location | State |
|---|---|---|
| Bitstream (A0) | `obu.go`, `seq.go`, `header.go`, `ivf.go`, `config.go`; `mp4x` | OBUs, sequence header (operating points, timing / decoder model, colour config), the full uncompressed header (frame refs short signaling, tile info, quantisation, segmentation, delta q / lf, loop filter, CDEF, LR, skip mode, global motion, film grain parameters), IVF; `av01` + `av1C` read and written by `mp4x`, played by the pure-Go backend |
| Entropy (A1) | `msac.go`, `cdf.go`, `cdf_tables.go`, `residual.go` | windowed multi-symbol arithmetic decoder with adaptation, 66 default CDF tables, coefficient coding with all contexts and scans |
| Intra (A2) | `intra.go`, `block.go` | directional with angle deltas and edge filtering / upsampling, smooth (H/V), Paeth, recursive filter intra, CfL, palette (with colour cache), intra block copy |
| Inter (A3) | `inter.go`, `mvpred.go`, `mfmv.go`, `mc.go` | MV prediction stack (spatial, temporal from the projected motion field, extra search), single / compound with wedge, difference-weighted, distance-weighted and inter-intra masks, OBMC, local warped and global motion, 8-tap filters incl. dual filter, reference scaling, skip mode, S-frames |
| Transforms (A4) | `transform.go`, `recon.go` | DCT 4–64, ADST 4/8/16, flipADST, identity, WHT for lossless; quantiser matrices |
| Loop filters (A5) | `lf.go`, `cdef.go`, `restoration.go`, `lr.go` | deblocking, CDEF, superres upscaling, loop restoration (Wiener, self-guided) |
| Film grain (A6) | `filmgrain.go` | grain synthesis on output (8 / 10-bit, chroma from luma, overlap, clipping), never in references |
| Performance (A7) | `mc_fast.go`, `neon_arm64.s` (from `neon_gen.go`), `parallel.go` | unscaled MC reads the reference in place with NEON 8-tap kernels (halved taps keep the 8-bit horizontal pass exact in int16); stripe-buffered loop restoration; CDEF on a per-block neighbourhood copy; **tiles decode in parallel**; deblocking (row / column bands), CDEF (64×64 rows) and LR (stripes) run on all cores; pooled frame and scratch buffers |

Where the specification and the reference decoders differ in detail
(inverse-transform butterfly signs and intermediate clamps, warp-sample
steps, the interintra context, the partition gather order, quantiser
matrices only below `IDTX`, OBMC on small chroma, superres over the full
width), the decoder follows libaom / libdav1d, as conformant streams
require. The inverse transforms were also checked one-dimensionally
against SVT-AV1's own (`AV1_TX_VECTORS`).

Evidence:

- **Bit-exact vs libdav1d:** 14 committed vectors (`testdata/vectors.txt`,
  320×180: intra and screen content, random access at presets 0 and 4
  with tiles, low-delay S-frames, superres, reference scaling, film grain
  8 / 10-bit, quantiser matrices with segmentation, lossless, 10-bit,
  global motion), plus corpus runs including the invisible frames: intra
  and random access at every preset 0–8, low delay, deblocking / CDEF /
  LR / superres variants, 10-bit, 2–8 tiles, screen content with intra
  block copy, resize, S-frames, segmentation (AQ and variance boost),
  overlays, lossless, quantiser matrices, film grain, ROTZOOM global
  motion, and 1080p. The AV1 + AAC MP4 plays through the pure-Go backend
  with frames matching libdav1d, precise seek and scrubbing
  (`TestGolangBackendMatchesFFmpeg`, `TestGolangBackendPreciseSeek`).
- **Robustness:** damaged temporal units return errors and never panic
  (`TestDecodeCorrupt`; `Decode` recovers, and panics in tile / filter
  workers are re-raised to it). The race detector is clean.
- **Speed** (1080p random access, SVT-AV1 preset 8, Apple silicon,
  `BenchmarkDecodeFile`): one tile **15.1 ms/frame** on 10 cores, 21.4 ms
  on one core (libdav1d single-threaded: 1.9 ms); four tiles **6.5 ms**,
  eight tiles 5.5 ms. Through the backend a 1080p MP4 plays at 63 fps
  (one tile) / 157 fps (four tiles). NEON and `-tags purego` builds
  decode identically (`TestMcNEON` checks the kernels for every filter,
  phase and width).

Open items: tools no local encoder emits (chroma palettes,
distance-weighted compound, dual filter, delta loop filter, intra-only
frames) are implemented but not pinned by vectors; 12-bit and 4:2:2 /
4:4:4 (High / Professional) are rejected; the backend rounds 10-bit
output to 8 bits; frame-parallel decoding (overlapping frames, as libdav1d
does), and NEON for CDEF, LR, transforms and 10-bit MC.

### Order and cost

| Item | Rough effort | Note |
|---|---|---|
| R1 H.264 encoder | 2–3 months to E4 | reuses the decoder's prediction, transforms, MC and deblocking |
| R2 HEVC decoder | 3–4 months | biggest reuse of H.264 design; Main 10 adds ~3 weeks |
| R2 HEVC encoder | 3–4 months | |
| R3 AV1 decoder | 5–7 months | largest toolset; film grain and loop restoration are the long poles |
| R3 AV1 encoder | 6+ months | |

Patents and licensing (§3) differ per codec: HEVC is covered by several
pools, AV1 is royalty-free from AOMedia. That does not change the
technical plan but is worth a decision before shipping HEVC.

---

## 1. Where we are today

`media/` currently ships an **AVFoundation backend** that only works on macOS
(`media/backend_darwin.go`, `//go:build darwin && cgo`). Everywhere else
`media/backend_other.go` registers no backend and every entry point returns
`ErrNotSupported`. The package is already well-shaped for a replacement: the
whole platform stack sits behind one internal interface (`media/backend.go`:

- `Backend` — `OpenSource(path)`, `Capabilities()`
- `Source` — `VideoTrack`, `AudioTrack`, `NextVideoFrame`, `SeekVideo`,
  `OpenScrubDecoder`, `AudioRenderer`, `Close`
- `ScrubDecoder`, `AudioRenderer`

plus `VideoFrame` (`media/types.go`) and the A/V sync clocks
(`media/clock.go`). The render path already exists: `media/frame_view.go`'s
`PushRGBA → uploadAndDraw` composites CPU frames onto the framebuffer via GL.

**Goal:** a second backend, `backend_golang.go`, that needs neither ffmpeg nor
macOS, so the same `VideoView` / `AudioPlayer` work on Linux and Windows.
*(Done — see [Implementation progress](#implementation-progress).)*

---

## 2. What we validated (runnable code, real files)

All four probes run from `tools/media-poc/` against real MP4s, no cgo for the
decode paths.

| Layer | Approach | Result | License |
|---|---|---|---|
| Demux | `github.com/abema/go-mp4` | H.264 + AAC tracks, chunk/sample tables, IDR indices, `esds`/`avcC` config all extracted | MIT |
| Video, all frames | Cisco **OpenH264** via `github.com/y9o/go-openh264` (purego `dlopen`) | 1920×1080, 7931 frames in 26.6 s = **298 fps (~9× realtime)**, 3.4 ms/frame; visually correct I/P/B output | binding MIT, binary see §3 |
| Video, keyframes | `github.com/Eyevinn/hi264` (pure Go) | IDR decode ~24 ms/frame @1080p; **IDR-only, cannot play** | MIT |
| Audio | `github.com/skrashevich/go-aac` (pure Go) | 11389 AAC-LC frames → 264.45 s stereo PCM, duration matches container exactly, RMS ≈ −6.7 dBFS | **LGPL-3.0** |
| Render + sync | existing `frame_view.go` / `clock.go` | ready, no engine change | — |
| Audio output | CoreAudio / WASAPI / ALSA | **not possible in pure Go**; thin cgo seam required | — |

**Takeaways**

1. The container layer is *cheap*. `go-mp4.Probe` already assembles the sample
   timeline; we supply the chunk→offset mapping (a few dozen lines, in
   `mp4util`). Budget days, not weeks.
2. Full-motion H.264 in Go **works and is fast** — but only via the OpenH264
   binary. A pure-Go decoder does not exist in usable form today (`hi264` is
   keyframes-only; `go-264` is experimental).
3. Pure-Go AAC-LC decode is a solved, off-the-shelf problem — under LGPL.
4. Audio *output* is the one layer pure Go cannot own; it must call the OS.

---

## 3. The real constraint is licensing and patents, not performance

**ffmpeg is not the problem; H.264 and AAC patents are** — and no
implementation, including one we write ourselves, avoids them. What we *can*
avoid is licensing that constrains how we ship.

**OpenH264 binary.** Cisco's AVC/H.264 Patent Portfolio coverage applies to
their binary **only if** all of:

1. the binary is **downloaded separately onto the end user's device** — not
   pre-bundled with third-party software;
2. the end user can **control (enable/disable)** it;
3. third-party software **displays** `"OpenH264 Video Codec provided by Cisco
   Systems, Inc."`, and reproduces it in the EULA.

Bundling the dylib into a qui app forfeits the coverage and puts the royalty on
us. That is why OpenH264 is used here as an **oracle/reference**, not as the
shipping codec.

**LGPL AAC.** The mature pure-Go AAC decoders derive from FFmpeg/FAAD2 and are
LGPL. Go statically links into one binary, which makes the LGPL relink
obligation awkward. Self-writing sidesteps it.

**Self-writing does not remove the patents.** It removes the *binary dependency*
and the *LGPL*, and gives us full control of the decode path (which we want for
a framework anyway). The patent position is a separate, commercial decision we
track explicitly.

---

## 4. Target architecture

```
media/
  backend.go            # existing seam; add runtime backend selection
  backend_darwin.go     # keep: hardware decode via VideoToolbox (optional accel)
  backend_golang.go     # NEW: the cross-platform pure-Go pipeline
  internal/
    mp4x/               # container: timeline, seek, fragmented MP4  (go-mp4 based)
    h264/               # NEW: self-written H.264 decoder
    aac/                # NEW: self-written AAC-LC decoder
    yuv/                # planar YUV -> RGBA, and I420 GL upload shader
    audioout/           # thin cgo sink: CoreAudio / WASAPI / ALSA
```

Rules this respects:

- `media/internal/*` are internal subpackages, so the public API does not grow.
- The decoders are pure Go and depend only on the standard library.
- `audioout` is the single cgo island, behind a small interface so the rest
  stays buildable with `CGO_ENABLED=0` (it returns `ErrNotSupported`).
- `backend_golang.go` implements the existing `Backend`/`Source`/
  `AudioRenderer` interfaces, so `VideoView` and `AudioPlayer` are unchanged.

**One engine change was required** and is done: backend selection is
runtime-driven through `QUI_MEDIA_BACKEND=native|golang|auto`, so the native
and pure-Go backends coexist and can be compared on the same file.

---

## 5. Decoder plan

### 5.1 H.264/AVC (`internal/h264`)

Ordered by dependency; each milestone is verified against the OpenH264 oracle
(same clip in, plane-exact diff out).

| # | Milestone | Contents |
|---|---|---|
| H0 | Bitstream | NAL parsing, EBSP→RBSP, exp-Golomb, SPS/PPS, slice header |
| H1 | Intra | I_PCM, I16×16 (all prediction modes), I4×4 (9 modes), chroma, 4:2:0 |
| H2 | Transform | inverse scan, 4×4 integer IDCT, dequant, default scaling |
| H3 | Deblocking | edge strength, luma/chroma filters |
| H4 | P slices | DPB/reference lists, MV prediction, sub-pel interpolation, weighted pred |
| H5 | B slices | bi-prediction, direct modes, reorder/output |
| H6 | CABAC | context modeling — main/high profile |
| H7 | High profile | 8×8 transform, custom scaling matrices |
| H8 | Robustness | multi-slice, ASO, error concealment |

H0–H5 give **H.264 Baseline/Main** — enough for the overwhelming majority of MP4
on the web. H6 opens High profile (what phones record).

### 5.2 AAC-LC (`internal/aac`)

| # | Milestone | Contents |
|---|---|---|
| A0 | Bitstream | raw access units + AudioSpecificConfig (MP4) and ADTS |
| A1 | Spectral | ICS, scalefactors, Huffman codebooks, quant |
| A2 | Filterbank | inverse quant, IMDCT, windowing, overlap-add |
| A3 | Tools | TNS, M/S stereo, intensity stereo, PNS |
| A4 | Coverage | channel configs up to stereo, 44.1/48 kHz |

Versions/features beyond LC (HE-AAC/SBR/PS) are explicitly out of scope initially.

### 5.3 Performance

Pure Go H.264 is the risk. A naive Go baseline decoder will not hit 1080p30.
The plan is:

- block-row parallelism via goroutines (I-frames and P/B deblocking parallelize
  cleanly across slice/MB rows), using all cores;
- hand-written assembly for the two hottest kernels (IDCT and sub-pel motion
  compensation) where profiling demands it;
- a decode-ring decoupled from the UI thread (mirrors the existing pump in
  `video_view.go`).

Target: 1080p30 decode with headroom on a modern laptop using ≤ 4 cores.
**All three landed.** One core alone gives ~59 fps at 1080p (default thread
count ~73 fps, see [Performance](#performance)); the block-row deblock wavefront
and the `avg2` asm kernels measured +20% / +9%.

---

## 6. Testing strategy

- **Oracle diffing.** `cmd/openh264-play` (video) and `cmd/aac-to-wav` (audio)
  produce reference output; the self-written decoders must match plane-exact /
  PCM-within-tolerance. This is the primary correctness gate.
- **Committed vectors.** A handful of tiny MP4s plus expected frame/PCM hashes,
  checked into `testdata`, so tests run with no network and no native library.
- **Conformance streams.** Broad profile/tool coverage clips for regression.

---

## 7. Milestones and timeline

Incremental value ships at every stage; nothing waits for the full decoder.

| Stage | Deliverable | Rough effort |
|---|---|---|
| 1 | Container layer (`internal/mp4x`) + `backend_golang.go` skeleton; **keyframe/thumbnail** via hi264 | 2–3 weeks |
| 2 | H.264 H0–H5 (Baseline/Main) + GL upload; **cross-platform H.264 playback, no cgo** | 6–10 weeks |
| 3 | AAC-LC A0–A4 + `audioout` cgo sink + A/V sync; **complete playback** | 5–8 weeks |
| 4 | H6–H8 (CABAC/High profile) + performance work | 6–10 weeks |
| 5 | Breadth: HEVC/AV1 evaluation, hardware-decode paths | ongoing |

A credible, fully self-owned stack is a **~6–9 month** effort; a usable
cross-platform player (stage 1–3) lands in roughly **3–4 months**.

---

## 8. Risks

| Risk | Severity | Mitigation |
|---|---|---|
| H.264 CABAC complexity | ~~high~~ **done** | CABAC is implemented and FFmpeg-exact |
| Pure-Go decode too slow | ~~high~~ **low** | 1080p ~59 fps single-core, ~73 fps at the default thread count; block-row deblock parallelism + `avg2` asm landed (+20% / +9%); keep OpenH264 as a fallback backend behind the same interface |
| Patents (H.264/AAC) | high, non-technical | explicit commercial decision; self-writing does not remove it |
| Fragmented MP4 / fMP4 playback | ~~medium~~ **done** | `mp4x` assembles the sample table from `moof/traf/trun` (default-base-is-moof and explicit base-data-offset), so fMP4 plays like progressive |
| Audio output portability | ~~medium~~ **done** | `audioout` covers macOS (AudioQueue), Linux (ALSA via `dlopen`) and Windows (WASAPI via syscalls) |

---

## 9. Try the probes

```bash
cd tools/media-poc

# pure Go, no native library
go run ./cmd/keyframe -idr 5   ~/video.mp4 out.png

# full-motion proof (fetch Cisco's binary first)
curl -L -o libopenh264.dylib.bz2 \
  https://ciscobinary.openh264.org/libopenh264-2.6.0-mac-arm64.dylib.bz2
bunzip2 libopenh264.dylib.bz2
go run ./cmd/openh264-play -lib ./libopenh264.dylib -out frames ~/video.mp4

# audio
go run ./cmd/aac-to-wav ~/video.mp4 out.wav
```
