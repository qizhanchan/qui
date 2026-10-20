// Command chartcompare renders ECharts options twice — once with the native
// qui chart renderer and once with ECharts in an offscreen Chrome canvas —
// then compares the PNGs with ImageMagick compare. It accepts one -case or
// runs every JSON fixture in -dir with -all.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/graphs/chart"
)

type parityCase struct {
	Width  int             `json:"width"`
	Height int             `json:"height"`
	Option json.RawMessage `json:"option"`
}

type result struct {
	Case          string  `json:"case"`
	Width         int     `json:"width"`
	Height        int     `json:"height"`
	Native        string  `json:"native"`
	Browser       string  `json:"browser"`
	Diff          string  `json:"diff"`
	RMSE          float64 `json:"rmse"`
	RMSEAbsolute  string  `json:"rmseAbsolute"`
	MaxRMSE       float64 `json:"maxRmse"`
	Pass          bool    `json:"pass"`
	CompareStatus int     `json:"compareStatus"`
	Error         string  `json:"error,omitempty"`
}

type compareConfig struct {
	echartsPath string
	chromePath  string
	comparePath string
	maxRMSE     float64
}

func main() {
	var (
		casePath    = flag.String("case", "", "parity case JSON")
		allCases    = flag.Bool("all", false, "run every JSON fixture in -dir")
		caseDir     = flag.String("dir", "graphs/chart/testdata/parity", "directory scanned by -all")
		outDir      = flag.String("out", "", "output directory (default: temp)")
		echartsPath = flag.String("echarts", "/opt/js_work/echarts/dist/echarts.min.js", "ECharts browser bundle")
		chromePath  = flag.String("chrome", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "Chrome executable")
		comparePath = flag.String("compare", "/opt/homebrew/bin/compare", "ImageMagick compare executable")
		fontRegular = flag.String("font-regular", "/System/Library/Fonts/Supplemental/Arial.ttf", "regular font loaded as family Arial")
		fontBold    = flag.String("font-bold", "/System/Library/Fonts/Supplemental/Arial Bold.ttf", "bold font loaded as family Arial")
		maxRMSE     = flag.Float64("max-rmse", 0.04, "fail when normalized ImageMagick RMSE exceeds this value; <=0 disables")
		keepTemp    = flag.Bool("keep", false, "keep generated HTML when -out is not set")
	)
	flag.Parse()
	if (*casePath == "") == !*allCases {
		fmt.Fprintln(os.Stderr, "chartcompare: specify exactly one of -case or -all")
		os.Exit(2)
	}
	cases := []string{*casePath}
	if *allCases {
		var err error
		cases, err = parityCases(*caseDir)
		if err != nil {
			fatalf("scan cases: %v", err)
		}
	}
	if err := loadParityFonts(*fontRegular, *fontBold); err != nil {
		fatalf("load fonts: %v", err)
	}
	rootDir := *outDir
	if rootDir == "" {
		var err error
		rootDir, err = os.MkdirTemp("", "qui-chart-parity-*")
		if err != nil {
			fatalf("create temp dir: %v", err)
		}
		if !*keepTemp {
			defer os.RemoveAll(rootDir)
		}
	} else if err := os.MkdirAll(rootDir, 0o755); err != nil {
		fatalf("create output dir: %v", err)
	}

	cfg := compareConfig{
		echartsPath: *echartsPath,
		chromePath:  *chromePath,
		comparePath: *comparePath,
		maxRMSE:     *maxRMSE,
	}
	results := make([]result, 0, len(cases))
	failed := false
	for _, path := range cases {
		dir := rootDir
		if *allCases {
			dir = filepath.Join(rootDir, strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fatalf("create case output dir: %v", err)
		}
		res, err := runCase(path, dir, cfg)
		if err != nil {
			res = result{Case: path, MaxRMSE: cfg.maxRMSE, Error: err.Error()}
		}
		results = append(results, res)
		if !res.Pass {
			failed = true
		}
	}

	var encoded []byte
	if *allCases {
		encoded, _ = json.MarshalIndent(results, "", "  ")
	} else {
		encoded, _ = json.MarshalIndent(results[0], "", "  ")
	}
	fmt.Println(string(encoded))
	if failed {
		os.Exit(1)
	}
}

func parityCases(dir string) ([]string, error) {
	cases, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(cases)
	if len(cases) == 0 {
		return nil, fmt.Errorf("no JSON fixtures in %s", dir)
	}
	return cases, nil
}

func runCase(casePath, dir string, cfg compareConfig) (result, error) {
	c, err := loadCase(casePath)
	if err != nil {
		return result{}, fmt.Errorf("load case: %w", err)
	}
	nativePath := filepath.Join(dir, "native.png")
	browserPath := filepath.Join(dir, "browser.png")
	diffPath := filepath.Join(dir, "diff.png")
	if err := renderNative(c, nativePath); err != nil {
		return result{}, fmt.Errorf("render native: %w", err)
	}
	htmlPath := filepath.Join(dir, "browser.html")
	if err := writeBrowserHTML(c, cfg.echartsPath, htmlPath); err != nil {
		return result{}, fmt.Errorf("write browser HTML: %w", err)
	}
	if err := renderBrowser(cfg.chromePath, htmlPath, browserPath, c.Width, c.Height, dir); err != nil {
		return result{}, fmt.Errorf("render browser: %w", err)
	}

	metric, status, err := compareImages(cfg.comparePath, browserPath, nativePath, diffPath)
	if err != nil {
		return result{}, fmt.Errorf("compare: %w", err)
	}
	rmse, absolute, err := parseRMSE(metric)
	if err != nil {
		return result{}, fmt.Errorf("parse compare metric %q: %w", metric, err)
	}
	return result{
		Case:          casePath,
		Width:         c.Width,
		Height:        c.Height,
		Native:        nativePath,
		Browser:       browserPath,
		Diff:          diffPath,
		RMSE:          rmse,
		RMSEAbsolute:  absolute,
		MaxRMSE:       cfg.maxRMSE,
		Pass:          cfg.maxRMSE <= 0 || rmse <= cfg.maxRMSE,
		CompareStatus: status,
	}, nil
}

func loadParityFonts(regular, bold string) error {
	if regular != "" {
		if err := qui.LoadFontVariantFromFile("Arial", qui.FontWeightNormal, false, regular); err != nil {
			return fmt.Errorf("regular: %w", err)
		}
	}
	if bold != "" {
		if err := qui.LoadFontVariantFromFile("Arial", qui.FontWeightBold, false, bold); err != nil {
			return fmt.Errorf("bold: %w", err)
		}
	}
	return nil
}

func loadCase(path string) (parityCase, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return parityCase{}, err
	}
	var c parityCase
	if err := json.Unmarshal(data, &c); err != nil {
		return parityCase{}, err
	}
	if c.Width <= 0 || c.Height <= 0 {
		return parityCase{}, errors.New("width and height must be positive")
	}
	if len(c.Option) == 0 {
		return parityCase{}, errors.New("option is required")
	}
	var nativeOption chart.Option
	if err := json.Unmarshal(c.Option, &nativeOption); err != nil {
		return parityCase{}, fmt.Errorf("native option: %w", err)
	}
	return c, nil
}

