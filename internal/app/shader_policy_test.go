package app

import (
	"errors"
	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/platform/capture"
	"github.com/aiwaki/lumatape/internal/shaderpack"
	"testing"
)

func TestCustomShaderMetadataGate(t *testing.T) {
	pack, err := shaderpack.Parse(`/* LumaTape
{"version":1,"name":"Warp","description":"test","coordinates":"warp","parameters":[{"name":"Amount","min":0,"max":1,"step":0.1,"default":0.5}]}
*/
vec3 lumatape(vec2 uv){return ltSample(uv);}`)
	if err != nil {
		t.Fatal(err)
	}
	c := config.Default()
	c.Mode = "full"
	c.Shader = config.ShaderConfig{ID: pack.ID, Params: pack.Defaults()}
	if validateShaderChoice(c, pack) == nil {
		t.Fatal("warp allowed exact mouse")
	}
	c.InputMode = "keyboard-gamepad"
	if err := validateShaderChoice(c, pack); err != nil {
		t.Fatal(err)
	}
	c.Shader.Params[0] = 2
	if validateShaderChoice(c, pack) == nil {
		t.Fatal("metadata max ignored")
	}
	c.Shader.Params = pack.Defaults()
	c.Shader.Params[7] = 1
	if validateShaderChoice(c, pack) == nil {
		t.Fatal("unused slot ignored")
	}
	c.Shader.Params = pack.Defaults()
	c.Shader.ID = ""
	if validateShaderChoice(c, pack) == nil {
		t.Fatal("identity mismatch accepted")
	}
}
func TestAutoCaptureOnlyKnownInteropFailure(t *testing.T) {
	for _, preference := range []string{config.TransferGPU, config.TransferCompatibility, config.TransferAuto} {
		for _, attempt := range []string{config.TransferGPU, config.TransferCompatibility} {
			for _, test := range []struct {
				err     error
				interop bool
			}{
				{nil, false}, {capture.ErrGPUInteropUnavailable, true}, {gpuUnavailable(capture.ErrGPUInteropUnavailable, true), true},
				{errors.New("capture open: Full mode unavailable: this OpenGL driver lacks WGL_NV_DX_interop2; use Lightweight"), true},
				{gpuUnavailable(errors.New("capture open: Full mode unavailable: this OpenGL driver lacks WGL_NV_DX_interop2; use Lightweight"), true), true},
				{errors.New("HDR source unavailable"), false}, {errors.New("window closed"), false},
				{errors.New("No hardware D3D11 adapter can share BGRA textures with the current OpenGL context; use Lightweight"), false},
				{errors.New("WGC access denied"), false}, {errors.New("close capture failed"), false},
			} {
				want := preference == config.TransferAuto && attempt == config.TransferGPU && test.interop
				if got := shouldFallbackCapture(preference, attempt, test.err); got != want {
					t.Fatalf("%s/%s %v => %v want%v", preference, attempt, test.err, got, want)
				}
			}
		}
	}
}
func TestShaderErrorsRemainTypedAndUnapplied(t *testing.T) {
	f := mutationFailure(&ShaderError{"shader_compile_failed", errors.New("driver compile rejected")}, false)
	if f.Code != "shader_compile_failed" || f.Applied || f.Unsaved {
		t.Fatalf("%+v", f)
	}
}
