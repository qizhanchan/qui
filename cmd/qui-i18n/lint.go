package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type finding struct {
	kind   string
	detail string
}

// placeholderRe matches {name}, skipping the {{ }} escape.
var placeholderRe = regexp.MustCompile(`\{([A-Za-z0-9_]+)\}`)

func placeholdersIn(s string) map[string]bool {
	out := map[string]bool{}
	// Strip escaped braces first so "{{literal}}" is not read as a
	// placeholder named "literal".
	s = strings.ReplaceAll(strings.ReplaceAll(s, "{{", "\x00"), "}}", "\x00")
	for _, m := range placeholderRe.FindAllStringSubmatch(s, -1) {
		out[m[1]] = true
	}
	return out
}

// lintAll runs every check and returns findings in a stable order.
//
// The checks are chosen for what actually ships broken:
//   - a key in code with no catalog entry renders as the key
//   - a key in the source locale missing from a translation renders in
//     the wrong language (silently, via fallback)
//   - a placeholder the code supplies but the message omits drops data
//   - a placeholder the message wants but the code never supplies
//     renders a literal "{name}" to the user
//   - a plural message missing "other" renders empty in some locales
//   - a plural key used without a count always renders "other"
func lintAll(refs map[string]keyRef, cats map[string]map[string]catalogEntry) []finding {
	var out []finding

	locales := make([]string, 0, len(cats))
	for l := range cats {
		locales = append(locales, l)
	}
	sort.Strings(locales)

	// Pick the most complete catalog as the source of truth for
	// cross-locale comparison. "en" wins ties by convention.
	source := "en"
	if _, ok := cats[source]; !ok {
		best := 0
		for _, l := range locales {
			if n := len(cats[l]); n > best {
				best, source = n, l
			}
		}
	}
	srcCat := cats[source]

	// 1. Keys referenced in code but absent from the source catalog.
	for _, key := range sortedRefKeys(refs) {
		if _, ok := srcCat[key]; !ok {
			out = append(out, finding{
				kind:   "missing-key",
				detail: fmt.Sprintf("%q used at %s but not in %s catalog", key, firstSite(refs[key]), source),
			})
		}
	}

	// 2. Catalog keys nothing references. Not an error — a key may be
	//    used through a dynamic call — but worth surfacing before it
	//    rots for a year.
	for _, key := range sortedCatKeys(srcCat) {
		if _, ok := refs[key]; !ok {
			out = append(out, finding{
				kind:   "unused-key",
				detail: fmt.Sprintf("%q in %s catalog is not referenced in code", key, source),
			})
		}
	}

	// 3. Translations missing keys the source locale has.
	for _, loc := range locales {
		if loc == source {
			continue
		}
		var missing []string
		for _, key := range sortedCatKeys(srcCat) {
			if _, ok := cats[loc][key]; !ok {
				missing = append(missing, key)
			}
		}
		if len(missing) > 0 {
			out = append(out, finding{
				kind:   "incomplete-locale",
				detail: fmt.Sprintf("%s is missing %d key(s): %s", loc, len(missing), summarize(missing)),
			})
		}
	}

	// 4. Placeholder drift, both directions, in every locale.
	for _, loc := range locales {
		for _, key := range sortedCatKeys(cats[loc]) {
			entry := cats[loc][key]
			if entry.raw != "" {
				continue
			}
			ref, referenced := refs[key]
			msgPlaceholders := placeholdersIn(entry.value())

			// The message wants a placeholder the code never supplies.
			// "n" is exempt: plural calls bind it automatically.
			for name := range msgPlaceholders {
				if name == "n" {
					continue
				}
				if referenced && ref.args != nil && ref.args[name] {
					continue
				}
				if !referenced {
					continue // already reported as unused-key
				}
				out = append(out, finding{
					kind:   "unsupplied-placeholder",
					detail: fmt.Sprintf("%s/%q wants {%s} but no call site supplies it", loc, key, name),
				})
			}
			// The code supplies a placeholder the message drops. Only
			// reported against the source locale — a translator may
			// legitimately rephrase around one.
			if loc == source && referenced {
				for name := range ref.args {
					if !msgPlaceholders[name] {
						out = append(out, finding{
							kind:   "dropped-placeholder",
							detail: fmt.Sprintf("%s/%q ignores {%s} supplied at %s", loc, key, name, firstSite(ref)),
						})
					}
				}
			}
		}
	}

	// 5. Plural shape problems.
	for _, loc := range locales {
		for _, key := range sortedCatKeys(cats[loc]) {
			entry := cats[loc][key]
			if entry.raw != "" {
				continue
			}
			if entry.isPlural() {
				if _, ok := entry.forms["other"]; !ok {
					out = append(out, finding{
						kind:   "plural-missing-other",
						detail: fmt.Sprintf("%s/%q has no \"other\" form; every CLDR language requires it", loc, key),
					})
				}
				for cat := range entry.forms {
					if !validCategory(cat) {
						out = append(out, finding{
							kind:   "plural-bad-category",
							detail: fmt.Sprintf("%s/%q has unknown category %q", loc, key, cat),
						})
					}
				}
				if ref, ok := refs[key]; ok && !ref.plural {
					out = append(out, finding{
						kind: "plural-without-count",
						detail: fmt.Sprintf("%s/%q has plural forms but %s calls it without a count — it will always render \"other\"",
							loc, key, firstSite(ref)),
					})
				}
			} else if ref, ok := refs[key]; ok && ref.plural && loc == source {
				out = append(out, finding{
					kind: "count-without-plural",
					detail: fmt.Sprintf("%s/%q is called with a count at %s but has no plural forms",
						loc, key, firstSite(ref)),
				})
			}
		}
	}

	return out
}

func validCategory(s string) bool {
	for _, c := range pluralOrder {
		if c == s {
			return true
		}
	}
	return false
}

func firstSite(r keyRef) string {
	if len(r.sites) == 0 {
		return "?"
	}
	return r.sites[0]
}

func sortedCatKeys(cat map[string]catalogEntry) []string {
	out := make([]string, 0, len(cat))
	for k := range cat {
		if strings.HasPrefix(k, "@") {
			continue
		}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// summarize keeps lint output readable when a locale is missing dozens
// of keys — the count is the actionable part, not the full list.
func summarize(keys []string) string {
	const max = 5
	if len(keys) <= max {
		return strings.Join(keys, ", ")
	}
	return strings.Join(keys[:max], ", ") + fmt.Sprintf(", … (+%d more)", len(keys)-max)
}
