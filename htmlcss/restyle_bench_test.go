package htmlcss

import (
	"fmt"
	"testing"
)

// Restyle benchmarks (scoped flushes via FlushPendingStyles; Restyle is
// the full pass) on a list the size of go-code's file tree (~2,700
// elements). Each reports restyled/op — the elements styled per
// operation — next to time, so a scope regression shows up as a count
// jump even when the machine is fast enough to hide it in ns/op.

const benchRestyleCSS = `.row { color: #ff0000; padding: 2px } .row span { color: #00ff00 }
.row.open { background: #eeeeee } .row:hover span { color: #0000ff }`

const benchRows = 1350 // × (.row > span) ≈ 2,700 elements

func reportRestyled(b *testing.B, eng *StyleEngine, before int) {
	b.ReportMetric(float64(eng.styled-before)/float64(b.N), "restyled/op")
}

// Appending rows styles only the new ones.
func BenchmarkRestyleAppendRows(b *testing.B) {
	eng, list, rows := listFixture(b, benchRestyleCSS, benchRows)
	b.ReportAllocs()
	b.ResetTimer()
	before := eng.styled
	for i := 0; i < b.N; i++ {
		extra := []*El{newRow(eng, "row"), newRow(eng, "row")}
		list.SetElementChildren(asWidgets(append(append([]*El{}, rows...), extra...)))
		eng.FlushPendingStyles()
		b.StopTimer()
		list.SetElementChildren(asWidgets(rows))
		eng.FlushPendingStyles()
		b.StartTimer()
	}
	reportRestyled(b, eng, before)
}

// Toggling one row's class restyles that row's subtree, not the list.
func BenchmarkRestyleToggleClass(b *testing.B) {
	eng, _, rows := listFixture(b, benchRestyleCSS, benchRows)
	row := rows[benchRows/2]
	b.ReportAllocs()
	b.ResetTimer()
	before := eng.styled
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			row.SetClass("row open")
		} else {
			row.SetClass("row")
		}
		eng.FlushPendingStyles()
	}
	reportRestyled(b, eng, before)
}

// A full restyle of the list: the baseline the scoped paths are measured
// against.
func BenchmarkRestyleFull(b *testing.B) {
	for _, n := range []int{100, benchRows} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			eng, list, _ := listFixture(b, benchRestyleCSS, n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				eng.SetRoot(list)
				eng.Restyle()
			}
		})
	}
}
