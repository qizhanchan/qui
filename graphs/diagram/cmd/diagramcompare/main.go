// Command diagramcompare renders each draw.io fixture twice: through the
// native qui/graphs/diagram renderer and through draw.io's local
// viewer-static bundle in headless Chrome. It then measures normalized
// RMSE with ImageMagick compare.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/graphs/diagram"
)

type parityCase struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Fit    *bool  `json:"fit,omitempty"`
	Drawio string `json:"drawio"`
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
	Changed       float64 `json:"changed"`
	ChangedPixels string  `json:"changedPixels"`
	MaxChanged    float64 `json:"maxChanged"`
	Pass          bool    `json:"pass"`
	CompareStatus int     `json:"compareStatus"`
	Error         string  `json:"error,omitempty"`
}

type compareConfig struct {
	viewerPath  string
	chromePath  string
	comparePath string
	fontRegular string
	fontBold    string
	maxRMSE     float64
	maxChanged  float64
	fuzz        float64
}

func main() {
	var (
		casePath    = flag.String("case", "", "parity fixture JSON")
		allCases    = flag.Bool("all", false, "run every JSON fixture in -dir")
		caseDir     = flag.String("dir", "graphs/diagram/testdata/parity", "directory scanned by -all")
		outDir      = flag.String("out", "", "output directory (default: temp)")
		viewerPath  = flag.String("viewer", "/opt/js_work/drawio/src/main/webapp/js/viewer-static.min.js", "draw.io viewer-static.min.js")
		chromePath  = flag.String("chrome", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "Chrome executable")
		comparePath = flag.String("compare", "/opt/homebrew/bin/compare", "ImageMagick compare executable")
		fontRegular = flag.String("font-regular", "/System/Library/Fonts/Supplemental/Arial.ttf", "regular font loaded as family Arial")
		fontBold    = flag.String("font-bold", "/System/Library/Fonts/Supplemental/Arial Bold.ttf", "bold font loaded as family Arial")
		maxRMSE     = flag.Float64("max-rmse", 0, "optional gate on normalized ImageMagick RMSE; <=0 disables")
		maxChanged  = flag.Float64("max-changed", .04, "fail when AE changed-pixel fraction exceeds this value; <=0 disables")
		fuzz        = flag.Float64("fuzz", 1, "AE fuzz percentage used to ignore rasterizer anti-alias noise")
		keepTemp    = flag.Bool("keep", false, "keep generated HTML when -out is not set")
	)
	flag.Parse()
	if (*casePath == "") == !*allCases {
		fmt.Fprintln(os.Stderr, "diagramcompare: specify exactly one of -case or -all")
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
	if err := loadFonts(*fontRegular, *fontBold); err != nil {
		fatalf("load fonts: %v", err)
	}
	rootDir := *outDir
	if rootDir == "" {
		var err error
		rootDir, err = os.MkdirTemp("", "qui-diagram-parity-*")
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
		viewerPath: *viewerPath, chromePath: *chromePath, comparePath: *comparePath,
		fontRegular: *fontRegular, fontBold: *fontBold, maxRMSE: *maxRMSE,
		maxChanged: *maxChanged, fuzz: *fuzz,
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
			res = result{Case: path, MaxRMSE: cfg.maxRMSE, MaxChanged: cfg.maxChanged, Error: err.Error()}
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
	if err := writeBrowserHTML(c, cfg.viewerPath, htmlPath); err != nil {
		return result{}, fmt.Errorf("write browser HTML: %w", err)
	}
	if err := renderBrowser(cfg.chromePath, htmlPath, browserPath, c.Width, c.Height, dir); err != nil {
		return result{}, fmt.Errorf("render browser: %w", err)
	}
	metric, status, err := compareImages(cfg.comparePath, "-metric", "RMSE", nativePath, browserPath, diffPath)
	if err != nil {
		return result{}, fmt.Errorf("compare RMSE: %w", err)
	}
	rmse, absolute, err := parseMetric(metric)
	if err != nil {
		return result{}, fmt.Errorf("parse RMSE metric %q: %w", metric, err)
	}
	aeMetric, aeStatus, err := compareImages(cfg.comparePath, "-metric", "AE", "-fuzz", strconv.FormatFloat(cfg.fuzz, 'f', -1, 64)+"%", nativePath, browserPath, "null:")
	if err != nil {
		return result{}, fmt.Errorf("compare AE: %w", err)
	}
	changed, changedPixels, err := parseMetric(aeMetric)
	if err != nil {
		return result{}, fmt.Errorf("parse AE metric %q: %w", aeMetric, err)
	}
	if err := writeMontage(filepath.Join(dir, "compare.png"), nativePath, browserPath, diffPath); err != nil {
		return result{}, fmt.Errorf("montage: %w", err)
	}
	passRMSE := cfg.maxRMSE <= 0 || rmse <= cfg.maxRMSE
	passChanged := cfg.maxChanged <= 0 || changed <= cfg.maxChanged
	return result{
		Case: filepath.Base(casePath), Width: c.Width, Height: c.Height,
		Native: nativePath, Browser: browserPath, Diff: diffPath,
		RMSE: rmse, RMSEAbsolute: absolute, MaxRMSE: cfg.maxRMSE,
		Changed: changed, ChangedPixels: changedPixels, MaxChanged: cfg.maxChanged,
		Pass: passRMSE && passChanged, CompareStatus: status | aeStatus,
	}, nil
}

func loadFonts(regular, bold string) error {
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
	if strings.TrimSpace(c.Drawio) == "" {
		return parityCase{}, errors.New("drawio XML is required")
	}
	if _, err := diagram.DecodeDrawio([]byte(c.Drawio)); err != nil {
		return parityCase{}, fmt.Errorf("decode drawio: %w", err)
	}
	return c, nil
}

func renderNative(c parityCase, path string) error {
	doc, err := diagram.DecodeDrawio([]byte(c.Drawio))
	if err != nil {
		return err
	}
	fit := c.Fit == nil || *c.Fit
	return diagram.WritePNGFile(path, doc, diagram.RenderOptions{Width: c.Width, Height: c.Height, Fit: fit})
}

func writeBrowserHTML(c parityCase, viewerPath, path string) error {
	fit := c.Fit == nil || *c.Fit
	config := map[string]any{
		"xml": c.Drawio, "nav": false, "resize": false, "toolbar": "",
		"auto-fit": fit, "allow-zoom-in": true, "border": 0,
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return err
	}
	scriptURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(viewerPath)}).String()
	var b bytes.Buffer
	fmt.Fprintf(&b, `<!doctype html>
<html><head><meta charset="utf-8">
<style>html,body{margin:0;padding:0;width:%dpx;height:%dpx;overflow:hidden;background:#fff}#host{width:%dpx;height:%dpx}.mxgraph{margin-top:0!important}</style>
</head><body><div id="host"><div class="mxgraph" style="width:%dpx;height:%dpx;" data-mxgraph="%s"></div></div>
<script src="%s"></script><script>GraphViewer.processElements();</script></body></html>`,
		c.Width, c.Height, c.Width, c.Height, c.Width, c.Height, html.EscapeString(string(encoded)), scriptURL)
	return os.WriteFile(path, b.Bytes(), 0o644)
}

