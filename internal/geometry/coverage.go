package geometry

// CoveredBy reports whether every physical pixel in bounds belongs to at least
// one display rectangle. A virtual-desktop bounding box is insufficient: two
// monitors may have a gap, different heights, or overlap at negative coordinates.
func CoveredBy(bounds Rect, displays []Rect) bool {
	if bounds.Validate() != nil {
		return false
	}
	remaining := []Rect{bounds}
	for _, display := range displays {
		if display.Validate() != nil {
			return false
		}
		next := make([]Rect, 0, len(remaining))
		for _, area := range remaining {
			left, top := max(area.X, display.X), max(area.Y, display.Y)
			right, bottom := min(area.X+area.W, display.X+display.W), min(area.Y+area.H, display.Y+display.H)
			if left >= right || top >= bottom {
				next = append(next, area)
				continue
			}
			// The four pieces are disjoint; subtracting another monitor cannot
			// count an overlap twice or conceal an uncovered pixel.
			if area.Y < top {
				next = append(next, Rect{area.X, area.Y, area.W, top - area.Y})
			}
			if bottom < area.Y+area.H {
				next = append(next, Rect{area.X, bottom, area.W, area.Y + area.H - bottom})
			}
			if area.X < left {
				next = append(next, Rect{area.X, top, left - area.X, bottom - top})
			}
			if right < area.X+area.W {
				next = append(next, Rect{right, top, area.X + area.W - right, bottom - top})
			}
		}
		remaining = next
	}
	return len(remaining) == 0
}
