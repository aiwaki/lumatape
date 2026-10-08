package main

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartupDeadlineAbortsPipesBeforeLateProcessCompletion(t *testing.T) {
	release := make(chan struct{})
	late := make(chan error, 1)
	var aborted atomic.Bool
	start := time.Now()
	err := startWithDeadline(func() error { <-release; return nil }, 20*time.Millisecond,
		func() { aborted.Store(true) }, func(err error) { late <- err })
	if !errors.Is(err, errStartupTimeout) || !aborted.Load() || time.Since(start) > time.Second {
		t.Fatalf("unbounded start or missing pipe abort: %v, aborted=%v", err, aborted.Load())
	}
	select {
	case <-late:
		t.Fatal("cleanup ran before process creation finished")
	default:
	}
	close(release)
	select {
	case err := <-late:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("late child result was not handed to cleanup")
	}
}

func TestStartupFailureDoesNotUseLateChildCleanup(t *testing.T) {
	want := errors.New("native startup failed")
	var touched atomic.Bool
	err := startWithDeadline(func() error { return want }, time.Second,
		func() { touched.Store(true) }, func(error) { touched.Store(true) })
	if !errors.Is(err, want) || touched.Load() {
		t.Fatalf("normal failure lost: %v, timeout cleanup=%v", err, touched.Load())
	}
}
