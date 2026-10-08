//go:build windows

package app

import (
	"errors"
	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/platform/capture"
	"github.com/aiwaki/lumatape/internal/platform/win32"
	"github.com/aiwaki/lumatape/internal/render"
	"strings"
	"testing"
)

func TestMissingShaderRejectedBeforeNativeMutations(t *testing.T) {
	current := config.Default()
	current.Mode = "full"
	current.Target = config.Target{Kind: "window", WindowTitle: "owned"}
	current.Capture.Transfer = config.TransferCompatibility
	owned := &capture.Capture{}
	a := &application{cfg: current, capture: owned, target: win32.Window{Handle: 123, PID: 456}, shaderDirectory: t.TempDir()}
	next := current
	next.Shader.ID = strings.Repeat("a", 64)
	next.Aspect.Enabled = true
	next.Aspect.Method = "window"
	var failure *ShaderError
	if err := a.ApplyDraft(next); !errors.As(err, &failure) || failure.Code != "shader_asset_unavailable" {
		t.Fatalf("%v", err)
	}
	if a.cfg != current || a.capture != owned || a.target.Handle != 123 || a.windowRestore != nil || a.quit || a.unsaved {
		t.Fatal("failed shader changed working state")
	}
}
func TestDisableCustomDoesNotRequireMissingAsset(t *testing.T) {
	c := config.Default()
	c.Mode = "full"
	c.Shader.ID = strings.Repeat("b", 64)
	program := &render.ShaderProgram{ID: c.Shader.ID}
	a := &application{cfg: c, shaderProgram: program, shaderDirectory: t.TempDir()}
	c.Enabled = false
	got, owned, err := a.prepareLiveShader(c)
	if err != nil || owned || got != program {
		t.Fatalf("disable needs asset/compile: %v %v %v", got, owned, err)
	}
}
func TestResolvedAutoPreservesPreferenceAndCPUPacing(t *testing.T) {
	c := config.Default()
	c.Mode = "full"
	c.Capture.Transfer = config.TransferAuto
	a := &application{cfg: c, resolvedTransfer: config.TransferCompatibility}
	resolved := a.resolvedConfig()
	if !compatibilityCapture(resolved) || presentationInterval(resolved, 144) != presentationInterval(resolved, 30) || a.cfg != c {
		t.Fatal("autoCPU resolution mutated preference or omitted cap")
	}
	a.cfg.Capture.Transfer = config.TransferGPU
	if compatibilityCapture(a.resolvedConfig()) {
		t.Fatal("stale resolution overrode GPU pin")
	}
}
