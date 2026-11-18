# third_party/typesetting

A maintained copy of [github.com/go-text/typesetting](https://github.com/go-text/typesetting)
(fonts, shaping, harfbuzz port, segmentation), kept inside the qui module so
every qui user gets our patches without a `replace` directive.

- **Upstream:** github.com/go-text/typesetting **v0.3.4**
- **License:** Unlicense OR BSD-3-Clause (see LICENSE, unchanged)
- **Copied:** non-test `.go` sources only (upstream tests need its large
  testdata and typesetting-utils); `testutils/` omitted. Import paths
  rewritten from `github.com/go-text/typesetting/...` to
  `github.com/qizhanchan/qui/third_party/typesetting/...`.

Only qui's text layers import it (`internal/typeset`, `internal/render`,
`pdf`); `internal/archtest` enforces that.

## Patches (keep this list current; mark each in code with `qui:`)

1. **Lazy gvar** (`font/variations.go`): glyph variation data is decoded
   per glyph on first use (atomic per-glyph cache) instead of for all
   glyphs in `NewFont`. The macOS system font's eager decode was ~28 MB of
   live heap. Verified identical outlines/advances against upstream for
   6000 glyphs × 4 weights of SFNS.ttf; `font/lazy_gvar_test.go`.
2. **Lazy gvar records** (`font/opentype/tables/xvar_src.go`): the table
   keeps its bytes and parses one glyph's `GlyphVariationData` on demand
   (`GlyphVariationDataAt`); offsets are still validated at load. The
   eager record parse was another ~15 MB for SFNS.
3. **Zero-copy tables** (`font/opentype/reader.go`, `BytesResource`): a font
   loaded from memory returns uncompressed tables as sub-slices of its
   bytes (a 23 MB CJK collection no longer gets its tables copied). `dst`
   is ignored for such fonts so a recycled buffer can never overwrite the
   font. qui loads every font this way (`internal/typeset`).

Patches 1–3 were checked together against upstream v0.3.4: identical
outlines and advances for 6000 glyphs × 4 weights of SFNS.ttf and
SFNSItalic.ttf.

## Updating from upstream

Copy the new release's non-test sources over this tree (keeping ORIGIN.md
and any `*_test.go` we added), rewrite the import paths, then re-apply the
patches above (search for `qui:`), and run `go test ./third_party/... ./internal/...`.
