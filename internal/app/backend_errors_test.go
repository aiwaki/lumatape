package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aiwaki/lumatape/internal/locale"
	"github.com/aiwaki/lumatape/internal/platform/capture"
)

func TestGPUFailureHasTypedExplicitChoiceWithoutRawDriverMessage(t *testing.T) {
	cause := fmt.Errorf("%w: WGL_NV_DX_interop2", capture.ErrGPUInteropUnavailable)
	backend := gpuUnavailable(cause, true)
	wrapped := fmt.Errorf("capture open: %w", backend)
	failure := mutationFailure(wrapped, false)
	if failure.Code != "gpu_interop_unavailable" || failure.SuggestedBackend != "full-compatibility" || failure.Applied || failure.Unsaved {
		t.Fatalf("wrong failure contract: %+v", failure)
	}
	data, err := json.Marshal(failure)
	if err != nil {
		t.Fatal(err)
	}
	for _, technical := range []string{"WGL_NV", "capture open", "prerequisites", "use Lightweight"} {
		if strings.Contains(string(data), technical) {
			t.Fatalf("technical cause leaked into UI: %s", data)
		}
	}
	if !strings.Contains(failure.Message, "CPU") || !errors.Is(wrapped, capture.ErrGPUInteropUnavailable) {
		t.Fatal("explicit CPU guidance or diagnostic cause lost")
	}
	legacy := mutationFailure(gpuUnavailable(cause, false), false)
	if legacy.SuggestedBackend != "" || !strings.Contains(legacy.Message, locale.Text("обновлённый комплект", "updated LumaTape package")) {
		t.Fatalf("unsupported CPU bridge was suggested as usable: %+v", legacy)
	}
}

func TestOnlyKnownGlobalInteropFailuresAreClassified(t *testing.T) {
	for _, text := range []string{
		"capture open: Full mode unavailable: this OpenGL driver lacks WGL_NV_DX_interop2; use Lightweight",
		"The driver advertises WGL interop but required entry points are missing",
	} {
		if !isMissingGPUInterop(errors.New(text)) {
			t.Fatalf("legacy ABI cause not classified: %s", text)
		}
	}
	for _, err := range []error{nil, errors.New("HDR source is unsupported"), errors.New("wglDXLockObjectsNV failed"), errors.New("game window closed"), errors.New("GPU probe could not read extension string")} {
		if isMissingGPUInterop(err) {
			t.Fatalf("source/device/probe failure became confirmed missing interop: %v", err)
		}
	}
	if f := mutationFailure(&AppliedSettingsError{Err: errors.New("write denied")}, true); f.Code != "save_failed" || !f.Applied || !f.Unsaved || f.SuggestedBackend != "" {
		t.Fatalf("typed backend handling changed applied/save failure semantics: %+v", f)
	}
}

func TestGPUProbeDistinguishesUnknownFromConfirmedMissingPrerequisites(t *testing.T) {
	for _, test := range []struct {
		err         error
		state, code string
	}{
		{nil, "unknown", ""},
		{errors.New("no extension query API"), "unknown", "gpu_probe_failed"},
		{errors.New("NULL query result"), "unknown", "gpu_probe_failed"},
		{errors.New("this OpenGL driver lacks WGL_NV_DX_interop2"), "unknown", "gpu_probe_failed"},
		{fmt.Errorf("%w: missing exact token", capture.ErrGPUInteropUnavailable), "unavailable", "gpu_interop_unavailable"},
	} {
		got := initialGPUCapability(test.err, true)
		if got.State != test.state || got.Code != test.code || got.Reason == "" {
			t.Fatalf("probe %v => %+v, want %s/%s", test.err, got, test.state, test.code)
		}
	}
}
