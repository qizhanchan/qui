# qui — Architecture

**English** · [中文](architecture.zh.md)

This document describes how the engine works internally. For the high-level picture see [overview.md](overview.md); for the feature catalog see [features.md](features.md).

## Layering and import direction

`qui` is one module with a strictly layered dependency graph.

```
htmlcss ──► widgets ──► root qui ◄── everything else
reactive/html ──► reactive ──► root qui
```

Subpackages import the root engine; the root never imports a subpackage. Root↔subpackage coupling goes through interfaces defined in root (`qui.Animator`, `qui.IMEClient`, `qui.VectorSource`, `qui.Translator`, focusable/tickable contracts). `widgets` imports nothing but root; `reactive` imports only root and is backend-agnostic. Allowed cross-subpackage edges are narrow and fixed (`scene3d`→`widgets`, `htmlcss`→`widgets`+`svg`, `reactive/html`→`reactive`+`htmlcss`+`widgets`, `icons`→`svg`). If root appears to need a subpackage symbol, the fix is to move the symbol or define an interface in root.

## Frame loop

The engine runs a single-threaded loop pinned to the OS main thread (`runtime.LockOSThread`). Each `Window.Step()`:

1. drain the `PostJob` queue;
2. `Tick(now)` every `Tickable` (returned rects union into `dirtyRegion`);
3. re-layout if `root.IsLayoutDirty()`;
4. **if `dirtyRegion` is empty, return without painting**;
5. clear and redraw only the dirty region;
6. upload only the redrawn pixels to the GPU (`GLRenderer.SetDamage`) and swap buffers.

Frames are paced on demand (`idle.go`), not by a fixed clock. Between iterations `App.Run` asks each window how soon it needs the next `Step` and blocks in the platform's event wait for exactly that long:

- **changing** — it painted last frame, or has animators, pending jobs, dirty paint or dirty layout: wake at display cadence (1/60 s). Because a paint buys the next frame, a `Tickable` that animates by returning a dirty rect each frame keeps running with no extra code and stops when its `Tick` reports nothing;
- **waiting on a moment** — a `Tickable` that changes after a quiet stretch (the caret blink) calls `Window.RequestTickAt(t)` from `Tick`; tooltip delays are tracked the same way: wake at the earliest such moment;
- **idle** — block until an OS event or a cross-goroutine wake (`PostJob`, `WakeEventLoop`, menu actions). An idle app costs no CPU.

Registered animators count as "changing" only while frames keep coming. An `Animator` that never reports done yet changes nothing — the classic case is a goroutine→UI queue drained from `Tick` — would otherwise pin the loop at 60 Hz forever. After 0.5 s of ticking without a painted frame, animators alone only buy a poll every 250 ms (they are still ticked, so such code keeps working at bounded latency), they stop counting against `IdleState.IsIdle` (reported as `QuietAnimators`), and each such animator type is logged once. Any paint restores full cadence. The fix on the app side is to hand goroutine results over with `PostJob` and to wake for a known moment with `RequestTickAt`.

Platform callbacks that AppKit can deliver from inside the wait without an input event (Dock minimize, a fullscreen transition settling, a programmatic move) cut the wait short so polled state (`OnMove`, `OnMinimize`, fullscreen) is still noticed. `App.SetMaxIdleWait(d)` caps the sleep for third-party `Tickable`s that predate `RequestTickAt`; prefer fixing the `Tickable`.

Widget trees, focus, overlays, layout metadata, rendering state and direct introspection belong to that UI goroutine. Cross-goroutine code must use `Window.PostJob` / `TryPostJob` / `PostPriorityJob`; read-side agent code uses the `*Synced` APIs. `QUI_DEBUG_THREAD=1` turns on fail-fast thread-ownership checks for production windows (and `Window.EnableUIThreadChecks` enables them selectively in tests or custom hosts). Detached widgets may be constructed off-thread, but once attached their state is UI-owned.

The process owns exactly one `App` and one platform backend. A second `NewApp` returns `ErrAppAlreadyExists`. The backend stays initialized until process exit; `App.Run` destroys app-owned windows and status items but never terminates process-global platform state. The same `App` may create another set of windows and run again after a previous run closes all windows.

### Invalidation and repaint scoping

The core invariant is that **nothing paints unless something invalidated**.

