package geometry

import (
	"math"
	"testing"
)

func TestCentered4x3(t *testing.T) {
	for _, tc := range []struct {
		name         string
		bounds, want Rect
	}{
		{"1080p", Rect{0, 0, 1920, 1080}, Rect{240, 0, 1440, 1080}},
		{"negative monitor", Rect{-2560, -200, 2560, 1440}, Rect{-2240, -200, 1920, 1440}},
		{"5:4 is not 4:3", Rect{0, 0, 1280, 1024}, Rect{0, 32, 1280, 960}},
		{"portrait", Rect{0, 0, 1080, 1920}, Rect{0, 555, 1080, 810}},
		{"odd padding", Rect{0, 0, 1921, 1080}, Rect{240, 0, 1440, 1080}},
		{"one pixel", Rect{-1, -1, 1, 1}, Rect{-1, -1, 1, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Centered4x3(tc.bounds)
			if err != nil || got != tc.want {
				t.Fatalf("got %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
}

func TestFitCropStretch(t *testing.T) {
	box := Rect{240, 0, 1440, 1080}
	source := Size{1920, 1080}
	fit, err := Layout(box, source, 0, Fit)
	if err != nil || fit.Viewport != (Rect{240, 135, 1440, 810}) || fit.SourceUV != (UVRect{0, 0, 1, 1}) {
		t.Fatalf("16:9 fit must letterbox inside 4:3: %+v, %v", fit, err)
	}
	crop, err := Layout(box, source, 0, Crop)
	if err != nil || crop.Viewport != box || crop.SourceUV != (UVRect{0.125, 0, 0.75, 1}) {
		t.Fatalf("16:9 crop should remove outer eighths: %+v, %v", crop, err)
	}
	stretch, err := Layout(box, source, 0, Stretch)
	if err != nil || stretch.Viewport != box || stretch.SourceUV != (UVRect{0, 0, 1, 1}) {
		t.Fatalf("explicit stretch: %+v, %v", stretch, err)
	}
	portrait, err := Layout(box, Size{1080, 1920}, 0, Crop)
	wantH := (1080.0 / 1920.0) / (4.0 / 3.0)
	if err != nil || portrait.SourceUV.H != wantH || portrait.SourceUV.Y != (1-wantH)/2 {
		t.Fatalf("portrait crop: %+v, %v", portrait, err)
	}
}

func TestDisplayAspectIsExplicit(t *testing.T) {
	box := Rect{0, 0, 1440, 1080}
	physical, _ := Layout(box, Size{320, 200}, 0, Fit)
	corrected, _ := Layout(box, Size{320, 200}, 4.0/3.0, Fit)
	if physical.Viewport != (Rect{0, 90, 1440, 900}) || corrected.Viewport != box {
		t.Fatalf("DAR independent of framebuffer: physical=%+v corrected=%+v", physical, corrected)
	}
	fiveByFour, _ := Layout(box, Size{1280, 1024}, 0, Fit)
	if fiveByFour.Viewport != (Rect{45, 0, 1350, 1080}) {
		t.Fatalf("5:4 source was incorrectly interpreted as 4:3: %+v", fiveByFour)
	}
}

func TestMouseAlignment(t *testing.T) {
	source := Rect{-1700, 50, 1280, 960}
	mapping, _ := Layout(source, source.Size(), 0, Fit)
	if !IsMouseAligned(source, mapping, 0) {
		t.Fatal("identity mapping should preserve clicks")
	}
	if IsMouseAligned(source, mapping, 0.001) || IsMouseAligned(source, mapping, math.NaN()) {
		t.Fatal("curvature must invalidate exact mouse alignment")
	}
	scaled, _ := Layout(Rect{-1600, 50, 1440, 1080}, source.Size(), 0, Fit)
	if IsMouseAligned(source, scaled, 0) {
		t.Fatal("click-through does not remap shifted/scaled coordinates")
	}
	cropped, _ := Layout(source, Size{1920, 1080}, 0, Crop)
	if IsMouseAligned(source, cropped, 0) {
		t.Fatal("cropping must invalidate exact clicks")
	}
	if IsMouseAligned(Rect{}, Mapping{}, 0) {
		t.Fatal("empty mapping is invalid")
	}
}

func TestRejectPathologicalBounds(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, r := range []Rect{
		{0, 0, 0, 1080}, {0, 0, 1920, -1}, {0, 0, MaxDimension + 1, 1},
		{maxInt, 0, 2, 2}, {0, maxInt - 1, 2, 2},
	} {
		if _, err := Centered4x3(r); err == nil {
			t.Errorf("accepted %+v", r)
		}
	}
	for _, aspect := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := Layout(Rect{0, 0, 10, 10}, Size{1, 1}, aspect, Fit); err == nil {
			t.Errorf("accepted aspect %v", aspect)
		}
	}
	if _, err := Layout(Rect{0, 0, 10, 10}, Size{}, 0, Fit); err == nil {
		t.Fatal("accepted empty source")
	}
	if _, err := Layout(Rect{0, 0, 10, 10}, Size{1, 1}, 0, "resize"); err == nil {
		t.Fatal("accepted unknown fit mode")
	}
	// The most negative origin is safe: all additions move toward zero.
	if _, err := Centered4x3(Rect{-maxInt - 1, -maxInt - 1, MaxDimension, MaxDimension}); err != nil {
		t.Fatal(err)
	}
	for _, aspect := range []float64{math.SmallestNonzeroFloat64, math.MaxFloat64} {
		got, err := CenteredAspect(Rect{0, 0, 100, 100}, aspect)
		if err != nil || got.W < 1 || got.H < 1 || got.W > 100 || got.H > 100 {
			t.Fatalf("extreme finite aspect overflow: %+v %v", got, err)
		}
	}
	if _, err := Layout(Rect{0, 0, MaxDimension, 1}, Size{1, 1}, math.SmallestNonzeroFloat64, Crop); err == nil {
		t.Fatal("crop underflow must fail instead of returning an empty texture region")
	}
}

func FuzzLayout(f *testing.F) {
	f.Add(-1920, 0, 1920, 1080, 1280, 1024, 0.0)
	f.Add(0, 0, 1, 1, 320, 200, 4.0/3.0)
	f.Fuzz(func(t *testing.T, x, y, w, h, sw, sh int, aspect float64) {
		bounds := Rect{x, y, w, h}
		m, err := Layout(bounds, Size{sw, sh}, aspect, Fit)
		if err != nil {
			return
		}
		if err := m.Viewport.Validate(); err != nil {
			t.Fatal(err)
		}
		if m.Viewport.X < x || m.Viewport.Y < y || m.Viewport.X+m.Viewport.W > x+w || m.Viewport.Y+m.Viewport.H > y+h {
			t.Fatalf("viewport escaped bounds: %+v in %+v", m, bounds)
		}
	})
}
