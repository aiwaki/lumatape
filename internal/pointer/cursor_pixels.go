package pointer

import "errors"

// composeCursorAlpha reconstructs premultiplied BGRA from the same native icon
// rendered against black and white. Ordinary monochrome AND masks and color
// cursor alpha are preserved. XOR inversion has no RGBA equivalent: use white
// pixels with a black outline so text/resize cursors remain visible on both light
// and dark backgrounds. This changes their appearance, never their hotspot.
// white is scratch storage; its alpha bytes record inversion before compositing.
func composeCursorAlpha(black, white []byte, width int) error {
	if len(black) != len(white) || len(black)%4 != 0 || width < 1 || len(black) == 0 || len(black)/4%width != 0 {
		return errors.New("invalid cursor pixel buffers")
	}
	for i := 0; i < len(black); i += 4 {
		alpha := 0
		inverts := false
		white[i+3] = 0
		for c := 0; c < 3; c++ {
			b, w := int(black[i+c]), int(white[i+c])
			if w < b {
				inverts = true
			}
			alpha = max(alpha, 255-(w-b), b)
		}
		if inverts {
			white[i+3] = 1
			black[i], black[i+1], black[i+2], black[i+3] = 255, 255, 255, 255
		} else {
			black[i+3] = byte(alpha)
		}
	}
	// Outline only transparent neighbours of original XOR pixels; do not grow
	// the outline recursively or overwrite existing color/antialiased pixels.
	height := len(black) / 4 / width
	for i := 0; i < len(black); i += 4 {
		if white[i+3] == 0 {
			continue
		}
		x, y := i/4%width, i/4/width
		for ny := max(0, y-1); ny <= min(height-1, y+1); ny++ {
			for nx := max(0, x-1); nx <= min(width-1, x+1); nx++ {
				j := (ny*width + nx) * 4
				if black[j+3] == 0 {
					black[j], black[j+1], black[j+2], black[j+3] = 0, 0, 0, 255
				}
			}
		}
	}
	return nil
}
