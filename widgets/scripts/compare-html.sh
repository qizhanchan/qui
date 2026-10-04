#!/usr/bin/env bash
#
# Side-by-side comparison harness for qui's DEFAULT widgets vs. the raw
# HTML form-control baseline (widgets/scripts/web/native.html).
#
#   1. (Re)generate qui snapshots via QUI_GOLDEN=1.
#   2. Re-capture the HTML reference via puppeteer-core (only if requested
#      with REFRESH_REF=1, or when the ref dir is empty — repeat runs stay
#      fast).
#   3. For each <case>.png pair, build a comparison strip:
#         [ qui | gap | html | gap | diff ]
#      saved to /tmp/qui-html-cmp/<case>.png. Diff uses ImageMagick's
#      `compare` after padding the smaller image up to the larger one's
#      bounds so a width/height mismatch is visible rather than an error.
#   4. Print an AE (absolute-pixel-error) table.
#
# Requires: ImageMagick (`compare`, `magick`); puppeteer-core via
# /opt/js_work/material-web for ref capture; Go Regular font file at
# widgets/scripts/web/go-regular.ttf (committed).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
QUI_DIR=/tmp/qui-html
REF_DIR=/tmp/html-native-ref
CMP_DIR=/tmp/qui-html-cmp
mkdir -p "$CMP_DIR"

echo "[1/4] Regenerating qui snapshots…"
(cd "$REPO_ROOT" && QUI_GOLDEN=1 go test -count=1 ./widgets -run TestHTMLNativeWidgetsSnapshot >/dev/null)

if [[ "${REFRESH_REF:-0}" == "1" || ! -d "$REF_DIR" || -z "$(ls -A "$REF_DIR" 2>/dev/null)" ]]; then
    echo "[2/4] Capturing HTML reference images…"
    # capture.mjs is ESM + top-level await; needs node >= 18. The shell's
    # default `node` may be an old nvm version, so resolve a modern one.
    NODE_BIN="${NODE_BIN:-}"
    if [[ -z "$NODE_BIN" ]]; then
        for cand in node "$HOME"/.nvm/versions/node/v2*/bin/node /opt/homebrew/bin/node; do
            if command -v "$cand" >/dev/null 2>&1; then
                major="$("$cand" -e 'console.log(process.versions.node.split(".")[0])' 2>/dev/null || echo 0)"
                if (( major >= 18 )); then NODE_BIN="$cand"; break; fi
            fi
        done
    fi
    [[ -n "$NODE_BIN" ]] || { echo "ERROR: need node >= 18 (set NODE_BIN=...)" >&2; exit 1; }
    "$NODE_BIN" "$REPO_ROOT/widgets/scripts/web/capture.mjs"
else
    echo "[2/4] Reusing existing $REF_DIR (set REFRESH_REF=1 to recapture)."
fi

if ! command -v compare >/dev/null 2>&1; then
    echo "ERROR: ImageMagick 'compare' not installed. brew install imagemagick" >&2
    exit 1
fi

echo "[3/4] Building side-by-side comparison strips…"
printf "\n%-24s  %10s  %10s  %10s  %s\n" "case" "qui" "html" "AE" "pct"
printf '%s\n' "--------------------------------------------------------------------"

total=0
sum_ae=0
sum_px=0

for q in "$QUI_DIR"/*.png; do
    name="$(basename "$q")"
    w="$REF_DIR/$name"
    [[ -f "$w" ]] || { echo "  $name: NO REF"; continue; }
    total=$((total+1))
    qw=$(magick identify -format "%w" "$q")
    qh=$(magick identify -format "%h" "$q")
    ww=$(magick identify -format "%w" "$w")
    wh=$(magick identify -format "%h" "$w")
    maxW=$qw; (( ww > maxW )) && maxW=$ww
    maxH=$qh; (( wh > maxH )) && maxH=$wh

    bg="white"
    qpad=$(mktemp -t qui_html_cmp.XXXX.png)
    wpad=$(mktemp -t qui_html_cmp.XXXX.png)
    diff=$(mktemp -t qui_html_cmp.XXXX.png)
    magick "$q" -background "$bg" -gravity NorthWest -extent "${maxW}x${maxH}" "$qpad"
    magick "$w" -background "$bg" -gravity NorthWest -extent "${maxW}x${maxH}" "$wpad"

    ae_raw=$(compare -metric AE "$qpad" "$wpad" "$diff" 2>&1 || true)
    if [[ "$ae_raw" =~ \(([0-9]+)\) ]]; then
        ae="${BASH_REMATCH[1]}"
    else
        ae=$(awk -v v="$ae_raw" 'BEGIN { printf "%d", v + 0 }')
    fi
    px=$((maxW*maxH))
    pct=$(awk -v d="$ae" -v p="$px" 'BEGIN { if (p==0) print "0.00"; else printf "%.2f", (d/p)*100 }')
    sum_ae=$(awk -v a="$sum_ae" -v b="$ae" 'BEGIN { printf "%d", a + b }')
    sum_px=$(awk -v a="$sum_px" -v b="$px" 'BEGIN { printf "%d", a + b }')

    out="$CMP_DIR/$name"
    label_h=18
    magick \
      \( "$qpad" -bordercolor "$bg" -border 0 -gravity South -background white -splice 0x${label_h} -font /System/Library/Fonts/Supplemental/Arial.ttf -pointsize 10 -fill black -annotate +0+2 "qui ${qw}x${qh}" \) \
      \( "$wpad" -bordercolor "$bg" -border 0 -gravity South -background white -splice 0x${label_h} -font /System/Library/Fonts/Supplemental/Arial.ttf -pointsize 10 -fill black -annotate +0+2 "html ${ww}x${wh}" \) \
      \( "$diff" -bordercolor white -border 0 -gravity South -background white -splice 0x${label_h} -font /System/Library/Fonts/Supplemental/Arial.ttf -pointsize 10 -fill black -annotate +0+2 "diff AE=${ae} (${pct}%)" \) \
      +smush 8 -background white -bordercolor white -border 4 "$out"
    rm -f "$qpad" "$wpad" "$diff"

    printf "%-24s  %10s  %10s  %10s  %5s%%\n" "$name" "${qw}x${qh}" "${ww}x${wh}" "$ae" "$pct"
done

if (( sum_px > 0 )); then
    overall=$(awk -v d="$sum_ae" -v p="$sum_px" 'BEGIN { printf "%.2f", (d/p)*100 }')
    echo
    echo "[4/4] Overall: ${sum_ae}/${sum_px} pixels differ (${overall}%) across $total cases."
    echo "       Comparison strips: $CMP_DIR/"
fi
