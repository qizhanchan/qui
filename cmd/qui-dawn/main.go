// Command qui-dawn installs the Dawn (WebGPU) shared library qui loads at
// run time — see docs/rfc/0003-webgpu.zh.md.
//
//	go run github.com/qizhanchan/qui/cmd/qui-dawn            # install for this machine
//	go run github.com/qizhanchan/qui/cmd/qui-dawn -path      # print where qui looks for it
//	go run github.com/qizhanchan/qui/cmd/qui-dawn -o DIR     # also copy it into DIR (app bundles)
//
// It downloads the release of the pinned Dawn revision from
// github.com/google/dawn (checking the archive's SHA-256 against the value
// pinned below), extracts the monolithic static library and links it into
// a shared library exporting only the wgpu* entry points, installed in the
// user cache directory under the revision.
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/qizhanchan/qui/internal/wgpu"
)

// asset is one prebuilt archive of the pinned release.
type asset struct {
	name   string // the part after "Dawn-<rev>-"
	sha256 string
}

// defaultMinMacOS is the oldest macOS the installed library loads on
// (Dawn itself supports macOS 11+).
const defaultMinMacOS = "11.0"

// assets maps GOOS/GOARCH to the release archive built for it.
var assets = map[string]asset{
	"darwin/arm64": {"macos-latest-Release.tar.gz", "29d072e6559d36cf1419cae60c9cddafdae0e4119e9028cf2495eeeb73898e0f"},
	"darwin/amd64": {"macos-15-intel-Release.tar.gz", "be43caf4cfbb02595ec018b2b4b3b05c119376d8ec0ce7e539ebb093b03d62bc"},
	"linux/amd64":  {"ubuntu-latest-Release.tar.gz", "8469022ea89105c33639b1342c81c9adf593ac0b2436f28348edd169e891e5f6"},
}

func main() {
	log.SetFlags(0)
	printPath := flag.Bool("path", false, "print the library search path and exit")
	out := flag.String("o", "", "also copy the installed library into this directory")
	force := flag.Bool("f", false, "reinstall even if present")
	flag.Parse()

	dst := filepath.Join(wgpu.CacheDir(), wgpu.LibraryName())
	if *printPath {
		for _, p := range wgpu.LibraryCandidates() {
			fmt.Println(p)
		}
		return
	}
	if _, err := os.Stat(dst); err != nil || *force {
		if err := install(dst); err != nil {
			log.Fatalf("qui-dawn: %v", err)
		}
	} else {
		log.Printf("qui-dawn: %s already installed", dst)
	}
	if *out != "" {
		if err := copyFile(dst, filepath.Join(*out, wgpu.LibraryName())); err != nil {
			log.Fatalf("qui-dawn: %v", err)
		}
		log.Printf("qui-dawn: copied to %s", *out)
	}
}

func install(dst string) error {
	key := runtime.GOOS + "/" + runtime.GOARCH
	a, ok := assets[key]
	if !ok {
		return fmt.Errorf("no prebuilt Dawn for %s yet; build Dawn %s from source and set QUI_DAWN_LIB", key, wgpu.DawnRevision)
	}
	name := "Dawn-" + wgpu.DawnRevision + "-" + a.name
	url := "https://github.com/google/dawn/releases/download/" + wgpu.DawnRelease + "/" + name
	tmp, err := os.MkdirTemp("", "qui-dawn-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	log.Printf("qui-dawn: downloading %s", url)
	archive := filepath.Join(tmp, "dawn.tar.gz")
	if err := download(url, archive, a.sha256); err != nil {
		return err
	}
	lib := filepath.Join(tmp, "libwebgpu_dawn.a")
	// lib/ in the macOS archives, lib64/ in the Linux one.
	if err := extract(archive, "/libwebgpu_dawn.a", lib); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	built := filepath.Join(tmp, wgpu.LibraryName())
	if err := link(lib, built); err != nil {
		return err
	}
	if err := copyFile(built, dst); err != nil {
		return err
	}
	log.Printf("qui-dawn: installed %s", dst)
	return nil
}

func download(url, path, want string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: %s", resp.Status)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), resp.Body); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("checksum mismatch: got %s, want %s", got, want)
	}
	return nil
}

// extract copies the archive member whose name ends in suffix to dst.
func extract(archive, suffix, dst string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("%s not in archive", suffix)
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg || !strings.HasSuffix(h.Name, suffix) {
			continue
		}
		out, err := os.Create(dst)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	}
}

// link turns the static library into a shared one exporting wgpu* only.
func link(static, out string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		syms := filepath.Join(filepath.Dir(out), "exports.txt")
		if err := os.WriteFile(syms, []byte("_wgpu*\n"), 0o644); err != nil {
			return err
		}
		// Without a deployment target the library would require the macOS it
		// was linked on; MACOSX_DEPLOYMENT_TARGET (an app's minimum) wins.
		minOS := os.Getenv("MACOSX_DEPLOYMENT_TARGET")
		if minOS == "" {
			minOS = defaultMinMacOS
		}
		cmd = exec.Command("clang", "-dynamiclib", "-o", out, "-mmacosx-version-min="+minOS,
			"-Wl,-all_load", static, "-lc++",
			"-framework", "Foundation", "-framework", "CoreFoundation", "-framework", "IOSurface",
			"-framework", "QuartzCore", "-framework", "Cocoa", "-framework", "IOKit", "-framework", "Metal",
			"-install_name", "@rpath/"+wgpu.LibraryName(),
			"-Wl,-dead_strip", "-Wl,-exported_symbols_list,"+syms)
	case "linux":
		cmd = exec.Command("c++", "-shared", "-o", out,
			"-Wl,--whole-archive", static, "-Wl,--no-whole-archive",
			"-ldl", "-lpthread", "-lX11", "-Wl,--gc-sections")
	default:
		return errors.New("no link recipe for " + runtime.GOOS)
	}
	var logBuf strings.Builder
	cmd.Stdout, cmd.Stderr = &logBuf, &logBuf
	err := cmd.Run()
	// Dawn's release archives are compiled for the macOS of the CI runner;
	// the linker says so once per object. Report it once.
	newer := 0
	for _, l := range strings.Split(logBuf.String(), "\n") {
		if strings.Contains(l, "was built for newer") {
			newer++
			continue
		}
		if l != "" {
			fmt.Fprintln(os.Stderr, l)
		}
	}
	if newer > 0 {
		log.Printf("qui-dawn: note: %d Dawn objects were compiled for a newer macOS than %s; "+
			"older systems are not guaranteed to work with the prebuilt release", newer, os.Getenv("MACOSX_DEPLOYMENT_TARGET")+orDefault())
	}
	return err
}

func orDefault() string {
	if os.Getenv("MACOSX_DEPLOYMENT_TARGET") != "" || runtime.GOOS != "darwin" {
		return ""
	}
	return defaultMinMacOS
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
