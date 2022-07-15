package main

import (
	"gioui.org/widget"
	"golang.org/x/exp/shiny/materialdesign/icons"
)

// OpenInNewIcon is used for opening Code in a new window.
var OpenInNewIcon = func() *widget.Icon {
	icon, _ := widget.NewIcon(icons.ActionOpenInNew)
	return icon
}()
