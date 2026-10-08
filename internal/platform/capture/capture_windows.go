//go:build windows

package capture

import (
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"

	"github.com/aiwaki/lumatape/internal/platform/win32"
)

type Library struct {
	dll                           *syscall.DLL
	open, acquire, release, close *syscall.Proc
	openEx, stats                 *syscall.Proc
	telemetry                     *syscall.Proc
}
type Capture struct {
	library     *Library
	handle      uintptr
	errorBuffer [2048]byte
	transfer    Transfer
}

func Load(dir string) (*Library, error) {
	dll, e := syscall.LoadDLL(filepath.Join(dir, "lumatape_capture.dll"))
	if e != nil {
		return nil, fmt.Errorf("Full mode needs optional lumatape_capture.dll: %w", e)
	}
	l := &Library{dll: dll}
	ok := false
	defer func() {
		if !ok {
			dll.Release()
		}
	}()
	abi, e := dll.FindProc("vhs_capture_abi_version")
	if e != nil {
		return nil, e
	}
	version, _, _ := abi.Call()
	if version != 1 || unsafe.Sizeof(Frame{}) != 56 || unsafe.Sizeof(Stats{}) != 40 {
		return nil, fmt.Errorf("unsupported capture ABI %d", version)
	}
	for n, out := range map[string]**syscall.Proc{"vhs_capture_open": &l.open, "vhs_capture_acquire": &l.acquire, "vhs_capture_release": &l.release, "vhs_capture_close": &l.close} {
		p, e := dll.FindProc(n)
		if e != nil {
			return nil, e
		}
		*out = p
	}
	// Additive ABI v1 exports. Older bridges still work in their GPU-only mode.
	l.openEx, _ = dll.FindProc("vhs_capture_open_ex")
	l.stats, _ = dll.FindProc("vhs_capture_get_stats")
	l.telemetry, _ = dll.FindProc("vhs_capture_get_telemetry_v1")
	if l.telemetry != nil && unsafe.Sizeof(TelemetryV1{}) != 136 {
		return nil, fmt.Errorf("unsupported capture telemetry layout")
	}
	ok = true
	return l, nil
}
func (l *Library) SupportsCompatibility() bool {
	return l != nil && l.openEx != nil && l.stats != nil
}

func (l *Library) Open(hwnd uintptr, transfer Transfer) (*Capture, error) {
	if !win32.CaptureSourceAllowed(hwnd) {
		return nil, fmt.Errorf("capture source is unavailable or is a Windows/LumaTape helper window; select a game window")
	}
	if transfer != TransferGPU && transfer != TransferCompatibility {
		return nil, fmt.Errorf("unknown capture transfer %d", transfer)
	}
	if transfer == TransferCompatibility && !l.SupportsCompatibility() {
		return nil, fmt.Errorf("explicit CPU compatibility requires an updated lumatape_capture.dll with transfer statistics")
	}
	c := &Capture{library: l, transfer: transfer}
	var r uintptr
	if transfer == TransferCompatibility {
		r, _, _ = l.openEx.Call(hwnd, uintptr(transfer), uintptr(unsafe.Pointer(&c.handle)), uintptr(unsafe.Pointer(&c.errorBuffer[0])), uintptr(len(c.errorBuffer)))
	} else {
		r, _, _ = l.open.Call(hwnd, uintptr(unsafe.Pointer(&c.handle)), uintptr(unsafe.Pointer(&c.errorBuffer[0])), uintptr(len(c.errorBuffer)))
	}
	if int32(r) != 1 {
		if c.handle != 0 {
			return c, c.err("capture open; cleanup required")
		}
		return nil, c.err("capture open")
	}
	return c, nil
}

func (c *Capture) Transfer() Transfer { return c.transfer }

// Telemetry is optional: older bridges, including GPU-only ABI v1, return nil.
// Diagnostic errors are exposed separately so callers can keep rendering.
func (c *Capture) Telemetry() (*TelemetryV1, error) {
	if c.library.telemetry == nil {
		return nil, nil
	}
	s := TelemetryV1{StructSize: 136}
	r, _, _ := c.library.telemetry.Call(c.handle, uintptr(unsafe.Pointer(&s)), uintptr(unsafe.Pointer(&c.errorBuffer[0])), uintptr(len(c.errorBuffer)))
	if int32(r) != 1 {
		return nil, c.err("capture telemetry")
	}
	if s.StructSize != 136 || s.Version != 1 {
		return nil, fmt.Errorf("unsupported capture telemetry size=%d version=%d", s.StructSize, s.Version)
	}
	return &s, nil
}

func (c *Capture) Stats() (Stats, error) {
	s := Stats{StructSize: 40, Transfer: c.transfer}
	if c.library.stats == nil {
		return s, nil
	}
	r, _, _ := c.library.stats.Call(c.handle, uintptr(unsafe.Pointer(&s)), uintptr(unsafe.Pointer(&c.errorBuffer[0])), uintptr(len(c.errorBuffer)))
	if int32(r) != 1 {
		return Stats{}, c.err("capture transfer statistics")
	}
	if s.Transfer != c.transfer {
		return Stats{}, fmt.Errorf("capture bridge changed transfer path without authorization")
	}
	return s, nil
}
func (c *Capture) err(stage string) error {
	b := c.errorBuffer[:]
	for i, v := range b {
		if v == 0 {
			b = b[:i]
			break
		}
	}
	return fmt.Errorf("%s: %s", stage, string(b))
}
func (c *Capture) Acquire() (Frame, bool, error) {
	f := Frame{StructSize: 56}
	r, _, _ := c.library.acquire.Call(c.handle, uintptr(unsafe.Pointer(&f)), uintptr(unsafe.Pointer(&c.errorBuffer[0])), uintptr(len(c.errorBuffer)))
	if int32(r) < 0 {
		return Frame{}, false, c.err("capture frame")
	}
	return f, int32(r) == 1, nil
}
func (c *Capture) Release() error {
	r, _, _ := c.library.release.Call(c.handle, uintptr(unsafe.Pointer(&c.errorBuffer[0])), uintptr(len(c.errorBuffer)))
	if int32(r) < 0 {
		return c.err("release capture texture")
	}
	return nil
}
func (c *Capture) Close() error {
	if c == nil || c.handle == 0 {
		return nil
	}
	r, _, _ := c.library.close.Call(uintptr(unsafe.Pointer(&c.handle)), uintptr(unsafe.Pointer(&c.errorBuffer[0])), uintptr(len(c.errorBuffer)))
	if int32(r) < 0 {
		return c.err("close capture")
	}
	return nil
}
func (l *Library) Close() {
	if l != nil && l.dll != nil {
		l.dll.Release()
		l.dll = nil
	}
}
