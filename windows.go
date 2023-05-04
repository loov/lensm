package main

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
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

type Windows struct {
	active sync.WaitGroup
}

func (windows *Windows) Open(title string, sizeDp image.Point, run func(*app.Window) error) {
	windows.active.Add(1)
	go func() {
		defer windows.active.Done()

		window := app.NewWindow(
			app.Title(title),
			app.Size(unit.Dp(sizeDp.X), unit.Dp(sizeDp.Y)),
		)
		if err := run(window); err != nil {
			log.Println(err)
		}
	}()
}

func (windows *Windows) Wait() {
	windows.active.Wait()
}

func WidgetWindow(widget layout.Widget) func(*app.Window) error {
	return func(w *app.Window) error {
		var ops op.Ops
		for {
			select {
			case e := <-w.Events():
				switch e := e.(type) {
				case system.FrameEvent:
					gtx := layout.NewContext(&ops, e)
					widget(gtx)
					e.Frame(gtx.Ops)

				case system.DestroyEvent:
					return e.Err
				}
			}
		}
	}
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
	fnt := font.Font{Variant: "Mono", Weight: font.Normal}
	fface := font.FontFace{Font: fnt, Face: face}
	return append(collection, fface)
}
