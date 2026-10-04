package qui

import (
	"fmt"
	"log"
	"os"
	"reflect"
	"strings"
)

// flexDebugLayoutEnabled gates the optional FlexLayout overflow log.
// Read once at package init to keep the hot path branch cheap.
var flexDebugLayoutEnabled = os.Getenv("QUI_DEBUG_LAYOUT") == "1"

// logFlexOverflow prints the items, their basis, and a hint when a
// FlexLayout.Apply overflows the main axis. Called from layout.go
// when QUI_DEBUG_LAYOUT=1 and computed sizes exceed available by
// more than half a pixel. With CSS-aligned defaults (Grow→Basis=0,
// Shrink default 1) this almost exclusively fires when a child has
// NoShrink set or a min-size that can't be reduced further.
func logFlexOverflow(l FlexLayout, bounds Rect, items []flexItem, basisSum, totalGap, mainAvail float32) {
	dir := "Horizontal"
	if l.Direction == Vertical {
		dir = "Vertical"
	}
	var b strings.Builder
	for i, it := range items {
		eff := it.flex.effectiveShrink()
		extra := ""
		if it.flex.NoShrink {
			extra = " NoShrink"
		}
		fmt.Fprintf(&b, "    [%d] %s  basis=%.1f grow=%.1f shrink(eff)=%.1f%s\n",
			i, widgetTypeNameForDebug(it.w),
			it.basis, it.flex.Grow, eff, extra)
	}
	log.Printf("[qui-flex] %s overflow @ (x=%.0f y=%.0f w=%.0f h=%.0f): "+
		"need=%.1f avail=%.1f (basisSum=%.1f, gaps=%.1f)\n%s"+
		"  hint: a child probably has NoShrink:true or a min-size that "+
		"prevents fitting; consider SetFlex(1) on the filler child or relax min-sizes.",
		dir, bounds.X, bounds.Y, bounds.W, bounds.H,
		basisSum+totalGap, mainAvail, basisSum, totalGap, b.String())
}

// widgetTypeNameForDebug returns the concrete package.Type name for a
// widget, e.g. "widgets.ScrollView". Mirrors debug_layout.go's
// widgetTypeName but lives here to keep that file's package-private
// helpers out of the layout hot path's compile dependencies.
func widgetTypeNameForDebug(w Widget) string {
	if w == nil {
		return "<nil>"
	}
	t := reflect.TypeOf(w)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.PkgPath() == "" {
		return t.Name()
	}
	parts := strings.Split(t.PkgPath(), "/")
	return parts[len(parts)-1] + "." + t.Name()
}
