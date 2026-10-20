# qui

**English** · [中文](README.zh.md)

`qui` is a retained-mode (not immediate-mode) Go GUI framework with its own rasterizer — a pure-Go CPU reference backend plus an optional OpenGL 3.3 GPU raster backend behind `QUI_GPU_RASTER=1` — over native windowing backends: AppKit on macOS, Xlib on Linux, a `<canvas>` in the browser (wasm), and a headless backend for offscreen rendering. It is a single Go module, `github.com/qizhanchan/qui`; the root `package qui` is the engine.

**Strategic direction: application development happens in (lightweight HTML + CSS) + Go.** The stack for that is:

- `htmlcss` — a small HTML5 + CSS engine that turns markup and stylesheets into qui widget trees, with a retained, restyleable live-element mode (`El` + `StyleEngine`);
- `reactive` — a declarative runtime ("React-shaped API, Solid-shaped engine"): a reconciler plus hooks for structure, and signals for fine-grained updates that skip the render pass entirely;
- `reactive/html` — the DSL gluing the two: components return `h.Div(...)` / `h.Button(...)` trees styled by plain CSS classes.

`go run ./examples/reactive-html` is the canonical demo of this direction. The native `widgets` package increasingly serves as the rendering/control primitive layer underneath this surface (see the backing-primitives contract below) while remaining usable directly.

## Documentation

| Document | Contents |
|---|---|
| [docs/overview.md](docs/overview.md) | What qui is, the problem it targets, design principles, project status |
| [docs/architecture.md](docs/architecture.md) | Engine internals: frame loop, widget tree, event dispatch, layout, rendering, text, theme, i18n, platform seam, introspection |
| [docs/features.md](docs/features.md) | Feature catalog across the engine, widgets, htmlcss, reactive, i18n and the auxiliary packages |
| [reactive/README.md](reactive/README.md) | Deep dive into the reactive runtime |
| [htmlcss/COVERAGE.md](htmlcss/COVERAGE.md) | The authoritative HTML/CSS coverage matrix |
| [agent/llm.txt](agent/llm.txt) | The agent wire spec (also served live at `/llm.txt`) |

Chinese versions of the top-level documents live alongside the English ones with a `.zh.md` suffix.

## Project status

- **Module:** `github.com/qizhanchan/qui` (Go 1.25).
- **Platforms:** macOS, Linux (X11) and the web (GOOS=js/wasm) are the priority targets, each with a native windowing backend; Windows still runs on GLFW. Native dialogs, emoji, spell checking and system-tray integration are macOS-only today.
- **Rendering:** the CPU rasterizer is the default and is pure Go; the GPU raster backend (`QUI_GPU_RASTER=1`) is opt-in. Both sit behind the same `Canvas` / `RasterBackend` seam.
- **Windowing:** native backends by default (`cocoa` / `x11` / `web`), `headless` everywhere; GLFW is opt-in (`-tags qui_glfw`) and the default only on Windows. See the backend table below.
- **Not a complete browser and not a complete design system.** `htmlcss` is a deliberate subset, and qui ships one light theme of design tokens — a designed look belongs in a stylesheet.

## Quick start

```go
package main

import (
	"log"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

func main() {
	app, err := qui.NewApp()
	if err != nil {
		log.Fatal(err)
	}
	window, err := app.NewWindow("Hello", 480, 320)
	if err != nil {
		log.Fatal(err)
	}
	window.SetRenderer(qui.NewGLRenderer())

	root := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Vertical, Gap: 12},
		widgets.NewLabel("Hello, qui."),
		widgets.NewButton("Quit", func() { app.CloseWindow(window) }),
	)
	window.SetRoot(root)

	app.Run()
}
```

The HTML/CSS surface looks like this (from `examples/reactive-html`):

```go
rt := h.Mount(window, css, func() h.Node {
	n, set := reactive.UseState(0)
	return h.Div(
		h.H1("Counter"),
		h.Button(fmt.Sprintf("count: %d", n)).OnClick(func() { set(n + 1) }),
	)
})
```

## Package map

