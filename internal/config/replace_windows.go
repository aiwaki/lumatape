//go:build windows

package config

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/aiwaki/lumatape/internal/locale"
)

var moveFileExW = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

func replaceFile(source, destination string) error {
	src, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	dst, err := syscall.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	// REPLACE_EXISTING | WRITE_THROUGH; both files share a directory/volume.
	ok, _, callErr := moveFileExW.Call(uintptr(unsafe.Pointer(src)), uintptr(unsafe.Pointer(dst)), 0x1|0x8)
	if ok == 0 {
		return fmt.Errorf(locale.Text("замена файла настроек: %w", "replace config: %w"), callErr)
	}
	return nil
}
