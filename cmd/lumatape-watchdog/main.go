// lumatape-watchdog is deliberately independent of the OpenGL/UI process. Do not
// start it manually: the application supplies its private inherited pipes.
package main

import (
	"fmt"
	"os"

	"github.com/aiwaki/lumatape/internal/display"
	"github.com/aiwaki/lumatape/internal/pointer"
)

func main() {
	if len(os.Args) != 2 || (os.Args[1] != "--stdio" && os.Args[1] != "--pointer-stdio") {
		fmt.Fprintln(os.Stderr, "lumatape-watchdog is a companion process; start lumatape instead")
		os.Exit(2)
	}
	var err error
	if os.Args[1] == "--pointer-stdio" {
		err = pointer.RunWorker(os.Stdin, os.Stdout)
	} else {
		err = display.RunWatchdog(os.Stdin, os.Stdout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