| Package | Role |
|---|---|
| root `qui` | Engine: window/frame loop, 3-phase event dispatch, layout engines (Flex/Grid/Flow/Absolute), Canvas + RasterBackend rendering, Path/Paint/Shape, SaveLayer/blend/shader/filters, theme tokens, Unicode-shaped text + inline flow (IFC), cross-widget text selection, and the always-on AI-introspection surface (AX tree, selectors, snapshots, actions, waits, event recording, main-thread job queue) |
| `internal/platform/*` | Windowing backends (`cocoa`, `x11`, `web`, `headless`, `glfw`) behind `contract`, the seam they implement; `darwinkeys` holds the macOS keycode tables shared with root's AppKit bridges |
| `widgets` | Native widgets: Box, Label, InlineBox, RichText, Anchor, Button, Input, TextArea, CheckBox, RadioButton/Group, Switch, Slider, Progress, Select, ScrollView, ListView, TableView, TabView, Popup, Dialog, MenuBar/ContextMenu, Image, Rule, FieldSet. Also `undo.go` (text-widget undo history) |
| `htmlcss` | HTML + CSS engine (details below) |
| `reactive`, `reactive/html` | Declarative runtime + web-like DSL (details below) |
| `icons` | Optional icon set: ~135 curated 24px outlined glyphs (Material Symbols, Apache 2.0) parsed into `*svg.Document`, usable anywhere a `qui.VectorSource` is accepted. Asset-only — nothing in root or `widgets` depends on it |
| `agent` + `cmd/qui-agent` | HTTP/SSE wire layer over root's introspection primitives (UDS by default, TCP + bearer opt-in) and a CLI client for it. Keep `agent` thin — every primitive lives in root |
| `scene3d` | 3D math + scene graph + `Viewport` widget (offscreen FBO composited via `QueueGLDraw`); `Vec3Field` integrates with widgets, so `scene3d` may import `widgets` |
| `anim` | `Tween[T]` / `Spring` / `Timeline` + easing catalog; all satisfy `qui.Animator` |
| `graphs` | Stable scale/data primitives plus two native solutions: `graphs/chart` (ECharts-shaped declarative charting) and `graphs/diagram` (draw.io-class rendering/editing with pages, layers, groups, routing, undo/redo, AX, JSON/draw.io XML and PNG/SVG/PDF export) |
| `svg` | SVG 1.1 subset: parse / build programmatically / rasterize (with tint) / serialize; `Document` satisfies `qui.VectorSource` |
| `media` | `AudioPlayer` (service) + `VideoView` / `FrameView` (widgets) without ffmpeg; two backends behind an internal interface — native AVFoundation (`native`, macOS) and a pure-Go codec stack (`golang`), selected by `QUI_MEDIA_BACKEND` |
| `webview` | CEF-based embedded browser widget, gated behind the `webview_cef` build tag + fetched SDK; the default build compiles without CEF. The JS↔Go bridge is JSON-only. See `webview/scripts/` for the .app packaging workflow |
| `physics`, `physics/p2`, `physics/p3` | Game physics: dimension-independent vocabulary in root; 2D and 3D engines are API mirrors (swept AABB, spatial hash, contact flags, one-way platforms, sensors). Zero dependencies — not even root `qui` |
| `i18n` + `cmd/qui-i18n` | Message catalogs (JSON, CLDR plurals) + locale-sensitive formatting (number / currency / date / relative time / list / collation), plugged into root through the `qui.Translator` seam. The CLI extracts keys from source, lints catalogs, and generates a pseudo-locale for layout testing |
| `fonts/jetbrainsmono` | Embedded typeface; `jetbrainsmono.Use()` swaps the global default font family (all weights) |

**Strict import direction:** subpackages import root `qui`; root must NEVER import a subpackage. Root↔subpackage coupling goes through interfaces defined in root (`qui.Animator`, `qui.IMEClient`, `qui.VectorSource`, `qui.Translator`, focusable/tickable contracts). The one exception: root imports the platform backends under `internal/platform/*` (blank imports in `platform_backends*.go` only, plus `internal/platform/contract` and `darwinkeys`). Those are leaf packages that import nothing from root — the compiler enforces it, since root imports them. Allowed cross-subpackage edges: `scene3d`→`widgets`, `htmlcss`→`widgets`+`svg`, `reactive/html`→`reactive`+`htmlcss`+`widgets`, `icons`→`svg`. `widgets` imports NOTHING but root — the menu's check/chevron marks are drawn geometrically rather than pulled from `icons`. (`i18n` imports ONLY root + `golang.org/x/text`; every other package reaches translations through `qui.Translate`, never by importing `i18n`.) **`reactive` itself imports ONLY root — it is backend-agnostic; keep it that way.** Everything else stays isolated. If root seems to need a subpackage symbol, move the symbol or define an interface in root.

