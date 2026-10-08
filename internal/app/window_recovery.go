package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/aiwaki/lumatape/internal/geometry"
	"github.com/aiwaki/lumatape/internal/locale"
)

type recoveryProcess struct {
	PID     uint32
	Created uint64
}
type recoveryPlacement struct {
	Flags, ShowCommand uint32
	Minimum, Maximum   [2]int32
	Normal             [4]int32
}
type windowRecoveryToken struct {
	Version          int
	Owner, Target    recoveryProcess
	Window, Nonce    uint64
	Original, Client geometry.Rect
	Placement        recoveryPlacement
	Applied          bool
}

func (t windowRecoveryToken) validate() error {
	if t.Version != 1 || t.Owner.PID == 0 || t.Target.PID == 0 || t.Owner.Created == 0 || t.Target.Created == 0 || t.Window == 0 || t.Nonce == 0 {
		return errors.New(locale.Text("неверная идентичность восстановления окна", "invalid window recovery identity"))
	}
	if err := t.Original.Validate(); err != nil {
		return err
	}
	if err := t.Client.Validate(); err != nil {
		return err
	}
	if t.Client.W*3 != t.Client.H*4 {
		return errors.New(locale.Text("неверные пропорции клиента в записи восстановления", "invalid recovery client aspect"))
	}
	if t.Placement.ShowCommand != 1 && t.Placement.ShowCommand != 2 && t.Placement.ShowCommand != 3 {
		return errors.New(locale.Text("неверная команда показа в записи восстановления", "invalid recovery show command"))
	}
	return nil
}

func readWindowRecovery(path string) (windowRecoveryToken, error) {
	var t windowRecoveryToken
	f, err := os.Open(path)
	if err != nil {
		return t, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil {
		return t, err
	}
	if len(data) > 65536 {
		return t, errors.New(locale.Text("запись восстановления окна слишком велика", "window recovery token too large"))
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err = d.Decode(&t); err != nil {
		return t, err
	}
	var trailing any
	if err = d.Decode(&trailing); err != io.EOF {
		return t, errors.New(locale.Text("лишние данные в записи восстановления", "trailing recovery data"))
	}
	return t, t.validate()
}

func writeWindowRecovery(path string, t windowRecoveryToken) error {
	if err := t.validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".window-recovery-*.tmp")
	if err != nil {
		return err
	}
	temporary := f.Name()
	defer os.Remove(temporary)
	if _, err = f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return replaceRecoveryFile(temporary, path)
}

type recoveryDecision string

const (
	recoveryRestore recoveryDecision = "restore"
	recoveryForget  recoveryDecision = "forget"
	recoveryKeep    recoveryDecision = "keep"
)

// All observations are independently validated native identities. In
// particular a process creation timestamp and a live window property prevent
// both PID and HWND reuse from authorizing an unrelated window mutation.
func decideWindowRecovery(t windowRecoveryToken, ownerAlive, targetSame, markerSame bool, outer, client geometry.Rect) (recoveryDecision, error) {
	if ownerAlive {
		return recoveryKeep, errors.New(locale.Text("процесс-владелец восстановления ещё работает", "the recovery owner is still running"))
	}
	if !targetSame || !markerSame || outer == t.Original {
		return recoveryForget, nil
	}
	if client == t.Client {
		return recoveryRestore, nil
	}
	if t.Applied {
		return recoveryForget, nil
	} // an independent move/resize wins
	return recoveryKeep, fmt.Errorf(locale.Text("после прерванного изменения геометрия окна не определена; восстановите источник вручную (исходные границы %+v)", "an interrupted window change has uncertain geometry; restore the source manually (original bounds %+v)"), t.Original)
}
