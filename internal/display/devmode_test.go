package display

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

func displayBytes(size, extra int, fields uint32) []byte {
	raw := make([]byte, size+extra)
	binary.LittleEndian.PutUint16(raw[64:], 0x401)
	binary.LittleEndian.PutUint16(raw[68:], uint16(size))
	binary.LittleEndian.PutUint16(raw[70:], uint16(extra))
	binary.LittleEndian.PutUint32(raw[72:], fields)
	for offset, value := range map[int]uint32{168: 32, 172: 1600, 176: 1200, 184: 75} {
		binary.LittleEndian.PutUint32(raw[offset:], value)
	}
	for i := size; i < len(raw); i++ {
		raw[i] = byte(i*13 + 1)
	}
	return raw
}

func TestDevModeVersionedPrivatePayload(t *testing.T) {
	for _, size := range []int{188, 212, 220} {
		for _, extra := range []int{0, 1, 64, 65535} {
			t.Run(fmt.Sprintf("public%d-private%d", size, extra), func(t *testing.T) {
				raw := displayBytes(size, extra, displayModeFields)
				want := append([]byte(nil), raw...)
				// EnumDisplaySettings gets a larger capacity than the returned
				// version. Unused capacity is not part of the saved DEVMODE.
				data := append(raw, bytes.Repeat([]byte{0xa5}, 64)...)
				got, err := decodeSnapshot(data)
				if err != nil {
					t.Fatal(err)
				}
				if got.Mode != (Mode{Width: 1600, Height: 1200, RefreshHz: 75, BitsPerPixel: 32}) || got.ValidFields != displayModeFields {
					t.Fatalf("mode=%+v fields=%#x", got.Mode, got.ValidFields)
				}
				if got.PanningWidth != 0 || got.PanningHeight != 0 || !bytes.Equal(got.native, want) {
					t.Fatal("private bytes interpreted as public fields, shifted, changed or lost")
				}
			})
		}
	}
}

func TestDevModeObserved188ByteResponse(t *testing.T) {
	// The real EnumDisplaySettingsW response collected on Windows 11 ARM,
	// 2026-10-06. The input buffer was 220+65535; returned public size is 188.
	const observed = "4300440044000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000001040104bc000000a0007c20000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000c00020000000d00b00006a07000000000000780000000000000000000000000000000000000000000000000000000000000000000000"
	raw, err := hex.DecodeString(observed)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := State{Mode: Mode{Width: 3024, Height: 1898, RefreshHz: 120, BitsPerPixel: 32}, ValidFields: displayModeFields | displayLayoutFields}
	if got.fingerprint != want || len(got.native) != 188 || !bytes.Equal(got.native, raw[:188]) {
		t.Fatalf("decoded=%+v native bytes=%d", got.fingerprint, len(got.native))
	}
}

func TestDevModeRejectsTruncatedAndContradictoryHeaders(t *testing.T) {
	for _, size := range []int{0, 68, 184, 187, 189, 208, 216, 219, 224, 65535} {
		raw := displayBytes(220, 0, displayModeFields)
		binary.LittleEndian.PutUint16(raw[68:], uint16(size))
		if _, err := decodeSnapshot(raw); err == nil {
			t.Errorf("accepted unsupported public size %d", size)
		}
	}
	for length := 0; length < 220; length++ {
		if _, err := decodeSnapshot(displayBytes(220, 0, displayModeFields)[:length]); err == nil {
			t.Errorf("accepted truncated full public size at %d", length)
		}
	}
	for _, size := range []int{188, 212, 220} {
		raw := displayBytes(size, 8, displayModeFields)
		if _, err := decodeSnapshot(raw[:len(raw)-1]); err == nil {
			t.Errorf("accepted truncated private data at public size %d", size)
		}
	}
	for _, flag := range []uint32{dmBitsPerPel, dmPelsWidth, dmPelsHeight, dmDisplayFlags, dmDisplayFrequency} {
		if _, err := decodeSnapshot(displayBytes(220, 0, displayModeFields&^flag)); err == nil {
			t.Errorf("accepted missing required field %#x", flag)
		}
	}
	for _, entry := range []struct {
		size int
		flag uint32
	}{
		{188, 0x800000}, {188, 0x1000000}, {188, 0x2000000}, {188, 0x4000000},
		{188, dmPanningWidth}, {188, dmPanningHeight}, {212, dmPanningWidth}, {212, dmPanningHeight},
	} {
		// Private bytes beyond dmSize must never satisfy a public field flag.
		if _, err := decodeSnapshot(displayBytes(entry.size, 64, displayModeFields|entry.flag)); err == nil {
			t.Errorf("accepted flag %#x beyond public size %d", entry.flag, entry.size)
		}
	}
}

