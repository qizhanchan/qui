package qui

import (
	"fmt"
	"log"
	"os"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"time"
)

// QUI_DEBUG_IDLE=1 logs, once a second, what kept the main loop awake:
// how many loop iterations and Window.Steps ran, how many frames were
// painted (and how many device pixels they covered), how many jobs ran,
// and which condition in nextFrameWait asked for display cadence. A page
// that should be idle but costs CPU shows up here as a steady stream of
// steps with one dominant reason.
var debugIdle = os.Getenv("QUI_DEBUG_IDLE") == "1"

type idleDebugStats struct {
	start     time.Time
	loops     int
	steps     int
	painted   int
	paintedPx float64
	jobs      int
	tickDirty int
	layouts   int
	reasons   map[string]int
	tickers   map[string]int
	jobFuncs  map[string]int
	gaps      []time.Duration // between painted frames

	sleepTotal time.Duration
}

var idleDbg = idleDebugStats{reasons: map[string]int{}, tickers: map[string]int{}, jobFuncs: map[string]int{}}

var lastPaintedFrame time.Time

// idleDebugFrame records the interval since the previous painted frame
// (gaps over 250 ms are idle time, not frames).
func idleDebugFrame(now time.Time) {
	if !lastPaintedFrame.IsZero() {
		if g := now.Sub(lastPaintedFrame); g < 250*time.Millisecond {
			idleDbg.gaps = append(idleDbg.gaps, g)
		}
	}
	lastPaintedFrame = now
}

// idleDebugJob records where a posted job's closure was written.
func idleDebugJob(fn func()) {
	name := "?"
	if f := runtime.FuncForPC(reflect.ValueOf(fn).Pointer()); f != nil {
		name = f.Name()
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
	}
	idleDbg.jobFuncs[name]++
}

func idleDebugReason(r string) {
	if debugIdle {
		idleDbg.reasons[r]++
	}
}

// idleDebugTicker records the concrete type of a Tickable whose Tick
// reported a dirty rect.
func idleDebugTicker(t any) {
	if debugIdle {
		idleDbg.tickers[fmt.Sprintf("%T", t)]++
	}
}

func idleDebugLoop(sleep time.Duration) {
	if !debugIdle {
		return
	}
	idleDbg.loops++
	if sleep < time.Hour {
		idleDbg.sleepTotal += sleep
	}
	now := time.Now()
	if idleDbg.start.IsZero() {
		idleDbg.start = now
		return
	}
	el := now.Sub(idleDbg.start)
	if el < time.Second {
		return
	}
	s := &idleDbg
	gaps := ""
	if len(s.gaps) > 0 {
		sort.Slice(s.gaps, func(i, j int) bool { return s.gaps[i] < s.gaps[j] })
		pct := func(p float64) time.Duration {
			return s.gaps[int(p*float64(len(s.gaps)-1))].Round(100 * time.Microsecond)
		}
		gaps = fmt.Sprintf(" frame gap p50=%v p95=%v max=%v", pct(0.5), pct(0.95), pct(1))
	}
	jobs := ""
	if s.jobs > 100 {
		jobs = " jobfn=" + fmtCounts(s.jobFuncs)
	}
	if n := idleCompFrames.Swap(0); n > 0 {
		gaps += fmt.Sprintf(" compositor-thread frames=%d", n)
	}
	log.Printf("qui/idle %.1fs: loops=%d steps=%d painted=%d (%.2f Mpx)%s jobs=%d tickDirty=%d layouts=%d wait=%s tickers=%s%s",
		el.Seconds(), s.loops, s.steps, s.painted, s.paintedPx/1e6, gaps, s.jobs, s.tickDirty, s.layouts,
		fmtCounts(s.reasons), fmtCounts(s.tickers), jobs)
	*s = idleDebugStats{start: now, reasons: map[string]int{}, tickers: map[string]int{}, jobFuncs: map[string]int{}}
}

func fmtCounts(m map[string]int) string {
	if len(m) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return m[keys[i]] > m[keys[j]] })
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%s:%d", k, m[k])
	}
	return b.String()
}

// noteSlowStep logs a Step long enough to miss a 60 Hz frame, split into
// its job drain and the rest (ticks, layout, paint, present).
func noteSlowStep(start, jobsDone time.Time, jobs int) {
	total := time.Since(start)
	if total < 12*time.Millisecond {
		return
	}
	r := func(d time.Duration) time.Duration { return d.Round(100 * time.Microsecond) }
	p := stepPhase
	log.Printf("qui/idle slow step %v: jobs %d took %v, rest %v (tick %v, style+layout %v, paint %v, present %v)",
		r(total), jobs, r(jobsDone.Sub(start)), r(time.Since(jobsDone)), r(p.tick), r(p.styleLayout), r(p.paint), r(p.present))
	stepPhase = stepPhases{}
}

// stepPhases splits the Step being timed (QUI_DEBUG_IDLE).
type stepPhases struct {
	start                             time.Time
	tick, styleLayout, paint, present time.Duration
}

var stepPhase stepPhases
