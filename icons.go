package main

import (
	"gioui.org/widget"
	"golang.org/x/exp/shiny/materialdesign/icons"
)

var OpenInNewIcon = func() *widget.Icon {
	icon, _ := widget.NewIcon(icons.ActionOpenInNew)
	return icon
}()
