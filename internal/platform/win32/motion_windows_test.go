//go:build windows

package win32

import (
	"testing"
	"unsafe"
)

func TestGUIThreadInfoX64ABI(t *testing.T) {
	var info guiThreadInfo
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("LumaTape Windows runtime is x64")
	}
	if unsafe.Sizeof(info) != 72 || unsafe.Offsetof(info.MoveSize) != 40 || unsafe.Offsetof(info.CaretRect) != 56 {
		t.Fatal("GUITHREADINFO no longer matches Windows x64 ABI")
	}
}
