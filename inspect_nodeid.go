package qui

import (
	"errors"
	"fmt"
	"strconv"
	"sync/atomic"
)

// NodeID is the process-wide identity of an inspectable node: a widget, a
// self-drawn accessible child, a DOM document/fragment, a reactive component
// instance. It is what the agent protocol hands out as `nodeId` and what the
// selector grammar addresses as `@123`.
//
// IDs come from one monotonic counter and are never reused, so an ID that no
// longer resolves means exactly "that node is gone" (ErrStaleNode) — never
// "some other node now sits there", which is what a positional path would
// silently answer after a sibling insertion. Identity follows the object, so
// a widget moved by a keyed reorder or a reparent keeps its ID.
//
// Allocation is lazy: a widget gets its ID the first time anything asks.
// That covers widgets built from a struct literal without NewBaseWidget, and
// keeps a BaseWidget copied by value before first use from sharing an ID.
type NodeID uint64

var nodeIDCounter atomic.Uint64

// NewNodeID allocates a fresh NodeID. Packages with inspectable nodes that are
// not widgets (reactive component instances) draw from the same counter so
// every ID in a protocol session lives in one space.
func NewNodeID() NodeID { return NodeID(nodeIDCounter.Add(1)) }

// String renders the ID in its selector form, "@123".
func (id NodeID) String() string { return "@" + strconv.FormatUint(uint64(id), 10) }

// ParseNodeRef parses the "@123" reference form. ok is false for anything
// else, including "@0".
func ParseNodeRef(s string) (NodeID, bool) {
	if len(s) < 2 || s[0] != '@' {
		return 0, false
	}
	n, err := strconv.ParseUint(s[1:], 10, 64)
	if err != nil || n == 0 {
		return 0, false
	}
	return NodeID(n), true
}

// ErrStaleNode is returned when a node reference no longer resolves: the
// node was removed from every window's tree (or never existed). It wraps
// ErrNoMatch so callers that only distinguish "found / not found" keep
// working; agents that hold references check for it to know they must
// re-resolve rather than retry.
var ErrStaleNode = fmt.Errorf("%w: node is no longer in the tree", ErrNoMatch)

// NodeID returns the widget's process-wide identity, allocating it on first
// use. Safe from any goroutine.
func (b *BaseWidget) NodeID() NodeID {
	if id := atomic.LoadUint64(&b.nodeID); id != 0 {
		return NodeID(id)
	}
	atomic.CompareAndSwapUint64(&b.nodeID, 0, uint64(NewNodeID()))
	return NodeID(atomic.LoadUint64(&b.nodeID))
}

// SyntheticNodeID returns a stable NodeID for a node this widget publishes
// but that is not itself a widget — a self-drawn accessible child, a DOM
// fragment wrapper — keyed by a string unique within the widget. The ID
// lives as long as the widget does. UI goroutine only.
func (b *BaseWidget) SyntheticNodeID(key string) NodeID {
	if id, ok := b.synthIDs[key]; ok {
		return id
	}
	if b.synthIDs == nil {
		b.synthIDs = map[string]NodeID{}
	}
	id := NewNodeID()
	b.synthIDs[key] = id
	return id
}

type nodeIdentified interface{ NodeID() NodeID }

type syntheticNodeOwner interface{ SyntheticNodeID(key string) NodeID }

// WidgetNodeID returns w's NodeID, or 0 for a widget that does not embed
// BaseWidget.
func WidgetNodeID(w Widget) NodeID {
	if n, ok := w.(nodeIdentified); ok {
		return n.NodeID()
	}
	return 0
}

// WidgetSyntheticNodeID returns owner's synthetic NodeID for key, or 0 when
// owner cannot hold one. UI goroutine only.
func WidgetSyntheticNodeID(owner Widget, key string) NodeID {
	if o, ok := owner.(syntheticNodeOwner); ok {
		return o.SyntheticNodeID(key)
	}
	return 0
}

// NodeByID returns the AX node with the given ID from the window's current
// tree (main tree and overlays), or ErrStaleNode. UI goroutine only.
func (w *Window) NodeByID(id NodeID) (*AXNode, error) {
	return w.AccessibilityTree().NodeByID(id)
}

// NodeByID finds a node in an already-captured tree.
func (t *AccessibilityTree) NodeByID(id NodeID) (*AXNode, error) {
	if id == 0 {
		return nil, errors.New("qui: node id 0 is never assigned")
	}
	for _, n := range t.flatten() {
		if n.NodeID == id {
			return n, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrStaleNode, id)
}

// DocumentNodeID is the NodeID of the window's DOM #document node — the root
// the protocol's DOM view hangs the main tree and overlay fragments under.
// Safe from any goroutine.
func (w *Window) DocumentNodeID() NodeID {
	if w == nil {
		return 0
	}
	if id := atomic.LoadUint64(&w.docNodeID); id != 0 {
		return NodeID(id)
	}
	atomic.CompareAndSwapUint64(&w.docNodeID, 0, uint64(NewNodeID()))
	return NodeID(atomic.LoadUint64(&w.docNodeID))
}
