# Extensibility & completeness audit

**English** · [中文](extensibility-audit.zh.md)

An audit of the places where an application built on qui, in its own Go module, runs out of road: it has to fork a widget, copy unexported code, or live with a behavior it can't change. Three areas were covered: the engine seams (root package), the native `widgets`, and the app-dev surface (`htmlcss` + `reactive` + `reactive/html`). It leaves out what is out of scope by design: non-macOS native integrations, and the non-goals listed in [htmlcss/COVERAGE.md](../htmlcss/COVERAGE.md).

The trigger was a concrete one: customizing a dialog's buttons from another project took far more work than it should have. The first round below fixes that, along with the bugs found on the way. The roadmap that follows is the rest, in priority order. References are `file` + symbol rather than line numbers, so they survive edits.

## Round 1: done

### Engine (root)

| Problem | Fix |
|---|---|
| A container that implements `Tickable` only ticked direct `Tickable` children, so a `ScrollView` (or any third-party wrapper) in a `Dialog` froze carets and hover transitions below it. | `qui.TickWidget` is exported. `Dialog`, `ListView` and `TableView` recurse through it. |
| Overlays were never re-laid out. Content that grew while shown kept its show-time geometry, and theme / locale switches didn't reach open popups. | New `OverlayLayouter` interface. `Step` re-lays out layout-dirty overlays, and `Window.InvalidateLayout` marks overlays too. `Dialog` and `Popup` implement it. |
| The overlay paint pass culled by `Bounds()`, so shadow halos were left un-repainted. | Culls by `PaintBoundsOf`. `Popup` reports its content halo. |
| Keys leaked past a modal: with nothing focused they went to the main root, and window accelerators (Cmd+S) fired behind a confirm dialog. | Keys aimed behind the top modal are retargeted to it. Accelerators wait while a modal is up. |
| Clicking a focusable push button stole focus from the text field it acts on. | New `ClickFocusPolicy` interface (`FocusOnClick() bool`). |
| Every selectable text label was a Tab stop. | New `TabStopper` interface: `Label`, `InlineBox` and the portal host stay click-focusable but are skipped by Tab. |
| `font-family: sans-serif` (any unregistered family) rendered bold text as regular. | An unknown family falls back to the default family at the requested weight. |

### `widgets`

| Problem | Fix |
|---|---|
| `Dialog.Buttons []*Button`; `AddButton` always closed the dialog; title was string-only; Esc always closed; no default button; colors were literals copied at construction; content painted over the actions. | Redesigned `Dialog`: `Actions []Widget`, `AddAction`, `CanClose` veto, `OnClose(reason)`, `DefaultAction` (Enter), `InitialFocus`, `DismissOnBackdrop`, `ActionsAlign`, `Header` slot, `SetTitleKey` + `AccessibleNameKey`, exported `DialogMetrics`. Colors resolve from theme tokens at draw time, and the content slot is clipped. |
| `Input` swallowed Enter and Escape even with nothing to do with them, so a dialog's default action and Escape were dead while a field had focus. | Enter bubbles when there's no `OnSubmit` (HTML implicit submission). Escape bubbles when there's no selection to drop. |
| `Slider` had no programmatic setter. | `Slider.SetValue`. |

### `htmlcss` / `reactive` / `reactive/html`

| Problem | Fix |
|---|---|
| Every dialog hand-rolled its title and button row. | `h.Dialog(DialogProps)`: a header (title + optional ×), body and action row with stable `.q-dialog-*` classes. Esc / backdrop / × dismiss; unconsumed Enter confirms. |
| The scrim and the dismiss gestures were hard-coded in `h.ModalPortal`. | `h.ModalPortalWith(ModalOptions)` takes the scrim, backdrop / Escape / Enter callbacks and an accessible label. `reactive.PortalOptions` gains `OnEnter`, `Role` and `Label`. |
| A framework component had nowhere to put a default look that the app could still override. | `htmlcss.RegisterFrameworkCSS`: class-keyed rules at UA origin (cascade tier 0), so any author rule beats them. |
| The portal took Escape in the capture phase, and `El` key handlers ran during capture, so an ancestor beat the focused element to its own keys. | Both act on target / bubble only (DOM order). |
| `<button>` was not focusable, and Enter / Space did nothing. | Push buttons are Tab-reachable and press on Enter / Space. A click doesn't move focus. |
| There was no imperative focus and no `autofocus`. | `El.RequestFocus()`, the `autofocus` attribute, and `Builder.Autofocus()`. |
| Portal content was its own style root: `.app.dark .dialog` never matched and custom properties didn't reach it, so dialogs stayed light in dark mode. | `El.SetStyleParent` links portal content to the element that declared it (a style-only parent: ancestor selectors and inheritance work, sibling selectors see an only child, `:root` no longer matches it). The reconciler wires it up. |
| Centered portal content taller than the window pushed its title off screen. | The top-left edge is clamped onto the screen. |

