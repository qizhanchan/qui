# htmlcss — Coverage

This is the authoritative capability list for the `htmlcss` engine. It answers what is supported, what is deliberately left to native widgets, and where the boundaries are.

> qui is not a full browser. The goal is a **lightweight HTML5 + CSS subset** covering the layout, styling and interaction that application development actually uses often; for everything else (media, canvas, advanced table layout, …) prefer native `widgets` or a dedicated package.

**Legend:** ✅ complete · 🟡 partial (see note) · ❌ not implemented · 🚫 deliberately not supported (use native widgets / another package).

---

## 1. HTML tags

### Structure / text / semantics

| Tag | Status | Notes |
|---|---|---|
| `html` `head` `body` | ✅ | head and its contents are not rendered; body is the root |
| `div` `section` `article` `header` `footer` `nav` `main` `aside` | ✅ | generic block containers |
| `span` | ✅ | inline, folds into text runs |
| `p` `h1`–`h6` | ✅ | UA default margin / font-size |
| `strong` `b` `em` `i` `small` `u` `s` `del` `ins` `strike` `mark` `abbr` `sub` `sup` | ✅ | inline, fold to styled spans |
| `code` `pre` | ✅ | monospace; `<pre>` / `white-space:pre` preserves whitespace and newlines |
| `a` | ✅ | href click calls `OpenURL`; also clickable inside a folded span (`InlineBox.LinkAt`) |
| `br` | ✅ | inline forced break |
| `blockquote` | ✅ | UA left border + indent |
| `hr` | ✅ | rendered as `widgets.Rule`; `border-*` applies to the line itself |
| `ul` `ol` `li` | ✅ | hanging `[marker\|content]` row; disc/circle/square/decimal; `ol` auto-numbering; the clipboard rebuilds list structure |
| `img` | 🟡 | local raster + `.svg` (with tint) + `data:` URI + `object-fit: cover/contain`; **no** remote URL / `srcset` |
| `label` | 🟡 | folds to inline text; `for=` click linkage is implemented on the `label[for]` row below |
| `table` `thead` `tbody` `tfoot` `tr` `td` `th` `caption` `colgroup` `col` | ✅ | flattened into one `GridLayout`: columns align across rows, content-based auto widths (equal `1fr` when the table has a width), `colspan`, `th` bold + centered, `tr:nth-child` striping, `border-spacing`, auto-width shrink-to-fit. Also: `<caption>` as a full-width spanning row (`caption-side: top/bottom`), `<colgroup>`/`<col>` widths (`width` attr or inline style → `GridFixedTrack`, with `span`, `%` of table width), `table-layout: fixed`, `border-collapse: collapse` (single-line merging), `rowspan` row-height contribution. Limits: `<col>` does not take class selectors; proportional column distribution is not implemented |
| `dl` `dt` `dd` `figure` `figcaption` | ✅ | UA defaults: `dd`/`figure` indented, `dt` bold, `figcaption` muted |
| `script` `style` `meta` `link` `title` | 🚫 | not rendered; `<style>` contents are collected into the stylesheet |

### Form controls (rendered through backing widgets)

