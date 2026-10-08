//go:build windows

package app

import (
	"fmt"
	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/control"
	"github.com/aiwaki/lumatape/internal/locale"
	"github.com/aiwaki/lumatape/internal/render"
	"github.com/aiwaki/lumatape/internal/shaderpack"
)

func (a *application) loadShader(c config.Config) (shaderpack.Pack, error) {
	p, err := shaderpack.Load(a.shaderDirectory, c.Shader.ID)
	if err != nil {
		return p, &ShaderError{"shader_asset_unavailable", fmt.Errorf(locale.Text("шейдер недоступен; импортируйте файл заново: %w", "shader unavailable; import the file again: %w"), err)}
	}
	if err = validateShaderChoice(c, p); err != nil {
		return p, &ShaderError{"shader_validation", err}
	}
	return p, nil
}

// Ownership is explicit: reused live pointers are borrowed, new candidates
// belong to the caller until committed. Nothing here changes the live program.
func (a *application) prepareLiveShader(c config.Config) (*render.ShaderProgram, bool, error) {
	if c.Shader.ID == "" {
		return nil, false, nil
	}
	disabled := a.cfg
	disabled.Enabled = false
	if c == disabled {
		return a.shaderProgram, false, nil // off never needs to read/compile an asset
	}
	if a.shaderProgram != nil && a.shaderProgram.ID == c.Shader.ID {
		pack := shaderpack.Pack{Descriptor: a.shaderProgram.Metadata}
		if err := validateShaderChoice(c, pack); err != nil {
			return nil, false, &ShaderError{"shader_validation", err}
		}
		return a.shaderProgram, false, nil
	}
	p, err := a.loadShader(c)
	if err != nil {
		return nil, false, err
	}
	program, err := a.renderer.CompileShader(p)
	if err != nil {
		return nil, false, &ShaderError{"shader_compile_failed", fmt.Errorf(locale.Text("шейдер не скомпилирован; предыдущий эффект сохранён: %w", "shader did not compile; the previous effect is preserved: %w"), err)}
	}
	return program, true, nil
}

func (a *application) previewRGBA(c config.Config, width, height int, seconds float64) ([]byte, error) {
	if c.Shader.ID == "" {
		return a.renderer.PreviewRGBA(c, width, height, seconds)
	}
	p, err := a.loadShader(c)
	if err != nil {
		return nil, err
	}
	if a.previewShader == nil || a.previewShader.ID != p.ID {
		program, err := a.renderer.CompileShader(p)
		if err != nil {
			return nil, &ShaderError{"shader_compile_failed", err}
		}
		a.previewShader.Close()
		a.previewShader = program
	}
	return a.renderer.PreviewRGBA(c, width, height, seconds, a.previewShader)
}

func (a *application) controlShaders(r control.Request) {
	fail := func(code string, err error) {
		a.control.Reply(r.ID, nil, &control.Failure{Code: code, Message: err.Error()})
	}
	switch r.Type {
	case "shaders":
		if err := control.DecodePayload(r.Payload, &struct{}{}); err != nil {
			fail("invalid_payload", err)
			return
		}
		items, err := shaderpack.List(a.shaderDirectory)
		if items == nil {
			items = []shaderpack.Descriptor{}
		}
		result := map[string]any{"items": items}
		if err != nil {
			result["warning"] = err.Error()
		}
		a.control.Reply(r.ID, result, nil)
	case "shader_source":
		var payload struct {
			ID string `json:"id"`
		}
		if err := control.DecodePayload(r.Payload, &payload); err != nil {
			fail("invalid_payload", err)
			return
		}
		p, err := shaderpack.Load(a.shaderDirectory, payload.ID)
		if err != nil {
			fail("shader_asset_unavailable", err)
			return
		}
		a.control.Reply(r.ID, map[string]any{"source": p.Source, "shader": p.Descriptor}, nil)
	case "shader_import":
		var payload struct {
			Source string `json:"source"`
		}
		if err := control.DecodePayload(r.Payload, &payload); err != nil {
			fail("invalid_payload", err)
			return
		}
		p, err := shaderpack.Parse(payload.Source)
		if err != nil {
			fail("shader_validation", err)
			return
		}
		program, err := a.renderer.CompileShader(p)
		if err != nil {
			fail("shader_compile_failed", fmt.Errorf(locale.Text("шейдер не скомпилирован; текущий эффект сохранён: %w", "shader did not compile; the current effect is preserved: %w"), err))
			return
		}
		program.Close()
		if err = shaderpack.Save(a.shaderDirectory, p); err != nil {
			fail("shader_save_failed", err)
			return
		}
		a.record("shader_imported", map[string]any{"id": p.ID, "coordinates": p.Coordinates, "parameters": len(p.Parameters)})
		a.control.Reply(r.ID, map[string]any{"shader": p.Descriptor}, nil)
	}
}
