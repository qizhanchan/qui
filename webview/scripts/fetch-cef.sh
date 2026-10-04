#!/usr/bin/env bash
# fetch-cef.sh — download the CEF binary distribution for the current
# platform into webview/lib/<platform>/cef/.
#
# Spotify hosts the canonical CEF builds at cef-builds.spotifycdn.com.
# URL shape:
#     https://cef-builds.spotifycdn.com/cef_binary_${CEF_VERSION}_${CEF_PLATFORM}.tar.bz2
#
# Where CEF_VERSION looks like:
#     138.0.40+g52ba8ad+chromium-138.0.7204.184
# and CEF_PLATFORM is one of: macosx64, macosarm64, linux64, linuxarm64, windows64.
#
# Usage:
#     webview/scripts/fetch-cef.sh                   # auto-detect platform, default version
#     CEF_VERSION="138.0.40+g52ba8ad+chromium-138.0.7204.184" fetch-cef.sh
#     CEF_PLATFORM=macosx64 fetch-cef.sh             # force Intel build on Apple Silicon
#     fetch-cef.sh --dry-run                         # print the URL and exit
#     fetch-cef.sh --force                           # re-download even if extracted
#
# The script is idempotent: it skips download+extract when the target
# directory already contains a Chromium Embedded Framework.framework
# of the requested version. To force a refresh, pass --force or rm -rf
# the target directory first.

set -euo pipefail

# -----------------------------------------------------------------------------
# Defaults
# -----------------------------------------------------------------------------

# Default CEF version. Override via CEF_VERSION env var.
#
# Choosing a default: pin to a CEF "release" branch (not "stable" which
# Spotify also publishes — release is what most embedders run). The
# string format is `{cef-version}+{commit}+chromium-{chromium-version}`.
# Update this constant when bumping; the rest of the script reads it.
#
# Compatibility note: CEF 138+ requires the host toolchain to build
# with C++20 (the public headers use std::same_as / std::convertible_to
# concepts). build-helpers.sh already passes -std=c++20 to clang++.
CEF_VERSION="${CEF_VERSION:-147.0.14+g76d2442+chromium-147.0.7727.138}"

# CDN base URL. Override via CEF_CDN env var if Spotify's CDN is
# blocked (some corporate networks); the layout under any mirror is
# identical.
CEF_CDN="${CEF_CDN:-https://cef-builds.spotifycdn.com}"

# Optional flags
DRY_RUN=0
FORCE=0
for arg in "$@"; do
    case "$arg" in
        --dry-run) DRY_RUN=1 ;;
        --force) FORCE=1 ;;
        -h|--help)
            sed -n '2,/^set -e/p' "$0" | sed -e '$d' -e 's/^# //; s/^#//' >&2
            exit 0
            ;;
        *)
            echo "fetch-cef.sh: unknown argument '$arg'" >&2
            exit 2
            ;;
    esac
done

# -----------------------------------------------------------------------------
# Platform detection
# -----------------------------------------------------------------------------

detect_platform() {
    local uname_s uname_m
    uname_s=$(uname -s 2>/dev/null || echo unknown)
    uname_m=$(uname -m 2>/dev/null || echo unknown)
    case "$uname_s" in
        Darwin)
            case "$uname_m" in
                arm64)  echo "macosarm64" ;;
                x86_64) echo "macosx64" ;;
                *)
                    echo "fetch-cef.sh: unsupported macOS arch '$uname_m'" >&2
                    exit 1
                    ;;
            esac
            ;;
        Linux)
            case "$uname_m" in
                x86_64)  echo "linux64" ;;
                aarch64) echo "linuxarm64" ;;
                *)
                    echo "fetch-cef.sh: unsupported Linux arch '$uname_m'" >&2
                    exit 1
                    ;;
            esac
            ;;
        *)
            echo "fetch-cef.sh: unsupported OS '$uname_s'" >&2
            exit 1
            ;;
    esac
}

CEF_PLATFORM="${CEF_PLATFORM:-$(detect_platform)}"

# -----------------------------------------------------------------------------
# Path derivation
# -----------------------------------------------------------------------------

# Resolve script directory + project layout.
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
WEBVIEW_DIR=$(cd "$SCRIPT_DIR/.." && pwd)
LIB_ROOT="$WEBVIEW_DIR/lib"

# Per-platform target directory. The Go cgo directives reference
# lib/darwin/cef regardless of the underlying CEF arch, so we drop a
# symlink (or copy) the arch-specific dir as "darwin" / "linux" /
# "windows" for darwin/linux/windows respectively.
case "$CEF_PLATFORM" in
    macos*) TARGET_PARENT="$LIB_ROOT/darwin" ;;
    linux*) TARGET_PARENT="$LIB_ROOT/linux" ;;
    windows*) TARGET_PARENT="$LIB_ROOT/windows" ;;
    *) echo "fetch-cef.sh: unmapped CEF_PLATFORM '$CEF_PLATFORM'" >&2; exit 1 ;;
