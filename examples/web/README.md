# Running a qui app in the browser

Any qui app builds for the web unchanged: under `GOOS=js` the `web`
platform backend (internal/platform/web) draws into a `<canvas>`, and
`GLRenderer` becomes the pure-Go CPU renderer.

```bash
GOOS=js GOARCH=wasm go build -o examples/web/app.wasm ./examples/reactive-html
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" examples/web/
python3 -m http.server 8765 --directory examples/web
```

Then open http://localhost:8765. `index.html` fetches `app.wasm` with
`cache: "no-cache"`, so a rebuild is picked up on the next reload instead
of the browser silently reusing a cached build.

Packages that need cgo or OpenGL (`scene3d`, `media`, `webview`) don't
build for wasm; everything on the app-dev surface (`widgets`, `htmlcss`,
`reactive`, `reactive/html`, `icons`, `svg`, `anim`, `i18n`) does.

## What the web backend handles

- **Chinese / Japanese / Korean and other scripts.** A wasm build cannot
  read system fonts and embeds only Latin faces. Anything those faces
  don't cover is measured and drawn by the browser's canvas text engine
  with the browser's own default fonts, so CJK text renders out of the
  box. For exact typographic control (one font everywhere, PDF export),
  fetch a font and call `qui.LoadFallbackFontFromBytes`; covered runes
  then never reach the browser fallback.
- **Emoji.** Color emoji are drawn by the browser's emoji font (the
  engine has no emoji provider of its own off macOS); an app-installed
  `qui.SetEmojiProvider` takes precedence.
- **Input methods.** Composition arrives through a hidden textarea kept
  at the caret, so the IME candidate window appears next to the text;
  the uncommitted text is shown as preedit in the field.
- **File drop.** Browsers hand over contents, not paths: dropped files
  are delivered as virtual paths (`/dropped/<n>/<name>`) — read them
  with `window.OpenDroppedFile(path)`, which is `os.Open` on desktop.
- **Shortcuts.** On a Mac or iOS browser, `CmdOrCtrl` shortcuts use
  Command (`qui.MacKeyboard()`), not Ctrl.
- **Pacing.** Frames are requested with `requestAnimationFrame` only when
  something needs painting; an idle tab runs no callbacks.
