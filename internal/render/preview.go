package render

import (
	"fmt"
	"math"
)

// PreviewScene is deterministic, unfiltered source content. Effects are applied
// only by the production GLSL. The returned RGBA rows have a top-left origin.
func PreviewScene(width, height int) ([]byte, error) {
	if width < 64 || height < 64 || width > 1920 || height > 1080 {
		return nil, fmt.Errorf("preview dimensions must be 64..1920 by 64..1080")
	}
	data := make([]byte, width*height*4)
	bars := [8][3]byte{{235, 235, 235}, {235, 235, 0}, {0, 235, 235}, {0, 235, 0}, {235, 0, 235}, {235, 0, 0}, {0, 0, 235}, {0, 0, 0}}
	tones := [8][3]byte{{62, 101, 154}, {81, 128, 154}, {89, 145, 132}, {128, 151, 105}, {167, 147, 91}, {177, 114, 91}, {159, 96, 128}, {116, 96, 150}}
	radius := float64(min(width, height)) * .23
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			rgb := [3]byte{16, 20, 24}
			if y >= height/10 && y < height/4 {
				rgb = bars[x*8/width]
			}
			if y >= height/3 && y < height*4/5 {
				rgb = tones[x*8/width]
				if x%32 == 0 || y%32 == 0 {
					rgb = [3]byte{35, 43, 46}
				}
				dx, dy := float64(x)-float64(width)/2, float64(y)-float64(height)*.56
				if math.Abs(math.Hypot(dx, dy)-radius) < .8 {
					rgb = [3]byte{242, 242, 235}
				}
			}
			if y >= height*4/5 && y < height*7/8 {
				v := byte(x * 255 / (width - 1))
				rgb = [3]byte{v, v, v}
			}
			if y >= height*9/10 && x > width/2 {
				v := byte((x % 2) * 255)
				rgb = [3]byte{v, v, v}
			}
			i := (y*width + x) * 4
			data[i], data[i+1], data[i+2], data[i+3] = rgb[0], rgb[1], rgb[2], 255
		}
	}
	// Actual small source text makes loss of legibility visible in the preview.
	glyphs := map[rune][7]byte{
		'C': {14, 17, 16, 16, 16, 17, 14}, 'R': {30, 17, 17, 30, 20, 18, 17}, 'T': {31, 4, 4, 4, 4, 4, 4},
		'V': {17, 17, 17, 17, 17, 10, 4}, 'H': {17, 17, 17, 31, 17, 17, 17}, 'S': {15, 16, 16, 14, 1, 1, 30},
		'0': {14, 17, 19, 21, 25, 17, 14}, '1': {4, 12, 4, 4, 4, 4, 14}, '2': {14, 17, 1, 2, 4, 8, 31},
		'3': {30, 1, 1, 14, 1, 1, 30}, '4': {2, 6, 10, 18, 31, 2, 2}, '5': {31, 16, 16, 30, 1, 1, 30},
		'6': {14, 16, 16, 30, 17, 17, 14}, '7': {31, 1, 2, 4, 8, 8, 8}, '8': {14, 17, 17, 14, 17, 17, 14}, '9': {14, 17, 17, 15, 1, 1, 14},
	}
	for _, line := range []struct {
		text     string
		y, scale int
	}{{"CRT VHS 0123456789", 8, 2}, {"0123456789", height/4 + 5, 1}} {
		x := 8
		for _, ch := range line.text {
			rows := glyphs[ch]
			for gy, row := range rows {
				for gx := 0; gx < 5; gx++ {
					if row&(1<<(4-gx)) == 0 {
						continue
					}
					for dy := 0; dy < line.scale; dy++ {
						for dx := 0; dx < line.scale; dx++ {
							px, py := x+gx*line.scale+dx, line.y+gy*line.scale+dy
							if px < width && py < height {
								i := (py*width + px) * 4
								data[i], data[i+1], data[i+2] = 240, 240, 240
							}
						}
					}
				}
			}
			x += 6 * line.scale
		}
	}
	return data, nil
}
