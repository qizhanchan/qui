# widgets/scripts — Chrome reference harnesses

Both harnesses compare qui against real Chrome, driven via `puppeteer-core`
from `/opt/js_work/material-web/node_modules` and the system Chrome at
`/Applications/Google Chrome.app`. Use node >= 18 (set `NODE_BIN` if the
default `node` is older).

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
