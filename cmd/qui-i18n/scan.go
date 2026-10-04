package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// keyRef is one message key discovered in the source tree.
type keyRef struct {
	key string
	// plural is true when at least one reference passes a count, which
	// tells extract to seed plural forms rather than a flat string.
	plural bool
	// args are the placeholder names the code supplies, unioned across
	// every reference. lint compares these against the {name} tokens in
	// each translation.
	args map[string]bool
	// sites are "file:line" locations, for lint output.
	sites []string
}

func (r *keyRef) note(site string, plural bool, args []string) {
	r.plural = r.plural || plural
	if len(args) > 0 && r.args == nil {
		r.args = map[string]bool{}
	}
	for _, a := range args {
		r.args[a] = true
	}
	r.sites = append(r.sites, site)
}

// textFuncs are the call expressions whose FIRST string-literal argument
// is a message key. The bool is whether the second argument is a count.
//
// Matched on the selector's method/function name only, not the package
// qualifier: apps alias these imports freely (i18n, qi, msg), and a
// false positive costs one spurious catalog entry a human deletes, while
// a false negative costs an untranslated string nobody notices.
var textFuncs = map[string]bool{
	"T":                 false,
	"TOr":               false,
	"TN":                true,
	"TNf":               true,
	"Translate":         false,
	"TranslateOr":       false,
	"TranslatePlural":   true,
	"SetTextKey":        false,
	"SetLabelKey":       false,
	"SetPlaceholderKey": false,
	"TPlaceholder":      false,
	"TTitle":            false,
	"TValue":            false,
}

// keyArgIndex is the position of the key argument. Translate and
// TranslateOr take the locale first.
var keyArgIndex = map[string]int{
	"Translate":       1,
	"TranslateOr":     1,
	"TranslatePlural": 1,
}

// countArgIndex is the position of the count argument for plural calls.
var countArgIndex = map[string]int{
	"TN":              1,
	"TNf":             1,
	"TranslatePlural": 2,
}

// i18nAttrRe finds data-i18n attributes in HTML — both in .html files
// and in Go string literals holding inline markup.
var i18nAttrRe = regexp.MustCompile(`data-i18n(?:-placeholder|-title|-value)?\s*=\s*["']([^"']+)["']`)

// i18nCountAttrRe detects a sibling count attribute so extract seeds
// plural forms for keys used that way in markup.
var i18nCountAttrRe = regexp.MustCompile(`data-i18n-count\s*=\s*["'][^"']*["']`)

// scanTree walks dir and returns every message key it can find.
//
// Skips vendor/, testdata/ and dot-directories: a key that only appears
// in a fixture is not a key the app ships.
func scanTree(dir string) (map[string]keyRef, error) {
	refs := map[string]keyRef{}
	fset := token.NewFileSet()

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == "vendor" || name == "testdata" || name == "node_modules" ||
				(strings.HasPrefix(name, ".") && name != "." && name != "..") {
				return filepath.SkipDir
			}
			return nil
		}
		switch {
		case strings.HasSuffix(path, ".go"):
			return scanGoFile(fset, path, refs)
		case strings.HasSuffix(path, ".html"), strings.HasSuffix(path, ".htm"):
			return scanMarkupFile(path, refs)
		}
		return nil
	})
	return refs, err
}

func scanGoFile(fset *token.FileSet, path string, refs map[string]keyRef) error {
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		// A file that does not parse is a compile error the user already
		// has; reporting it again here is noise, and skipping it lets
		// extract work on a tree with one broken file.
		return nil
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			scanCall(fset, node, refs)
		case *ast.BasicLit:
			// Inline markup in a Go string literal.
			if node.Kind == token.STRING {
				if s, err := strconv.Unquote(node.Value); err == nil && strings.Contains(s, "data-i18n") {
					noteMarkup(s, fset.Position(node.Pos()).String(), refs)
				}
			}
		}
		return true
	})
	return nil
}

func scanCall(fset *token.FileSet, call *ast.CallExpr, refs map[string]keyRef) {
	name := calleeName(call.Fun)
	if name == "" {
		return
	}
	isPlural, known := textFuncs[name]
	if !known {
		return
	}
	idx := keyArgIndex[name]
	if idx >= len(call.Args) {
		return
	}
	key, ok := stringLit(call.Args[idx])
	if !ok || key == "" {
		// A non-literal key (a variable, a concatenation) cannot be
		// extracted. That is a real limitation, reported by lint rather
		// than silently ignored — see lintDynamicKeys.
		return
	}

	site := fset.Position(call.Pos()).String()
	// Placeholder names are the string literals in the trailing
	// name/value pairs, starting after the key (and after the count for
	// plural calls).
	start := idx + 1
	if ci, has := countArgIndex[name]; has {
		start = ci + 1
	}
	var args []string
	for i := start; i+1 < len(call.Args); i += 2 {
		if s, ok := stringLit(call.Args[i]); ok {
			args = append(args, s)
		}
	}

	r := refs[key]
	r.key = key
	r.note(site, isPlural, args)
	refs[key] = r
}

// calleeName returns the identifier a call resolves to, ignoring the
// package or receiver qualifier: both i18n.T(...) and btn.SetTextKey(...)
// reduce to their final name.
func calleeName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

func scanMarkupFile(path string, refs map[string]keyRef) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	noteMarkup(string(data), path, refs)
	return nil
}

// noteMarkup records every data-i18n key in a chunk of markup.
//
// Plural detection is per-element rather than per-document: a count
// attribute anywhere in the source would otherwise mark every key in the
// file plural. We approximate an element as the text between the key
// match and the next '>'.
func noteMarkup(src, site string, refs map[string]keyRef) {
	for _, m := range i18nAttrRe.FindAllStringSubmatchIndex(src, -1) {
		key := src[m[2]:m[3]]
		if key == "" {
			continue
		}
		tagEnd := strings.IndexByte(src[m[1]:], '>')
		elem := src[m[0]:]
		if tagEnd >= 0 {
			elem = src[m[0] : m[1]+tagEnd]
		}
		r := refs[key]
		r.key = key
		r.note(site, i18nCountAttrRe.MatchString(elem), argNamesInMarkup(elem))
		refs[key] = r
	}
}

var i18nArgAttrRe = regexp.MustCompile(`data-i18n-arg-([A-Za-z0-9_]+)\s*=`)

func argNamesInMarkup(elem string) []string {
	var out []string
	for _, m := range i18nArgAttrRe.FindAllStringSubmatch(elem, -1) {
		out = append(out, m[1])
	}
	return out
}

// sortedRefKeys returns the keys of a ref map in stable order.
func sortedRefKeys(refs map[string]keyRef) []string {
	out := make([]string, 0, len(refs))
	for k := range refs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
