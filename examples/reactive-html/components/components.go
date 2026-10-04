// Package components holds reusable reactive components in a SEPARATE
// package from the app, to verify cross-package composition: another
// package builds elements with the same h DSL + reactive hooks, and the
// app imports and mounts them like any other component.
package components

import (
	"github.com/qizhanchan/qui/reactive"
	h "github.com/qizhanchan/qui/reactive/html"
)

// Badge is a presentational (stateless) component: it returns a chainable
// builder so callers can compose it directly as a child node.
func Badge(text string) *h.Builder {
	return h.Span(text).Class("badge")
}

// Stepper is a self-contained STATEFUL component defined in this package.
// Each instance owns its own hook state (UseState), proving hook isolation
// works across package boundaries and that the h builders resolve the
// app's style engine even when constructed from an imported package.
func Stepper(label string) h.Node {
	return h.Component("Stepper", label, label, func(label string) h.Node {
		n, set := reactive.UseState(0)
		return h.Div(
			h.Span(label).Class("stepper-label"),
			h.Button("−").Class("step").OnClick(func() { set(n - 1) }),
			h.Span(n).Class("stepper-val").ID("stepper-"+label),
			h.Button("+").Class("step").OnClick(func() { set(n + 1) }),
		).Class("stepper")
	})
}
