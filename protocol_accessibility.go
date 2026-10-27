package qui

import (
	"strconv"
	"strings"
)

// The Accessibility domain: the AX tree in CDP's flat shape — a node list
// linked by childIds, each node carrying its DOM identity in
// backendDOMNodeId (the same NodeID the DOM domain uses). Agents usually
// want Agent.snapshot instead, which is the same tree as compact text.

type axValue struct {
	Type  string `json:"type"`
	Value any    `json:"value"`
}

type axProperty struct {
	Name  string  `json:"name"`
	Value axValue `json:"value"`
}

type axNodeJSON struct {
	NodeID           string       `json:"nodeId" desc:"the NodeID as a string, as CDP's AXNodeId is"`
	Ignored          bool         `json:"ignored"`
	Role             *axValue     `json:"role,omitempty"`
	Name             *axValue     `json:"name,omitempty"`
	Value            *axValue     `json:"value,omitempty"`
	Properties       []axProperty `json:"properties,omitempty"`
	ParentID         string       `json:"parentId,omitempty"`
	ChildIDs         []string     `json:"childIds,omitempty"`
	BackendDOMNodeID NodeID       `json:"backendDOMNodeId,omitempty"`
	// qui extensions.
	Bounds  AXRect `json:"bounds"`
	Visible bool   `json:"visible"`
	Layer   string `json:"layer,omitempty"`
	ID      string `json:"id,omitempty" desc:"qui: developer-assigned widget ID (#id)"`
	NameKey string `json:"nameKey,omitempty" desc:"qui: message key the name was translated from ([key=] selector)"`
}

type axTreeParams struct {
	Depth  *int   `json:"depth,omitempty" desc:"levels below the root to include; omitted = whole tree"`
	NodeID NodeID `json:"nodeId,omitempty" desc:"qui: subtree root (default: the window root plus every overlay)"`
}

type axNodesResult struct {
	Nodes []axNodeJSON `json:"nodes"`
}

type axQueryParams struct {
	NodeID         NodeID `json:"nodeId,omitempty" desc:"scope: only this node's subtree"`
	BackendNodeID  NodeID `json:"backendNodeId,omitempty" desc:"alias of nodeId"`
	AccessibleName string `json:"accessibleName,omitempty" desc:"exact accessible name"`
	Role           string `json:"role,omitempty" desc:"exact role"`
}

type axPartialParams struct {
	NodeID         NodeID `json:"nodeId,omitempty"`
	BackendNodeID  NodeID `json:"backendNodeId,omitempty"`
	FetchRelatives bool   `json:"fetchRelatives,omitempty" desc:"include ancestors and direct children"`
}

func axID(id NodeID) string { return strconv.FormatUint(uint64(id), 10) }

// axFlatten emits n and (to depth) its descendants as flat CDP nodes.
func axFlatten(n *AXNode, parent NodeID, depth, maxDepth int, out *[]axNodeJSON) {
	j := axNodeToJSON(n, parent)
	descend := maxDepth < 0 || depth < maxDepth
	if descend {
		for _, c := range n.Children {
			j.ChildIDs = append(j.ChildIDs, axID(c.NodeID))
		}
	}
	*out = append(*out, j)
	if descend {
		for _, c := range n.Children {
			axFlatten(c, n.NodeID, depth+1, maxDepth, out)
		}
	}
}

func axNodeToJSON(n *AXNode, parent NodeID) axNodeJSON {
	j := axNodeJSON{
		NodeID:           axID(n.NodeID),
		Role:             &axValue{Type: "role", Value: n.Role},
		BackendDOMNodeID: n.NodeID,
		Bounds:           n.Bounds,
		Visible:          n.Visible,
		Layer:            n.Layer,
		ID:               n.ID,
		NameKey:          n.NameKey,
	}
	if parent != 0 {
		j.ParentID = axID(parent)
	}
	if n.Name != "" {
		j.Name = &axValue{Type: "computedString", Value: n.Name}
	}
	if n.Value != "" {
		j.Value = &axValue{Type: "string", Value: n.Value}
	}
	for _, bit := range strings.Split(n.State, "|") {
		if bit != "" {
			j.Properties = append(j.Properties, axProperty{Name: bit, Value: axValue{Type: "booleanOrUndefined", Value: true}})
		}
	}
	if n.Shortcut != "" {
		j.Properties = append(j.Properties, axProperty{Name: "keyshortcuts", Value: axValue{Type: "string", Value: n.Shortcut}})
	}
	if n.HasPopup {
		j.Properties = append(j.Properties, axProperty{Name: "hasPopup", Value: axValue{Type: "token", Value: "true"}})
	}
	j.Ignored = !n.Visible && n.Role == RoleGeneric
	return j
}

