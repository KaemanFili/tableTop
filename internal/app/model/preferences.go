package model

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// PlayerPreferences belong to an account, independently of tabletop membership.
type PlayerPreferences struct {
	Hotkeys map[string]string `json:"hotkeys,omitempty"`
}

type HotkeyAction struct {
	ID, Label string
	GMOnly    bool
}

// NormalizeHotkey accepts displayed or pasted combinations, storing one stable
// representation regardless of modifier order, spacing, or keyboard labels.
func NormalizeHotkey(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	parts := strings.Split(strings.ToLower(value), "+")
	key := strings.TrimSpace(parts[len(parts)-1])
	modifiers := make(map[string]bool)
	aliases := map[string]string{"ctrl": "ctrl", "control": "ctrl", "alt": "alt", "option": "alt", "shift": "shift", "meta": "meta", "cmd": "meta", "command": "meta", "win": "meta", "windows": "meta", "super": "meta"}
	for _, part := range parts[:len(parts)-1] {
		modifier := aliases[strings.TrimSpace(part)]
		if modifier == "" || modifiers[modifier] {
			return "", fmt.Errorf("enter a key combination such as P or Ctrl+Shift+P")
		}
		modifiers[modifier] = true
	}
	var normalized []string
	for _, modifier := range []string{"ctrl", "alt", "shift", "meta"} {
		if modifiers[modifier] {
			normalized = append(normalized, modifier)
		}
	}
	binding := strings.Join(append(normalized, key), "+")
	if !hotkeyPattern.MatchString(binding) {
		return "", fmt.Errorf("use a letter or number, optionally combined with modifier keys")
	}
	return binding, nil
}

func HotkeyLabel(binding string) string {
	if binding == "" {
		return ""
	}
	names := map[string]string{"ctrl": "Ctrl", "alt": "Alt", "shift": "Shift", "meta": "Meta"}
	parts := strings.Split(binding, "+")
	for i, part := range parts {
		if name := names[part]; name != "" {
			parts[i] = name
		} else {
			parts[i] = strings.ToUpper(part)
		}
	}
	return strings.Join(parts, "+")
}

// Canonical order keeps equivalent combinations from bypassing duplicate checks.
// Single-character bindings from earlier preference files remain valid.
var hotkeyPattern = regexp.MustCompile(`^(ctrl\+)?(alt\+)?(shift\+)?(meta\+)?[a-z0-9]$`)

func HotkeyActions() []HotkeyAction {
	return []HotkeyAction{
		{"ruler", "Ruler", false},
		{"pen", "Pen", false},
		{"eraser", "Eraser", false},
		{"ping", "Ping", false},
		{"fog", "Fog of war (GM only)", true},
		{"fog-eraser", "Erase fog (GM only)", true},
	}
}

func (p PlayerPreferences) Validate() error {
	actions := HotkeyActions()
	seen := make(map[string]bool)
	for action, key := range p.Hotkeys {
		if !slices.ContainsFunc(actions, func(a HotkeyAction) bool { return a.ID == action }) {
			return fmt.Errorf("unknown hotkey utility %q", action)
		}
		if !hotkeyPattern.MatchString(key) {
			return fmt.Errorf("choose a letter A–Z or number 0–9 with optional Ctrl, Alt/Option, Shift, or Meta modifiers")
		}
		if seen[key] {
			return fmt.Errorf("each utility must use a different hotkey combination")
		}
		seen[key] = true
	}
	return nil
}
