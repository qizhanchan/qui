// Package typeset is qui's text engine: the font registry and fallback
// chains, OpenType shaping (go-text/typesetting), bidi, line breaking and
// grapheme segmentation, paragraph and rich-text layout, the inline
// formatting context, caret and selection geometry, emoji detection and
// providers, and SFNT parsing and subsetting for PDF embedding.
//
// It sits above internal/gfx and below the rasterizers: it measures and
// shapes, and draws only through gfx.Canvas (a canvas with a DrawShapedLine
// method paints shaped lines itself). The engine hands in its locale
// through SetLocaleSource; caches of measured or shaped text key on
// that generation. Root qui re-exports the public API (typeset.go).
package typeset
