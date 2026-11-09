//go:build !darwin

package main

import (
	"sync/atomic"

	"gioui.org/io/event"
)

// Elsewhere the system draws the title bar.
const customTitleBar = false

var overTitleDrag atomic.Bool

// windowClosed never receives: Gio's DestroyEvent reports the close.
var windowClosed chan struct{}

type titleBar struct{}

func (*titleBar) event(event.Event) {}
