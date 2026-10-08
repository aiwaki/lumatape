package app

import (
	"encoding/json"
	"testing"

	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/control"
)

func TestDelayedApplyCannotUndoEmergency(t *testing.T) {
	var barrier emergencyBarrier
	state := config.Default()
	state.Target.Kind, state.Target.WindowTitle = "window", "game"
	state.Enabled, state.Aspect.Enabled = true, true
	state.Aspect.Method = "window"
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	// Serialize while the old UI draft is valid, but deliberately delay its
	// delivery until emergency has already cleared the transport queue.
	payload, err := json.Marshal(map[string]any{"config": state, "expected_emergency_sequence": barrier.sequence})
	if err != nil {
		t.Fatal(err)
	}
	barrier.advance()
	state.Enabled, state.Aspect.Enabled = false, false
	afterStop := state
	var delayed struct {
		Config   config.Config `json:"config"`
		Expected *uint64       `json:"expected_emergency_sequence"`
	}
	if err := control.DecodePayload(payload, &delayed); err != nil {
		t.Fatal(err)
	}
	mutations := 0
	if err := barrier.check(delayed.Expected); err == nil {
		mutations++
		state = delayed.Config
	}
	if mutations != 0 || state != afterStop {
		t.Fatal("a request delivered after queue cancellation undid emergency")
	}
	// A new explicit action based on the authoritative stopped snapshot works.
	current := barrier.sequence
	if err := barrier.check(&current); err != nil {
		t.Fatal(err)
	}
}

func TestEmergencyBarrierRequiresCurrentSequenceEvenAfterFailedCleanup(t *testing.T) {
	var barrier emergencyBarrier
	if err := barrier.check(nil); err == nil {
		t.Fatal("missing sequence accepted")
	}
	for attempt := uint64(1); attempt <= 3; attempt++ {
		previous := barrier.sequence
		barrier.advance() // cleanup failure must not roll this intent back
		if barrier.sequence != attempt {
			t.Fatalf("sequence=%d", barrier.sequence)
		}
		if err := barrier.check(&previous); err == nil {
			t.Fatal("old sequence accepted")
		}
	}
}

func TestApplyConfigCASRejectsInterveningPreferenceChanges(t *testing.T) {
	var barrier emergencyBarrier
	original := validFullConfig()
	expected, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	sequence := barrier.sequence
	payload := control.ApplyPayload{ExpectedEmergencySequence: &sequence, ExpectedConfig: expected}
	changes := map[string]func(*config.Config){
		"hotkey toggle": func(c *config.Config) { c.Enabled = !c.Enabled },
		"preset":        func(c *config.Config) { c.Preset = "VHS Tape"; c.Effects, _ = config.Preset(c.Preset) },
		"source":        func(c *config.Config) { c.Target.WindowTitle = "other game" },
		"backend":       func(c *config.Config) { c.Capture.Transfer = config.TransferCompatibility },
		"format":        func(c *config.Config) { c.Aspect.Enabled = true; c.Aspect.Method = "window" },
		"shape":         func(c *config.Config) { c.Screen.Shape = config.ShapeRounded },
		"input":         func(c *config.Config) { c.InputMode = "keyboard-gamepad" },
		"hotkeys":       func(c *config.Config) { c.Hotkeys.Toggle = "Ctrl+Shift+9" },
		"saved params":  func(c *config.Config) { c.Effects.NoiseSeed++ },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			current := original
			change(&current)
			if err := current.Validate(); err != nil {
				t.Fatal(err)
			}
			before := current
			failure := barrier.checkApply(payload, current)
			if failure == nil || failure.Code != "stale_config" || failure.Applied || failure.Unsaved {
				t.Fatalf("intervening change not rejected before apply: %+v", failure)
			}
			if current != before || barrier.sequence != sequence {
				t.Fatal("CAS rejection changed authoritative state")
			}
		})
	}
	if failure := barrier.checkApply(payload, original); failure != nil {
		t.Fatalf("unchanged snapshot rejected: %+v", failure)
	}
	// An explicit new action with a new snapshot is accepted, without rebasing or
	// automatically replaying the action whose original precondition failed.
	current := original
	current.Enabled = false
	payload.ExpectedConfig, _ = json.Marshal(current)
	if failure := barrier.checkApply(payload, current); failure != nil {
		t.Fatalf("fresh explicit action rejected: %+v", failure)
	}
}

func TestApplyConfigCASCompatibilityAndValidation(t *testing.T) {
	var barrier emergencyBarrier
	current := validFullConfig()
	sequence := barrier.sequence
	legacy := control.ApplyPayload{ExpectedEmergencySequence: &sequence}
	if failure := barrier.checkApply(legacy, current); failure != nil {
		t.Fatalf("legacy client without expected_config rejected: %+v", failure)
	}
	for _, raw := range []string{"null", "[]", `{"mode":"full","mode":"overlay"}`, `{"extra":true}`, `{"enabled":null}`, `{"version":-1}`} {
		legacy.ExpectedConfig = json.RawMessage(raw)
		if failure := barrier.checkApply(legacy, current); failure == nil || failure.Code != "invalid_payload" {
			t.Fatalf("invalid expected_config %s accepted or misclassified: %+v", raw, failure)
		}
	}
	// Compare decoded settings, not JSON formatting or object key ordering.
	raw, _ := json.Marshal(current)
	var reordered map[string]any
	if err := json.Unmarshal(raw, &reordered); err != nil {
		t.Fatal(err)
	}
	legacy.ExpectedConfig, _ = json.MarshalIndent(reordered, "", "  ")
	if failure := barrier.checkApply(legacy, current); failure != nil {
		t.Fatalf("equivalent JSON snapshot rejected: %+v", failure)
	}
	barrier.advance()
	if failure := barrier.checkApply(legacy, current); failure == nil || failure.Code != "stale_emergency_sequence" {
		t.Fatalf("matching config bypassed emergency barrier: %+v", failure)
	}
	legacy.ExpectedConfig = json.RawMessage("null")
	if failure := barrier.checkApply(legacy, current); failure == nil || failure.Code != "stale_emergency_sequence" {
		t.Fatalf("emergency must be checked before config parsing: %+v", failure)
	}
}

func TestPreferenceEditsDoNotResolveAnotherSameTitleSource(t *testing.T) {
	current := config.Default()
	current.Target.Kind, current.Target.WindowTitle = "window", "closed game"
	current.Mode = "full"
	current.Aspect.Enabled, current.Aspect.Method = true, "window"
	next := current
	next.Effects.Intensity = .7
	next.Hotkeys.Toggle, next.Hotkeys.Emergency = "Ctrl+Shift+9", "Ctrl+Shift+0"
	plan, err := planTransition(current, next, true)
	if err != nil {
		t.Fatal(err)
	}
	if needsSourceResolution(plan, false) {
		t.Fatal("preference-only edit searches for a replacement game")
	}
	if !needsSourceResolution(plan, true) {
		t.Fatal("explicit source identity was ignored")
	}
	for _, mutate := range []func(*config.Config){
		func(c *config.Config) { c.Target.WindowTitle = "new game" },
		func(c *config.Config) { c.Capture.Transfer = config.TransferCompatibility },
		func(c *config.Config) { c.Aspect.Method = "system" },
	} {
		next = current
		mutate(&next)
		plan, err = planTransition(current, next, true)
		if err != nil {
			t.Fatal(err)
		}
		if !needsSourceResolution(plan, false) {
			t.Fatal("native transition lost live source validation")
		}
	}
}