func renderNative(c parityCase, path string) error {
	img := image.NewRGBA(image.Rect(0, 0, c.Width, c.Height))
	canvas := qui.NewImageCanvas(img)
	canvas.Clear(qui.Color{R: 1, G: 1, B: 1, A: 1})
	var opt chart.Option
	if err := json.Unmarshal(c.Option, &opt); err != nil {
		return err
	}
	// Native screenshots are deterministic final states, matching the
	// browser side where writeBrowserHTML forces animation off.
	animation := false
	opt.Animation = &animation
	w := chart.New(opt)
	w.Layout(qui.Rect{X: 0, Y: 0, W: float32(c.Width), H: float32(c.Height)})
	w.Draw(canvas)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func writeBrowserHTML(c parityCase, echartsPath, path string) error {
	var option any
	if err := json.Unmarshal(c.Option, &option); err != nil {
		return err
	}
	// Animation is forced off so screenshots are deterministic. Other raw
	// ECharts-only fields pass through untouched.
	if object, ok := option.(map[string]any); ok {
		object["animation"] = false
	}
	optionJSON, err := json.Marshal(option)
	if err != nil {
		return err
	}
	scriptURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(echartsPath)}).String()
	var html bytes.Buffer
	fmt.Fprintf(&html, `<!doctype html>
<html><head><meta charset="utf-8">
<style>html,body{margin:0;width:%dpx;height:%dpx;overflow:hidden;background:#fff}#chart{width:%dpx;height:%dpx}</style>
</head><body><div id="chart"></div>
<script src="%s"></script>
<script>
const option = %s;
const chart = echarts.init(document.getElementById('chart'), null, {
  renderer: 'canvas', width: %d, height: %d, devicePixelRatio: 1
});
chart.setOption(option, {notMerge: true, lazyUpdate: false});
</script></body></html>
`, c.Width, c.Height, c.Width, c.Height, scriptURL, optionJSON, c.Width, c.Height)
	return os.WriteFile(path, html.Bytes(), 0o644)
}

func renderBrowser(chrome, htmlPath, screenshotPath string, width, height int, tempBase string) error {
	profile, err := os.MkdirTemp(tempBase, "chrome-profile-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(profile)
	htmlURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(htmlPath)}).String()
	args := []string{
		"--headless=new",
		"--disable-gpu",
		"--hide-scrollbars",
		"--no-first-run",
		"--no-default-browser-check",
		"--force-device-scale-factor=1",
		"--user-data-dir=" + profile,
		"--window-size=" + strconv.Itoa(width) + "," + strconv.Itoa(height),
		"--virtual-time-budget=1000",
		"--screenshot=" + screenshotPath,
		htmlURL,
	}
	cmd := exec.Command(chrome, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	info, err := os.Stat(screenshotPath)
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return errors.New("Chrome wrote an empty screenshot")
	}
	return nil
}

func compareImages(compare, expected, actual, diff string) (string, int, error) {
	cmd := exec.Command(compare, "-metric", "RMSE", expected, actual, diff)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	status := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return "", -1, err
		}
		status = exitErr.ExitCode()
		// ImageMagick compare returns 1 when the images differ. That is
		// the normal measurement path; 2+ indicates an execution failure.
		if status > 1 {
			return "", status, fmt.Errorf("status %d: %s", status, strings.TrimSpace(output.String()))
		}
	}
	return strings.TrimSpace(output.String()), status, nil
}

func parseRMSE(output string) (float64, string, error) {
	fields := strings.Fields(output)
	if len(fields) == 0 {
		return 0, "", errors.New("empty output")
	}
	absolute := fields[0]
	normalized := ""
	for _, field := range fields[1:] {
		field = strings.Trim(field, "()")
		if _, err := strconv.ParseFloat(field, 64); err == nil {
			normalized = field
			break
		}
	}
	if normalized == "" {
		return 0, absolute, errors.New("normalized metric not found")
	}
	value, err := strconv.ParseFloat(normalized, 64)
	return value, absolute, err
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "chartcompare: "+format+"\n", args...)
	os.Exit(1)
}
