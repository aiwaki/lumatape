//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aiwaki/lumatape/internal/shaderpack"
)

func readShaderExample(path string) (shaderpack.Pack, error) {
	f, err := os.Open(path)
	if err != nil {
		return shaderpack.Pack{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, shaderpack.MaxSourceBytes+1))
	if err != nil {
		return shaderpack.Pack{}, err
	}
	return shaderpack.Parse(string(data))
}

func importShader(p *peer, source string) (shaderpack.Descriptor, error) {
	var result struct {
		Shader shaderpack.Descriptor `json:"shader"`
	}
	reply, err := p.request("shader_import", map[string]any{"source": source})
	if err == nil {
		err = json.Unmarshal(reply.Result, &result)
	}
	if err == nil && !shaderpack.ValidID(result.Shader.ID) {
		err = errors.New("import omitted immutable shader ID")
	}
	return result.Shader, err
}

func shaderLibrary(p *peer) ([]shaderpack.Descriptor, error) {
	var result struct {
		Items   []shaderpack.Descriptor `json:"items"`
		Warning string                  `json:"warning"`
	}
	reply, err := p.request("shaders", map[string]any{})
	if err == nil {
		err = json.Unmarshal(reply.Result, &result)
	}
	if err == nil && result.Warning != "" {
		err = fmt.Errorf("shader library warning: %s", result.Warning)
	}
	return result.Items, err
}

func currentSnapshot(p *peer) (snapshot, error) {
	reply, err := p.request("snapshot", map[string]any{})
	if err != nil {
		return snapshot{}, err
	}
	return decodeSnapshot(reply.Result)
}

// An unchanged config alone cannot establish GL ownership: runtime.shader_id
// comes from the actual live program, separate from the preview program slot.
func shaderUnchanged(p *peer, before snapshot, hwnd uintptr, outer rect, name, dir string) error {
	after, err := currentSnapshot(p)
	if err != nil {
		return err
	}
	actual, err := getRect(hwnd, false)
	if err != nil {
		return err
	}
	if !equalConfig(before.Config, after.Config) || before.EmergencySequence != after.EmergencySequence ||
		after.Runtime.ShaderID != before.Runtime.ShaderID || after.Runtime.Enabled != before.Runtime.Enabled ||
		after.Runtime.FormatActive != before.Runtime.FormatActive || after.Runtime.RecoveryPending || actual != outer {
		return fmt.Errorf("%s changed live config/program/format/geometry: before shader=%s after shader=%s rect=%+v", name, before.Runtime.ShaderID, after.Runtime.ShaderID, actual)
	}
	if err = saveCapabilityEvidence(dir, name+"-snapshot", after); err != nil {
		return err
	}
	emit(name+"_preserved_live_state_verified", map[string]any{"shader_id": after.Runtime.ShaderID, "rect": actual, "sequence": after.EmergencySequence})
	return nil
}

func shaderPreview(p *peer, cfg map[string]any, before bool, name, dir string) ([]byte, error) {
	// Server owns the rate limit; every request remains below two per second.
	time.Sleep(550 * time.Millisecond)
	reply, err := p.request("preview", map[string]any{"config": cfg, "width": 320, "height": 240, "time": 1, "before": before})
	if err != nil {
		return nil, err
	}
	pixels, encoded, err := shaderPreviewPixels(reply.Result, 320, 240)
	if err != nil {
		return nil, err
	}
	if err = os.WriteFile(filepath.Join(dir, name+".png"), encoded, 0600); err != nil {
		return nil, err
	}
	emit("custom_shader_preview_saved", map[string]any{"file": name + ".png", "width": 320, "height": 240, "alpha_opaque": true})
	return pixels, nil
}

func shaderChoice(pack shaderpack.Pack) map[string]any {
	return map[string]any{"id": pack.ID, "params": pack.Defaults()}
}

func rejectedShaderApply(p *peer, target source, hwnd uintptr, outer rect, before snapshot, cfg map[string]any, name, dir string) error {
	reply, err := p.requestRaw("apply", map[string]any{"config": cfg, "source": target, "expected_emergency_sequence": before.EmergencySequence})
	if err != nil {
		return err
	}
	var failure struct {
		Code    string `json:"code"`
		Applied bool   `json:"applied"`
		Unsaved bool   `json:"unsaved"`
	}
	if err = json.Unmarshal(reply.Error, &failure); err != nil {
		return err
	}
	if reply.OK || failure.Code != "shader_validation" || failure.Applied || failure.Unsaved {
		return fmt.Errorf("%s expected atomic shader_validation, got %s", name, reply.Error)
	}
	actual, err := decodeSnapshot(reply.Result)
	if err != nil {
		return err
	}
	if !equalConfig(before.Config, actual.Config) || actual.Runtime.ShaderID != before.Runtime.ShaderID {
		return fmt.Errorf("%s reply lost authoritative live state", name)
	}
	if err = saveCapabilityEvidence(dir, name+"-response", reply); err != nil {
		return err
	}
	return shaderUnchanged(p, before, hwnd, outer, name, dir)
}

