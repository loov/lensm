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
	SettingsIcon       = mustIcon(icons.ImageTune)
	FunctionsIcon      = mustIcon(icons.EditorFormatListBulleted)
	CommentsIcon       = mustIcon(icons.CommunicationComment)
	HistoryIcon        = mustIcon(icons.ActionHistory)
	WatchIcon          = mustIcon(icons.ActionVisibility)
	MarksIcon          = mustIcon(icons.ActionAssessment)
	ProfileGraphIcon   = mustIcon(icons.EditorBubbleChart)
	FullscreenIcon     = mustIcon(icons.NavigationFullscreen)
	FullscreenExitIcon = mustIcon(icons.NavigationFullscreenExit)
	FlameGraphIcon     = mustIcon(icons.SocialWhatsHot)
	GraphIcon          = mustIcon(icons.HardwareDeviceHub)
	SummaryIcon        = mustIcon(icons.EditorShowChart)
	CallersIcon        = mustIcon(icons.CommunicationCallReceived)
	BackIcon           = mustIcon(icons.NavigationArrowBack)
	ForwardIcon        = mustIcon(icons.NavigationArrowForward)
)
