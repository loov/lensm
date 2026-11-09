package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"loov.dev/lensm/internal/atomicfile"
	"loov.dev/lensm/internal/comments"
	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/syntax"
)

type AppSettings struct {
	SyntaxStyle   string `json:"syntax_style"`
	Dark          bool   `json:"dark,omitempty"`
	ShowGoAsm     bool   `json:"show_go_asm"`
	ShowNativeAsm bool   `json:"show_native_asm"`
	ShowSource    bool   `json:"show_source"`
	ShowAsmHelp   bool   `json:"show_asm_help"`
	// HoverLinksOnly draws only the hovered line's source-to-assembly
	// link instead of all of them.
	HoverLinksOnly bool `json:"hover_links_only,omitempty"`
	// SyncScroll scrolls the assembly and the source together.
	SyncScroll bool `json:"sync_scroll,omitempty"`
	// HiddenMarks are the run time cost marks turned off in the
	// Diagnostics panel.
	HiddenMarks disasm.Mark `json:"hidden_marks,omitempty"`
	// HiddenNotes are the compiler note kinds turned off there. Not
	// omitted when empty, since the default hides some.
	HiddenNotes disasm.NoteKind `json:"hidden_notes"`
	// HideDiagnostics turns off every mark and note at once, keeping
	// the kinds chosen above for when they come back.
	HideDiagnostics bool    `json:"hide_diagnostics,omitempty"`
	TextSize        int     `json:"text_size,omitempty"`
	SidebarRatio    float32 `json:"sidebar_ratio,omitempty"`
	// HideSidebar collapses the panel left of the code to its icon strip.
	HideSidebar bool `json:"hide_sidebar,omitempty"`
	// GraphRatio is the code's share of the width beside the profile graph.
	GraphRatio float32 `json:"graph_ratio,omitempty"`
	LastPath   string  `json:"last_path,omitempty"`
	// Sessions are the tabs left open in each binary, most recent first.
	Sessions []Session `json:"sessions,omitempty"`

	// OpenTabs and ActiveTab are the single session of older versions,
	// read once into Sessions.
	OpenTabs  []string `json:"open_tabs,omitempty"`
	ActiveTab string   `json:"active_tab,omitempty"`
}

// maxSessions is how many binaries keep their tabs.
const maxSessions = 20

// Session is the tabs open in one binary.
type Session struct {
	Path   string       `json:"path"`
	Tabs   []TabSession `json:"tabs,omitempty"`
	Active string       `json:"active,omitempty"`
}

// TabSession is one open tab and the row selected in it, shown again
// when the tab reopens.
type TabSession struct {
	Func string        `json:"func"`
	View comments.View `json:"view,omitempty"`
	File string        `json:"file,omitempty"`
	Line int           `json:"line,omitempty"`
	PC   uint64        `json:"pc,omitempty"`
}

// session returns the session kept for path.
func (settings AppSettings) session(path string) (Session, bool) {
	for _, s := range settings.Sessions {
		if s.Path == path {
			return s, true
		}
	}
	return Session{}, false
}

// withSession puts s first, replacing the one for its path.
func (settings AppSettings) withSession(s Session) AppSettings {
	sessions := []Session{s}
	for _, old := range settings.Sessions {
		if old.Path != s.Path && len(sessions) < maxSessions {
			sessions = append(sessions, old)
		}
	}
	settings.Sessions = sessions
	return settings
}

func DefaultAppSettings() AppSettings {
	return AppSettings{
		SyntaxStyle:   syntax.StyleXcode,
		ShowGoAsm:     true,
		ShowNativeAsm: true,
		ShowSource:    true,
		ShowAsmHelp:   true,
		TextSize:      12,
		SidebarRatio:  0.15,
		GraphRatio:    0.55,
		// Most parameters leak somewhere and most pointer uses have a
		// nil check, so these would bury the other notes. The bounds
		// check mark already shows the remaining bounds checks.
		HiddenNotes: disasm.NoteLeak | disasm.NoteNil | disasm.NoteBounds,
	}
}

// LoadAppSettings reads the settings at path, or returns the defaults
// when there is no file yet.
func LoadAppSettings(path string) (AppSettings, error) {
	settings := DefaultAppSettings()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return settings, nil
		}
		return settings, err
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		// Move the unreadable file aside: callers proceed with defaults,
		// and the next save would otherwise silently replace every
		// preference the user had.
		backup := path + ".corrupt"
		if renameErr := os.Rename(path, backup); renameErr == nil {
			return DefaultAppSettings(), fmt.Errorf("decode %s (moved to %s): %w", path, backup, err)
		}
		return DefaultAppSettings(), fmt.Errorf("decode %s: %w", path, err)
	}
	return settings.normalized(), nil
}

// normalized fills invalid or missing values with defaults and drops
// references to tabs that don't exist; applied on both load and save so
// the file and the in-memory settings never disagree.
func (settings AppSettings) normalized() AppSettings {
	defaults := DefaultAppSettings()
	settings.SyntaxStyle = syntax.NormalizeStyle(settings.SyntaxStyle)
	if settings.TextSize <= 0 {
		settings.TextSize = defaults.TextSize
	}
	if settings.GraphRatio <= 0 || settings.GraphRatio >= 1 {
		settings.GraphRatio = defaults.GraphRatio
	}
	if settings.SidebarRatio <= 0 || settings.SidebarRatio >= 1 {
		settings.SidebarRatio = defaults.SidebarRatio
	}
	settings.LastPath = comments.CleanPath(settings.LastPath)
	if len(settings.OpenTabs) > 0 && settings.LastPath != "" {
		if _, ok := settings.session(settings.LastPath); !ok {
			s := Session{Path: settings.LastPath, Active: settings.ActiveTab}
			for _, name := range settings.OpenTabs {
				s.Tabs = append(s.Tabs, TabSession{Func: name})
			}
			settings = settings.withSession(s)
		}
	}
	settings.OpenTabs, settings.ActiveTab = nil, ""
	sessions := settings.Sessions[:0:0]
	for _, s := range settings.Sessions {
		s.Path = comments.CleanPath(s.Path)
		seen := map[string]bool{}
		tabs := s.Tabs[:0:0]
		for _, tab := range s.Tabs {
			if tab.Func != "" && !seen[tab.Func] {
				seen[tab.Func] = true
				tabs = append(tabs, tab)
			}
		}
		s.Tabs = tabs
		if !seen[s.Active] {
			s.Active = ""
		}
		if s.Path != "" && len(sessions) < maxSessions {
			sessions = append(sessions, s)
		}
	}
	settings.Sessions = sessions
	return settings
}

// SaveAppSettings writes settings to path.
func SaveAppSettings(path string, settings AppSettings) error {
	settings = settings.normalized()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicfile.Write(path, data, 0o644)
}

// AppSettingsPath is where settings live for the current user.
func AppSettingsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "lensm", "config.json"), nil
}

func cleanFuncNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	out := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}
