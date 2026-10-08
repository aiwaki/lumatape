package app

import (
	"fmt"
	"testing"

	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/geometry"
)

func TestMouseExactMaskDoesNotResample(t *testing.T) {
	c := config.Default()
	c.Mode = "full"
	c.Aspect.Enabled = true
	s := geometry.Rect{X: -1920, Y: 0, W: 1920, H: 1080}
	p, e := calculatePresentation(c, s, s)
	if e != nil {
		t.Fatal(e)
	}
	if p.Bounds != s || p.Area != (geometry.Rect{X: 240, W: 1440, H: 1080}) || p.SourceUV != (geometry.UVRect{X: .125, W: .75, H: 1}) {
		t.Fatalf("pixels moved: %+v", p)
	}
	// Each output pixel samples the identical source pixel, even with a mask.
	if float64(p.Area.W)/float64(s.W) != p.SourceUV.W {
		t.Fatal("unexpected scale")
	}
}
func TestMouseExactRejectsDARAndCrop(t *testing.T) {
	c := config.Default()
	c.Mode = "full"
	c.Aspect.SourceDAR = 4.0 / 3
	r := geometry.Rect{W: 320, H: 200}
	if _, e := calculatePresentation(c, r, r); e == nil {
		t.Fatal("DAR silently moved mouse targets")
	}
}
func TestWindow43KeepsDecorationsAndDesktopUncovered(t *testing.T) {
	c := config.Default()
	c.Aspect.Enabled = true
	c.Aspect.Method = "window"
	for _, mode := range []string{"overlay", "full"} {
		c.Mode = mode
		for _, x := range []int{-2100, -1500, 250} {
			m := geometry.Rect{X: -1920, Y: -200, W: 1920, H: 1080}
			s := geometry.Rect{X: x, Y: -160, W: 1280, H: 960}
			p, e := calculatePresentation(c, s, m)
			if e != nil {
				t.Fatal(e)
			}
			if p.Bounds != s || p.Area != (geometry.Rect{W: s.W, H: s.H}) || p.SourceUV != (geometry.UVRect{W: 1, H: 1}) {
				t.Fatalf("mode %s client %+v covered outside pixels: %+v", mode, s, p)
			}
		}
	}
}
func TestGamepadFitDoesNotPretend169Is43(t *testing.T) {
	c := config.Default()
	c.Mode = "full"
	c.InputMode = "keyboard-gamepad"
	c.Aspect.Enabled = true
	m := geometry.Rect{W: 1920, H: 1080}
	p, e := calculatePresentation(c, m, m)
	if e != nil {
		t.Fatal(e)
	}
	if p.Area != (geometry.Rect{X: 240, Y: 135, W: 1440, H: 810}) {
		t.Fatalf("source was stretched: %+v", p)
	}
}

func TestInputModeAloneDoesNotMoveWindowImage(t *testing.T) {
	c := config.Default()
	c.Mode = "full"
	source := geometry.Rect{X: 750, Y: 502, W: 960, H: 720}
	monitor := geometry.Rect{W: 3024, H: 1890}
	mouse, err := calculatePresentation(c, source, monitor)
	if err != nil {
		t.Fatal(err)
	}
	c.InputMode = "keyboard-gamepad"
	gamepad, err := calculatePresentation(c, source, monitor)
	if err != nil {
		t.Fatal(err)
	}
	if gamepad != mouse {
		t.Fatalf("changing input mode moved visible controls away from their real client: mouse=%+v gamepad=%+v", mouse, gamepad)
	}
}

// Rendering never creates an interactive fullscreen window: it is a
// click-through surface above the actual source client. Every supported layout
// must stay within that client, including a window crossing monitor boundaries.
func TestGamepadPresentationStaysInsideSourceClient(t *testing.T) {
	monitor := geometry.Rect{X: -1920, Y: -200, W: 1920, H: 1080}
	for _, source := range []geometry.Rect{
		{X: 200, Y: 100, W: 320, H: 200},
		{X: -1800, Y: -100, W: 640, H: 480},
		{X: -2100, Y: -160, W: 960, H: 720},
		{X: -1500, Y: -160, W: 1280, H: 1024},
		monitor, // borderless is still bounded by its real client
	} {
		for _, method := range []string{"mask", "window", "system"} {
			for _, scale := range []geometry.ScaleMode{geometry.Fit, geometry.Crop, geometry.Stretch} {
				for _, dar := range []float64{0, 4.0 / 3} {
					for _, enabled := range []bool{false, true} {
						name := fmt.Sprintf("%v/%s/%s/dar=%g/aspect=%t", source, method, scale, dar, enabled)
						t.Run(name, func(t *testing.T) {
							c := config.Default()
							c.Mode, c.InputMode = "full", "keyboard-gamepad"
							c.Aspect.Method, c.Aspect.Scale, c.Aspect.SourceDAR, c.Aspect.Enabled = method, scale, dar, enabled
							p, err := calculatePresentation(c, source, monitor)
							if err != nil {
								t.Fatal(err)
							}
							if p.Bounds != source || p.Area.X < 0 || p.Area.Y < 0 || p.Area.W < 1 || p.Area.H < 1 || p.Area.X+p.Area.W > source.W || p.Area.Y+p.Area.H > source.H {
								t.Fatalf("presentation exposes desktop outside the source client: %+v", p)
							}
							uv := p.SourceUV
							if uv.X < 0 || uv.Y < 0 || uv.W <= 0 || uv.H <= 0 || uv.X+uv.W > 1 || uv.Y+uv.H > 1 {
								t.Fatalf("layout samples outside the source image: %+v", p)
							}
							if scale != geometry.Crop && uv != (geometry.UVRect{W: 1, H: 1}) {
								t.Fatalf("non-crop layout discarded source pixels: %+v", p)
							}
						})
					}
				}
			}
		}
	}
}
