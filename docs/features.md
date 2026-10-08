# qui — Features

**English** · [中文](features.zh.md)

A catalog of what ships today. For how it is built, see [architecture.md](architecture.md). For the authoritative HTML/CSS matrix, see [../htmlcss/COVERAGE.md](../htmlcss/COVERAGE.md).

## Engine (root `qui`)

**Window and lifecycle**

- `App` owns the platform backend; `App.NewWindow` / `App.NewOverlayPanel`; `App.Run` with SIGINT/SIGTERM close handling; `App.SetKeepAlive` (tray apps that outlive their windows) and `App.Quit`.
- Close veto: `Window.OnCloseRequest` (return false to keep the window — the "save changes?" flow), `RequestClose`, unvetoable `Close`.
- Lifecycle callbacks: `OnActivate`, `OnMove`, `OnMinimize`, `OnFullscreenChange`, `OnClose` (each returns its remover); owned child windows (`SetOwner`) and sheets (`ShowAsSheet`, macOS).
- Multiple windows per app; detached windows may be constructed off-thread.
- Per-window renderer selection, device-pixel-ratio handling and content-viewport zoom.
- Window modes: normal, fullscreen, overlay panel (non-activating, transparent, always-on-top — cocoa backend).
- Client-side title bars with `TitlebarOverlay`, `TitlebarInsets` and window-manager drag.
- Monitor enumeration and geometry persistence helpers.

**Event dispatch**

