//go:build windows

package win32

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	EventMenu      = 1
	EventToggle    = 2
	EventEmergency = 3
	EventQuit      = 4
	EventSettings  = 5
)

type Tray struct {
	HWND                      uintptr
	events                    []int
	icon                      notifyIcon
	callback                  uintptr
	taskbarMessage            uintptr
	keys                      hotkeySet
	iconOwned                 bool
	visibleIcon               bool
	LastToggle, LastEmergency uint64
}
type windowClass struct {
	Size, Style                        uint32
	Proc                               uintptr
	ClsExtra, WndExtra                 int32
	Instance, Icon, Cursor, Background uintptr
	Menu, Class                        *uint16
	SmallIcon                          uintptr
}
type notifyIcon struct {
	Size                uint32
	Hwnd                uintptr
	ID, Flags, Callback uint32
	Icon                uintptr
	Tip                 [128]uint16
	State, StateMask    uint32
	Info                [256]uint16
	Timeout             uint32
	InfoTitle           [64]uint16
	InfoFlags           uint32
	Guid                [16]byte
	BalloonIcon         uintptr
}
type MenuItem struct {
	ID                int
	Label             string
	Checked, Disabled bool
	Children          []MenuItem
}

// NewTray always creates the hidden hotkey/session controller. The desktop host
// owns its Tauri icon; only standalone mode requests a second, native Go icon.
// Keep a hidden top-level window so TaskbarCreated, session shutdown and the
// existing single-instance discovery continue to work.
func NewTray(visibleIcon bool) (*Tray, error) {
	t := &Tray{visibleIcon: visibleIcon}
	t.taskbarMessage = call("RegisterWindowMessageW", uintptr(unsafe.Pointer(U16("TaskbarCreated"))))
	t.callback = syscall.NewCallback(func(h uintptr, msg uint32, w, l uintptr) uintptr {
		switch msg {
		case 0x8002:
			t.events = append(t.events, EventSettings)
			return 0
		case 0x8001:
			if t.visibleIcon && (l == 0x205 || l == 0x202 || l == 0x7b) {
				t.events = append(t.events, EventMenu)
			} // mouse up/context
			return 0
		case 0x312:
			if event := t.keys.event(int(w)); event != 0 {
				t.events = append(t.events, event)
				if event == EventToggle {
					t.LastToggle++
				} else {
					t.LastEmergency++
				}
			}
			return 0
		case 0x10:
			t.events = append(t.events, EventQuit)
			return 0
		case 0x11:
			t.events = append(t.events, EventEmergency)
			return 1
		case 0x16:
			if w != 0 {
				t.events = append(t.events, EventQuit)
			}
			return 0
		}
		if t.visibleIcon && uintptr(msg) == t.taskbarMessage && t.HWND != 0 {
			t.addIcon()
			return 0
		}
		return call("DefWindowProcW", h, uintptr(msg), w, l)
	})
	instance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	wc := windowClass{Proc: t.callback, Instance: instance, Class: U16("LumaTape.Control")}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if call("RegisterClassExW", uintptr(unsafe.Pointer(&wc))) == 0 {
		return nil, fmt.Errorf("register tray window class failed")
	}
	t.HWND = call("CreateWindowExW", 0, uintptr(unsafe.Pointer(wc.Class)), uintptr(unsafe.Pointer(U16("LumaTape controls"))), 0, 0, 0, 0, 0, 0, 0, instance, 0)
	if t.HWND == 0 {
		call("UnregisterClassW", uintptr(unsafe.Pointer(wc.Class)), instance)
		return nil, fmt.Errorf("create tray controller failed")
	}
	if !t.visibleIcon {
		return t, nil
	}
	icon, owned := LoadAppIcon(true)
	t.iconOwned = owned
	t.icon = notifyIcon{Hwnd: t.HWND, ID: 1, Flags: 1 | 2 | 4, Callback: 0x8001, Icon: icon}
	t.icon.Size = uint32(unsafe.Sizeof(t.icon))
	copy(t.icon.Tip[:], syscall.StringToUTF16("LumaTape — CRT / VHS overlay"))
	if !t.addIcon() {
		t.Close()
		return nil, fmt.Errorf("Shell_NotifyIconW failed")
	}
	return t, nil
}
func (t *Tray) addIcon() bool {
	if !t.visibleIcon {
		return false
	}
	r, _, _ := shell32.NewProc("Shell_NotifyIconW").Call(0, uintptr(unsafe.Pointer(&t.icon)))
	return r != 0
}
func (t *Tray) Hotkeys(toggleMods, toggleKey, emergencyMods, emergencyKey uint32) error {
	return t.keys.replace(keyChord{toggleMods, toggleKey}, keyChord{emergencyMods, emergencyKey}, func(id int, c keyChord) error {
		return BoolCall("RegisterHotKey", t.HWND, uintptr(id), uintptr(c.mods|0x4000), uintptr(c.key))
	}, func(id int) { call("UnregisterHotKey", t.HWND, uintptr(id)) })
}
func (t *Tray) ClearHotkeys() {
	for _, b := range t.keys.bindings {
		call("UnregisterHotKey", t.HWND, uintptr(b.id))
	}
	t.keys.bindings = nil
}
func (t *Tray) HotkeysRegistered() bool { return len(t.keys.bindings) == 2 }
func (t *Tray) Tooltip(value string) {
	if !t.visibleIcon {
		return
	}
	clear(t.icon.Tip[:])
	copy(t.icon.Tip[:127], syscall.StringToUTF16(value))
	t.icon.Flags = 4
	shell32.NewProc("Shell_NotifyIconW").Call(1, uintptr(unsafe.Pointer(&t.icon)))
	t.icon.Flags = 1 | 2 | 4
}
func (t *Tray) Events() []int { e := t.events; t.events = nil; return e }
func (t *Tray) Notify(title, body string) {
	if !t.visibleIcon {
		return
	}
	t.icon.Flags = 0x10
	clear(t.icon.Info[:])
	clear(t.icon.InfoTitle[:])
	copy(t.icon.Info[:len(t.icon.Info)-1], syscall.StringToUTF16(body))
	copy(t.icon.InfoTitle[:len(t.icon.InfoTitle)-1], syscall.StringToUTF16(title))
	t.icon.InfoFlags = 1
	shell32.NewProc("Shell_NotifyIconW").Call(1, uintptr(unsafe.Pointer(&t.icon)))
	t.icon.Flags = 1 | 2 | 4
}
func createMenu(items []MenuItem) uintptr {
	h := call("CreatePopupMenu")
	for _, i := range items {
		flags := uintptr(0)
		if i.Checked {
			flags |= 8
		}
		if i.Disabled {
			flags |= 3
		}
		id := uintptr(i.ID)
		if i.Label == "" {
			flags |= 0x800
		} else if len(i.Children) > 0 {
			flags |= 0x10
			id = createMenu(i.Children)
		}
		call("AppendMenuW", h, flags, id, uintptr(unsafe.Pointer(U16(i.Label))))
	}
	return h
}
func (t *Tray) Menu(items []MenuItem) int {
	if !t.visibleIcon {
		return 0
	}
	h := createMenu(items)
	defer call("DestroyMenu", h)
	var p Point
	call("GetCursorPos", uintptr(unsafe.Pointer(&p)))
	call("SetForegroundWindow", t.HWND) // user explicitly opened the tray menu
	id := call("TrackPopupMenu", h, 0x100|0x2, uintptr(p.X), uintptr(p.Y), 0, t.HWND, 0)
	call("PostMessageW", t.HWND, 0, 0, 0)
	return int(id)
}
func (t *Tray) Close() {
	if t.HWND != 0 {
		t.ClearHotkeys()
		if t.visibleIcon {
			shell32.NewProc("Shell_NotifyIconW").Call(2, uintptr(unsafe.Pointer(&t.icon)))
		}
		if t.iconOwned {
			call("DestroyIcon", t.icon.Icon)
			t.iconOwned = false
		}
		call("DestroyWindow", t.HWND)
		t.HWND = 0
		instance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
		call("UnregisterClassW", uintptr(unsafe.Pointer(U16("LumaTape.Control"))), instance)
	}
}
