package main

/*
#include <stdint.h>

void lensmTitleBar(uintptr_t view, double height, double left);
*/
import "C"

import (
	"sync/atomic"

	"gioui.org/app"
	"gioui.org/io/event"
)

// customTitleBar says the toolbar is the window's title bar, with the
// window buttons at its left end.
const customTitleBar = true

// overTitleDrag is set while the pointer is over empty toolbar space,
// where a press drags the window and a double-click zooms it.
var overTitleDrag atomic.Bool

//export lensmOverTitleDrag
func lensmOverTitleDrag() C.int {
	if overTitleDrag.Load() {
		return 1
	}
	return 0
}

// titleBar places the window buttons in the toolbar once the view
// exists, and again after Gio configures the window, which hides them.
type titleBar struct{ view uintptr }

func (t *titleBar) event(e event.Event) {
	switch e := e.(type) {
	case app.AppKitViewEvent:
		t.view = e.View
	case app.ConfigEvent:
	default:
		return
	}
	if t.view != 0 {
		C.lensmTitleBar(C.uintptr_t(t.view), toolbarHeight, windowButtonsLeft)
	}
}
