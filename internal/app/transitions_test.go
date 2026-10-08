package app

import (
	"testing"

	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/geometry"
)

func validFullConfig() config.Config {
	c := config.Default()
	c.Mode = "full"
	c.Target = config.Target{Kind: "window", WindowTitle: "test"}
	return c
}

func TestPreferenceChangesNeverReplayOwnedGeometry(t *testing.T) {
	for _, method := range []string{"window", "system"} {
		current := validFullConfig()
		current.Aspect.Enabled, current.Aspect.Method = true, method
		for _, preset := range config.PresetNames {
			next := current
			next.Preset = preset
			next.Effects, _ = config.Preset(preset)
			for _, explicit := range []bool{false, true} {
				p, err := planTransition(current, next, explicit)
				if err != nil || p.RestoreFormat || p.ApplyWindow || p.ApplySystem || p.Capture {
					t.Fatalf("preset replayed OS mutation: %+v %v", p, err)
				}
			}
		}
	}
}

func TestFormatTransitionsHaveSingleExplicitMutation(t *testing.T) {
	for _, before := range []string{"off", "mask", "window", "system"} {
		for _, after := range []string{"off", "mask", "window", "system"} {
			current := validFullConfig()
			current.Aspect.Enabled = before != "off"
			if before != "off" {
				current.Aspect.Method = before
			}
			next := current
			next.Aspect.Enabled = after != "off"
			if after != "off" {
				next.Aspect.Method = after
			}
			p, err := planTransition(current, next, true)
			if err != nil {
				t.Fatal(err)
			}
			changed := before != after
			if p.RestoreFormat != changed || p.ApplyWindow != (changed && after == "window") || p.ApplySystem != (changed && after == "system") {
				t.Fatalf("%s -> %s: %+v", before, after, p)
			}
			reload, err := planTransition(current, next, false)
			if err != nil || reload.ApplyWindow || reload.ApplySystem {
				t.Fatalf("reload applied format: %+v %v", reload, err)
			}
		}
	}
}

func TestChangingFullTransferKeepsWindowFormatIndependent(t *testing.T) {
	current := validFullConfig()
	current.Aspect.Enabled, current.Aspect.Method = true, "window"
	next := current
	next.Capture.Transfer = config.TransferCompatibility
	p, err := planTransition(current, next, true)
	if err != nil || !p.Capture || p.RestoreFormat || p.ApplyWindow {
		t.Fatalf("backend change disturbed format: %+v %v", p, err)
	}
}

func TestInvalidInputTransitionDoesNotProduceAnActionPlan(t *testing.T) {
	c := validFullConfig()
	next := c
	next.Aspect.Scale = geometry.Crop
	if p, err := planTransition(c, next, true); err == nil || p != (transitionPlan{}) {
		t.Fatal("invalid exact-mouse transition produced mutation plan")
	}
	next = c
	next.Screen.Shape = "convex"
	if _, err := planTransition(c, next, true); err != nil {
		t.Fatalf("built-in convex must allow projected cursor without input-mode changes: %v", err)
	}
}
