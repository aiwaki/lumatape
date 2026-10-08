package main

import (
	"errors"
	"time"
)

var errStartupTimeout = errors.New("process creation deadline expired; parent pipes closed, no force-kill performed")

// CreateProcess itself has no cancellation contract. Keep its lifetime separate
// from the helper's deadline: closing the parent pipe ends makes an eventual
// child see EOF, while lateResult owns cleanup after Start actually returns.
func startWithDeadline(start func() error, timeout time.Duration, abort func(), lateResult func(error)) error {
	started := make(chan error, 1)
	go func() { started <- start() }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-started:
		return err
	case <-timer.C:
		abort()
		go func() { lateResult(<-started) }()
		return errStartupTimeout
	}
}
