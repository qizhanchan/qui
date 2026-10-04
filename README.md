# README.md
CLAUDE.md and AGENTS.md are symbol link or README.md

# What qui is

`qui` is a retained-mode (not immediate-mode) Go GUI framework built on GLFW 3.3 + OpenGL 3.3 Core with its own rasterizer — a CPU reference backend, plus an optional GPU raster backend behind `QUI_GPU_RASTER=1`. Single Go module `github.com/qizhanchan/qui`; root `package qui` is the engine.

**Strategic direction: application development happens in (lightweight HTML + CSS) + Go.** The stack for that is:

- `htmlcss` — a small HTML5+CSS engine that turns markup + stylesheets into qui widget trees, with a retained, restyleable live-element mode (`El` + `StyleEngine`);
- `reactive` — a declarative runtime ("React-shaped API, Solid-shaped engine"): reconciler + hooks for structure, signals for fine-grained updates that skip the render pass entirely;
- `reactive/html` — the DSL gluing the two: components return `h.Div(...)` / `h.Button(...)` trees styled by plain CSS classes.

`go run ./examples/reactive-html` is the canonical demo of this direction. The native `widgets` package increasingly serves as the rendering/control primitive layer underneath this surface (see the backing-primitives contract below) while remaining usable directly.

# Package map

| Package | Role |
|---|---|
| root `qui` | Engine: window/frame loop, 3-phase event dispatch, layout engines (Flex/Grid/Flow/Absolute), Canvas + RasterBackend rendering, Path/Paint/Shape, SaveLayer/blend/shader/filters, theme tokens, Unicode-shaped text + inline flow (IFC), cross-widget text selection, and the always-on AI-introspection surface (AX tree, selectors, snapshots, actions, waits, event recording, main-thread job queue) |
| `apps` | 包含真实的工业例子，用于验证 qui 核心功能是否足够健壮。如果你开发 apps 的时候，遇到 qui 一些功能问题，可以直接改 root qui 的逻辑|
| `widgets` | Native widgets: Box, Label, InlineBox, RichText, Anchor, Button, Input, TextArea, CheckBox, RadioButton/Group, Switch, Slider, Progress, Select, ScrollView, ListView, TableView, TabView, Popup, Dialog, MenuBar/ContextMenu, Image, Rule, FieldSet. Also `undo.go` (text-widget undo history) |
| `htmlcss` | HTML+CSS engine (details below) |
| `reactive`, `reactive/html` | Declarative runtime + web-like DSL (details below) |
| `icons` | Optional icon set: ~135 curated 24px outlined glyphs (Material Symbols, Apache 2.0) parsed into `*svg.Document`, usable anywhere a `qui.VectorSource` is accepted. Asset-only — nothing in root or `widgets` depends on it |
| `agent` + `cmd/qui-agent` | HTTP/SSE wire layer over root's introspection primitives (UDS by default, TCP+bearer opt-in) and a CLI client for it. Keep `agent` thin — every primitive lives in root |
| `scene3d` | 3D math + scene graph + `Viewport` widget (offscreen FBO composited via `QueueGLDraw`); `Vec3Field` integrates with widgets, so scene3d may import widgets |
| `anim` | `Tween[T]` / `Spring` / `Timeline` + easing catalog; all satisfy `qui.Animator` |
| `graphs` | 2D charting: Line/Scatter/Bar/Area/Pie series, ValueAxis/CategoryAxis, Legend, pan/zoom |
| `svg` | SVG 1.1 subset: parse / build programmatically / rasterize (with tint) / serialize; `Document` satisfies `qui.VectorSource` |
| `media` | `AudioPlayer` (service) + `VideoView` (widget) without ffmpeg; platform backends behind an internal interface (darwin only in v1, others return `ErrNotSupported`) |
| `webview` | CEF-based embedded browser widget, gated behind the `webview_cef` build tag + fetched SDK; default build compiles without CEF. JS↔Go bridge is JSON-only. See `webview/` scripts + HANDOFF.md for the .app packaging workflow |
| `physics`, `physics/p2`, `physics/p3` | Game physics: dimension-independent vocabulary in root; 2D and 3D engines are API mirrors (swept AABB, spatial hash, contact flags, one-way platforms, sensors). Zero dependencies — not even root `qui` |
| `i18n` + `cmd/qui-i18n` | Message catalogs (JSON, CLDR plurals) + locale-sensitive formatting (number / currency / date / relative time / list / collation), plugged into root through the `qui.Translator` seam. The CLI extracts keys from source, lints catalogs, and generates a pseudo-locale for layout testing |
| `fonts/jetbrainsmono` | Embedded typeface; `jetbrainsmono.Use()` swaps the global default font family (all weights) |

**Strict import direction:** subpackages import root `qui`; root must NEVER import a subpackage. Root↔subpackage coupling goes through interfaces defined in root (`qui.Animator`, `qui.IMEClient`, `qui.VectorSource`, `qui.Translator`, focusable/tickable contracts). Allowed cross-subpackage edges: `scene3d`→`widgets`, `htmlcss`→`widgets`+`svg`, `reactive/html`→`reactive`+`htmlcss`+`widgets`, `icons`→`svg`. `widgets` imports NOTHING but root — the menu's check/chevron marks are drawn geometrically rather than pulled from `icons`. (`i18n` imports ONLY root + `golang.org/x/text`; every other package reaches translations through `qui.Translate`, never by importing `i18n`.) **`reactive` itself imports ONLY root — it is backend-agnostic; keep it that way.** Everything else stays isolated. If root seems to need a subpackage symbol, move the symbol or define an interface in root.

