package app

import (
	"errors"
	"fmt"

	"github.com/aiwaki/lumatape/internal/geometry"
	"github.com/aiwaki/lumatape/internal/locale"
)

// windowMutation is also used by failure-injection tests. The restore closure
// captures the complete original native placement before the first mutation.
type windowMutation interface {
	Snapshot() (original geometry.Rect, restore func() error, err error)
	ResizeClient(geometry.Rect) error
	Bounds() (geometry.Rect, error)
	ClientBounds() (geometry.Rect, error)
}

type persistentWindowMutation interface {
	Prepare(geometry.Rect) error // durable original state before the first mutation
	Commit() error               // mark verified ownership, keeping the original state
	Clear() error                // forget only after restoration or ownership release
}

type windowTransaction struct {
	driver       windowMutation
	restore      func() error
	applied      geometry.Rect
	original     geometry.Rect
	verified     bool
	recoveryOnly bool
	active       bool
}

func beginWindowTransaction(driver windowMutation, client geometry.Rect) (*windowTransaction, error) {
	if err := client.Validate(); err != nil {
		return nil, err
	}
	if client.W*3 != client.H*4 {
		return nil, fmt.Errorf(locale.Text("запрошенная клиентская область должна иметь точные пропорции 4:3", "requested client must be exactly 4:3"))
	}
	original, restore, err := driver.Snapshot()
	if err != nil {
		return nil, fmt.Errorf(locale.Text("сохранение исходного положения окна: %w", "save original window placement: %w"), err)
	}
	t := &windowTransaction{driver: driver, restore: restore, original: original, active: true}
	clear := func() error {
		if durable, ok := driver.(persistentWindowMutation); ok {
			return durable.Clear()
		}
		return nil
	}
	fail := func(cause error) (*windowTransaction, error) {
		// This is still the explicit operation, not a later inferred restore.
		// A driver may partially mutate before returning an error.
		if rollback := restore(); rollback != nil {
			t.recoveryOnly = true
			return t, errors.Join(cause, fmt.Errorf(locale.Text("восстановление исходного положения окна: %w", "restore original window placement: %w"), rollback))
		}
		if cleanup := clear(); cleanup != nil {
			t.recoveryOnly = true
			return t, errors.Join(cause, cleanup)
		}
		t.active = false
		return nil, cause
	}
	if durable, ok := driver.(persistentWindowMutation); ok {
		if err := durable.Prepare(client); err != nil {
			if cleanup := clear(); cleanup != nil {
				t.recoveryOnly = true
				return t, errors.Join(err, cleanup)
			}
			return nil, fmt.Errorf(locale.Text("сохранение записи восстановления окна: %w", "save durable window recovery: %w"), err)
		}
	}
	if err = driver.ResizeClient(client); err != nil {
		return fail(fmt.Errorf(locale.Text("изменение клиентской области: %w", "resize client: %w"), err))
	}
	if t.applied, err = driver.Bounds(); err != nil {
		return fail(fmt.Errorf(locale.Text("проверка изменённых границ окна: %w", "verify changed window bounds: %w"), err))
	}
	t.verified = true
	actual, err := driver.ClientBounds()
	if err != nil {
		return fail(fmt.Errorf(locale.Text("проверка изменённых границ клиента: %w", "verify changed client bounds: %w"), err))
	}
	if actual != client {
		return fail(fmt.Errorf(locale.Text("окно не приняло клиентскую область 4:3; выберите оконный/borderless режим или задайте 4:3 в игре", "the window did not accept the requested 4:3 client; choose windowed/borderless mode or set 4:3 in the game")))
	}
	if durable, ok := driver.(persistentWindowMutation); ok {
		if err := durable.Commit(); err != nil {
			return fail(fmt.Errorf(locale.Text("сохранение владельца восстановления окна: %w", "commit window recovery ownership: %w"), err))
		}
	}
	return t, nil
}

func (t *windowTransaction) finish() error {
	if durable, ok := t.driver.(persistentWindowMutation); ok {
		if err := durable.Clear(); err != nil {
			t.recoveryOnly = true
			return err
		}
	}
	t.active = false
	return nil
}

func (t *windowTransaction) Restore() error {
	if t == nil || !t.active {
		return nil
	}
	if t.verified || t.recoveryOnly {
		current, err := t.driver.Bounds()
		if err != nil {
			return fmt.Errorf(locale.Text("проверка собственного изменения окна перед восстановлением: %w", "check owned window placement before restoration: %w"), err)
		}
		if current == t.original {
			return t.finish()
		}
		if t.recoveryOnly && (!t.verified || current != t.applied) {
			return errors.New(locale.Text("восстановление окна не подтверждено; восстановите положение вручную до следующей смены формата", "window recovery could not be verified; restore its placement manually before another format change"))
		}
		if current != t.applied {
			return t.finish() // the user/game changed it; never overwrite that change
		}
	}
	if err := t.restore(); err != nil {
		t.recoveryOnly = true
		return err
	}
	return t.finish()
}
