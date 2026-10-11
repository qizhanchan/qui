# Media foundation: a self-owned, pure-Go audio/video stack

Status: **validation complete (2026-10-11); build in progress.** Direction
agreed: the codecs are **written by us in pure Go**; the container layer builds
on `go-mp4`; only audio *output* stays behind a thin cgo seam.

This document records the evidence behind that decision, the target
architecture, and the milestone plan. The runnable probes live in
[`tools/media-poc/`](../tools/media-poc/README.md).

## Implementation progress

| Piece | Location | State |
|---|---|---|
| Container (ISO-BMFF) | `media/internal/mp4x` | ✅ tracks, sample timeline, sample read, keyframes, keyframe seek, avcC/ASC, fMP4 detection — tested |
| H.264 **H0** bitstream | `media/internal/h264` (bits, nal, sps, pps) | ✅ exp-Golomb, RBSP/EBSP, Annex-B split, SPS/PPS incl. High-profile + frame cropping — tested against real SPS bytes |
| H.264 **H1** intra | `media/internal/h264` (frame, transform, slice, cavlc, intra, decoder) | ✅ **decodes real keyframes** — CAVLC residual, intra prediction (**I16x16 and I4x4**, both pixel-exact vs OpenH264 on real content), I_PCM, 4:2:0 chroma, slice-data loop — verified on 6 oracle streams. Remaining: I8x8 |
| H.264 **H3** deblocking | `media/internal/h264` (deblock) | ✅ intra deblocking (strong MB-edge + normal transform-edge filters), slice alpha/beta offsets, `disable_deblocking_filter_idc` 0/1/2 — **pixel-exact vs the reference** with deblocking on or off |
| H.264 **H4** inter | `media/internal/h264` (inter, mc, decoder) | ✅ **P-slice inter prediction** — reference list L0 + DPB, median MV prediction, quarter-pel luma (6-tap) and 1/8-pel chroma motion compensation, `P_Skip` and `P_16x16/16x8/8x16/8x8` partitions, inter residual, weighted prediction, inter deblocking boundary strength — **pixel-exact vs OpenH264** on 2 Baseline multi-frame oracle streams **and a 1280×544 High-profile P frame (0/696320 luma, 0/174080 per chroma plane)**. Remaining: B slices, ref-list reordering |
| H.264 **H5** B slices | `media/internal/h264` (bslice, poc) | 🚧 implemented: POC (types 0/1/2), DPB, L0/L1 construction, bi-prediction, spatial + temporal direct, `B_16x16/16x8/8x16/8x8` and `B_Skip`. **Oracle verification pending** — the OpenH264 Main-profile CAVLC B conformance clip has a non-standard POC/reference structure; the clean I/P/B conformance stream is High profile. On that stream the P-slice **desync is fixed**: the inter `transform_size_8x8_flag` is read *after* coded_block_pattern (not before `sub_mb_type`, matching FFmpeg), so all P macroblocks now parse; weighted-prediction parsing added. The P frame on that stream is now **100% pixel-exact vs OpenH264** after fixing the 2D half-pel filter (`j`: vertical-then-horizontal with an unclipped 16-bit intermediate), the partition-specific MV prediction for 16x8/8x16, and the inter deblocking boundary strength. Remaining: the B-slice MB layer desyncs on this clip (parse issue), then oracle verification |
| H.264 **H7** High profile | `media/internal/h264` (transform8x8, intra, decoder) | 🚧 I_8x8 (all 9 modes, 8x8 inverse transform + dequant) and the inter `transform_size_8x8_flag` implemented — **the I_8x8 IDR and the P frame of a real 1280×544 High-profile CAVLC stream are both pixel-exact vs OpenH264 (0/696320)**. Remaining: custom scaling matrices; the B-slice parse above |
| YUV → RGBA | `media/internal/yuv` | ✅ I420→RGBA (BT.601/BT.709, per-plane strides) plus the I420 GLSL upload shader — tested |
| AAC-LC **A0** bitstream | `media/internal/aac` | ✅ AudioSpecificConfig (MP4 `esds`) and ADTS header parsing, sampling-frequency table, raw access-unit splitting — tested |
| Audio output | `media/internal/audioout` | ✅ the single cgo island: an AudioQueue (macOS) sink behind a small `Sink` interface; CGO-disabled builds return `ErrNotSupported` — builds and tests both ways |

The OpenH264 tool remains the correctness oracle for every decoder milestone
that follows.

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

**One engine change is required:** `media/backend.go`'s `setBackend` is
init-once (backend.go:134). To let the native and pure-Go backends coexist and
be compared, selection must become runtime-driven (build tag or
`QUI_MEDIA_BACKEND=native|golang|auto`).

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
| H.264 CABAC complexity | high | stage 4; ship Baseline/Main first |
| Pure-Go decode too slow | high | parallelism + targeted asm; keep OpenH264 as a fallback backend behind the same interface |
| Patents (H.264/AAC) | high, non-technical | explicit commercial decision; self-writing does not remove it |
| Fragmented MP4 / fMP4 playback | medium | `go-mp4` handles `moof/traf`; extend `mp4x` |
| Audio output portability | medium | isolate in `audioout`; reuse the existing darwin audio code |

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
