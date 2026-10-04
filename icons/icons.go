// Package icons is qui's optional icon set: a curated collection of
// 24px outlined glyphs, parsed into SVG documents ready to drop into
// any widget that accepts a qui.VectorSource (Button.IconVector,
// IconButton.Icon, …). It is an asset package only — no design system,
// no theme coupling, and nothing in qui's engine or widgets depends on
// it.
//
// Source: the Material Symbols set,
// https://github.com/google/material-design-icons (Apache 2.0).
// SVG files live in icons/svg/ and are pulled by icons/scripts/fetch.sh;
// re-run that script to refresh against the latest upstream symbols.
//
// Authoring grid: each SVG uses viewBox "0 -960 960 960". qui's svg
// package applies the negative-Y origin via translate(-vb.X, -vb.Y) in
// svg/render.go, so the docs render identically to any standard 24×24
// SVG when handed to canvas.DrawVector.
//
// Two access paths:
//   - typed accessors (icons.Settings, icons.Search) — compile-time
//     safety for the curated set
//   - lookup by name (icons.Get("settings")) — for data-driven menus,
//     toolbar config files, etc.
package icons

import (
	"embed"
	"sort"

	"github.com/qizhanchan/qui/svg"
)

//go:embed svg/*.svg
var svgFS embed.FS

// registry holds every embedded icon keyed by its canonical
// snake_case Material Symbols name. Populated eagerly as a package-level
// var (not init()) so vars.go's typed accessors — which call mustLoad
// from their own initializers — see a populated registry. Go's
// initialization analysis follows the chain mustLoad → registry and
// orders this before any consumer.
var registry = buildRegistry()

func buildRegistry() map[string]*svg.Document {
	out := make(map[string]*svg.Document)
	entries, err := svgFS.ReadDir("svg")
	if err != nil {
		panic("icons: read embedded svg dir: " + err.Error())
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if len(name) <= 4 || name[len(name)-4:] != ".svg" {
			continue
		}
		data, err := svgFS.ReadFile("svg/" + name)
		if err != nil {
			panic("icons: read " + name + ": " + err.Error())
		}
		doc, err := svg.ParseBytes(data)
		if err != nil {
			panic("icons: parse " + name + ": " + err.Error())
		}
		out[name[:len(name)-4]] = doc
	}
	return out
}

// Get returns the icon registered under name (canonical snake_case
// Material Symbols name like "arrow_back" / "more_vert"). Returns nil
// if the name isn't bundled — callers should treat that as "no icon"
// rather than panic so data-driven UIs degrade gracefully.
func Get(name string) *svg.Document {
	return registry[name]
}

// MustGet is Get with a panic on missing name. Use only when the icon
// is statically known (e.g. populating an internal toolbar config that
// must succeed at boot).
func MustGet(name string) *svg.Document {
	d, ok := registry[name]
	if !ok {
		panic("icons: no icon named " + name)
	}
	return d
}

// Names returns every bundled icon name in lexicographic order. Useful
// for icon-picker UIs and tests.
func Names() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func mustLoad(name string) *svg.Document {
	d, ok := registry[name]
	if !ok {
		panic("icons: icon not bundled: " + name)
	}
	return d
}
