package av1

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestHeaderTrace (opt-in: AV1_TRACE=a.ivf,b.ivf or a directory) parses
// every OBU header of the given streams and compares each syntax element
// (bit position and value) with FFmpeg's trace_headers bitstream filter.
func TestHeaderTrace(t *testing.T) {
	spec := os.Getenv("AV1_TRACE")
	if spec == "" {
		t.Skip("set AV1_TRACE to IVF files or a directory")
	}
	var files []string
	for _, p := range strings.Split(spec, ",") {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			m, _ := filepath.Glob(filepath.Join(p, "*.ivf"))
			files = append(files, m...)
		} else {
			files = append(files, p)
		}
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) { compareTrace(t, f) })
	}
}

type traceEl struct {
	pos, val int
	name     string
}

var traceLine = regexp.MustCompile(`\] (\d+)\s+(\S+)\s+[01]+ = (-?\d+)$`)

// ffmpegTrace returns the syntax elements of each OBU after the first
// packet, without the OBU header fields and trailing bits.
func ffmpegTrace(t *testing.T, file string) [][]traceEl {
	out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "debug", "-i", file, "-c", "copy", "-bsf:v", "trace_headers", "-f", "null", "-").CombinedOutput()
	if err != nil {
		t.Skipf("ffmpeg: %v", err)
	}
	var obus [][]traceEl
	started := false
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		l := sc.Text()
		if !strings.Contains(l, "[trace_headers") {
			continue
		}
		if strings.Contains(l, "] Packet:") {
			started = true
			continue
		}
		if !started {
			continue
		}
		if strings.HasSuffix(l, "] OBU header") {
			obus = append(obus, nil)
			continue
		}
		m := traceLine.FindStringSubmatch(l)
		if m == nil || len(obus) == 0 {
			continue
		}
		name := m[2]
		if strings.HasPrefix(name, "obu_") || strings.HasPrefix(name, "trailing_") || name == "zero_bit" || strings.HasPrefix(name, "temporal_id") || strings.HasPrefix(name, "spatial_id") || strings.HasPrefix(name, "extension_header") {
			continue
		}
		pos, _ := strconv.Atoi(m[1])
		val, _ := strconv.Atoi(m[3])
		obus[len(obus)-1] = append(obus[len(obus)-1], traceEl{pos, val, name})
	}
	return obus
}

// Elements whose FFmpeg value is a derived one rather than the coded bits.
var traceDerived = map[string]bool{"tx_mode": true, "lr_unit_shift": true}

func compareTrace(t *testing.T, file string) {
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	tus, _, _, err := ReadIVF(data)
	if err != nil {
		t.Fatal(err)
	}
	want := ffmpegTrace(t, file)
	d := New()
	var got [][]traceEl
	d.trace = func(pos, val int) {
		got[len(got)-1] = append(got[len(got)-1], traceEl{pos: pos, val: val})
	}
	for i, tu := range tus {
		obus, err := splitOBUs(tu)
		if err != nil {
			t.Fatalf("TU %d: %v", i, err)
		}
		for j := range obus {
			got = append(got, nil)
			if err := d.decodeOBU(&obus[j]); err != nil {
				t.Fatalf("TU %d OBU %d: %v", i, j, err)
			}
		}
	}
	if len(got) != len(want) {
		t.Errorf("%d OBUs, FFmpeg traced %d", len(got), len(want))
	}
	bad := 0
	for i := 0; i < min(len(got), len(want)) && bad < 10; i++ {
		g, w := got[i], want[i]
		gi := 0
		for _, e := range w {
			for gi < len(g) && g[gi].pos < e.pos {
				gi++
			}
			if gi >= len(g) || g[gi].pos != e.pos {
				t.Errorf("OBU %d: %s at bit %d not read", i, e.name, e.pos)
				bad++
				break
			}
			if g[gi].val != e.val && !traceDerived[e.name] {
				t.Errorf("OBU %d: %s at bit %d = %d, FFmpeg %d", i, e.name, e.pos, g[gi].val, e.val)
				bad++
			}
		}
		if len(w) > 0 && len(g) > 0 && g[len(g)-1].pos > w[len(w)-1].pos {
			t.Errorf("OBU %d: read past FFmpeg's last element (bit %d > %d)", i, g[len(g)-1].pos, w[len(w)-1].pos)
			bad++
		}
	}
}
