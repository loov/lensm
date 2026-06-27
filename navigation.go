package main

const navigationHistoryLimit = 256

// NavigationHistory tracks function visits independently from open tabs.
type NavigationHistory struct {
	entries []string
	index   int
}

func (history *NavigationHistory) Reset() {
	history.entries = nil
	history.index = -1
}

func (history *NavigationHistory) Visit(name string) {
	if name == "" {
		return
	}
	if history.index >= 0 && history.index < len(history.entries) && history.entries[history.index] == name {
		return
	}
	if history.index+1 < len(history.entries) {
		history.entries = history.entries[:history.index+1]
	}
	history.entries = append(history.entries, name)
	if len(history.entries) > navigationHistoryLimit {
		drop := len(history.entries) - navigationHistoryLimit
		history.entries = history.entries[drop:]
	}
	history.index = len(history.entries) - 1
}

func (history *NavigationHistory) CanBack() bool {
	return history.index > 0
}

func (history *NavigationHistory) CanForward() bool {
	return history.index >= 0 && history.index+1 < len(history.entries)
}

func (history *NavigationHistory) Back() (string, bool) {
	if !history.CanBack() {
		return "", false
	}
	history.index--
	return history.entries[history.index], true
}

func (history *NavigationHistory) Forward() (string, bool) {
	if !history.CanForward() {
		return "", false
	}
	history.index++
	return history.entries[history.index], true
}

func (history *NavigationHistory) Current() string {
	if history.index < 0 || history.index >= len(history.entries) {
		return ""
	}
	return history.entries[history.index]
}
