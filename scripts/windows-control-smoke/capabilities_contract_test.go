package main

import (
	"encoding/json"
	"testing"
)

func fixtureEvidence(t *testing.T) mutationEvidence {
	t.Helper()
	var cfg map[string]any
	if err := json.Unmarshal([]byte(`{"enabled":true,"mode":"full","capture":{"transfer":"compatibility"},"screen":{"shape":"rounded","glass":0.15},"effects":{"intensity":1},"aspect":{"enabled":true,"method":"window","scale":"fit"}}`), &cfg); err != nil {
		t.Fatal(err)
	}
	return mutationEvidence{Config: cfg, Sequence: 3, RuntimeEnabled: true, FormatRequested: true, FormatActive: true,
		Outer: rect{10, 20, 998, 791}, Client: rect{0, 0, 960, 720}}
}

func TestStartupCapabilityRequiresKnownUnavailableBeforeAnyCPUClaim(t *testing.T) {
	gpu := capabilityStatus{State: "unavailable", Code: "gpu_interop_unavailable", Reason: "Use explicit CPU compatibility"}
	cpu := capabilityStatus{State: "unknown"}
	if err := verifyInitialGPUCapability(gpu, cpu, false); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []capabilityStatus{{}, {State: "unknown"}, {State: "failed", Code: gpu.Code, Reason: gpu.Reason}, {State: gpu.State, Reason: gpu.Reason}, {State: gpu.State, Code: gpu.Code}} {
		if verifyInitialGPUCapability(bad, cpu, false) == nil {
			t.Fatalf("accepted insufficient startup capability: %+v", bad)
		}
	}
	for _, state := range []string{"available", "failed", "unavailable", ""} {
		if verifyInitialGPUCapability(gpu, capabilityStatus{State: state}, false) == nil {
			t.Fatalf("accepted unproved CPU capability %q", state)
		}
	}
	if verifyInitialGPUCapability(gpu, cpu, true) == nil {
		t.Fatal("accepted an already enabled startup as fresh preflight evidence")
	}
}

func TestGPUFailureRequiresTypedGuidanceAndUntouchedAppliedState(t *testing.T) {
	before := fixtureEvidence(t)
	failure := backendFailure{Code: "gpu_interop_unavailable", Message: "GPU unavailable; explicitly choose CPU", SuggestedBackend: "full-compatibility"}
	if err := verifyTypedGPURejection(failure, before, fixtureEvidence(t)); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*backendFailure){
		func(f *backendFailure) { f.Code = "apply_failed" },
		func(f *backendFailure) { f.Message = "" },
		func(f *backendFailure) { f.SuggestedBackend = "" },
		func(f *backendFailure) { f.Applied = true },
		func(f *backendFailure) { f.Unsaved = true },
	} {
		bad := failure
		change(&bad)
		if verifyTypedGPURejection(bad, before, fixtureEvidence(t)) == nil {
			t.Fatalf("accepted wrong error contract %+v", bad)
		}
	}
	for _, change := range []func(*mutationEvidence){
		func(s *mutationEvidence) { s.Config["mode"] = "overlay" }, // hidden fallback
		func(s *mutationEvidence) { s.Sequence++ },
		func(s *mutationEvidence) { s.RuntimeEnabled = false },
		func(s *mutationEvidence) { s.FormatActive = false },
		func(s *mutationEvidence) { s.RecoveryPending = true },
		func(s *mutationEvidence) { s.Outer.Left++ },
		func(s *mutationEvidence) { s.Client.Bottom++ },
	} {
		bad := fixtureEvidence(t)
		change(&bad)
		if verifyTypedGPURejection(failure, before, bad) == nil {
			t.Fatalf("accepted mutated applied state %+v", bad)
		}
	}
}

func TestDisableIsIdempotentAndPreservesIndependentFormatAndShape(t *testing.T) {
	before := fixtureEvidence(t)
	disabled := func() mutationEvidence {
		s := fixtureEvidence(t)
		s.Config["enabled"], s.RuntimeEnabled = false, false
		return s
	}
	first := disabled()
	if err := verifyDisabledPreservesFormat(before, first); err != nil {
		t.Fatal(err)
	}
	if err := verifyDisabledPreservesFormat(first, disabled()); err != nil {
		t.Fatalf("repeated disable must remain disabled: %v", err)
	}
	for _, change := range []func(*mutationEvidence){
		func(s *mutationEvidence) { s.Config["enabled"] = true },
		func(s *mutationEvidence) { s.RuntimeEnabled = true },
		func(s *mutationEvidence) { s.Config["screen"].(map[string]any)["shape"] = "flat" },
		func(s *mutationEvidence) { s.Config["aspect"].(map[string]any)["enabled"] = false },
		func(s *mutationEvidence) { s.Sequence++ }, // STOP is different from disable
		func(s *mutationEvidence) { s.FormatRequested = false },
		func(s *mutationEvidence) { s.FormatActive = false },
		func(s *mutationEvidence) { s.RecoveryPending = true },
		func(s *mutationEvidence) { s.Outer.Right-- },
		func(s *mutationEvidence) { s.Client.Right-- },
	} {
		bad := disabled()
		change(&bad)
		if verifyDisabledPreservesFormat(first, bad) == nil {
			t.Fatalf("accepted invalid repeated disable: %+v", bad)
		}
	}
}
