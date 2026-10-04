#!/usr/bin/env bash
# Fetch a curated subset of Material Symbols Outlined SVGs from Google's
# google/material-design-icons GitHub repository into icons/svg/.
#
# Source: https://github.com/google/material-design-icons (Apache 2.0).
# Each file is the 24px outlined variant; viewBox is "0 -960 960 960"
# (Material Symbols' authoring grid) — the qui svg package handles the
# negative-Y origin correctly via the translate(-vb.X, -vb.Y) step in
# svg/render.go.
#
# Re-run idempotently; existing files are overwritten with the latest
# upstream version. After running, regenerate Go bindings is automatic
# via the //go:embed all:svg directive in icons.go.

set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
out_dir="$script_dir/../svg"
mkdir -p "$out_dir"

base="https://raw.githubusercontent.com/google/material-design-icons/master/symbols/web"
style="materialsymbolsoutlined"
suffix="_24px.svg"

# Curated Material Symbols icon set. Names match Google's canonical
# Material Symbols name; the on-disk file uses the same name with .svg
# suffix so a downstream Go lookup table can be generated mechanically.
icons=(
  # Navigation
  arrow_back arrow_forward arrow_upward arrow_downward
  chevron_left chevron_right expand_more expand_less
  menu more_vert more_horiz close
  home navigate_before navigate_next first_page last_page

  # Actions
  add remove delete edit save
  content_copy content_paste content_cut
  search settings refresh undo redo
  share download upload print send
  filter_list sort tune

  # Communication
  mail chat call notifications
  person group account_circle

  # Status / feedback
  check check_circle done
  info warning error help
  cancel block

  # Media
  play_arrow pause stop
  skip_next skip_previous
  volume_up volume_off volume_down
  fullscreen fullscreen_exit
  photo_camera image videocam mic

  # Files
  folder folder_open file_present
  attach_file link description draft
  cloud cloud_upload cloud_download
  note_add create_new_folder unfold_less my_location

  # Toggles. Material Symbols' default style is OUTLINED (hollow); the
  # filled counterpart is the same glyph with the `FILL` axis at 1, not
  # a separate icon name. We ship outlined only here — apps that need
  # the filled version can switch the variant at runtime via the Symbols
  # font (future work). Legacy `*_border` names from Material Icons do
  # not resolve in Symbols.
  visibility visibility_off
  favorite
  star
  bookmark
  lock lock_open

  # Layout / view
  dashboard view_list grid_view view_module
  calendar_today schedule timer

  # Editing
  format_bold format_italic format_underlined
  format_align_left format_align_center format_align_right format_align_justify
  format_list_bulleted format_list_numbered
  format_color_text format_color_fill
  border_all wrap_text find_replace filter_alt insert_chart

  # Theme / misc
  dark_mode light_mode palette
  code terminal build bug_report
  language public lightbulb
  science school
  shopping_cart store
  location_on map
  wifi bluetooth
  thumb_up thumb_down
  mood
  zoom_in zoom_out
)

count=0
failed=()
for name in "${icons[@]}"; do
  url="$base/$name/$style/${name}${suffix}"
  out="$out_dir/${name}.svg"
  if curl -sSf --max-time 10 -o "$out" "$url"; then
    count=$((count+1))
  else
    failed+=("$name")
  fi
done

printf 'fetched %d icons into %s\n' "$count" "$out_dir"
if [ ${#failed[@]} -gt 0 ]; then
  printf 'FAILED (%d):\n' "${#failed[@]}"
  printf '  %s\n' "${failed[@]}"
  exit 1
fi
