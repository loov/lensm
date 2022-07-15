package main

import (
	"fmt"
	"image"
	"io/ioutil"
	"log"
	"sync"

	"gioui.org/app"
	"gioui.org/font/gofont"
	"gioui.org/font/opentype"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
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

func LoadFonts(userfont string) []text.FontFace {
	collection := gofont.Collection()
	if userfont == "" {
		return collection
	}
	b, err := ioutil.ReadFile(userfont)
	if err != nil {
		panic(fmt.Errorf("failed to parse font: %v", err))
	}
	face, err := opentype.Parse(b)
	if err != nil {
		panic(fmt.Errorf("failed to parse font: %v", err))
	}
	fnt := text.Font{Variant: "Mono", Weight: text.Normal}
	fface := text.FontFace{Font: fnt, Face: face}
	return append(collection, fface)
}
