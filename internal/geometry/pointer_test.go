package geometry

import (
	"math"
	"math/rand"
	"testing"
)

func pointerIdentity(w, h int) PointerMap {
	return PointerMap{Bounds: Rect{0, 0, w, h}, Area: Rect{0, 0, w, h},
		SourceUV: UVRect{0, 0, 1, 1}, SourceSize: Size{w, h}}
}

func assertPointerNear(t *testing.T, got, want PointerPoint) {
	t.Helper()
	if math.Abs(got.X-want.X) > 1e-7 || math.Abs(got.Y-want.Y) > 1e-7 {
		t.Fatalf("point %+v; want %+v", got, want)
	}
}

func TestPointerIdentityAndPixelCenters(t *testing.T) {
	m := pointerIdentity(1280, 720)
	m.Bounds.X, m.Bounds.Y = -1500, 117
	for _, source := range []PointerPoint{{0, 0}, {1279, 719}, {640, 360}, {12.25, 97.75}} {
		screen := PointerPoint{source.X - 1500, source.Y + 117}
		got, ok := m.MapScreen(screen.X, screen.Y)
		if !ok {
			t.Fatalf("identity rejected %+v", screen)
		}
		assertPointerNear(t, got, source)
		back, ok := m.SourceToScreen(source.X, source.Y)
		if !ok {
			t.Fatalf("inverse rejected %+v", source)
		}
		assertPointerNear(t, back, screen)
	}
	// Twice the output density: output pixel zero samples before the first
	// source texel's center and must clamp, just like sampleImage in the shader.
	m = pointerIdentity(200, 100)
	m.SourceSize = Size{100, 50}
	for _, tc := range []struct{ screen, source PointerPoint }{
		{PointerPoint{0, 0}, PointerPoint{0, 0}},
		{PointerPoint{1, 1}, PointerPoint{.25, .25}},
		{PointerPoint{100, 50}, PointerPoint{49.75, 24.75}},
		{PointerPoint{199, 99}, PointerPoint{99, 49}},
	} {
		got, ok := m.MapScreen(tc.screen.X, tc.screen.Y)
		if !ok {
			t.Fatalf("scaled map rejected %+v", tc.screen)
		}
		assertPointerNear(t, got, tc.source)
	}
	back, ok := m.SourceToScreen(0, 0)
	if !ok {
		t.Fatal("first source texel must have a canonical center preimage")
	}
	assertPointerNear(t, back, PointerPoint{.5, .5})

	m = pointerIdentity(1, 1)
	got, ok := m.MapScreen(0, 0)
	if !ok || got != (PointerPoint{}) {
		t.Fatalf("single pixel: %+v, %v", got, ok)
	}
}

func TestPointerConvexShaderFormula(t *testing.T) {
	m := pointerIdentity(100, 100)
	m.Curvature = .5
	// Fragment p=(75,25), centered=(.5,-.5), factor=1+.5*.09*.5.
	// Its sampled UV is (.755625,.244375), before the texel-center conversion.
	got, ok := m.MapScreen(74.5, 24.5)
	if !ok {
		t.Fatal("visible convex pixel rejected")
	}
	assertPointerNear(t, got, PointerPoint{75.0625, 23.9375})
	back, ok := m.SourceToScreen(75.0625, 23.9375)
	if !ok {
		t.Fatal("visible source pixel rejected")
	}
	assertPointerNear(t, back, PointerPoint{74.5, 24.5})
	center, ok := m.MapScreen(49.5, 49.5)
	if !ok {
		t.Fatal("screen center rejected")
	}
	assertPointerNear(t, center, PointerPoint{49.5, 49.5})
	for _, p := range []PointerPoint{{0, 0}, {99, 99}, {0, 49}, {99, 49}} {
		if _, ok := m.MapScreen(p.X, p.Y); ok {
			t.Errorf("accepted black warped edge %+v", p)
		}
	}
}

func TestPointerNegativeMonitorAndPhysicalDPISize(t *testing.T) {
	m := pointerIdentity(2560, 1440)
	m.Bounds.X, m.Bounds.Y = -2560, -400
	m.SourceSize = Size{1280, 720}
	got, ok := m.MapScreen(-2000, 100)
	if !ok {
		t.Fatal("negative monitor pixel rejected")
	}
	assertPointerNear(t, got, PointerPoint{279.75, 249.75})
	back, ok := m.SourceToScreen(got.X, got.Y)
	if !ok {
		t.Fatal("physical DPI inverse rejected")
	}
	assertPointerNear(t, back, PointerPoint{-2000, 100})
}

