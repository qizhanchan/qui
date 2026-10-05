# reactive

A declarative UI runtime built on top of qui's **retained-mode** widget tree. The goal is to write complex business interfaces in as little code as possible — using the mental model React has proven, without inheriting the flaws of React's engine.

In one line: **the API is React-shaped; the engine is Solid-shaped.**

**English** · [中文](README.zh.md)

---

## 1. Why it exists, and why it looks like this

qui's bottom layer is a retained tree plus dirty-region repaint: widgets are stable Go objects, nothing is repainted unless invalidated, and changing one Label repaints one rectangle. That layer is actually a better fit for fine-grained reactivity than the browser DOM — widget identity is naturally stable, a setter is a method call, and invalidation is `InvalidateRect`.

But "hand-write a widget tree and manually add/remove children" is far too verbose for business interfaces. React's declarative paradigm (`UI = f(state)`) removes that verbosity, at the cost of "every state change re-runs render from the root and then diffs". Copying that directly fights qui's underlying design: an upper layer doing full recomputation to drive a lower layer built for precise invalidation.

So this package splits into **two engines, each owning one job**:

| Engine | Owns | Trigger | Cost |
|------|--------|----------|------|
| **Reconcile** | structural change (mount/unmount, list add/remove) | `UseState` / component setState | one scoped tree diff |
| **Signal** | high-frequency value change + fast path for structural change | `Signal.Set` | direct widget setter / local diff, **no render** |

When writing a UI: use the Reconcile engine (familiar React style) for skeletons and conditional structure, and the Signal engine for high-frequency updates (clocks, progress, drag values, large lists) to bypass the VDOM.

---

## 2. Layering

```
reactive/            engine: Element / Reconciler / Runtime / hooks / signal (depends only on root qui, backend-agnostic)
reactive/html/       h DSL: HTML element builders lowering to Elements backed by htmlcss.El, styled with plain CSS
```

- The `reactive` engine depends on no backend — the host's Create/Update/SetChildren hooks are supplied by the layer above. `reactive/html` is the current official backend (the htmlcss live element); the backend's own state (style engine) is resolved per runtime via `Runtime.SetHostData` / `reactive.CurrentRuntime()`, which supports multiple windows.
- It strictly obeys qui's import direction: the root never depends back on it.

A minimal program (`import h "github.com/qizhanchan/qui/reactive/html"`):

```go
rt := h.Mount(window, `.title { font-size: 20px }`, func() h.Node {
    n, set := reactive.UseState(0)
    return h.Div(
        h.H1("Counter").Class("title"),
        h.Button(fmt.Sprintf("count: %d", n)).OnClick(func() { set(n + 1) }),
    )
})
```

---

## 3. The Reconcile engine

### 3.1 Element and instance

An `Element` is a **declaration** (a one-shot, value-typed description). An `instance` is **retained state** (long-lived after mount, holding the real widget, hook slots and children). Each `render` produces a fresh Element tree; the Reconciler compares it against the previous instance tree and makes the minimal change.

There are several element kinds. Besides an ordinary host (one real widget), there are "widget-less" special nodes whose children **splice** into the nearest host ancestor:

| kind | role |
|------|------|
| host | one real widget (`Node`/`Leaf` constructors) |
| component | `Component[P](name, key, props, render)`, with its own hook state |
| `Fragment(...)` | several siblings, no wrapping container (React `<>...</>`) |
| provider | `ctx.Provide(value, child)`, Context injection |
| portal | `Portal(child)`, mounts onto the window overlay stack |
| boundary | `ErrorBoundary(...)`, catches subtree render panics |
| bound | `Show`/`For`, signal-driven structure (see §4.3) |
| `Empty()` | conditional placeholder slot |

### 3.2 Keyed diff and conditional rendering

Children are matched and reused by `(kind, key)`. Give list rows a stable `Key` so add/remove/reorder reuses widget instances instead of rebuilding.

`h.If(cond, node)` degrades to `h.Nothing()` (i.e. `Empty()`) when false. **Crucially**, the Reconciler keeps `Empty()` as a nil placeholder slot in the sibling sequence, so toggling the condition mounts/unmounts only that one subtree and the keyless siblings before and after do not shift (React null-child semantics).

```go
h.Div(
    header,
    h.If(expanded, detailPanel), // keeps a nil placeholder while collapsed
    footer,                      // not remounted when `expanded` toggles
)
```

### 3.3 Components are not memoized by default; memo must compare explicitly

A `Component` re-runs whenever its parent renders, so freshly generated callback closures are installed each time and render-local state cannot get stuck in an old handler. Only the component's own `setState` is a local update.

Use `MemoComponent` when optimization is actually needed, and supply a **type-safe comparator**:

```go
reactive.MemoComponent("Row", key, props,
    func(previous, next RowProps) bool {
        return previous.ID == next.ID && previous.Title == next.Title
    },
    renderRow,
)
```

The comparator *is* the memo contract: it must cover everything that affects rendered output and handler semantics. Go function values have no usable closure equality — a code address does not include captured values — so the framework no longer tries to infer it. `valuesEqual` is used by Signals, hook deps and Context and follows the safe rule: non-nil functions are never equal.

