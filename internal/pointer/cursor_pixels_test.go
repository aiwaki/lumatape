package pointer

import (
	"bytes"
	"testing"
)

func cursorBuffers(w, h int) ([]byte, []byte) {
	b, white := make([]byte, w*h*4), make([]byte, w*h*4)
	for i := range white {
		white[i] = 255
	}
	return b, white
}
func invertPixel(b, white []byte, i int) {
	copy(b[i*4:], []byte{255, 255, 255, 0})
	copy(white[i*4:], []byte{0, 0, 0, 0})
}

func TestInvertingCursorStaysVisibleOnLightAndDark(t *testing.T) {
	// The 1px padded bitmap contains a thin XOR I-beam. It must stay intact
	// on black and have an outline on white without changing its coordinates.
	const w, h = 7, 9
	b, white := cursorBuffers(w, h)
	shape := []int{w + 2, w + 3, w + 4, 2*w + 3, 3*w + 3, 4*w + 3, 5*w + 3, 6*w + 3, 7*w + 2, 7*w + 3, 7*w + 4}
	for _, i := range shape {
		invertPixel(b, white, i)
	}
	if err := composeCursorAlpha(b, white, w); err != nil {
		t.Fatal(err)
	}
	for _, i := range shape {
		if !bytes.Equal(b[i*4:i*4+4], []byte{255, 255, 255, 255}) {
			t.Fatalf("I-beam pixel %d lost: %v", i, b[i*4:i*4+4])
		}
	}
	for _, i := range []int{3*w + 2, 3*w + 4, 3} {
		if !bytes.Equal(b[i*4:i*4+4], []byte{0, 0, 0, 255}) {
			t.Fatalf("missing outline at %d", i)
		}
	}
	for _, i := range []int{0, w - 1, 4 * w, w*h - 1} {
		if b[i*4+3] != 0 {
			t.Fatalf("outline grew beyond 1px at %d", i)
		}
	}
}

func TestInvertingCursorPreservesAdjacentOrdinaryPixels(t *testing.T) {
	b, white := cursorBuffers(5, 3)
	invertPixel(b, white, 7)
	copy(b[6*4:], []byte{20, 30, 40, 0})
	copy(white[6*4:], []byte{147, 157, 167, 0})
	copy(b[8*4:], []byte{0, 0, 0, 0})
	copy(white[8*4:], []byte{0, 0, 0, 0})
	if err := composeCursorAlpha(b, white, 5); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b[6*4:7*4], []byte{20, 30, 40, 128}) {
		t.Fatal("changed adjacent antialias/color", b[6*4:7*4])
	}
	if !bytes.Equal(b[8*4:9*4], []byte{0, 0, 0, 255}) {
		t.Fatal("changed opaque native pixel")
	}
}

func TestCursorAlphaRejectsMalformedDimensionsBeforeWriting(t *testing.T) {
	for _, size := range []int{0, 1, 3, 4, 8, 12} {
		for _, width := range []int{-1, 0, 2, 4} {
			b, white := make([]byte, size), make([]byte, size)
			before := append([]byte(nil), b...)
			valid := size > 0 && size%4 == 0 && width > 0 && size/4%width == 0
			err := composeCursorAlpha(b, white, width)
			if (err == nil) != valid {
				t.Fatalf("size=%d width=%d err=%v", size, width, err)
			}
			if !valid && !bytes.Equal(b, before) {
				t.Fatal("invalid buffer partially changed")
			}
		}
	}
}

func TestCursorOutlineDoesNotWrapRowsOrLeavePremultipliedRange(t *testing.T) {
	b, white := cursorBuffers(5, 3)
	invertPixel(b, white, 5) // Left edge; must not leak to the previous row's right.
	if err := composeCursorAlpha(b, white, 5); err != nil {
		t.Fatal(err)
	}
	if b[4*4+3] != 0 || b[9*4+3] != 0 {
		t.Fatal("outline wrapped a row")
	}
	for i := 0; i < len(b); i += 4 {
		for c := 0; c < 3; c++ {
			if b[i+c] > b[i+3] {
				t.Fatal("invalid premultiplied alpha")
			}
		}
	}
}
