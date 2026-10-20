# Running a qui app in the browser

Any qui app builds for the web unchanged: under `GOOS=js` the `web`
platform backend (internal/platform/web) draws into a `<canvas>`, and
`GLRenderer` becomes the pure-Go CPU renderer.

```bash
GOOS=js GOARCH=wasm go build -o examples/web/app.wasm ./examples/reactive-html
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" examples/web/
python3 -m http.server 8765 --directory examples/web
```

Then open http://localhost:8765.

Packages that need cgo or OpenGL (`scene3d`, `media`, `webview`) don't
build for wasm; everything on the app-dev surface (`widgets`, `htmlcss`,
`reactive`, `reactive/html`, `icons`, `svg`, `anim`, `i18n`) does.
