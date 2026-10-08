package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/aiwaki/lumatape/internal/locale"
	"github.com/aiwaki/lumatape/internal/platform/capture"
)

func TestLocalizedFailuresKeepProtocolAndOriginalCause(t *testing.T) {
	cause := capture.ErrGPUInteropUnavailable
	for _, language := range []string{"ru", "en"} {
		t.Run(language, func(t *testing.T) {
			t.Setenv(locale.Environment, language)
			err := gpuUnavailable(cause, true)
			failure := mutationFailure(err, false)
			if failure.Code != "gpu_interop_unavailable" || failure.SuggestedBackend != "full-compatibility" || !errors.Is(err, cause) {
				t.Fatalf("localized copy changed protocol/cause: %+v", failure)
			}
			want := "Fast GPU transfer"
			if language == "ru" {
				want = "Быстрый GPU-перенос"
			}
			if !strings.HasPrefix(failure.Message, want) {
				t.Fatalf("wrong UI language: %s", failure.Message)
			}
			capability := initialGPUCapability(cause, true)
			if capability.State != "unavailable" || capability.Code != failure.Code || capability.Reason != failure.Message {
				t.Fatalf("capability protocol/copy differs: %+v", capability)
			}
			const userDetail = "Игра & Game: пользовательский шейдер"
			applied := &AppliedSettingsError{Err: errors.New(userDetail)}
			message := mutationFailure(applied, true)
			if message.Code != "save_failed" || !message.Applied || !message.Unsaved || !strings.HasSuffix(message.Message, userDetail) {
				t.Fatalf("save semantics or user detail changed: %+v", message)
			}
		})
	}
}