func renderBrowser(chrome, htmlPath, screenshotPath string, width, height int, tempBase string) error {
	profile, err := os.MkdirTemp(tempBase, "chrome-profile-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(profile)
	htmlURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(htmlPath)}).String()
	args := []string{
		"--headless=new", "--disable-gpu", "--hide-scrollbars", "--no-first-run",
		"--no-default-browser-check", "--force-device-scale-factor=1",
		"--allow-file-access-from-files", "--run-all-compositor-stages-before-draw",
		"--user-data-dir=" + profile, "--window-size=" + strconv.Itoa(width) + "," + strconv.Itoa(height),
		"--virtual-time-budget=3000", "--screenshot=" + screenshotPath, htmlURL,
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

func compareImages(compare string, args ...string) (string, int, error) {
	cmd := exec.Command(compare, args...)
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
		if status > 1 {
			return "", status, fmt.Errorf("status %d: %s", status, strings.TrimSpace(output.String()))
		}
	}
	return strings.TrimSpace(output.String()), status, nil
}

func parseMetric(output string) (float64, string, error) {
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

func writeMontage(path, native, browser, diff string) error {
	tmp := path + ".tmp.png"
	cmd := exec.Command("magick", "montage", native, browser, diff,
		"-tile", "3x1", "-geometry", "+8+8", "-background", "#e5e7eb", tmp)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(output.String()))
	}
	return os.Rename(tmp, path)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "diagramcompare: "+format+"\n", args...)
	os.Exit(1)
}