- `Invalidate` / `InvalidateRect` mark pure visual changes.
- `InvalidateLayout` marks size-affecting changes. It dirty-rects only the caller's paint extent (not the whole window) and bubbles a flag to the root. The layout pass then promotes to a full repaint only if some widget's bounds actually changed (`BaseWidget.Layout` → `noteBoundsChange`). Sub-pixel jitter under `boundsEpsilon` stays scoped; out-of-pass `Layout` calls invalidate old+new extents directly.
- Measurement is cached (`measure_cache.go`). Layout engines measure children through `MeasureChild`, which memoizes each widget's `Measure` per available size. `InvalidateLayout` drops the cache of the widget and every ancestor, so a change re-measures only its ancestor chain while clean sibling subtrees answer from their caches; theme, font, locale, window-size changes and `Window.InvalidateLayout` drop every cache. The contract is that a widget whose size-affecting state changes invalidates **itself** — changing a child and invalidating only an ancestor leaves the child's cached size stale. `QUI_DEBUG_LAYOUT_CACHE=1` re-measures every cache hit and logs widgets that break it.
- A text tick that measures to the same size repaints just its own rect, so the window genuinely idles between updates and `WaitIdle` works against continuously-updating apps. Set `QUI_DEBUG_PAINT=1` to log full-repaint promotions and the first widget that moved.

Text measurement is memoized (the `BuildTextLayout` cache in `text.go`, invalidated on font-registry changes), so the full-tree relayout that any layout invalidation triggers costs map lookups for unchanged text rather than re-measurement.

## Widget tree and `BaseWidget.self`

All widgets implement `Widget` (`widget.go`); `BaseWidget` provides defaults and is embedded everywhere. Go embedding has no virtual dispatch, so `BaseWidget` carries a `self Widget` field set via `SetSelf(outer)` right after construction — **always call `SetSelf` after constructing a `Container` subclass**, or event dispatch misses ancestors. `Container` is the sole tree-composition type; children are exposed to framework walks via `ChildList()`.

## Event dispatch

Dispatch is DOM-style and three-phase: capture (root→parent of target), target, then bubble (parent→root), with `StopPropagation`. Returning `true` from `Handle` is an implicit stop. Additional rules:

- mouse capture on `MouseDown` — `MouseMove`/`MouseUp` route to the captured widget;
- hover enter/leave is synthesized by diffing the dispatch path;
- focus updates on `MouseDown` and Tab cycling;
- drag/drop is synthesized with a 4px dead-zone;
- overlays hit-test top-down, and `Modal()` overlays trap focus.

### Coordinate spaces

Every widget receives mouse, gesture and drag events in **its own coordinate space** — the one its `Bounds()` live in — so it can always compare event coordinates against its own geometry (`eventInWidgetSpace` in `event.go`; identity for untransformed trees).

A widget's own space differs from window space when an ancestor transforms it:

- a CSS `transform` (`InteractionTransformer`) moves the widget's own box;
- a scroll container (`ChildInteractionTransformer`) moves descendants but not the container's box. `widgets.ScrollView` lays its content out **once** at the viewport origin and scrolls by transform, so scrolling is O(1) and child bounds are scroll-independent.

Cross the boundary with `WindowPointToLocal` / `LocalPointToWindow` / `InteractionBoundsOf` (`widget.go`). Code that hands geometry to something positioned in window space — overlay/popup anchors, the IME caret rect, tooltip anchors, AX and agent bounds — must use the on-screen form, never raw `Bounds()`. Descendant invalidation maps out through `ChildPaintTransformer` and is clipped by `PaintClipper`, so a scrolled-out widget dirties nothing.

## Rendering

Rendering is Skia-shaped and split into two layers.

**Canvas frontend** (`render.go`, `canvas_state.go`, `canvas_paint.go`, `canvas_layer.go`, `path.go`): a Save/Restore state stack (clip + 2×3 affine matrix), `DrawShape(Shape, Paint)` as the single entry point (convenience methods are wrappers), `Path` with quad/cubic Béziers, anti-aliased winding/even-odd fill, stroke cap/join/miter, `SaveLayer` with blend modes and `Paint.Alpha`, `ClipPath` (masked at Restore), `Paint.Shader` (linear/radial gradients), `ColorFilter`/`ImageFilter` (drop shadow, blur), and `DrawShadow` for elevation shadows.

**RasterBackend** (`backend.go`): the pixel-producing abstraction below the frontend. All coordinates are already physical, and every method receives a clip it must honor. `backend_cpu.go` is the reference implementation; `backend_gpu.go` (FBO + shaders, `QUI_GPU_RASTER=1`) swaps into the same slot and interleaves with the CPU path per primitive. `renderer_gl.go` owns the GL window blit.

