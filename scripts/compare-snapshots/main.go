// compare-snapshots presents two actual PNG readbacks side by side, with an
// optional identical crop/nearest-neighbor zoom. It never adds an image effect.
// Left is before; right is after. The console reports RGB differences in the
// selected region; use a static region when independent captured frames move.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

func main() {
	beforePath := flag.String("before", "", "actual intensity-zero PNG")
	afterPath := flag.String("after", "", "actual processed PNG")
	outPath := flag.String("out", "", "side-by-side PNG: before left, after right")
	cropText := flag.String("crop", "", "optional x,y,width,height static comparison region")
	scale := flag.Int("scale", 1, "identical nearest-neighbor presentation zoom, 1..8")
	flag.Parse()
	if err := run(*beforePath, *afterPath, *outPath, *cropText, *scale); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func read(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

func run(beforePath, afterPath, outPath, cropText string, scale int) error {
	if beforePath == "" || afterPath == "" || outPath == "" {
		return fmt.Errorf("--before, --after and --out are required")
	}
	if scale < 1 || scale > 8 {
		return fmt.Errorf("--scale must be 1..8")
	}
	before, err := read(beforePath)
	if err != nil {
		return err
	}
	after, err := read(afterPath)
	if err != nil {
		return err
	}
	if before.Bounds() != after.Bounds() {
		return fmt.Errorf("different source framebuffer dimensions: %v / %v", before.Bounds(), after.Bounds())
	}
	area := before.Bounds()
	if cropText != "" {
		var x, y, w, h int
		if n, err := fmt.Sscanf(cropText, "%d,%d,%d,%d", &x, &y, &w, &h); err != nil || n != 4 || w <= 0 || h <= 0 {
			return fmt.Errorf("invalid --crop: use x,y,width,height")
		}
		area = image.Rect(x, y, x+w, y+h)
		if !area.In(before.Bounds()) {
			return fmt.Errorf("crop lies outside the snapshots")
		}
	}
	w, h := area.Dx()*scale, area.Dy()*scale
	if uint64(w*2+8)*uint64(h)*4 > 256<<20 {
		return fmt.Errorf("comparison exceeds 256 MiB")
	}
	out := image.NewRGBA(image.Rect(0, 0, w*2+8, h))
	draw.Draw(out, out.Bounds(), image.NewUniform(color.RGBA{14, 18, 27, 255}), image.Point{}, draw.Src)
	var sum, squared float64
	var maximum, changed int
	var alphaMismatch int
	for y := 0; y < area.Dy(); y++ {
		for x := 0; x < area.Dx(); x++ {
			a := color.NRGBAModel.Convert(before.At(area.Min.X+x, area.Min.Y+y)).(color.NRGBA)
			b := color.NRGBAModel.Convert(after.At(area.Min.X+x, area.Min.Y+y)).(color.NRGBA)
			if a.A != 255 || b.A != 255 {
				alphaMismatch++
			}
			pixelChanged := false
			for _, d := range []int{int(a.R) - int(b.R), int(a.G) - int(b.G), int(a.B) - int(b.B)} {
				if d < 0 {
					d = -d
				}
				sum += float64(d)
				squared += float64(d * d)
				if d > maximum {
					maximum = d
				}
				if d != 0 {
					pixelChanged = true
				}
			}
			if pixelChanged {
				changed++
			}
			for zy := 0; zy < scale; zy++ {
				for zx := 0; zx < scale; zx++ {
					out.Set(x*scale+zx, y*scale+zy, a)
					out.Set(w+8+x*scale+zx, y*scale+zy, b)
				}
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0700); err != nil {
		return err
	}
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	encodeErr := png.Encode(f, out)
	closeErr := f.Close()
	if encodeErr != nil {
		return encodeErr
	}
	if closeErr != nil {
		return closeErr
	}
	n := area.Dx() * area.Dy()
	fmt.Printf("actual PNGs, before=LEFT after=RIGHT; crop=%d,%d,%d,%d; presentation zoom=%dx\n", area.Min.X, area.Min.Y, area.Dx(), area.Dy(), scale)
	fmt.Printf("changed pixels=%d/%d (%.3f%%), RGB mean abs=%.6f/255, RGB RMS=%.6f/255, maximum=%d, nonopaque pairs=%d\n", changed, n, 100*float64(changed)/float64(n), sum/float64(n*3), math.Sqrt(squared/float64(n*3)), maximum, alphaMismatch)
	fmt.Println(outPath)
	return nil
}
