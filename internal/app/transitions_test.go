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

func TestWindowResizeRetryRequiresExplicitIntentAndIsIdempotent(t *testing.T) {
	c := validFullConfig()
	c.Aspect.Enabled, c.Aspect.Method = true, "window"
	unchanged, err := planTransition(c, c, true)
	if err != nil || unchanged.ApplyWindow || unchanged.RestoreFormat {
		t.Fatalf("ordinary apply reasserted geometry: %+v %v", unchanged, err)
	}
	// A still-correct window must not visibly jump through original placement.
	active, err := explicitWindowReapply(unchanged, c, true)
	if err != nil || active != unchanged {
		t.Fatalf("repeated button click disturbed active 4:3: %+v %v", active, err)
	}
	// Once the game overrides the size, only the separate button intent retries.
	retry, err := explicitWindowReapply(unchanged, c, false)
	if err != nil || !retry.ApplyWindow || !retry.RestoreFormat || retry.ApplySystem {
		t.Fatalf("explicit resize did not recover an overridden format: %+v %v", retry, err)
	}
	for _, invalid := range []string{"off", "mask", "system", "monitor"} {
		next := c
		switch invalid {
		case "off":
			next.Aspect.Enabled = false
		case "monitor":
			next.Target.Kind = "monitor"
		default:
			next.Aspect.Method = invalid
		}
		if _, err := explicitWindowReapply(unchanged, next, false); err == nil {
			t.Fatalf("window-retry flag accepted %s", invalid)
		}
	}
}

func TestDynamicGameTitleDoesNotReplayWindowOrDisplayFormat(t *testing.T) {
	for _, method := range []string{"window", "system"} {
		current := validFullConfig()
		current.Target.WindowTitle = "Game — 60 FPS"
		current.Aspect.Enabled, current.Aspect.Method = true, method
		for _, preset := range config.PresetNames {
			next := current
			next.Target.WindowTitle, next.Preset = "Game — 59 FPS", preset
			next.Effects, _ = config.Preset(preset)
			for _, explicit := range []bool{false, true} {
				p, err := planResolvedSourceTransition(current, next, explicit, true)
				if err != nil || p.Source || p.Capture || p.RestoreFormat || p.ApplyWindow || p.ApplySystem {
					t.Fatalf("same window %s/%s title update replayed geometry: %+v %v", method, preset, p, err)
				}
			}
			p, err := planResolvedSourceTransition(current, next, true, false)
			if err != nil || !p.Source || !p.Capture || !p.RestoreFormat {
				t.Fatalf("different source identity was ignored: %+v %v", p, err)
			}
		}
		// Title normalization must not erase an independently requested format,
		// backend or hotkey change in the same draft.
		next := current
		next.Target.WindowTitle = "Game — new level"
		next.Aspect.Enabled = false
		next.Capture.Transfer = config.TransferCompatibility
		next.Hotkeys.Toggle = "Ctrl+Shift+9"
		p, err := planResolvedSourceTransition(current, next, true, true)
		if err != nil || p.Source || !p.Capture || !p.Hotkeys || !p.RestoreFormat || p.ApplyWindow || p.ApplySystem {
			t.Fatalf("title normalization lost another explicit change: %+v %v", p, err)
		}
	}
}
