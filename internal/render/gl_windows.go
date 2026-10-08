//go:build windows

package render

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/geometry"
)

// API uses pointer forms of float uniforms; the ABI binding therefore requires
// no platform-dependent floating point return/register marshaling.
type API struct{ functions map[string]uintptr }

var copyDriverString = syscall.NewLazyDLL("kernel32.dll").NewProc("lstrcpynA")

//go:uintptrescapes
func (g *API) call(name string, a ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(g.functions[name], a...)
	return r
}

type Program struct {
	id       uint32
	uniforms map[string]int32
}
type Renderer struct {
	gl                      *API
	overlay, full           Program
	vao                     uint32
	queries                 [4]uint32
	pending                 [4]bool
	queryIndex              int
	GPUTimeMS               float64
	GPUTimeSequence         uint64 // increments only when a new asynchronous query result arrives
	frameWidth, frameHeight int
}
type Frame struct {
	Width, Height int
	Area          geometry.Rect
	SourceUV      geometry.UVRect
	Texture       uint32
	SourceSize    geometry.Size
	Time          float64
	Effects       config.Effects
	Screen        config.Screen // Config.EffectiveScreen(), independently of the preset
	Shader        *ShaderProgram
	ShaderParams  [8]float64
}

func New(resolve func(string) uintptr) (*Renderer, error) {
	g := &API{functions: map[string]uintptr{}}
	for _, name := range []string{"glCreateShader", "glShaderSource", "glCompileShader", "glGetShaderiv", "glGetShaderInfoLog", "glDeleteShader", "glCreateProgram", "glAttachShader", "glLinkProgram", "glGetProgramiv", "glGetProgramInfoLog", "glDeleteProgram", "glUseProgram", "glGetUniformLocation", "glUniform1fv", "glUniform2fv", "glUniform4fv", "glUniform1i", "glUniform1ui", "glGenVertexArrays", "glBindVertexArray", "glDeleteVertexArrays", "glViewport", "glDisable", "glDrawArrays", "glBindTexture", "glActiveTexture", "glGenQueries", "glBeginQuery", "glEndQuery", "glGetQueryObjectiv", "glGetQueryObjectui64v", "glDeleteQueries", "glGetError", "glReadPixels", "glReadBuffer", "glPixelStorei", "glGetIntegerv", "glBindBuffer", "glBindFramebuffer", "glGenTextures", "glDeleteTextures", "glTexImage2D", "glTexParameteri", "glGenFramebuffers", "glDeleteFramebuffers", "glFramebufferTexture2D", "glCheckFramebufferStatus", "glEnable", "glIsEnabled", "glBlendFuncSeparate", "glBlendEquationSeparate"} {
		p := resolve(name)
		if p == 0 || p <= 3 || p == ^uintptr(0) {
			return nil, fmt.Errorf("OpenGL 3.3 function %s unavailable", name)
		}
		g.functions[name] = p
	}
	g.functions["glGetString"] = resolve("glGetString")
	if p := g.functions["glGetString"]; p <= 3 || p == ^uintptr(0) {
		return nil, fmt.Errorf("OpenGL glGetString unavailable")
	}
	r := &Renderer{gl: g, GPUTimeMS: -1}
	var err error
	if r.overlay, err = g.program(Vertex, Overlay); err != nil {
		r.Close()
		return nil, err
	}
	if r.full, err = g.program(Vertex, CRT); err != nil {
		r.Close()
		return nil, err
	}
	g.call("glGenVertexArrays", 1, uintptr(unsafe.Pointer(&r.vao)))
	g.call("glBindVertexArray", uintptr(r.vao))
	g.call("glGenQueries", 4, uintptr(unsafe.Pointer(&r.queries[0])))
	g.call("glDisable", 0x0be2) // no blend: final fragment owns RGBA, premultiplied for overlay
	g.call("glDisable", 0x0b71)
	g.call("glDisable", 0x8db9) // explicit SDR encoding, no second sRGB transfer
	if e := r.Error(); e != nil {
		r.Close()
		return nil, e
	}
	return r, nil
}
func (g *API) shader(kind uint32, source string) (uint32, error) {
	s := uint32(g.call("glCreateShader", uintptr(kind)))
	if s == 0 {
		return 0, fmt.Errorf("glCreateShader failed")
	}
	b := append([]byte(source), 0)
	ptr := &b[0]
	length := int32(len(b) - 1)
	g.call("glShaderSource", uintptr(s), 1, uintptr(unsafe.Pointer(&ptr)), uintptr(unsafe.Pointer(&length)))
	runtime.KeepAlive(b)
	g.call("glCompileShader", uintptr(s))
	var ok int32
	g.call("glGetShaderiv", uintptr(s), 0x8b81, uintptr(unsafe.Pointer(&ok)))
	if ok == 0 {
		var log [8192]byte
		g.call("glGetShaderInfoLog", uintptr(s), 8192, 0, uintptr(unsafe.Pointer(&log[0])))
		g.call("glDeleteShader", uintptr(s))
		return 0, fmt.Errorf("shader compilation: %s", cstring(log[:]))
	}
	return s, nil
}
func cstring(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
func (g *API) program(vs, fs string) (Program, error) {
	v, e := g.shader(0x8b31, vs)
	if e != nil {
		return Program{}, e
	}
	defer g.call("glDeleteShader", uintptr(v))
	f, e := g.shader(0x8b30, fs)
	if e != nil {
		return Program{}, e
	}
	defer g.call("glDeleteShader", uintptr(f))
	p := uint32(g.call("glCreateProgram"))
	g.call("glAttachShader", uintptr(p), uintptr(v))
	g.call("glAttachShader", uintptr(p), uintptr(f))
	g.call("glLinkProgram", uintptr(p))
	var ok int32
	g.call("glGetProgramiv", uintptr(p), 0x8b82, uintptr(unsafe.Pointer(&ok)))
	if ok == 0 {
		var log [8192]byte
		g.call("glGetProgramInfoLog", uintptr(p), 8192, 0, uintptr(unsafe.Pointer(&log[0])))
		g.call("glDeleteProgram", uintptr(p))
		return Program{}, fmt.Errorf("shader link: %s", cstring(log[:]))
	}
	result := Program{id: p, uniforms: map[string]int32{}}
	for _, n := range []string{"uOutput", "uArea", "uSourceUV", "uSource", "uSourceSize", "uCRT", "uVHS", "uVignette", "uCurvature", "uScreen", "uIntensity", "uTime", "uSeed", "uFreeze", "ltResolution", "ltTime", "ltParams[0]"} {
		b := append([]byte(n), 0)
		result.uniforms[n] = int32(g.call("glGetUniformLocation", uintptr(p), uintptr(unsafe.Pointer(&b[0]))))
	}
	return result, nil
}
func (g *API) uniform(p Program, n string, v ...float32) {
	loc, found := p.uniforms[n]
	if !found || loc < 0 {
		return
	}
	fn := "glUniform1fv"
	if len(v) == 2 {
		fn = "glUniform2fv"
	}
	if len(v) == 4 {
		fn = "glUniform4fv"
	}
	g.call(fn, uintptr(loc), 1, uintptr(unsafe.Pointer(&v[0])))
}
func (r *Renderer) Draw(f Frame, full bool) {
	r.draw(f, full, true)
}
func (r *Renderer) draw(f Frame, full, measure bool) {
	g := r.gl
	p := r.overlay
	if full {
		p = r.full
		if f.Shader != nil && f.Shader.program.id != 0 {
			p = f.Shader.program
		}
	}
	// Never block waiting for a GPU query from a prior frame.
	i := r.queryIndex
	var available int32
	if measure && r.pending[i] {
		g.call("glGetQueryObjectiv", uintptr(r.queries[i]), 0x8867, uintptr(unsafe.Pointer(&available)))
		if available != 0 {
			var ns uint64
			g.call("glGetQueryObjectui64v", uintptr(r.queries[i]), 0x8866, uintptr(unsafe.Pointer(&ns)))
			r.GPUTimeMS = float64(ns) / 1e6
			r.GPUTimeSequence++
			r.pending[i] = false
		}
	}
	query := measure && !r.pending[i]
	if query {
		g.call("glBeginQuery", 0x88bf, uintptr(r.queries[i]))
	}
	g.call("glViewport", 0, 0, uintptr(f.Width), uintptr(f.Height))
	g.call("glUseProgram", uintptr(p.id))
	g.call("glBindVertexArray", uintptr(r.vao))
	g.uniform(p, "uOutput", float32(f.Width), float32(f.Height))
	g.uniform(p, "uArea", float32(f.Area.X), float32(f.Area.Y), float32(f.Area.W), float32(f.Area.H))
	e := f.Effects
	g.uniform(p, "uCRT", float32(e.CRT.Scanlines), float32(e.CRT.Mask), float32(e.CRT.Bloom), float32(e.CRT.Softness))
	g.uniform(p, "uVHS", float32(e.VHS.ChromaBleed), float32(e.VHS.Noise), float32(e.VHS.Jitter), float32(e.VHS.Tracking))
	g.uniform(p, "uVignette", float32(e.CRT.Vignette))
	g.uniform(p, "uCurvature", float32(f.Screen.Curvature))
	g.uniform(p, "uScreen", float32(f.Screen.CornerRadius), float32(f.Screen.Glass), 0, 0)
	g.uniform(p, "uIntensity", float32(e.Intensity))
	g.uniform(p, "uTime", float32(f.Time))
	g.uniform(p, "ltResolution", float32(f.Area.W), float32(f.Area.H))
	shaderTime := f.Time
	if e.FreezeNoise {
		shaderTime = 0
	}
	g.uniform(p, "ltTime", float32(shaderTime))
	if loc := p.uniforms["ltParams[0]"]; loc >= 0 {
		var values [8]float32
		for i, v := range f.ShaderParams {
			values[i] = float32(v)
		}
		g.call("glUniform1fv", uintptr(loc), 8, uintptr(unsafe.Pointer(&values[0])))
	}
	g.call("glUniform1ui", uintptr(p.uniforms["uSeed"]), uintptr(e.NoiseSeed))
	freeze := uintptr(0)
	if e.FreezeNoise {
		freeze = 1
	}
	g.call("glUniform1i", uintptr(p.uniforms["uFreeze"]), freeze)
	if full {
		g.call("glActiveTexture", 0x84c0)
		g.call("glBindTexture", 0x0de1, uintptr(f.Texture))
		g.call("glUniform1i", uintptr(p.uniforms["uSource"]), 0)
		g.uniform(p, "uSourceSize", float32(f.SourceSize.W), float32(f.SourceSize.H))
		g.uniform(p, "uSourceUV", float32(f.SourceUV.X), float32(f.SourceUV.Y), float32(f.SourceUV.W), float32(f.SourceUV.H))
	}
	g.call("glDrawArrays", 4, 0, 3)
	if measure {
		r.frameWidth, r.frameHeight = f.Width, f.Height
	}
	if full {
		g.call("glBindTexture", 0x0de1, 0)
	}
	if query {
		g.call("glEndQuery", 0x88bf)
		r.pending[i] = true
	}
	if measure {
		r.queryIndex = (i + 1) % len(r.queries)
	}
}
func (r *Renderer) Error() error {
	if e := r.gl.call("glGetError"); e != 0 {
		return fmt.Errorf("OpenGL error 0x%x", e)
	}
	return nil
}

// DriverInfo contains public OpenGL implementation strings only. Call on the
// renderer's current GL context/owner thread; no filesystem/user data is read.
func (r *Renderer) DriverInfo() map[string]string {
	info := map[string]string{}
	if r == nil || r.gl == nil {
		return info
	}
	for name, key := range map[string]uintptr{"vendor": 0x1f00, "renderer": 0x1f01, "version": 0x1f02, "glsl_version": 0x8b8c} {
		address := r.gl.call("glGetString", key)
		if address == 0 {
			continue
		}
		// Copy from the driver-owned native pointer through Win32 instead of
		// constructing a Go pointer from a long-lived uintptr. The bound includes
		// the terminator and prevents driver metadata from growing diagnostics.
		var value [4096]byte
		if copied, _, _ := copyDriverString.Call(uintptr(unsafe.Pointer(&value[0])), address, uintptr(len(value))); copied != 0 {
			info[name] = cstring(value[:])
		}
	}
	return info
}
func (r *Renderer) Close() {
	if r.gl == nil {
		return
	}
	g := r.gl
	for _, p := range []Program{r.overlay, r.full} {
		if p.id != 0 {
			g.call("glDeleteProgram", uintptr(p.id))
		}
	}
	if r.vao != 0 {
		g.call("glDeleteVertexArrays", 1, uintptr(unsafe.Pointer(&r.vao)))
	}
	if r.queries[0] != 0 {
		g.call("glDeleteQueries", 4, uintptr(unsafe.Pointer(&r.queries[0])))
	}
	r.gl = nil
}
