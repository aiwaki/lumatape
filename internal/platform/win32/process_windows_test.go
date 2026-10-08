//go:build windows

package win32

import (
	"os"
	"testing"
)

func TestCurrentProcessIdentityIsStableAndLive(t *testing.T) {
	first, alive, err := ProcessIdentity(uint32(os.Getpid()))
	if err != nil || !alive || first == 0 {
		t.Fatalf("current process: %d %v %v", first, alive, err)
	}
	second, alive, err := ProcessIdentity(uint32(os.Getpid()))
	if err != nil || !alive || second != first {
		t.Fatalf("unstable creation identity: %d %v %v", second, alive, err)
	}
}
