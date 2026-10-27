package archtest

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const module = "github.com/qizhanchan/qui"

// allowed lists, per package (module-relative; "" is root qui), the module
// packages it may import. A trailing "/..." matches a subtree. Packages not
// covered by a rule fail the test, so a new package must state its place in
// the layering here. Third-party and standard-library imports are not
// checked.
var allowed = map[string][]string{
	// Root is the engine and the public facade. It may reach down into the
	// internal layers, never up into a subpackage.
	"": {"internal/gfx", "internal/typeset", "internal/render", "internal/platform/..."},

	// The internal layers, bottom up.
	"internal/gfx":          {},
	"internal/typeset":      {"internal/gfx", "internal/platform/contract", "internal/platform/macos"},
	"internal/render":       {"internal/gfx", "internal/typeset"},
	"internal/platform/...": {"internal/platform/..."},
	"internal/archtest":     {},

	// Exporters build on the internal layers directly.
	"pdf": {"internal/gfx", "internal/typeset", "internal/platform/macos"},
	"svg": {"", "internal/gfx", "internal/typeset"},

	// Everything else goes through root's public API.
	"widgets":          {""},
	"reactive":         {""},
	"reactive/html":    {"", "reactive", "htmlcss", "widgets"},
	"htmlcss":          {"", "svg", "widgets"},
	"scene3d":          {"", "widgets"},
	"icons":            {"svg"},
	"i18n":             {""},
	"anim":             {""},
	"fonts/...":        {""},
	"webview":          {""},
	"graphs":           {},
	"graphs/chart":     {"", "graphs"},
	"graphs/diagram":   {"", "pdf", "svg"},
	"physics":          {},
	"physics/...":      {"physics"},
	"media":            {"", "media/internal/..."},
	"media/...":        {"media/internal/..."},
	"agent":            {"", "agent/internal/..."},
	"agent/...":        {"agent/internal/..."},
	"cmd/...":          nil, // binaries and examples may import anything
	"examples/...":     nil,
	"tools/...":        nil,
	"graphs/*/cmd/...": nil,
}

// configs are the environments whose build-tagged imports are checked.
var configs = [][]string{
	{},
	{"GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0"},
	{"GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0"},
	{"GOOS=js", "GOARCH=wasm"},
}

func TestImportLayering(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go tool not on PATH")
	}
	touchSources(t)
	for _, env := range configs {
		for pkg, imports := range listImports(t, env) {
			rule, ok := ruleFor(pkg)
			if !ok {
				t.Errorf("%s: no layering rule; add one to allowed in imports_test.go", show(pkg))
				continue
			}
			if rule == nil {
				continue
			}
			for _, imp := range imports {
				if !matchesAny(imp, rule) {
					t.Errorf("%s imports %s, which its layer does not allow (env %v)", show(pkg), show(imp), env)
				}
			}
		}
	}
}

// touchSources stats every directory and Go file of the module. The imports
// are read by a go subprocess the test cache cannot see; statting them here
// makes any source change invalidate a cached pass.
func touchSources(t *testing.T) {
	t.Helper()
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != root && (d.Name() == "vendor" || strings.HasPrefix(d.Name(), ".")) {
			return filepath.SkipDir
		}
		if d.IsDir() || strings.HasSuffix(path, ".go") {
			_, err = os.Stat(path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// listImports returns each module package's module-internal imports.
func listImports(t *testing.T, env []string) map[string][]string {
	t.Helper()
	cmd := exec.Command("go", "list", "-e", "-json=ImportPath,Imports", module+"/...")
	cmd.Env = append(os.Environ(), env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list %v: %v\n%s", env, err, stderr.String())
	}
	res := map[string][]string{}
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var p struct {
			ImportPath string
			Imports    []string
		}
		if err := dec.Decode(&p); err != nil {
			t.Fatal(err)
		}
		pkg, ok := rel(p.ImportPath)
		if !ok {
			continue
		}
		var mine []string
		for _, imp := range p.Imports {
			if r, ok := rel(imp); ok {
				mine = append(mine, r)
			}
		}
		sort.Strings(mine)
		res[pkg] = mine
	}
	return res
}

// rel strips the module prefix; ok is false for packages outside it.
func rel(path string) (string, bool) {
	if path == module {
		return "", true
	}
	if r, ok := strings.CutPrefix(path, module+"/"); ok && !strings.HasPrefix(r, "vendor/") {
		return r, true
	}
	return "", false
}

// ruleFor returns the most specific rule covering pkg.
func ruleFor(pkg string) ([]string, bool) {
	if r, ok := allowed[pkg]; ok {
		return r, true
	}
	best, found := "", false
	var rule []string
	for pat, r := range allowed {
		if strings.HasSuffix(pat, "/...") && match(pkg, pat) && len(pat) > len(best) {
			best, rule, found = pat, r, true
		}
	}
	return rule, found
}

func matchesAny(imp string, pats []string) bool {
	for _, p := range pats {
		if match(imp, p) {
			return true
		}
	}
	return false
}

// match reports whether pkg matches pat: an exact path, a "dir/..." subtree,
// with "*" standing for one path element.
func match(pkg, pat string) bool {
	if base, ok := strings.CutSuffix(pat, "/..."); ok {
		pe, be := strings.Split(pkg, "/"), strings.Split(base, "/")
		if len(pe) < len(be) {
			return false
		}
		for i := range be {
			if be[i] != "*" && be[i] != pe[i] {
				return false
			}
		}
		return true
	}
	return pkg == pat
}

func show(pkg string) string {
	if pkg == "" {
		return "qui"
	}
	return pkg
}