### Text

Text uses `go-text/typesetting` glyph-run shaping (`text_shaping.go`) with OpenType GSUB/GPOS, Unicode bidi, UAX #14 line breaking and UAX #29 grapheme boundaries. Measurement, CPU/GL drawing, PDF output, wrapping, hit testing, selection and editor carets all share the same shaped clusters. `RuneAdvance` remains only as a compatibility helper for standalone-rune utilities. Styled runs preserve shaping context across paint-only boundaries.

Inline flow — text and atomic inline boxes sharing lines with baseline alignment — lives in `inline.go` and is surfaced as `widgets.InlineBox`.

GL widgets composite via `GPUCanvas.QueueGLDraw` + `ActiveGLRenderer().DrawTexture` (used by `scene3d` and `media`); `qui.PhysicalScissor` converts logical clips to GL scissor boxes.

## Layout

Engines: `FlexLayout` (CSS-aligned flexbox), `GridLayout`, `FlowLayout` and `AbsoluteLayout`. Two deliberate CSS-matching rules that bite if unknown:

1. **`Grow > 0` with unset `Basis` implies `Basis = 0`** (CSS `flex: <grow>`); otherwise full-measure children starve fixed siblings.
2. **`Shrink` defaults to 1**; opt out with `NoShrink: true`.

Use `w.SetFlex(grow)` for "fill remaining space". `Style.Margin` has CSS margin-box semantics in `FlexLayout` + `Container.Measure` (Grid and Absolute ignore it). Debug overflow with `QUI_DEBUG_LAYOUT=1` or the debug overlay.

Per-child layout metadata is controlled state: `FlexItemValue` with `SetFlexItem`/`UpdateFlexItem`, `GridItemValue` with `SetGridItem`/`UpdateGridItem`, and `AbsolutePositionValue` with `SetAbsolutePosition`/`UpdateAbsolutePosition`. The legacy pointer getters remain for compatibility but bypass layout invalidation when mutated directly.

## Viewport zoom

Browser-style page zoom: `Window.SetZoom` / `ZoomIn` / `ZoomOut` / `ResetZoom` / `SmartZoom`. Zooming shrinks the **content viewport** (`windowSize / zoom`) and scales the canvas by the same factor, so the tree **reflows** — text re-wraps, flex redistributes, percentages resolve against the smaller viewport — exactly like Cmd+/Cmd− in a browser, unlike a magnifier that scales finished pixels and needs panning.

Two things to know:

1. **`Window.Size()` is the content viewport**, not the OS window. Every widget-side caller (dialog centering, popup measurement, menu clamping, the CSS `vw` unit) wants the space it lives in. Use `WindowSize()` only when you mean the window itself — placing it on a monitor, persisting geometry. They are equal at 100%. `DevicePixelRatio()` stays the display's true ratio; `EffectiveScale()` is `dpr × zoom` and is what converts a widget coordinate to a pixel.
2. **Zoom is opt-in** via `SetViewportZoomEnabled(true)`, which turns on Cmd/Ctrl+`=`/`-`/`0`, trackpad pinch and two-finger double-tap. It is off by default because apps that already interpret pinch (a chart zooming an axis, a canvas app zooming its content) would otherwise get a second, competing zoom. A widget that consumes the gesture wins; only an unclaimed one zooms the page.

Input is converted to viewport space once, in `window_handler.go`, so dispatch, hit-testing, `actions.go` and the agent never see zoom. Zoom snaps to 2.5% steps — text measurement is memoized by font size, and a continuous pinch would otherwise miss the cache every frame.

## Theme

A global `Theme` of design tokens, deliberately design-system agnostic: a five-step surface ladder (`Surface` / `SurfaceSunken` / `SurfaceRaised` / `SurfaceStrong` / `SurfaceOverlay`), `Text` / `TextMuted` / `TextSelection`, the `Accent` family, `Border` / `BorderStrong` / `BorderFocus`, semantic `Error` / `Warning` / `Success`, a six-level `Elevation` shadow ladder, hover/focus/pressed/dragged overlay opacities, plus spacing / radius / font / transition scales. `ThemeFont(role)` resolves the six neutral text roles (`TextBody`, `TextLabel`, `TextHeading`, …) off that font scale. `SetTheme` invalidates subscribed windows; widgets read `CurrentTheme()` fresh inside `Draw`.

