package main

import (
	"gioui.org/widget"
	"golang.org/x/exp/shiny/materialdesign/icons"
)

// CommentIcon is used for the current assembly line comment editor.
var CommentIcon = func() *widget.Icon {
	icon, _ := widget.NewIcon(icons.EditorModeComment)
	return icon
}()

// SettingsIcon is used for application settings.
var SettingsIcon = func() *widget.Icon {
	icon, _ := widget.NewIcon(icons.ActionSettings)
	return icon
}()