### 3.4 Scoped re-render: childDirty + visitDirty

When a component's props did not change and it is not itself dirty, it bails out (skips re-render). But if a component **below** it called setState, returning the old subtree directly would freeze that dirty component.

The fix: every fiber carries a `parent` chain and a `childDirty` flag. On setState, walk the parent chain marking `childDirty` up to the root; a bailout that sees `childDirty` runs `visitDirty`, walking the instance subtree and re-rendering only the genuinely dirty fibers, re-syncing host child widget lists along the way. (The mark walk **never returns early** — an ancestor that was cleared would cut the chain for later passes.)

### 3.5 Child sync is by widget list, not instance pointer

`syncChildWidgets` collects the widget list a host node ultimately contributes (flattening through component/fragment/provider), and only calls `SetChildren` when that **flat widget list actually changed**. This way, a reused component instance whose root widget kind changed still propagates correctly to its parent container.

---

## 4. The Signal engine ("Solid engine")

### 4.1 Signal

```go
s := reactive.NewSignal(0)
s.Get()              // read (registers a dependency during auto-tracking)
s.Peek()             // read without registering
s.Set(1)             // write; equal values do not notify
s.Update(func(v int) int { return v + 1 })
unsub := s.Subscribe(func() { ... })
```

An equal `Set` does not notify (compared with `valuesEqual`).

### 4.2 Direct property binding: `BindWidget`

```go
count := reactive.NewSignal(0)
text  := reactive.Map(count, func(n int) string { return fmt.Sprintf("n=%d", n) })

h.Span("").BindText(text)     // signal changes → SetTextContent directly, no render, no reconcile
```

`BindWidget(widget, signal, apply)` is the core: a signal change is marshaled to the window main goroutine via `PostJob`, calls the widget setter directly (bursts coalesce — only the latest value is applied per frame), then rides qui's native dirty-region repaint. **The VDOM is never involved.**

The h layer wraps this: `.BindText` (element text) and `.BindClass` (CSS class switch, with restyle). Unsubscription is wired automatically through `Element.Destroy` (the unmount hook captured at mount).

**Iron rule:** a signal's identity must be stable for the widget's lifetime. Either create it outside render, or use `UseSignal` (memoized per fiber; `Set` on it does not re-render the component). Do **not** build a fresh `Map(...)` chain inside render every time — it leaks subscriptions.

### 4.3 Direct structural binding: `Show` / `For` (Solid's `<Show>` / `<For>`)

```go
visible := reactive.NewSignal(false)
h.Show("hint", visible, func() h.Node { return h.Span("now you see me") })

todos := reactive.NewSignal([]todo{...})
h.For("rows", todos, func(i int, t todo) h.Node {
    return h.Li(t.Text).Key(strconv.Itoa(t.ID))
})
// container layout options go through h.ForWith(key, reactive.BoundLayout{...}, todos, render)
```

A signal change coalesces into one main-thread job that runs **one local reconcile of just that node's subtree** (reusing keyed diff) — the root render function and the rest of the tree never run.

Two details make or break this:

1. **setState inside a row must not die:** the bound node captures `parentFiber` at mount, and the local pass uses it as the stack base, so a row fiber's `childDirty` chain connects back to real ancestors and a full pass's `visitDirty` can find it.
2. **`Context.Use` inside a row must not go stale:** a local sync does not descend from the root, so the provider stack would be empty. Therefore mount and every regular pass snapshot the provider stack, and a local sync restores it.

Nodes touched by a local sync count toward the profiler totals but **do not increment the render-pass counter** — assert "zero reconcile" by checking that `rt.Profile().Renders` is constant.

### 4.4 Derived: `Map` / `Computed`

```go
double := reactive.Map(base, func(n int) int { return n * 2 })

// zero deps = auto-tracking: every Get inside fn registers as a dependency
total := reactive.Computed(func() string {
    return fmt.Sprintf("%d/%d", done.Get(), all.Get())
})
```

`Computed(fn)` with no deps does **automatic dependency tracking** (Solid style): every `Signal.Get` during fn is registered, and **re-collected on each recompute** — a computed hidden in an `if` branch only listens to the branch actually taken, and dropped dependencies unsubscribe automatically. `Signal.Peek` reads inside fn without registering. Passing explicit deps switches to fixed subscription and no Get interception.

`Map` and `Computed` still return an ordinary `*Signal[T]`, but with `Dispose()`: it removes all upstream subscriptions (including dynamic auto-tracked ones) and may be called repeatedly. Derived signals created for temporary pages, dynamic components or switched data sources should be `Dispose()`d when their owner ends; application-lifetime derived values usually need no handling.

Implementation: a global atomic tracker slot — with no tracking a `Get` costs one extra atomic load (lock-free fast path); collection during tracking is lock-protected, so unrelated goroutines reading concurrently at worst produce a benign redundant dependency, never a race.

---

## 5. Hooks

Positional per fiber (call order must be stable or it panics — just like React, do not put them in an `if`):