esac

CEF_DIR="$TARGET_PARENT/cef"
ARCHIVE_NAME="cef_binary_${CEF_VERSION}_${CEF_PLATFORM}.tar.bz2"
URL="$CEF_CDN/$(printf %s "$ARCHIVE_NAME" | sed 's/+/%2B/g')"
DOWNLOAD_PATH="$LIB_ROOT/$ARCHIVE_NAME"

# -----------------------------------------------------------------------------
# Action
# -----------------------------------------------------------------------------

echo "fetch-cef.sh: platform=$CEF_PLATFORM version=$CEF_VERSION"
echo "fetch-cef.sh: target=$CEF_DIR"
echo "fetch-cef.sh: source=$URL"

if [[ $DRY_RUN -eq 1 ]]; then
    echo "fetch-cef.sh: --dry-run; exiting without downloading"
    exit 0
fi

# Idempotence check — does the framework already exist at the target?
# On macOS we check for the framework binary; on linux/windows we check
# for the shared library equivalent.
already_present=0
if [[ $FORCE -eq 0 ]] && [[ -d "$CEF_DIR/Release" ]]; then
    case "$CEF_PLATFORM" in
        macos*)
            if [[ -e "$CEF_DIR/Release/Chromium Embedded Framework.framework/Chromium Embedded Framework" ]]; then
                already_present=1
            fi
            ;;
        linux*)
            if [[ -e "$CEF_DIR/Release/libcef.so" ]]; then
                already_present=1
            fi
            ;;
        windows*)
            if [[ -e "$CEF_DIR/Release/libcef.dll" ]]; then
                already_present=1
            fi
            ;;
    esac
fi

if [[ $already_present -eq 1 ]]; then
    echo "fetch-cef.sh: framework already present; pass --force to re-download"
    exit 0
fi

mkdir -p "$LIB_ROOT"

# Pick a downloader. Prefer curl (most common on macOS), fall back to wget.
if command -v curl >/dev/null 2>&1; then
    echo "fetch-cef.sh: downloading via curl..."
    curl --fail --location --progress-bar --output "$DOWNLOAD_PATH" "$URL"
elif command -v wget >/dev/null 2>&1; then
    echo "fetch-cef.sh: downloading via wget..."
    wget --show-progress --progress=bar:force --output-document="$DOWNLOAD_PATH" "$URL"
else
    echo "fetch-cef.sh: neither curl nor wget is available; install one and retry" >&2
    exit 1
fi

# Verify download isn't an HTML 404 page (curl --fail catches that, but
# the spotify CDN occasionally returns a tiny valid-but-empty response
# for invalid versions). A real CEF tarball is at least 100 MB.
size=$(stat -f%z "$DOWNLOAD_PATH" 2>/dev/null || stat -c%s "$DOWNLOAD_PATH" 2>/dev/null || echo 0)
if [[ "$size" -lt 50000000 ]]; then
    echo "fetch-cef.sh: downloaded archive is suspiciously small ($size bytes); aborting" >&2
    echo "fetch-cef.sh: this usually means CEF_VERSION='$CEF_VERSION' is not a real CEF build." >&2
    echo "fetch-cef.sh: pick a valid version from https://cef-builds.spotifycdn.com/index.html" >&2
    rm -f "$DOWNLOAD_PATH"
    exit 1
fi

# Extract. The tarball top-level dir is named cef_binary_VERSION_PLATFORM/
# — rename it to plain "cef" after extraction for stable cgo paths.
echo "fetch-cef.sh: extracting..."
rm -rf "$CEF_DIR"
mkdir -p "$TARGET_PARENT"

TMP_EXTRACT=$(mktemp -d)
trap 'rm -rf "$TMP_EXTRACT"' EXIT

if ! tar -xjf "$DOWNLOAD_PATH" -C "$TMP_EXTRACT"; then
    echo "fetch-cef.sh: extraction failed" >&2
    exit 1
fi

# Locate the extracted top-level directory (should be exactly one).
extracted_root=$(find "$TMP_EXTRACT" -mindepth 1 -maxdepth 1 -type d | head -n 1)
if [[ -z "$extracted_root" ]]; then
    echo "fetch-cef.sh: no top-level dir found in archive" >&2
    exit 1
fi
mv "$extracted_root" "$CEF_DIR"

# Clean up the archive; users that want to keep it can pass an env var.
if [[ "${CEF_KEEP_ARCHIVE:-0}" -ne 1 ]]; then
    rm -f "$DOWNLOAD_PATH"
fi

echo "fetch-cef.sh: done. CEF is at $CEF_DIR"
echo "fetch-cef.sh: next step — build a webview-enabled app:"
echo "    go build -tags webview_cef ./examples/webview"
echo "    webview/scripts/package-app.sh ./webview-demo WebViewDemo.app"
