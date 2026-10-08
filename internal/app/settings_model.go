package app

import "github.com/aiwaki/lumatape/internal/config"

// draftWithPreset edits signal controls only. In particular, selecting a preset
// must not enable a disabled filter, raise a zero intensity, change screen
// geometry, or reset deterministic noise controls used for comparison.
// It accepts incomplete drafts; validation belongs to preview/apply, not this
// operation while a user may be halfway through typing another field.
func draftWithPreset(draft config.Config, name string) (config.Config, error) {
	if name == "Custom" {
		draft.Preset = name
		return draft, nil
	}
	effects, err := config.Preset(name)
	if err != nil {
		return draft, err
	}
	draft.Preset = name
	draft.Shader = config.ShaderConfig{}
	draft.Effects.CRT = effects.CRT
	draft.Effects.VHS = effects.VHS
	return draft, nil
}

// draftSignalValues is the complete preset-controlled UI range, in the same
// order as fields 4100..4108. Its fixed size deliberately excludes independent
// screen controls (4109..4111) and the global intensity edit.
func draftSignalValues(e config.Effects) [9]float64 {
	return [9]float64{e.CRT.Scanlines, e.CRT.Mask, e.CRT.Bloom, e.CRT.Softness, e.CRT.Vignette,
		e.VHS.ChromaBleed, e.VHS.Noise, e.VHS.Jitter, e.VHS.Tracking}
}
