package win32

import (
	"fmt"

	"github.com/aiwaki/lumatape/internal/locale"
)

type keyChord struct{ mods, key uint32 }
type keyBinding struct {
	chord     keyChord
	id, event int
}
type hotkeySet struct {
	bindings []keyBinding
	nextID   int
}

// Replace first reserves every new chord. Failed reservations release only the
// new registrations, so a working emergency chord is never lost on conflict.
func (s *hotkeySet) replace(toggle, emergency keyChord, register func(int, keyChord) error, unregister func(int)) error {
	if toggle == emergency {
		return fmt.Errorf(locale.Text("переключение и аварийное отключение требуют разных сочетаний", "toggle and emergency disable require different shortcuts"))
	}
	want := []keyBinding{{chord: toggle, event: 2}, {chord: emergency, event: 3}}
	added := []int{}
	if s.nextID < 100 {
		s.nextID = 100
	}
	for i := range want {
		for _, old := range s.bindings {
			if want[i].chord == old.chord {
				want[i].id = old.id
				break
			}
		}
		if want[i].id != 0 {
			continue
		}
		if s.nextID >= 0xbfff {
			for _, id := range added {
				unregister(id)
			}
			return fmt.Errorf(locale.Text("исчерпаны идентификаторы клавиш; перезапустите приложение", "hotkey identifiers exhausted; restart the application"))
		}
		want[i].id = s.nextID
		s.nextID++
		if err := register(want[i].id, want[i].chord); err != nil {
			for _, id := range added {
				unregister(id)
			}
			return fmt.Errorf(locale.Text("сочетание занято Windows или другим приложением: %w", "this shortcut is in use by Windows or another application: %w"), err)
		}
		added = append(added, want[i].id)
	}
	for _, old := range s.bindings {
		keep := false
		for _, n := range want {
			if old.id == n.id {
				keep = true
			}
		}
		if !keep {
			unregister(old.id)
		}
	}
	s.bindings = want
	return nil
}
func (s *hotkeySet) event(id int) int {
	for _, b := range s.bindings {
		if b.id == id {
			return b.event
		}
	}
	return 0
}
