//go:build windows

package display

import (
	"testing"
	"unsafe"
)

// Display arm of the SDK26100 wingdi.h DEVMODEW unions. Windows LONG/DWORD
// remain 32-bit on AMD64; both UTF-16 arrays contain exactly 32 WCHARs.
type devModeWABI struct {
	DeviceName                                                        [32]uint16
	SpecVersion, DriverVersion, Size, DriverExtra                     uint16
	Fields                                                            uint32
	X, Y                                                              int32
	DisplayOrientation, DisplayFixedOutput                            uint32
	Color, Duplex, YResolution, TTOption, Collate                     int16
	FormName                                                          [32]uint16
	LogPixels                                                         uint16
	BitsPerPel, PelsWidth, PelsHeight, DisplayFlags, DisplayFrequency uint32
	ICMMethod, ICMIntent, MediaType, DitherType, Reserved1, Reserved2 uint32
	PanningWidth, PanningHeight                                       uint32
}

func TestDevModeWPublicABI(t *testing.T) {
	var d devModeWABI
	if unsafe.Sizeof(d) != devModeSize || unsafe.Alignof(d) != 4 {
		t.Fatalf("DEVMODEW size/alignment=%d/%d; expected 220/4", unsafe.Sizeof(d), unsafe.Alignof(d))
	}
	for _, field := range []struct {
		name             string
		actual, expected uintptr
	}{
		{"dmSpecVersion", unsafe.Offsetof(d.SpecVersion), 64},
		{"dmSize", unsafe.Offsetof(d.Size), 68}, {"dmDriverExtra", unsafe.Offsetof(d.DriverExtra), 70},
		{"dmFields", unsafe.Offsetof(d.Fields), 72}, {"dmPosition", unsafe.Offsetof(d.X), 76},
		{"dmDisplayOrientation", unsafe.Offsetof(d.DisplayOrientation), 84},
		{"dmDisplayFixedOutput", unsafe.Offsetof(d.DisplayFixedOutput), 88},
		{"dmBitsPerPel", unsafe.Offsetof(d.BitsPerPel), 168}, {"dmPelsWidth", unsafe.Offsetof(d.PelsWidth), 172},
		{"dmPelsHeight", unsafe.Offsetof(d.PelsHeight), 176}, {"dmDisplayFlags", unsafe.Offsetof(d.DisplayFlags), 180},
		{"dmDisplayFrequency", unsafe.Offsetof(d.DisplayFrequency), 184},
		{"dmPanningWidth", unsafe.Offsetof(d.PanningWidth), 212}, {"dmPanningHeight", unsafe.Offsetof(d.PanningHeight), 216},
	} {
		if field.actual != field.expected {
			t.Errorf("%s offset=%d, expected=%d", field.name, field.actual, field.expected)
		}
	}
}

func TestDecodeSnapshotUsesWideDisplayFields(t *testing.T) {
	d := devModeWABI{Size: devModeSize, Fields: displayStateFields,
		X: -1920, Y: 108, DisplayOrientation: 1, DisplayFixedOutput: 2, BitsPerPel: 32, PelsWidth: 1600, PelsHeight: 1200, DisplayFlags: 0, DisplayFrequency: 75, PanningWidth: 1700, PanningHeight: 1300}
	raw := unsafe.Slice((*byte)(unsafe.Pointer(&d)), devModeSize)
	got, err := decodeSnapshot(append([]byte(nil), raw...))
	if err != nil {
		t.Fatal(err)
	}
	want := State{Mode: Mode{Width: 1600, Height: 1200, RefreshHz: 75, BitsPerPixel: 32}, X: -1920, Y: 108, Orientation: 1, FixedOutput: 2, PanningWidth: 1700, PanningHeight: 1300, ValidFields: displayStateFields}
	if got.fingerprint != want || len(got.native) != devModeSize {
		t.Fatalf("decoded snapshot=%+v, want=%+v", got.fingerprint, want)
	}
}