| Tag | Status | Notes |
|---|---|---|
| `input[type=text]` (default) | ✅ | `widgets.Input`; placeholder / value / onInput / onSubmit |
| `input[type=checkbox]` | ✅ | `widgets.CheckBox`; onToggle; optional value label |
| `input[type=checkbox switch]` | ✅ | Safari-style `switch` boolean attribute → `widgets.Switch`; checkbox semantics (`checked` / `:checked` / form serialization); `accent-color` tints the on-track |
| `input[type=radio]` | ✅ | real `RadioButton`, grouped by `name`; `value` is the visible label, `checked` the initial selection |
| `input[type=password]` | ✅ | masked display (• per character), real value retained, caret/selection measured on the mask |
| `input[type=range]` | ✅ | `widgets.Slider`; min/max/value/step; onChange reports a numeric string |
| `input[type=number/email/search/tel/url]` | 🟡 | rendered as ordinary text boxes. `type=search` has a UA clear button (an inline ✕ that clears and fires input, not submit). `type=number` has a stepper (increments by `step`, default 1, clamps to `[min,max]`, reports via onInput). Missing: type validation (`:valid`/`:invalid`) |
| `readonly` / `maxlength` attributes | ✅ | via the backing widget's `BeforeInput` veto hook — every mutation path goes through it. Readonly fields stay focusable / selectable / copyable. `maxlength` counts runes; deletion is never blocked; replacing a selection frees its length |
| `input[type=date/time/datetime-local/month/week]` | 🟡 | text box + format-hint placeholder (`date`→`YYYY-MM-DD`, `time`→`HH:MM`, …); **no** native date picker or validation |
| `input[type=color]` | 🟡 | rendered as a filled swatch button; click opens an in-app palette `Popup` (None row + grayscale row + 10-hue matrix + hand-written hex input with live preview). Popup position is adaptive (below, flips above if needed, clamps to the viewport). Interaction states change only the border ring, never the fill. **No** native color panel / free-form picker |
| `input[type=submit/reset/button]` | ✅ | rendered as buttons (Box + text, label from `value`, defaults Submit/Reset) reusing `<button>` UA chrome. Submit submits the enclosing `<form>`, reset restores each control's default, button fires only author `onClick`. Not successful controls in form serialization |
| `input[type=file]` | ✅ | rendered as a button; click opens the native file dialog (`qui.OpenFile`/`OpenFiles`; `multiple`; `accept` `.ext` tokens → `AllowedExtensions`). Label is the selected file's base name (`(+N)` for multiple), default `Choose File…`; serializes absolute paths. Non-darwin returns `ErrDialogNotSupported` |
| `textarea` | ✅ | `widgets.TextArea` |
| `select` `option` `optgroup` | ✅ | requires `Options.Window`; without one it degrades to a placeholder label. `option value` is the submitted value (falling back to visible text), `selected` is initial and restored by form reset, `disabled` is skipped by click / arrows / type-ahead; `<optgroup>` inserts a non-clickable group header. Keyboard type-ahead (cycles on repeated letters, buffer clears after 1s). Limits: groups are a header + flattened approximation; no `multiple`/`size` |
| `button` | ✅ | Box rendering + built-in hover/press feedback |
| `form` | ✅ | `El.SetOnFormSubmit(func(map[string]string))` collects the current values of named descendant controls. Triggered by Enter in a text field or a submit button; `<input type=reset>` restores defaults. Limits: no native action/method network submit, no `formdata`/validation |
| `fieldset` `legend` | ✅ | UA border/padding + bold legend |
| `label[for]` | ✅ | click activates the control (toggle checkbox, select radio, focus text field). As a control surface its text is not drag-selectable; a click only toggles |
| `datalist` | ✅ | native autocomplete for `<input list=ID>`: filters on case-insensitive substring (like Chrome), opens a non-modal `Popup` anchored to the field (the field keeps focus), arrow keys move, Enter confirms, Esc closes. Navigation keys are intercepted in the capture phase so the confirming Enter is not read as a form submit. The `<datalist>` itself is not rendered. Programmatic `El.SetSuggestions` / `h.Suggest`. Known difference: the suggestion list auto-sizes to the widest item rather than matching the field width |
| `output` | ❌ | not implemented |

### Embedded / custom-draw mount points

