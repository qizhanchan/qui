package qui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// The inspection protocol: a CDP-shaped (Chrome DevTools Protocol) set of
// domains, each a bag of typed commands and events, that the agent package
// serves over JSON-RPC. The registry lives in root so every package with
// something to inspect registers its own domain — root registers the engine
// domains (Accessibility, DOM, Input, Page, Agent, Debug…), htmlcss registers
// CSS and its DOM projection, reactive registers Reactive — and the agent
// package stays a transport that knows no domain by name.
//
// Wire shape, as in CDP: a request {id, method: "Domain.command", params}
// answers {id, result} or {id, error: {code, message, data}}; an event is
// {method: "Domain.event", params}. Commands are declared with Go types, and
// ProtocolSchema reflects them into a browser_protocol.json-shaped document
// that the docs, the CLI and the MCP tool list are generated from.

// JSON-RPC error codes used by the protocol.
const (
	ProtocolParseError     = -32700
	ProtocolInvalidRequest = -32600
	ProtocolMethodNotFound = -32601
	ProtocolInvalidParams  = -32602
	ProtocolServerError    = -32000
)

// ProtocolError is a command failure. Code is the JSON-RPC code; Reason is
// the semantic error an agent branches on (bad_selector, no_match,
// stale_node, modal_blocked, not_text_target, not_visible, disabled,
// timeout, target_closed, ambiguous_target, unsupported, invalid_params,
// error); Data carries extra context (a hint, a blocking node).
type ProtocolError struct {
	Code    int
	Message string
	Reason  string
	Data    map[string]any
}

func (e *ProtocolError) Error() string { return e.Message }

// MarshalJSON renders the JSON-RPC error object, with Reason folded into
// data.reason.
func (e *ProtocolError) MarshalJSON() ([]byte, error) {
	data := map[string]any{}
	for k, v := range e.Data {
		data[k] = v
	}
	if e.Reason != "" {
		data["reason"] = e.Reason
	}
	out := map[string]any{"code": e.Code, "message": e.Message}
	if len(data) > 0 {
		out["data"] = data
	}
	return json.Marshal(out)
}

// NewProtocolError builds a server error with a semantic reason.
func NewProtocolError(reason, format string, args ...any) *ProtocolError {
	return &ProtocolError{Code: ProtocolServerError, Reason: reason, Message: fmt.Sprintf(format, args...)}
}

// ProtocolErrorFor classifies an error from the engine into a
// ProtocolError, so every command reports the same reasons for the same
// failures.
func ProtocolErrorFor(err error) *ProtocolError {
	if err == nil {
		return nil
	}
	var pe *ProtocolError
	if errors.As(err, &pe) {
		return pe
	}
	e := &ProtocolError{Code: ProtocolServerError, Message: err.Error(), Reason: "error"}
	var modal *ErrModalBlocked
	var disabled *ErrDisabled
	switch {
	case errors.As(err, &modal):
		e.Reason = "modal_blocked"
		e.Data = map[string]any{"blockingPath": modal.BlockingPath, "hint": "dismiss the modal, or address it with :layer(modal)"}
	case errors.As(err, &disabled):
		e.Reason = "disabled"
	case errors.Is(err, ErrInvalidSelector):
		e.Reason = "bad_selector"
	case errors.Is(err, ErrStaleNode):
		e.Reason = "stale_node"
		e.Data = map[string]any{"hint": "the node is gone; resolve the selector again (Agent.snapshot)"}
	case errors.Is(err, ErrNoMatch):
		e.Reason = "no_match"
		e.Data = map[string]any{"hint": "re-read the tree (Agent.snapshot) and adjust the selector"}
	case errors.Is(err, ErrNotTextTarget):
		e.Reason = "not_text_target"
	case errors.Is(err, ErrNotVisible):
		e.Reason = "not_visible"
	case errors.Is(err, ErrActionTimeout), errors.Is(err, ErrWaitTimeout), errors.Is(err, context.DeadlineExceeded):
		e.Reason = "timeout"
	case errors.Is(err, ErrWindowClosed):
		e.Reason = "target_closed"
	}
	return e
}

// ProtocolSession is the caller of a command: one attached client. Emit
// sends an event to this session alone.
type ProtocolSession interface {
	ID() string
	Emit(method string, params any)
}

// ProtocolSessionToggles is implemented by sessions that keep switches
// beyond Domain.enable, such as LifecycleEventsToggle. A transport whose
// sessions lack it delivers every event of an enabled domain.
type ProtocolSessionToggles interface {
	SetToggle(name string, on bool)
}

// ProtocolContext is what a command handler receives.
type ProtocolContext struct {
	// Context is cancelled when the client goes away or the server stops.
	Context context.Context
	// Window is the target the command runs against. Nil only for commands
	// declared AppLevel.
	Window *Window
	// Session is the calling client; nil for one-shot HTTP calls.
	Session ProtocolSession
}

