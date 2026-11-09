package main

import (
	"gioui.org/layout"
	"gioui.org/widget"

	"loov.dev/lensm/internal/gui"
)

// Menu bar items beyond the ones the window's app menu has.
const (
	menuGoAsm = 100 + iota
	menuNativeAsm
	menuSource
	menuDiagnostics
	menuSyncScroll
	menuGoTo
	menuFilter
	menuSidebar
)

// nativeMenuDef is one menu of the menu bar.
type nativeMenuDef struct {
	Title string
	Items []nativeMenuItem
}

// nativeMenuItem is a menu bar item, a separator without a label. Key
// is its shortcut with Command, and Option when Alt is set. Selector
// sends a standard action instead of reporting Tag.
type nativeMenuItem struct {
	Label    string
	Key      string
	Alt      bool
	Tag      int
	Selector string
}

// menuBar lists the menu bar. Copy has no shortcut: the menu would
// take Command-C from the text fields too.
var menuBar = []nativeMenuDef{
	{"lensm", []nativeMenuItem{
		{Label: "Settings…", Key: ",", Tag: menuSettings},
		{},
		{Label: "Hide lensm", Key: "h", Selector: "hide:"},
		{Label: "Hide Others", Key: "h", Alt: true, Selector: "hideOtherApplications:"},
		{Label: "Show All", Selector: "unhideAllApplications:"},
		{},
		{Label: "Quit lensm", Key: "q", Tag: menuQuit},
	}},
	{"File", []nativeMenuItem{
		{Label: "Open…", Key: "o", Tag: menuOpen},
		{Label: "Reload", Key: "r", Tag: menuReload},
		{Label: "Watch for Changes", Tag: menuWatch},
		{},
		{Label: "Open Profile…", Tag: menuOpenProfile},
		{Label: "Close Profile", Tag: menuCloseProfile},
		{Label: "Compare Profile With…", Tag: menuBaseProfile},
		{Label: "Stop Comparing Profiles", Tag: menuCloseBaseProfile},
		{},
		{Label: "Compare With…", Tag: menuCompare},
		{Label: "Stop Comparing", Tag: menuStopCompare},
	}},
	{"Edit", []nativeMenuItem{
		{Label: "Copy Selection", Tag: menuCopy},
	}},
	{"View", []nativeMenuItem{
		{Label: "Sidebar", Key: "b", Tag: menuSidebar},
		{},
		{Label: "Go Assembly", Tag: menuGoAsm},
		{Label: "Native Assembly", Tag: menuNativeAsm},
		{Label: "Source", Tag: menuSource},
		{Label: "Diagnostics", Tag: menuDiagnostics},
		{},
		{Label: "Sync Scroll", Tag: menuSyncScroll},
		{Label: "Profile Graph", Tag: menuProfileGraph},
	}},
	{"Go", []nativeMenuItem{
		{Label: "Go to Function…", Key: "p", Tag: menuGoTo},
		{Label: "Filter Functions", Key: "f", Tag: menuFilter},
		{},
		{Label: "Back", Key: "[", Tag: menuBack},
		{Label: "Forward", Key: "]", Tag: menuForward},
	}},
	{"Window", []nativeMenuItem{
		{Label: "Minimize", Key: "m", Selector: "performMiniaturize:"},
		{Label: "Zoom", Selector: "performZoom:"},
	}},
	{"Help", []nativeMenuItem{
		{Label: "Keyboard Shortcuts", Tag: menuShortcuts},
	}},
}

// runMenuItem does what a menu item, in the window or the menu bar,
// says.
func (ui *FileUI) runMenuItem(gtx layout.Context, item int) {
	switch item {
	case menuOpen:
		ui.chooseFile()
	case menuOpenProfile:
		ui.chooseProfile()
	case menuCompare:
		if ui.File != nil {
			ui.pick(pickCompare)
		}
	case menuStopCompare:
		ui.setCompare("", nil)
	case menuSidebar:
		ui.showSidebar(ui.Settings.HideSidebar)
	case menuProfileGraph:
		ui.ShowGraph = !ui.ShowGraph
	case menuBaseProfile:
		if ui.Profile != nil {
			ui.pick(pickBaseProfile)
		}
	case menuCloseBaseProfile:
		ui.Config.BaseProfilePath = ""
		ui.loadBaseProfile()
	case menuCloseProfile:
		ui.Config.ProfilePath = ""
		ui.loadProfile()
	case menuReload:
		ui.reload()
	case menuWatch:
		ui.setWatch(!ui.Config.Watch)
	case menuCopy:
		if code := ui.activeCode(); code != nil && code.Code != nil {
			ui.writeClipboardText(gtx, code.SelectedText(), "Copied selection")
		}
	case menuBack:
		ui.navigateBack()
	case menuForward:
		ui.navigateForward()
	case menuSettings:
		ui.openSettings()
	case menuShortcuts:
		ui.Shortcuts.Open = true
	case menuQuit:
		ui.quit()
	case menuGoAsm:
		ui.toggleColumn(&ui.ShowGoAsm)
	case menuNativeAsm:
		ui.toggleColumn(&ui.ShowNativeAsm)
	case menuSource:
		ui.toggleColumn(&ui.ShowSource)
	case menuDiagnostics:
		ui.applySetting(func(s *AppSettings) { s.HideDiagnostics = !s.HideDiagnostics })
	case menuSyncScroll:
		ui.applySetting(func(s *AppSettings) { s.SyncScroll = !s.SyncScroll })
	case menuGoTo:
		ui.QuickOpen.Show(gtx)
	case menuFilter:
		ui.focusFilter(gtx)
	}
}

