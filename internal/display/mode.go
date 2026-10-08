// Package display implements opt-in, temporary display modes. The independent
// watchdog owns every system change; losing the main process's pipe restores it.
package display

import (
	"errors"
	"fmt"
	"sort"
)

var ErrUnsupported = errors.New("temporary display modes require Windows")

// Mode is an actual mode reported by the display driver, in physical pixels.
// RefreshHz is the integer frequency reported by EnumDisplaySettingsW.
type Mode struct {
	Width, Height, RefreshHz, BitsPerPixel uint32
	DisplayFlags                           uint32
}

func (m Mode) String() string {
	return fmt.Sprintf("%d×%d @ %d Hz (%d bit)", m.Width, m.Height, m.RefreshHz, m.BitsPerPixel)
}

func (m Mode) Is43() bool {
	return m.Width > 0 && m.Height > 0 && uint64(m.Width)*3 == uint64(m.Height)*4
}

// Choose43 prefers the current refresh rate, then the nearest refresh rate,
// then the largest 4:3 resolution that fits within the current desktop. For the
// same refresh rate, larger-than-desktop modes are a last resort. 5:4 and interlaced modes
// are deliberately excluded. A returned mode still requires CDS_TEST.
func Choose43(modes []Mode, current Mode) (Mode, error) {
	var candidates []Mode
	for _, m := range modes {
		if m.Is43() && m.BitsPerPixel == 32 && m.DisplayFlags&2 == 0 {
			candidates = append(candidates, m)
		}
	}
	if len(candidates) == 0 {
		return Mode{}, errors.New("the driver reports no progressive 32-bit 4:3 display mode")
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		ad, bd := frequencyDistance(a.RefreshHz, current.RefreshHz), frequencyDistance(b.RefreshHz, current.RefreshHz)
		if ad != bd {
			return ad < bd
		}
		af, bf := a.Width <= current.Width && a.Height <= current.Height, b.Width <= current.Width && b.Height <= current.Height
		if af != bf {
			return af
		}
		return uint64(a.Width)*uint64(a.Height) > uint64(b.Width)*uint64(b.Height)
	})
	return candidates[0], nil
}

func frequencyDistance(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

// State is the current public display fingerprint. It is read-only diagnostic
// data, not an instruction that can bypass mode enumeration or CDS_TEST.
type State struct {
	Mode
	X, Y                                                  int32
	Orientation, FixedOutput, PanningWidth, PanningHeight uint32
	// ValidFields distinguishes an unavailable member from a reported zero.
	// A loss of validity must relinquish restoration ownership like any change.
	ValidFields uint32
}

type fingerprint = State

func CurrentState(device string) (State, error) {
	d, err := newDriver(device)
	if err != nil {
		return State{}, err
	}
	s, err := d.current()
	return s.fingerprint, err
}

type snapshot struct {
	fingerprint
	// native contains the entire public DEVMODEW and any returned driver data.
	native []byte
}

type driver interface {
	current() (snapshot, error)
	resolve(Mode, snapshot) (snapshot, error)
	test(snapshot) error
	apply(snapshot) error
}
