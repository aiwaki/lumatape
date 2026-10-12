//go:build windows

package app

import (
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/platform/win32"
)

func TestBlockedCaptureRemainsPausedWhenSourceIsNotForeground(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	user32 := syscall.NewLazyDLL("user32.dll")
	cls, _ := syscall.UTF16PtrFromString("STATIC")
	// Own hidden test window. No activation, input, capture or renderer required.
	hwnd, _, err := user32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(cls)), 0, 0, 0, 0, 64, 64, 0, 0, 0, 0)
	if hwnd == 0 {
		t.Fatalf("create test source: %v", err)
	}
	defer user32.NewProc("DestroyWindow").Call(hwnd)
	cfg := config.Default()
	cfg.Enabled, cfg.Target.Kind, cfg.Target.WindowTitle = true, "window", "test source"
	a := &application{cfg: cfg, target: win32.Window{Handle: hwnd, PID: uint32(os.Getpid())}}
	now := time.Now()
	a.captureRecovery.Observe(now, 1, false, time.Second)
	for i := uint64(1); i <= 3; i++ {
		if a.captureRecovery.Observe(now.Add(time.Duration(i)*50*time.Millisecond), i+1, true, time.Millisecond) {
			a.captureRecovery.Presented(now.Add(time.Duration(i)*50*time.Millisecond), i+1)
		}
	}
	a.captureRecovery.Observe(now.Add(time.Second), 5, false, time.Second)
	for i := 0; i < 3; i++ {
		if err := a.frame(); err != nil {
			t.Fatal(err)
		}
		if a.phase != "paused-stale" || a.visible || a.cfg != cfg || a.capture != nil {
			t.Fatalf("blocked capture was retried or masked by focus: phase=%s", a.phase)
		}
		status := a.RuntimeStatus()
		if status.CaptureRecovery.State != "blocked" || status.EffectActive || status.SurfaceVisible || status.Reason == "" {
			t.Fatalf("blocked capture status is misleading: %+v", status)
		}
	}
}
