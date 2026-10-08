package config

import (
	"fmt"

	"github.com/aiwaki/lumatape/internal/geometry"
	"github.com/aiwaki/lumatape/internal/locale"
)

// ValidateCombinations is shared by JSON, CLI and interactive changes. Invalid
// input geometry must be rejected before replacing a working presentation.
func (c Config) ValidateCombinations() error {
	if c.Shader.ID != "" && c.Mode != "full" {
		return fmt.Errorf(locale.Text("пользовательский шейдер требует Full", "custom shaders require Full"))
	}
	if c.Mode == "full" && c.Target.Kind != "window" {
		return fmt.Errorf(locale.Text("для Full выберите окно; захват монитора недоступен", "Full requires a selected window; monitor capture is unavailable"))
	}
	if c.InputMode == "mouse-exact" && (c.Aspect.SourceDAR != 0 || c.Aspect.Scale != geometry.Fit) {
		return fmt.Errorf(locale.Text("точные клики требуют Fit и квадратных исходных пикселей; для Crop, Stretch или DAR выберите «Искажения изображения → Разрешить произвольные искажения»", "accurate clicks require Fit and square source pixels; for Crop, Stretch or DAR choose “Image distortion → Allow arbitrary distortion”"))
	}
	if c.Aspect.Enabled && c.Aspect.Method == "window" && c.Target.Kind != "window" {
		return fmt.Errorf(locale.Text("для оконного 4:3 выберите окно", "window 4:3 requires a selected window"))
	}
	if c.Screen.Shape == "convex" && c.Mode != "full" {
		return fmt.Errorf(locale.Text("выпуклый экран требует Full; системный курсор проецируется вместе с изображением", "convex screen requires Full; its system cursor is projected with the image"))
	}
	return nil
}