## Commands

GLFW needs native headers (`brew install glfw` on macOS; X11/Wayland dev packages on Linux).

```bash
go build ./...                              # compile everything
go test ./...                               # all tests
go test ./htmlcss ./reactive/...            # single packages
go test -run TestName ./widgets             # single test
go vet ./...

# The app-dev surface
go run ./examples/reactive-html             # reactive + htmlcss + h DSL (canonical)
go run ./examples/html-css                  # one-shot htmlcss.Render showcase
go run ./examples/chrome-tabs               # browser-style tab strip IN the title bar
go run ./examples/i18n                      # 7 languages switched at runtime, CLDR plurals, RTL

QUI_PLATFORM=cocoa go run ./examples/overlay-panel   # non-activating transparent panel

# Other examples: 3dviewer animation customgl debug-layout dialog filedialog
# flowchart graphs layouts listview media-audio
# media-video multiwindow popup svg systray text textarea textfield theme
# webview (webview needs the CEF build — see webview/ scripts + HANDOFF)

# Drive a running app from outside (AI-native operability)
QUI_AGENT=1 go run ./examples/reactive-html &
go run ./cmd/qui-agent tree                 # discovers the socket; also:
go run ./cmd/qui-agent act click '[role=button][name="New"]'
go run ./cmd/qui-agent screenshot /tmp/ui.png
# or raw: curl --unix-socket $TMPDIR/qui-agent-*.sock http://./llm.txt
```

Useful env vars: `QUI_PLATFORM=glfw|cocoa` (windowing backend — see below), `QUI_GPU_RASTER=1` (GPU raster backend), `QUI_AGENT=1` / `QUI_AGENT_TCP=:port` / `QUI_AGENT_TOKEN` / `QUI_AGENT_SOCK` (agent server), `QUI_DEBUG_LAYOUT=1` (flex overflow logging), `QUI_DEBUG_PAINT=1` (log full-repaint promotions + first moved widget), `QUI_DEBUG_THREAD=1` (fail-fast UI-thread ownership checks), `QUI_DEBUG_GESTURE=1` (log which gesture selectors the macOS bridge installed, incl. whether GLFW's scrollWheel: was preserved), `QUI_I18N_STRICT=1` (render unresolved message keys as ⟦key⟧ and log each once), `QUI_GOLDEN=1` (write golden snapshots), `QUI_HTMLCSS_SNAPSHOT=1` (htmlcss snapshot PNGs).

**Windowing backends (`QUI_PLATFORM`).** The OS windowing layer sits behind the platform seam (`platform.go`) and more than one implementation is compiled in, selected at *runtime*:

| Value | Backend |
|---|---|
| `glfw` (default) | GLFW 3.3 + a macOS bridge that swizzles in gestures and exact scroll modifiers |
| `cocoa` (darwin) | Native AppKit — our own NSApplication event pump, NSWindow/NSView, gestures as real responder methods |

Both produce identical geometry (verified numerically, including multi-monitor coordinates, maximize/restore and DPR), so switching is a comparison rather than a migration:

```bash
QUI_PLATFORM=cocoa go run ./examples/reactive-html
```

The default stays on the most proven backend, not the newest. An unrecognized name is an error listing what's available — a typo shouldn't look like "nothing changed". See `docs/backend-abstraction-plan.md`.

**Process lifecycle for shell-driven testing:** `go run` does NOT forward signals to the spawned binary — always `go build -o /tmp/app ./... && /tmp/app &` when you need to `kill` the real GLFW host. `App.Run` traps SIGINT/SIGTERM through the normal close path (agent socket unlinked, `OnClose` callbacks run).

## Engine architecture (root)

### Frame loop (app.go → window.go)

Single-threaded loop pinned to the OS main thread (`runtime.LockOSThread`). `glfw.WaitEventsTimeout(1/60)` blocks on input but wakes at 60 Hz for animators. Each `Window.Step()`: drain PostJob queue → `Tick(now)` on Tickables (returned Rects union into `dirtyRegion`) → re-layout if `root.IsLayoutDirty()` → **if dirtyRegion is empty, return without painting** → clear + redraw only the dirty region → SwapBuffers.

Widget trees, focus, overlays, layout metadata, rendering state, and direct introspection belong to that UI goroutine. Cross-goroutine code must use `Window.PostJob` / `TryPostJob` / `PostPriorityJob`; read-side agent code uses the `*Synced` APIs. `QUI_DEBUG_THREAD=1` enables fail-fast checks for production windows, and `Window.EnableUIThreadChecks` enables them selectively in tests or custom hosts. Detached widgets may be constructed off-thread, but once attached their state is UI-owned.

The process owns exactly one `App` and one platform backend. A second `NewApp` returns `ErrAppAlreadyExists`. The backend stays initialized until process exit; `App.Run` destroys app-owned windows/status items but never terminates process-global platform state. The same `App` may create another set of windows and run again after a previous run closes all windows.

Key invariant: **nothing paints unless something invalidated.** `Invalidate`/`InvalidateRect` for pure visual changes; `InvalidateLayout` for size-affecting changes.

Repaint scoping for layout changes: `InvalidateLayout` dirty-rects only the CALLER's paint extent (not the window) and bubbles a flag to the root. The layout pass then promotes to a full repaint only if some widget's bounds actually changed (`BaseWidget.Layout` → `noteBoundsChange`; sub-pixel jitter under `boundsEpsilon` stays scoped, and out-of-pass Layout calls invalidate old+new extents directly). A text tick that measures to the same size repaints just its own rect — the window genuinely idles between updates, so `WaitIdle` works against continuously-updating apps. Debug promotions with `QUI_DEBUG_PAINT=1` (logs the first moved widget). Text measurement is memoized (`BuildTextLayout` cache in text.go, invalidated on font-registry changes), so the full-tree relayout that any layout invalidation triggers costs map lookups for unchanged text, not re-measurement.

### Widget tree & `BaseWidget.self`

All widgets implement `Widget` (widget.go); `BaseWidget` provides defaults and is embedded everywhere. Go embedding has no virtual dispatch, so `BaseWidget` carries a `self Widget` field set via `SetSelf(outer)` right after construction — **always call `SetSelf` after constructing a Container subclass** or event dispatch misses ancestors. `Container` is the sole tree-composition type; children are exposed to framework walks via `ChildList()`.

### Event dispatch (event.go + window.go)

DOM-style three-phase dispatch (capture → target → bubble) with `StopPropagation`; returning `true` from `Handle` is implicit stop. Additional rules: mouse capture on MouseDown (Move/Up route to the captured widget), hover enter/leave synthesis via path diffing, focus updates on MouseDown + Tab cycling, drag/drop synthesized with a 4px dead-zone, overlays hit-test top-down and `Modal()` overlays trap focus.

**Coordinate spaces.** Every widget receives mouse/gesture/drag events in ITS OWN coordinate space — the one its `Bounds()` live in — so it can always compare event coords against its own geometry (`eventInWidgetSpace` in event.go; identity for untransformed trees). A widget's own space differs from window space when an ancestor transforms it: a CSS `transform` (`InteractionTransformer`, the widget's own box moves) or a scroll container (`ChildInteractionTransformer`, descendants move but the container's box doesn't — `widgets.ScrollView` lays its content out ONCE at the viewport origin and scrolls by transform, so scrolling is O(1) and child bounds are scroll-independent). Cross the boundary with `WindowPointToLocal` / `LocalPointToWindow` / `InteractionBoundsOf` (`widget.go`); code that hands geometry to something positioned in window space — overlay/popup anchors, IME caret rect, tooltip anchors, AX + agent bounds — must use the on-screen form, never raw `Bounds()`. Descendant invalidation maps out through `ChildPaintTransformer` and is clipped by `PaintClipper`, so a scrolled-out widget dirties nothing.