// axRoots returns the subtree roots a command works on: the node with id,
// or the main tree plus every overlay.
func axRoots(tree *AccessibilityTree, id NodeID) ([]*AXNode, error) {
	if id != 0 {
		n, err := tree.NodeByID(id)
		if err != nil {
			return nil, err
		}
		return []*AXNode{n}, nil
	}
	var roots []*AXNode
	if tree.Root != nil {
		roots = append(roots, tree.Root)
	}
	return append(roots, tree.Overlays...), nil
}

// axParent finds the parent of id in tree (0 for a root).
func axParent(tree *AccessibilityTree, id NodeID) []*AXNode {
	var chain []*AXNode
	var walk func(n *AXNode, path []*AXNode) bool
	walk = func(n *AXNode, path []*AXNode) bool {
		if n.NodeID == id {
			chain = append([]*AXNode(nil), path...)
			return true
		}
		for _, c := range n.Children {
			if walk(c, append(path, n)) {
				return true
			}
		}
		return false
	}
	roots, _ := axRoots(tree, 0)
	for _, r := range roots {
		if walk(r, nil) {
			break
		}
	}
	return chain
}

func (w *Window) syncedTree() (*AccessibilityTree, error) {
	var tree *AccessibilityTree
	err := w.synchronously(func() error { tree = w.AccessibilityTree(); return nil })
	return tree, err
}

func accessibilityDomain() ProtocolDomain {
	return ProtocolDomain{
		Name:        "Accessibility",
		Description: "The accessibility tree, flat as in CDP. Node IDs are qui NodeIDs (addressable as @<id>) and equal backendDOMNodeId.",
		Commands: []ProtocolCommand{
			NewProtocolCommand("getFullAXTree", "The whole AX tree (main tree, then overlays bottom-to-top), or the subtree under nodeId.",
				func(ctx *ProtocolContext, p axTreeParams) (axNodesResult, error) {
					tree, err := ctx.Window.syncedTree()
					if err != nil {
						return axNodesResult{}, err
					}
					roots, err := axRoots(tree, p.NodeID)
					if err != nil {
						return axNodesResult{}, err
					}
					maxDepth := -1
					if p.Depth != nil {
						maxDepth = *p.Depth
					}
					out := axNodesResult{Nodes: []axNodeJSON{}}
					for _, r := range roots {
						axFlatten(r, 0, 0, maxDepth, &out.Nodes)
					}
					return out, nil
				}),
			NewProtocolCommand("queryAXTree", "Nodes matching role and/or accessible name (exact), optionally within a subtree.",
				func(ctx *ProtocolContext, p axQueryParams) (axNodesResult, error) {
					tree, err := ctx.Window.syncedTree()
					if err != nil {
						return axNodesResult{}, err
					}
					scope := p.NodeID
					if scope == 0 {
						scope = p.BackendNodeID
					}
					roots, err := axRoots(tree, scope)
					if err != nil {
						return axNodesResult{}, err
					}
					out := axNodesResult{Nodes: []axNodeJSON{}}
					for _, r := range roots {
						for _, n := range flattenAX(r) {
							if (p.Role == "" || n.Role == p.Role) && (p.AccessibleName == "" || n.Name == p.AccessibleName) {
								out.Nodes = append(out.Nodes, axNodeToJSON(n, 0))
							}
						}
					}
					return out, nil
				}),
			NewProtocolCommand("getPartialAXTree", "One node; with fetchRelatives, its ancestors and direct children too.",
				func(ctx *ProtocolContext, p axPartialParams) (axNodesResult, error) {
					tree, err := ctx.Window.syncedTree()
					if err != nil {
						return axNodesResult{}, err
					}
					id := p.NodeID
					if id == 0 {
						id = p.BackendNodeID
					}
					n, err := tree.NodeByID(id)
					if err != nil {
						return axNodesResult{}, err
					}
					out := axNodesResult{}
					if !p.FetchRelatives {
						out.Nodes = []axNodeJSON{axNodeToJSON(n, 0)}
						return out, nil
					}
					chain := axParent(tree, id)
					var parent NodeID
					for _, a := range chain {
						out.Nodes = append(out.Nodes, axNodeToJSON(a, parent))
						parent = a.NodeID
					}
					axFlatten(n, parent, 0, 1, &out.Nodes)
					return out, nil
				}),
		},
	}
}
