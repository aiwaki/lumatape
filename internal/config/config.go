// Package config owns validated, versioned settings. Transient HWNDs and system
// video-mode recovery state deliberately do not belong in a preferences file.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/aiwaki/lumatape/internal/geometry"
	"github.com/aiwaki/lumatape/internal/locale"
)

const Version = 1
const MaxFileSize = 64 << 10

type Config struct {
	Version   int             `json:"version"`
	Mode      string          `json:"mode"`
	Enabled   bool            `json:"enabled"`
	Target    Target          `json:"target"`
	Capture   CaptureSettings `json:"capture"`
	Preset    string          `json:"preset"`
	Shader    ShaderConfig    `json:"shader"`
	Effects   Effects         `json:"effects"`
	Screen    Screen          `json:"screen"`
	Aspect    Aspect          `json:"aspect"`
	InputMode string          `json:"input_mode"`
	Hotkeys   Hotkeys         `json:"hotkeys"`
}

const (
	TransferAuto          = "auto"
	TransferGPU           = "gpu"
	TransferCompatibility = "compatibility"
)

// CaptureSettings preserves the user preference. Auto may resolve to CPU only
// after a classified GPU interoperability failure; explicit paths stay pinned.
type CaptureSettings struct {
	Transfer string `json:"transfer"`
}

type Target struct {
	Kind        string `json:"kind"`
	Monitor     int    `json:"monitor"`
	WindowTitle string `json:"window_title"`
}

type Aspect struct {
	Enabled   bool               `json:"enabled"`
	Method    string             `json:"method"`
	Scale     geometry.ScaleMode `json:"scale"`
	SourceDAR float64            `json:"source_dar"`
}

type Effects struct {
	Intensity   float64 `json:"intensity"`
	CRT         CRT     `json:"crt"`
	VHS         VHS     `json:"vhs"`
	NoiseSeed   uint32  `json:"noise_seed"`
	FreezeNoise bool    `json:"freeze_noise"`
}

type CRT struct {
	Scanlines float64 `json:"scanlines"`
	Mask      float64 `json:"mask"`
	Bloom     float64 `json:"bloom"`
	Softness  float64 `json:"softness"`
	Vignette  float64 `json:"vignette"`
	Curvature float64 `json:"curvature"`
}

type VHS struct {
	ChromaBleed float64 `json:"chroma_bleed"`
	Noise       float64 `json:"noise"`
	Jitter      float64 `json:"jitter"`
	Tracking    float64 `json:"tracking"`
}

const (
	ShapeFlat    = "flat"
	ShapeRounded = "rounded"
	ShapeConvex  = "convex"
)

var ScreenShapes = []string{ShapeFlat, ShapeRounded, ShapeConvex}

// Screen styles the display independently of the signal preset. CornerRadius
// is a fraction of the shorter physical output side, not UV axes or desktop DPI.
type Screen struct {
	Shape        string  `json:"shape"`
	CornerRadius float64 `json:"corner_radius"`
	Curvature    float64 `json:"curvature"`
	Glass        float64 `json:"glass"`
}

type Hotkeys struct {
	Toggle    string `json:"toggle"`
	Emergency string `json:"emergency"`
}

func Default() Config {
	effects, _ := Preset("Subtle CRT")
	return Config{
		Version: Version, Mode: "overlay", Enabled: true,
		Target:  Target{Kind: "monitor", Monitor: 0},
		Capture: CaptureSettings{Transfer: TransferGPU},
		Preset:  "Subtle CRT", Effects: effects,
		Screen:    Screen{Shape: ShapeFlat, CornerRadius: .04, Curvature: .18, Glass: .15},
		Aspect:    Aspect{Method: "mask", Scale: geometry.Fit},
		InputMode: "mouse-exact",
		Hotkeys:   Hotkeys{Toggle: "Ctrl+Alt+F9", Emergency: "Ctrl+Alt+F10"},
	}
}

var PresetNames = []string{"Subtle CRT", "CRT Classic", "Soft TV", "VHS Light", "VHS Tape"}

