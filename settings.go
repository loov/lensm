package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

type AppSettings struct {
	SyntaxStyle   string   `json:"syntax_style"`
	Dark          bool     `json:"dark,omitempty"`
	ShowNativeAsm bool     `json:"show_native_asm"`
	ShowAsmHelp   bool     `json:"show_asm_help"`
	TextSize      int      `json:"text_size,omitempty"`
	Context       int      `json:"context,omitempty"`
	LastPath      string   `json:"last_path,omitempty"`
	OpenTabs      []string `json:"open_tabs,omitempty"`
	ActiveTab     string   `json:"active_tab,omitempty"`
}

func DefaultAppSettings() AppSettings {
	return AppSettings{
		SyntaxStyle:   SyntaxStyleGoLand,
		ShowNativeAsm: true,
		ShowAsmHelp:   true,
		TextSize:      12,
		Context:       3,
	}
}

func LoadAppSettings() (AppSettings, error) {
	settings := DefaultAppSettings()
	path, err := appSettingsPath()
	if err != nil {
		return settings, err
	}

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
	settings.SyntaxStyle = NormalizeSyntaxStyle(settings.SyntaxStyle)
	if settings.TextSize <= 0 {
		settings.TextSize = DefaultAppSettings().TextSize
	}
	if settings.Context <= 0 {
		settings.Context = DefaultAppSettings().Context
	}
	settings.LastPath = cleanPath(settings.LastPath)
	settings.OpenTabs = cleanFuncNames(settings.OpenTabs)
	if settings.ActiveTab != "" && !slices.Contains(settings.OpenTabs, settings.ActiveTab) {
		settings.ActiveTab = ""
	}
	return settings, nil
}

func SaveAppSettings(settings AppSettings) error {
	settings.SyntaxStyle = NormalizeSyntaxStyle(settings.SyntaxStyle)
	if settings.TextSize <= 0 {
		settings.TextSize = DefaultAppSettings().TextSize
	}
	if settings.Context <= 0 {
		settings.Context = DefaultAppSettings().Context
	}
	settings.LastPath = cleanPath(settings.LastPath)
	settings.OpenTabs = cleanFuncNames(settings.OpenTabs)
	if settings.ActiveTab != "" && !slices.Contains(settings.OpenTabs, settings.ActiveTab) {
		settings.ActiveTab = ""
	}
	path, err := appSettingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func appSettingsPath() (string, error) {
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
