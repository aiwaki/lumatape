//go:build !windows

package app

import "os"

func replaceRecoveryFile(source, destination string) error { return os.Rename(source, destination) }