func TestPointerAspectMaskFitCropAndStretch(t *testing.T) {
	m := pointerIdentity(1920, 1080)
	m.Bounds.X, m.Bounds.Y = -1920, 50
	mask, err := Centered4x3(Rect{0, 0, 1920, 1080})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []ScaleMode{Fit, Crop, Stretch} {
		t.Run(string(mode), func(t *testing.T) {
			layout, err := Layout(mask, m.SourceSize, 0, mode)
			if err != nil {
				t.Fatal(err)
			}
			mapped := m
			mapped.Area, mapped.SourceUV = layout.Viewport, layout.SourceUV
			center, ok := mapped.MapScreen(-960.5, 589.5)
			if !ok {
				t.Fatal("aspect center rejected")
			}
			assertPointerNear(t, center, PointerPoint{959.5, 539.5})
			if _, ok := mapped.MapScreen(-1800, 590); ok {
				t.Fatal("4:3 side mask must reject pointer input")
			}
			switch mode {
			case Fit:
				if _, ok := mapped.MapScreen(-960, 100); ok {
					t.Fatal("fit letterbox must reject pointer input")
				}
				got, ok := mapped.MapScreen(-1680, 185)
				if !ok {
					t.Fatal("fit first pixel rejected")
				}
				assertPointerNear(t, got, PointerPoint{1.0 / 6, 1.0 / 6})
			case Crop:
				got, ok := mapped.MapScreen(-1680, 50)
				if !ok {
					t.Fatal("crop first visible pixel rejected")
				}
				assertPointerNear(t, got, PointerPoint{240, 0})
				if _, ok := mapped.SourceToScreen(100, 500); ok {
					t.Fatal("cropped-away source pixel must have no screen position")
				}
			case Stretch:
				got, ok := mapped.MapScreen(-1680, 50)
				if !ok {
					t.Fatal("stretch first pixel rejected")
				}
				assertPointerNear(t, got, PointerPoint{1.0 / 6, 0})
			}
		})
	}
	// Portrait crop exercises Y/height, not just the horizontal crop path.
	m = pointerIdentity(400, 300)
	m.SourceSize = Size{300, 600}
	layout, err := Layout(m.Area, m.SourceSize, 0, Crop)
	if err != nil {
		t.Fatal(err)
	}
	m.SourceUV = layout.SourceUV
	got, ok := m.MapScreen(199.5, 0)
	if !ok {
		t.Fatal("portrait crop rejected")
	}
	assertPointerNear(t, got, PointerPoint{149.5, 187.375})
}

func TestPointerRoundedCoverageAndBoundaries(t *testing.T) {
	m := pointerIdentity(100, 100)
	m.CornerRadius = .2
	for _, p := range []PointerPoint{{0, 0}, {99, 0}, {0, 99}, {99, 99}, {7.5, 3.5}} {
		// p=(7.5,3.5) names fragment (8,4): the 12-16-20 corner triangle
		// lies exactly at signed distance zero / coverage 0.5.
		if _, ok := m.MapScreen(p.X, p.Y); ok {
			t.Errorf("accepted rounded coverage <= 0.5 at %+v", p)
		}
		if _, ok := m.SourceToScreen(p.X, p.Y); ok {
			t.Errorf("inverse accepted invisible rounded point %+v", p)
		}
	}
	for _, p := range []PointerPoint{{7.6, 3.5}, {49, 0}, {99, 49}, {20, 20}} {
		if _, ok := m.MapScreen(p.X, p.Y); !ok {
			t.Errorf("rejected covered pixel %+v", p)
		}
	}
	m.CornerRadius = 0
	for _, p := range []PointerPoint{{-1, 50}, {100, 50}, {50, -1}, {50, 100}, {99.5, 50}} {
		if _, ok := m.MapScreen(p.X, p.Y); ok {
			t.Errorf("accepted outside framebuffer %+v", p)
		}
	}
	// Subpixel inputs name centers too: -0.5 is the physical left edge,
	// whereas 99.5 is the excluded physical right edge of a 100-pixel client.
	if _, ok := m.MapScreen(-.5, 50); !ok {
		t.Fatal("flat shader includes the left image edge")
	}
}

func TestPointerJitterTrackingAndInverse(t *testing.T) {
	m := pointerIdentity(1000, 1000)
	m.Jitter, m.Tracking, m.Time = .4, .7, 11
	// At final q.y=.3 the time=11 tracking band reaches its full amplitude.
	wantX := 499.5 + 1000*(.4*.002*math.Sin(11*3.7+.3*9)+.7*.012)
	got, ok := m.MapScreen(499.5, 299.5)
	if !ok {
		t.Fatal("temporal sample rejected")
	}
	assertPointerNear(t, got, PointerPoint{wantX, 299.5})
	back, ok := m.SourceToScreen(got.X, got.Y)
	if !ok {
		t.Fatal("temporal inverse rejected")
	}
	assertPointerNear(t, back, PointerPoint{499.5, 299.5})

	for _, time := range []float64{0, 10.799, 12} {
		m.Jitter, m.Time = 0, time
		got, ok := m.MapScreen(499.5, 299.5)
		if !ok {
			t.Fatal("inactive tracking sample rejected")
		}
		assertPointerNear(t, got, PointerPoint{499.5, 299.5})
	}
	// GLSL mod(-1,12)=11 and fract(-.3)=.7: negative time is also defined.
	m.Time = -1
	got, ok = m.MapScreen(499.5, 699.5)
	if !ok {
		t.Fatal("negative-time tracking sample rejected")
	}
	assertPointerNear(t, got, PointerPoint{507.9, 699.5})

	// Full tracking at the right edge shifts the sample into opaque black.
	m.Time = 11
	if _, ok := m.MapScreen(999, 299.5); ok {
		t.Fatal("tracking-shifted black edge accepted")
	}
}

