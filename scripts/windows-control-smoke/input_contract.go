package main

import "fmt"

// These assertions are shared with portable tests. Input success means the
// engine actually received each hotkey and reached the corresponding state;
// successful SendInput/registration alone cannot satisfy the contract.
type inputObservation struct {
	Registered, ConfigEnabled, RuntimeEnabled, FormatRequested, FormatActive, RecoveryPending bool
	Toggle, Emergency, Sequence                                                               uint64
	Backend, Phase                                                                            string
	EffectiveIntensity                                                                        *float64
}

func verifyInputObservation(got inputObservation, toggle, emergency, sequence uint64, enabled, formatted bool) error {
	if !got.Registered || got.Toggle != toggle || got.Emergency != emergency || got.Sequence != sequence {
		return fmt.Errorf("hotkey receipt/sequence mismatch: got=%+v want toggle=%d emergency=%d sequence=%d", got, toggle, emergency, sequence)
	}
	if got.ConfigEnabled != enabled || got.RuntimeEnabled != enabled || got.FormatRequested != formatted || got.FormatActive != formatted || got.RecoveryPending {
		return fmt.Errorf("requested/actual input transition mismatch: got=%+v enabled=%v formatted=%v", got, enabled, formatted)
	}
	// The filter and format are independent. With window 4:3 still requested,
	// Full presents an unmodified, flat image in bypass. Requiring a hidden
	// backend here could only accept the transient hide before its next frame.
	wantBackend, wantPhase, wantIntensity := "", "disabled", float64(0)
	if enabled || formatted {
		wantBackend = "full-compatibility"
	}
	if enabled {
		wantPhase, wantIntensity = "active", 1 // This fixture applies 100% intensity.
	} else if formatted {
		wantPhase = "bypass"
	}
	if got.Backend != wantBackend {
		return fmt.Errorf("actual backend %q, want %q", got.Backend, wantBackend)
	}
	if got.Phase != wantPhase || got.EffectiveIntensity == nil || *got.EffectiveIntensity != wantIntensity {
		return fmt.Errorf("actual effect state mismatch: phase=%q intensity=%v, want phase=%q intensity=%v", got.Phase, got.EffectiveIntensity, wantPhase, wantIntensity)
	}
	return nil
}