- DOM-style three-phase dispatch (capture → target → bubble) with `StopPropagation`.
- Mouse capture, synthesized hover enter/leave, focus on click + Tab cycling, 4px-dead-zone drag/drop.
- Per-widget coordinate spaces with `WindowPointToLocal` / `LocalPointToWindow` / `InteractionBoundsOf`.
- Modifier abstraction (`IsCommandMod`) and named-key + text/char events.
- Gestures: pinch, rotation, two-finger double-tap; precise scroll phase and modifiers.
- Modal overlay focus trapping, top-down hit testing, and keyboard containment (keys and window accelerators don't reach the UI behind a modal).
- `Window.AddEventFilter`: see (and optionally consume) every event before dispatch — command palettes, modal key layers, macro recording.
- App-defined events: `CustomEvent` + `DispatchCustomEvent` travel capture → target → bubble like input.
- `MouseEvent.Clicks` (double / triple click count), `MouseEvent.ReleasedOver` (a press dragged off a control and released elsewhere is a cancelled click), and per-widget `OnFocus` / `OnBlur` / `OnKeyDown` hooks on every `BaseWidget`, each returning its remover. `OnKeyDown` sees Tab before focus navigation.
- Accelerators: `CmdOrCtrl` token, `Bind` / `Unregister`, conditional bindings (`BindIf` — skipped while unavailable, so the key falls through), widget-scoped bindings (`RegisterScoped`) that outrank window-wide ones and stay live inside a modal that contains them; the newest binding wins. `MenuBar.BindAccelerators` adds a menu's shortcuts to an existing registry and keeps them in sync; disabled items don't fire.
- Drag and drop with typed payloads (`DragData`, `DragDataProvider`), `DragEnter` / `DragLeave`, `DropAcceptor` accept/reject, a drag image, Escape to cancel, and OS file drops routed to `Droppable` widgets first.
- Optional focus policies: `ClickFocusPolicy` (Tab-reachable but a click keeps focus where it was) and `TabStopper` (click-focusable but skipped by Tab — selectable text).
- Overlays re-lay themselves out when their content changes (`OverlayLayouter`); `TickWidget` forwards frame ticks through non-Tickable containers.
- Overlay layers (`PushOverlayLayer`: toasts stay above a later modal, tooltips above everything) and exit animations (`OverlayExiter`).
- Optional widget contracts are named, exported interfaces: `ChildLister`, `ModalOverlay`, `FocusVisibleAware`, `WindowAware`, `LayoutDirtyMarker`; `QUI_DEBUG_WIDGETS=1` reports a missing `SetSelf`.

**Layout**

- `FlexLayout` (CSS-aligned flexbox), `GridLayout`, `FlowLayout`, `AbsoluteLayout`.
- CSS margin-box semantics in flex/container measure.
- Controlled per-child metadata: `SetFlexItem` / `SetGridItem` / `SetAbsolutePosition`.
- Custom layout engines report an intrinsic size through `LayoutMeasurer`; `Window.AfterLayout` runs code against final geometry before paint.
- Shrink-to-fit measurement; grid row spans; debug overflow overlay (`QUI_DEBUG_LAYOUT=1`).

**Rendering**

- `Canvas` frontend: Save/Restore state stack (clip + 2×3 affine matrix), `Path` with quad/cubic Béziers, anti-aliased winding/even-odd fill, stroke cap/join/miter.
- `SaveLayer` with blend modes and `Paint.Alpha`; `ClipPath`; linear/radial gradient shaders; `ColorFilter` / `ImageFilter` (drop shadow, blur); `DrawShadow`.
- `RasterBackend` seam: pure-Go CPU reference (`backend_cpu.go`) and optional GPU backend (`QUI_GPU_RASTER=1`, `backend_gpu.go`).
- Dirty-region painting, `Invalidate` / `InvalidateRect` / `InvalidateLayout` with bounds-change promotion.
- Offscreen rendering and readback; PDF canvas backend; SVG canvas helper.
- GL escape hatch: `GPUCanvas.QueueGLDraw`, `GLState`, `ActiveGLRenderer().DrawTexture`, `PhysicalScissor`.

**Text**

- `go-text/typesetting` glyph-run shaping: OpenType GSUB/GPOS, Unicode bidi, UAX #14 line breaking, UAX #29 grapheme boundaries.
- Shared shaped clusters across measurement, drawing, PDF, wrapping, hit testing, selection and editor carets.
- Inline flow (`inline.go`, `widgets.InlineBox`): text and atomic inline boxes on shared lines with baseline alignment.
- Rich styled runs (`text_rich.go`), text selection across widgets, CJK line breaking and emoji.

**Theme**

- Global design-token `Theme`: surface ladder, text roles, accent, borders, semantic colors, elevation, state opacities, spacing/radius/font/transition scales.
- `SetTheme` invalidation; live `CurrentTheme()` reads; `ThemeGeneration()` for caches; removable `SubscribeTheme`. Native controls built with baseline colors follow a theme swap. One light scheme; dark UI is CSS.
- Configurable tooltips per window (`SetTooltipStyle`: colors, font, padding, width, show delay, grace period).

**Internationalization seam**

- `Locale`, `Direction`, `Translate`, `LocaleGeneration`, `SetDefaultLocale`, `SubscribeLocale`.
- Locale-aware text-measurement cache keys; `Translator` interface plugging in the `i18n` package.

**Threading and jobs**

- `PostJob` / `TryPostJob` / `PostPriorityJob` main-thread queue; `IdleState`.
- `QUI_DEBUG_THREAD=1` fail-fast UI-thread ownership checks.
- `Tickable` / `Animator` / `Focusable` / `IMEClient` contracts; `Window.RequestTickAt` for Tickables that must wake an idle loop (on-demand frame pacing, `App.SetMaxIdleWait`); animators that stay registered without painting are throttled to a 4 Hz poll and logged.

**AI-native introspection**

- Accessibility tree with roles, names, values, bounds and message keys.
- CSS-attribute selector grammar (`#id`, `[role=]`, `[name*=]`, `[key=]`, `:nth`, `:visible`, `:focused`, `:layer(modal)`).
- Selector-targeted actions (click, type, drag, scroll, …) with modal blocking and scroll-into-view.
- Scaled / region / annotated PNG snapshots through the main-thread job queue.
- `WaitIdle` / `WaitOverlay`; event recording; in-window agent overlay on `Cmd/Ctrl+Shift+A`.

## `widgets`

- Structure: `Box`, `Container` layouts, `Rule`, `FieldSet`, `Anchor`, `ScrollView` (content size measured automatically), `ListView` (virtualized; factory rows built only while visible), `TableView` (cell renderers, column alignment, header-click sort that keeps the selected record via `RowKeyModel`, factory rows), `TabView` (icons, badges, closable and disabled tabs, fit-width tabs with an overflow-scrolling strip, switch veto). List and table colors come from the theme, overridable per field (`RowColors`; `NoStripe` / `NoHover` switch decorations off). The zero-config controls (Input, TextArea, Select, TabView, MenuBar, …) follow `SetTheme`.
- Text: `Label`, `RichText`, `InlineBox`; text selection and copy.
- Input: `Input`, `TextArea` (both with clipboard, IME and undo/redo), `CheckBox`, `RadioButton` / `RadioGroup`, `Switch`, `Slider`, `Select` (value / label options with icons and groups, custom option rows, open / close events).
- Feedback: `Progress`, `Tooltip`.
- Overlays: `Popup` (close reasons + veto, click-through, auto-focus, edge clamping; recovers when removed from the overlay stack behind its back), `Dialog` (any widgets as actions, `CanClose` veto, `OnClose(reason)`, Enter → `DefaultAction`, `InitialFocus`, custom `Header`, theme-token colors), `MenuBar` (Left / Right between menus), `ContextMenu`, `MenuItem` icons / IDs / custom content, keyboard-navigable panel rows (`MenuActivatable`).
- Media: `Image` (raster + vector).
- i18n: `TextKey`-style keys on every text-bearing widget (tabs, columns, menu items, labels, fieldset titles, tooltips) resolved in Measure/Draw, with `AccessibleNameKey()`.
- Accessibility: label-aware names, `SetAccessibleName` override, self-drawn tabs / list rows / table cells published as AX children, Tab scrolls the focused widget into view.
- Text-widget undo history (`undo.go`) and a reusable selection model.

## `htmlcss`

**HTML tags:** structural/semantic containers (`div`, `section`, `article`, `header`, `footer`, `nav`, `main`, `aside`, `p`, headings, lists, `table`/`tr`/`td`/`th`/`caption`/`colgroup`, `pre`, `code`, `blockquote`, `hr`, `br`, `span`, `b`, `em`, `strong`, `i`, `u`, `a`, `img`, `svg`, `canvas`, `input` variants, `textarea`, `select`/`option`, `button`, `label`, `fieldset`, `datalist`, `template`). Unsupported tags are parsed and skipped.

**CSS:** the full selector set including interactive states (`:hover`, `:focus`, `:focus-visible`, `:checked`, `:disabled`, `:enabled`, `:required`, `:optional`, `:read-only`, `:read-write`), `var()` + `:root`, shorthands, the box model with per-side borders, background color/gradient, box-shadow, opacity, transform, `position:relative`, overflow (`auto`/`scroll` hosted by a `ScrollView`), `display:flex`/`grid`, list markers, text-decoration/transform/overflow, white-space, `overflow-wrap`/`word-break`, `scrollbar-color`. Native popups (select, datalist, color palette, tooltips) follow `--popup-*` / `--tooltip-*` custom properties.

**Extension points:** `RegisterElement` (custom tags backed by native widgets), `RegisterProperty` + `ComputedStyle.Property` / `Var` (CSS-wide keywords included), `El.SetOnStyle`, style-aware canvas painting (`SetCanvasPaint`), `StyleEngine.SetLinkHandler` (told which `<a>` was clicked, folded or not). `h.Mount` styles its first pass before that pass's effects run.

**Events and interaction:** click/double-click (with position and `PreventDefault`), pointer down / move / up with capture, hover, focus (`El.RequestFocus`, `autofocus`; `<button>` is Tab-reachable and presses on Enter/Space), keyboard (author handlers act on target/bubble), `pointer-events:none`, wheel, drag-reorder (`Draggable`/`DragHandle`/`OnDrop`/`OnDragOver`), element-level file drops and paste hooks, `<a>` link activation, `app-region: drag|no-drag`.

**Two entry points, one assembly:** the live `El` + `StyleEngine` (retained, subtree-scoped restyle) and the one-shot `Render` / `RenderDoc` that compiles a parsed DOM into a static but still mutable `El` tree.

**Known gaps:** CSS transitions/animations, `display:none` toggling at runtime in static `Render`, native date/time pickers, real validation pseudo-classes, `::placeholder`.

## `reactive` and `reactive/html`

- Reconciler for structure: `UseState`, `UseReducer`, `UseRef`, `UseMemo`, `UseCallback`, `UseEffect(Once)`, `UseLayoutEffect` (after layout, before paint), `UseId`, `UseResource` (async load with cancellation), `UseSignal`, `UseContext`; keyed lists; error boundaries; portals, including anchored popovers (`PortalAnchored`).
- Signal engine for high-frequency updates: `Signal[T]`, `Map`/`Computed`, `BindWidget`, `Show`/`For` bound nodes — all skipping the render pass.
- `reactive/html`: fluent builders for every common tag, chainable props under HTML names, `h.If`/`h.Show`/`h.For`/`h.Each`/`h.Frag`, overlays (`h.Portal`/`h.ModalPortal`/`h.ModalPortalWith`/`h.Popover`/`h.ContextMenu`), refs that track the latest render (`.Ref`, `.RefTo`), `h.Canvas`, `h.Leaf` (a native widget hosted in a styled element), pointer / click / file-drop / paste / form-submit builders, the `h.Dialog` shell (header / body / actions with `.q-dialog-*` classes and framework-origin CSS), `h.Mount`. Portal content cascades from where it is declared (`.app.dark .q-dialog` matches; custom properties inherit).
- HTML components: `h.MustParse` / `MustParseSet` compile markup fragments; `Template.Bind(Scope)` fills them; signal-valued holes bind instead of interpolating; errors carry the template line number.

## `i18n` + `cmd/qui-i18n`

- Message catalogs (JSON) with CLDR plural categories via `golang.org/x/text`.
- Locale-sensitive formatting: number, percentage, currency, date, relative time, list joining, collation.
- `i18n.Install` (built-in `qui.*` strings), `i18n.Load` (app catalogs), `i18n.In(loc)` printer.
- CLI: `extract` (scan source, update catalog without overwriting translations), `lint` (missing/unused keys, placeholder drift, plural shape), `pseudo` (accented +40% pseudo-locale).

## `agent` + `cmd/qui-agent`

- HTTP + SSE server over the engine's introspection primitives; Unix socket by default, TCP + bearer token opt-in.
- Endpoints: `GET /tree`, `/screenshot`, `/diagnostics`, `/health`, `/wait`, `/console`, `/events` (SSE), `POST /act`, plus `/llm.txt` and the DOM/reactive surfaces (`/dom`, `/dom/styles`, `/dom/act`, `/reactive`).
- `agent.BindEnv(window)` env-gated binding; `cmd/qui-agent` CLI (`tree`, `click`, `rightclick`, `type`, `wait`, `pinch`, `shot`, `raw`).

## Auxiliary packages

| Package | Highlights |
|---|---|
| `anim` | `Tween[T]`, `Spring`, `Timeline`; easing catalog; satisfies `qui.Animator` |
| `graphs` | Line / Scatter / Bar / Area / Pie series, value + category axes, legend, tooltip, pan/zoom, theme palette |
| `svg` | SVG 1.1 subset: parse/build/rasterize (with tint)/serialize; `Document` satisfies `qui.VectorSource` |
| `scene3d` | Scene graph (nodes, mesh, material, lights, camera), `Viewport` FBO widget, orbit controls, picking, `Vec3Field` |
| `media` | `AudioPlayer` (service) and `VideoView` (widget) without ffmpeg; AVFoundation backend on macOS |
| `webview` | CEF-based embedded browser widget, gated behind `webview_cef`; CPU frame path working, GL/IOSurface path future; JSON-only JS↔Go bridge |
| `physics` / `p2` / `p3` | Dimension-independent vocabulary plus mirrored 2D/3D engines: swept AABB, spatial hash, contact flags, one-way platforms, sensors |
| `icons` | ~135 Material Symbols outlined glyphs as `*svg.Document` |
| `fonts/jetbrainsmono` | Embedded JetBrains Mono; `Use()` swaps the global default family |

## Tooling

- `cmd/qui-agent` — agent CLI client.
- `cmd/qui-i18n` — catalog extraction, linting and pseudo-locale generation.
- `examples/` — runnable demos, including the canonical `reactive-html`, the htmlcss showcase (`html-css`), i18n, chrome-tabs, overlay-panel, graphs, svg, media, scene3d and more.
