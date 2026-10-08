// Package geometry calculates layouts in physical desktop/framebuffer pixels.
// It does not change a game's projection, internal resolution, or input mapping.
package geometry

import (
	"errors"
	"fmt"
	"math"
)

// MaxDimension bounds calculations to an exactly representable, realistic pixel
// domain. Bogus capture dimensions must not overflow coordinates or allocations.
const MaxDimension = 1 << 24

type Size struct{ W, H int }
type Rect struct{ X, Y, W, H int }

func (s Size) Validate() error {
	if s.W < 1 || s.H < 1 || s.W > MaxDimension || s.H > MaxDimension {
		return fmt.Errorf("invalid physical size %dx%d (allowed 1..%d)", s.W, s.H, MaxDimension)
	}
	return nil
}

func (r Rect) Validate() error {
	if err := (Size{r.W, r.H}).Validate(); err != nil {
		return err
	}
	maxInt := int(^uint(0) >> 1)
	if r.X > maxInt-r.W || r.Y > maxInt-r.H {
		return errors.New("rectangle edge overflows integer coordinates")
	}
	return nil
}

func (r Rect) Size() Size { return Size{r.W, r.H} }

type ScaleMode string

const (
	Fit     ScaleMode = "fit"
	Crop    ScaleMode = "crop"
	Stretch ScaleMode = "stretch"
)

// UVRect uses normalized image coordinates with the image origin at its upper
// left. The renderer is responsible for any API-specific vertical texture flip.
type UVRect struct{ X, Y, W, H float64 }

// Mapping describes an output viewport and the visible part of the source.
// Draw black outside Viewport in a full presentation; an overlay cannot perform
// this resampling because it has no source image.
type Mapping struct {
	Viewport Rect
	SourceUV UVRect
}

func validAspect(aspect float64) bool {
	return aspect > 0 && !math.IsNaN(aspect) && !math.IsInf(aspect, 0)
}

// CenteredAspect fits an intended display aspect inside bounds. Odd remaining
// pixels are placed at the right/bottom. Tiny frames necessarily round to pixels.
func CenteredAspect(bounds Rect, aspect float64) (Rect, error) {
	if err := bounds.Validate(); err != nil {
		return Rect{}, err
	}
	if !validAspect(aspect) {
		return Rect{}, errors.New("display aspect must be finite and positive")
	}
	w, h := bounds.W, bounds.H
	if float64(w)/float64(h) > aspect {
		w = max(1, min(w, int(math.Round(float64(h)*aspect))))
	} else {
		h = max(1, min(h, int(math.Round(float64(w)/aspect))))
	}
	return Rect{bounds.X + (bounds.W-w)/2, bounds.Y + (bounds.H-h)/2, w, h}, nil
}

func Centered4x3(bounds Rect) (Rect, error) {
	return CenteredAspect(bounds, 4.0/3.0)
}

// Layout distinguishes capture framebuffer size from its intended display
// aspect. displayAspect=0 uses square pixels; e.g. a 320x200 capture can be
// explicitly interpreted as 4:3, while 1280x1024 defaults correctly to 5:4.
func Layout(container Rect, source Size, displayAspect float64, mode ScaleMode) (Mapping, error) {
	if err := container.Validate(); err != nil {
		return Mapping{}, fmt.Errorf("output: %w", err)
	}
	if err := source.Validate(); err != nil {
		return Mapping{}, fmt.Errorf("source: %w", err)
	}
	if displayAspect == 0 {
		displayAspect = float64(source.W) / float64(source.H)
	}
	if !validAspect(displayAspect) {
		return Mapping{}, errors.New("source display aspect must be zero or finite and positive")
	}
	m := Mapping{Viewport: container, SourceUV: UVRect{0, 0, 1, 1}}
	switch mode {
	case Fit:
		var err error
		m.Viewport, err = CenteredAspect(container, displayAspect)
		return m, err
	case Crop:
		outAspect := float64(container.W) / float64(container.H)
		if displayAspect > outAspect {
			m.SourceUV.W = outAspect / displayAspect
			m.SourceUV.X = (1 - m.SourceUV.W) / 2
		} else {
			m.SourceUV.H = displayAspect / outAspect
			m.SourceUV.Y = (1 - m.SourceUV.H) / 2
		}
		if m.SourceUV.W <= 0 || m.SourceUV.H <= 0 {
			return Mapping{}, errors.New("display aspect is too extreme for a representable crop")
		}
	case Stretch:
		// An explicit request to map the entire source onto the entire output.
	default:
		return Mapping{}, fmt.Errorf("unknown scaling mode %q", mode)
	}
	return m, nil
}

// IsMouseAligned is deliberately conservative: no synthetic mouse remapping is
// implemented. Click-through preserves input only if output and source client
// areas coincide and no cropping or geometric curvature moves visual controls.
func IsMouseAligned(sourceBounds Rect, mapping Mapping, curvature float64) bool {
	return sourceBounds.Validate() == nil && mapping.Viewport == sourceBounds &&
		mapping.SourceUV == (UVRect{0, 0, 1, 1}) && curvature == 0
}