| Tag | Status | Notes |
|---|---|---|
| `canvas` | ✅ | a "widget draws itself" mount point (no JS 2D context — use qui's drawing). `El.SetCanvasDraw(func(cv qui.Canvas, bounds qui.Rect))` installs a per-frame draw callback; `El.SetCanvas(qui.Widget)` inserts an arbitrary custom widget (`scene3d.Viewport`, a `graphs` chart, …). CSS `width`/`height` size it (default 300×150); unwired renders a dashed placeholder. Default `display:inline-block`, `Role()=image` |

### Deliberately not supported (use native widgets / other packages)

| Tag | Alternative |
|---|---|
| `audio` | `media.AudioPlayer` |
| `video` | `media.VideoView` |
| `iframe` / embedded browser | `webview` (CEF, build tag) |
| `svg` (inline tag) | `<img src=*.svg>` or `h.Icon` + `svg.Document` (`qui.VectorSource`) |
| `dialog` | reactive `h.ModalPortal` / `widgets.Dialog` |
| `details/summary` `progress` `meter` | composed native `widgets` (ScrollView/Progress etc.) |
| `text-shadow` | ❌ |

---

## 2. CSS properties

### Text / font (inheritable)

| Property | Status | Notes |
|---|---|---|
| `color` | ✅ | also pushed to form-control text color; overrides the UA default only when explicitly declared |
| `accent-color` | ✅ | inheritable; drives control active chrome (checkbox box, radio dot, switch track, slider active track/handle/state layer). The key property for a business dark theme expressed as CSS |
| `font-size` | ✅ | px/em/rem/pt/% |
| `font-weight` | ✅ | bold/normal/lighter + numeric |
| `font-style` | ✅ | italic |
| `font-family` | ✅ | first family |
| `font` (shorthand) | ✅ | `expandFont` |
| `line-height` | ✅ | unitless multiple / length |
| `text-align` | ✅ | start/center/end/**justify**. Justify stretches only soft-wrapped lines; it flows through the root `JustifyExtra` + `Font.WordSpacing` channel on both plain (Label) and inline (InlineBox) paths, so drawing/selection/hit-testing agree. Rich-text spans do not participate |
| `text-decoration` / `-line` | ✅ | underline / line-through / overline + `-color` + `-style` (solid/double/dotted/dashed/wavy) + `-thickness`. The root `DecorationPaint` runs through plain / rich / inline paths. Limit: wavy is approximated with a zig-zag path |
| `text-transform` | ✅ | upper/lower/capitalize |
| `overflow-wrap` / `word-wrap` / `word-break` | ✅ | `break-word`/`anywhere`/`break-all` enable intra-word breaks (URLs, unbroken digit/CJK strings); `normal`/`keep-all` overflow. Both text assemblies honor it (plain and inline), with break points on grapheme boundaries so the caret can cross them. Limit: `break-all` is approximated by `break-word`; `hyphens` not implemented |
| `text-overflow: ellipsis` | 🟡 | only with `white-space:nowrap`, single line |
| `white-space` | ✅ | normal / nowrap / `pre` / `pre-wrap` / `pre-line`. Limit: mixed inline children inside `<pre>` still collapse |
| `letter-spacing` `word-spacing` | ✅ | via `Font.LetterSpacing`/`WordSpacing`; `normal` zeroes; inheritable; tracking scales with canvas scale. Limit: synthetic italic shear degrades on the spacing path |
| `text-indent` | ✅ | first-line indent (`ParagraphStyle.FirstIndent`). Limit: combined with `text-align:center/end` and unbounded width the indent is overridden |

### Box model / borders

| Property | Status | Notes |
|---|---|---|
| `width` `height` `min-*` `max-*` | 🟡 | px/em/rem/pt/`calc()`; normal-flow blocks respect explicit width/height (left-aligned, not full-width; min/max clamp) plus flex/grid/inline-block. `%` resolves against the containing block during layout, including min/max (e.g. `max-width:100%` prevents overflow), interpreted as border-box. Missing: `calc()` containing `%`, `%` on inline-block / absolute |
| `padding` `margin` (shorthand + per-side) | ✅ | 1–4 value rules; CSS margin-box semantics (Flex/Flow); `margin: 0 auto` horizontal centering; flex-item auto margins. Missing: flex auto margin under overflow (treated as 0, per spec) |
| `border` / `border-width/color/style` | ✅ | `border: none` explicitly clears a control's native border (`HasBorder` distinguishes "declared none" from "undeclared") |
| `border-<side>*` per-side | ✅ | |
| `border-style` | 🟡 | solid/dashed/dotted/none; double/groove/ridge/inset/outset degrade to solid |
| `border-radius` | ✅ | uniform + per-corner + 1–4 value shorthand + elliptical `a / b`; `overflow:hidden` clips children to the rounded shape. Limit: `box-shadow` still uses the uniform radius |
| `box-sizing` | ✅ | default `content-box`; `border-box` supported. Border is drawn inside and does not add layout extent, so the content area can differ by the border thickness |
| `outline` | ✅ | `outline`/`-width`/`-color` → a stroke outside the border (does not affect layout); no `outline-offset`/style |

### Background / visual effects

| Property | Status | Notes |
|---|---|---|
| `background` / `background-color` | ✅ | shorthand parsed as a color |
| `background-image: linear-gradient()` | ✅ | angle + `to <side>`; multi-stop |
| `background-image: url()` | 🟡 | bitmap stretched to fill via `qui.ImageShader` (including `data:` URIs); **no** `background-size`/`position`/`repeat` (always 100% stretch) |
| `radial-gradient` | ✅ | shape/size/position prefixes ignored, always farthest-corner; multi-stop |
| `conic-gradient` | ✅ | `from <angle>` + stops (`qui.ConicGradient`); position ignored (centered) |
| `repeating-*` gradient | ❌ | |
| `background-size` | 🟡 | `cover`/`contain`/stretch (`qui.ImageFit`); no explicit size / `position` / `repeat` |
| `box-shadow` | 🟡 | multiple layers (comma-separated); **inset ignored** (no inner-shadow primitive) |
| `opacity` | ✅ | |
| `transform` | ✅ | translate/rotate/scale/skew/matrix (2D, via `Canvas.Concat`) + `transform-origin` (keyword/%); **no** 3D |
| `filter` | 🟡 | `blur()` + `drop-shadow()` via `SaveLayer`; first function only; no brightness/contrast |
| `backdrop-filter` | ❌ | needs background sampling |

### Layout / positioning

| Property | Status | Notes |
|---|---|---|
| `display: block/inline/inline-block/none/flex/grid` | ✅ | static `none` pruned at compile time; runtime toggling to/from `none` also works and the parent layout skips the box entirely (no flex/grid gap residue) |
| Flex: `flex-direction/justify-content/align-items/flex-wrap/gap/flex-grow` | ✅ | `align-items:stretch` (default) stretches only cross-axis-auto items; explicit `width` (column) / `height` (row), including percentages, keep their size and align to the start, like the browser |
| Flex: `align-self/flex-shrink/flex-basis/order` | ✅ | `flex` shorthand expands grow/shrink/basis (none/auto/initial); `order` uses stable sort; `flex-shrink:0` → `NoShrink` |
| Flex: `align-content` | ✅ | multi-line distribution in the remaining cross-axis space: start/center/end/space-between/around/evenly/stretch |
| Grid: `grid-template-columns/rows` (px/fr/auto/repeat), `gap`/`row-gap`/`column-gap` | ✅ | |
| Grid placement: `grid-column/row`, `span`, `grid-template-areas` | ✅ | line numbers (1-based `a / b`), `span N`, `grid-area` (named area or 4 line numbers); named areas with implicit track counts. Limit: only the `grid-column/row` shorthand (not `*-start`/`*-end`); mixing an explicit axis with an auto axis degrades to pure span auto-placement |
| `position: relative` | ✅ | top/right/bottom/left offsets; establishes a containing block |
| `position: absolute` | ✅ | out of flow, positioned against the nearest positioned ancestor. Limits: containing block approximated as the border box; no `z-index` stacking; an `auto` side lands at the containing-block origin rather than the static position |
| `position: fixed` | 🟡 | treated as absolute (relative to the nearest positioned ancestor / root); **not** viewport-fixed |
| `position: sticky` | ❌ | |
| `overflow: hidden/clip/auto/scroll` (incl. `-x`/`-y`) | ✅ | `auto`/`scroll` hosts a `ScrollView` |
| `visibility: hidden/collapse` | ✅ | keeps the layout box, does not draw (self + subtree). Limit: descendant `visibility:visible` re-show is not supported |
| `z-index` | 🟡 | paint order ascending by z; **no** full stacking context (no new context, no subtree isolation) |
| `float` `clear` `columns` | ❌ | no floats / multi-column |

### Interaction / pointer

| Property | Status | Notes |
|---|---|---|
| `cursor` | ✅ | inheritable; resolved declaratively at the root in two levels — an explicit declaration (CSS or `SetCursorShape`) wins from innermost out, otherwise the widget's own shape is used (link hand, text I-beam, anchor hand, tooltip). UA adds `pointer` to `a[href]`; `<button>` is not given `pointer` (browser behavior). Limit: GLFW 3.3 has only six standard cursors, so `not-allowed`/`move`/`grab`/`wait`/`zoom-*`/diagonal-resize degrade to the arrow; no `url()` custom cursor |
| `user-select` | ✅ | inheritable (incl. `-webkit-` alias). `none` removes the element's text from drag-selection and the clipboard; `text`/`auto`/`all`/`contain` restore it. Control surfaces are non-selectable even without a declaration. Two-way: dropping the rule restores selectability |
| `pointer-events: none / auto` | ✅ | inheritable; filtered at target selection, so hover/focus/tooltip/cursor/drag all see the same target. A transparent element is not a target itself but descendants are still probed first, so an `auto` descendant re-opens a hole (browser behavior). SVG-specific values are treated as hittable |

### Selectors / variables / at-rules

| Feature | Status | Notes |
|---|---|---|
| tag / `.class` / `#id` / `*` | ✅ | |
| attribute selectors `[a]` `=` `^=` `$=` `*=` `~=` `\|=` | ✅ | case-insensitive flag `i` supported |
| combinators (descendant / `>` / `+` / `~`) | ✅ | right-to-left matching |
| `:hover` `:focus` `:active` | ✅ | `focus-within`/`visible` normalize to focus |
| Ancestor/sibling state triggering descendants (`.row:hover .del`, `.a:hover ~ .b`, …) | ✅ | the state of the element named by the selector drives box decoration / text color / `visibility`. Triggers are discovered precisely; payload is computed per active trigger combination. Limits: `display:none` reveal not supported; a rule with two non-subject state compounds is not discovered |
| `:root` `:first-child` `:last-child` `:only-child` `:nth-child(An+B)` `:not(simple)` | ✅ | |
| `:nth-of-type(An+B)` | ✅ | counts same tag |
| `:checked` `:disabled` | ✅ | attribute-driven + runtime relink: toggling a checkbox/radio or `El.SetDisabled(bool)` syncs the attribute and triggers a scoped restyle, so `:checked`/`:disabled` (including `:has(:checked)` etc.) re-cascade immediately |
| `:enabled` `:required` `:optional` `:read-only` `:read-write` | ✅ | attribute-driven. `:enabled`/`:required`/`:optional` apply only to form controls; `:read-only` matches everything not user-editable (per spec); `:read-write` matches editable controls. Missing: `:valid`/`:invalid`/`:placeholder-shown`/`:indeterminate`/`:default` |
| `:nth-last-child(An+B)` `:empty` `:has()` | ✅ | `:has()` is limited to a single simple descendant selector (`div:has(img)`) |
| `::before` `::after` | 🟡 | generate `content` text (leaf/inline elements fold into the same InlineBox). `content` supports quoted strings + `attr(name)` + concatenation. Limits: not generated on elements with block children; no `counter()` or full generated-box model |
| Other pseudo-elements (`::first-line`, …) | ❌ | parsed but not generated |
| CSS variables `--x` / `var(--x, fallback)` / `:root` | ✅ | inheritance + recursive resolution (depth limit 16) |
| Shorthands `font` `flex` `inset` | ✅ | |
| `@media` | 🟡 | `min-width`/`max-width` (`and`, screen/all/print) evaluated at parse time against the viewport width; matching blocks flatten into the stylesheet. **Not responsive** (does not re-evaluate on resize) |
| `@font-face` `@keyframes` `@import` `@supports` | ❌ | at-rules skipped wholesale |
| `!important` / cascade priority / inline style | ✅ | UA < author < inline, important across layers |
| Runtime stylesheet swap `StyleEngine.SetCSS` | ✅ | re-parses and full-restyles in place — the theme-switch hook (swap `:root` variables). `RenderResult.Engine` exposes the static render's engine |
| Colors: hex 3/6/8, `rgb()`/`rgba()`, `hsl()`/`hsla()`, full named set | ✅ | hsl supports comma and space syntax and `deg`/negative hue; the full CSS Level 4 named set (148 + transparent). No `hwb()`/`lab()`/`lch()` |
| Lengths: px/em/rem/pt/%/unitless/`calc()`/`min()`/`max()`/`clamp()` | 🟡 | `calc()` supports `+ - * /`, parentheses, nesting, mixed units; `min`/`max`/`clamp` parse each argument through `parseLength`. Limit: `%` inside `calc()` only works where the property itself supports `%`. **No** `vw`/`vh`/`ch` |
| `transition` / `animation` | ❌ | dynamic infrastructure not built |

---

## 3. Events and interaction

| Capability | Status | Notes |
|---|---|---|
| Click `onClick` | ✅ | |
| `onContextMenu` | ✅ | reports window coordinates, so a menu can be anchored |
| `<a href>` navigation | ✅ | clickable as the element or inside a folded span |
| `:hover`/`:active` box decoration | ✅ | buttons get built-in darken/press when the author has no rule |
| `:hover`/`:focus`/`:active` text color + text-decoration | ✅ | both standalone elements and folded inline links. Limit: state changing `font-weight`/`size` re-lays-out, so it is not applied at draw time |
| `:focus` box style | ✅ | a focusable box disables child-label selection to take focus |
| Input `onInput` / Enter `onSubmit` | ✅ | input/textarea |
| checkbox `onToggle` / select `onChange` | ✅ | toggling syncs the `checked` attribute and relinks `:checked` |
| `<form>` submit `onFormSubmit` | ✅ | `SetOnFormSubmit(map[string]string)`; triggered by Enter or a submit button; collects named control values |
| `onMouseEnter` / `onMouseLeave` | ✅ | fired once per crossing (synthesized by diffing the hit path; DOM mouseenter/leave semantics, non-bubbling) |
| `onDoubleClick` | ✅ | synthesized from two left releases within 400ms + 5px; click→click→dblclick order |
| `onWheel` | ✅ | receives `(dx, dy)`; return true to consume, otherwise the outer ScrollView scrolls (equivalent to not calling preventDefault). Bubble phase, inner-first |
| `onKeyDown` / `onKeyUp` | ✅ | receives `qui.KeyEvent`; return true to consume. Reached either because the element holds focus (declaring a key handler makes it focusable) or because the event bubbles from a focused descendant. Disabled elements swallow keys |
| `onFocus` / `onBlur` | ✅ | fired once per focus transition |
| Cross-widget text selection | ✅ | Label + InlineBox implement `TextSelectable` |
| Clipboard copy (plain text + HTML flavor) | ✅ | images inline as data URIs; lists rebuilt as `<ol>/<ul>`; tables rebuilt as `<table>` (HTML → real tables in Docs/Word; plain → TSV for Sheets). Mixed selections each stay structured, in document order. Selection coordinates normalize to the bounding box. Limits: nested lists inside a cell flatten to `<br>`; spanning cells in a whole-block selection are approximated |
| Drag & drop reorder | ✅ | Draggable/DragHandle/OnDrop/OnDragOver with paint-only Transform feedback |
| `app-region: drag / no-drag` | ✅ | inheritable (Electron `-webkit-app-region` alias); `drag` on a title-bar strip lets the whole subtree drag the window, `no-drag` children opt back out. Backed by `Window.BeginWindowDrag()` (native window-manager drag). Only meaningful with `Window.SetTitlebarStyle(qui.TitlebarOverlay)`; unsupported platforms degrade to a normal press |
| Keyboard focus / Tab cycling | ✅ | framework-level; control editing uses the backing widget |
| HTML `disabled` attribute | ✅ | disables the backing control; `El.SetDisabled(bool)` toggles at runtime and relinks `:disabled` |
| HTML `title` attribute → hover tooltip | ✅ | pushed to the widget's tooltip on restyle; folded inline elements have no widget of their own |
| HTML `hidden` attribute | ✅ | implemented per UA rules (a tier-0 `display:none`), so author CSS can override it. Static `Render` prunes resolved-none subtrees at compile time |
| `tabindex` / `accesskey` / `minlength` / `pattern` / `rows` / `cols` / `autofocus` / `inputmode` / `spellcheck` | ❌ | not read |
| `label[for]` click focus | ✅ | toggle / select / focus |
| AX roles (button/link/textbox/img/list/heading…) | ✅ | `El.Role()` + `AccessibleName()`, addressable by the agent |

---

## 4. Known gaps

Grouped by how likely a desktop application is to need them.

**High value (native + everyday):** `aspect-ratio`; `position: sticky`; `::placeholder` and `:placeholder-shown`/`:valid`/`:invalid`/`:indeterminate`/`:default` (all need a "value changed → scoped restyle" hook); `::selection`/`::marker`; `:is()`/`:where()`/CSS nesting; `transition`; the remaining mouse events (`mousemove`, `scroll`, `change`); the `tabindex`/`minlength`/`pattern`/`rows`/`cols`/`autofocus`/`inputmode`/`spellcheck` attributes.

**Medium value:** Grid `justify-items`/`justify-self`/`place-*`/`grid-auto-*`/implicit tracks; `min-content`/`max-content`/`fit-content` sizing keywords; responsive `@media` re-evaluation and `prefers-color-scheme`/`(hover)`/`(pointer)`/`prefers-reduced-motion`; `@container`; `color-scheme`; `oklch()`/`color-mix()`/`light-dark()`/`hwb()`/`lab()`; `text-shadow`; text polish (`text-wrap`, `hyphens`, `tab-size`, `font-variant*`, `font-feature-settings`, `text-underline-offset`); `background-position`/`-repeat`/explicit `background-size`/repeating gradients; remote image URLs / `srcset`; scroll and resize polish (`resize`, `appearance`, `caret-color`, `scrollbar-*`, `overscroll-behavior`, `scroll-behavior`, scroll-snap); `<select multiple>`/`size`; native date/color pickers; `<a target>`/`download`.

**Deliberately not supported:** `float`/`clear`; multi-column; `@keyframes` + `animation`; `writing-mode`; `clip-path`/`mask`; `mix-blend-mode`/`isolation`; full stacking context; `backdrop-filter`; 3D `transform`; `contenteditable`; `<iframe>` (use `webview`); Shadow DOM / `<template>` / `<slot>`; `content-visibility`/`will-change`; `@import`; `@supports`.

---

## 5. Backing-widget contract

htmlcss renders through `widgets.Box`, `Label`, `InlineBox`, `Rule`, `Input`, `TextArea`, `CheckBox`, `Select`, `ScrollView`, `Image` (and `reactive/html` uses `MenuItem`/`ShowContextMenu`). These APIs are a stable contract — changing them silently breaks CSS rendering. Run `go test ./htmlcss ./reactive/...` after touching them.
