package app

import (
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
