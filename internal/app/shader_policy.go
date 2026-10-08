package app

import (
	"fmt"
	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/locale"
	"github.com/aiwaki/lumatape/internal/shaderpack"
)

type ShaderError struct {
	Code string
	Err  error
}

func (e *ShaderError) Error() string { return e.Err.Error() }
func (e *ShaderError) Unwrap() error { return e.Err }

func validateShaderChoice(c config.Config, p shaderpack.Pack) error {
	if c.Shader.ID != p.ID {
		return fmt.Errorf(locale.Text("идентичность шейдера изменилась; импортируйте его заново", "shader identity changed; import it again"))
	}
	if c.Mode != "full" {
		return fmt.Errorf(locale.Text("пользовательский шейдер требует Full", "custom shaders require Full"))
	}
	if p.Coordinates == "warp" && c.InputMode != "keyboard-gamepad" {
		return fmt.Errorf(locale.Text("этот шейдер смещает изображение; выберите «Искажения изображения → Разрешить произвольные искажения». Клики могут не совпадать с изображением", "this shader distorts the image; choose “Image distortion → Allow arbitrary distortion”. Clicks may not align with the image"))
	}
	return p.ValidateParams(c.Shader.Params)
}
