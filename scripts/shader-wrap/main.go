// shader-wrap writes the exact runtime wrapper for an imported v1 shader. It is
// a developer validation tool, not an alternate import or activation path.
package main

import (
	"flag"
	"fmt"
	"github.com/aiwaki/lumatape/internal/render"
	"github.com/aiwaki/lumatape/internal/shaderpack"
	"os"
)

func main() {
	input := flag.String("input", "", "LumaTape shader asset")
	output := flag.String("output", "", "wrapped GLSL fragment path")
	flag.Parse()
	if *input == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "--input and --output are required")
		os.Exit(2)
	}
	source, err := os.ReadFile(*input)
	if err != nil {
		fatal(err)
	}
	pack, err := shaderpack.Parse(string(source))
	if err != nil {
		fatal(err)
	}
	if err = os.WriteFile(*output, []byte(render.CustomFragment(pack.Body)), 0600); err != nil {
		fatal(err)
	}
}
func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
