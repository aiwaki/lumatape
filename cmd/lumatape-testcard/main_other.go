//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "LumaTape test card requires Windows 10 1703+ (native Win32/GDI, per-monitor DPI v2).")
	os.Exit(1)
}
