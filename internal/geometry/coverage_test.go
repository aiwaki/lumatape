package geometry

import (
	"math/rand"
	"testing"
)

func TestDisplayCoverage(t *testing.T) {
	for _, test := range []struct {
		name     string
		bounds   Rect
		displays []Rect
		want     bool
	}{
		{"one monitor", Rect{0, 0, 1920, 1080}, []Rect{{0, 0, 1920, 1080}}, true},
		{"one pixel offscreen", Rect{-1, 0, 1920, 1080}, []Rect{{0, 0, 1920, 1080}}, false},
		{"negative monitor", Rect{-1900, -180, 960, 720}, []Rect{{-1920, -200, 1920, 1080}}, true},
		{"across touching monitors", Rect{-500, 100, 1000, 720}, []Rect{{-1920, 0, 1920, 1080}, {0, 0, 2560, 1440}}, true},
		{"monitor gap", Rect{-500, 100, 1000, 720}, []Rect{{-1920, 0, 1919, 1080}, {0, 0, 2560, 1440}}, false},
		{"unequal heights", Rect{-500, 1000, 1000, 400}, []Rect{{-1920, 0, 1920, 1080}, {0, 0, 2560, 1440}}, false},
		{"overlapping monitors", Rect{-100, 100, 1000, 720}, []Rect{{-1000, 0, 1500, 1080}, {0, 0, 1920, 1080}}, true},
		{"interior hole", Rect{0, 0, 100, 100}, []Rect{{0, 0, 100, 40}, {0, 60, 100, 40}, {0, 40, 40, 20}, {60, 40, 40, 20}}, false},
		{"four tiles", Rect{0, 0, 100, 100}, []Rect{{0, 0, 50, 50}, {50, 0, 50, 50}, {0, 50, 50, 50}, {50, 50, 50, 50}}, true},
		{"none", Rect{0, 0, 10, 10}, nil, false},
		{"invalid source", Rect{}, []Rect{{0, 0, 1920, 1080}}, false},
		{"invalid monitor", Rect{0, 0, 10, 10}, []Rect{{0, 0, 1920, 1080}, {}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := CoveredBy(test.bounds, test.displays); got != test.want {
				t.Fatalf("CoveredBy(%+v, %+v) = %v, want %v", test.bounds, test.displays, got, test.want)
			}
		})
	}
}

func TestDisplayCoverageMatchesPerPixelUnion(t *testing.T) {
	rng := rand.New(rand.NewSource(4132026))
	for sample := 0; sample < 3000; sample++ {
		bounds := Rect{rng.Intn(20) - 10, rng.Intn(20) - 10, 1 + rng.Intn(12), 1 + rng.Intn(12)}
		displays := make([]Rect, rng.Intn(6))
		for i := range displays {
			displays[i] = Rect{rng.Intn(30) - 15, rng.Intn(30) - 15, 1 + rng.Intn(20), 1 + rng.Intn(20)}
		}
		want := true
		for y := bounds.Y; y < bounds.Y+bounds.H; y++ {
			for x := bounds.X; x < bounds.X+bounds.W; x++ {
				covered := false
				for _, display := range displays {
					covered = covered || x >= display.X && x < display.X+display.W && y >= display.Y && y < display.Y+display.H
				}
				want = want && covered
			}
		}
		if got := CoveredBy(bounds, displays); got != want {
			t.Fatalf("sample %d: bounds=%+v displays=%+v got %v want %v", sample, bounds, displays, got, want)
		}
	}
}
