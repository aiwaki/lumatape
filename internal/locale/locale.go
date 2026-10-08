// Package locale selects product copy without changing protocol/config keys or
// user-owned content. The desktop host passes its language to child processes.
package locale

import (
	"os"
	"strings"
)

const Environment = "LUMATAPE_UI_LANGUAGE"

// Language follows the Windows display language, not regional formatting or the
// VM host language. Only the two supported explicit overrides are accepted.
func Language() string {
	return resolve(os.Getenv(Environment), systemLanguage())
}

func resolve(override string, system uint16) string {
	switch strings.ToLower(strings.TrimSpace(override)) {
	case "ru":
		return "ru"
	case "en":
		return "en"
	}
	if system&0x03ff == 0x19 { // PRIMARYLANGID == LANG_RUSSIAN
		return "ru"
	}
	return "en"
}

// Text translates only the app-owned string at the call site. Dynamic window
// titles, shader metadata, OS error details and other arguments stay intact.
func Text(ru, en string) string {
	if Language() == "ru" {
		return ru
	}
	return en
}
