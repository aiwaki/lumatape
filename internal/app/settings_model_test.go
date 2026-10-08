package app

import (
	"testing"

	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/geometry"
)

func TestDraftPresetPreservesIndependentControls(t *testing.T) {
	draft := config.Default()
	draft.Mode = "full"
	draft.Enabled = false
	draft.Target = config.Target{Kind: "window", WindowTitle: "Unsaved game selection"}
	draft.Capture.Transfer = config.TransferCompatibility
	draft.InputMode = "keyboard-gamepad"
	draft.Screen = config.Screen{Shape: config.ShapeConvex, CornerRadius: .075, Curvature: .42, Glass: .29}
	draft.Aspect = config.Aspect{Enabled: true, Method: "system", Scale: geometry.Crop, SourceDAR: 4.0 / 3.0}
	draft.Hotkeys = config.Hotkeys{Toggle: "Ctrl+Shift+9", Emergency: "Ctrl+Shift+0"}
	draft.Effects.Intensity = .37
	draft.Effects.NoiseSeed = 0xabcdef
	draft.Effects.FreezeNoise = true
	for _, name := range config.PresetNames {
		t.Run(name, func(t *testing.T) {
			got, err := draftWithPreset(draft, name)
			if err != nil {
				t.Fatal(err)
			}
			effects, err := config.Preset(name)
			if err != nil {
				t.Fatal(err)
			}
			want := draft
			want.Preset = name
			want.Effects.CRT, want.Effects.VHS = effects.CRT, effects.VHS
			// Compare the whole config so future independent fields are protected
			// too, rather than only checking the currently reported Screen bug.
			if got != want {
				t.Fatalf("preset changed independent draft controls:\ngot  %+v\nwant %+v", got, want)
			}
			if got.Effective().Intensity != 0 {
				t.Fatal("preset enabled a disabled filter")
			}
		})
	}
}

func TestDraftPresetRetainsZeroIntensityAndIncompleteSource(t *testing.T) {
	draft := config.Default()
	draft.Target = config.Target{} // source can be selected after the effect
	draft.Screen.Shape = config.ShapeRounded
	draft.Effects.Intensity = 0
	got, err := draftWithPreset(draft, "VHS Tape")
	if err != nil {
		t.Fatal("preset change must accept an incomplete draft:", err)
	}
	if got.Effects.Intensity != 0 || got.Effective().Intensity != 0 || got.EffectiveScreen().Shape != config.ShapeFlat {
		t.Fatal("selecting Tape broke zero-intensity bypass")
	}
	if got.Target != draft.Target || got.Screen != draft.Screen || !got.Enabled {
		t.Fatal("preset supplied a source or changed independent screen/enabled preferences")
	}
}

func TestDraftPresetCustomAndUnknown(t *testing.T) {
	draft := config.Default()
	draft.Effects.CRT.Scanlines = .431
	draft.Screen.CornerRadius = .09
	got, err := draftWithPreset(draft, "Custom")
	if err != nil {
		t.Fatal(err)
	}
	want := draft
	want.Preset = "Custom"
	if got != want {
		t.Fatal("choosing Custom replaced manual values")
	}
	for _, name := range []string{"", "VHS missing"} {
		got, err = draftWithPreset(draft, name)
		if err == nil || got != draft {
			t.Fatal("unknown preset must fail without changing the draft")
		}
	}
}

func TestDraftSignalControlMapping(t *testing.T) {
	effects := config.Effects{
		Intensity: .01, NoiseSeed: 19, FreezeNoise: true,
		CRT: config.CRT{Scanlines: .11, Mask: .22, Bloom: .33, Softness: .44, Vignette: .55, Curvature: .02},
		VHS: config.VHS{ChromaBleed: .66, Noise: .77, Jitter: .88, Tracking: .99},
	}
	if got, want := draftSignalValues(effects), [9]float64{.11, .22, .33, .44, .55, .66, .77, .88, .99}; got != want {
		t.Fatalf("preset-controlled edit order changed: got %v, want %v", got, want)
	}
}