// Preset changes effects only. Display format and source selection are
// independent; selecting a preset must not silently resize a running game.
func Preset(name string) (Effects, error) {
	e := Effects{Intensity: 1, NoiseSeed: 1}
	switch name {
	case "Subtle CRT":
		e.CRT = CRT{Scanlines: 0.12, Mask: 0.04, Bloom: 0.06, Softness: 0.08, Vignette: 0.10}
	case "CRT Classic":
		e.CRT = CRT{Scanlines: .62, Mask: .42, Bloom: .18, Softness: .10, Vignette: .24}
	case "Soft TV":
		e.CRT = CRT{Scanlines: .16, Mask: .025, Bloom: .44, Softness: .76, Vignette: .18}
		e.VHS = VHS{ChromaBleed: .12, Noise: .01}
	case "VHS Light":
		e.CRT = CRT{Scanlines: .14, Mask: .02, Bloom: .10, Softness: .20, Vignette: .14}
		e.VHS = VHS{ChromaBleed: .50, Noise: .24}
	case "VHS Tape":
		// A visibly worn tape, still safe for exact mouse coordinates. Strong
		// chroma/noise controls engage the wider signal-bandwidth effects.
		e.CRT = CRT{Scanlines: 0.10, Mask: 0.015, Bloom: 0.08, Softness: 0.28, Vignette: 0.16}
		e.VHS = VHS{ChromaBleed: 0.92, Noise: 0.62}
	default:
		return Effects{}, fmt.Errorf(locale.Text("неизвестный пресет %q", "unknown preset %q"), name)
	}
	return e, nil
}

// Effective returns shader-ready strengths. The original controls survive
// toggling. At zero intensity every effect, including geometry, is exactly zero.
// Exact mouse mode disables legacy curvature and VHS jitter/tracking. The
// built-in screen shape is handled separately by EffectiveScreen.
func (c Config) Effective() Effects {
	e := c.Effects
	i := e.Intensity
	if !c.Enabled {
		i = 0
	}
	e.CRT.Scanlines *= i
	e.CRT.Mask *= i
	e.CRT.Bloom *= i
	e.CRT.Softness *= i
	e.CRT.Vignette *= i
	e.CRT.Curvature *= i
	e.VHS.ChromaBleed *= i
	e.VHS.Noise *= i
	e.VHS.Jitter *= i
	e.VHS.Tracking *= i
	if c.InputMode == "mouse-exact" {
		e.CRT.Curvature = 0
		e.VHS.Jitter = 0
		e.VHS.Tracking = 0
	}
	e.Intensity = i
	return e
}

// EffectiveScreen is shader-ready and leaves saved settings intact. The
// renderer never derives deformation from the legacy CRT.Curvature control;
// an explicitly selected convex Full screen moves pixels and projects the system
// cursor through the same geometry without altering input.
func (c Config) EffectiveScreen() Screen {
	s := c.Screen
	i := c.Effects.Intensity
	if !c.Enabled || i <= 0 || s.Shape == ShapeFlat || s.Shape == "" {
		return Screen{Shape: ShapeFlat}
	}
	s.CornerRadius *= i
	s.Glass *= i
	if s.Shape == ShapeConvex && c.Mode == "full" {
		s.Curvature *= i
	} else {
		s.Curvature = 0
		s.Shape = ShapeRounded
	}
	return s
}