### Rendering

Skia-shaped, two layers:

- **Canvas frontend** (`render.go`, `canvas_state.go`, `canvas_paint.go`, `canvas_layer.go`, `path.go`): Save/Restore state stack (clip + 2×3 affine matrix), `DrawShape(Shape, Paint)` as the single entry point (convenience methods are wrappers), `Path` with quad/cubic Béziers, AA winding/even-odd fill, stroke cap/join/miter, `SaveLayer` + blend modes + `Paint.Alpha`, `ClipPath` (mask at Restore), `Paint.Shader` (linear/radial gradients), `ColorFilter`/`ImageFilter` (drop shadow, blur), `DrawShadow` for elevation shadows.
- **RasterBackend** (`backend.go`): the pixel-producing abstraction below the frontend — all coordinates already physical, every method receives a clip it must honor. `backend_cpu.go` is the reference; `backend_gpu.go` (FBO + shaders, `QUI_GPU_RASTER=1`) swaps into the same slot and interleaves with the CPU path per primitive. `renderer_gl.go` owns the GL window blit.

Text uses `go-text/typesetting` glyph-run shaping (`text_shaping.go`) with OpenType GSUB/GPOS, Unicode bidi, UAX #14 line breaking and UAX #29 grapheme boundaries. Measurement, CPU/GL drawing, PDF output, wrapping, hit testing, selection and editor carets share the same shaped clusters; `RuneAdvance` remains only a compatibility helper for standalone-rune utilities. Styled runs preserve shaping context across paint-only boundaries. Inline flow (text + atomic inline boxes sharing lines with baseline alignment) lives in `inline.go` and is surfaced as `widgets.InlineBox`. GL widgets composite via `GPUCanvas.QueueGLDraw` + `ActiveGLRenderer().DrawTexture` (scene3d, media); `qui.PhysicalScissor` converts logical clips to GL scissor boxes.

### Layout (layout.go)

Engines: `FlexLayout` (CSS-aligned flexbox), `GridLayout`, `FlowLayout`, `AbsoluteLayout`. Two deliberate CSS-matching rules that bite if unknown:

