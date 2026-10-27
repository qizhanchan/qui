package reactive

import "github.com/qizhanchan/qui"

// The Reactive protocol domain — React DevTools' component view: which
// components are mounted, their hook state, what is waiting to re-render.
// Host nodes carry their widget's NodeID, so they join the DOM and AX views.

func init() {
	qui.RegisterProtocolDomain(qui.ProtocolDomain{
		Name:        "Reactive",
		Description: "The reactive component tree: mounted components, hook state (state/signal/memo/effect), pending re-renders. Host nodes share their element's nodeId.",
		Commands: []qui.ProtocolCommand{
			qui.NewProtocolCommand("getComponentTree", "Snapshot of the component tree; root is null when the window has no reactive runtime.",
				func(ctx *qui.ProtocolContext, _ struct{}) (componentTreeResult, error) {
					rt := RuntimeForWindow(ctx.Window)
					if rt == nil {
						return componentTreeResult{}, nil
					}
					root := rt.Inspect()
					out := componentTreeResult{Root: root}
					if root != nil {
						out.Text = root.String()
					}
					return out, nil
				}),
		},
	})
}

type componentTreeResult struct {
	Root *ReactiveNode `json:"root"`
	Text string        `json:"text,omitempty" desc:"the same tree as an indented outline"`
}
