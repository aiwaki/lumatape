package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/aiwaki/lumatape/internal/locale"
)

// Win32 RegisterHotKey modifier bits, intentionally independent of the platform
// package. The registrar may add MOD_NOREPEAT when calling the native API.
const (
	ModAlt     uint32 = 0x1
	ModControl uint32 = 0x2
	ModShift   uint32 = 0x4
	ModWin     uint32 = 0x8
)

type Hotkey struct{ Modifiers, Key uint32 }

var namedKeys = map[string]uint32{
	"ESC": 0x1b, "ESCAPE": 0x1b, "SPACE": 0x20, "PAUSE": 0x13,
	"HOME": 0x24, "END": 0x23, "INSERT": 0x2d, "DELETE": 0x2e,
	"PAGEUP": 0x21, "PAGEDOWN": 0x22,
	"LEFT": 0x25, "UP": 0x26, "RIGHT": 0x27, "DOWN": 0x28,
}

// ParseHotkey accepts chords such as Ctrl+Alt+F9 and Shift+Pause. Requiring a
// modifier avoids swallowing a common in-game key accidentally; registration
// conflicts must still be reported by the platform registrar before showing UI.
func ParseHotkey(value string) (Hotkey, error) {
	var result Hotkey
	parts := strings.Split(value, "+")
	for _, part := range parts {
		part = strings.ToUpper(strings.TrimSpace(part))
		var modifier uint32
		switch part {
		case "ALT":
			modifier = ModAlt
		case "CTRL", "CONTROL":
			modifier = ModControl
		case "SHIFT":
			modifier = ModShift
		case "WIN", "SUPER":
			modifier = ModWin
		}
		if modifier != 0 {
			if result.Modifiers&modifier != 0 {
				return Hotkey{}, fmt.Errorf(locale.Text("повторяющийся модификатор в %q", "duplicate modifier in %q"), value)
			}
			result.Modifiers |= modifier
			continue
		}
		if result.Key != 0 {
			return Hotkey{}, fmt.Errorf(locale.Text("сочетание %q должно содержать одну клавишу", "hotkey %q must contain one key"), value)
		}
		key, ok := namedKeys[part]
		if !ok && len(part) == 1 && ((part[0] >= 'A' && part[0] <= 'Z') || (part[0] >= '0' && part[0] <= '9')) {
			key = uint32(part[0])
			ok = true
		}
		if !ok && len(part) >= 2 && strings.HasPrefix(part, "F") {
			n, err := strconv.Atoi(part[1:])
			if err == nil && n >= 1 && n <= 24 && part == "F"+strconv.Itoa(n) {
				key, ok = uint32(0x70+n-1), true
			}
		}
		if !ok {
			return Hotkey{}, fmt.Errorf(locale.Text("неизвестная часть сочетания %q", "unknown hotkey component %q"), part)
		}
		result.Key = key
	}
	if result.Modifiers == 0 || result.Key == 0 {
		return Hotkey{}, fmt.Errorf(locale.Text("сочетание %q требует модификатор и одну клавишу", "hotkey %q requires a modifier and one key"), value)
	}
	if result.Key == 0x7b { // F12 is reserved for the debugger by RegisterHotKey.
		return Hotkey{}, fmt.Errorf(locale.Text("F12 зарезервирована Windows; выберите другую клавишу", "F12 is reserved by Windows; choose another key"))
	}
	return result, nil
}
