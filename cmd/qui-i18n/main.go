// Command qui-i18n manages a qui application's message catalogs.
//
//	qui-i18n extract [-dir .] [-out locales/en.json]
//	qui-i18n lint    [-dir .] [-locales locales]
//	qui-i18n pseudo  [-locales locales] [-source en] [-out locales/en-XA.json]
//
// extract scans Go source for message keys and writes/updates the source
// catalog. lint reports gaps between catalogs and code. pseudo generates
// a pseudo-locale that makes untranslated strings and layout overflow
// visible without any translator involvement.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "extract":
		err = runExtract(os.Args[2:])
	case "lint":
		err = runLint(os.Args[2:])
	case "pseudo":
		err = runPseudo(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "qui-i18n: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "qui-i18n: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `qui-i18n — message catalog tooling for qui apps

Usage:
  qui-i18n extract [flags]   scan Go source for keys, write the source catalog
  qui-i18n lint    [flags]   report missing / unused keys and placeholder drift
  qui-i18n pseudo  [flags]   generate a pseudo-locale for layout testing

extract flags:
  -dir string     package tree to scan (default ".")
  -out string     catalog to write (default "locales/en.json")
  -prune          drop keys no longer referenced in code (default false)

lint flags:
  -dir string     package tree to scan (default ".")
  -locales string catalog directory (default "locales")
  -strict         exit non-zero on any finding (default true)

pseudo flags:
  -locales string catalog directory (default "locales")
  -source string  locale to derive from (default "en")
  -out string     output file (default "<locales>/en-XA.json")
  -expand float   length multiplier (default 1.4)

Keys are found at these call sites:
  i18n.T("k") / TN / TNf / TOr        qui.T / qui.Translate / qui.TranslateOr
  widget.SetTextKey("k") and the SetLabelKey / SetPlaceholderKey family
  h.T("k") / h.TN / h.TPlaceholder / h.TTitle / h.TValue
  data-i18n="k" and data-i18n-placeholder / -title / -value in Go string
  literals (inline HTML) and in .html files under -dir
`)
}

// ---------------------------------------------------------------- extract

func runExtract(argv []string) error {
	fs := flag.NewFlagSet("extract", flag.ExitOnError)
	dir := fs.String("dir", ".", "package tree to scan")
	out := fs.String("out", filepath.Join("locales", "en.json"), "catalog to write")
	prune := fs.Bool("prune", false, "drop keys no longer referenced in code")
	if err := fs.Parse(argv); err != nil {
		return err
	}

	refs, err := scanTree(*dir)
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		fmt.Fprintln(os.Stderr, "no message keys found — is -dir pointing at the right tree?")
	}

	existing, err := readCatalogFile(*out)
	if err != nil {
		return err
	}

	merged, added, removed := mergeCatalog(existing, refs, *prune)
	if err := writeCatalogFile(*out, merged); err != nil {
		return err
	}

	fmt.Printf("%s: %d keys (%d added", *out, len(merged), len(added))
	if *prune {
		fmt.Printf(", %d removed", len(removed))
	} else if len(removed) > 0 {
		fmt.Printf(", %d unreferenced kept — pass -prune to drop", len(removed))
	}
	fmt.Println(")")
	for _, k := range added {
		fmt.Printf("  + %s\n", k)
	}
	if *prune {
		for _, k := range removed {
			fmt.Printf("  - %s\n", k)
		}
	}
	return nil
}

// mergeCatalog keeps every existing translation, adds a stub for each
// newly-referenced key, and reports which keys are no longer referenced.
//
// Never overwriting an existing value is the whole point: extract runs
// against a catalog people have translated, and a tool that clobbered
// their work would be run exactly once.
func mergeCatalog(existing map[string]catalogEntry, refs map[string]keyRef, prune bool) (out map[string]catalogEntry, added, removed []string) {
	out = make(map[string]catalogEntry, len(refs))
	for k, v := range existing {
		if strings.HasPrefix(k, "@") {
			out[k] = v
			continue
		}
		if _, still := refs[k]; !still && prune {
			removed = append(removed, k)
			continue
		}
		if _, still := refs[k]; !still {
			removed = append(removed, k)
		}
		out[k] = v
	}
	for k, ref := range refs {
		if _, have := out[k]; have {
			continue
		}
		added = append(added, k)
		// A key referenced with a count needs plural forms; seed both
		// so the translator sees the shape immediately.
		if ref.plural {
			out[k] = catalogEntry{forms: map[string]string{"one": k, "other": k}}
		} else {
			out[k] = catalogEntry{text: k}
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return out, added, removed
}

// ------------------------------------------------------------------ lint

func runLint(argv []string) error {
	fs := flag.NewFlagSet("lint", flag.ExitOnError)
	dir := fs.String("dir", ".", "package tree to scan")
	locales := fs.String("locales", "locales", "catalog directory")
	strict := fs.Bool("strict", true, "exit non-zero on any finding")
	if err := fs.Parse(argv); err != nil {
		return err
	}

	refs, err := scanTree(*dir)
	if err != nil {
		return err
	}
	cats, err := readCatalogDir(*locales)
	if err != nil {
		return err
	}
	if len(cats) == 0 {
		return fmt.Errorf("no catalogs found in %s", *locales)
	}

	findings := lintAll(refs, cats)
	for _, f := range findings {
		fmt.Printf("%s: %s\n", f.kind, f.detail)
	}
	if len(findings) == 0 {
		fmt.Printf("ok — %d keys across %d locales\n", len(refs), len(cats))
		return nil
	}
	fmt.Printf("\n%d finding(s)\n", len(findings))
	if *strict {
		os.Exit(1)
	}
	return nil
}

// ---------------------------------------------------------------- pseudo

func runPseudo(argv []string) error {
	fs := flag.NewFlagSet("pseudo", flag.ExitOnError)
	locales := fs.String("locales", "locales", "catalog directory")
	source := fs.String("source", "en", "locale to derive from")
	out := fs.String("out", "", "output file (default <locales>/en-XA.json)")
	expand := fs.Float64("expand", 1.4, "length multiplier")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *out == "" {
		*out = filepath.Join(*locales, "en-XA.json")
	}

	cats, err := readCatalogDir(*locales)
	if err != nil {
		return err
	}
	src, ok := cats[*source]
	if !ok {
		return fmt.Errorf("no catalog for source locale %q in %s", *source, *locales)
	}

	pseudo := make(map[string]catalogEntry, len(src))
	for k, e := range src {
		if strings.HasPrefix(k, "@") {
			continue
		}
		pseudo[k] = pseudoEntry(e, *expand)
	}
	pseudo["@meta"] = catalogEntry{raw: `{"locale":"en-XA","generated":"qui-i18n pseudo","note":"Do not translate. Regenerate after changing the source catalog."}`}

	if err := writeCatalogFile(*out, pseudo); err != nil {
		return err
	}
	fmt.Printf("%s: %d pseudo-localized keys from %s\n", *out, len(pseudo)-1, *source)
	fmt.Println("run the app with QUI_I18N_STRICT=1 and the locale set to en-XA")
	return nil
}
