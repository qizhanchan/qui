package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

func TestElConsumesTabAttr(t *testing.T) {
	e := newEl("div", NewStyleEngine(""))
	var w qui.Widget = e
	tc, ok := w.(qui.TabConsumer)
	if !ok {
		t.Fatal("El should implement qui.TabConsumer")
	}
	if tc.ConsumesTab(false) {
		t.Fatal("no data-keep-tab: Tab navigates")
	}
	e.SetAttr("data-keep-tab", "")
	if !tc.ConsumesTab(false) || !tc.ConsumesTab(true) {
		t.Fatal("data-keep-tab should keep Tab")
	}
}
