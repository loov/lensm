package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"os"

	"gioui.org/app"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

func main() {
	textSize := flag.Int("text-size", 12, "default font size")
	filter := flag.String("filter", "", "filter the functions by regexp")
	watch := flag.Bool("watch", false, "auto reload executable")
	context := flag.Int("context", 3, "source line context")
	font := flag.String("font", "", "user font")

	flag.Parse()
	exePath := flag.Arg(0)

	if exePath == "" {
		fmt.Fprintln(os.Stderr, "lensm <exePath>")
		flag.Usage()
		os.Exit(1)
	}

	windows := &Windows{}

	theme := material.NewTheme(LoadFonts(*font))
	theme.TextSize = unit.Sp(*textSize)

	ui := NewExeUI(windows, theme)
	ui.Config = FileUIConfig{
		Path:    exePath,
		Watch:   *watch,
		Context: *context,
	}
	ui.Funcs.SetFilter(*filter)

	windows.Open("lensm", image.Pt(1400, 900), ui.Run)

	go func() {
		windows.Wait()
		os.Exit(0)
	}()

	// This starts Gio main.
	app.Main()
}

var (
	secondaryBackground = color.NRGBA{R: 0xF0, G: 0xF0, B: 0xF0, A: 0xFF}
	splitterColor       = color.NRGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xFF}
)
