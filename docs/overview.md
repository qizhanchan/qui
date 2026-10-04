# qui — Overview

**English** · [中文](overview.zh.md)

## What qui is

`qui` is a retained-mode Go GUI framework. It builds desktop applications from a persistent widget tree and renders them with its own rasterizer — a pure-Go CPU reference backend by default, plus an optional GPU backend behind `QUI_GPU_RASTER=1`. The only system dependencies are GLFW and an OpenGL 3.3 context for the window and presentation layer.

The whole project is one Go module, `github.com/qizhanchan/qui`. The root `package qui` is the engine; everything else is an optional subpackage.

There are two ways to build a UI with qui:

1. **Native widgets** — compose `widgets.Button`, `widgets.Input`, `widgets.ListView`, … inside the layout and container primitives. Direct, debuggable, no translation layer.
2. **HTML + CSS + Go** — describe the interface in lightweight markup and stylesheets (`htmlcss`), and drive it with a declarative runtime (`reactive` + `reactive/html`). This is the strategic direction, because it gives application developers a familiar, high-level, model-friendly surface while the framework underneath stays a retained widget engine.

Both paths compile to the same widget tree, so they can be mixed freely.

## The problem it targets

Most Go GUI options sit at one of two extremes. Immediate-mode libraries are small and simple but push state management, layout and styling into user code. Native toolkit bindings are capable but lock the application to one platform's API and require the developer to learn a large, imperative object model.

qui takes the retained-mode position — declare a stable tree, and let the framework handle input routing, layout, invalidation and painting — but:

- **keeps the core small and dependency-light**, with optional packages for charts, SVG, 3D, media, physics and internationalization;
- **uses HTML + CSS as the high-level application surface**, so the most common UI work is markup and styles, not imperative Go;
- **treats AI operability as a first-class engine feature**, not an add-on: every app is inspectable and drivable by semantic selector out of the box.

## Design principles

**Retained-mode and invalidation-driven.** Nothing paints unless something was invalidated. `Invalidate` / `InvalidateRect` cover pure visual changes; `InvalidateLayout` covers size-affecting changes and is scoped to the caller's paint extent, with a full repaint promoted only if a widget's bounds actually moved. This makes idle frames genuinely free and lets `WaitIdle` work against continuously-updating apps.

**Strict import direction.** Subpackages import the root engine; the root never imports a subpackage. Coupling goes through small interfaces defined in root (`qui.Animator`, `qui.IMEClient`, `qui.VectorSource`, `qui.Translator`). `widgets` imports nothing but root; `reactive` imports only root and is backend-agnostic. This keeps the engine independent of optional features and prevents dependency cycles.

**CSS is the styling layer.** The engine ships one light theme of design tokens, deliberately design-system agnostic. A designed look is a stylesheet, not a token package.

**Resolve late.** Because the tree is retained, a value captured at construction freezes. Text, translations and message keys are therefore resolved inside Measure/Draw (and `data-i18n` during restyle), so a locale switch costs one relayout rather than a rebuild.

**AI-native by default.** The accessibility tree, selector grammar, snapshots, actions, waits and event recording live in the root engine and are always available. The `agent` package only exposes them over HTTP.

**One scheme, no bundled design system.** qui is not a browser and not a widget library clone; `htmlcss` is a deliberate subset aimed at application layout, not documents.

## Architecture at a glance

```
                 application code
        ┌──────────────┴──────────────┐
        │                             │
   native widgets              htmlcss + reactive/html
   (widgets package)           (markup + CSS + Go DSL)
        │                             │
        └──────────────┬──────────────┘
                       │
              root qui engine
   frame loop · event dispatch · layout engines
   Canvas / RasterBackend · text shaping · theme
   locale seam · AI introspection
                       │
             platform seam (platform.go)
          ┌────────────┴────────────┐
       GLFW backend             Cocoa backend
```

Optional packages — `scene3d`, `graphs`, `svg`, `media`, `webview`, `physics`, `anim`, `i18n`, `icons`, `fonts` — plug in through root interfaces without the engine ever depending on them.

## Platform support and status

- **macOS** is the fully supported target. Both the GLFW backend and the native AppKit (`cocoa`) backend are compiled in and selected at runtime; they produce identical geometry.
- **Linux / Windows** compile and run the core. Native dialogs, IME, emoji and system-tray integration are macOS-only today; the platform seam returns `ErrNotSupported` rather than failing silently.
- **Rendering** defaults to the CPU rasterizer. The GPU backend is opt-in and interleaves with the CPU path per primitive.
- **Text** is shaped with `go-text/typesetting` (OpenType GSUB/GPOS, Unicode bidi, UAX #14 / #29) and is shared by measurement, drawing, PDF output, wrapping, hit testing, selection and editing.
- **Internationalization** supports message catalogs, CLDR plurals, locale-sensitive formatting and full bidi text. Layout mirroring (RTL) is not implemented yet.

## Where to go next

- [architecture.md](architecture.md) — how the engine works internally.
- [features.md](features.md) — the feature catalog.
- [../README.md](../README.md) — the package map and quick start.
- [../reactive/README.md](../reactive/README.md) and [../htmlcss/COVERAGE.md](../htmlcss/COVERAGE.md) — deep dives on the app-dev surface.
