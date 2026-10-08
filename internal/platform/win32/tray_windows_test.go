//go:build windows

package win32

import (
	"reflect"
	"runtime"
	"testing"
)

func TestControlledTrayHasNoIconButRetainsControllerEvents(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	tray, err := NewTray(false)
	if err != nil {
		t.Fatal(err)
	}
	defer tray.Close()
	hwnd := tray.HWND
	if hwnd == 0 || call("IsWindowVisible", hwnd) != 0 || tray.icon.Icon != 0 || tray.iconOwned {
		t.Fatalf("desktop controller must be hidden and own no tray icon: %+v", tray)
	}
	tray.Notify("ignored", "desktop notifications belong to the host")
	tray.Tooltip("ignored")
	if tray.addIcon() || tray.Menu([]MenuItem{{ID: 1, Label: "must not open"}}) != 0 {
		t.Fatal("desktop controller exposed a legacy icon/menu")
	}
	call("SendMessageW", hwnd, 0x8001, 0, 0x205)
	call("SendMessageW", hwnd, tray.taskbarMessage, 0, 0)
	if len(tray.Events()) != 0 || tray.icon.Icon != 0 || tray.icon.Flags != 0 {
		t.Fatal("legacy icon callback/Explorer restart affected controlled mode")
	}
	// No global keyboard input: exercise the exact controller dispatch with
	// owned synthetic registration IDs. Registration rollback is tested separately.
	tray.keys.bindings = []keyBinding{{id: 101, event: EventToggle}, {id: 102, event: EventEmergency}}
	call("SendMessageW", hwnd, 0x312, 101, 0)
	call("SendMessageW", hwnd, 0x312, 102, 0)
	call("SendMessageW", hwnd, 0x8002, 0, 0)
	if got := tray.Events(); !reflect.DeepEqual(got, []int{EventToggle, EventEmergency, EventSettings}) {
		t.Fatalf("controller events: %v", got)
	}
	if tray.LastToggle != 1 || tray.LastEmergency != 1 {
		t.Fatal("controlled hotkey receipts were lost")
	}
	if call("SendMessageW", hwnd, 0x11, 0, 0) != 1 {
		t.Fatal("session shutdown was not acknowledged")
	}
	call("SendMessageW", hwnd, 0x16, 1, 0)
	if got := tray.Events(); !reflect.DeepEqual(got, []int{EventEmergency, EventQuit}) {
		t.Fatalf("session recovery events: %v", got)
	}
	tray.Close()
	if call("IsWindow", hwnd) != 0 {
		t.Fatal("controller was not destroyed")
	}
}