// toggleColumn flips a column, unless it is the last one showing.
func (ui *FileUI) toggleColumn(b *widget.Bool) {
	b.Value = !b.Value
	if !ui.ShowGoAsm.Value && !ui.ShowNativeAsm.Value && !ui.ShowSource.Value {
		b.Value = true
	}
	ui.applySetting(func(s *AppSettings) {
		s.ShowGoAsm, s.ShowNativeAsm, s.ShowSource = ui.ShowGoAsm.Value, ui.ShowNativeAsm.Value, ui.ShowSource.Value
	})
}

// handleMenuBar runs the items chosen in the menu bar since the last
// frame and updates its checkmarks and disabled items.
func (ui *FileUI) handleMenuBar(gtx layout.Context) {
	if !nativeMenu {
		return
	}
	for _, item := range ui.menuQueue {
		ui.runMenuItem(gtx, item)
	}
	ui.menuQueue = ui.menuQueue[:0]

	selection := ""
	if code := ui.activeCode(); code != nil && code.Code != nil {
		selection = code.SelectedText()
	}
	type state struct{ checked, enabled bool }
	states := map[int]state{
		menuReload:           {false, ui.Config.Path != ""},
		menuWatch:            {ui.Config.Watch, true},
		menuOpenProfile:      {false, ui.File != nil},
		menuCloseProfile:     {false, ui.Profile != nil},
		menuBaseProfile:      {false, ui.Profile != nil},
		menuCloseBaseProfile: {false, ui.BaseProfile != nil},
		menuCompare:          {false, ui.File != nil},
		menuStopCompare:      {false, ui.Compare.file != nil},
		menuProfileGraph:     {ui.ShowGraph, ui.Profile != nil},
		menuSidebar:          {!ui.Settings.HideSidebar, true},
		menuCopy:             {false, selection != ""},
		menuBack:             {false, ui.Navigation.CanBack()},
		menuForward:          {false, ui.Navigation.CanForward()},
		menuGoAsm:            {ui.ShowGoAsm.Value, true},
		menuNativeAsm:        {ui.ShowNativeAsm.Value, true},
		menuSource:           {ui.ShowSource.Value, true},
		menuDiagnostics:      {!ui.Settings.HideDiagnostics, true},
		menuSyncScroll:       {ui.Settings.SyncScroll, true},
	}
	if ui.menuStates == nil {
		ui.menuStates = map[int]uint8{}
	}
	for tag, st := range states {
		packed := uint8(0)
		if st.checked {
			packed |= 1
		}
		if st.enabled {
			packed |= 2
		}
		if old, ok := ui.menuStates[tag]; !ok || old != packed {
			ui.menuStates[tag] = packed
			setMenuState(tag, st.checked, st.enabled)
		}
	}
}

// App menu items, in the order appMenuItems lists them.
const (
	menuOpen = iota
	menuReload
	menuWatch
	menuOpenProfile
	menuCloseProfile
	menuBaseProfile
	menuCloseBaseProfile
	menuCompare
	menuStopCompare
	menuProfileGraph
	sepFile
	menuCopy
	menuBack
	menuForward
	sepGo
	menuSettings
	menuShortcuts
	sepApp
	menuQuit
)

func (ui *FileUI) appMenuItems() []gui.MenuItem {
	selection := ""
	if code := ui.activeCode(); code != nil && code.Code != nil {
		selection = code.SelectedText()
	}
	return []gui.MenuItem{
		menuOpen:             {Label: "Open…", Shortcut: shortcut("O")},
		menuReload:           {Label: "Reload", Shortcut: shortcut("R"), Disabled: ui.Config.Path == ""},
		menuWatch:            {Label: "Watch for Changes", Checked: ui.Config.Watch},
		menuOpenProfile:      {Label: "Open Profile…", Disabled: ui.File == nil},
		menuCloseProfile:     {Label: "Close Profile", Disabled: ui.Profile == nil},
		menuBaseProfile:      {Label: "Compare Profile With…", Disabled: ui.Profile == nil},
		menuCloseBaseProfile: {Label: "Stop Comparing Profiles", Disabled: ui.BaseProfile == nil},
		menuCompare:          {Label: "Compare With…", Disabled: ui.File == nil},
		menuStopCompare:      {Label: "Stop Comparing", Disabled: ui.Compare.file == nil},
		menuProfileGraph:     {Label: "Profile Graph", Checked: ui.ShowGraph, Disabled: ui.Profile == nil},
		sepFile:              {Separator: true},
		menuCopy:             {Label: "Copy Selection", Shortcut: shortcut("C"), Disabled: selection == ""},
		menuBack:             {Label: "Back", Shortcut: altShortcut("←"), Disabled: !ui.Navigation.CanBack()},
		menuForward:          {Label: "Forward", Shortcut: altShortcut("→"), Disabled: !ui.Navigation.CanForward()},
		sepGo:                {Separator: true},
		menuSettings:         {Label: "Settings…", Shortcut: shortcut(",")},
		menuShortcuts:        {Label: "Keyboard Shortcuts", Shortcut: "?"},
		sepApp:               {Separator: true},
		menuQuit:             {Label: "Quit", Shortcut: shortcut("Q")},
	}
}

func (ui *FileUI) handleAppMenu(gtx layout.Context) {
	if item, ok := ui.AppMenu.Update(gtx); ok {
		ui.runMenuItem(gtx, item)
	}
}
