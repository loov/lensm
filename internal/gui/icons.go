package gui

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
	CheckIcon      = mustIcon(icons.NavigationCheck)
	SearchIcon     = mustIcon(icons.ActionSearch)
	ExpandMoreIcon = mustIcon(icons.NavigationExpandMore)
	CloseIcon      = mustIcon(icons.NavigationClose)
)
