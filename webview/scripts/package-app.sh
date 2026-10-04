#!/usr/bin/env bash
# package-app.sh — bundle a Go binary + CEF framework + 5 helpers into
# a macOS .app suitable for double-clicking from Finder.
#
# Result layout:
#     <APP_NAME>.app/
#       Contents/
#         Info.plist                              # from helper/Info.plist.host.tmpl
#         MacOS/<APP_NAME>                        # the Go binary (renamed)
#         Frameworks/
#           Chromium Embedded Framework.framework # copied from webview/lib/darwin/cef/Release/
#           <APP_NAME> Helper.app
#           <APP_NAME> Helper (GPU).app
#           <APP_NAME> Helper (Plugin).app
#           <APP_NAME> Helper (Renderer).app
#           <APP_NAME> Helper (Alerts).app
#         Resources/                              # icon, optional Resources/ from CEF
#
# Usage:
#     package-app.sh <GO_BINARY> <OUT_APP> [<BUNDLE_ID_HOST>]
#         GO_BINARY        path to the binary built with `go build -tags webview_cef`
#         OUT_APP          path to the .app to create (e.g. ./build/WebViewDemo.app)
#         BUNDLE_ID_HOST   defaults to com.example.<APP_NAME>

set -euo pipefail

if [[ $# -lt 2 ]]; then
    echo "usage: package-app.sh <GO_BINARY> <OUT_APP> [<BUNDLE_ID_HOST>]" >&2
    exit 2
fi

GO_BINARY="$1"
OUT_APP="$2"
APP_NAME=$(basename "$OUT_APP" .app)
BUNDLE_ID_HOST_DEFAULT="com.example.$(echo "$APP_NAME" | tr '[:upper:]' '[:lower:]' | tr -cd '[:alnum:].-')"
BUNDLE_ID_HOST="${3:-$BUNDLE_ID_HOST_DEFAULT}"
VERSION="${WEBVIEW_VERSION:-0.1.0}"
SIGN_APP="${WEBVIEW_SIGN_APP:-1}"

if [[ ! -f "$GO_BINARY" ]]; then
    echo "package-app.sh: binary not found: $GO_BINARY" >&2
    exit 1
fi

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
WEBVIEW_DIR=$(cd "$SCRIPT_DIR/.." && pwd)
CEF_DIR="${WEBVIEW_CEF_DIR:-$WEBVIEW_DIR/lib/darwin/cef}"
FRAMEWORK_SRC="$CEF_DIR/Release/Chromium Embedded Framework.framework"
PLIST_HOST_TMPL="$WEBVIEW_DIR/helper/Info.plist.host.tmpl"

if [[ ! -d "$FRAMEWORK_SRC" ]]; then
    echo "package-app.sh: Chromium Embedded Framework.framework not found at $FRAMEWORK_SRC" >&2
    echo "package-app.sh: run webview/scripts/fetch-cef.sh first" >&2
    exit 1
fi

if [[ ! -f "$PLIST_HOST_TMPL" ]]; then
    echo "package-app.sh: host Info.plist template missing: $PLIST_HOST_TMPL" >&2
    exit 1
fi

# -----------------------------------------------------------------------------
# Build helpers (idempotent — overwrites every time so dev iterates safely)
# -----------------------------------------------------------------------------

HELPERS_OUT=$(mktemp -d)
trap 'rm -rf "$HELPERS_OUT"' EXIT

echo "package-app.sh: building helpers..."
"$SCRIPT_DIR/build-helpers.sh" "$APP_NAME" "$HELPERS_OUT" "$BUNDLE_ID_HOST"

# -----------------------------------------------------------------------------
# Assemble the host .app
# -----------------------------------------------------------------------------

echo "package-app.sh: assembling $OUT_APP"
rm -rf "$OUT_APP"
mkdir -p "$OUT_APP/Contents/MacOS"
mkdir -p "$OUT_APP/Contents/Frameworks"
mkdir -p "$OUT_APP/Contents/Resources"

# Host binary.
cp "$GO_BINARY" "$OUT_APP/Contents/MacOS/$APP_NAME"
chmod +x "$OUT_APP/Contents/MacOS/$APP_NAME"

# Host Info.plist.
sed \
    -e "s/@APP_NAME@/$APP_NAME/g" \
    -e "s/@EXECUTABLE@/$APP_NAME/g" \
    -e "s/@BUNDLE_ID_HOST@/$BUNDLE_ID_HOST/g" \
    -e "s/@VERSION@/$VERSION/g" \
    "$PLIST_HOST_TMPL" > "$OUT_APP/Contents/Info.plist"

# Copy CEF framework. Use -R + --preserve to keep symlinks and code-
# signing attributes intact — Chromium Embedded Framework.framework
# uses heavy symlink structure (Versions/A → Versions/Current).
echo "package-app.sh: copying Chromium Embedded Framework..."
cp -R "$FRAMEWORK_SRC" "$OUT_APP/Contents/Frameworks/"
# Normalize framework anatomy to standard macOS layout: Versions/A
# plus top-level symlinks.
CEF_FW="$OUT_APP/Contents/Frameworks/Chromium Embedded Framework.framework"
if [[ ! -d "$CEF_FW/Versions" ]]; then
    mkdir -p "$CEF_FW/Versions/A"
    mv "$CEF_FW/Chromium Embedded Framework" "$CEF_FW/Versions/A/"
    mv "$CEF_FW/Libraries" "$CEF_FW/Versions/A/"
    mv "$CEF_FW/Resources" "$CEF_FW/Versions/A/"
    ln -sfn A "$CEF_FW/Versions/Current"
    ln -sfn "Versions/Current/Chromium Embedded Framework" \
        "$CEF_FW/Chromium Embedded Framework"
    ln -sfn "Versions/Current/Libraries" "$CEF_FW/Libraries"
    ln -sfn "Versions/Current/Resources" "$CEF_FW/Resources"
fi

# Mirror CEF resources into app-level Contents/Resources. CEF's macOS
# defaults load pack/localized resources from the host app bundle.
echo "package-app.sh: copying CEF resources to app Contents/Resources..."
cp -R "$OUT_APP/Contents/Frameworks/Chromium Embedded Framework.framework/Resources/." \
   "$OUT_APP/Contents/Resources/"

# Copy the 5 helper .apps from the temp build dir.
echo "package-app.sh: copying helper bundles..."
for helper in "$HELPERS_OUT"/*.app; do
    cp -R "$helper" "$OUT_APP/Contents/Frameworks/"
done

if [[ "$SIGN_APP" == "1" ]]; then
    if ! command -v codesign >/dev/null 2>&1; then
        echo "package-app.sh: codesign not found; skipping signing" >&2
    else
        echo "package-app.sh: ad-hoc signing framework/helpers/host..."
        # Sign inside-out so the final host app signature sees consistent nested blobs.
        codesign --force --sign - --timestamp=none \
            "$OUT_APP/Contents/Frameworks/Chromium Embedded Framework.framework"
        for helper in "$OUT_APP/Contents/Frameworks/"*.app; do
            codesign --force --sign - --timestamp=none "$helper"
        done
        codesign --force --sign - --timestamp=none "$OUT_APP"
        echo "package-app.sh: signature verification"
        codesign --verify --deep --strict --verbose=2 "$OUT_APP"
    fi
fi

echo "package-app.sh: done."
echo "package-app.sh: open $OUT_APP"
