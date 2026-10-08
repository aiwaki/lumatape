package shaderpack

import (
	"strings"
	"testing"

	"github.com/aiwaki/lumatape/internal/locale"
)

func TestLocalePreservesShaderMetadataIdentityAndValidation(t *testing.T) {
	source := strings.ReplaceAll(fixture, `"Test"`, `"Эффект & Shader"`)
	source = strings.ReplaceAll(source, `"Tint"`, `"Цвет & Tint"`)
	var id string
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Setenv(locale.Environment, language)
			p, err := Parse(source)
			if err != nil {
				t.Fatal(err)
			}
			if id == "" {
				id = p.ID
			} else if p.ID != id {
				t.Fatal("language changed immutable shader identity")
			}
			if p.Name != "Эффект & Shader" || p.Parameters[0].Name != "Цвет & Tint" {
				t.Fatal("user-owned shader metadata was translated")
			}
			params := p.Defaults()
			params[0] = 2
			err = p.ValidateParams(params)
			if err == nil || !strings.HasPrefix(err.Error(), "Цвет & Tint: ") {
				t.Fatalf("user parameter name changed: %v", err)
			}
			suffix := "value outside the allowed range"
			if language == "ru" {
				suffix = "значение вне диапазона"
			}
			if !strings.HasSuffix(err.Error(), suffix) {
				t.Fatalf("validation language mismatch: %v", err)
			}
		})
	}
}
