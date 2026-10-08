package main

import (
	"math"
	"testing"
)

func intensity(value float64) *float64 { return &value }

func TestInputEvidenceRequiresReceiptAndActualState(t *testing.T) {
	good := inputObservation{Registered: true, ConfigEnabled: true, RuntimeEnabled: true, FormatRequested: true, FormatActive: true, Toggle: 2, Emergency: 0, Sequence: 0, Backend: "full-compatibility", Phase: "active", EffectiveIntensity: intensity(1)}
	if err := verifyInputObservation(good, 2, 0, 0, true, true); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*inputObservation){
		func(s *inputObservation) { s.Toggle = 1 },              // keys sent but never delivered
		func(s *inputObservation) { s.Toggle = 3 },              // duplicate delivery
		func(s *inputObservation) { s.Backend = "none" },        // configured but hidden
		func(s *inputObservation) { s.Backend = "lightweight" }, // silent fallback
		func(s *inputObservation) { s.RuntimeEnabled = false },
		func(s *inputObservation) { s.FormatActive = false },
		func(s *inputObservation) { s.Registered = false },
		func(s *inputObservation) { s.Sequence = 1 }, // out-of-band emergency
		func(s *inputObservation) { s.Phase = "bypass" },
		func(s *inputObservation) { s.EffectiveIntensity = intensity(0) },
		func(s *inputObservation) { s.EffectiveIntensity = nil },
	} {
		bad := good
		mutate(&bad)
		if verifyInputObservation(bad, 2, 0, 0, true, true) == nil {
			t.Fatalf("accepted insufficient input evidence: %+v", bad)
		}
	}
}

func TestPhysicalEmergencyRequiresReceiptSequenceAndRestoration(t *testing.T) {
	good := inputObservation{Registered: true, Toggle: 2, Emergency: 1, Sequence: 1, Phase: "disabled", EffectiveIntensity: intensity(0)}
	if err := verifyInputObservation(good, 2, 1, 1, false, false); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*inputObservation){
		func(s *inputObservation) { s.Emergency = 0 },
		func(s *inputObservation) { s.Sequence = 0 },
		func(s *inputObservation) { s.ConfigEnabled = true },
		func(s *inputObservation) { s.FormatRequested = true },
		func(s *inputObservation) { s.FormatActive = true },
		func(s *inputObservation) { s.RecoveryPending = true },
		func(s *inputObservation) { s.Backend = "full-compatibility" },
		func(s *inputObservation) { s.Phase = "bypass" },
		func(s *inputObservation) { s.EffectiveIntensity = intensity(1) },
	} {
		bad := good
		mutate(&bad)
		if verifyInputObservation(bad, 2, 1, 1, false, false) == nil {
			t.Fatalf("accepted incomplete physical emergency: %+v", bad)
		}
	}
}

func TestFilterOffKeepsWindowFormatButRequiresSettledBypass(t *testing.T) {
	// Regression from the real hidden-controller smoke: the first toggle was
	// received and Full correctly kept showing the independently formatted game.
	good := inputObservation{Registered: true, Toggle: 1, FormatRequested: true, FormatActive: true,
		Backend: "full-compatibility", Phase: "bypass", EffectiveIntensity: intensity(0)}
	if err := verifyInputObservation(good, 1, 0, 0, false, true); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*inputObservation){
		func(s *inputObservation) { s.ConfigEnabled = true },
		func(s *inputObservation) { s.RuntimeEnabled = true },
		func(s *inputObservation) { s.Toggle = 0 },
		func(s *inputObservation) { s.Toggle = 2 },
		func(s *inputObservation) { s.FormatRequested = false },
		func(s *inputObservation) { s.FormatActive = false },
		func(s *inputObservation) { s.Backend = "" }, // transient hidden state is not proof of bypass
		func(s *inputObservation) { s.Backend = "lightweight" },
		func(s *inputObservation) { s.Phase = "active" },
		func(s *inputObservation) { s.Phase = "ready" },
		func(s *inputObservation) { s.EffectiveIntensity = intensity(.01) },
		func(s *inputObservation) { s.EffectiveIntensity = intensity(math.NaN()) },
		func(s *inputObservation) { s.EffectiveIntensity = nil },
	} {
		bad := good
		mutate(&bad)
		if verifyInputObservation(bad, 1, 0, 0, false, true) == nil {
			t.Fatalf("accepted incomplete filter-off evidence: %+v", bad)
		}
	}
}