1. **`Grow > 0` with unset `Basis` implies `Basis = 0`** (CSS `flex: <grow>`), otherwise full-measure children starve fixed siblings.
2. **`Shrink` defaults to 1**; opt out with `NoShrink: true`.

Use `w.SetFlex(grow)` for "fill remaining space". `Style.Margin` has CSS margin-box semantics in FlexLayout + `Container.Measure` (Grid/Absolute ignore it). Debug overflow with `QUI_DEBUG_LAYOUT=1` or the debug overlay.

Per-child layout metadata is controlled state: use `FlexItemValue` with `SetFlexItem`/`UpdateFlexItem`, `GridItemValue` with `SetGridItem`/`UpdateGridItem`, and `AbsolutePositionValue` with `SetAbsolutePosition`/`UpdateAbsolutePosition`. The legacy pointer getters remain for compatibility but bypass layout invalidation when mutated directly.

### Viewport zoom (zoom.go)

Browser-style page zoom: `Window.SetZoom` / `ZoomIn` / `ZoomOut` / `ResetZoom` / `SmartZoom`. Zooming shrinks the **content viewport** (`windowSize / zoom`) and scales the canvas by the same factor, so the tree **reflows** — text re-wraps, flex redistributes, percentages resolve against the smaller viewport — exactly like Cmd+/Cmd− in a browser, and unlike a magnifier that scales finished pixels and needs panning.

Two things to know:

1. **`Window.Size()` is the content viewport**, not the OS window. Every widget-side caller (dialog centering, popup measurement, menu clamping, the CSS `vw` unit) wants the space it lives in. Use `WindowSize()` only when you mean the window itself — placing it on a monitor, persisting geometry. They're equal at 100%. `DevicePixelRatio()` stays the display's true ratio; `EffectiveScale()` is `dpr × zoom` and is what converts a widget coordinate to a pixel.
2. **Zoom is opt-in** via `SetViewportZoomEnabled(true)`, which turns on Cmd/Ctrl+`=`/`-`/`0`, trackpad pinch and two-finger double-tap. Off by default because apps that already interpret pinch (q-excel zooms its grid, q-word its page, `graphs` an axis) would otherwise get a second competing zoom. A widget that consumes the gesture wins; only an unclaimed one zooms the page.

Input is converted to viewport space once, in `window_handler.go`, so dispatch, hit-testing, `actions.go` and the agent never see zoom. Zoom snaps to 2.5% steps — text measurement is memoized by font size, and a continuous pinch would otherwise miss the cache every frame.

### Theme (theme.go)

Global `Theme` of design tokens, deliberately design-system agnostic: a five-step surface ladder (`Surface` / `SurfaceSunken` / `SurfaceRaised` / `SurfaceStrong` / `SurfaceOverlay`), `Text` / `TextMuted` / `TextSelection`, the `Accent` family, `Border` / `BorderStrong` / `BorderFocus`, semantic `Error` / `Warning` / `Success`, a six-level `Elevation` shadow ladder, hover/focus/pressed/dragged overlay opacities, plus spacing / radius / font / transition scales. `ThemeFont(role)` resolves the six neutral text roles (`TextBody`, `TextLabel`, `TextHeading`, …) off that font scale. `SetTheme` invalidates subscribed windows; widgets read `CurrentTheme()` fresh inside `Draw`.

**qui ships ONE scheme: light.** There is no dark theme and no light/dark branching anywhere in the engine or in `widgets` — a dark UI is an app concern expressed as CSS on the htmlcss layer (see `examples/reactive-html`, which toggles a `.dark` class). Likewise there is no bundled design system: retinting a `Theme` is a brand-palette swap, and a *designed* look (Material, Fluent, your own) is a stylesheet, not a token package.

### Internationalization (locale.go + `i18n`)

Root owns a tiny seam; the catalog and CLDR machinery live in `i18n`, which
root never imports — the same arrangement as `qui.Animator` / `IMEClient` /
`VectorSource`. `widgets`, `htmlcss` and `reactive` all reach translations
through `qui.Translate`, so no new import edges exist.

```go
i18n.Install()                       // load builtin qui.* strings, register the Translator
i18n.Load(myLocales, "locales")      // add the app's catalogs (go:embed)
qui.SetDefaultLocale(qui.SystemLocale())
```

`qui.SetDefaultLocale` is the entire language switch: it bumps
`LocaleGeneration()`, drops the text-layout memo, and fires every window's and
style engine's subscription. **Nothing is rebuilt** — widgets resolve their
message keys inside Measure/Draw, and htmlcss re-resolves `data-i18n` during
restyle, so a switch costs one relayout, not a reconcile.

That resolve-late rule is the one thing to internalize. qui is retained-mode,
so a string captured at construction (`NewButton(i18n.T("save"), …)`) freezes
in whatever language was active then. Set a KEY instead:

```go
btn.SetTextKey("qui.save")                       // native widget
h.Button("Save").T("qui.save")                   // reactive/html DSL
<button data-i18n="qui.save">Save</button>       <!-- htmlcss markup -->
```

