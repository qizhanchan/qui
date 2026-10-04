package htmlcss

import "strings"

// CSS custom properties (--x) + var() resolution. Custom properties
// cascade like any declaration and INHERIT down the tree; var(--x,
// fallback) references are substituted into other property values after
// the cascade picks winners but before interpret reads them.

// buildCustomProps computes this node's inherited-then-overridden custom
// property table from the parent's table plus any `--x` declarations in
// the (post-cascade) merged map m. var() inside custom-prop values is
// itself resolved (bounded) so `--a: var(--b)` works.
func buildCustomProps(parent *ComputedStyle, m map[string]string) map[string]string {
	props := map[string]string{}
	if parent != nil {
		for k, v := range parent.customProps {
			props[k] = v
		}
	}
	for k, v := range m {
		if strings.HasPrefix(k, "--") {
			props[k] = v
		}
	}
	// Resolve var() references that appear inside custom-prop values.
	for k, v := range props {
		if strings.Contains(v, "var(") {
			props[k] = resolveVars(v, props, 0)
		}
	}
	return props
}

// resolveMapVars returns a copy of m with var() substituted in every
// non-custom property value, using the supplied custom-prop table.
func resolveMapVars(m, props map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		if strings.HasPrefix(k, "--") {
			continue // custom props aren't consumed as normal properties
		}
		if strings.Contains(v, "var(") {
			out[k] = resolveVars(v, props, 0)
		} else {
			out[k] = v
		}
	}
	return out
}

// resolveVars substitutes every var(--name[, fallback]) in value with the
// custom property's value (or the fallback when unset). Nested var() in
// the fallback / substituted value is resolved recursively up to a small
// depth cap (guards against cyclic definitions).
func resolveVars(value string, props map[string]string, depth int) string {
	if depth > 16 || !strings.Contains(value, "var(") {
		return value
	}
	var b strings.Builder
	for i := 0; i < len(value); {
		if strings.HasPrefix(value[i:], "var(") {
			j := i + 4
			pd := 1
			for j < len(value) && pd > 0 {
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
			inner := value[i+4 : j] // between '(' and ')'
			name, fallback := splitVarArgs(inner)
			repl := ""
			if v, ok := props[strings.TrimSpace(name)]; ok {
				repl = v
			} else if fallback != "" {
				repl = fallback
			}
			b.WriteString(resolveVars(strings.TrimSpace(repl), props, depth+1))
			if j < len(value) {
				i = j + 1 // skip past ')'
			} else {
				i = j
			}
		} else {
			b.WriteByte(value[i])
			i++
		}
	}
	return b.String()
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
