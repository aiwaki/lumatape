package config

import "testing"

func TestHotkeys(t *testing.T) {
	for _, tc := range []struct {
		text string
		want Hotkey
	}{
		{"Ctrl+Alt+F9", Hotkey{ModControl | ModAlt, 0x78}},
		{" ALT + Control + f9 ", Hotkey{ModControl | ModAlt, 0x78}},
		{"Shift+Pause", Hotkey{ModShift, 0x13}},
		{"Win+Z", Hotkey{ModWin, 'Z'}},
		{"Super+9", Hotkey{ModWin, '9'}},
		{"Ctrl+F24", Hotkey{ModControl, 0x87}},
	} {
		got, err := ParseHotkey(tc.text)
		if err != nil || got != tc.want {
			t.Errorf("%q: got %+v %v, want %+v", tc.text, got, err, tc.want)
		}
	}
	for _, value := range []string{"", "F9", "Ctrl", "Ctrl+", "Ctrl++F9", "Ctrl+Control+F9", "Ctrl+F0", "Ctrl+F25", "Ctrl+F09", "Ctrl+F12", "Ctrl+F9+F10", "Ctrl+香蕉", "Ctrl+Enter"} {
		if _, err := ParseHotkey(value); err == nil {
			t.Errorf("accepted invalid hotkey %q", value)
		}
	}
}
