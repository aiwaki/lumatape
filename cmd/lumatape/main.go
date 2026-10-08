package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/aiwaki/lumatape/internal/app"
)

// Pin the initial goroutine to the initial OS thread before main. GLFW is NOT
// initialized here. A locked arbitrary worker goroutine would not be equivalent.
func init() { runtime.LockOSThread() }

func main() {
	if err := app.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "LumaTape:", err)
		app.ReportError(err)
		os.Exit(1)
	}
}
