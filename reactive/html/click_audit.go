package html

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/qizhanchan/qui/htmlcss"
	"github.com/qizhanchan/qui/reactive"
)

// debugClicks turns on the nested-click audit (QUI_DEBUG_CLICKS=1, or a
// file path to append the report to): each builder that installs a click
// handler records its call site, and after every commit the mounted trees
// are checked with htmlcss.AuditClickNesting — each place where one click
// would run two elements' handlers (Chrome bubbling) is reported once.
var debugClicks = os.Getenv("QUI_DEBUG_CLICKS") != ""

// clickAuditFn receives each nested pair when set through AuditClicks.
var clickAuditFn func(htmlcss.ClickNesting)

// AuditClicks turns the nested-click audit on for this process and hands
// each place where one click would run two elements' handlers to fn —
// once per pair — after the render that mounted it. Clicks bubble as in
// Chrome, so such a pair runs both handlers; when only the inner one
// should run, give it StopClickPropagation. An app's UI tests can call
// it to fail on any nesting nobody decided about. AuditClicks(nil) turns
// it off again (QUI_DEBUG_CLICKS keeps reporting to stderr / its file).
func AuditClicks(fn func(htmlcss.ClickNesting)) {
	clickAuditFn = fn
	debugClicks = fn != nil || os.Getenv("QUI_DEBUG_CLICKS") != ""
	auditSeen = map[string]bool{}
}

var auditSeen = map[string]bool{}

// noteClickSite records the first caller outside this package.
func (b *Builder) noteClickSite() {
	if !debugClicks || b.clickSite != "" {
		return
	}
	for skip := 2; skip < 12; skip++ {
		_, file, line, ok := runtime.Caller(skip)
		if !ok {
			return
		}
		if strings.Contains(filepath.ToSlash(file), "/qui/reactive/html/") && !strings.HasSuffix(file, "_test.go") {
			continue
		}
		b.clickSite = fmt.Sprintf("%s:%d", shortPath(file), line)
		return
	}
}

// shortPath keeps the last three path elements: enough to find the file.
func shortPath(file string) string {
	parts := strings.Split(filepath.ToSlash(file), "/")
	if len(parts) > 3 {
		parts = parts[len(parts)-3:]
	}
	return strings.Join(parts, "/")
}

// auditPending are the elements updated in the current pass; their trees
// are audited once it commits.
var auditPending []*htmlcss.El

func auditClicksAfterCommit(el *htmlcss.El) {
	auditPending = append(auditPending, el)
	if len(auditPending) > 1 {
		return
	}
	run := func() {
		roots := map[*htmlcss.El]bool{}
		for _, e := range auditPending {
			roots[e.TopEl()] = true
		}
		auditPending = nil
		for r := range roots {
			if os.Getenv("QUI_DEBUG_CLICKS") != "" {
				htmlcss.ReportClickNesting(r)
			}
			if fn := clickAuditFn; fn != nil {
				for _, n := range htmlcss.AuditClickNesting(r) {
					if s := n.String(); !auditSeen[s] {
						auditSeen[s] = true
						fn(n)
					}
				}
			}
		}
	}
	if !reactive.AfterCommit(run) {
		run()
	}
}
