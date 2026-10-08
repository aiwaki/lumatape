// Exports the exact production wrapper for optional native CGL validation.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aiwaki/lumatape/internal/render"
	"github.com/aiwaki/lumatape/internal/shaderpack"
)

func main() {
	out := flag.String("out", "artifacts/customization-validation", "generated fixture directory")
	flag.Parse()
	if err := os.MkdirAll(*out, 0755); err != nil {
		panic(err)
	}
	paths, err := filepath.Glob("examples/shaders/*.lumatape.glsl")
	if err != nil {
		panic(err)
	}
	for _, path := range paths {
		b, e := os.ReadFile(path)
		if e != nil {
			panic(e)
		}
		p, e := shaderpack.Parse(string(b))
		if e != nil {
			panic(e)
		}
		name := strings.TrimSuffix(filepath.Base(path), ".lumatape.glsl")
		if e = os.WriteFile(filepath.Join(*out, name+".frag"), []byte(render.CustomFragment(p.Body)), 0644); e != nil {
			panic(e)
		}
		metadata, e := json.MarshalIndent(p.Descriptor, "", "  ")
		if e != nil {
			panic(e)
		}
		if e = os.WriteFile(filepath.Join(*out, name+".json"), metadata, 0644); e != nil {
			panic(e)
		}
		var params strings.Builder
		for _, value := range p.Defaults() {
			fmt.Fprintf(&params, "%.17g\n", value)
		}
		if e = os.WriteFile(filepath.Join(*out, name+".params"), []byte(params.String()), 0644); e != nil {
			panic(e)
		}
		fmt.Println(name, p.ID)
	}
}
