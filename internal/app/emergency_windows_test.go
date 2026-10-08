//go:build windows

package app

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/control"
	"github.com/aiwaki/lumatape/internal/platform/win32"
)

type emergencyOutput struct{ bytes.Buffer }

func (*emergencyOutput) Close() error { return nil }

func TestNativeEmergencyCancelsQueuedControllerMutations(t *testing.T) {
	in, writer := io.Pipe()
	out := &emergencyOutput{}
	server := control.New(in, out)
	t.Cleanup(func() { _ = writer.Close(); _ = server.Close() })
	queued := make(chan struct{}, 8)
	server.SetWake(func() { queued <- struct{}{} })
	encoder := json.NewEncoder(writer)
	for _, kind := range []string{"toggle", "reload", "apply"} {
		if err := encoder.Encode(control.Request{Version: control.Version, ID: kind, Type: kind}); err != nil {
			t.Fatal(err)
		}
		select {
		case <-queued:
		case <-time.After(time.Second):
			t.Fatal("command was not queued")
		}
	}
	cfg := config.Default()
	cfg.Enabled = true
	a := &application{cfg: cfg, configPath: filepath.Join(t.TempDir(), "config.json"), control: server, tray: &win32.Tray{}}
	// This is the exact path used by EventEmergency and the native menu, without
	// executeControl's dispatch. No live capture, window or global hotkey is needed.
	if err := a.emergency(); err != nil {
		t.Fatal(err)
	}
	if request, ok := server.Next(); ok {
		t.Fatalf("keyboard emergency retained a command that could restart it: %+v", request)
	}
	if a.cfg.Enabled || a.cfg.Aspect.Enabled || !a.suspended || a.emergencyBarrier.sequence != 1 || a.phase != "disabled" {
		t.Fatal("emergency did not disarm intent and rendering")
	}
	_ = writer.Close()
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), "cancelled_by_emergency") != 3 {
		t.Fatalf("queued requests were not explicitly cancelled: %s", out.String())
	}
}
