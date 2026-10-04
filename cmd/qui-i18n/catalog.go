package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// catalogEntry mirrors i18n.Message but keeps unknown shapes verbatim so
// a round-trip through this tool never destroys hand-authored content it
// does not understand. That matters: the tool edits files translators own.
type catalogEntry struct {
	text  string
	forms map[string]string
	// raw is set for entries the tool passes through untouched (@meta).
	raw string
}

func (e catalogEntry) isPlural() bool { return len(e.forms) > 0 }

// value returns the representative string for placeholder checks.
func (e catalogEntry) value() string {
	if e.isPlural() {
		return e.forms["other"]
	}
	return e.text
}

var pluralOrder = []string{"zero", "one", "two", "few", "many", "other"}

func (e catalogEntry) marshal() ([]byte, error) {
	if e.raw != "" {
		return []byte(e.raw), nil
	}
	if !e.isPlural() {
		return json.Marshal(e.text)
	}
	var b bytes.Buffer
	b.WriteByte('{')
	first := true
	for _, cat := range pluralOrder {
		v, ok := e.forms[cat]
		if !ok {
			continue
		}
		if !first {
			b.WriteString(", ")
		}
		first = false
		k, _ := json.Marshal(cat)
		val, _ := json.Marshal(v)
		b.Write(k)
		b.WriteString(": ")
		b.Write(val)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// readCatalogFile loads one catalog. A missing file is not an error —
// extract's whole job on a fresh project is to create it.
func readCatalogFile(path string) (map[string]catalogEntry, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]catalogEntry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return parseCatalogBytes(path, data)
}

func parseCatalogBytes(path string, data []byte) (map[string]catalogEntry, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	out := make(map[string]catalogEntry, len(raw))
	for k, v := range raw {
		if strings.HasPrefix(k, "@") {
			out[k] = catalogEntry{raw: string(v)}
			continue
		}
		trimmed := bytes.TrimSpace(v)
		if len(trimmed) > 0 && trimmed[0] == '"' {
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				return nil, fmt.Errorf("%s key %q: %w", path, k, err)
			}
			out[k] = catalogEntry{text: s}
			continue
		}
		var forms map[string]string
		if err := json.Unmarshal(v, &forms); err != nil {
			return nil, fmt.Errorf("%s key %q: expected a string or plural object: %w", path, k, err)
		}
		out[k] = catalogEntry{forms: forms}
	}
	return out, nil
}

// readCatalogDir loads every *.json in dir, keyed by locale. Follows the
// same "qui.<locale>.json" naming rule as the runtime loader.
func readCatalogDir(dir string) (map[string]map[string]catalogEntry, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return map[string]map[string]catalogEntry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	out := map[string]map[string]catalogEntry{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		cat, err := readCatalogFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		loc := localeFromFilename(e.Name())
		if existing, ok := out[loc]; ok {
			for k, v := range cat {
				existing[k] = v
			}
			continue
		}
		out[loc] = cat
	}
	return out, nil
}

func localeFromFilename(name string) string {
	base := strings.TrimSuffix(name, ".json")
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[i+1:]
	}
	return base
}

// writeCatalogFile writes a catalog as stable, human-diffable JSON:
// two-space indent, keys sorted, @meta first, one entry per line.
//
// Hand-rolled rather than json.MarshalIndent because Go's encoder sorts
// map keys but cannot keep plural forms in CLDR order or float @meta to
// the top — and these files land in code review, where a stable diff is
// the difference between a reviewable change and a wall of churn.
func writeCatalogFile(path string, cat map[string]catalogEntry) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}

	keys := make([]string, 0, len(cat))
	var metaKeys []string
	for k := range cat {
		if strings.HasPrefix(k, "@") {
			metaKeys = append(metaKeys, k)
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(metaKeys)
	sort.Strings(keys)

	var b bytes.Buffer
	b.WriteString("{\n")
	ordered := append(metaKeys, keys...)
	for i, k := range ordered {
		val, err := cat[k].marshal()
		if err != nil {
			return fmt.Errorf("encode %q: %w", k, err)
		}
		kb, _ := json.Marshal(k)
		b.WriteString("  ")
		b.Write(kb)
		b.WriteString(": ")
		b.Write(val)
		if i < len(ordered)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString("}\n")

	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
