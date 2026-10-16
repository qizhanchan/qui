# widgets/scripts — Chrome reference harnesses

These harnesses compare qui against real Chrome, driven via `puppeteer-core`.
It is this directory's own dependency, pinned in `web/package.json` +
`web/package-lock.json`; install it once (node >= 18, set `NODE_BIN` if the
default `node` is older):

```bash
npm ci --prefix widgets/scripts/web
```

puppeteer-core drives an installed Chrome rather than downloading one:
`CHROME_BIN` names the executable, defaulting to
`/Applications/Google Chrome.app` (see `web/chrome.mjs`). The Go tests never
need either — only regenerating goldens does.

## compare-html.sh — visual diff of default form controls

Renders each raw HTML form control from `web/native.html` in Chrome
(`web/capture.mjs`) and builds side-by-side strips against qui's snapshots.

```bash
widgets/scripts/compare-html.sh              # reuse the cached Chrome PNGs
REFRESH_REF=1 widgets/scripts/compare-html.sh
```

## web/dom-events.mjs — DOM event-order goldens

Records the order of keydown / keyup / pointerdown / pointerup / click /
dblclick / contextmenu / wheel / focus / blur (plus form submits and the
post-step focus and control values) for each fixture in
`htmlcss/testdata/events/` and writes `<name>.golden.json` next to it.
`go test ./htmlcss -run DOMEventOrder` replays the same input through
htmlcss and diffs against the committed goldens, so the test needs no Chrome.

Regenerate after adding or changing a fixture (`<name>.html` +
`<name>.steps.json`):

```bash
REFRESH_REF=1 go test ./htmlcss -run DOMEventOrder
```

or only the Chrome side, optionally for named fixtures:

```bash
node widgets/scripts/web/dom-events.mjs text-fields
```

Known differences live in `domEventAllowlist` in
`htmlcss/dom_event_order_test.go`, one line each with the reason. An entry
that no longer matches a difference fails the test, so delete it when the
fix lands.

## web/dom-layout.mjs — layout-geometry goldens

Records the border box (`getBoundingClientRect`, page coordinates) of every
element with an id for each fixture in `htmlcss/testdata/layout/` at a
640×480 viewport and writes `<name>.golden.json` next to it.
`go test ./htmlcss -run DOMLayout` renders the same page through htmlcss and
compares each El's window-space bounds field by field (x, y, w, h) within
1 px, or the element's `data-tol` attribute.

Fixtures use `font-family: GoRegular`, which the page `@font-face`-loads
from `web/go-regular.ttf` and the test registers from the same Go Regular
TTF, so text-driven sizes come from identical metrics. Each case sits in its
own fixed-height `<section class="case">` so one difference doesn't shift
every box after it.

Regenerate after adding or changing a fixture (`<name>.html`):

```bash
REFRESH_REF=1 go test ./htmlcss -run DOMLayout
```

or only the Chrome side, optionally for named fixtures:

```bash
node widgets/scripts/web/dom-layout.mjs flex grid
```

Known differences live in `domLayoutAllowlist` in
`htmlcss/dom_layout_test.go`, split into documented limits (COVERAGE.md) and
undocumented differences that are fix candidates. As with the event goldens,
an entry that no longer matches a difference fails the test.

## web/dom-style.mjs — computed-style goldens

Records `getComputedStyle` for every element with an id in
`htmlcss/testdata/style/` over a fixed property list (display, colors,
font, line-height, text, box model, borders, flex) and writes
`<name>.golden.json`. `go test ./htmlcss -run DOMStyle` serializes each El's
resolved `ComputedStyle` the same way and diffs it per property — lengths
within 0.05px, colors within 1 per channel. It checks the cascade,
inheritance, `var()` and unit resolution; layout is `DOMLayout`'s job.

```bash
REFRESH_REF=1 go test ./htmlcss -run DOMStyle
```

Known differences live in `domStyleAllowlist` in
`htmlcss/dom_style_test.go` (mostly control chrome that qui's backing
widgets paint themselves); a stale entry fails the test.