func (c Config) Validate() error {
	if c.Version != Version {
		return fmt.Errorf(locale.Text("версия настроек %d не поддерживается (ожидается %d)", "unsupported config version %d (expected %d)"), c.Version, Version)
	}
	if c.Mode != "overlay" && c.Mode != "full" {
		return fmt.Errorf(locale.Text("неизвестный режим %q", "unknown mode %q"), c.Mode)
	}
	if c.Capture.Transfer != TransferAuto && c.Capture.Transfer != TransferGPU && c.Capture.Transfer != TransferCompatibility {
		return errors.New(locale.Text("capture.transfer должен быть auto, gpu или compatibility", "capture transfer must be auto, gpu or compatibility"))
	}
	if err := c.Shader.Validate(); err != nil {
		return err
	}
	if c.Target.Kind != "monitor" && c.Target.Kind != "window" {
		return fmt.Errorf(locale.Text("неизвестный тип источника %q", "unknown target kind %q"), c.Target.Kind)
	}
	if c.Target.Monitor < 0 || c.Target.Monitor > 1024 {
		return errors.New(locale.Text("индекс монитора должен быть от 0 до 1024", "monitor index must be between 0 and 1024"))
	}
	if len(c.Target.WindowTitle) > 4096 || strings.ContainsRune(c.Target.WindowTitle, 0) {
		return errors.New(locale.Text("заголовок окна слишком длинный или содержит NUL", "window title is too long or contains NUL"))
	}
	if c.Target.Kind == "window" && strings.TrimSpace(c.Target.WindowTitle) == "" {
		return errors.New(locale.Text("для источника-окна нужен заголовок", "window target requires a title selector"))
	}
	if c.Preset != "Custom" {
		if _, err := Preset(c.Preset); err != nil {
			return err
		}
	}
	if c.Aspect.Method != "mask" && c.Aspect.Method != "window" && c.Aspect.Method != "system" {
		return fmt.Errorf(locale.Text("неизвестный метод формата %q", "unknown aspect method %q"), c.Aspect.Method)
	}
	if c.Aspect.Scale != geometry.Fit && c.Aspect.Scale != geometry.Crop && c.Aspect.Scale != geometry.Stretch {
		return fmt.Errorf(locale.Text("неизвестный режим масштаба %q", "unknown scaling mode %q"), c.Aspect.Scale)
	}
	if math.IsNaN(c.Aspect.SourceDAR) || math.IsInf(c.Aspect.SourceDAR, 0) || c.Aspect.SourceDAR < 0 || c.Aspect.SourceDAR > 100 {
		return errors.New(locale.Text("исходные пропорции должны быть 0 (квадратные пиксели) либо конечным числом в (0,100]", "source display aspect must be zero (square pixels) or finite in (0,100]"))
	}
	if c.InputMode != "mouse-exact" && c.InputMode != "keyboard-gamepad" {
		return fmt.Errorf(locale.Text("неизвестный режим искажений %q", "unknown input mode %q"), c.InputMode)
	}
	if c.Screen.Shape != ShapeFlat && c.Screen.Shape != ShapeRounded && c.Screen.Shape != ShapeConvex {
		return fmt.Errorf(locale.Text("форма экрана должна быть flat, rounded или convex", "screen shape must be flat, rounded or convex"))
	}
	if math.IsNaN(c.Screen.CornerRadius) || math.IsInf(c.Screen.CornerRadius, 0) || c.Screen.CornerRadius < 0 || c.Screen.CornerRadius > .2 {
		return errors.New(locale.Text("screen.corner_radius должен быть конечным числом от 0 до 0.2", "screen.corner_radius must be finite and between 0 and 0.2"))
	}
	values := []struct {
		name  string
		value float64
	}{
		{"intensity", c.Effects.Intensity},
		{"crt.scanlines", c.Effects.CRT.Scanlines}, {"crt.mask", c.Effects.CRT.Mask},
		{"crt.bloom", c.Effects.CRT.Bloom}, {"crt.softness", c.Effects.CRT.Softness},
		{"crt.vignette", c.Effects.CRT.Vignette}, {"crt.curvature", c.Effects.CRT.Curvature},
		{"vhs.chroma_bleed", c.Effects.VHS.ChromaBleed}, {"vhs.noise", c.Effects.VHS.Noise},
		{"vhs.jitter", c.Effects.VHS.Jitter}, {"vhs.tracking", c.Effects.VHS.Tracking},
		{"screen.curvature", c.Screen.Curvature}, {"screen.glass", c.Screen.Glass},
	}
	for _, field := range values {
		if math.IsNaN(field.value) || math.IsInf(field.value, 0) || field.value < 0 || field.value > 1 {
			return fmt.Errorf(locale.Text("%s должен быть конечным числом от 0 до 1", "%s must be finite and between 0 and 1"), field.name)
		}
	}
	toggle, err := ParseHotkey(c.Hotkeys.Toggle)
	if err != nil {
		return fmt.Errorf(locale.Text("клавиша переключения: %w", "toggle hotkey: %w"), err)
	}
	emergency, err := ParseHotkey(c.Hotkeys.Emergency)
	if err != nil {
		return fmt.Errorf(locale.Text("аварийная клавиша: %w", "emergency hotkey: %w"), err)
	}
	if toggle == emergency {
		return errors.New(locale.Text("переключение и аварийное отключение должны иметь разные сочетания", "toggle and emergency hotkeys must be different"))
	}
	return c.ValidateCombinations()
}

