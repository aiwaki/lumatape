package win32

import (
	"errors"
	"testing"
)

func TestHotkeysTransactional(t *testing.T) {
	var s hotkeySet
	active := map[int]keyChord{}
	blocked := keyChord{6, 48}
	reg := func(id int, c keyChord) error {
		if c == blocked {
			return errors.New("occupied")
		}
		for _, a := range active {
			if a == c {
				return errors.New("duplicate")
			}
		}
		active[id] = c
		return nil
	}
	unreg := func(id int) { delete(active, id) }
	a, b, c := keyChord{3, 120}, keyChord{3, 121}, keyChord{6, 57}
	if err := s.replace(a, b, reg, unreg); err != nil {
		t.Fatal(err)
	}
	emergencyID := s.bindings[1].id
	if err := s.replace(c, blocked, reg, unreg); err == nil {
		t.Fatal("expected conflict")
	}
	if len(active) != 2 || s.event(emergencyID) != 3 || active[emergencyID] != b {
		t.Fatal("working emergency lost")
	}
	if err := s.replace(b, a, reg, unreg); err != nil {
		t.Fatal(err)
	}
	if len(active) != 2 || s.event(emergencyID) != 2 {
		t.Fatal("swap should reuse registered chords")
	}
	if err := s.replace(b, a, reg, unreg); err != nil {
		t.Fatal(err)
	}
	if err := s.replace(a, a, reg, unreg); err == nil {
		t.Fatal("equal commands")
	}
}