func TestPointerRejectsInvalidMappingsAndCoordinates(t *testing.T) {
	base := pointerIdentity(100, 100)
	for name, change := range map[string]func(*PointerMap){
		"zero bounds":       func(m *PointerMap) { m.Bounds.W = 0 },
		"negative area":     func(m *PointerMap) { m.Area.X = -1 },
		"escaped area":      func(m *PointerMap) { m.Area.X = 1 },
		"zero source":       func(m *PointerMap) { m.SourceSize.H = 0 },
		"empty crop":        func(m *PointerMap) { m.SourceUV.W = 0 },
		"negative crop":     func(m *PointerMap) { m.SourceUV.X = -.1 },
		"escaped crop":      func(m *PointerMap) { m.SourceUV.W = 1.1 },
		"nonfinite crop":    func(m *PointerMap) { m.SourceUV.H = math.NaN() },
		"negative warp":     func(m *PointerMap) { m.Curvature = -.1 },
		"nonfinite warp":    func(m *PointerMap) { m.Curvature = math.Inf(1) },
		"nonfinite corners": func(m *PointerMap) { m.CornerRadius = math.NaN() },
		"nonfinite jitter":  func(m *PointerMap) { m.Jitter = math.NaN() },
		"large tracking":    func(m *PointerMap) { m.Tracking = 2 },
		"nonfinite time":    func(m *PointerMap) { m.Time = math.Inf(-1) },
	} {
		t.Run(name, func(t *testing.T) {
			m := base
			change(&m)
			if _, ok := m.MapScreen(50, 50); ok {
				t.Fatal("invalid forward mapping accepted")
			}
			if _, ok := m.SourceToScreen(50, 50); ok {
				t.Fatal("invalid inverse mapping accepted")
			}
		})
	}
	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		for _, p := range []PointerPoint{{bad, 1}, {1, bad}} {
			if _, ok := base.MapScreen(p.X, p.Y); ok {
				t.Errorf("accepted nonfinite screen point %+v", p)
			}
			if _, ok := base.SourceToScreen(p.X, p.Y); ok {
				t.Errorf("accepted nonfinite source point %+v", p)
			}
		}
	}
	for _, p := range []PointerPoint{{-1, 50}, {100, 50}, {50, -1}, {50, 100}} {
		if _, ok := base.SourceToScreen(p.X, p.Y); ok {
			t.Errorf("accepted outside source %+v", p)
		}
	}
}

func TestPointerDeterministicRandomRoundTrips(t *testing.T) {
	random := rand.New(rand.NewSource(0x435254))
	checked := 0
	for scene := 0; scene < 32; scene++ {
		w, h := 600+random.Intn(2500), 400+random.Intn(1300)
		xpad, ypad := random.Intn(100), random.Intn(100)
		cropX, cropY := random.Float64()*.2, random.Float64()*.2
		m := PointerMap{
			Bounds: Rect{random.Intn(10000) - 8000, random.Intn(4000) - 2000, w, h},
			Area:   Rect{xpad, ypad, w - 2*xpad, h - 2*ypad}, SourceSize: Size{512 + random.Intn(3000), 512 + random.Intn(2000)},
			SourceUV:  UVRect{cropX, cropY, 1 - 2*cropX, 1 - 2*cropY},
			Curvature: random.Float64(), CornerRadius: random.Float64() * .2,
			Jitter: random.Float64(), Tracking: random.Float64(), Time: 10.8 + random.Float64()*1.2,
		}
		for sample := 0; sample < 80; sample++ {
			screen := PointerPoint{
				float64(m.Bounds.X+m.Area.X) + random.Float64()*float64(m.Area.W) - .5,
				float64(m.Bounds.Y+m.Area.Y) + random.Float64()*float64(m.Area.H) - .5,
			}
			source, ok := m.MapScreen(screen.X, screen.Y)
			if !ok || source.X == 0 || source.Y == 0 || source.X == float64(m.SourceSize.W-1) || source.Y == float64(m.SourceSize.H-1) {
				continue // Opaque black or the explicitly non-invertible texel clamp.
			}
			back, ok := m.SourceToScreen(source.X, source.Y)
			if !ok {
				t.Fatalf("scene %d: inverse rejected visible source %+v", scene, source)
			}
			assertPointerNear(t, back, screen)
			again, ok := m.MapScreen(back.X, back.Y)
			if !ok {
				t.Fatalf("scene %d: forward rejected inverse result %+v", scene, back)
			}
			assertPointerNear(t, again, source)
			checked++
		}
	}
	if checked < 1500 {
		t.Fatalf("too few visible round trips: %d", checked)
	}
}
