package main

import (
	"flag"
	"fmt"
	"image"
	"log"
	"os"
	"runtime/pprof"

	"gioui.org/app"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		os.Exit(runMCPCommand(os.Args[2:]))
	}

	cpuprofile := flag.String("cpuprofile", "", "enable cpu profiling")
	defaults := DefaultAppSettings()
	textSize := flag.Int("text-size", defaults.TextSize, "default font size")
	filter := flag.String("filter", "", "filter the functions by regexp")
	watch := flag.Bool("watch", false, "auto reload executable")
	context := flag.Int("context", 3, "source line context")
	comments := flag.String("comments", "", "comments sidecar path")
	font := flag.String("font", "", "user font")

	workInProgressWASM = os.Getenv("LENSM_EXPERIMENT_WASM") != ""

	flag.Parse()
	exePath := flag.Arg(0)
	explicitTextSize := false
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "text-size":
			explicitTextSize = true
		}
	})

	if flag.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "lensm [exePath]")
		flag.Usage()
		os.Exit(2)
	}

	windows := &Windows{}

	theme := material.NewTheme()
	theme.Shaper = text.NewShaper(text.WithCollection(LoadFonts(*font)))
	theme.TextSize = unit.Sp(*textSize)

	ui := NewExeUI(windows, theme)
	if !explicitTextSize {
		theme.TextSize = unit.Sp(ui.Settings.TextSize)
	}
	if exePath == "" {
		exePath = ui.Settings.LastPath
	}
	ui.Config = FileUIConfig{
		Path:         exePath,
		Watch:        *watch,
		Context:      *context,
		CommentsPath: *comments,
	}
	ui.Funcs.SetFilter(*filter)

	windows.Open("lensm", image.Pt(1400, 900), ui.Run)

	go func() {
		profile(*cpuprofile, windows.Wait)
		os.Exit(0)
	}()

	// This starts Gio main.
	app.Main()
}

func profile(cpuprofile string, fn func()) {
	if cpuprofile != "" {
		f, err := os.Create(cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}
	fn()
}
