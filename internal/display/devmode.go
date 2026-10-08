package display

import (
	"encoding/binary"
	"fmt"
)

const (
	devModeSize          = 220 // Current SDK public DEVMODEW; input capacity to EnumDisplaySettingsW.
	dmPosition           = 0x20
	dmDisplayOrientation = 0x80
	dmDisplayFixedOutput = 0x20000000
	dmBitsPerPel         = 0x40000
	dmPelsWidth          = 0x80000
	dmPelsHeight         = 0x100000
	dmDisplayFlags       = 0x200000
	dmDisplayFrequency   = 0x400000
	dmPanningWidth       = 0x8000000
	dmPanningHeight      = 0x10000000
	displayModeFields    = dmBitsPerPel | dmPelsWidth | dmPelsHeight | dmDisplayFlags | dmDisplayFrequency
	displayLayoutFields  = dmPosition | dmDisplayOrientation | dmDisplayFixedOutput
	displayStateFields   = displayModeFields | displayLayoutFields | dmPanningWidth | dmPanningHeight
)

// decodeSnapshot accepts the public layouts declared by wingdi.h: the original
// display prefix through dmDisplayFrequency (188), the WINVER >= 0x0400 ICM tail
// (212), and the later panning tail (220). dmSize describes the returned public
// layout, not our current SDK's sizeof(DEVMODEW). Private bytes begin at dmSize,
// even when they overlap offsets occupied by a newer version's public fields.
// Never expand the header or move private data before returning it to Windows.
func decodeSnapshot(data []byte) (snapshot, error) {
	if len(data) < 76 {
		return snapshot{}, fmt.Errorf("truncated DEVMODEW header: capacity=%d", len(data))
	}
	u32 := func(offset int) uint32 { return binary.LittleEndian.Uint32(data[offset:]) }
	size := int(binary.LittleEndian.Uint16(data[68:]))
	extra := int(binary.LittleEndian.Uint16(data[70:]))
	fields := u32(72)
	invalid := func(reason string) (snapshot, error) {
		// Never serialize driver-private data: on older versions it occupies
		// offsets that the current SDK assigns to the public tail. An unknown
		// size is not a trustworthy boundary, so report only the fixed header.
		publicEnd := 76
		if size == 188 || size == 212 || size == devModeSize {
			publicEnd = min(len(data), size)
		}
		return snapshot{}, fmt.Errorf("invalid DEVMODEW (%s): dmSize=%d supported=188/212/220 dmDriverExtra=%d capacity=%d specVersion=%#x fields=%#x raw_public=%x", reason, size, extra, len(data), binary.LittleEndian.Uint16(data[64:]), fields, data[:publicEnd])
	}
	if size != 188 && size != 212 && size != devModeSize {
		return invalid("unsupported public layout")
	}
	if size+extra > len(data) {
		return invalid("truncated public or private data")
	}
	// EnumDisplaySettings guarantees these five fields. Never fabricate a mode
	// from bytes a driver did not mark valid.
	if fields&displayModeFields != displayModeFields {
		return invalid("missing required display fields")
	}
	for _, member := range []struct {
		flag uint32
		end  int
	}{
		{0x800000, 192},  // DM_ICMMETHOD
		{0x1000000, 196}, // DM_ICMINTENT
		{0x2000000, 200}, // DM_MEDIATYPE
		{0x4000000, 204}, // DM_DITHERTYPE
		{dmPanningWidth, 216}, {dmPanningHeight, 220},
	} {
		if fields&member.flag != 0 && size < member.end {
			return invalid("validity flag refers beyond public layout")
		}
	}
	state := fingerprint{
		Mode:        Mode{Width: u32(172), Height: u32(176), RefreshHz: u32(184), BitsPerPixel: u32(168), DisplayFlags: u32(180)},
		ValidFields: fields & displayStateFields,
	}
	if fields&dmPosition != 0 {
		state.X, state.Y = int32(u32(76)), int32(u32(80))
	}
	if fields&dmDisplayOrientation != 0 {
		state.Orientation = u32(84)
	}
	if fields&dmDisplayFixedOutput != 0 {
		state.FixedOutput = u32(88)
	}
	if fields&dmPanningWidth != 0 {
		state.PanningWidth = u32(212)
	}
	if fields&dmPanningHeight != 0 {
		state.PanningHeight = u32(216)
	}
	return snapshot{fingerprint: state, native: data[:size+extra]}, nil
}

// modeWithLayout changes only the enumerated resolution/frequency. It preserves
// the candidate's version and driver-private payload and copies only layout
// fields the saved current mode declares valid. In particular, unflagged union
// bytes must not become a requested rotation or scaling policy.
func modeWithLayout(candidate, original snapshot) (snapshot, error) {
	if _, err := decodeSnapshot(original.native); err != nil {
		return snapshot{}, fmt.Errorf("saved display layout: %w", err)
	}
	checked, err := decodeSnapshot(candidate.native)
	if err != nil {
		return snapshot{}, fmt.Errorf("enumerated display mode: %w", err)
	}
	data := append([]byte(nil), checked.native...)
	clear(data[76:92])
	originalFields := binary.LittleEndian.Uint32(original.native[72:])
	for _, member := range []struct {
		flag       uint32
		start, end int
	}{
		{dmPosition, 76, 84}, {dmDisplayOrientation, 84, 88}, {dmDisplayFixedOutput, 88, 92},
	} {
		if originalFields&member.flag != 0 {
			copy(data[member.start:member.end], original.native[member.start:member.end])
		}
	}
	binary.LittleEndian.PutUint32(data[72:], uint32(displayModeFields)|originalFields&displayLayoutFields)
	return decodeSnapshot(data)
}
