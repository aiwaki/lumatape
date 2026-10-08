package render

import "testing"

func TestPreviewSceneDeterministicSource(t *testing.T) {
	a, err := PreviewScene(320, 240)
	if err != nil {
		t.Fatal(err)
	}
	b, err := PreviewScene(320, 240)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 320*240*4 {
		t.Fatal("wrong source size")
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatal("source scene changes between frames")
		}
	}
	for i := 3; i < len(a); i += 4 {
		if a[i] != 255 {
			t.Fatal("source is not opaque")
		}
	}
	for _, size := range [][2]int{{0, 240}, {320, 0}, {1921, 720}, {1920, 1081}} {
		if _, err := PreviewScene(size[0], size[1]); err == nil {
			t.Fatalf("invalid preview size accepted: %v", size)
		}
	}
}
