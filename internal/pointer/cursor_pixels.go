package pointer

import "errors"

// composeCursorAlpha reconstructs premultiplied BGRA from the same native icon
// rendered against black and white. Ordinary monochrome AND masks and color
// cursor alpha are preserved; an XOR-inverting pixel has no RGBA equivalent.
func composeCursorAlpha(black, white []byte) error {
	if len(black) != len(white) || len(black)%4 != 0 {
		return errors.New("invalid cursor pixel buffers")
	}
	for i := 0; i < len(black); i += 4 {
		alpha := 0
		for c := 0; c < 3; c++ {
			b, w := int(black[i+c]), int(white[i+c])
			if w < b {
				return errors.New("XOR-inverting cursor cannot be faithfully projected")
			}
			alpha = max(alpha, 255-(w-b), b)
		}
		black[i+3] = byte(alpha)
	}
	return nil
}
