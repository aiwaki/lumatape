//go:build windows

package locale

import (
	"sync"
	"syscall"
)

// Display language is fixed for this process, matching its host shell. Cache the
// native read because status text may be assembled on each render iteration.
var nativeLanguage = sync.OnceValue(func() uint16 {
	language, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage").Call()
	return uint16(language)
})

func systemLanguage() uint16 { return nativeLanguage() }
