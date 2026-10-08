//go:build windows

package render

import (
	"fmt"
	"runtime"
	"unsafe"
)

// WriteSnapshot captures the last rendered BACK buffer before SwapBuffers.
// It is an explicit diagnostic, never part of the normal per-frame path.
// The synchronous GPU readback stalls rendering for this one frame; its time
// must not be reported as normal rendering latency. Call on the GL owner thread.
func (r *Renderer) WriteSnapshot(path string) error {
	w, h := r.frameWidth, r.frameHeight
	if r.gl == nil || w <= 0 || h <= 0 {
		return fmt.Errorf("no rendered frame is available for snapshot")
	}
	if uint64(w)*uint64(h)*4 > 256<<20 {
		return fmt.Errorf("snapshot exceeds 256 MiB readback limit")
	}
	if err := r.Error(); err != nil {
		return fmt.Errorf("before snapshot: %w", err)
	}
	g := r.gl
	get := func(key uintptr) int32 {
		var v int32
		g.call("glGetIntegerv", key, uintptr(unsafe.Pointer(&v)))
		return v
	}
	readFramebuffer := get(0x8caa)
	readBuffer := get(0x0c02)
	packBuffer := get(0x88ed)
	packKeys := []uintptr{0x0d05, 0x0d02, 0x0d03, 0x0d04} // alignment, row length, skip rows/pixels
	packValues := make([]int32, len(packKeys))
	for i, key := range packKeys {
		packValues[i] = get(key)
	}
	defer func() {
		g.call("glBindFramebuffer", 0x8ca8, uintptr(readFramebuffer))
		g.call("glReadBuffer", uintptr(readBuffer))
		g.call("glBindBuffer", 0x88eb, uintptr(packBuffer))
		for i, key := range packKeys {
			g.call("glPixelStorei", key, uintptr(packValues[i]))
		}
	}()
	g.call("glBindFramebuffer", 0x8ca8, 0) // READ_FRAMEBUFFER, window framebuffer
	g.call("glReadBuffer", 0x0405)         // BACK, before swap
	g.call("glBindBuffer", 0x88eb, 0)      // no PBO: destination is client memory
	for _, key := range packKeys {
		value := uintptr(0)
		if key == 0x0d05 {
			value = 1
		}
		g.call("glPixelStorei", key, value)
	}
	data := make([]byte, w*h*4)
	g.call("glReadPixels", 0, 0, uintptr(w), uintptr(h), 0x1908, 0x1401, uintptr(unsafe.Pointer(&data[0]))) // RGBA, UNSIGNED_BYTE
	runtime.KeepAlive(data)
	if err := r.Error(); err != nil {
		return fmt.Errorf("snapshot readback: %w", err)
	}
	return writeSnapshotPNG(path, w, h, data)
}
