package webview

import "github.com/qizhanchan/qui"

// Accessibility hooks. WebView is an opaque leaf in the qui AX tree —
// the embedded page's DOM is not surfaced here. Agents that need to
// drive the page should use EvaluateJS / RegisterHandler instead.

func (v *WebView) Role() string { return qui.RoleWebview }

func (v *WebView) AccessibleName() string {
	if v == nil {
		return ""
	}
	return v.Title()
}

func (v *WebView) AccessibleValue() string {
	if v == nil {
		return ""
	}
	return v.URL()
}
