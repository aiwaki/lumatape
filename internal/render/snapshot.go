package render

import (
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
)

func writeSnapshotPNG(path string, w, h int, bottomUpRGBA []byte) error {
	if w <= 0 || h <= 0 || uint64(w)*uint64(h)*4 != uint64(len(bottomUpRGBA)) {
		return fmt.Errorf("invalid snapshot buffer dimensions")
	}
	// OpenGL reads from bottom-left; PNG uses top-left. Flip actual readback
	// rows in place without processing colors or adding an imitation effect.
	stride := w * 4
	row := make([]byte, stride)
	for y := 0; y < h/2; y++ {
		a, b := y*stride, (h-1-y)*stride
		copy(row, bottomUpRGBA[a:a+stride])
		copy(bottomUpRGBA[a:a+stride], bottomUpRGBA[b:b+stride])
		copy(bottomUpRGBA[b:b+stride], row)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	// Full is opaque. RGBA also correctly describes the premultiplied overlay
	// framebuffer if an explicit diagnostic caller captures that mode.
	img := &image.RGBA{Pix: bottomUpRGBA, Stride: stride, Rect: image.Rect(0, 0, w, h)}
	encodeErr := png.Encode(f, img)
	return errors.Join(encodeErr, f.Close())
}
