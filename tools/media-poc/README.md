# media-poc

Self-contained probe tools for the **media foundation** work (see
[`docs/media-foundation.md`](../../docs/media-foundation.md)). They answer the
question *"can a qui media backend be built without ffmpeg, and what does the
pure-Go part cost?"* with runnable code.

This directory is a **separate Go module** (its own `go.mod`), so it is excluded
from the root module's `go build ./...` / `go test ./...` and does not touch the
root `go.mod` or `vendor/`.

## Tools

| Command | What it proves |
|---|---|
| `cmd/keyframe` | Demux an MP4 + decode an **IDR keyframe** with pure-Go `hi264` → PNG. Fully pure Go, MIT, zero native deps. Thumbnails only — cannot play. |
| `cmd/openh264-play` | Demux + decode **every frame (I/P/B)** via Cisco **OpenH264 loaded with purego** (no cgo, no ffmpeg) → PNGs + fps. The correctness oracle for the self-written decoder. |
| `cmd/aac-to-wav` | Demux the **AAC** track + decode to PCM with a pure-Go AAC-LC decoder → `.wav`. Oracle for the self-written AAC decoder. |
| `cmd/oh264-vectors` | **Encode** a small Baseline/CAVLC stream with inter (P) frames using OpenH264's encoder, then decode it with OpenH264's own decoder and print per-frame Y/Cb/Cr SHA-256 hashes. The oracle for the self-written decoder's H4/H5 inter milestones (hi264 only emits I-frames). `-noise` produces a high-detail pattern that forces I4x4 intra. |
| `cmd/oh264-hashes` | Decode a **raw Annex-B `.264`** stream with OpenH264 and print per-frame Y/Cb/Cr SHA-256 hashes (`-dump` writes raw planes). The oracle for conformance streams that are not packaged in an MP4. |

## Run

```bash
# keyframe (device-independent; needs no native library)
go run ./cmd/keyframe -idr 5   input.mp4 out.png
go run ./cmd/keyframe -bench 20 input.mp4

# full-motion playback proof; fetch Cisco's official binary first
#   https://ciscobinary.openh264.org/libopenh264-2.6.0-mac-arm64.dylib.bz2
go run ./cmd/openh264-play -lib ./libopenh264.dylib -out frames -every 2000 input.mp4

# audio
go run ./cmd/aac-to-wav input.mp4 out.wav

# inter-frame oracle vectors (needs the OpenH264 2.4.1 binary matching the binding)
go run ./cmd/oh264-vectors -lib ./libopenh264-2.4.1.dylib -w 64 -h 48 -frames 4 -qp 26 \
    -out ../../media/internal/h264/testdata/baseline_p_64x48.h264
```

## Measured (2026-10, Apple Silicon)

| Path | Result |
|---|---|
| OpenH264 full decode, 1920×1080 | 7931 frames / 26.6 s ≈ **298 fps (~9× realtime)**, 3.4 ms/frame |
| hi264 IDR decode, 1920×1080 | ~24 ms/frame (keyframes only) |
| pure-Go AAC-LC, 44.1 kHz stereo | 11389 frames → 264.45 s PCM, duration matches container exactly |

## Licenses of the dependencies used here

| Module | License |
|---|---|
| `github.com/abema/go-mp4` | MIT |
| `github.com/y9o/go-openh264` (binding only) | MIT |
| Cisco OpenH264 **binary** | BSD **+ Cisco binary license conditions** (see docs) |
| `github.com/Eyevinn/hi264` | MIT |
| `github.com/skrashevich/go-aac` | **LGPL-3.0** |

The licensing analysis is the whole reason the plan is to **write our own**
decoders; see [`docs/media-foundation.md`](../../docs/media-foundation.md).
