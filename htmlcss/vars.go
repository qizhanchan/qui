package htmlcss

import "strings"

// CSS custom properties (--x) + var() resolution. Custom properties
// cascade like any declaration and INHERIT down the tree; var(--x,
// fallback) references are substituted into other property values after
// the cascade picks winners but before interpret reads them.

// maxVarExpansion caps a custom property's substituted length: chained
// `--b: var(--a) var(--a)` doubles per link, so a few dozen links would
// otherwise build gigabytes. Over the cap the property is invalid.
const maxVarExpansion = 64 << 10

// buildCustomProps computes this node's inherited-then-overridden custom
// property table from the parent's table plus any `--x` declarations in
// the (post-cascade) merged map m. var() inside custom-prop values is
// itself resolved, so `--a: var(--b)` works. A property in a reference
// cycle (or over maxVarExpansion) is invalid and left out of the table,
// so a var() naming it falls back — as in CSS.
//
// The tables are never written once built, so an element that declares no
// custom property shares its parent's (a page styled through a few dozen
// tokens on its root would otherwise copy them into every element).
// varResolver only writes into the fresh table built here.
func buildCustomProps(parent *ComputedStyle, m map[string]string) map[string]string {
	own := false
	for k := range m {
		if strings.HasPrefix(k, "--") {
			own = true
			break
		}
	}
	if !own && parent != nil && parent.customProps != nil {
		return parent.customProps
	}
	props := map[string]string{}
	if parent != nil {
		// Inherited values were resolved on the parent: no var() left.
		for k, v := range parent.customProps {
			props[k] = v
		}
	}
	r := varResolver{done: props, raw: map[string]string{}}
	for k, v := range m {
		if strings.HasPrefix(k, "--") {
			if strings.Contains(v, "var(") {
				r.raw[k] = v
				delete(props, k)
			} else {
				props[k] = v
			}
		}
	}
	for k := range r.raw {
		r.resolveProp(k) // stores into props (r.done) or leaves it out
	}
	return props
}

// resolveMapVars returns a copy of m with var() substituted in every
// non-custom property value, using the supplied custom-prop table. A
// value whose var() can't be resolved (unset with no fallback) is invalid
// at computed-value time and dropped, so the property inherits or takes
// its initial value instead of reaching interpret as literal `var(...)`.
func resolveMapVars(m, props map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	r := varResolver{done: props}
	for k, v := range m {
		if strings.HasPrefix(k, "--") {
			continue // custom props aren't consumed as normal properties
		}
		if strings.Contains(v, "var(") {
			if sub, ok := r.substitute(v); ok {
				out[k] = sub
			}
		} else {
			out[k] = v
		}
	}
	return out
}

// varResolver resolves custom properties on demand. done holds resolved
// values (and receives newly resolved ones); raw holds this node's own
// declarations still containing var(); visiting detects cycles.
type varResolver struct {
	done     map[string]string
	raw      map[string]string
	visiting map[string]bool
	invalid  map[string]bool
}

// resolveProp returns the resolved value of custom property name, or
// false when it is unset or invalid (a cycle member, or too long).
func (r *varResolver) resolveProp(name string) (string, bool) {
	if v, ok := r.done[name]; ok {
		return v, true
	}
	raw, ok := r.raw[name]
	if !ok || r.invalid[name] || r.visiting[name] {
		return "", false // unset, known-bad, or a cycle back to itself
	}
	if r.visiting == nil {
		r.visiting = map[string]bool{}
	}
	r.visiting[name] = true
	v, ok := r.substitute(raw)
	delete(r.visiting, name)
	if !ok {
		if r.invalid == nil {
			r.invalid = map[string]bool{}
		}
		r.invalid[name] = true
		return "", false
	}
	r.done[name] = v
	return v, true
}

// substitute replaces every var(--name[, fallback]) in value. It fails
// when a reference is unresolvable with no (valid) fallback, or the
// result grows past maxVarExpansion.
func (r *varResolver) substitute(value string) (string, bool) {
	if !strings.Contains(value, "var(") {
		return value, true
	}
	var b strings.Builder
	for i := 0; i < len(value); {
		if !strings.HasPrefix(value[i:], "var(") {
			b.WriteByte(value[i])
			i++
			continue
		}
		j := i + 4
		pd := 1
		for j < len(value) {
			switch value[j] {
			case '(':
				pd++
			case ')':
				pd--
			}
			if pd == 0 {
				break
			}
			j++
		}
		name, fallback := splitVarArgs(value[i+4 : j]) // between '(' and ')'
		repl, ok := r.resolveProp(strings.TrimSpace(name))
		if !ok {
			if fallback == "" {
				return "", false
			}
			if repl, ok = r.substitute(fallback); !ok {
				return "", false
			}
		}
		b.WriteString(strings.TrimSpace(repl))
		if b.Len() > maxVarExpansion {
			return "", false
		}
		if j < len(value) {
			i = j + 1 // skip past ')'
		} else {
			i = j
		}
	}
	return b.String(), true
}

// splitVarArgs splits `--name, fallback...` at the first top-level comma.
// The fallback (which may itself contain commas / var()) is returned whole.
func splitVarArgs(inner string) (name, fallback string) {
	parts := splitTopLevel(inner, ',')
	if len(parts) == 0 {
		return "", ""
	}
	name = strings.TrimSpace(parts[0])
	if len(parts) > 1 {
		fallback = strings.TrimSpace(strings.Join(parts[1:], ","))
	}
	return name, fallback
}
