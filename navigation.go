package main

const navigationHistoryLimit = 256

// NavigationHistory tracks function visits independently from open tabs.
type NavigationHistory struct {
	entries []string
	index   int
}

func (h *NavigationHistory) Reset() {
	h.entries = nil
	h.index = -1
}

func (h *NavigationHistory) Visit(name string) {
	if name == "" {
		return
	}
	if h.index >= 0 && h.index < len(h.entries) && h.entries[h.index] == name {
		return
	}
	if h.index+1 < len(h.entries) {
		h.entries = h.entries[:h.index+1]
	}
	h.entries = append(h.entries, name)
	if len(h.entries) > navigationHistoryLimit {
		drop := len(h.entries) - navigationHistoryLimit
		h.entries = h.entries[drop:]
	}
	h.index = len(h.entries) - 1
}

func (h *NavigationHistory) CanBack() bool {
	return h.index > 0
}

func (h *NavigationHistory) CanForward() bool {
	return h.index >= 0 && h.index+1 < len(h.entries)
}

func (h *NavigationHistory) Back() (string, bool) {
	if !h.CanBack() {
		return "", false
	}
	h.index--
	return h.entries[h.index], true
}

func (h *NavigationHistory) Forward() (string, bool) {
	if !h.CanForward() {
		return "", false
	}
	h.index++
	return h.entries[h.index], true
}

func (h *NavigationHistory) Current() string {
	if h.index < 0 || h.index >= len(h.entries) {
		return ""
	}
	return h.entries[h.index]
}

func (ui *FileUI) recordNavigation(name string) {
	if !ui.navigatingHistory {
		ui.Navigation.Visit(name)
	}
}

func (ui *FileUI) navigateBack() {
	for ui.Navigation.CanBack() {
		name, _ := ui.Navigation.Back()
		if ui.navigateHistoryEntry(name) {
			return
		}
	}
}

func (ui *FileUI) navigateForward() {
	for ui.Navigation.CanForward() {
		name, _ := ui.Navigation.Forward()
		if ui.navigateHistoryEntry(name) {
			return
		}
	}
}

func (ui *FileUI) navigateHistoryEntry(name string) bool {
	fn := ui.findFunc(name)
	if fn == nil {
		return false
	}
	ui.navigatingHistory = true
	ui.openFuncTab(fn)
	ui.navigatingHistory = false
	ui.copyStatus = ""
	ui.invalidateMain()
	return true
}
