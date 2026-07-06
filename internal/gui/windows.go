package gui

import (
	"fmt"
	"image"
	"log"
	"os"
	"sync"

	"gioui.org/app"
	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/font/opentype"
	"gioui.org/unit"
)

type Windows struct {
	active sync.WaitGroup
}

func (windows *Windows) Open(title string, sizeDp image.Point, run func(*app.Window) error) {
	windows.active.Go(func() {

		window := new(app.Window)
		window.Option(
			app.Title(title),
			app.Size(unit.Dp(sizeDp.X), unit.Dp(sizeDp.Y)),
		)
		if err := run(window); err != nil {
			log.Println(err)
		}
	})
}

func (windows *Windows) Wait() {
	windows.active.Wait()
}

func LoadFonts(userfont string) []font.FontFace {
	collection := gofont.Collection()
	if userfont == "" {
		return collection
	}
	b, err := os.ReadFile(userfont)
	if err != nil {
		panic(fmt.Errorf("failed to parse font: %v", err))
	}
	face, err := opentype.Parse(b)
	if err != nil {
		panic(fmt.Errorf("failed to parse font: %v", err))
	}
	fnt := font.Font{Typeface: "override-monospace,monospace", Weight: font.Normal}
	fface := font.FontFace{Font: fnt, Face: face}
	return append(collection, fface)
}