## Commands

macOS needs only Xcode's command-line tools. Linux needs `libx11-dev` (plus `libgl1-mesa-dev` for the GL renderer, or build with `-tags qui_nogl`); `CGO_ENABLED=0` builds need nothing and get the headless backend. GLFW (`-tags qui_glfw`, and Windows) needs its usual native headers.

```bash
go build ./...                              # compile everything
go test ./...                               # all tests
go test ./graphs/...                        # chart + diagram solutions
go test ./htmlcss ./reactive/...            # single packages
go test -run TestName ./widgets             # single test
go vet ./...

# The app-dev surface
go run ./examples/reactive-html             # reactive + htmlcss + h DSL (canonical)
go run ./examples/html-css                  # one-shot htmlcss.Render showcase
go run ./examples/chrome-tabs               # browser-style tab strip IN the title bar
go run ./examples/i18n                      # 7 languages switched at runtime, CLDR plurals, RTL

go run ./examples/overlay-panel                     # non-activating transparent panel (cocoa)

# Offscreen rendering check — no display, no GPU, works in CI/containers
QUI_PLATFORM=headless QUI_SNAPSHOT=/tmp/ui.png go run ./examples/reactive-html

# The same app in the browser (see examples/web/README.md)
GOOS=js GOARCH=wasm go build -o examples/web/app.wasm ./examples/reactive-html

# Other examples: 3dviewer animation customgl debug-layout dialog filedialog
# flowchart graphs layouts listview media-audio media-video multiwindow
# multiwindow-sessions popup svg systray text textarea textfield theme
# webview (webview needs the CEF build — see webview/scripts)

# Drive a running app from outside (AI-native operability)
QUI_AGENT=1 go run ./examples/reactive-html &
go run ./cmd/qui-agent tree                 # discovers the socket; also:
go run ./cmd/qui-agent click '[role=button][name="New"]'
go run ./cmd/qui-agent shot /tmp/ui.png
# or raw: curl --unix-socket $TMPDIR/qui-agent-*.sock http://./llm.txt
```