// Decode applies omitted fields to defaults, rejects unknown fields, nulls,
// duplicate keys, malformed JSON and trailing documents. This prevents a typo
// from silently weakening an emergency control or loading only half a config.
func Decode(data []byte) (Config, error) {
	if len(data) > MaxFileSize {
		return Config{}, errors.New(locale.Text("размер настроек превышает 64 KiB", "config exceeds 64 KiB"))
	}
	if err := checkJSON(data); err != nil {
		return Config{}, err
	}
	c := Default()
	// A small hand-written config can name a preset and override only selected
	// controls. Resolve that preset before applying the optional effect fields.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return Config{}, err
	}
	for key, value := range fields {
		if strings.EqualFold(key, "preset") {
			var name string
			if err := json.Unmarshal(value, &name); err != nil {
				return Config{}, fmt.Errorf(locale.Text("пресет: %w", "preset: %w"), err)
			}
			if name != "Custom" {
				effects, err := Preset(name)
				if err != nil {
					return Config{}, err
				}
				c.Effects = effects
			}
		}
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return Config{}, fmt.Errorf(locale.Text("чтение настроек: %w", "decode config: %w"), err)
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

func Load(path string) (Config, error) {
	c, err := LoadExisting(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	return c, err
}

// LoadExisting preserves a missing-file error so a caller can distinguish a
// first launch from an existing profile that intentionally uses default fields.
// Load retains its historical missing-file defaults for CLI/reload callers.
func LoadExisting(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxFileSize+1))
	if err != nil {
		return Config{}, err
	}
	c, err := Decode(data)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// Save writes a complete file in the destination directory, syncs it and then
// replaces the old file. Failure before replacement preserves the old settings.
// Mode recovery uses its own durable journal; it must not rely on preferences.
func Save(path string, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".lumatape-config-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return replaceFile(tmp, path)
}

func checkJSON(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	tok, err := d.Token()
	if err != nil || tok != json.Delim('{') {
		return errors.New(locale.Text("настройки должны быть JSON-объектом", "config must be a JSON object"))
	}
	if err := walkObject(d, ""); err != nil {
		return fmt.Errorf(locale.Text("неверный JSON настроек: %w", "invalid config JSON: %w"), err)
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New(locale.Text("после JSON настроек есть лишние данные", "config contains trailing data"))
	}
	return nil
}

func walkObject(d *json.Decoder, path string) error {
	seen := map[string]bool{}
	for d.More() {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		key, ok := tok.(string)
		if !ok {
			return errors.New(locale.Text("ожидается ключ объекта", "expected an object key"))
		}
		// encoding/json matches struct fields case-insensitively. Reject aliases
		// as duplicates too, so "mode" and "Mode" cannot override each other.
		folded := strings.ToLower(key)
		if seen[folded] {
			return fmt.Errorf(locale.Text("повторяющийся ключ %q", "duplicate key %q"), key)
		}
		seen[folded] = true
		tok, err = d.Token()
		if err != nil {
			return err
		}
		if tok == nil {
			return fmt.Errorf(locale.Text("null недопустим в настройках (%s)", "null is not a setting value (%s)"), key)
		}
		if delimiter, ok := tok.(json.Delim); ok {
			if delimiter == '[' && path == "shader" && folded == "params" {
				count := 0
				for d.More() {
					value, err := d.Token()
					if err != nil {
						return err
					}
					if _, ok := value.(float64); !ok {
						return errors.New(locale.Text("shader.params должен содержать числа", "shader.params must contain numbers"))
					}
					count++
					if count > 8 {
						return errors.New(locale.Text("shader.params должен содержать ровно 8 чисел", "shader.params must contain exactly 8 numbers"))
					}
				}
				if _, err := d.Token(); err != nil {
					return err
				}
				if count != 8 {
					return errors.New(locale.Text("shader.params должен содержать ровно 8 чисел", "shader.params must contain exactly 8 numbers"))
				}
				continue
			}
			if delimiter != '{' {
				return errors.New(locale.Text("массив допустим только для shader.params", "arrays are only allowed for shader.params"))
			}
			childPath := folded
			if path != "" {
				childPath = path + "." + folded
			}
			if err := walkObject(d, childPath); err != nil {
				return err
			}
		}
	}
	end, err := d.Token()
	if err != nil {
		return err
	}
	if end != json.Delim('}') {
		return errors.New(locale.Text("незавершённый объект", "unterminated object"))
	}
	return nil
}