func TestDevModeLayoutPreservesPrivateDataAndIgnoresUnflaggedUnion(t *testing.T) {
	for _, originalSize := range []int{188, 212, 220} {
		for _, candidateSize := range []int{188, 212, 220} {
			original := displayBytes(originalSize, 64, displayModeFields|dmPosition|dmDisplayOrientation)
			candidate := displayBytes(candidateSize, 64, displayModeFields|displayLayoutFields)
			binary.LittleEndian.PutUint32(original[76:], 0xfffff880) // -1920
			binary.LittleEndian.PutUint32(original[80:], 108)
			binary.LittleEndian.PutUint32(original[84:], 1)
			binary.LittleEndian.PutUint32(original[88:], 0xdeadbeef) // unflagged
			before := append([]byte(nil), candidate...)
			got, err := modeWithLayout(snapshot{native: candidate}, snapshot{native: original})
			if err != nil {
				t.Fatal(err)
			}
			if got.X != -1920 || got.Y != 108 || got.Orientation != 1 || got.FixedOutput != 0 || got.ValidFields != displayModeFields|dmPosition|dmDisplayOrientation {
				t.Fatalf("copied unavailable layout or lost valid layout: %+v", got.fingerprint)
			}
			if len(got.native) != len(before) || !bytes.Equal(got.native[:72], before[:72]) || !bytes.Equal(got.native[92:], before[92:]) || !bytes.Equal(candidate, before) {
				t.Fatal("version/private payload changed or enumerated source mutated")
			}
			if binary.LittleEndian.Uint32(got.native[88:]) != 0 {
				t.Fatal("unflagged fixed-output member must be zero before calling Windows")
			}
		}
	}
	if _, err := modeWithLayout(snapshot{}, snapshot{native: displayBytes(188, 0, displayModeFields)}); err == nil {
		t.Fatal("accepted missing candidate")
	}
	if _, err := modeWithLayout(snapshot{native: displayBytes(188, 0, displayModeFields)}, snapshot{}); err == nil {
		t.Fatal("accepted missing original")
	}
}

func TestDevModeReadbackVersionAndValidity(t *testing.T) {
	short, _ := decodeSnapshot(displayBytes(188, 64, displayModeFields))
	fullData := displayBytes(220, 0, displayModeFields)
	// Invalid/unreported members can contain nonzero bytes. They do not
	// describe a display change; different public versions may compare equal.
	for _, offset := range []int{76, 80, 84, 88, 212, 216} {
		binary.LittleEndian.PutUint32(fullData[offset:], 0xa5a5a5a5)
	}
	full, _ := decodeSnapshot(fullData)
	if short.fingerprint != full.fingerprint {
		t.Fatal("version or unreported bytes changed the effective display state")
	}
	// Losing a valid zero-valued position must relinquish ownership, rather
	// than treating missing information as proof that restoration is safe.
	valid, _ := decodeSnapshot(displayBytes(188, 0, displayModeFields|dmPosition))
	d := &fakeDriver{state: short}
	g := guard{driver: d, active: true, applied: valid, original: valid}
	if err := g.restore(); err != nil {
		t.Fatal(err)
	}
	if !g.relinquished || g.active || len(d.changes) != 0 {
		t.Fatal("restored over a change in public field validity")
	}
}

func TestDevModeDiagnosticExcludesPrivatePayload(t *testing.T) {
	marker := []byte("PRIVATE-DRIVER-MARKER")
	for _, size := range []int{188, 212, 220} {
		raw := displayBytes(size, len(marker), displayModeFields&^dmPelsWidth)
		copy(raw[size:], marker)
		_, err := decodeSnapshot(raw)
		if err == nil || strings.Contains(err.Error(), hex.EncodeToString(marker)) {
			t.Fatalf("size %d: private payload escaped diagnostic boundary: %v", size, err)
		}
		_, dump, ok := strings.Cut(err.Error(), "raw_public=")
		if !ok || dump != hex.EncodeToString(raw[:size]) {
			t.Fatalf("size %d: diagnostic did not preserve exactly public bytes", size)
		}
	}
	for _, size := range []uint16{0, 68, 189, 216, 65535} {
		raw := displayBytes(220, 0, displayModeFields)
		binary.LittleEndian.PutUint16(raw[68:], size)
		copy(raw[76:], marker)
		_, err := decodeSnapshot(raw)
		if err == nil {
			t.Fatalf("accepted invalid size %d", size)
		}
		_, dump, ok := strings.Cut(err.Error(), "raw_public=")
		if !ok || dump != hex.EncodeToString(raw[:76]) {
			t.Fatalf("unknown size %d: diagnostic exceeded the fixed header", size)
		}
	}
}
