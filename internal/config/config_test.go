package config

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultsAndPresets(t *testing.T) {
	c := Default()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.Mode != "overlay" || c.InputMode != "mouse-exact" || c.Aspect.Enabled {
		t.Fatalf("unexpected first-run configuration: %+v", c)
	}
	for _, name := range PresetNames {
		e, err := Preset(name)
		if err != nil {
			t.Fatal(err)
		}
		c.Preset, c.Effects = name, e
		if err = c.Validate(); err != nil {
			t.Fatal(err)
		}
		if e.CRT.Curvature != 0 || e.VHS.Tracking != 0 || e.VHS.Jitter != 0 {
			t.Fatalf("default preset %q moves image geometry", name)
		}
	}
	pure, _ := Preset("Subtle CRT")
	if pure.VHS != (VHS{}) {
		t.Fatal("pure CRT should not imply VHS tape damage")
	}
	if _, err := Preset("fake preset"); err == nil {
		t.Fatal("accepted unknown preset")
	}
}

func TestCapturePreferenceKeepsLegacyDefaultsAndExplicitPins(t *testing.T) {
	if c, err := Decode([]byte(`{"capture":{"transfer":"auto"}}`)); err != nil || c.Capture.Transfer != TransferAuto {
		t.Fatalf("explicit auto: %+v %v", c.Capture, err)
	}
	for _, input := range []string{`{}`, `{"mode":"full","target":{"kind":"window","window_title":"test"}}`, `{"capture":{}}`} {
		c, err := Decode([]byte(input))
		if err != nil || c.Capture.Transfer != TransferGPU {
			t.Fatalf("legacy/minimal settings must default to GPU: %s: %+v %v", input, c.Capture, err)
		}
	}
	c, err := Decode([]byte(`{"mode":"full","target":{"kind":"window","window_title":"test"},"capture":{"transfer":"compatibility"}}`))
	if err != nil || c.Capture.Transfer != TransferCompatibility {
		t.Fatalf("explicit compatibility rejected: %+v %v", c.Capture, err)
	}
	for _, input := range []string{`{"capture":{"transfer":"cpu"}}`, `{"capture":{"transfer":""}}`, `{"capture":null}`} {
		if _, err := Decode([]byte(input)); err == nil {
			t.Fatalf("invalid or implicit fallback choice accepted: %s", input)
		}
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil || loaded != c {
		t.Fatalf("explicit choice lost in save/load: %+v %v", loaded.Capture, err)
	}
}

func TestVHSTapePresetAndExactMouse(t *testing.T) {
	c, err := Decode([]byte(`{"preset":"VHS Tape","mode":"full","target":{"kind":"window","window_title":"test"},"capture":{"transfer":"compatibility"},"effects":{"noise_seed":173}}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Preset != "VHS Tape" || c.Mode != "full" || c.Capture.Transfer != TransferCompatibility || c.InputMode != "mouse-exact" || c.Aspect.Enabled {
		t.Fatalf("demo lost explicit Full compatibility/exact-mouse choices: %+v", c)
	}
	preset, _ := Preset(c.Preset)
	preset.NoiseSeed = c.Effects.NoiseSeed
	if c.Effects != preset || c.Effects.FreezeNoise {
		t.Fatalf("demo must inherit the animated tape preset: %+v", c.Effects)
	}
	e := c.Effective()
	if e.VHS.ChromaBleed < .8 || e.VHS.Noise < .5 || e.CRT.Curvature != 0 || e.VHS.Jitter != 0 || e.VHS.Tracking != 0 {
		t.Fatalf("visible tape must preserve exact mouse geometry: %+v", e)
	}
	c.Effects.Intensity = 0
	if e = c.Effective(); e.CRT != (CRT{}) || e.VHS != (VHS{}) {
		t.Fatalf("tape bypass retained an effect: %+v", e)
	}
}

func TestIntensityAndExactMouse(t *testing.T) {
	c := Default()
	c.Aspect.Enabled = true
	c.InputMode = "keyboard-gamepad"
	c.Effects.CRT.Curvature = 0.4
	c.Effects.VHS.Jitter, c.Effects.VHS.Tracking = 0.2, 0.3
	c.Effects.Intensity = 0
	e := c.Effective()
	if e.CRT != (CRT{}) || e.VHS != (VHS{}) || !c.Aspect.Enabled {
		t.Fatalf("zero intensity must remove all effects and retain format: %+v", e)
	}
	c.Effects.Intensity = 0.5
	e = c.Effective()
	if e.CRT.Curvature != 0.2 || e.VHS.Jitter != 0.1 || e.VHS.Tracking != 0.15 {
		t.Fatalf("independent strengths not multiplied exactly once: %+v", e)
	}
	c.InputMode = "mouse-exact"
	e = c.Effective()
	if e.CRT.Curvature != 0 || e.VHS.Jitter != 0 || e.VHS.Tracking != 0 || e.CRT.Scanlines == 0 {
		t.Fatalf("exact mouse should disable geometric effects only: %+v", e)
	}
	if c.Effects.CRT.Curvature != 0.4 {
		t.Fatal("Effective mutated saved preferences")
	}
	c.Enabled = false
	e = c.Effective()
	if e.Intensity != 0 || e.CRT != (CRT{}) || e.VHS != (VHS{}) {
		t.Fatal("toggle off left an effect active")
	}
}

func TestStrictDecode(t *testing.T) {
	for _, data := range []string{
		``, `[]`, `null`, `{`, `{}` + `{}`,
		`{"version":2}`, `{"unknown":true}`, `{"enabled":null}`,
		`{"effects":null}`, `{"effects":{"crt":{"scanlines":null}}}`,
		`{"mode":"overlay","mode":"full"}`, `{"mode":"overlay","Mode":"full"}`,
		`{"effects":{"intensity":1,"intensity":0}}`,
		`{"effects":{"intensity":-0.01}}`, `{"effects":{"intensity":1.01}}`,
		`{"effects":{"intensity":1e309}}`, `{"effects":{"intensity":"0.5"}}`,
		`{"effects":{"noise_seed":-1}}`, `{"effects":{"noise_seed":4294967296}}`,
		`{"target":{"kind":"window"}}`, `{"target":{"kind":"unknown"}}`,
		`{"target":{"monitor":-1}}`, `{"target":{"window_title":"test\u0000"}}`,
		`{"aspect":{"scale":"native"}}`, `{"aspect":{"method":"custom-mode"}}`,
		`{"aspect":{"source_dar":-1}}`, `{"mode":"exclusive"}`,
		`{"input_mode":"remap"}`, `{"hotkeys":{"emergency":"Alt+Ctrl+F9"}}`,
		`{"hotkeys":{"toggle":"Ctrl+F12"}}`, `{"effects":{"crt":[]}}`,
		strings.Repeat(" ", MaxFileSize+1),
	} {
		t.Run(data[:min(len(data), 65)], func(t *testing.T) {
			if _, err := Decode([]byte(data)); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
	c, err := Decode([]byte(`{"effects":{"intensity":0},"aspect":{"enabled":true}}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Effects.Intensity != 0 || !c.Aspect.Enabled || c.Hotkeys != Default().Hotkeys {
		t.Fatalf("defaults or zero-valued overrides lost: %+v", c)
	}
}

func TestValidateRejectsNonfiniteValues(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -0.01, 1.01} {
		c := Default()
		c.Effects.VHS.Tracking = value
		if err := c.Validate(); err == nil {
			t.Errorf("accepted invalid tracking strength %v", value)
		}
	}
}

func TestPresetAndPartialEffectOverrides(t *testing.T) {
	c, err := Decode([]byte(`{"preset":"Soft TV","effects":{"vhs":{"noise":0}}}`))
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := Preset("Soft TV")
	expected.VHS.Noise = 0
	if c.Effects != expected {
		t.Fatalf("preset defaults lost when applying explicit zero override: %+v", c.Effects)
	}
}

func TestSaveLoadAndPreservation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	c, err := Load(path)
	if err != nil || !reflect.DeepEqual(c, Default()) {
		t.Fatalf("missing config should use defaults: %v", err)
	}
	c.Target = Target{Kind: "window", WindowTitle: "Старая игра"}
	c.Effects.NoiseSeed = 17
	c.Effects.FreezeNoise = true
	if err = Save(path, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || !reflect.DeepEqual(got, c) {
		t.Fatalf("roundtrip mismatch: %+v %v", got, err)
	}
	c.Effects.Intensity = 0.3
	if err = Save(path, c); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	bad := c
	bad.Effects.Intensity = math.NaN()
	if err = Save(path, bad); err == nil {
		t.Fatal("invalid configuration was saved")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("failed save modified existing config")
	}
	temps, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".lumatape-config-*.tmp"))
	if len(temps) != 0 {
		t.Fatalf("save leaked temporary files: %v", temps)
	}
	if err = os.WriteFile(path, []byte(`{"enabled":`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(path); err == nil {
		t.Fatal("corrupted existing config silently reset")
	}
}

func TestSaveReplacementFailureDoesNotRemoveDestination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, Default()); err == nil {
		t.Fatal("replaced a directory as a config")
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		t.Fatal("failed save removed destination")
	}
	temps, _ := filepath.Glob(filepath.Join(dir, ".lumatape-config-*.tmp"))
	if len(temps) != 0 {
		t.Fatalf("failed save leaked temporary files: %v", temps)
	}
}

func FuzzDecode(f *testing.F) {
	data, _ := json.Marshal(Default())
	f.Add(data)
	f.Add([]byte(`{"effects":{"intensity":0}}`))
	f.Add([]byte(`{"hotkeys":null}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		c, err := Decode(data)
		if err != nil {
			return
		}
		if err := c.Validate(); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		roundtrip, err := Decode(encoded)
		if err != nil || !reflect.DeepEqual(roundtrip, c) {
			t.Fatalf("roundtrip failed: %v", err)
		}
	})
}