func (c *ProtocolContext) ctx() context.Context {
	if c.Context == nil {
		return context.Background()
	}
	return c.Context
}

// ProtocolCommand is one method of a domain.
type ProtocolCommand struct {
	Name        string
	Description string
	// Params / Result are the Go types the schema is reflected from.
	Params, Result reflect.Type
	// AppLevel commands run without a target window (Target.getTargets).
	AppLevel bool
	// Strict rejects unknown parameter names. Agent-facing commands are
	// strict, so a misspelt field ("selector" for "target") is an error
	// instead of a silently-empty parameter; CDP-mirrored ones are lenient,
	// because clients like the DevTools frontend send parameters qui does
	// not model.
	Strict  bool
	handler func(ctx *ProtocolContext, params json.RawMessage, strict bool) (any, error)
}

// Call decodes params and runs the command.
func (c ProtocolCommand) Call(ctx *ProtocolContext, params json.RawMessage) (any, error) {
	return c.handler(ctx, params, c.Strict)
}

// NewProtocolCommand declares a typed protocol command. P and R must be structs (use
// struct{} for none); their json tags name the fields and an optional
// `desc:"…"` tag documents each.
func NewProtocolCommand[P, R any](name, description string, fn func(ctx *ProtocolContext, params P) (R, error)) ProtocolCommand {
	cmd := ProtocolCommand{
		Name:        name,
		Description: description,
		Params:      reflect.TypeFor[P](),
		Result:      reflect.TypeFor[R](),
	}
	cmd.handler = func(ctx *ProtocolContext, raw json.RawMessage, strict bool) (any, error) {
		var p P
		if len(bytes.TrimSpace(raw)) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			dec := json.NewDecoder(bytes.NewReader(raw))
			if strict {
				dec.DisallowUnknownFields()
			}
			if err := dec.Decode(&p); err != nil {
				return nil, &ProtocolError{Code: ProtocolInvalidParams, Reason: "invalid_params",
					Message: fmt.Sprintf("%s: bad params: %v", name, err)}
			}
		}
		return fn(ctx, p)
	}
	return cmd
}

// WithStrict returns c with unknown-parameter rejection on.
func (c ProtocolCommand) WithStrict() ProtocolCommand { c.Strict = true; return c }

// WithAppLevel returns c marked as runnable without a target window.
func (c ProtocolCommand) WithAppLevel() ProtocolCommand { c.AppLevel = true; return c }

// ProtocolEvent declares an event a domain emits.
type ProtocolEvent struct {
	Name        string
	Description string
	Params      reflect.Type
}

// NewProtocolEvent declares a typed protocol event.
func NewProtocolEvent[P any](name, description string) ProtocolEvent {
	return ProtocolEvent{Name: name, Description: description, Params: reflect.TypeFor[P]()}
}

// ProtocolDomain groups commands and events under one name.
type ProtocolDomain struct {
	Name        string
	Description string
	Commands    []ProtocolCommand
	Events      []ProtocolEvent
	// Buffered domains have their events recorded in the server's event
	// buffer even with no session subscribed, so a polling client can read
	// them back (Log, Runtime). Unbuffered domains (DOM mutations) are
	// produced only while a session has enabled them.
	Buffered bool
}

var (
	protocolMu      sync.RWMutex
	protocolDomains = map[string]ProtocolDomain{}
)

// RegisterProtocolDomain adds (or replaces, by name) a domain.
func RegisterProtocolDomain(d ProtocolDomain) {
	protocolMu.Lock()
	defer protocolMu.Unlock()
	protocolDomains[d.Name] = d
}