| Hook | Description |
|------|------|
| `UseState(init)` | `(value, set)` |
| `UseStateFn(init)` | `(value, set, update)`; `update(func(prev) next)` is based on the latest value, burst-safe |
| `UseReducer(reducer, init)` | `(state, dispatch)`; dispatch is based on the latest state |
| `UseRef(init)` | stable `*T`; mutating it does not render |
| `UseMemo(fn, deps...)` | memoize by deps |
| `UseCallback(fn, deps...)` | stable function identity |
| `UseEffect(fn, deps...)` | run a side effect after commit; return a cleanup |
| `UseEffectOnce(fn)` | run once on mount; cleanup on unmount |
| `UseSignal(init)` | per-fiber memoized Signal, satisfying the stable-identity requirement for direct binding; `Set` does not re-render |

Context:

```go
var ThemeCtx = reactive.NewContext("theme", defaultTheme)
ThemeCtx.Provide(dark, subtree)   // provide high in the tree
theme := ThemeCtx.Use()           // consume at any depth (subscribes; marks consumers dirty on change)
```

---

## 6. Other capabilities

- **Fragment** (`reactive.Fragment` / `h.Frag`): a component returns several siblings that participate directly in the parent layout, with no wrapping container.
- **Portal** (`h.Portal` / `h.ModalPortal(onDismiss, node)` / `h.ModalPortalWith(opts, node)`): declarative overlay. `h.If(open, h.ModalPortal(...))` is a modal with a scrim, click/Esc dismissal and a focus trap; `ModalPortalWith` sets the scrim, which gestures dismiss, and an `OnEnter` default action. Escape and Enter are handled on the way back up, so focused content (a textarea's newline, an inline editor's cancel) gets them first. Portal content cascades from where it is declared — it inherits custom properties and matches ancestor selectors like `.app.dark .x` even though it lives in the overlay stack. Overlay invalidation is handled locally in the portal and does not bubble to the main tree.
- **Dialog** (`h.Dialog(h.DialogProps{Title, Body, Actions, OnDismiss, OnConfirm, CloseButton, Class})`): the ready-made dialog shell on top of `ModalPortalWith` — `div.q-dialog > header.q-dialog-header(.q-dialog-title, .q-dialog-close) + .q-dialog-body + footer.q-dialog-actions`. Its look is framework-origin CSS that any author rule overrides; `--q-dialog-bg/-fg/-radius/-shadow` theme it. Buttons are Tab-reachable and press on Enter/Space; give the opening field `.Autofocus()`.
- **ErrorBoundary** (`reactive.ErrorBoundary(key, child, fallback)`): catches **render/reconcile panics** in a subtree (which would kill the process in a Go GUI), switching to a fallback + retry. A panic in the fallback itself propagates; panics in event handlers/effects are out of scope.

---

## 7. Quick reference

**h DSL** (`reactive/html`, import alias `h`)

```
container   Div / Section / Header / Footer / Nav / Main / Ul / Ol / Form
text        Span / P / H1..H6 / Li / Button / Label / A(text, href)
forms       Input() / Checkbox() / Textarea() / Select(items...)
              → .Value/.OnInput/.OnSubmit/.Checked/.OnToggle/.Options/.Selected/.OnSelect
common      .Class/.ID/.Key/.Attr/.Text/.Children/.OnClick/.OnContextMenu/.Icon
drag        .Draggable(key)/.DragHandle/.OnDrop/.OnDragOver/.OnDragEnd
condition   If / Nothing / Frag / ForEach / El(rawElement)
signals     .BindText/.BindClass; Show(key, sig, build) / For(key, sig, render) / ForWith
overlay     Portal / ModalPortal(onDismiss, node) / ModalPortalWith(opts, node) / Dialog(props)
            ContextMenu(window, x, y, items)
focus       .Autofocus(); El.RequestFocus()
```

**Styling rule:** everything goes through CSS classes (the stylesheet passed to `h.Mount`). State-driven styling switches the class name via `.BindClass`, or changes `.Class` through setState — there is no inline style prop.

---

## 8. Mental model summary

- **Skeleton / conditional structure:** ordinary render + hooks, React style.
- **High-frequency value updates:** `Signal` + `.BindText`/`.BindClass`/`BindWidget`, bypassing the VDOM.
- **High-frequency structural updates** (large lists, frequent add/remove): `For`/`Show`, local diff, bypassing the root render.
- **Component memo:** off by default; when truly necessary use `MemoComponent` and fully express in the comparator the data that rendering and callbacks depend on.
- **Derived signals:** call `Dispose()` on temporary `Map`/`Computed` when their owner ends, to release upstream subscriptions.
- **Directly bound signals:** identity must be stable — create outside render, or use `UseSignal`.

In this design the reconciler degrades from "the tax on every update" to "a tool used on demand": only genuine structural change pays for a diff; value changes and list changes each have their own fast path.

---

## 9. Examples

```
examples/reactive-html   signals + For/Show + BindClass, drag reorder, context menu,
                         modal dialog, CSS grid card wall, light/dark theme toggle, cross-package components
```

For more detailed implementation conventions see the "App-dev surface" section of the repository README.