**qui ships ONE scheme: light.** There is no dark theme and no light/dark branching anywhere in the engine or in `widgets` — a dark UI is an application concern expressed as CSS on the htmlcss layer (see `examples/reactive-html`, which toggles a `.dark` class). Likewise there is no bundled design system: retinting a `Theme` is a brand-palette swap, and a *designed* look (Material, Fluent, your own) is a stylesheet, not a token package.

## Internationalization

Root owns a tiny seam; the catalog and CLDR machinery live in `i18n`, which root never imports — the same arrangement as `qui.Animator` / `IMEClient` / `VectorSource`. `widgets`, `htmlcss` and `reactive` all reach translations through `qui.Translate`, so no new import edges exist.

```go
i18n.Install()                       // load builtin qui.* strings, register the Translator
i18n.Load(myLocales, "locales")      // add the app's catalogs (go:embed)
qui.SetDefaultLocale(qui.SystemLocale())
```

`qui.SetDefaultLocale` is the entire language switch: it bumps `LocaleGeneration()`, drops the text-layout memo, and fires every window's and style engine's subscription. **Nothing is rebuilt** — widgets resolve their message keys inside Measure/Draw, and htmlcss re-resolves `data-i18n` during restyle, so a switch costs one relayout, not a reconcile.

That resolve-late rule is the one thing to internalize. qui is retained-mode, so a string captured at construction (`NewButton(i18n.T("save"), …)`) freezes in whatever language was active then. Set a KEY instead:

```go
btn.SetTextKey("qui.save")                       // native widget
h.Button("Save").T("qui.save")                   // reactive/html DSL
<button data-i18n="qui.save">Save</button>       <!-- htmlcss markup -->
```

The literal stays as the fallback (and as in-code documentation): a missing catalog entry renders "Save", not "qui.save". `QUI_I18N_STRICT=1` flips that to a visible `⟦qui.save⟧` so gaps are impossible to miss while developing.

Lookup walks three tiers — the requested locale's fallback chain, the active default's, then `FallbackLocale()` (the locale the strings were authored in, "en" by default). Plurals use real CLDR categories via `x/text`, so Russian gets four forms and Arabic six; `if n == 1` is wrong in most languages.

`i18n.Printer` (from `i18n.In(loc)`) formats numbers, percentages, currency, dates, relative time and lists, and hands out a `collate.Collator` — use that for any user-visible sort, since byte order puts "Ä" after "Z" in German.

**Agent selectors and i18n:** a translated caption breaks `[name="Save"]`. Every keyed widget publishes its message key as the AX node's `nameKey`, and the selector grammar matches it as `[key=qui.save]` — locale-independent, so one script drives the app in every language. Prefer `#id` or `[key=]` over `[name=]` in anything that must survive a language switch.

Tooling: `go run ./cmd/qui-i18n extract` scans Go source (`i18n.T`, `SetTextKey`, `h.T`, `data-i18n` in markup) and updates the source catalog without ever overwriting a translation; `lint` reports missing/unused keys, placeholder drift and plural-shape errors; `pseudo` generates an accented, 40%-longer locale that surfaces hard-coded strings and clipping before a translator is involved.

**State of RTL:** text is fully bidi — shaping, caret, selection and `TextAlignStart/End` all resolve against paragraph direction, and `Locale.Direction()` reports it. **Layout mirroring is NOT implemented** — flex main axis, grid column order, scrollbar side, popup anchoring and dialog button order are still physical. `examples/i18n` in Arabic shows exactly what works and what does not.

## Platform bridges

Platform code is split by build tag into `*_darwin.go` (+ `.m`/`.cc`) and `*_other.go` — IME (`NSTextInputClient` → `IMEClient` widgets), native dialogs, emoji (Core Text bitmaps), clipboard, system tray, native menus. APIs must stay identical across platforms; non-darwin gets compiling stubs. `IsCommandMod` abstracts Cmd-vs-Ctrl.

**Client-side title bars** (`window_titlebar*.go`): `Window.SetTitlebarStyle(TitlebarOverlay)` extends the content area under a transparent system title bar (macOS `FullSizeContentView`, so resize edges / traffic lights / fullscreen stay native). `TitlebarInsets()` reports the room the system's own window buttons need, and `BeginWindowDrag()` hands an in-flight press to the window manager (never move the window from mouse deltas — that loses snapping and is impossible on Wayland). Each returns false / zero where unsupported, so the app keeps its native title bar instead of breaking. htmlcss maps CSS `app-region: drag|no-drag` onto it; `examples/chrome-tabs` is built on both.

