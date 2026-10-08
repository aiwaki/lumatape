package display

import (
	"errors"
	"fmt"
	"time"
)

// guard is shared by the real watchdog and portable failure-path tests. Only
// the watchdog's event loop calls it, so apply/confirm/restore are serialized.
type guard struct {
	driver                          driver
	original, applied               snapshot
	deadline                        time.Time
	active, confirmed, relinquished bool
}

func (g *guard) start(wanted Mode, now time.Time, timeout time.Duration) error {
	// Preserve the caller's time base for deterministic state tests, but do
	// not charge driver enumeration, CDS_TEST, apply or readback against the
	// user's confirmation window.
	preparationStarted := time.Now()
	if g.active {
		return errors.New("a display session is already active")
	}
	if !wanted.Is43() || wanted.BitsPerPixel != 32 || wanted.DisplayFlags&2 != 0 {
		return errors.New("only enumerated progressive 32-bit 4:3 modes are permitted")
	}
	original, err := g.driver.current()
	if err != nil {
		return fmt.Errorf("save current display state: %w", err)
	}
	candidate, err := g.driver.resolve(wanted, original)
	if err != nil {
		return err
	}
	if err = g.driver.test(candidate); err != nil {
		return fmt.Errorf("CDS_TEST: %w", err)
	}
	// Do not override a change made while the candidate was being tested.
	current, err := g.driver.current()
	if err != nil {
		return err
	}
	if current.fingerprint != original.fingerprint {
		return errors.New("display changed during preparation; retry from the current mode")
	}
	g.original, g.applied = original, candidate
	g.deadline, g.active = now.Add(timeout), true
	if err = g.driver.apply(candidate); err != nil {
		// Some drivers can partially change mode even when the API fails. The
		// conditional restoration still protects independent changes.
		restoreErr := g.restore()
		return errors.Join(fmt.Errorf("apply display mode: %w", err), restoreErr)
	}
	current, err = g.driver.current()
	if err != nil {
		return errors.Join(fmt.Errorf("verify display mode: %w", err), g.restore())
	}
	if current.Mode != wanted {
		// A different effective mode cannot safely be claimed as ours.
		g.active, g.relinquished = false, true
		return errors.New("driver applied an unexpected mode; refusing to overwrite its current state")
	}
	// Store the actual layout after Windows has normalized its DEVMODE.
	g.applied = current
	g.deadline = now.Add(time.Since(preparationStarted)).Add(timeout)
	return nil
}

func (g *guard) observe() error {
	if !g.active {
		return nil
	}
	current, err := g.driver.current()
	if err != nil {
		return err
	}
	if current.fingerprint != g.applied.fingerprint {
		// Relinquish permanently, even if another program later chooses the
		// same mode we originally applied.
		g.active, g.relinquished = false, true
	}
	return nil
}

func (g *guard) confirm(now time.Time) error {
	if !now.Before(g.deadline) {
		return errors.Join(errors.New("display confirmation timed out"), g.restore())
	}
	if err := g.observe(); err != nil {
		return err
	}
	if !g.active {
		return errors.New("display session is no longer active")
	}
	g.confirmed = true
	return nil
}

func (g *guard) tick(now time.Time) (bool, error) {
	if err := g.observe(); err != nil {
		return false, err
	}
	if !g.active {
		return true, nil
	}
	if !g.confirmed && !now.Before(g.deadline) {
		return true, g.restore()
	}
	return false, nil
}

func (g *guard) restore() error {
	if !g.active {
		return nil
	}
	if err := g.observe(); err != nil {
		return fmt.Errorf("check mode before restoration: %w", err)
	}
	if !g.active {
		return nil
	}
	if err := g.driver.test(g.original); err != nil {
		return fmt.Errorf("test original mode: %w", err)
	}
	// Recheck after CDS_TEST; never restore merely because the original mode
	// remains supported. Windows has no atomic compare-and-set for modes.
	if err := g.observe(); err != nil {
		return err
	}
	if !g.active {
		return nil
	}
	if err := g.driver.apply(g.original); err != nil {
		return fmt.Errorf("restore original mode: %w", err)
	}
	current, err := g.driver.current()
	if err != nil {
		return fmt.Errorf("verify restored display state: %w", err)
	}
	if current.fingerprint != g.original.fingerprint {
		if current.fingerprint != g.applied.fingerprint {
			g.active, g.relinquished = false, true
		}
		return errors.New("driver did not restore the original display state; restoration remains unconfirmed")
	}
	g.active = false
	return nil
}