The literal stays as the fallback (and as in-code documentation): a missing
catalog entry renders "Save", not "qui.save". `QUI_I18N_STRICT=1` flips that
to a visible `⟦qui.save⟧` so gaps are impossible to miss while developing.

Lookup walks three tiers — the requested locale's fallback chain, the active
default's, then `FallbackLocale()` (the locale the strings were authored in,
"en" by default). Plurals use real CLDR categories via `x/text`, so Russian
gets four forms and Arabic six; `if n == 1` is wrong in most languages.

`i18n.Printer` (from `i18n.In(loc)`) formats numbers, percentages, currency,
dates, relative time and lists, and hands out a `collate.Collator` — use that
for any user-visible sort, since byte order puts "Ä" after "Z" in German.

**Agent selectors and i18n:** a translated caption breaks `[name="Save"]`.
Every keyed widget publishes its message key as the AX node's `nameKey`, and
the selector grammar matches it as `[key=qui.save]` — locale-independent, so
one script drives the app in every language. Prefer `#id` or `[key=]` over
`[name=]` in anything that must survive a language switch.

Tooling: `go run ./cmd/qui-i18n extract` scans Go source (`i18n.T`,
`SetTextKey`, `h.T`, `data-i18n` in markup) and updates the source catalog
without ever overwriting a translation; `lint` reports missing/unused keys,
placeholder drift and plural-shape errors; `pseudo` generates an accented,
40%-longer locale that surfaces hard-coded strings and clipping before a
translator is involved.

**State of RTL:** text is fully bidi — shaping, caret, selection and
`TextAlignStart/End` all resolve against paragraph direction, and
`Locale.Direction()` reports it. **Layout mirroring is NOT implemented** —
flex main axis, grid column order, scrollbar side, popup anchoring and dialog
button order are still physical. `examples/i18n` in Arabic shows exactly what
works and what doesn't. Plan: `docs/i18n-design.md` phase 2.

### Platform bridges

Split by build tag into `*_darwin.go` (+ `.m`/`.cc`) and `*_other.go` — IME (`NSTextInputClient` → `IMEClient` widgets), native dialogs, emoji (Core Text bitmaps), clipboard, system tray, native menus. Keep APIs identical across platforms; non-darwin gets compiling stubs. `IsCommandMod` abstracts Cmd-vs-Ctrl.

**Client-side title bars** (`window_titlebar*.go`): `Window.SetTitlebarStyle(TitlebarOverlay)` extends the content area under a transparent system title bar (macOS `FullSizeContentView`, so resize edges / traffic lights / fullscreen stay native), `TitlebarInsets()` reports the room the system's own window buttons need, and `BeginWindowDrag()` hands an in-flight press to the window manager (never move the window from mouse deltas — that loses snapping and is impossible on Wayland). Each returns false / zero where unsupported, so the app keeps its native title bar instead of breaking. htmlcss maps CSS `app-region: drag|no-drag` onto it; `examples/chrome-tabs` is the browser-style tab strip built on both.

**Overlay panels** (`window_overlay.go`): `App.NewOverlayPanel(w, h)` creates a `WindowOverlayPanel` — a borderless, transparent, always-on-top OS window that **never takes keyboard focus**. This is a distinct `WindowKind`, not a normal window with its chrome switched off, because three properties are load-bearing together: non-activating (clicking it must not deactivate the app the user is typing into), transparent framebuffer (the panel draws its own rounded card, the rest composites through), and above everything on every Space including fullscreen apps. Panels start hidden; `ShowAt(x, y)` positions **then** shows (the reverse order flickers at the old position for a frame), and `Hide()` orders out without destroying — an IME candidate bar toggles per keystroke and rebuilding an OS window plus GPU context at that rate is far too slow. The window's kind drives the transparent clear in `Window.Step` and `GLRenderer.SetTransparent` (which also switches to `BlendFuncSeparate` so straight alpha isn't squared during the composite). Only the `cocoa` backend implements it (`NSPanel` + `NSWindowStyleMaskNonactivatingPanel`); GLFW returns `ErrOverlayPanelUnsupported` rather than silently handing back a focus-stealing window. `examples/overlay-panel` is the demo.

### AI-native introspection + operability

