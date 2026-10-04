#!/usr/bin/env bash
# build-helpers.sh — compile webview/helper/main.mm into the helper
# executable and build the 5 CEF helper .app bundles macOS multi-process
# CEF requires.
#
# The 5 helpers are all the same executable but live inside distinct
# .app dirs with distinct Info.plist files. CEF spawns them by name
# from Contents/Frameworks/<App> Helper*.app/Contents/MacOS/<App> Helper*.
#
#     <App> Helper.app           # default subprocess
#     <App> Helper (GPU).app
#     <App> Helper (Plugin).app
#     <App> Helper (Renderer).app
#     <App> Helper (Alerts).app
#
# Output:
#     build/helpers/<App> Helper*.app/...
#
# Usage:
#     build-helpers.sh <APP_NAME> [<OUT_DIR>] [<BUNDLE_ID_HOST>]
#         APP_NAME         e.g. "WebViewDemo" (the host app's display name)
#         OUT_DIR          defaults to build/helpers relative to cwd
#         BUNDLE_ID_HOST   defaults to com.example.<APP_NAME-lowercased>
#
# Re-runnable: rebuilds executable + plists from scratch each call.

set -euo pipefail

if [[ $# -lt 1 ]]; then
    echo "usage: build-helpers.sh <APP_NAME> [<OUT_DIR>] [<BUNDLE_ID_HOST>]" >&2
    exit 2
fi

APP_NAME="$1"
OUT_DIR="${2:-build/helpers}"
BUNDLE_ID_HOST_DEFAULT="com.example.$(echo "$APP_NAME" | tr '[:upper:]' '[:lower:]' | tr -cd '[:alnum:].-')"
BUNDLE_ID_HOST="${3:-$BUNDLE_ID_HOST_DEFAULT}"
VERSION="${WEBVIEW_VERSION:-0.1.0}"

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
WEBVIEW_DIR=$(cd "$SCRIPT_DIR/.." && pwd)
CEF_DIR="${WEBVIEW_CEF_DIR:-$WEBVIEW_DIR/lib/darwin/cef}"
CMAKE_BIN="${CMAKE_BIN:-$(command -v cmake || true)}"

if [[ ! -d "$CEF_DIR/Release/Chromium Embedded Framework.framework" ]]; then
    echo "build-helpers.sh: CEF framework not found at $CEF_DIR" >&2
    echo "build-helpers.sh: run webview/scripts/fetch-cef.sh first" >&2
    exit 1
fi

if [[ ! -f "$CEF_DIR/libcef_dll_wrapper.a" ]] && [[ ! -f "$CEF_DIR/Release/libcef_dll_wrapper.a" ]] && [[ ! -f "$CEF_DIR/build/libcef_dll_wrapper/libcef_dll_wrapper.a" ]]; then
    # The wrapper static lib is shipped pre-built in some CEF
    # distributions (under Release/) and needs to be built from source
    # in others (CMake invocation against CEF_DIR/libcef_dll/). Detect
    # both. If neither exists, the user needs to build it; we point
    # them at the docs.
    if [[ -d "$CEF_DIR/libcef_dll" ]] && [[ -f "$CEF_DIR/CMakeLists.txt" ]]; then
        echo "build-helpers.sh: building libcef_dll_wrapper.a via CMake..."
        if [[ -z "$CMAKE_BIN" || ! -x "$CMAKE_BIN" ]]; then
            echo "build-helpers.sh: cmake not found; set CMAKE_BIN=/opt/homebrew/bin/cmake" >&2
            exit 1
        fi
        (
            cd "$CEF_DIR"
            mkdir -p build
            cd build
            "$CMAKE_BIN" -DCMAKE_BUILD_TYPE=Release -DUSE_SANDBOX=OFF ..
            "$CMAKE_BIN" --build . --target libcef_dll_wrapper --config Release
        )
    else
        echo "build-helpers.sh: libcef_dll_wrapper.a not found and source unavailable" >&2
        echo "build-helpers.sh: see https://bitbucket.org/chromiumembedded/cef/wiki/BranchesAndBuilding.md" >&2
        exit 1
    fi
fi

# Locate the wrapper static lib (either pre-built or freshly built).
WRAPPER_LIB=""
for candidate in \
    "$CEF_DIR/build/libcef_dll_wrapper/libcef_dll_wrapper.a" \
    "$CEF_DIR/Release/libcef_dll_wrapper.a" \
    "$CEF_DIR/libcef_dll_wrapper.a"; do
    if [[ -f "$candidate" ]]; then
        WRAPPER_LIB="$candidate"
        break
    fi
done
if [[ -z "$WRAPPER_LIB" ]]; then
    echo "build-helpers.sh: libcef_dll_wrapper.a still not found after build step" >&2
    exit 1
fi

# -----------------------------------------------------------------------------
# Compile the helper executable
# -----------------------------------------------------------------------------

mkdir -p "$OUT_DIR"
HELPER_EXE_NAME="$APP_NAME Helper"
HELPER_BUILD_DIR=$(mktemp -d)
trap 'rm -rf "$HELPER_BUILD_DIR"' EXIT

# We compile main.mm once and copy the result into each helper .app.
# CEF 138+ uses C++20 concepts (std::same_as, std::convertible_to) in
# its public headers, so the helper must compile with -std=c++20.
echo "build-helpers.sh: compiling helper executable..."
clang++ \
    -std=c++20 \
    -fobjc-arc \
    -mmacosx-version-min=12.0 \
    -I"$CEF_DIR" \
    -o "$HELPER_BUILD_DIR/$HELPER_EXE_NAME" \
    "$WEBVIEW_DIR/helper/main.mm" \
    "$WRAPPER_LIB" \
    -framework Cocoa \
    -framework AppKit

# -----------------------------------------------------------------------------
# Build the 5 helper .app bundles
# -----------------------------------------------------------------------------

# Suffix → bundle-id-component pairs.
HELPER_ROLES=(
    "::"
    " (GPU)::gpu"
    " (Plugin)::plugin"
    " (Renderer)::renderer"
    " (Alerts)::alerts"
)

PLIST_TMPL="$WEBVIEW_DIR/helper/Info.plist.tmpl"

for role in "${HELPER_ROLES[@]}"; do
    suffix="${role%%::*}"
    role_id="${role##*::}"
    helper_name="$APP_NAME Helper$suffix"
    helper_app="$OUT_DIR/$helper_name.app"
    if [[ -n "$role_id" ]]; then
        bundle_id_helper="$BUNDLE_ID_HOST.helper.$role_id"
    else
        bundle_id_helper="$BUNDLE_ID_HOST.helper"
    fi

    echo "build-helpers.sh: building $helper_name.app"

    # Clean and recreate the bundle layout.
    rm -rf "$helper_app"
    mkdir -p "$helper_app/Contents/MacOS"

    # Copy executable with the role-specific name.
    cp "$HELPER_BUILD_DIR/$HELPER_EXE_NAME" "$helper_app/Contents/MacOS/$helper_name"

    # Render Info.plist from template.
    sed \
        -e "s/@APP_NAME@/$APP_NAME/g" \
        -e "s/@HELPER_NAME@/$helper_name/g" \
        -e "s/@HELPER_SUFFIX@/$suffix/g" \
        -e "s/@BUNDLE_ID_HOST@/$BUNDLE_ID_HOST/g" \
        -e "s/@BUNDLE_ID_HELPER@/$bundle_id_helper/g" \
        -e "s/@VERSION@/$VERSION/g" \
        "$PLIST_TMPL" > "$helper_app/Contents/Info.plist"
done

echo "build-helpers.sh: done. Helpers at $OUT_DIR/"
