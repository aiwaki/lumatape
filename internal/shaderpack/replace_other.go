//go:build !windows

package shaderpack

import "os"

func replaceFile(source, destination string) error { return os.Rename(source, destination) }