## Roadmap

Priority: **P1** blocks a common app pattern, **P2** is a real gap with a workaround.

### Engine (root)

- **P1 — Custom layouts can't report an intrinsic size.** `Layout` only has `Apply`. `Container.Measure` type-asserts the three built-in engines, by value, so `&FlexLayout{}` misses too, and any other engine measures as "all available space". A custom masonry layout inside a `ScrollView` or `Popup` sizes to the viewport. *Fix:* add an optional `Measurer` interface on `Layout`. (`layout.go` `Layout`; `container.go` `Container.Measure`)
- **P1 — No window-level event interception.** `AddEventListener` only observes. A root capture handler doesn't see events whose target is inside an overlay. Command palettes, Vim modes and macro recording have no hook. *Fix:* add `Window.SetEventFilter(func(Event) bool)`, run before dispatch. (`window.go` `dispatch`; `recording.go`)
- **P1 — Accelerators.** No `Unregister`; the first registered match wins; no scoping (a panel or document); no `CmdOrCtrl` token. (`accelerator.go`)
- **P1 — Window close can't be vetoed.** The comment in `Window.OnClose` points at a `SetShouldClose` that doesn't exist; `runCloseHandlers` destroys the window right away. (`window.go`, `app.go`)
- **P2 — Event set is closed.** `Event` has an unexported method and `EventType` is an enum, so there are no app-defined bubbling events. (`event.go`)
- **P2 — Drag-and-drop.** No payload or MIME type, no `DragEnter` / `DragLeave`, no accept / reject, no drag image. OS file drops are window-level only and aren't routed to `Droppable` widgets. (`event.go` `DragEvent`; `window.go` drag handling; `filedrop.go`)
- **P2 — Window lifecycle.** No activate / deactivate / move / minimize callbacks. No sheet, child or owned windows. No "keep running with no windows" for tray apps. (`window_overlay.go` `WindowKind`; `app.go`)
- **P2 — Theme.** Process-global. There is no `ThemeGeneration()` for caches. `SubscribeTheme` isn't tied to widget lifetime and its slice never shrinks. Many widgets copy resolved colors at construction (`DefaultStyle`, `defaultButtonStates`), so `SetTheme` leaves them unchanged. (`theme.go`, `style.go`, `widgets/basic.go`)
- **P2 — Focus listeners and overlay layers.** `AddFocusChangeListener` can't be removed. Overlays are a single stack with no layers (a toast can't sit above a later modal) and no exit-animation hook before `RemoveOverlay`. (`focus.go`, `window.go`)
- **P2 — Optional hooks are only reachable by duck typing.** `Modal()`, `SetFocusVisible()` and `ChildList()` are satisfied structurally but documented nowhere public; `markLayoutDirty` is unexported. A missing `SetSelf` silently breaks hit testing. Consider a debug assertion under `QUI_DEBUG_*`. (`focus.go`, `widget.go`, `container.go`)

### `widgets`