// ProtocolDomains returns every registered domain, sorted by name.
func ProtocolDomains() []ProtocolDomain {
	protocolMu.RLock()
	defer protocolMu.RUnlock()
	out := make([]ProtocolDomain, 0, len(protocolDomains))
	for _, d := range protocolDomains {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// LookupProtocolDomain returns the named domain.
func LookupProtocolDomain(name string) (ProtocolDomain, bool) {
	protocolMu.RLock()
	defer protocolMu.RUnlock()
	d, ok := protocolDomains[name]
	return d, ok
}

// LookupProtocolCommand resolves "Domain.command".
func LookupProtocolCommand(method string) (ProtocolCommand, bool) {
	domain, name, ok := strings.Cut(method, ".")
	if !ok {
		return ProtocolCommand{}, false
	}
	d, ok := LookupProtocolDomain(domain)
	if !ok {
		return ProtocolCommand{}, false
	}
	for _, c := range d.Commands {
		if c.Name == name {
			return c, true
		}
	}
	return ProtocolCommand{}, false
}

// -------------------------------------------------------------------
// Events

// ProtocolEventSink receives every event a window emits. The agent server
// installs one and routes events to sessions and its buffer.
type ProtocolEventSink interface {
	// WantsEvents reports whether events of domain are wanted for w right
	// now. Producers check it before building an event, so an unwatched
	// domain costs nothing.
	WantsEvents(w *Window, domain string) bool
	HandleEvent(w *Window, method string, params any)
}

var (
	sinkMu     sync.RWMutex
	sinks      []ProtocolEventSink
	sinksCount atomic.Int32
)

// AddProtocolEventSink installs a sink; the returned func removes it.
func AddProtocolEventSink(s ProtocolEventSink) (remove func()) {
	sinkMu.Lock()
	sinks = append(sinks, s)
	sinksCount.Store(int32(len(sinks)))
	sinkMu.Unlock()
	return func() {
		sinkMu.Lock()
		defer sinkMu.Unlock()
		for i, x := range sinks {
			if x == s {
				sinks = append(sinks[:i:i], sinks[i+1:]...)
				break
			}
		}
		sinksCount.Store(int32(len(sinks)))
	}
}

// ProtocolEventsWanted reports whether any sink wants domain's events for
// w. Cheap when no sink is installed.
func ProtocolEventsWanted(w *Window, domain string) bool {
	if sinksCount.Load() == 0 {
		return false
	}
	sinkMu.RLock()
	defer sinkMu.RUnlock()
	for _, s := range sinks {
		if s.WantsEvents(w, domain) {
			return true
		}
	}
	return false
}

// EmitProtocolEvent delivers "Domain.event" to every sink that wants the
// domain. w may be nil for app-level events. Sinks must not block.
func EmitProtocolEvent(w *Window, method string, params any) {
	if sinksCount.Load() == 0 {
		return
	}
	domain, _, _ := strings.Cut(method, ".")
	sinkMu.RLock()
	list := append([]ProtocolEventSink(nil), sinks...)
	sinkMu.RUnlock()
	for _, s := range list {
		if s.WantsEvents(w, domain) {
			s.HandleEvent(w, method, params)
		}
	}
}

// -------------------------------------------------------------------
// Schema

// ProtocolSchema reflects the registered domains into the shape of CDP's
// browser_protocol.json: {version, domains: [{domain, description,
// commands: [{name, description, parameters, returns}], events}]}.
func ProtocolSchema() map[string]any {
	var domains []any
	for _, d := range ProtocolDomains() {
		dm := map[string]any{"domain": d.Name}
		if d.Description != "" {
			dm["description"] = d.Description
		}
		var cmds []any
		for _, c := range d.Commands {
			cm := map[string]any{"name": c.Name}
			if c.Description != "" {
				cm["description"] = c.Description
			}
			if ps := schemaFields(c.Params, 0); len(ps) > 0 {
				cm["parameters"] = ps
			}
			if rs := schemaFields(c.Result, 0); len(rs) > 0 {
				cm["returns"] = rs
			}
			cmds = append(cmds, cm)
		}
		if len(cmds) > 0 {
			dm["commands"] = cmds
		}
		var evs []any
		for _, e := range d.Events {
			em := map[string]any{"name": e.Name}
			if e.Description != "" {
				em["description"] = e.Description
			}
			if ps := schemaFields(e.Params, 0); len(ps) > 0 {
				em["parameters"] = ps
			}
			evs = append(evs, em)
		}
		if len(evs) > 0 {
			dm["events"] = evs
		}
		domains = append(domains, dm)
	}
	return map[string]any{
		"version": map[string]string{"major": "2", "minor": "0"},
		"domains": domains,
	}
}

// schemaFields lists a struct type's JSON fields as CDP parameter entries.
func schemaFields(t reflect.Type, depth int) []any {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}
	var out []any
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if f.Anonymous && name == "" {
			out = append(out, schemaFields(f.Type, depth)...)
			continue
		}
		if name == "" {
			name = f.Name
		}
		entry := schemaType(f.Type, depth)
		entry["name"] = name
		if strings.Contains(opts, "omitempty") || f.Type.Kind() == reflect.Pointer {
			entry["optional"] = true
		}
		if d := f.Tag.Get("desc"); d != "" {
			entry["description"] = d
		}
		out = append(out, entry)
	}
	return out
}

var rawMessageType = reflect.TypeFor[json.RawMessage]()

func schemaType(t reflect.Type, depth int) map[string]any {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == rawMessageType || t.Kind() == reflect.Interface {
		return map[string]any{"type": "any"}
	}
	if t == reflect.TypeFor[NodeID]() {
		return map[string]any{"type": "integer", "$ref": "NodeId"}
	}
	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": schemaType(t.Elem(), depth+1)}
	case reflect.Map:
		return map[string]any{"type": "object"}
	case reflect.Struct:
		m := map[string]any{"type": "object"}
		// Recursive types (a node with children) stop at a few levels.
		if depth < 3 {
			if props := schemaFields(t, depth+1); len(props) > 0 {
				m["properties"] = props
			}
		}
		return m
	}
	return map[string]any{"type": "any"}
}