**Overlay panels** (`window_overlay.go`): `App.NewOverlayPanel(w, h)` creates a `WindowOverlayPanel` — a borderless, transparent, always-on-top OS window that **never takes keyboard focus**. This is a distinct `WindowKind`, not a normal window with its chrome switched off, because three properties are load-bearing together: non-activating (clicking it must not deactivate the app the user is typing into), transparent framebuffer (the panel draws its own rounded card, the rest composites through), and above everything on every Space including fullscreen apps. Panels start hidden; `ShowAt(x, y)` positions **then** shows (the reverse order flickers at the old position for a frame), and `Hide()` orders out without destroying — an IME candidate bar toggles per keystroke, and rebuilding an OS window plus GPU context at that rate is far too slow. The window's kind drives the transparent clear in `Window.Step` and `GLRenderer.SetTransparent` (which also switches to `BlendFuncSeparate` so straight alpha is not squared during the composite). Only the `cocoa` backend implements it (`NSPanel` + `NSWindowStyleMaskNonactivatingPanel`); GLFW returns `ErrOverlayPanelUnsupported` rather than silently handing back a focus-stealing window. `examples/overlay-panel` is the demo.

## AI-native introspection and operability

Root files:

- `roles.go` — role constants plus the optional `Roled`/`Named`/`Valued` interfaces;
- `accessibility.go` — the AX tree (overlays are peer top-level subtrees);
- `selector.go` — the CSS-attribute selector grammar (`#id`, `[role=]`, `[name*=]`, `:nth`, `:visible`, `:focused`, `:layer(modal)`);
- `snapshot*.go` — scaled and annotated PNGs; `SnapshotScaled/Region/Annotated` serialize through the main-thread job queue so agent-goroutine captures never race a frame in progress (do not call them from the main goroutine of a live window);
- `actions.go` — selector-targeted synchronous Click/Type/Drag/… that respect modal blocking and scroll-into-view;
- `wait.go` — `WaitIdle`, `WaitOverlay`;
- `recording.go` — event listeners;
- `jobs.go` — the `PostJob` main-thread queue.

`Cmd/Ctrl+Shift+A` toggles the agent overlay in ANY qui app with no code change. `agent.BindEnv(window)` after `SetRoot` starts the HTTP server when `QUI_AGENT=1`; `GET /llm.txt` teaches an agent the full wire surface. New widget AX overrides go in each subpackage's `accessibility.go`.

## App-dev surface: htmlcss and reactive

### htmlcss

The CSS engine (parse → cascade → `ComputedStyle`) and ONE widget assembly — the live `El` — sit behind both entry points.

