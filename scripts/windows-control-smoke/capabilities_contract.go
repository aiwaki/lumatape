package main

import (
	"errors"
	"fmt"
	"reflect"
)

type rect struct{ Left, Top, Right, Bottom int32 }
type capabilityStatus struct {
	State  string `json:"state"`
	Code   string `json:"code"`
	Reason string `json:"reason"`
}
type backendFailure struct {
	Code             string `json:"code"`
	Message          string `json:"message"`
	SuggestedBackend string `json:"suggested_backend"`
	Applied          bool   `json:"applied"`
	Unsaved          bool   `json:"unsaved"`
}
type mutationEvidence struct {
	Config                                                         map[string]any
	Sequence                                                       uint64
	RuntimeEnabled, FormatRequested, FormatActive, RecoveryPending bool
	Outer, Client                                                  rect
}

// This is an explicit unsupported-GPU fixture, not a general GPU capability
// test. A fresh disabled profile must expose the reason before its first Apply.
func verifyInitialGPUCapability(gpu, cpu capabilityStatus, enabled bool) error {
	if enabled || gpu.State != "unavailable" || gpu.Code != "gpu_interop_unavailable" || gpu.Reason == "" {
		return fmt.Errorf("startup did not expose unavailable GPU before Apply: enabled=%v gpu=%+v", enabled, gpu)
	}
	if cpu.State != "unknown" {
		return fmt.Errorf("untested CPU path must remain unknown before explicit Apply, got %+v", cpu)
	}
	return nil
}

func verifyTypedGPURejection(failure backendFailure, before, after mutationEvidence) error {
	if failure.Code != "gpu_interop_unavailable" || failure.Message == "" || failure.SuggestedBackend != "full-compatibility" || failure.Applied || failure.Unsaved {
		return fmt.Errorf("GPU rejection lacks actionable typed CPU guidance: %+v", failure)
	}
	if !reflect.DeepEqual(before, after) {
		return errors.New("GPU rejection changed applied config, emergency sequence, runtime flags or exact window geometry")
	}
	return nil
}

func verifyDisabledPreservesFormat(before, after mutationEvidence) error {
	expected := make(map[string]any, len(before.Config))
	for key, value := range before.Config {
		expected[key] = value
	}
	expected["enabled"] = false
	if !reflect.DeepEqual(expected, after.Config) || after.RuntimeEnabled ||
		!after.FormatRequested || !after.FormatActive || after.RecoveryPending ||
		after.Sequence != before.Sequence || after.Outer != before.Outer || after.Client != before.Client {
		return errors.New("disable changed effects/shape/format, toggled filter back on, advanced emergency, or changed exact geometry")
	}
	return nil
}
