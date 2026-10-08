//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func evidence(s snapshot, hwnd uintptr) (mutationEvidence, error) {
	outer, err := getRect(hwnd, false)
	if err != nil {
		return mutationEvidence{}, err
	}
	client, err := getRect(hwnd, true)
	return mutationEvidence{Config: s.Config, Sequence: s.EmergencySequence,
		RuntimeEnabled: s.Runtime.Enabled, FormatRequested: s.Runtime.FormatRequested,
		FormatActive: s.Runtime.FormatActive, RecoveryPending: s.Runtime.RecoveryPending,
		Outer: outer, Client: client}, err
}

func saveCapabilityEvidence(dir, name string, data any) error {
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name+".json"), encoded, 0600)
}

func rejectedGPU(p *peer, target source, hwnd uintptr, desired map[string]any, before snapshot, dir, name string) error {
	old, err := evidence(before, hwnd)
	if err != nil {
		return err
	}
	gpu := cloneMap(desired)
	gpu["capture"] = map[string]any{"transfer": "gpu"}
	reply, err := p.requestRaw("apply", map[string]any{"config": gpu, "source": target, "expected_emergency_sequence": before.EmergencySequence})
	if err != nil {
		return err
	}
	if err = saveCapabilityEvidence(dir, name+"-response", reply); err != nil {
		return err
	}
	if reply.OK {
		return errors.New("GPU unexpectedly accepted: unsupported-GPU fixture is not applicable")
	}
	var failure backendFailure
	if err = json.Unmarshal(reply.Error, &failure); err != nil {
		return err
	}
	after, err := decodeSnapshot(reply.Result)
	if err != nil {
		return err
	}
	actual, err := evidence(after, hwnd)
	if err != nil {
		return err
	}
	if err = verifyTypedGPURejection(failure, old, actual); err != nil {
		return err
	}
	emit(name+"_verified", map[string]any{"error": failure, "preserved": actual})
	return nil
}

func runCapabilitiesCase(p *peer, target source, hwnd uintptr, original rect, initial snapshot, cfg map[string]any, dir string) error {
	if err := saveCapabilityEvidence(dir, "startup-snapshot", initial); err != nil {
		return err
	}
	if err := verifyInitialGPUCapability(initial.Runtime.GPU, initial.Runtime.Compatibility, initial.Runtime.Enabled); err != nil {
		return err
	}
	if initial.Config["enabled"] != false || initial.Runtime.Backend != "" || initial.Runtime.FormatRequested || initial.Runtime.FormatActive || initial.Runtime.RecoveryPending {
		return errors.New("capabilities fixture did not start disabled without a backend or format mutation")
	}
	emit("startup_gpu_unavailable_before_first_apply_verified", initial.Runtime.GPU)
	// This request deliberately includes window 4:3: a rejected GPU must fail
	// before changing even the original source's geometry or applied profile.
	if err := rejectedGPU(p, target, hwnd, cfg, initial, dir, "initial_gpu_rejection"); err != nil {
		return err
	}
	reply, err := p.request("apply", map[string]any{"config": cfg, "source": target, "expected_emergency_sequence": initial.EmergencySequence})
	if err != nil {
		return err
	}
	applied, err := decodeSnapshot(reply.Result)
	if err != nil {
		return err
	}
	formatted, err := evidence(applied, hwnd)
	if err != nil {
		return err
	}
	if !equalConfig(cfg, applied.Config) || !formatted.RuntimeEnabled || !formatted.FormatActive || !formatted.FormatRequested || formatted.RecoveryPending || applied.Runtime.Compatibility.State != "available" ||
		formatted.Client.Right <= formatted.Client.Left || formatted.Client.Bottom <= formatted.Client.Top ||
		(formatted.Client.Right-formatted.Client.Left)*3 != (formatted.Client.Bottom-formatted.Client.Top)*4 {
		return fmt.Errorf("explicit CPU Apply did not confirm requested config, open and window 4:3: %+v", formatted)
	}
	if err = saveCapabilityEvidence(dir, "explicit-cpu-snapshot", applied); err != nil {
		return err
	}
	emit("explicit_cpu_apply_after_gpu_rejection_verified", map[string]any{"state": formatted, "cpu": applied.Runtime.Compatibility})
	if err = rejectedGPU(p, target, hwnd, cfg, applied, dir, "working_cpu_gpu_rejection"); err != nil {
		return err
	}
	for i := 1; i <= 2; i++ {
		disabled, err := p.request("disable", map[string]any{})
		if err != nil {
			return err
		}
		current, err := decodeSnapshot(disabled.Result)
		if err != nil {
			return err
		}
		actual, err := evidence(current, hwnd)
		if err != nil {
			return err
		}
		if err = saveCapabilityEvidence(dir, fmt.Sprintf("disable-%d-snapshot", i), current); err != nil {
			return err
		}
		if err = verifyDisabledPreservesFormat(formatted, actual); err != nil {
			return fmt.Errorf("disable #%d: %w", i, err)
		}
		emit("idempotent_disable_preserved_format_verified", map[string]any{"iteration": i, "state": actual})
	}
	// No foreground or input injection: this case qualifies Open/Apply/state
	// and native RECT. Visible Full/bypass pixels use the separate input case.
	return stopAndQuit(p, target, hwnd, original, applied)
}
