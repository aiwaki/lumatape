package config

import (
	"fmt"
	"math"

	"github.com/aiwaki/lumatape/internal/locale"
	"github.com/aiwaki/lumatape/internal/shaderpack"
)

// ShaderConfig stores immutable asset identity and fixed-size values, keeping
// Config comparable. Source text and native program handles never enter JSON.
type ShaderConfig struct {
	ID     string     `json:"id"`
	Params [8]float64 `json:"params"`
}

func (s ShaderConfig) Validate() error {
	if s.ID != "" && !shaderpack.ValidID(s.ID) {
		return fmt.Errorf(locale.Text("shader.id должен быть неизменяемым SHA-256 идентификатором файла", "shader.id must be an immutable SHA-256 asset ID"))
	}
	for _, value := range s.Params {
		if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) > 1e6 {
			return fmt.Errorf(locale.Text("параметры шейдера должны быть конечными числами в пределах ±1000000", "shader parameters must be finite and within ±1000000"))
		}
		if s.ID == "" && value != 0 {
			return fmt.Errorf(locale.Text("встроенным эффектам нужны пустые параметры шейдера", "built-in effects require empty shader parameters"))
		}
	}
	return nil
}
