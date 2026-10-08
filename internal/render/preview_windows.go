//go:build windows

package render

import (
	"fmt"
	"runtime"
	"unsafe"

	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/geometry"
)

// PreviewRGBA renders a deterministic test scene with the SAME shader programs
// as the main renderer. It returns opaque top-left RGBA for a native settings
// preview, never a game capture. Call on the owning GL thread, outside an active
// capture Acquire/Release pair. This synchronous diagnostic readback is intended
// for control changes or a slow preview timer, not normal frame measurements.
// For the original half of A/B, pass a copy with Effects.Intensity=0.
func (r *Renderer) PreviewRGBA(c config.Config, width, height int, seconds float64, custom ...*ShaderProgram) ([]byte, error) {
	if r == nil || r.gl == nil {
		return nil, fmt.Errorf("preview renderer is closed")
	}
	validated := c
	// A synthetic preview does not need a selected/live game, but all rendering
	// and input-mode combinations must still obey the normal validation rules.
	validated.Target = config.Target{Kind: "window", WindowTitle: "LumaTape preview"}
	if err := validated.Validate(); err != nil {
		return nil, err
	}
	input, err := PreviewScene(width, height)
	if err != nil {
		return nil, err
	}
	if err = r.Error(); err != nil {
		return nil, fmt.Errorf("before preview: %w", err)
	}
	g := r.gl
	get := func(key uintptr) int32 {
		var value int32
		g.call("glGetIntegerv", key, uintptr(unsafe.Pointer(&value)))
		return value
	}
	drawFB, readFB := get(0x8ca6), get(0x8caa)
	program, vao, active := get(0x8b8d), get(0x85b5), get(0x84e0)
	var viewport [4]int32
	g.call("glGetIntegerv", 0x0ba2, uintptr(unsafe.Pointer(&viewport[0])))
	g.call("glActiveTexture", 0x84c0)
	texture0 := get(0x8069)
	packBuffer, unpackBuffer := get(0x88ed), get(0x88ef)
	pixelKeys := []uintptr{0x0d05, 0x0d02, 0x0d03, 0x0d04, 0x0cf5, 0x0cf2, 0x0cf3, 0x0cf4}
	pixelValues := make([]int32, len(pixelKeys))
	for i, key := range pixelKeys {
		pixelValues[i] = get(key)
	}
	caps := []uintptr{0x0be2, 0x0c11, 0x0b71, 0x8db9}
	enabled := make([]bool, len(caps))
	for i, cap := range caps {
		enabled[i] = g.call("glIsEnabled", cap) != 0
		g.call("glDisable", cap)
	}
	blendKeys := []uintptr{0x80c9, 0x80c8, 0x80cb, 0x80ca, 0x8009, 0x883d}
	blend := make([]int32, len(blendKeys))
	for i, key := range blendKeys {
		blend[i] = get(key)
	}
	var textures [2]uint32
	var framebuffer uint32
	defer func() {
		g.call("glBindFramebuffer", 0x8ca9, uintptr(drawFB))
		g.call("glBindFramebuffer", 0x8ca8, uintptr(readFB))
		g.call("glUseProgram", uintptr(program))
		g.call("glBindVertexArray", uintptr(vao))
		g.call("glViewport", uintptr(viewport[0]), uintptr(viewport[1]), uintptr(viewport[2]), uintptr(viewport[3]))
		g.call("glActiveTexture", 0x84c0)
		g.call("glBindTexture", 0x0de1, uintptr(texture0))
		g.call("glActiveTexture", uintptr(active))
		g.call("glBindBuffer", 0x88eb, uintptr(packBuffer))
		g.call("glBindBuffer", 0x88ec, uintptr(unpackBuffer))
		for i, key := range pixelKeys {
			g.call("glPixelStorei", key, uintptr(pixelValues[i]))
		}
		g.call("glBlendFuncSeparate", uintptr(blend[0]), uintptr(blend[1]), uintptr(blend[2]), uintptr(blend[3]))
		g.call("glBlendEquationSeparate", uintptr(blend[4]), uintptr(blend[5]))
		for i, cap := range caps {
			if enabled[i] {
				g.call("glEnable", cap)
			} else {
				g.call("glDisable", cap)
			}
		}
		g.call("glDeleteTextures", 2, uintptr(unsafe.Pointer(&textures[0])))
		g.call("glDeleteFramebuffers", 1, uintptr(unsafe.Pointer(&framebuffer)))
	}()
	g.call("glBindBuffer", 0x88eb, 0)
	g.call("glBindBuffer", 0x88ec, 0)
	for _, key := range pixelKeys {
		value := uintptr(0)
		if key == 0x0d05 || key == 0x0cf5 {
			value = 1
		}
		g.call("glPixelStorei", key, value)
	}
	g.call("glGenTextures", 2, uintptr(unsafe.Pointer(&textures[0])))
	for i, texture := range textures {
		g.call("glBindTexture", 0x0de1, uintptr(texture))
		pointer := uintptr(0)
		if i == 0 {
			pointer = uintptr(unsafe.Pointer(&input[0]))
		}
		g.call("glTexImage2D", 0x0de1, 0, 0x8058, uintptr(width), uintptr(height), 0, 0x1908, 0x1401, pointer)
		g.call("glTexParameteri", 0x0de1, 0x2801, 0x2601)
		g.call("glTexParameteri", 0x0de1, 0x2800, 0x2601)
		g.call("glTexParameteri", 0x0de1, 0x2802, 0x812f)
		g.call("glTexParameteri", 0x0de1, 0x2803, 0x812f)
	}
	runtime.KeepAlive(input)
	g.call("glGenFramebuffers", 1, uintptr(unsafe.Pointer(&framebuffer)))
	g.call("glBindFramebuffer", 0x8d40, uintptr(framebuffer))
	g.call("glFramebufferTexture2D", 0x8d40, 0x8ce0, 0x0de1, uintptr(textures[1]), 0)
	if g.call("glCheckFramebufferStatus", 0x8d40) != 0x8cd5 {
		return nil, fmt.Errorf("preview framebuffer is incomplete")
	}
	frame := Frame{Width: width, Height: height, Area: geometry.Rect{W: width, H: height}, SourceUV: geometry.UVRect{W: 1, H: 1}, Texture: textures[0], SourceSize: geometry.Size{W: width, H: height}, Time: seconds, Effects: c.Effective(), Screen: c.EffectiveScreen()}
	if c.Shader.ID != "" {
		if len(custom) == 0 || custom[0] == nil || custom[0].ID != c.Shader.ID {
			return nil, fmt.Errorf("preview shader is not prepared")
		}
		frame.Shader, frame.ShaderParams = custom[0], c.Shader.Params
	}
	if c.Mode == "overlay" {
		base := frame
		base.Effects = config.Effects{}
		base.Screen = config.Screen{Shape: config.ShapeFlat}
		r.draw(base, true, false)
		g.call("glEnable", 0x0be2)
		g.call("glBlendFuncSeparate", 1, 0x0303, 1, 0x0303)
		g.call("glBlendEquationSeparate", 0x8006, 0x8006)
		r.draw(frame, false, false)
	} else {
		r.draw(frame, true, false)
	}
	pixels := make([]byte, width*height*4)
	g.call("glReadBuffer", 0x8ce0)
	g.call("glReadPixels", 0, 0, uintptr(width), uintptr(height), 0x1908, 0x1401, uintptr(unsafe.Pointer(&pixels[0])))
	runtime.KeepAlive(pixels)
	if err = r.Error(); err != nil {
		return nil, fmt.Errorf("preview render: %w", err)
	}
	stride := width * 4
	row := make([]byte, stride)
	for y := 0; y < height/2; y++ {
		a, b := y*stride, (height-1-y)*stride
		copy(row, pixels[a:a+stride])
		copy(pixels[a:a+stride], pixels[b:b+stride])
		copy(pixels[b:b+stride], row)
	}
	return pixels, nil
}
