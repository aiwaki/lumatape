package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/aiwaki/lumatape/internal/locale"
)

func TestLocaleDoesNotChangeSavedConfigurationOrUserWindowTitle(t *testing.T) {
	var baseline string
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Setenv(locale.Environment, language)
			c := Default()
			c.Target = Target{Kind: "window", WindowTitle: "Игра & Game — Выключен"}
			c.Preset = "VHS Tape"
			raw, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			if baseline == "" {
				baseline = string(raw)
			} else if string(raw) != baseline {
				t.Fatal("locale changed persisted configuration")
			}
			decoded, err := Decode(raw)
			if err != nil || decoded != c {
				t.Fatalf("configuration round trip changed user content: %v", err)
			}
			c.Mode = c.Target.WindowTitle
			err = c.Validate()
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%q", c.Target.WindowTitle)) {
				t.Fatalf("validation translated user content: %v", err)
			}
			prefix := "unknown mode"
			if language == "ru" {
				prefix = "неизвестный режим"
			}
			if !strings.HasPrefix(err.Error(), prefix) {
				t.Fatalf("validation not localized: %v", err)
			}
		})
	}
}
