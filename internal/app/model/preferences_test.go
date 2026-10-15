package model

import "testing"

func TestNormalizeTypedHotkeys(t *testing.T) {
	for input, want := range map[string]string{" P ": "p", "Shift + Control + P": "ctrl+shift+p", "Option+Command+1": "alt+meta+1", "Win+Q": "meta+q", "Ctrl+V": "ctrl+v", "": ""} {
		got, err := NormalizeHotkey(input)
		if err != nil || got != want {
			t.Errorf("NormalizeHotkey(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"Ctrl+Control+P", "Ctrl+", "Ctrl+Escape", "Super+Meta+Q", "P+Q"} {
		if _, err := NormalizeHotkey(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}

func TestHotkeyCombinations(t *testing.T) {
	for _, key := range []string{"p", "0", "ctrl+q", "alt+p", "shift+1", "meta+r", "ctrl+alt+shift+meta+9"} {
		if err := (PlayerPreferences{Hotkeys: map[string]string{"pen": key}}).Validate(); err != nil {
			t.Errorf("rejected %q: %v", key, err)
		}
	}
	for _, key := range []string{"ctrl", "ctrl+", "ctrl+ctrl+p", "shift+ctrl+p", "cmd+p", "ctrl+Escape", "ctrl+P", "ctrl++p", "ctrl+p+q"} {
		if err := (PlayerPreferences{Hotkeys: map[string]string{"pen": key}}).Validate(); err == nil {
			t.Errorf("accepted invalid binding %q", key)
		}
	}
	if err := (PlayerPreferences{Hotkeys: map[string]string{"pen": "p", "ruler": "ctrl+p", "ping": "ctrl+shift+p"}}).Validate(); err != nil {
		t.Fatalf("distinct combinations should be allowed: %v", err)
	}
	if err := (PlayerPreferences{Hotkeys: map[string]string{"pen": "ctrl+p", "ruler": "ctrl+p"}}).Validate(); err == nil {
		t.Fatal("duplicate combination was accepted")
	}
}
