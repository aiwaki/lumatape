//go:build windows

package win32

import (
	"strings"
	"syscall"
	"unsafe"
)

// Environment contains adapter descriptions and driver versions, never device
// registry paths or a user's window names. Called for an explicit support view.
func Environment() map[string]any {
	var v struct {
		Size, Major, Minor, Build, Platform uint32
		Service                             [128]uint16
		SPMajor, SPMinor, Suite             uint16
		Product, Reserved                   byte
	}
	v.Size = uint32(unsafe.Sizeof(v))
	syscall.NewLazyDLL("ntdll.dll").NewProc("RtlGetVersion").Call(uintptr(unsafe.Pointer(&v)))
	adapters := []map[string]string{}
	for i := 0; i < 16; i++ {
		var d struct {
			Size        uint32
			Name        [32]uint16
			Description [128]uint16
			Flags       uint32
			ID, Key     [128]uint16
		}
		d.Size = uint32(unsafe.Sizeof(d))
		if call("EnumDisplayDevicesW", 0, uintptr(i), uintptr(unsafe.Pointer(&d)), 0) == 0 {
			break
		}
		if d.Flags&1 != 0 {
			version := "unknown"
			key := strings.TrimPrefix(syscall.UTF16ToString(d.Key[:]), `\Registry\Machine\`)
			var h syscall.Handle
			if err := syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, U16(key), 0, syscall.KEY_READ, &h); err == nil {
				var data [256]uint16
				size := uint32(len(data) * 2)
				var kind uint32
				if syscall.RegQueryValueEx(h, U16("DriverVersion"), nil, &kind, (*byte)(unsafe.Pointer(&data[0])), &size) == nil && kind == syscall.REG_SZ {
					version = syscall.UTF16ToString(data[:])
				}
				syscall.RegCloseKey(h)
			}
			adapters = append(adapters, map[string]string{"description": syscall.UTF16ToString(d.Description[:]), "driver_version": version})
		}
	}
	return map[string]any{"windows_major": v.Major, "windows_minor": v.Minor, "windows_build": v.Build, "display_adapters": adapters}
}
