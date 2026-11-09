package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/gui"
)

// The render flags draw the window offscreen into a PNG, for checking a
// UI change without opening the app, e.g.
//
//	go test -run TestRender . -render /tmp/out.png -render.path ./testdata/testprog -render.func main.dot
var (
	renderOut      = flag.String("render", "", "render the window to this PNG file")
	renderPath     = flag.String("render.path", "", "binary or package directory to open")
	renderFunc     = flag.String("render.func", "", "function to open in a tab")
	renderFilter   = flag.String("render.filter", "", "function list filter")
	renderDark     = flag.Bool("render.dark", false, "use the dark theme")
	renderPanel    = flag.String("render.panel", "functions", "sidebar panel: functions, comments, history or diagnostics")
	renderCols     = flag.String("render.cols", "gnsd", "columns to show: g for Go asm, n for native asm, s for source, d for diagnostics")
	renderSyntax   = flag.String("render.syntax", "xcode", "syntax style")
	renderSize     = flag.String("render.size", "1400x900", "window size in dp")
	renderScale    = flag.Float64("render.scale", 2, "pixels per dp")
	renderHover    = flag.String("render.hover", "", "pointer position in dp, as x,y")
	renderFind     = flag.String("render.find", "", "open the find bar with this query")
	renderQuick    = flag.String("render.quick", "", "open the go-to-function popup with this query")
	renderMenu     = flag.Bool("render.menu", false, "open the app menu, drawn in the window except on macOS")
	renderSettings = flag.Int("render.settings", -1, "open the settings on this page")
)

// TestRender is skipped unless -render names an output file. It starts
// from the default settings, so the user's settings don't change it.
func TestRender(t *testing.T) {
	if *renderOut == "" {
		t.Skip("set -render to draw the window")
	}
	var w, h int
	if _, err := fmt.Sscanf(*renderSize, "%dx%d", &w, &h); err != nil {
		t.Fatalf("-render.size %q: %v", *renderSize, err)
	}
	scale := float32(*renderScale)
	size := image.Pt(int(float32(w)*scale), int(float32(h)*scale))

	theme := material.NewTheme()
	theme.Shaper = text.NewShaper(text.WithCollection(gui.LoadFonts("")))
	theme.TextSize = unit.Sp(12)
	ui := NewFileUI(&gui.Windows{}, theme)
	ui.settingsPath = "" // never write the real settings

	st := DefaultAppSettings()
	st.Dark = *renderDark
	st.ShowGoAsm = strings.Contains(*renderCols, "g")
	st.ShowNativeAsm = strings.Contains(*renderCols, "n")
	st.ShowSource = strings.Contains(*renderCols, "s")
	st.HideDiagnostics = !strings.Contains(*renderCols, "d")
	st.SyntaxStyle = *renderSyntax
	ui.split.Ratio = st.SidebarRatio
	ui.applySettings(st)

	panels := map[string]int{"functions": panelFunctions, "comments": panelComments, "history": panelHistory, "diagnostics": panelMarks, "callers": panelCallers, "summary": panelSummary}
	panel, ok := panels[*renderPanel]
	if !ok {
		t.Fatalf("unknown -render.panel %q", *renderPanel)
	}
	ui.Panel = panel

	ui.Config = FileUIConfig{Path: *renderPath, Context: 3}
	if *renderPath != "" {
		file, err := loadFile(*renderPath)
		if err != nil {
			t.Fatal(err)
		}
		ui.SetFile(file)
		defer ui.File.Close()
	}
	ui.Funcs.SetFilter(*renderFilter)
	if *renderFunc != "" {
		if ui.File == nil {
			t.Fatal("-render.func needs -render.path")
		}
		fn := disasm.FindFunc(ui.File, *renderFunc)
		if fn == nil {
			t.Fatalf("no function %q", *renderFunc)
		}
		ui.openTab(fn, false)
	}
	if *renderSettings >= 0 {
		ui.openSettings()
		ui.SettingsNav.Selected = *renderSettings
	}
	ui.AppMenu.Open = *renderMenu

	var hover f32.Point
	if *renderHover != "" {
		if _, err := fmt.Sscanf(*renderHover, "%f,%f", &hover.X, &hover.Y); err != nil {
			t.Fatalf("-render.hover %q: %v", *renderHover, err)
		}
	}

	win, err := headless.NewWindow(size.X, size.Y)
	if err != nil {
		t.Fatal(err)
	}
	defer win.Release()
	var router input.Router
	var ops op.Ops
	// Code loads on its own goroutine, and hover and the popups need a
	// frame to settle, so draw a few frames and keep the last. The clock
	// steps a second a frame, past the tooltip delay.
	start := time.Now()
	for i := range 4 {
		ops.Reset()
		gtx := layout.Context{
			Ops:         &ops,
			Now:         start.Add(time.Duration(i) * time.Second),
			Metric:      unit.Metric{PxPerDp: scale, PxPerSp: scale},
			Constraints: layout.Exact(size),
			Source:      router.Source(),
		}
		if i == 0 && *renderFind != "" {
			ui.Find.Editor.SetText(*renderFind)
			ui.openFind(gtx)
		}
		if i == 0 && *renderQuick != "" {
			ui.QuickOpen.Editor.SetText(*renderQuick)
			ui.QuickOpen.Show(gtx)
		}
		ui.Layout(gtx)
		router.Frame(gtx.Ops)
		if i == 1 && *renderHover != "" {
			router.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: hover.Mul(scale)})
		}
		if err := win.Frame(gtx.Ops); err != nil {
			t.Fatal(err)
		}
	}

	img := image.NewRGBA(image.Rectangle{Max: size})
	if err := win.Screenshot(img); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(*renderOut)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