Root files: `roles.go` (role constants + optional `Roled`/`Named`/`Valued` interfaces), `accessibility.go` (AX tree; overlays are peer top-level subtrees), `selector.go` (CSS-attribute grammar: `#id`, `[role=]`, `[name*=]`, `:nth`, `:visible`, `:focused`, `:layer(modal)`), `snapshot*.go` (scaled + annotated PNGs; `SnapshotScaled/Region/Annotated` serialize through the main-thread job queue so agent-goroutine captures never race a frame in progress — don't call them from the main goroutine of a live window), `actions.go` (selector-targeted synchronous Click/Type/Drag/… that respect modal blocking + scroll-into-view), `wait.go` (`WaitIdle`, `WaitOverlay`), `recording.go` (event listeners), `jobs.go` (`PostJob` main-thread queue). Cmd/Ctrl+Shift+A toggles the agent overlay in ANY qui app with no code change. `agent.BindEnv(window)` after `SetRoot` starts the HTTP server when `QUI_AGENT=1`; `GET /llm.txt` teaches an agent the full wire surface. New widget AX overrides go in each subpackage's `accessibility.go`.

## App-dev surface: htmlcss + reactive (the direction)

### htmlcss

CSS engine (parse → cascade → `ComputedStyle`) + ONE widget assembly — the live `El` — behind both entry points:

- **`El` + `StyleEngine`** (el.go / engine_live.go): the retained element widget. Each `El` embeds `widgets.Box`, keeps a `*Node` mirror in sync (so the selector cascade matches it), and recomputes CSS in place on class/attr/text/children changes. Form tags render through backing widgets; `overflow:auto/scroll` hosts a ScrollView; `<img>` loads raster/svg from `src` (BaseDir option); `<li>` inside ul/ol renders a hanging `[marker | content]` row honoring `list-style-type`; `<a>` opens its href on click (element or folded span, via `InlineBox.LinkAt`); icon leaves render `qui.VectorSource`; drag-reorder is built in (Draggable/DragHandle/OnDrop/OnDragOver with paint-only Transform feedback). Style appliers (`applyBox`/`applyCommon`/`applyTextStyle`/`chooseLayout`/`gridLayout`) are free functions in apply.go.
- **Restyle is SUBTREE-SCOPED** (engine_live.go): each mutation records its element; the coalesced flush (once per frame via PostJob) restyles from each dirty element's PARENT down — the complete impact scope since the selector grammar has no parent-facing selectors (parent covers self, descendants, and `~`/`+` sibling fallout). Nested scopes are pruned; portal/dialog roots restyle independently. `Restyle()` remains the full-document pass (mount time).
- **Inline formatting** (`El.buildFlow`): consecutive inline-level children group into anonymous runs — each run ONE `widgets.InlineBox` where text segments and pure-text inline elements (span/b/em/a/…) fold into styled, Unicode-shaped runs (UAX #14 wrap, bidi visual order, shared baselines, href spans), while children needing real widget behavior (handlers, drag, `#id`, visual boxes, icons, controls, `inline-block`) ride along as atomic inline boxes that still hit-test — and block children stack BETWEEN the runs (CSS anonymous-block behavior). `InlineBox` implements `qui.TextSelectable`, so folded text participates in cross-widget selection. `El.AccessibleName` includes folded span text.
- **One-shot:** `Render(html, css, opts)` / `RenderDoc` compile a parsed DOM into a static `El` tree (static.go: text nodes become anonymous `#text` segment Els, form content maps onto the control model) and restyle it once. `RenderResult` indexes every element by id/class — the result stays mutable/restyleable like any El tree.

CSS coverage (static-friendly set, complete): full selector set incl. interactive states, `var()` + `:root`, shorthands, box model + per-side borders, shadow/gradient/opacity/transform, `position:relative`, overflow, `display:flex/grid`, list markers, text-decoration/transform/overflow, white-space. **Known gaps:** transitions/animations (dynamic infrastructure deferred); `display:none` toggling at runtime (static Render skips resolved-none subtrees at compile time).

**Backing-primitives contract:** htmlcss renders through `widgets.Box`, `Label`, `InlineBox`, `Rule`, `Input`, `TextArea`, `CheckBox`, `Select`, `ScrollView`, `Image` (and reactive/html uses `MenuItem`/`ShowContextMenu`). Treat these widgets' APIs as a stable contract — changes silently break CSS rendering; run `go test ./htmlcss ./reactive/...` after touching them.

### reactive

Two engines, one runtime (`reactive/README.md` has the deep dive):

| Engine | Handles | Trigger | Cost |
|---|---|---|---|
| Reconcile | structure (mount/unmount, keyed lists) | `UseState` / setState | scoped tree diff |
| Signal | high-frequency values + structural fast path | `Signal.Set` | direct widget setter / scoped child sync — **no render pass** |

- Element kinds: host, component (`Component[P]` — per-instance hook state), `Fragment`, context provider, `Portal`/`PortalWith` (declarative overlay; `ModalPortal` = dialog shell), `ErrorBoundary`, `Empty()` (conditional placeholder), bound nodes (`Show`/`For`).
- Hooks: `UseState(Fn)` / `UseRef` / `UseMemo` / `UseCallback` / `UseReducer` / `UseEffect(Once)` / `UseSignal` — positional per fiber; order must be stable.
- Signals: `Signal[T]`, `Map`/`Computed` (auto-tracked via Get interception; `Peek` reads untracked), `BindWidget` (signal → setter, burst-coalesced, no reconcile). Signal identity must be stable for the widget's lifetime — create outside render or via `UseSignal`.
- `Show`/`For` mount/unmount subtrees straight from a signal via a reconcile scoped to that node — rows keep full hook/context/effect support; `For` rows need stable Keys.
- Props equality is `valuesEqual` (funcs compare by code pointer) — handlers must read fresh state via functional updaters or props, never captured render-locals.
- **Host-layer state rides on the Runtime:** `Runtime.SetHostData`/`HostData` + `reactive.CurrentRuntime()` (set during every render/reconcile/scoped-sync pass). This is how element Create hooks resolve per-runtime backends — never a package global (breaks multi-window).

### reactive/html (`package html`, import aliased as `h`)

Fluent HTML-element builders lowering to reactive Elements backed by `htmlcss.El`. The surface is deliberately regular, because irregularity is what both people and models get wrong:

**Every tag is `h.Tag(...any) *Builder`** — one shape for all of them, so `h.Button(icons.Save, "Save")` and `h.P("Hello ", h.B("world"), "!")` are written the way they read. Arguments are classified by type: a `string` is text, a `Node` (`*Builder`, `reactive.Element`, `Frag`, a component) is a child, a slice of any of those splices in place (pass `rows`, **not** `rows...`), a number is formatted as text, a `qui.VectorSource` is an icon, `nil` is dropped, and anything else panics naming the tag, the argument index and the type. Text mixed with element children lowers to anonymous `#text` segments, so it folds into one inline run with a shared baseline and wrap; text-only content stays the element's own text (one widget). The four elements that hold no text read a string as their own payload instead: `img`→src, `input`/`textarea`→value, `select`→options.

**There is no lowering ceremony.** `reactive.Element` satisfies `h.Node`, so a component is a plain `func(...) h.Node` — no `.Build()` at the end, no `h.El(...)` around a component or portal. `h.Component` / `h.Leaf` wrap the reactive constructors so app code never has to name `reactive.Element` at all.

**Components can be written as HTML instead of builders.** `h.MustParse` compiles a markup fragment once (typically `//go:embed`-ed from its own `.html` file); `Template.Bind(h.Scope{…})` fills it per render, at about the cost of the equivalent builders. The deliberate constraint is that **there is no expression language** — a hole names one key in a flat `Scope`, and that is all:

```html
<div class="todo" :key="id">
  <span class="todo-text">{text}</span>
  <span class="tag" :if="urgent">urgent</span>
  <button class="del" @click="onDelete">✕</button>
</div>
```

`{name}` interpolates (in content or inside an attribute value); `:if` `:key` `:class` `:text` `:value` `:checked` `:selected` `:disabled` `:options` `:draggable` `:icon` `:ref` take one Scope key each; `@click` `@input` `@commit` `@toggle` `@select` `@contextmenu` `@keydown` `@drop` `@dragover` `@dragend` (and the rest) take a handler. Loops, conditions with any structure, derived values and formatting stay in Go and reach the template through the Scope or through `<slot name="rows"></slot>` — that is the trade that keeps a template legible markup. A Scope value that is a **signal binds instead of interpolating**, so the update skips the render pass: a `*Signal[string]` hole becomes `BindText`, `:class` becomes `BindClass`, `:if` with a `*Signal[bool]` becomes `Show`. Structural rules (whitespace significance, `<select>`'s `<option>` children, text-only vs mixed content) are shared with `htmlcss`'s static build, so a template and the same markup through `htmlcss.Render` produce the same tree. Errors carry the **template line number** plus the element path; `Template.Names()` lists the Scope keys a template needs, so a test can assert a caller covers them. Several fragments can share one file, each wrapped in `<template id="…">`, compiled with `h.MustParseSet` and bound as `set.Bind("row", scope)` — a dialog is a handful of small fragments and a file each is worse than one file. `examples/reactive-html/card.html` is the smallest worked example; `apps/q-excel/cf_dialog.html` (shared by the conditional-formatting and data-validation dialogs) is the realistic one.

Tags (`h.Div/Span/H1/Button/Input/Checkbox/Textarea/Select/Img/Icon/A/Ul/Li/Table/Tr/Td/B/Em/Code/…`), chainable props under their HTML names (`.Class/.ID/.Key/.Attr/.Href/.Src/.Type/.Name/.Disabled/.Min/.Max/.Step/.OnClick/.OnInput/.Value/.BindText/.BindClass/.Draggable/.OnDrop/…`), conditionals and lists (`h.If/Nothing/Show/For/Each/Frag` — `For` is the signal-driven keyed list that skips the render pass, `Each` the plain slice map), overlays (`h.Portal/ModalPortal/ContextMenu`). `h.Mount(window, css, app)` creates the style engine, stores it on the Runtime, renders, and wires the engine root — each window owns its engine; multiple windows never share styling state.

## Conventions

- **Where new code goes:** engine changes → root (only when it affects the engine, not one widget). New widget → `widgets/` (file named after the widget). CSS features → `htmlcss` (implement in the shared appliers so BOTH paths get it; add both an engine test and, when visual, a snapshot test).
- **User-facing strings:** never hard-code one in `widgets/`, `htmlcss` or the engine. Use `qui.TOr(key, literal)` so an app without catalogs is unaffected, and give any new text-bearing widget a `TextKey`-style field resolved in Measure/Draw (see `widgets/i18n.go`). Widgets whose caption comes from a key must implement `AccessibleNameKey()` — otherwise `[key=]` selectors silently stop covering them.
- **Locale-dependent caches:** anything memoizing measured or shaped text must include `LocaleGeneration()` in its key, exactly like `FontRegistryGeneration()`. Reactive runtime → `reactive`; DSL surface → `reactive/html`. AX overrides → each subpackage's `accessibility.go`. HTTP endpoints → `agent/` (primitives themselves go in root). Platform code follows the `_darwin.go`/`_other.go` split — never spread darwin code across a tagged and a generic file.
- `widgets/` files use `import . "github.com/qizhanchan/qui"` (dot import) — the one blessed place; qualify normally everywhere else. The dot import does NOT bypass unexported fields: use accessors (`Bounds()`, `Style()`, `Enabled()`).
- Container subclasses must call `SetSelf` after construction.
- Tests use root `testhelpers.go` fixtures: `NewTestWindow`, `NewMouseEvent`/`NewKeyEvent`/`NewCharEvent`/`NewScrollEvent`, `RecordingCanvas`, `NewImageCanvas`, `DrainJobsForTest`. Test-mode windows run actions inline (no frame pump needed).
- Widgets that animate implement `Tickable`; text input implements `IMEClient`; focusables implement `Focusable() bool` + `SetFocused(bool)`.
- `vendor/` is git-ignored but committed locally; deps: `go-gl/gl`, `go-gl/glfw`, `golang.org/x/image`.


# 调试手段：qui-agent（从外部驱动 / 观测运行中的应用）

每个 qui 应用内置一个「无障碍 + 可操作」surface：可读出 widget 树（角色 / 名称 / 值 / 包围盒），并用选择器派发真实的点击 / 输入 / 滚动事件。配合 cmd/qui-agent 这个命令行客户端，可以像写脚本一样验证一个功能——无需肉眼盯着窗口。常用于：
- 验收一个交互流程（点这里 → 弹菜单 → 点 Delete → 某行消失）
- 远程 / headless 冒烟测试
- 让 AI / 自动化按语义选择器驱动 UI

启用（两步）：

1) 应用里在 SetRoot 之后绑定 agent 服务（env 门控，不设 QUI_AGENT 时是 no-op）：

    import "github.com/qizhanchan/qui/agent"

    if srv, err := agent.BindEnv(window); err == nil && srv != nil {
        window.OnClose(func() { _ = srv.Stop() })
    }

   examples/reactive 和 apps/q-excel 已经接好，可直接参考。

2) 带 QUI_AGENT=1 运行。注意 go run 不转发信号，调试时建议先 build 再后台跑：

    go build -o /tmp/demo ./examples/reactive && QUI_AGENT=1 /tmp/demo &

服务默认监听一个 Unix socket（`$TMPDIR/qui-agent-<pid>.sock`）。

**多个 qui 应用同时在跑时，一定要用 `QUI_AGENT_SOCK` 钉住 socket 路径**——默认路径带
PID，客户端「自动挑最新的存活 socket」会挑到另一个应用上（症状：命令都返回 ok，但截图
里的窗口不是你以为的那个）。两侧都指同一个文件即可：

    go build -o /tmp/demo ./examples/reactive
    QUI_AGENT_SOCK=/tmp/demo.sock /tmp/demo &          # 指定 SOCK 即隐含 QUI_AGENT=1
    /tmp/qui-agent -sock /tmp/demo.sock tree

使用 qui-agent 命令行（不带 `-sock` 时自动发现「存活进程」对应的最新 socket）：

    go build -o /tmp/qui-agent ./cmd/qui-agent      # 或 go run ./cmd/qui-agent <子命令>

    /tmp/qui-agent health                            # 就绪探针
    /tmp/qui-agent tree                              # 打印整棵 widget 树（缩进大纲）
    /tmp/qui-agent tree '[role=menu]'                # 只看匹配选择器的子树
    /tmp/qui-agent click   '#save-btn'               # 按 ID 点击
    /tmp/qui-agent rightclick '#todo-1'              # 右键（弹出上下文菜单）
    /tmp/qui-agent wait    '[role=menuitem]'         # 等某选择器出现（替代 sleep）
    /tmp/qui-agent click   '[name="Delete"]'         # 按可访问名称点击
    /tmp/qui-agent wait    '[role=menuitem]' --gone  # 等其消失
    /tmp/qui-agent type    '#search' "hello"         # 输入文本
    /tmp/qui-agent pinch   '#grid' 1.5               # 双指捏合手势（缩放到 150%）
    /tmp/qui-agent shot    /tmp/ui.png --annotate    # 截图（可叠加 ID/角色标注）
    /tmp/qui-agent raw GET '/tree?maxDepth=1'        # 直接打 HTTP 端点的逃生舱

选择器语法（CSS 属性风格）：

    #id                 按 widget ID
    [role=button]       按角色（button / textbox / menuitem / listitem / dialog ...）
    [name="Save"]       按可访问名称（[name*="av"] 子串，^= 前缀，$= 后缀）
    [text=...]          name 的别名
    :nth(N) :visible :focused :layer(modal)

不写代码也能用：任意 qui 应用按 Cmd+Shift+A（mac）/ Ctrl+Shift+A 可切换「agent 叠加层」，直接在窗口上显示每个 widget 的 ID / 角色 / 名称 / 焦点。

底层 HTTP 端点（cmd/qui-agent 只是薄封装）：GET /tree、/screenshot、/diagnostics、/health、/wait、/console、/events（SSE）、POST /act。完整线缆参考见 agent/llm.txt（也通过 GET /llm.txt 内嵌返回）。