- **`El` + `StyleEngine`** (`el.go` / `engine_live.go`): the retained element widget. Each `El` embeds `widgets.Box`, keeps a `*Node` mirror in sync (so the selector cascade matches it), and recomputes CSS in place on class/attr/text/children changes. Form tags render through backing widgets; `overflow:auto/scroll` hosts a `ScrollView`; `<img>` loads raster/svg from `src` (BaseDir option); `<li>` inside ul/ol renders a hanging `[marker | content]` row honoring `list-style-type`; `<a>` opens its href on click (element or folded span, via `InlineBox.LinkSourceAt`, which also names the folded `<a>`); icon leaves render a `qui.VectorSource`; drag-reorder is built in. Style appliers (`applyBox`/`applyCommon`/`applyTextStyle`/`chooseLayout`/`gridLayout`) are free functions in `apply.go`.
- **Restyle is SUBTREE-SCOPED** (`engine_live.go`): each mutation records its element; the coalesced flush (once per frame via `PostJob`) restyles from each dirty element's PARENT down — the complete impact scope, since the selector grammar has no parent-facing selectors (parent covers self, descendants, and `~`/`+` sibling fallout). Nested scopes are pruned; portal/dialog roots restyle independently. `Restyle()` remains the full-document pass used at mount time.
- **Inline formatting** (`El.buildFlow`): consecutive inline-level children group into anonymous runs — each run is ONE `widgets.InlineBox` where text segments and pure-text inline elements (span/b/em/a/…) fold into styled, Unicode-shaped runs (UAX #14 wrap, bidi visual order, shared baselines, href spans), while children needing real widget behavior (handlers, drag, `#id`, visual boxes, icons, controls, `inline-block`) ride along as atomic inline boxes that still hit-test. Block children stack BETWEEN the runs (CSS anonymous-block behavior). `InlineBox` implements `qui.TextSelectable`, so folded text participates in cross-widget selection. `El.AccessibleName` includes folded span text.
- **One-shot:** `Render(html, css, opts)` / `RenderDoc` compile a parsed DOM into a static `El` tree (`static.go`) and restyle it once. `RenderResult` indexes every element by id/class — the result stays mutable and restyleable like any `El` tree.

The backing-primitives contract: htmlcss renders through `widgets.Box`, `Label`, `InlineBox`, `Rule`, `Input`, `TextArea`, `CheckBox`, `Select`, `ScrollView`, `Image` (and `reactive/html` uses `MenuItem`/`ShowContextMenu`). Treat these widget APIs as a stable contract — changes silently break CSS rendering; run `go test ./htmlcss ./reactive/...` after touching them.

### reactive

Two engines, one runtime (see [reactive/README.md](../reactive/README.md) for the deep dive):

| Engine | Handles | Trigger | Cost |
|---|---|---|---|
| Reconcile | structure (mount/unmount, keyed lists) | `UseState` / setState | scoped tree diff |
| Signal | high-frequency values + structural fast path | `Signal.Set` | direct widget setter / scoped child sync — **no render pass** |

- Element kinds: host, component (`Component[P]`), `Fragment`, context provider, `Portal`/`PortalWith` (`ModalPortal` = dialog shell), `ErrorBoundary`, `Empty()`, bound nodes (`Show`/`For`).
- Hooks: `UseState(Fn)` / `UseRef` / `UseMemo` / `UseCallback` / `UseReducer` / `UseEffect(Once)` / `UseSignal` — positional per fiber; order must be stable.
- Signals: `Signal[T]`, `Map`/`Computed` (auto-tracked via Get interception; `Peek` reads untracked), `BindWidget` (signal → setter, burst-coalesced, no reconcile). Signal identity must be stable for the widget's lifetime.
- `Show`/`For` mount/unmount subtrees straight from a signal via a reconcile scoped to that node. `For` rows need stable keys.
- Props equality is `valuesEqual` (funcs compare by code pointer), so handlers must read fresh state via functional updaters or props, never captured render-locals.
- **Host-layer state rides on the Runtime:** `Runtime.SetHostData`/`HostData` + `reactive.CurrentRuntime()` (set during every render/reconcile/scoped-sync pass). This is how element Create hooks resolve per-runtime backends — never a package global (that breaks multi-window).

### reactive/html

The fluent HTML-element builders lower to reactive Elements backed by `htmlcss.El`. The surface is deliberately regular:

- **Every tag is `h.Tag(...any) *Builder`.** Arguments are classified by type: a `string` is text, a `Node` is a child, a slice of any of those splices in place, a number is formatted as text, a `qui.VectorSource` is an icon, `nil` is dropped, and anything else panics naming the tag, argument index and type. Text mixed with element children lowers to anonymous `#text` segments so it folds into one inline run; text-only content stays the element's own text. The four elements that hold no text read a string as their own payload: `img`→src, `input`/`textarea`→value, `select`→options.
- **There is no lowering ceremony.** `reactive.Element` satisfies `h.Node`, so a component is a plain `func(...) h.Node`. `h.Component` / `h.Leaf` wrap the reactive constructors.
- **Components can be written as HTML.** `h.MustParse` compiles a markup fragment once (typically `//go:embed`-ed); `Template.Bind(h.Scope{…})` fills it per render. There is deliberately no expression language: a hole names one key in a flat `Scope`. `{name}` interpolates; `:if` `:key` `:class` `:text` `:value` `:checked` `:selected` `:disabled` `:options` `:draggable` `:icon` `:ref` each take one Scope key; `@click` `@input` `@commit` `@toggle` `@select` `@contextmenu` `@keydown` `@drop` `@dragover` `@dragend` (and the rest) take a handler. A Scope value that is a signal binds instead of interpolating (`BindText`, `BindClass`, `Show`), so the update skips the render pass. Structural rules are shared with htmlcss's static build, so a template and the same markup through `htmlcss.Render` produce the same tree. Errors carry the template line number plus the element path; `Template.Names()` lists the Scope keys a template needs.
- `h.Mount(window, css, app)` creates the style engine, stores it on the Runtime, renders, and wires the engine root — each window owns its engine; multiple windows never share styling state.
