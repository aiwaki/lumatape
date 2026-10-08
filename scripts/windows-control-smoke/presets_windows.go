//go:build windows

package main

import (
	"errors"
	"fmt"
	"strings"
)

// Presets qualify actual foreground Auto->CPU presentation without resizing the
// owned testcard. PNGs are separate synthetic shader previews, never WGC frames.
func runPresetsCase(p *peer, target source, hwnd uintptr, original rect, initial snapshot, helper, targetExe, dir string) (result error) {
	client, err := getRect(hwnd, true)
	if err != nil {
		return err
	}
	last := initial
	completed := make([]string, 0, 5)
	defer func() {
		result = errors.Join(result, stopAndQuit(p, target, hwnd, original, last))
		if result == nil {
			result = saveCapabilityEvidence(dir, "presets-summary", map[string]any{
				"presets": completed, "requested_backend": "full-auto", "actual_backend": "full-compatibility",
				"intensity": 1, "shape": "flat", "format_enabled": false, "exact_source_foreground_verified": true,
				"original_outer": original, "original_client": client, "geometry_unchanged": true,
				"original_client_4_3":                 (client.Right-client.Left)*3 == (client.Bottom-client.Top)*4,
				"png_origin":                          "synthetic shader preview at 320x240; NOT WGC/game screenshots",
				"emergency_restore_and_quit_verified": true,
			})
		}
	}()
	for _, name := range []string{"Subtle CRT", "CRT Classic", "Soft TV", "VHS Light", "VHS Tape"} {
		var effects map[string]any
		for _, preset := range initial.Presets {
			if preset.Name == name {
				if effects != nil {
					return fmt.Errorf("duplicate builtin preset %q", name)
				}
				effects = cloneMap(preset.Effects)
			}
		}
		if effects == nil {
			return fmt.Errorf("builtin preset %q missing from engine", name)
		}
		cfg := cloneMap(initial.Config)
		cfg["enabled"], cfg["mode"], cfg["input_mode"] = true, "full", "mouse-exact"
		cfg["target"] = map[string]any{"kind": "window", "window_title": target.Title, "monitor": 0}
		cfg["capture"] = map[string]any{"transfer": "auto"}
		cfg["hotkeys"] = map[string]any{"toggle": "Ctrl+Shift+9", "emergency": "Ctrl+Shift+0"}
		cfg["screen"].(map[string]any)["shape"] = "flat"
		cfg["aspect"] = map[string]any{"enabled": false, "method": "mask", "scale": "fit", "source_dar": 0}
		cfg["shader"] = map[string]any{"id": "", "params": [8]float64{}}
		effects["intensity"] = 1
		cfg["preset"], cfg["effects"] = name, effects
		reply, err := p.request("apply", map[string]any{"config": cfg, "source": target, "expected_emergency_sequence": last.EmergencySequence})
		if err != nil {
			return fmt.Errorf("%s apply: %w", name, err)
		}
		applied, err := decodeSnapshot(reply.Result)
		if err != nil {
			return err
		}
		last = applied
		if !equalConfig(cfg, applied.Config) || applied.Runtime.ShaderID != "" {
			return fmt.Errorf("%s did not preserve builtin Full Auto config: %+v", name, applied.Runtime)
		}
		slug := "preset-" + strings.ReplaceAll(strings.ToLower(name), " ", "-")
		if err = saveCapabilityEvidence(dir, slug+"-applied", applied); err != nil {
			return err
		}
		if _, err = runInputHelper(helper, targetExe, target.PID, "foreground", "", slug+"-foreground", dir); err != nil {
			return err
		}
		active, err := awaitInputState(p, hwnd, applied.Hotkeys.Toggle, applied.Hotkeys.Emergency, applied.EmergencySequence, true, false, slug+"-active", dir)
		if err != nil {
			return err
		}
		last = active
		if !equalConfig(cfg, last.Config) || last.Runtime.GPU.State != "unavailable" {
			return fmt.Errorf("%s active snapshot changed config or did not confirm unavailable GPU: %+v", name, last.Runtime)
		}
		outerNow, outerErr := getRect(hwnd, false)
		clientNow, clientErr := getRect(hwnd, true)
		if outerErr != nil || clientErr != nil || outerNow != original || clientNow != client {
			return errors.Join(fmt.Errorf("%s changed source geometry: outer=%+v client=%+v", name, outerNow, clientNow), outerErr, clientErr)
		}
		if len(completed) == 0 {
			if _, err = shaderPreview(p, cfg, true, "presets-before-synthetic-preview", dir); err != nil {
				return err
			}
		}
		if _, err = shaderPreview(p, cfg, false, slug+"-synthetic-preview", dir); err != nil {
			return err
		}
		completed = append(completed, name)
		emit("builtin_preset_active_and_preview_verified", map[string]any{"preset": name, "backend": last.Runtime.Backend,
			"phase": last.Runtime.Phase, "intensity": last.Runtime.EffectiveIntensity, "geometry_unchanged": true,
			"png": slug + "-synthetic-preview.png", "png_origin": "synthetic shader preview; NOT WGC/game screenshot"})
	}
	return nil
}
