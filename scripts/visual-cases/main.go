// visual-cases emits reproducible real-renderer cases, not fabricated images.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/aiwaki/lumatape/internal/config"
	"os"
	"strings"
)

func main() {
	output := flag.String("output", "artifacts/polish-validation/visual-cases.json", "case manifest output")
	flag.Parse()
	type visualCase struct {
		Name   string        `json:"name"`
		Config config.Config `json:"config"`
	}
	cases := []visualCase{}
	for _, p := range config.PresetNames {
		for _, shape := range config.ScreenShapes {
			for _, intensity := range []float64{0, .5, 1} {
				c := config.Default()
				c.Mode = "full"
				c.Capture.Transfer = config.TransferCompatibility
				c.Target = config.Target{Kind: "window", WindowTitle: "LumaTape test card"}
				c.Effects, _ = config.Preset(p)
				c.Preset = p
				c.Effects.Intensity = intensity
				c.Effects.FreezeNoise = true
				c.Effects.NoiseSeed = 173
				c.Screen.Shape = shape
				if shape == config.ShapeConvex {
					c.InputMode = "keyboard-gamepad"
					c.Aspect.Enabled = true
				}
				if err := c.Validate(); err != nil {
					panic(err)
				}
				cases = append(cases, visualCase{fmt.Sprintf("%s-%s-%03.0f", strings.ReplaceAll(strings.ToLower(p), " ", "-"), shape, intensity*100), c})
			}
		}
	}
	b, err := json.MarshalIndent(cases, "", "  ")
	if err != nil {
		panic(err)
	}
	if err = os.WriteFile(*output, b, 0644); err != nil {
		panic(err)
	}
	fmt.Printf("%d cases → %s\n", len(cases), *output)
}
