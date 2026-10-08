//go:build windows

// Developer-only display watchdog smoke. Read-only unless --case is explicit.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/aiwaki/lumatape/internal/display"
	"github.com/aiwaki/lumatape/internal/platform/win32"
)

func emit(event string, data any) {
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"time": time.Now(), "event": event, "data": data})
}
func main() {
	runtime.LockOSThread()
	if err := run(); err != nil {
		emit("error", err.Error())
		os.Exit(1)
	}
}
func run() (result error) {
	device := flag.String("device", "", "explicit device, e.g. \\\\.\\DISPLAY1")
	testCase := flag.String("case", "read", "read, timeout, confirm, restore, parent-exit, await-kill, external-change")
	watchdog := flag.String("watchdog", "", "absolute companion watchdog path")
	flag.Parse()
	if err := win32.InitializeDPI(); err != nil {
		return err
	}
	if *testCase == "read" {
		monitors, err := win32.Monitors()
		if err != nil {
			return err
		}
		for _, m := range monitors {
			if *device != "" && *device != m.Device {
				continue
			}
			state, err := display.CurrentState(m.Device)
			if err != nil {
				return err
			}
			modes, err := display.ListModes(m.Device)
			if err != nil {
				return err
			}
			var sameHz43 []display.Mode
			for _, mode := range modes {
				if mode.Is43() && mode.RefreshHz == state.RefreshHz && mode.BitsPerPixel == 32 && mode.DisplayFlags&2 == 0 {
					sameHz43 = append(sameHz43, mode)
				}
			}
			emit("read", map[string]any{"device": m.Device, "state": state, "work_area": m.Work, "same_hz_43": sameHz43})
		}
		return nil
	}
	switch *testCase {
	case "timeout", "confirm", "restore", "parent-exit", "await-kill", "external-change":
	default:
		return errors.New("unknown explicit test case")
	}
	if *device == "" || *watchdog == "" {
		return errors.New("mutating cases require --device and --watchdog")
	}
	original, err := display.CurrentState(*device)
	if err != nil {
		return err
	}
	modes, err := display.ListModes(*device)
	if err != nil {
		return err
	}
	var eligible []display.Mode
	for _, m := range modes {
		if m.RefreshHz == original.RefreshHz && m != original.Mode {
			eligible = append(eligible, m)
		}
	}
	chosen, err := display.Choose43(eligible, original.Mode)
	if err != nil {
		return err
	}
	emit("before", map[string]any{"pid": os.Getpid(), "device": *device, "case": *testCase, "original": original, "candidate": chosen})
	s, err := display.StartSession(*watchdog, *device, chosen, 5*time.Second)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, s.Restore()) }()
	applied, err := display.CurrentState(*device)
	if err != nil {
		return err
	}
	if applied.Mode != chosen {
		return errors.New("applied mode readback differs from candidate")
	}
	emit("applied", map[string]any{"pid": os.Getpid(), "state": applied, "deadline": s.Deadline()})
	switch *testCase {
	case "parent-exit":
		emit("parent_exiting_without_restore", map[string]any{"pid": os.Getpid(), "expected": original})
		os.Exit(0) // intentional: only the independent watchdog sees EOF
	case "await-kill":
		if err = s.Confirm(); err != nil {
			return err
		}
		emit("awaiting_parent_pid_kill", map[string]any{"pid": os.Getpid(), "seconds": 30, "expected": original})
		time.Sleep(30 * time.Second)
		return errors.New("no parent-PID kill arrived within 30 seconds; restoring normally")
	case "external-change":
		if err = s.Confirm(); err != nil {
			return err
		}
		emit("awaiting_external_mode_change", map[string]any{"seconds": 30, "note": "change independently; watchdog must relinquish instead of overwriting it"})
		if err = waitSession(s, 30*time.Second); err != nil {
			return err
		}
		current, err := display.CurrentState(*device)
		if err != nil {
			return err
		}
		if current == applied || current == original {
			return errors.New("no distinct external display state observed")
		}
		time.Sleep(500 * time.Millisecond)
		again, err := display.CurrentState(*device)
		if err != nil {
			return err
		}
		if again != current {
			return errors.New("external state was not preserved")
		}
		emit("external_state_preserved", current)
		return nil
	case "timeout":
		if err = waitSession(s, 15*time.Second); err != nil {
			return err
		}
	case "confirm":
		if err = s.Confirm(); err != nil {
			return err
		}
		time.Sleep(time.Until(s.Deadline().Add(700 * time.Millisecond)))
		if done, err := s.Poll(); done {
			return fmt.Errorf("confirmed session ended unexpectedly: %v", err)
		}
		current, err := display.CurrentState(*device)
		if err != nil {
			return err
		}
		if current != applied {
			return errors.New("confirmed mode changed before explicit restoration")
		}
	}
	if err = s.Restore(); err != nil {
		return err
	}
	after, err := display.CurrentState(*device)
	if err != nil {
		return err
	}
	if after != original {
		return fmt.Errorf("original public display state not restored: before=%+v after=%+v", original, after)
	}
	emit("restored_verified", after)
	return nil
}
func waitSession(s *display.Session, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if done, err := s.Poll(); done {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("watchdog did not complete before smoke timeout")
}
