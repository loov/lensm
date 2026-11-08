package main

import (
	"gioui.org/widget"
	"golang.org/x/exp/shiny/materialdesign/icons"
)

func mustIcon(data []byte) *widget.Icon {
	icon, err := widget.NewIcon(data)
	if err != nil {
		panic(err)
	}
	return icon
}

var (
	SettingsIcon  = mustIcon(icons.ImageTune)
	CloseIcon     = mustIcon(icons.NavigationClose)
	FunctionsIcon = mustIcon(icons.EditorFormatListBulleted)
	CommentsIcon  = mustIcon(icons.CommunicationComment)
	HistoryIcon   = mustIcon(icons.ActionHistory)
	WatchIcon     = mustIcon(icons.ActionVisibility)
	MarksIcon     = mustIcon(icons.ActionAssessment)
)
