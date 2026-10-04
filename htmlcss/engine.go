package htmlcss

import (
	"os"
	"strings"

	"github.com/qizhanchan/qui"
)

// RenderResult carries the built element tree plus lookup indexes so
// callers and tests can address HTML elements without a live *Window.
// ByID maps an element's `id` to its widget (first in document order on
// duplicates); ByClass maps a class name to every widget carrying it.
// Every element is an *El — including inline pieces that fold into a
// text run visually (they remain addressable, restyleable objects).
type RenderResult struct {
	Root    qui.Widget
	ByID    map[string]qui.Widget
	ByClass map[string][]qui.Widget
	// Engine is the style engine the document was compiled with. The result
	// stays live, so the engine's runtime hooks apply — notably SetCSS to
	// swap the stylesheet (theme switching) and restyle in place.
	Engine *StyleEngine
}

// Render parses HTML + CSS and returns a qui widget tree for the
// document body, ready to hand to Window.SetRoot. External CSS passed in
// cssSrc is combined with any <style> blocks found in the markup (author
// order: <style> blocks after cssSrc, matching document order roughly).
//
// The document compiles into a live *El tree styled once through a
// StyleEngine — the SAME widget assembly the reactive path uses — so
// the result is not frozen: elements found via RenderDoc's indexes can
// be mutated (SetClass / SetText / …) and restyle in place.
func Render(htmlSrc, cssSrc string, opts Options) qui.Widget {
	return RenderDoc(htmlSrc, cssSrc, opts).Root
}

// RenderDoc is Render plus the id / class lookup indexes (see
// RenderResult). Use it when you need to locate specific HTML elements'
// widgets — e.g. to drive them in tests or wire extra behavior.
func RenderDoc(htmlSrc, cssSrc string, opts Options) RenderResult {
	dom := ParseHTML(htmlSrc)
	css := cssSrc
	if embedded := collectStyleText(dom); embedded != "" {
		css = css + "\n" + embedded
	}
	vw := float32(defaultViewportWidth)
	if opts.ViewportWidth > 0 {
		vw = opts.ViewportWidth
	} else if opts.Window != nil {
		vw = opts.Window.Size().W
	}
	eng := NewStyleEngineViewport(css, vw)
	eng.opts = opts

	body := findTag(dom, "body")
	if body == nil {
		body = dom
	}
	styles := resolveStyles(dom, eng.sheet)
	root := eng.buildStaticEl(body, styles)
	if root == nil {
		// Empty document → an empty block element so the window has a root.
		root = eng.NewEl("body")
	}
	eng.SetRoot(root)
	eng.Restyle()

	byID := map[string]qui.Widget{}
	byClass := map[string][]qui.Widget{}
	collectStaticIndexes(root, byID, byClass)
	return RenderResult{Root: root, ByID: byID, ByClass: byClass, Engine: eng}
}

// RenderFiles reads an HTML file and an optional CSS file from disk and
// renders them. BaseDir defaults to the HTML file's directory (so
// relative <img src> and any file references resolve) unless the caller
// set opts.BaseDir.
func RenderFiles(htmlPath, cssPath string, opts Options) (qui.Widget, error) {
	htmlBytes, err := os.ReadFile(htmlPath)
	if err != nil {
		return nil, err
	}
	css := ""
	if cssPath != "" {
		cssBytes, err := os.ReadFile(cssPath)
		if err != nil {
			return nil, err
		}
		css = string(cssBytes)
	}
	if opts.BaseDir == "" {
		opts.BaseDir = dirOf(htmlPath)
	}
	return Render(string(htmlBytes), css, opts), nil
}

// collectStyleText concatenates the text content of every <style>
// element in the document.
func collectStyleText(root *Node) string {
	var sb strings.Builder
	var walk func(*Node)
	walk = func(n *Node) {
		if n.isElement("style") {
			sb.WriteString(textContent(n))
			sb.WriteByte('\n')
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return sb.String()
}

func dirOf(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[:i]
	}
	return "."
}
