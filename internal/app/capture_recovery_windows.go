//go:build windows

package app

import (
	"time"

	"github.com/aiwaki/lumatape/internal/locale"
	"github.com/aiwaki/lumatape/internal/platform/capture"
)

func (a *application) captureFrameUsable(frame capture.Frame, updated bool, age time.Duration, now time.Time, idleValidated bool) bool {
	before := a.captureRecovery.Status()
	usable := a.captureRecovery.ObserveFrame(now, frame.Serial, updated, age, idleValidated)
	after := a.captureRecovery.Status()
	if after != before {
		data := a.captureObservation(frame, true)
		data["recovery"] = after
		data["observed_age_ms"] = float64(age) / float64(time.Millisecond)
		if usable {
			a.record("capture_recovery_ready", data)
		} else if after.State == "blocked" {
			a.record("capture_recovery_blocked", data)
		} else {
			a.record("stale_capture_hidden", data)
		}
	}
	if !usable {
		a.hideForCaptureRecovery()
	}
	return usable
}

func (a *application) hideForCaptureRecovery() {
	a.hide()
	reason := locale.Text("Захват задерживается; показано исходное окно. Ждём стабильного потока кадров.", "Capture is delayed; showing the original window while waiting for steady frames.")
	if a.captureRecovery.Status().State == "blocked" {
		reason = locale.Text("Захват снова задержался. Эффект приостановлен, чтобы изображение не мигало. Выключите и включите эффект для повтора; при повторной паузе попробуйте меньшее разрешение или другой режим обработки.", "Capture stalled again. The effect is paused to prevent flashing. Turn it off and on to retry; if it pauses again, try a lower resolution or another processing mode.")
	}
	a.setPhase("paused-stale", reason)
}

// Call after releasing the acquired texture, never while native ownership is
// outstanding. Closing a failed capture must not clear its retry latch.
func (a *application) finishCaptureRecoveryPause() error {
	if a.captureRecovery.Status().State == "blocked" {
		return a.closeCapture()
	}
	return nil
}

// Call only after the complete frame survived every final check. Failed first
// presentations stay part of the same recovery episode and never flash a frame.
func (a *application) captureFramePresented(frame capture.Frame, now time.Time) {
	if a.captureRecovery.Presented(now, frame.Serial) {
		data := a.captureObservation(frame, true)
		data["recovery"] = a.captureRecovery.Status()
		a.record("capture_recovery_ready", data)
	}
}
