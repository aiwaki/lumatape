package config

import (
	"encoding/json"
	"math"
	"testing"
)

func TestScreenDefaultsAndPresetIndependence(t *testing.T) {
	c, err := Decode([]byte(`{"preset":"Subtle CRT"}`))
	if err != nil || c.Screen.Shape != ShapeFlat || c.EffectiveScreen() != (Screen{Shape: ShapeFlat}) {
		t.Fatalf("old settings must remain flat: %+v %v", c.Screen, err)
	}
	c.Screen.Shape = ShapeRounded
	original := c.Screen
	for _, preset := range PresetNames {
		c.Effects, err = Preset(preset)
		if err != nil || c.Screen != original {
			t.Fatalf("preset altered screen: %s", preset)
		}
	}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Decode(data)
	if err != nil || loaded.Screen != original {
		t.Fatalf("screen roundtrip: %+v %v", loaded.Screen, err)
	}
}

func TestEffectiveScreenBypassAndInputSafety(t *testing.T) {
	c := Default()
	c.Screen.Shape = ShapeRounded
	c.Effects.Intensity = .5
	saved := c.Screen
	e := c.EffectiveScreen()
	if e.CornerRadius != saved.CornerRadius*.5 || e.Glass != saved.Glass*.5 || e.Curvature != 0 {
		t.Fatalf("rounded scale: %+v", e)
	}
	c.Screen.Shape = ShapeConvex
	if c.EffectiveScreen().Curvature != 0 {
		t.Fatal("unsupported convex can distort mouse input")
	}
	c.Mode, c.Target.Kind = "full", "window"
	if c.InputMode != "mouse-exact" || c.ValidateCombinations() != nil {
		t.Fatal("Full convex must allow automatic cursor projection for ordinary mouse input")
	}
	if c.EffectiveScreen().Curvature != saved.Curvature*.5 {
		t.Fatal("supported convex lost intensity")
	}
	c.Effects.Intensity = 0
	if c.EffectiveScreen() != (Screen{Shape: ShapeFlat}) {
		t.Fatal("zero intensity retained the screen")
	}
	c.Effects.Intensity, c.Enabled = 1, false
	if c.EffectiveScreen() != (Screen{Shape: ShapeFlat}) {
		t.Fatal("disabled retained the screen")
	}
	if c.Screen.CornerRadius != saved.CornerRadius {
		t.Fatal("effective screen changed saved settings")
	}
}

func TestScreenValidation(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), -.01, .201} {
		c := Default()
		c.Screen.CornerRadius = value
		if c.Validate() == nil {
			t.Fatalf("accepted invalid radius %v", value)
		}
	}
	for _, value := range []float64{math.NaN(), math.Inf(-1), -.01, 1.01} {
		c := Default()
		c.Screen.Glass = value
		if c.Validate() == nil {
			t.Fatalf("accepted invalid glass %v", value)
		}
		c = Default()
		c.Screen.Curvature = value
		if c.Validate() == nil {
			t.Fatalf("accepted invalid curve %v", value)
		}
	}
	c := Default()
	c.Screen.Shape = "unknown"
	if c.Validate() == nil {
		t.Fatal("accepted unknown shape")
	}
}
