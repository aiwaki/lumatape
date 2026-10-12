package app

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/aiwaki/lumatape/internal/geometry"
)

func TestWindow43KeepsDecorationsInsideWorkArea(t *testing.T) {
	work := geometry.Rect{W: 1920, H: 1040}
	insets := clientInsets{8, 31, 8, 8}
	client, err := fitClient43(work, insets)
	if err != nil {
		t.Fatal(err)
	}
	if client.Y-insets.Top < work.Y || client.X-insets.Left < work.X || client.X+client.W+insets.Right > work.X+work.W || client.Y+client.H+insets.Bottom > work.Y+work.H {
		t.Fatalf("decorated window leaves work area: %+v", client)
	}
	if client.W*3 != client.H*4 {
		t.Fatalf("client is not exact 4:3: %+v", client)
	}
}

func TestWindow43OnUltrawideUsesHeightAndPreservesDecorations(t *testing.T) {
	// DPI scales here are physical non-client metrics supplied by Windows.
	// Maximized windows use their normal decoration metrics before SW_RESTORE.
	for _, monitor := range []geometry.Rect{
		{W: 3440, H: 1400},
		{X: -5120, Y: -320, W: 5120, H: 1400},
		{X: 1920, Y: -900, W: 2560, H: 1040},
	} {
		for _, dpi := range []int{96, 120, 144, 192} {
			for _, frame := range []string{"normal", "maximized", "borderless"} {
				t.Run(fmt.Sprintf("%dx%d/%s/dpi%d", monitor.W, monitor.H, frame, dpi), func(t *testing.T) {
					insets := clientInsets{}
					if frame != "borderless" {
						insets = clientInsets{8 * dpi / 96, 31 * dpi / 96, 8 * dpi / 96, 8 * dpi / 96}
					}
					client, err := fitClient43(monitor, insets)
					if err != nil {
						t.Fatal(err)
					}
					if client.W*3 != client.H*4 || client.H != (monitor.H-insets.Top-insets.Bottom)/3*3 {
						t.Fatalf("ultrawide did not use the available height: %+v", client)
					}
					left := client.X - insets.Left - monitor.X
					right := monitor.X + monitor.W - (client.X + client.W + insets.Right)
					top := client.Y - insets.Top - monitor.Y
					bottom := monitor.Y + monitor.H - (client.Y + client.H + insets.Bottom)
					if left < 0 || right < 0 || top < 0 || bottom < 0 || left-right > 1 || right-left > 1 || top-bottom > 1 || bottom-top > 1 {
						t.Fatalf("decorated 4:3 window is offscreen or not centred: client=%+v margins=%d,%d,%d,%d", client, left, right, top, bottom)
					}
				})
			}
		}
	}
}

func TestWindow43PropertiesAcrossDPIAndNegativeMonitors(t *testing.T) {
	random := rand.New(rand.NewSource(8413))
	for i := 0; i < 2000; i++ {
		work := geometry.Rect{X: random.Intn(8000) - 4000, Y: random.Intn(4000) - 2000, W: 320 + random.Intn(5000), H: 240 + random.Intn(3000)}
		scale := 1 + random.Intn(4)
		insets := clientInsets{8 * scale, 31 * scale, 8 * scale, 8 * scale}
		client, err := fitClient43(work, insets)
		if err != nil {
			t.Fatal(err)
		}
		if client.W*3 != client.H*4 || client.X-insets.Left < work.X || client.Y-insets.Top < work.Y || client.X+client.W+insets.Right > work.X+work.W || client.Y+client.H+insets.Bottom > work.Y+work.H {
			t.Fatalf("work=%+v client=%+v insets=%+v", work, client, insets)
		}
	}
}

func TestWindow43RejectsImpossibleDecorations(t *testing.T) {
	for _, insets := range []clientInsets{{-1, 0, 0, 0}, {100, 100, 100, 100}, {0, int(^uint(0) >> 1), 0, 0}} {
		if _, err := fitClient43(geometry.Rect{W: 100, H: 100}, insets); err == nil {
			t.Fatal("accepted impossible non-client bounds")
		}
	}
}
