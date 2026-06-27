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

// CopyIcon is used for copying assembly to the clipboard.
var CopyIcon = func() *widget.Icon {
	icon, _ := widget.NewIcon(icons.ContentContentCopy)
	return icon
}()

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

// BackIcon and ForwardIcon navigate through visited functions.
var BackIcon = func() *widget.Icon {
	icon, _ := widget.NewIcon(icons.NavigationArrowBack)
	return icon
}()

var ForwardIcon = func() *widget.Icon {
	icon, _ := widget.NewIcon(icons.NavigationArrowForward)
	return icon
}()
