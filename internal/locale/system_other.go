//go:build !windows

package locale

func systemLanguage() uint16 { return 0 }