func runShadersCase(p *peer, target source, hwnd uintptr, original rect, initial snapshot, cfg map[string]any, amberPath, coldPath, inputHelper, targetExe, dir string) error {
	if err := verifyInitialGPUCapability(initial.Runtime.GPU, initial.Runtime.Compatibility, initial.Runtime.Enabled); err != nil {
		return err
	}
	amber, err := readShaderExample(amberPath)
	if err != nil {
		return err
	}
	cold, err := readShaderExample(coldPath)
	if err != nil {
		return err
	}
	if amber.Name != "Amber CRT" || cold.Name != "Cold Bleed" || amber.ID == cold.ID || len(amber.Parameters) != 2 {
		return errors.New("expected distinct current Amber CRT and Cold Bleed examples")
	}
	imported, err := importShader(p, amber.Source)
	if err != nil {
		return err
	}
	if imported.ID != amber.ID || imported.Name != amber.Name {
		return errors.New("import did not retain normalized immutable Amber identity")
	}
	duplicate, err := importShader(p, amber.Source)
	if err != nil {
		return err
	}
	if duplicate.ID != imported.ID {
		return errors.New("duplicate shader import changed identity")
	}
	items, err := shaderLibrary(p)
	if err != nil {
		return err
	}
	count := 0
	for _, item := range items {
		if item.ID == amber.ID {
			count++
		}
	}
	if count != 1 {
		return fmt.Errorf("duplicate import resulted in %d Amber entries", count)
	}
	fetched, err := p.request("shader_source", map[string]any{"id": amber.ID})
	if err != nil {
		return err
	}
	var sourceResult struct {
		Source string                `json:"source"`
		Shader shaderpack.Descriptor `json:"shader"`
	}
	if err = json.Unmarshal(fetched.Result, &sourceResult); err != nil {
		return err
	}
	if sourceResult.Source != amber.Source || sourceResult.Shader.ID != amber.ID {
		return errors.New("library source roundtrip differs")
	}
	if err = saveCapabilityEvidence(dir, "shader-library-after-duplicate", items); err != nil {
		return err
	}
	emit("shader_import_duplicate_library_source_verified", map[string]any{"shader": imported, "library_count": len(items)})
	// Auto is an explicit requested mode; this fixture proves its reported
	// resolution to compatibility only on a known unsupported-interop machine.
	cfg["capture"] = map[string]any{"transfer": "auto"}
	cfg["shader"] = shaderChoice(amber)
	reply, err := p.request("apply", map[string]any{"config": cfg, "source": target, "expected_emergency_sequence": initial.EmergencySequence})
	if err != nil {
		return err
	}
	applied, err := decodeSnapshot(reply.Result)
	if err != nil {
		return err
	}
	formatted, err := getRect(hwnd, false)
	if err != nil {
		return err
	}
	client, err := getRect(hwnd, true)
	if err != nil {
		return err
	}
	if !equalConfig(cfg, applied.Config) || applied.Runtime.ShaderID != amber.ID || applied.Runtime.ShaderName != amber.Name ||
		!applied.Runtime.Enabled || !applied.Runtime.FormatActive || applied.Runtime.RecoveryPending || applied.Runtime.Compatibility.State != "available" ||
		(client.Right-client.Left)*3 != (client.Bottom-client.Top)*4 {
		return fmt.Errorf("Auto CPU shader Apply/defaults/window4:3 not confirmed: %+v", applied.Runtime)
	}
	if err = saveCapabilityEvidence(dir, "amber-applied", applied); err != nil {
		return err
	}
	emit("shader_auto_cpu_apply_defaults_verified", map[string]any{"runtime": applied.Runtime, "config": applied.Config, "client": client})
	if inputHelper != "" {
		if _, err = runInputHelper(inputHelper, targetExe, target.PID, "foreground", "", "shader-source-foreground", dir); err != nil {
			return err
		}
		if _, err = awaitInputState(p, hwnd, applied.Hotkeys.Toggle, applied.Hotkeys.Emergency, applied.EmergencySequence, true, true, "shader-active-full", dir); err != nil {
			return err
		}
	}
	base, err := shaderPreview(p, applied.Config, true, "custom-before", dir)
	if err != nil {
		return err
	}
	zeroCfg := cloneMap(applied.Config)
	zeroCfg["effects"].(map[string]any)["intensity"] = 0
	zero, err := shaderPreview(p, zeroCfg, false, "custom-zero", dir)
	if err != nil {
		return err
	}
	full, err := shaderPreview(p, applied.Config, false, "custom-amber", dir)
	if err != nil {
		return err
	}
	mae, err := shaderPreviewDifference(base, zero, full)
	if err != nil {
		return err
	}
	emit("custom_shader_exact_zero_and_visible_full_verified", map[string]any{"rgb_mae_255": mae, "alpha_opaque": true, "dimensions": "320x240"})
	bad := amber.Source[:strings.Index(amber.Source, "*/")+2] + "\nvec3 lumatape(vec2 uv){return unknownFunction(uv);}\n"
	if _, err = shaderpack.Parse(bad); err != nil {
		return fmt.Errorf("invalid compile fixture failed before GLSL: %w", err)
	}
	rejected, err := p.requestRaw("shader_import", map[string]any{"source": bad})
	if err != nil {
		return err
	}
	var compilerFailure struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err = json.Unmarshal(rejected.Error, &compilerFailure); err != nil {
		return err
	}
	if rejected.OK || compilerFailure.Code != "shader_compile_failed" || compilerFailure.Message == "" {
		return fmt.Errorf("real GLSL compile rejection missing: %s", rejected.Error)
	}
	if err = saveCapabilityEvidence(dir, "invalid-compile-response", rejected); err != nil {
		return err
	}
	if err = shaderUnchanged(p, applied, hwnd, formatted, "invalid-compile", dir); err != nil {
		return err
	}
	afterItems, err := shaderLibrary(p)
	if err != nil {
		return err
	}
	if len(afterItems) != len(items) {
		return errors.New("rejected compile wrote a library asset")
	}
	if _, err = importShader(p, cold.Source); err != nil {
		return err
	}
	coldCfg := cloneMap(applied.Config)
	coldCfg["shader"] = shaderChoice(cold)
	coldPixels, err := shaderPreview(p, coldCfg, false, "custom-cold-preview-only", dir)
	if err != nil {
		return err
	}
	if bytes.Equal(coldPixels, full) {
		return errors.New("other shader preview did not render a different image")
	}
	if err = shaderUnchanged(p, applied, hwnd, formatted, "other-preview", dir); err != nil {
		return err
	}
	again, err := shaderPreview(p, applied.Config, false, "custom-amber-after-failure-and-preview", dir)
	if err != nil {
		return err
	}
	if !bytes.Equal(again, full) {
		return errors.New("Amber pixels changed after failed import/another preview")
	}
	badParams := cloneMap(applied.Config)
	badParams["shader"].(map[string]any)["params"].([]any)[0] = amber.Parameters[0].Max + 1
	if err = rejectedShaderApply(p, target, hwnd, formatted, applied, badParams, "out-of-range-parameter", dir); err != nil {
		return err
	}
	warpSource := "/* LumaTape\n{\"version\":1,\"name\":\"Warp smoke\",\"coordinates\":\"warp\",\"parameters\":[]}\n*/\nvec3 lumatape(vec2 uv){return ltSample(uv+vec2(.01,0));}\n"
	warp, err := shaderpack.Parse(warpSource)
	if err != nil {
		return err
	}
	if _, err = importShader(p, warp.Source); err != nil {
		return err
	}
	warpCfg := cloneMap(applied.Config)
	warpCfg["shader"] = shaderChoice(warp)
	if err = rejectedShaderApply(p, target, hwnd, formatted, applied, warpCfg, "warp-mouse-exact", dir); err != nil {
		return err
	}
	if err = saveCapabilityEvidence(dir, "shader-summary", map[string]any{"amber_id": amber.ID, "cold_id": cold.ID, "full_rgb_mae_255": mae, "exact_zero_bypass": true, "opaque_alpha": true, "real_gl_compile_rejected": true, "live_program_preserved": true, "auto_cpu_open": true, "foreground_full_verified": inputHelper != ""}); err != nil {
		return err
	}
	return stopAndQuit(p, target, hwnd, original, applied)
}
