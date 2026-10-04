# webview/lib

CEF (Chromium Embedded Framework) binary distribution lands here after
running:

```bash
webview/scripts/fetch-cef.sh
```

Expected layout after fetch (macOS):

```
webview/lib/darwin/
  cef/
    Release/
      Chromium Embedded Framework.framework/
    Resources/
    include/cef_*.h, capi/, ...
```

Everything inside `lib/` except this README is git-ignored. The fetch
script is idempotent — running it again with the same `CEF_VERSION`
short-circuits if the framework is already in place.

The Cgo bindings under `webview/backend_darwin.*` reference these
paths via `${SRCDIR}/lib/darwin/cef/...`, so the framework must live
exactly here (or use the `WEBVIEW_CEF_DIR` env var override at
build time).