Useful env vars: `QUI_PLATFORM=cocoa|x11|web|headless|glfw` (windowing backend — see below), `QUI_SNAPSHOT=out.png` (render, wait for the UI to settle, write the frame, exit; `QUI_SNAPSHOT_SETTLE` / `QUI_SNAPSHOT_TIMEOUT` tune it), `QUI_HEADLESS_SCALE=2` (headless display scale), `QUI_X11_SCALE` (override Xft.dpi-derived scale), `QUI_GPU_RASTER=1` (GPU raster backend), `QUI_GLYPH_CACHE=0` (disable the glyph coverage-mask cache; `exact` keys sub-pixel x exactly), `QUI_AGENT=1` / `QUI_AGENT_TCP=:port` / `QUI_AGENT_TOKEN` / `QUI_AGENT_SOCK` (agent server), `QUI_DEBUG_LAYOUT=1` (flex overflow logging), `QUI_DEBUG_PAINT=1` (log full-repaint promotions + first moved widget), `QUI_DEBUG_DRAW=1` (fail-fast when Draw mutates the widget tree or invalidates layout), `QUI_DEBUG_LAYOUT_CACHE=1` (re-measure every measure-cache hit and log widgets whose size changed without `InvalidateLayout` on themselves), `QUI_DEBUG_THREAD=1` (fail-fast UI-thread ownership checks), `QUI_DEBUG_WIDGETS=1` (log children whose Parent() mismatches — a missing `SetSelf`), `QUI_DEBUG_GESTURE=1` (log which gesture selectors the macOS bridge installed, incl. whether GLFW's scrollWheel: was preserved), `QUI_I18N_STRICT=1` (render unresolved message keys as ⟦key⟧ and log each once), `QUI_GOLDEN=1` (write golden snapshots), `QUI_HTMLCSS_SNAPSHOT=1` (htmlcss snapshot PNGs).

**Windowing backends (`QUI_PLATFORM`).** The OS windowing layer sits behind the platform seam. Each backend is a leaf package under `internal/platform/` implementing `internal/platform/contract` (which documents the seam); `platform_adapter.go` wraps it for the engine. Several can be compiled in; the highest-priority one is the default and `QUI_PLATFORM` picks another at *runtime*:

| Value | Package | Default on | Backend |
|---|---|---|---|
| `cocoa` | `internal/platform/cocoa` | macOS | Native AppKit — our own NSApplication event pump, NSWindow/NSView, gestures as real responder methods. `GLRenderer` windows swap an NSOpenGLContext; `CPURenderer` windows (incl. all `-tags qui_nogl` builds) present as CALayer contents, paced by a CVDisplayLink |
| `x11` | `internal/platform/x11` | Linux (cgo) | Xlib — XPutImage presentation of CPU frames, XIM text input, CLIPBOARD selection, Xft.dpi scale |
| `web` | `internal/platform/web` | GOOS=js | `<canvas>` + putImageData of the damaged rect, paced by requestAnimationFrame; keys from keydown, text + IME preedit from a hidden textarea anchored at the caret; glyphs the embedded fonts lack (CJK…) and color emoji drawn with the browser's own fonts; file drop via `Window.OpenDroppedFile`; Cmd shortcuts on Mac browsers |
| `headless` | `internal/platform/headless` | CGO_ENABLED=0 | In-memory windows; frames read back via `Window.Snapshot` / agent `shot` / `QUI_SNAPSHOT` |
| `glfw` | `internal/platform/glfw` | Windows | GLFW 3.3; elsewhere only with `-tags qui_glfw` |

Surfaces without a GL context (`x11`, `web`, `headless`) present CPU frames directly (`platformSurface.presentCPU`), as does `cocoa` for any window running `CPURenderer` (a `GLRenderer` window always presents by buffer swap, since its CPU image lacks raw-GL content); a `GLRenderer` handed to such a window is swapped for `CPURenderer`, so desktop app code runs unchanged. Builds without cgo, for wasm, or with `-tags qui_nogl` contain no OpenGL at all (`GLRenderer` is then an alias of `CPURenderer`). An unrecognized name is an error listing what's available — a typo shouldn't look like "nothing changed".

**Process lifecycle for shell-driven testing:** `go run` does NOT forward signals to the spawned binary — always `go build -o /tmp/app ./... && /tmp/app &` when you need to `kill` the real host process. `App.Run` traps SIGINT/SIGTERM through the normal close path (agent socket unlinked, `OnClose` callbacks run).

## Engine architecture

Single-threaded loop pinned to the OS main thread. Each `Window.Step()` drains the job queue, ticks animators, re-lays-out if something is layout-dirty, and — **only if the dirty region is non-empty** — clears and redraws that region. Widget trees, focus, overlays, layout metadata and rendering state belong to that UI goroutine; cross-goroutine code goes through `Window.PostJob` / `TryPostJob` / `PostPriorityJob`, and read-side agent code uses the `*Synced` APIs.

Rendering is Skia-shaped and two-layered: a `Canvas` frontend (state stack, `Path`, `Paint`, shaders, filters, `SaveLayer`) over a `RasterBackend` that produces pixels (`backend_cpu.go` reference, `backend_gpu.go` opt-in). Text uses `go-text/typesetting` glyph-run shaping with OpenType GSUB/GPOS, Unicode bidi, UAX #14 line breaking and UAX #29 grapheme boundaries; measurement, CPU/GL drawing, PDF output, wrapping, hit testing, selection and editor carets all share the same shaped clusters.

Layout engines are `FlexLayout`, `GridLayout`, `FlowLayout` and `AbsoluteLayout`. Two CSS-matching rules bite if unknown: `Grow > 0` with unset `Basis` implies `Basis = 0`, and `Shrink` defaults to 1 (opt out with `NoShrink: true`).

See [docs/architecture.md](docs/architecture.md) for the full treatment: frame loop, event dispatch and coordinate spaces, viewport zoom, the theme, i18n, platform bridges, and the AI-native introspection surface.

## App-dev surface: htmlcss + reactive

`htmlcss` parses HTML + CSS into a widget tree through one assembly, the live `El`. It supports the full selector set (including interactive states), `var()` + `:root`, shorthands, the box model with per-side borders, shadow/gradient/opacity/transform, `position:relative`, overflow, `display:flex/grid`, list markers, text-decoration/transform/overflow, and white-space. Restyle is subtree-scoped. Inline text folds into Unicode-shaped runs inside `widgets.InlineBox`, so wrapped text still participates in cross-widget selection.

`reactive` runs two engines on one runtime: a **reconciler** for structure (`UseState`, keyed lists) and a **signal** engine for high-frequency values (`Signal.Set` calls a widget setter directly and skips the render pass). `reactive/html` is the fluent DSL — every tag is `h.Tag(...any) *Builder`, and components can also be written as HTML fragments compiled with `h.MustParse` and filled per render through a flat `Scope`. `h.Mount(window, css, app)` wires a per-window style engine.

See [docs/features.md](docs/features.md) for the feature catalog and [reactive/README.md](reactive/README.md) for the reactive runtime deep dive.

## AI-native introspection + operability

Every qui app has a built-in accessibility + operability surface: read the widget tree (role / name / value / bounds) and dispatch real click / type / scroll events by selector — from inside the process or over HTTP + SSE via `agent.BindEnv(window)`. The `agent` server is UDS by default (TCP + bearer opt-in) and serves the full wire spec at `/llm.txt`.

```go
import "github.com/qizhanchan/qui/agent"

if srv, err := agent.BindEnv(window); err == nil && srv != nil {
	window.OnClose(func() { _ = srv.Stop() })
}
```

Run with `QUI_AGENT=1` and drive it with `cmd/qui-agent` (`tree`, `click`, `type`, `wait`, `shot`, …). `Cmd/Ctrl+Shift+A` toggles an in-window overlay that labels every widget with its ID / role / name in ANY qui app, with no code change.

## Conventions

- **Where new code goes:** engine changes → root (only when it affects the engine, not one widget). New widget → `widgets/` (file named after the widget). CSS features → `htmlcss` (implement in the shared appliers so BOTH paths get it; add both an engine test and, when visual, a snapshot test).
- **User-facing strings:** never hard-code one in `widgets/`, `htmlcss` or the engine. Use `qui.TOr(key, literal)` so an app without catalogs is unaffected, and give any new text-bearing widget a `TextKey`-style field resolved in Measure/Draw (see `widgets/i18n.go`). Widgets whose caption comes from a key must implement `AccessibleNameKey()` — otherwise `[key=]` selectors silently stop covering them.
- **Locale-dependent caches:** anything memoizing measured or shaped text must include `LocaleGeneration()` in its key, exactly like `FontRegistryGeneration()`. Reactive runtime → `reactive`; DSL surface → `reactive/html`. AX overrides → each subpackage's `accessibility.go`. HTTP endpoints → `agent/` (primitives themselves go in root). Platform code follows the `_darwin.go`/`_other.go` split — never spread darwin code across a tagged and a generic file. Windowing goes in a backend under `internal/platform/` — never in root; if the engine needs something new from the OS, add it to `internal/platform/contract` and implement it in every backend.
- `widgets/` files use `import . "github.com/qizhanchan/qui"` (dot import) — the one blessed place; qualify normally everywhere else. The dot import does NOT bypass unexported fields: use accessors (`Bounds()`, `Style()`, `Enabled()`).
- Container subclasses must call `SetSelf` after construction.
- Tests use root `testhelpers.go` fixtures: `NewTestWindow`, `NewMouseEvent`/`NewKeyEvent`/`NewCharEvent`/`NewScrollEvent`, `RecordingCanvas`, `NewImageCanvas`, `DrainJobsForTest`. Test-mode windows run actions inline (no frame pump needed).
- Widgets that animate implement `Tickable`; text input implements `IMEClient`; focusables implement `Focusable() bool` + `SetFocused(bool)`.
- Dependencies: `go-gl/gl` (GL renderer, cgo builds), `go-gl/glfw` (Windows / `-tags qui_glfw` only), `go-text/typesetting`, `srwiley/rasterx`, `golang.org/x/{image,net,sys,text}`.

`CLAUDE.md` and `AGENTS.md` are symlinks to this file, so coding agents read the same front page.

## License

Released under the MIT License — see [LICENSE](LICENSE).

```text
MIT License

Copyright (c) 2026 qizhanchan

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```
