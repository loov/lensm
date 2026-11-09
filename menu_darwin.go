package main

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework AppKit

#include <stdlib.h>

void lensmMenuAdd(const char *title);
void lensmMenuItem(const char *label, const char *key, int alt, int tag, const char *selector);
void lensmMenuSeparator(void);
void lensmMenuInstall(void);
void lensmMenuSetState(int tag, int checked, int enabled);
*/
import "C"

import "unsafe"

// nativeMenu says the menu bar has the app menu, so the window
// doesn't draw one.
const nativeMenu = true

// menuActions receives the items chosen in the menu bar.
var menuActions = make(chan int, 16)

//export lensmMenuAction
func lensmMenuAction(tag C.int) {
	// The main thread calls this, so it must not wait for the UI.
	select {
	case menuActions <- int(tag):
	default:
	}
}

// installMenu replaces the menu bar Gio sets up, which has only Hide and
// Quit.
func installMenu(menus []nativeMenuDef) {
	cstr := func(s string) *C.char { return C.CString(s) }
	free := func(p *C.char) { C.free(unsafe.Pointer(p)) }
	for _, m := range menus {
		title := cstr(m.Title)
		C.lensmMenuAdd(title)
		free(title)
		for _, it := range m.Items {
			if it.Label == "" {
				C.lensmMenuSeparator()
				continue
			}
			label, key, sel := cstr(it.Label), cstr(it.Key), cstr(it.Selector)
			alt := C.int(0)
			if it.Alt {
				alt = 1
			}
			C.lensmMenuItem(label, key, alt, C.int(it.Tag), sel)
			free(label)
			free(key)
			free(sel)
		}
	}
	C.lensmMenuInstall()
}

func setMenuState(tag int, checked, enabled bool) {
	b := func(v bool) C.int {
		if v {
			return 1
		}
		return 0
	}
	C.lensmMenuSetState(C.int(tag), b(checked), b(enabled))
}
