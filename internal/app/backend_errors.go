package app

import (
	"errors"
	"strings"

	"github.com/aiwaki/lumatape/internal/control"
	"github.com/aiwaki/lumatape/internal/locale"
	"github.com/aiwaki/lumatape/internal/platform/capture"
)

// BackendUnavailableError separates an actionable UI error from its technical
// cause. The latter belongs in diagnostics, not the primary product message.
type BackendUnavailableError struct {
	Code, Message, SuggestedBackend string
	Cause                           error
}

func (e *BackendUnavailableError) Error() string { return e.Message }
func (e *BackendUnavailableError) Unwrap() error { return e.Cause }

func gpuUnavailable(cause error, compatibilitySupported bool) *BackendUnavailableError {
	e := &BackendUnavailableError{Code: "gpu_interop_unavailable", Cause: cause,
		Message: locale.Text("Быстрый GPU-перенос недоступен в этом драйвере.", "Fast GPU transfer is unavailable with this driver.")}
	if compatibilitySupported {
		e.SuggestedBackend = "full-compatibility"
		e.Message += locale.Text(" Для эффектов VHS и CRT выберите «Full · CPU (совместимость, до 30 кадров/с)».", " For VHS and CRT effects, choose “Full · CPU (compatibility, up to 30 FPS)”.")
	} else {
		e.Message += locale.Text(" В этом комплекте доступен Lightweight. Для Full CPU требуется обновлённый комплект LumaTape.", " This package supports Lightweight. Full CPU requires an updated LumaTape package.")
	}
	return e
}

func initialGPUCapability(probeErr error, compatibilitySupported bool) CapabilityStatus {
	if errors.Is(probeErr, capture.ErrGPUInteropUnavailable) {
		failure := gpuUnavailable(probeErr, compatibilitySupported)
		return CapabilityStatus{State: "unavailable", Code: failure.Code, Reason: failure.Message}
	}
	// Metadata support is necessary, but only opening the chosen source proves
	// that its WGC/D3D texture can be shared with this GL context.
	c := CapabilityStatus{State: "unknown", Reason: locale.Text("GPU-захват будет проверен после выбора окна и включения.", "GPU capture will be checked after selecting a window and enabling the effect.")}
	if probeErr != nil {
		c.Code = "gpu_probe_failed"
		c.Reason = locale.Text("Возможности GPU пока не удалось проверить. Захват будет проверен при включении для выбранного окна.", "GPU capabilities could not be checked yet. Capture will be checked for the selected window when enabled.")
	}
	return c
}

func isMissingGPUInterop(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, capture.ErrGPUInteropUnavailable) {
		return true
	}
	// ABI v1 reports strings. These two native prerequisite failures are known
	// and global to the current GL context; source/HDR/device failures must not
	// be mislabeled as missing extensions or trigger an automatic CPU choice.
	return strings.Contains(err.Error(), "this OpenGL driver lacks WGL_NV_DX_interop2") ||
		strings.Contains(err.Error(), "driver advertises WGL interop but required entry points are missing")
}

func mutationFailure(err error, unsaved bool) *control.Failure {
	if err == nil {
		return nil
	}
	f := &control.Failure{Code: "apply_failed", Message: err.Error(), Unsaved: unsaved}
	var applied *AppliedSettingsError
	var backend *BackendUnavailableError
	var shader *ShaderError
	if errors.As(err, &applied) {
		f.Code, f.Applied = "save_failed", true
	} else if errors.As(err, &shader) {
		f.Code = shader.Code
	} else if errors.As(err, &backend) {
		f.Code, f.Message, f.SuggestedBackend = backend.Code, backend.Message, backend.SuggestedBackend
	}
	return f
}
