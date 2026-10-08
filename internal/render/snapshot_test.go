package render

import (
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotKeepsColorsAndConvertsGLRowOrigin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "frame.png")
	// GL bottom row: blue/white. GL top row: red/green.
	pixels := []byte{0, 0, 255, 255, 255, 255, 255, 255, 255, 0, 0, 255, 0, 255, 0, 255}
	if err := writeSnapshotPNG(path, 2, 2, pixels); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	want := []color.RGBA{{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}, {255, 255, 255, 255}}
	for i, c := range want {
		got := color.RGBAModel.Convert(img.At(i%2, i/2)).(color.RGBA)
		if got != c {
			t.Fatalf("pixel %d got %v want %v", i, got, c)
		}
	}
}

func TestSnapshotRejectsIncompleteReadback(t *testing.T) {
	if err := writeSnapshotPNG(filepath.Join(t.TempDir(), "bad.png"), 2, 2, make([]byte, 15)); err == nil {
		t.Fatal("accepted a truncated framebuffer")
	}
}
