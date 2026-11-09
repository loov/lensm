package main

import (
	"flag"
	"fmt"
	"image"
	"log"
	"os"
	"runtime/pprof"
	"strconv"

	"gioui.org/app"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"loov.dev/lensm/internal/gui"
	"loov.dev/lensm/internal/mcp"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		os.Exit(mcp.RunCommand(os.Args[2:]))
	}

	cpuprofile := flag.String("cpuprofile", "", "enable cpu profiling")
	defaults := DefaultAppSettings()
	textSize := flag.Int("text-size", defaults.TextSize, "default font size")
	filter := flag.String("filter", "", "filter the functions by regexp")
	watch := flag.Bool("watch", false, "auto reload executable, or rebuild a package on change")
	context := flag.Int("context", 3, "source line context")
	comments := flag.String("comments", "", "comments sidecar path")
	font := flag.String("font", "", "user font")
	profilePath := flag.String("profile", "", "pprof profile of the binary to show")

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
		fmt.Fprintln(os.Stderr, "lensm [exePath | packageDir]")
		flag.Usage()
		os.Exit(2)
	}

	windows := &gui.Windows{}

	theme := material.NewTheme()
	theme.Shaper = text.NewShaper(text.WithCollection(gui.LoadFonts(*font)))
	theme.TextSize = unit.Sp(*textSize)

	ui := NewFileUI(windows, theme)
	if explicitTextSize {
		ui.SettingsDialog.TextSize.SetText(strconv.Itoa(*textSize))
	} else {
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
		ProfilePath:  *profilePath,
	}
	ui.Funcs.SetFilter(*filter)

	// With its own title bar the window is not decorated, which on
	// macOS puts the content under a transparent title bar.
	windows.Open("lensm", image.Pt(1400, 900), ui.Run, app.Decorated(!customTitleBar))

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