- **P1 — `Select` options are `[]string`.** No value vs label, no icons, groups, renderer or open / close events. `SetText` matches on the translated label. (`select.go`)
- **P1 — `ListView` / `TableView` colors are hard-coded dark** (header, selection, hover, stripes, scrollbar), as literals inside `Draw` and the constructors. They should read theme tokens or exported fields. (`listview.go`, `tableview.go`, root `scrollbar.go`)
- **P1 — `TableView` cells are text-only.** No cell renderer, column alignment or header-click sort. Widget-row mode isn't virtualized, so every row widget is created up front. (`tableview.go`, `listview.go` `SetRowWidgets`)
- **P1 — `TabView`.** Titles only (no icon, close button, disabled state or badge); equal-width tabs with no overflow; strip height is a private constant; no veto on switching. (`tabs.go`)
- **P1 — Menus.** `menuActivatable` has an unexported method, so custom rows can't join keyboard navigation. Panel rows are skipped by arrow keys. `MenuItem` has no icon or key. Left in a submenu closes the whole chain; Left / Right don't switch top-level menus. (`menu.go`)
- **P1 — `Select` / `MenuBar` capture the `*Window` at construction.** Build the tree first and attach later, and they silently never open. *Fix:* fall back to `Window()`. (`select.go`, `menu.go`)
- **P2 — `MenuBar` doc points at a nonexistent API.** `mb.Menus()[i].Trigger()` doesn't exist, and trigger styling is copied once in `AddMenu`. (`menu.go` `AddMenu`)
- **P2 — `Popup`.** No close reason or veto; an outside click is always swallowed (no click-through); `ShowAt` doesn't move focus into the content or clamp to the window edges. (`popup.go`)
- **P2 — No click count on `MouseEvent`.** `ListView` / `TableView` `OnActivate` is Enter-only, so double-click-to-open has to be timed by hand. (root `event.go`)
- **P2 — Native widgets have no `OnFocus` / `OnBlur` / `OnKeyDown`** (htmlcss `El` has them). An autocomplete on an `Input` has to wrap it and override `Handle`. (`input.go`)
- **P2 — i18n gaps** (against the `TextKey` rule in CLAUDE.md): `Tab.Title`, `TableColumn.Title`, `MenuItem.Label` / `AddMenu`, `RadioButton.Label`, `Input.Label` / `Select.Label`, `FieldSet` title, `SetTooltip`.
- **P2 — Accessibility.** `Input` / `Select` `AccessibleName` ignores `Label`; there is no name override. Tabs and model-mode rows expose no `AccessibleChildren`. Tab navigation doesn't scroll the focused widget into view. `ScrollView.ContentSize` is manual. (`accessibility.go`, `scroll.go`)
- **P2 — Fixed geometry.** `FieldSet`'s title height and font are fixed; the built-in tooltip view isn't configurable (padding, font, delay). (`frame.go`, root `tooltip.go`)

### `htmlcss` / `reactive` / `reactive/html`

- **P1 — Native-backed popups can't be styled from CSS.** The `<select>` dropdown, `title` tooltips, scrollbars, the datalist popup and the color palette read the process-global Go theme, so `SetCSS` / `:root` variables don't reach them, and a dark stylesheet leaves them light. (`widgets/select.go`, root `tooltip.go`, `widgets/scroll.go`, `datalist.go`, `el.go` color popup)
- **P1 — No pointer down / move / up events** in the DSL (already a known gap in COVERAGE), so splitters, custom sliders and marquee selection aren't possible. `OnClick` carries no position or button. There is no `preventDefault` for built-in behavior (a submit button submits alongside the author's `onClick`).
- **P1 — Effects run before layout.** `UseEffect` sees stale or zero `Bounds()`. A `UseLayoutEffect` is needed to position popovers, scroll to new rows, and measure.
- **P1 — Popover positioning.** `PortalAlign` has only Center / Fill / AtPosition. There's no element-anchored popover that follows the window and flips; only native menus have `AnchorMenu`. (`reactive/portal.go`)
- **P2 — `Ref` fires once at creation.** Callbacks installed through it keep their first-render closure, and there's no detach callback. `OnFormSubmit` and canvas drawing have no Builder method. (`reactive/html/html.go`)
- **P2 — `h.Leaf` (native widget) isn't in the node mirror.** Classes, margins and flex don't apply to it, and `:nth-child` / `+` skip it. There's no `h.Canvas`. (`reactive/html/html.go`)
- **P2 — Links folded into inline text call `qui.OpenURL` directly.** There's no app hook for in-app routes (`#/settings`), and that's a concern with untrusted content. (`el.go` click handling)
- **P2 — No extension points for custom tags or custom CSS properties** mapped to native setters. (`htmlcss`)
- **P2 — Custom-drawn content can't read computed style.** `SetCanvasDraw` content has no access to `color` / `--vars`.
- **P2 — No `UseId`, and no async resource primitive** (loading / error state with unmount-safe completion).
- **P2 — `:focus-visible` is treated as `:focus`.** An author focus ring therefore also shows after a click, and a push button with a `:focus` rule opts back into click focus. (`css.go`)
- **P2 — Files and paste are window-level only.** There's no element-level drop zone or paste event.

## How round 1 was verified

- **Unit tests** for each change: `overlay_modal_test.go`, `widgets/dialog_actions_test.go`, `reactive/html/dialog_test.go`, `font_test.go`. `go test ./...` passes.
- **Live checks** through `cmd/qui-agent` against `examples/dialog` and `examples/reactive-html`, covering:
  - validation that keeps the dialog open;
  - Enter-to-save from a focused field, and Escape;
  - Tab cycling × → Cancel → Delete;
  - the dialog following `.app.dark`.
- **Downstream:** q-office builds and its `cmd/q-excel` tests pass against this tree.
