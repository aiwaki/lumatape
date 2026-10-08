package config

import (
	"testing"

	"github.com/aiwaki/lumatape/internal/geometry"
)

func TestCombinationMatrixRejectsUnsafeInputBeforeRendering(t *testing.T) {
	count := 0
	for _, mode := range []string{"overlay", "full"} {
		for _, target := range []string{"monitor", "window"} {
			for _, input := range []string{"mouse-exact", "keyboard-gamepad"} {
				for _, shape := range []string{"flat", "rounded", "convex"} {
					for _, enabled := range []bool{false, true} {
						for _, method := range []string{"mask", "window", "system"} {
							for _, scale := range []geometry.ScaleMode{geometry.Fit, geometry.Crop, geometry.Stretch} {
								for _, dar := range []float64{0, 4.0 / 3.0} {
									for _, transfer := range []string{TransferGPU, TransferCompatibility} {
										c := Default()
										c.Mode, c.Target.Kind, c.Target.WindowTitle = mode, target, "test"
										c.InputMode, c.Screen.Shape, c.Capture.Transfer = input, shape, transfer
										c.Aspect.Enabled, c.Aspect.Method, c.Aspect.Scale, c.Aspect.SourceDAR = enabled, method, scale, dar
										wantValid := (mode != "full" || target == "window") &&
											(input != "mouse-exact" || (scale == geometry.Fit && dar == 0)) &&
											(!enabled || method != "window" || target == "window") &&
											(shape != "convex" || mode == "full")
										// Disabling the filter must not turn an unsafe saved
										// combination into a valid one. Later toggle-on must
										// not expose a configuration validation skipped.
										for _, filterEnabled := range []bool{false, true} {
											c.Enabled = filterEnabled
											if err := c.Validate(); (err == nil) != wantValid {
												t.Fatalf("filter_enabled=%v mode=%s target=%s input=%s shape=%s format=%v/%s scale=%s DAR=%g transfer=%s: %v", filterEnabled, mode, target, input, shape, enabled, method, scale, dar, transfer, err)
											}
											count++
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
	if count != 3456 {
		t.Fatalf("matrix lost cases: %d", count)
	}
	t.Logf("validated %d combinations across independent filter_enabled=false/true states", count)
}

func TestInvalidReloadCannotPassJSONValidation(t *testing.T) {
	for _, data := range []string{
		`{"mode":"full"}`,
		`{"mode":"full","target":{"kind":"window","window_title":"test"},"aspect":{"scale":"crop"}}`,
		`{"aspect":{"enabled":true,"method":"window"}}`,
		`{"screen":{"shape":"convex"}}`,
	} {
		if _, err := Decode([]byte(data)); err == nil {
			t.Fatalf("invalid transition accepted by JSON: %s", data)
		}
	}
}
