package main

import (
	"context"
	"flag"
	"log"
	"runtime"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

func main() {
	pprofAddr := flag.String("pprof", "", "enable pprof server on addr (e.g. 127.0.0.1:6060)")
	autoCJK := flag.Bool("auto-cjk-font", true, "auto probe/load system CJK font on first text draw")
	memstats := flag.Bool("memstats", false, "log startup runtime memstats")
	flag.Parse()
	qui.SetAutoLoadSystemCJK(*autoCJK)

	if *memstats {
		logMemStats("startup")
		defer logMemStats("exit")
		go func() {
			time.Sleep(2 * time.Second)
			logMemStats("post-startup-2s")
		}()
	}

	var pprofSrv *qui.PprofServer
	if *pprofAddr != "" {
		var err error
		pprofSrv, err = qui.StartPprofServer(*pprofAddr)
		if err != nil {
			log.Fatal(err)
		}
		defer func() {
			_ = pprofSrv.Stop(context.Background())
		}()
		log.Printf("pprof listening at http://%s/debug/pprof/", pprofSrv.Addr())
	}

	app, err := qui.NewApp()
	if err != nil {
		log.Fatal(err)
	}

	window, err := app.NewWindow("Input Demo", 300, 400)
	if err != nil {
		log.Fatal(err)
	}

	window.SetRenderer(qui.NewGLRenderer())

	title := widgets.NewLabel("Input Component Demo")
	title.Style().Foreground = qui.ColorWhite
	title.Style().Font.Size = 24

	nameField := widgets.NewInput("Enter your name...")
	nameField.OnChange = func(text string) {
		log.Printf("name changed: %q", text)
	}
	nameField.OnSubmit = func(text string) {
		log.Printf("name submitted: %q", text)
	}

	cjkField := widgets.NewInput("输入中文试试...")
	cjkField.OnChange = func(text string) {
		log.Printf("cjk changed: %q", text)
	}
	cjkField.OnSubmit = func(text string) {
		log.Printf("cjk submitted: %q", text)
	}

	instructions := widgets.NewLabel("Click to focus, type to edit\nEnter triggers OnSubmit\nIME: try typing Chinese in the second field")
	instructions.Style().Foreground = qui.Color{R: 0.7, G: 0.7, B: 0.7, A: 1}
	instructions.Style().Font.Size = 14

	getBtn := widgets.NewButton("Print Values", func() {
		log.Printf("name=%q cjk=%q", nameField.GetText(), cjkField.GetText())
	})
	setBtn := widgets.NewButton("Set Sample", func() {
		nameField.SetText("Hello World")
		cjkField.SetText("你好，世界")
	})
	clearBtn := widgets.NewButton("Clear", func() {
		nameField.SetText("")
		cjkField.SetText("")
	})

	buttons := qui.NewContainer(qui.FlexLayout{Direction: qui.Horizontal, Gap: 8}, getBtn, setBtn, clearBtn)

	root := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Vertical, Gap: 16},
		title,
		nameField,
		cjkField,
		instructions,
		buttons,
	)
	root.Style().Background = qui.Color{R: 0.12, G: 0.12, B: 0.12, A: 1}
	root.Style().Padding = qui.Insets{Top: 20, Right: 20, Bottom: 20, Left: 20}

	window.SetRoot(root)
	if *memstats {
		logMemStats("before-run")
	}

	app.Run()
}

func logMemStats(stage string) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	const mb = 1024 * 1024
	log.Printf(
		"memstats stage=%s alloc=%dMB heapAlloc=%dMB heapInuse=%dMB heapSys=%dMB heapIdle=%dMB heapReleased=%dMB stackInuse=%dMB numGC=%d",
		stage,
		m.Alloc/mb,
		m.HeapAlloc/mb,
		m.HeapInuse/mb,
		m.HeapSys/mb,
		m.HeapIdle/mb,
		m.HeapReleased/mb,
		m.StackInuse/mb,
		m.NumGC,
	)
}
