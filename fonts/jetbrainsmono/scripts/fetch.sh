#!/usr/bin/env bash
# Fetch the JetBrains Mono TTFs bundled by the jetbrainsmono package.
#
# Idempotent: downloads the release zip, extracts the weights we embed
# (Regular / Medium / Bold + their italics) into ../ttf, and refreshes
# the license. Re-run to upgrade — bump VERSION and run.
#
#   bash fonts/jetbrainsmono/scripts/fetch.sh
set -euo pipefail

VERSION="${JBMONO_VERSION:-2.304}"
URL="https://github.com/JetBrains/JetBrainsMono/releases/download/v${VERSION}/JetBrainsMono-${VERSION}.zip"

# Resolve paths relative to this script so it works from any CWD.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PKG_DIR="$(dirname "$SCRIPT_DIR")"
TTF_DIR="$PKG_DIR/ttf"

FACES=(
  JetBrainsMono-Regular.ttf
  JetBrainsMono-Medium.ttf
  JetBrainsMono-Bold.ttf
  JetBrainsMono-Italic.ttf
  JetBrainsMono-MediumItalic.ttf
  JetBrainsMono-BoldItalic.ttf
)

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Fetching JetBrains Mono v${VERSION} ..."
curl -sSL -o "$tmp/jbm.zip" "$URL"

mkdir -p "$TTF_DIR"
for f in "${FACES[@]}"; do
  unzip -o -q "$tmp/jbm.zip" "fonts/ttf/$f" -d "$tmp"
  cp "$tmp/fonts/ttf/$f" "$TTF_DIR/$f"
  echo "  ttf/$f"
done

unzip -o -q "$tmp/jbm.zip" "OFL.txt" -d "$tmp"
cp "$tmp/OFL.txt" "$PKG_DIR/OFL.txt"
echo "  OFL.txt"
echo "Done."
