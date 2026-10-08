//go:build !windows

package display

import "os/exec"

func supported() bool                  { return false }
func configureWatchdog(*exec.Cmd)      {}
func newDriver(string) (driver, error) { return nil, ErrUnsupported }
func ListModes(string) ([]Mode, error) { return nil, ErrUnsupported }
func CurrentMode(string) (Mode, error) { return Mode{}, ErrUnsupported }
