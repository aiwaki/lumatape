//go:build windows

package display

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

var (
	user32                = syscall.NewLazyDLL("user32.dll")
	enumDisplaySettings   = user32.NewProc("EnumDisplaySettingsW")
	changeDisplaySettings = user32.NewProc("ChangeDisplaySettingsExW")
	// EnumDisplaySettingsW caches the list on index zero; serialize complete
	// enumerations so another caller cannot replace that cache mid-enumeration.
	enumerationMu sync.Mutex
)

func supported() bool { return true }

func configureWatchdog(cmd *exec.Cmd) {
	// Do not create an additional console or put the watchdog in the parent's
	// console-control group. It remains alive if the main process is killed.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000 | 0x00000200}
}

type windowsDriver struct{ device *uint16 }

func newDriver(device string) (driver, error) {
	const prefix = `\\.\DISPLAY`
	if !strings.HasPrefix(strings.ToUpper(device), prefix) {
		return nil, errors.New(`expected an explicit monitor name such as \\.\DISPLAY1`)
	}
	number, err := strconv.Atoi(device[len(prefix):])
	if err != nil || number < 1 {
		return nil, errors.New("invalid monitor device name")
	}
	wide, err := syscall.UTF16PtrFromString(device)
	if err != nil {
		return nil, err
	}
	return &windowsDriver{device: wide}, nil
}

// readMode reserves the maximum representable private-data region, and then
// retains exactly the public structure plus the driver-reported private bytes.
// No assumptions about a driver's DEVMODE tail are made or serialized to disk.
func (d *windowsDriver) readMode(index uint32) (snapshot, bool, error) {
	data := make([]byte, devModeSize+65535)
	binary.LittleEndian.PutUint16(data[68:], devModeSize)
	binary.LittleEndian.PutUint16(data[70:], 65535)
	ok, _, _ := enumDisplaySettings.Call(uintptr(unsafe.Pointer(d.device)), uintptr(index), uintptr(unsafe.Pointer(&data[0])))
	runtime.KeepAlive(d)
	if ok == 0 {
		return snapshot{}, false, nil
	}
	s, err := decodeSnapshot(data)
	if err != nil {
		return snapshot{}, false, fmt.Errorf("EnumDisplaySettingsW(index=%#x): %w", index, err)
	}
	return s, true, nil
}

func (d *windowsDriver) current() (snapshot, error) {
	enumerationMu.Lock()
	defer enumerationMu.Unlock()
	s, ok, err := d.readMode(0xffffffff) // ENUM_CURRENT_SETTINGS, never registry defaults.
	if err != nil {
		return snapshot{}, err
	}
	if !ok {
		return snapshot{}, errors.New("EnumDisplaySettingsW could not read the current monitor mode")
	}
	return s, nil
}

func (d *windowsDriver) modes() ([]snapshot, error) {
	enumerationMu.Lock()
	defer enumerationMu.Unlock()
	var modes []snapshot
	for index := uint32(0); ; index++ {
		mode, ok, err := d.readMode(index)
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		modes = append(modes, mode)
	}
	if len(modes) == 0 {
		return nil, errors.New("EnumDisplaySettingsW reported no modes for this monitor")
	}
	return modes, nil
}

func (d *windowsDriver) resolve(wanted Mode, original snapshot) (snapshot, error) {
	modes, err := d.modes()
	if err != nil {
		return snapshot{}, err
	}
	for _, candidate := range modes {
		if candidate.Mode != wanted {
			continue
		}
		return modeWithLayout(candidate, original)
	}
	return snapshot{}, errors.New("selected mode is no longer reported by the monitor driver")
}

func (d *windowsDriver) change(s snapshot, flags uintptr) error {
	if _, err := decodeSnapshot(s.native); err != nil {
		return fmt.Errorf("saved DEVMODEW: %w", err)
	}
	result, _, _ := changeDisplaySettings.Call(uintptr(unsafe.Pointer(d.device)), uintptr(unsafe.Pointer(&s.native[0])), 0, flags, 0)
	runtime.KeepAlive(s)
	runtime.KeepAlive(d)
	if int32(result) != 0 {
		return fmt.Errorf("ChangeDisplaySettingsExW returned %d", int32(result))
	}
	return nil
}

func (d *windowsDriver) test(s snapshot) error  { return d.change(s, 2) } // CDS_TEST
func (d *windowsDriver) apply(s snapshot) error { return d.change(s, 0) } // Dynamic; no registry/unsafe/custom modes.

func CurrentMode(device string) (Mode, error) {
	d, err := newDriver(device)
	if err != nil {
		return Mode{}, err
	}
	s, err := d.current()
	return s.Mode, err
}

func ListModes(device string) ([]Mode, error) {
	d, err := newDriver(device)
	if err != nil {
		return nil, err
	}
	snapshots, err := d.(*windowsDriver).modes()
	if err != nil {
		return nil, err
	}
	seen := map[Mode]bool{}
	var modes []Mode
	for _, s := range snapshots {
		if !seen[s.Mode] {
			modes = append(modes, s.Mode)
			seen[s.Mode] = true
		}
	}
	return modes, nil
}
