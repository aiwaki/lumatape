//go:build windows

package shaderpack

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/aiwaki/lumatape/internal/locale"
)

func replaceFile(source, destination string) error {
	src, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	dst, err := syscall.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	ok, _, callErr := syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW").Call(uintptr(unsafe.Pointer(src)), uintptr(unsafe.Pointer(dst)), 0x1|0x8)
	if ok == 0 {
		return fmt.Errorf(locale.Text("сохранить шейдер: %w", "save shader: %w"), callErr)
	}
	return nil
}
