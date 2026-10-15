package widgets

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// drawPurityAllowed lists the Draw / Measure / Layout methods allowed to
// write a style, each with why. Keyed "Type.Method".
var drawPurityAllowed = map[string]string{
	// Restyles its own private content / scroll children from the menu
	// palette; an app never holds those widgets.
	"menuListView.Draw": "private children",
	// TODO: remove once the theme-provenance work stops FieldSet.Draw
	// overwriting the legend's Font / Foreground every frame.
	"FieldSet.Draw": "pending fix",
}

// Draw, Measure and Layout must not assign a widget's Style() fields or
// States: whatever the app set after construction would be overwritten on
// the next frame (the Dialog AddButton bug, 2d1b04b), and a write there
// never invalidates layout or paint. Derive a local copy instead.
func TestDrawDoesNotWriteStyles(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || fd.Body == nil {
				continue
			}
			switch fd.Name.Name {
			case "Draw", "Measure", "Layout":
			default:
				continue
			}
			key := receiverName(fd) + "." + fd.Name.Name
			if _, ok := drawPurityAllowed[key]; ok {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				as, ok := n.(*ast.AssignStmt)
				if !ok || as.Tok != token.ASSIGN {
					return true
				}
				for _, lhs := range as.Lhs {
					if field := styleField(lhs); field != "" {
						t.Errorf("%s: %s writes %s", fset.Position(as.Pos()), key, field)
					}
				}
				return true
			})
		}
	}
}

// styleField reports the style written by an assignment target of the
// form x.Style().F or x.States, or "".
func styleField(e ast.Expr) string {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	if sel.Sel.Name == "States" {
		return "States"
	}
	if call, ok := sel.X.(*ast.CallExpr); ok {
		if fn, ok := call.Fun.(*ast.SelectorExpr); ok && fn.Sel.Name == "Style" {
			return "Style()." + sel.Sel.Name
		}
	}
	return ""
}

func receiverName(fd *ast.FuncDecl) string {
	t := fd.Recv.List[0].Type
	if s, ok := t.(*ast.StarExpr); ok {
		t = s.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name
	}
	return "?"
}
