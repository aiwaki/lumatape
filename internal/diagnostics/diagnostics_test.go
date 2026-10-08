package diagnostics

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func readEntries(t *testing.T, path string) []map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		var e map[string]any
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

func TestRecordsAreSnapshotsAndCloseDrains(t *testing.T) {
	l, err := Open(filepath.Join(t.TempDir(), "logs", "lumatape.log"))
	if err != nil {
		t.Fatal(err)
	}
	d := map[string]any{"enabled": true}
	if !l.Record(Info, "toggle", d) {
		t.Fatal(l.Status())
	}
	d["enabled"] = false
	if !l.Record(Error, "failure", ErrorData("capture_unavailable", errors.New("driver unavailable"))) {
		t.Fatal(l.Status())
	}
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	entries := readEntries(t, l.Status().Path)
	if len(entries) != 2 || entries[0]["data"].(map[string]any)["enabled"] != true {
		t.Fatal(entries)
	}
	if entries[0]["session_id"] == "" || entries[0]["session_id"] != entries[1]["session_id"] || entries[1]["level"] != "error" {
		t.Fatal(entries)
	}
	if l.Record(Info, "too_late", nil) || l.Status().Written != 2 {
		t.Fatal(l.Status())
	}
}

func TestConcurrentRecordsAndClose(t *testing.T) {
	l, err := Open(filepath.Join(t.TempDir(), "log"))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				l.Record(Info, "frame", j)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := l.Close(); err != nil {
			t.Error(err)
		}
	}()
	wg.Wait()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if uint64(len(readEntries(t, l.Status().Path))) != l.Status().Written {
		t.Fatal("accepted JSON records corrupted during concurrent close")
	}
}

func TestRotationKeepsFourBoundedFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	l, err := OpenWithOptions(Options{Path: path, QueueSize: 256, MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 120; i++ {
		l.Record(Info, "sample", map[string]any{"index": i, "message": strings.Repeat("x", 160)})
	}
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(path + "*")
	if err != nil || len(files) != 4 {
		t.Fatalf("files=%v err=%v", files, err)
	}
	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil || st.Size() > 1024 {
			t.Fatalf("%s: %v %v", f, st, err)
		}
		readEntries(t, f)
	}
	e := readEntries(t, path)
	if e[len(e)-1]["data"].(map[string]any)["index"] != float64(119) {
		t.Fatal("newest event not retained")
	}
}

func TestFallbackAndFailureAreVisible(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	l, err := OpenWithOptions(Options{Path: filepath.Join(blocked, "log"), FallbackDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	s := l.Status()
	if !s.Fallback || s.Warning == "" || !strings.HasPrefix(s.Path, dir) {
		t.Fatal(s)
	}
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = OpenWithOptions(Options{Path: filepath.Join(blocked, "log"), FallbackDir: blocked}); err == nil {
		t.Fatal("both failed paths accepted")
	}
}

func TestBackpressureAndEncodingFailuresRemainObservable(t *testing.T) {
	// A deliberately paused consumer models an unresponsive disk; Record must
	// return even when the queue cannot advance.
	l := &Logger{queue: make(chan []byte, 1), status: Status{SessionID: "test"}}
	if !l.Record(Info, "first", nil) || l.Record(Info, "overflow", nil) {
		t.Fatal("queue limit not enforced")
	}
	if l.Record(Error, "invalid", func() {}) {
		t.Fatal("accepted non-JSON data")
	}
	if l.Status().Dropped != 2 || l.Status().LastError == "" {
		t.Fatal(l.Status())
	}
}

func TestDiskFailureReturnedByClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	f, _, err := openFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	l := &Logger{file: f, queue: make(chan []byte, 2), done: make(chan struct{}), maxBytes: 4 << 20, status: Status{Path: path}}
	l.Record(Error, "essential", nil)
	go l.run()
	if l.Close() == nil || l.Status().LastError == "" || l.Status().Dropped != 1 {
		t.Fatal(l.Status())
	}
}

func TestSupportRecordSanitizesNestedSecretsAndUserPaths(t *testing.T) {
	b, err := SupportRecord(map[string]any{"build": BuildInfo(), "target": map[string]any{"window_title": "private game", "password": "secret"}, "message": `C:\Users\Alice\AppData\LumaTape and /Users/Bob/Developer and /home/charlie/config`, "list": []any{map[string]any{"token": "abc"}}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, forbidden := range []string{"Alice", "Bob", "charlie", "private game", `"secret"`, `"abc"`} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("leaked %q: %s", forbidden, s)
		}
	}
	if !strings.Contains(s, "version") || !strings.Contains(s, "[path]") {
		t.Fatal("removed useful non-sensitive diagnostics")
	}
}

func TestSupportRecordRemovesEmbeddedAbsolutePaths(t *testing.T) {
	for _, value := range []string{
		`open C:\Users\Alice\My Games\config.json: access denied`,
		`open D:\private\project\settings.json: access denied`,
		`open \\server\share\secret.json: access denied`,
		`open /home/alice/private/settings.json: access denied`,
		`open /Users/Alice/My Games/config.json: access denied`,
		`open "/Users/Alice/My Games/config.json": access denied`,
	} {
		b, err := SupportRecord(map[string]any{"error": value})
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"Alice", "private", "config.json", "settings.json", "server", "secret.json", "My Games"} {
			if strings.Contains(string(b), forbidden) {
				t.Fatalf("leaked %q: %s", forbidden, b)
			}
		}
		if !strings.Contains(string(b), "access denied") || !strings.Contains(string(b), "[path]") {
			t.Fatalf("lost error context: %s", b)
		}
	}
}
