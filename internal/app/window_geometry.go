package app

import (
	"fmt"

	"github.com/aiwaki/lumatape/internal/geometry"
	"github.com/aiwaki/lumatape/internal/locale"
)

type clientInsets struct{ Left, Top, Right, Bottom int }

// fitClient43 fits the entire decorated window in the monitor work area. The
// returned coordinates describe its client, so title bars never move offscreen.
func fitClient43(work geometry.Rect, insets clientInsets) (geometry.Rect, error) {
	if err := work.Validate(); err != nil {
		return geometry.Rect{}, err
	}
	for _, inset := range []int{insets.Left, insets.Top, insets.Right, insets.Bottom} {
		if inset < 0 || inset > geometry.MaxDimension {
			return geometry.Rect{}, fmt.Errorf(locale.Text("неверные размеры рамки окна", "invalid non-client window insets"))
		}
	}
	if insets.Left+insets.Right >= work.W || insets.Top+insets.Bottom >= work.H {
		return geometry.Rect{}, fmt.Errorf(locale.Text("рамка окна превышает рабочую область монитора", "window decoration exceeds monitor work area"))
	}
	available := geometry.Rect{X: work.X + insets.Left, Y: work.Y + insets.Top,
		W: work.W - insets.Left - insets.Right, H: work.H - insets.Top - insets.Bottom}
	if available.W < 4 || available.H < 3 {
		return geometry.Rect{}, fmt.Errorf(locale.Text("рабочая область монитора слишком мала для клиента 4:3", "monitor work area is too small for a 4:3 client"))
	}
	unit := min(available.W/4, available.H/3)
	w, h := unit*4, unit*3
	return geometry.Rect{X: available.X + (available.W-w)/2, Y: available.Y + (available.H-h)/2, W: w, H: h}, nil
}
