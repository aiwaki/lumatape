//go:build !windows

package pointer

import "os/exec"

func currentIdentity() (identity, error)             { return identity{}, ErrUnsupported }
func configureProcess(*exec.Cmd)                     {}
func recoverCursor() error                           { return ErrUnsupported }
func newNativeDriver(identity) (cursorDriver, error) { return nil, ErrUnsupported }
