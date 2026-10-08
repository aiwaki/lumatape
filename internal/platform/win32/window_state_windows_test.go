//go:build windows

package win32

import (
	"testing"
	"unsafe"
)

func TestWindowPlacementABI(t *testing.T) {
	var p WindowPlacement
	if unsafe.Sizeof(p) != 44 || unsafe.Offsetof(p.Normal) != 28 {
		t.Fatal("WINDOWPLACEMENT no longer matches Win32 ABI")
	}
}
