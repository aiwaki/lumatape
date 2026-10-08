//go:build !windows

package app

import "errors"

func Run(_ []string) error {
	return errors.New("LumaTape targets Windows 10 2004+ x64; this host can run go test ./... and cross-compile the Go executables")
}
func ReportError(error) {}
