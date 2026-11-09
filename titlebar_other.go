//go:build !darwin

package main

import (
	"sync/atomic"

	"gioui.org/io/event"
)

// Elsewhere the system draws the title bar.
const customTitleBar = false

var overTitleDrag atomic.Bool

type titleBar struct{}

func (*titleBar) event(event.Event) {}
