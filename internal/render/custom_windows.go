//go:build windows

package render

import (
	"fmt"
	"unsafe"

	"github.com/aiwaki/lumatape/internal/shaderpack"
)

// ShaderProgram owns one GL program on the renderer thread. A candidate never
// replaces any current program implicitly; callers commit an explicit pointer.
type ShaderProgram struct {
	ID       string
	Metadata shaderpack.Descriptor
	program  Program
	gl       *API
}

func (r *Renderer) CompileShader(pack shaderpack.Pack) (*ShaderProgram, error) {
	if r == nil || r.gl == nil {
		return nil, fmt.Errorf("renderer is closed")
	}
	p, err := r.gl.program(Vertex, CustomFragment(pack.Body))
	if err != nil {
		return nil, err
	}
	return &ShaderProgram{ID: pack.ID, Metadata: pack.Descriptor, program: p, gl: r.gl}, nil
}
func (p *ShaderProgram) Close() {
	if p != nil && p.program.id != 0 {
		var current int32
		p.gl.call("glGetIntegerv", 0x8b8d, uintptr(unsafe.Pointer(&current)))
		if uint32(current) == p.program.id {
			p.gl.call("glUseProgram", 0)
		}
		p.gl.call("glDeleteProgram", uintptr(p.program.id))
		p.program.id = 0
	}
}
