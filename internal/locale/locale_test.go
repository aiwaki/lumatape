package locale

import (
	"fmt"
	"testing"
)

func TestResolveDisplayLanguageAndSupportedOverrides(t *testing.T) {
	for _, tc := range []struct {
		override string
		system   uint16
		want     string
	}{
		{"", 0x0419, "ru"}, {"", 0x0819, "ru"},
		{"", 0x0409, "en"}, {"", 0x0809, "en"},
		{"", 0x0407, "en"}, {"", 0, "en"},
		{"en", 0x0419, "en"}, {"ru", 0x0409, "ru"},
		{" RU ", 0x0409, "ru"}, {"fr", 0x0419, "ru"},
		{"ru-RU", 0x0409, "en"},
	} {
		t.Run(fmt.Sprintf("%s/%04x", tc.override, tc.system), func(t *testing.T) {
			if got := resolve(tc.override, tc.system); got != tc.want {
				t.Fatalf("resolve = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTextPreservesUserContentAndSupportsChildOverride(t *testing.T) {
	const userText = "Игра & Game — shader: выключен"
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Setenv(Environment, language)
			format := Text("Окно %q недоступно", "Window %q is unavailable")
			want := fmt.Sprintf("Window %q is unavailable", userText)
			if language == "ru" {
				want = fmt.Sprintf("Окно %q недоступно", userText)
			}
			if got := fmt.Sprintf(format, userText); got != want || Language() != language {
				t.Fatalf("language/user content changed: %q", got)
			}
		})
	}
}
