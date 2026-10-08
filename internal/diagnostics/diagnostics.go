// Package diagnostics records bounded, asynchronous local JSON diagnostics.
// It never sends records over the network.
package diagnostics

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/aiwaki/lumatape/internal/locale"
)

// Set by the release build with -ldflags -X.
var Version = "0.1.0"
var Commit = "unknown"
var BuildTime = "unknown"

type Level string

const (
	Debug Level = "debug"
	Info  Level = "info"
	Warn  Level = "warn"
	Error Level = "error"
)

type Build struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

func BuildInfo() Build {
	return Build{Version, Commit, BuildTime, runtime.Version(), runtime.GOOS, runtime.GOARCH}
}

type Status struct {
	Path       string `json:"path"`
	SessionID  string `json:"session_id"`
	Warning    string `json:"warning,omitempty"`
	LastError  string `json:"last_error,omitempty"`
	Dropped    uint64 `json:"dropped"`
	Written    uint64 `json:"written"`
	QueueDepth int    `json:"queue_depth"`
	Closed     bool   `json:"closed"`
	Fallback   bool   `json:"fallback"`
}

type Options struct {
	Path string
	// FallbackDir is only a parent; a private, unique directory is created in it.
	// Empty uses os.TempDir. DisableFallback is useful for explicit tests/tools.
	FallbackDir     string
	DisableFallback bool
	QueueSize       int
	MaxBytes        int64
	Backups         int
}

type entry struct {
	Time      string `json:"time"`
	Level     Level  `json:"level"`
	SessionID string `json:"session_id"`
	Event     string `json:"event"`
	Data      any    `json:"data,omitempty"`
}

type Logger struct {
	mu       sync.Mutex
	status   Status
	queue    chan []byte
	done     chan struct{}
	file     *os.File // owned only by the worker after Open returns
	size     int64
	maxBytes int64
	backups  int
	closeErr error
}

func Open(path string) (*Logger, error) { return OpenWithOptions(Options{Path: path}) }

func OpenWithOptions(o Options) (*Logger, error) {
	if o.QueueSize <= 0 {
		o.QueueSize = 256
	}
	if o.MaxBytes <= 0 {
		o.MaxBytes = 4 << 20
	}
	if o.Backups <= 0 {
		o.Backups = 3
	}
	if o.QueueSize > 4096 || o.Backups > 16 || o.MaxBytes < 1024 {
		return nil, errors.New("invalid diagnostics limits")
	}
	var seed [16]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return nil, fmt.Errorf("diagnostics session ID: %w", err)
	}
	status := Status{Path: o.Path, SessionID: hex.EncodeToString(seed[:])}
	f, size, err := openFile(o.Path)
	if err != nil {
		if o.DisableFallback {
			return nil, fmt.Errorf(locale.Text("открытие журнала: %w", "open diagnostics: %w"), err)
		}
		primaryErr := err
		parent := o.FallbackDir
		if parent == "" {
			parent = os.TempDir()
		}
		dir, fallbackErr := os.MkdirTemp(parent, "lumatape-diagnostics-")
		if fallbackErr == nil {
			status.Path = filepath.Join(dir, "lumatape.log")
			f, size, fallbackErr = openFile(status.Path)
		}
		if fallbackErr != nil {
			return nil, fmt.Errorf(locale.Text("журнал недоступен (основной: %v; резервный: %w)", "diagnostics unavailable (preferred: %v; fallback: %w)"), primaryErr, fallbackErr)
		}
		status.Fallback = true
		status.Warning = fmt.Sprintf(locale.Text("Основной журнал недоступен: %v. Используется временный журнал %s", "Preferred diagnostics path unavailable: %v. Using temporary log %s"), primaryErr, status.Path)
	}
	l := &Logger{status: status, queue: make(chan []byte, o.QueueSize), done: make(chan struct{}), file: f, size: size, maxBytes: o.MaxBytes, backups: o.Backups}
	go l.run()
	if status.Fallback {
		l.Record(Warn, "diagnostics_fallback", map[string]any{"message": status.Warning})
	}
	return l, nil
}

func openFile(path string) (*os.File, int64, error) {
	if path == "" {
		return nil, 0, errors.New("empty log path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, 0, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, 0, err
	}
	s, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	if !s.Mode().IsRegular() {
		f.Close()
		return nil, 0, errors.New("diagnostics path is not a regular file")
	}
	return f, s.Size(), nil
}

// Record takes a JSON snapshot before queuing, so callers may reuse maps after
// this method returns. It never waits for disk or for queue capacity.
func (l *Logger) Record(level Level, event string, data any) bool {
	if l == nil {
		return false
	}
	if level != Debug && level != Info && level != Warn && level != Error {
		level = Info
	}
	event = strings.TrimSpace(event)
	if event == "" {
		event = "unspecified"
	}
	l.mu.Lock()
	if l.status.Closed {
		l.mu.Unlock()
		return false
	}
	session := l.status.SessionID
	l.mu.Unlock()
	b, err := json.Marshal(entry{Time: time.Now().Format(time.RFC3339Nano), Level: level, SessionID: session, Event: event, Data: data})
	limit := int64(64 << 10)
	if l.maxBytes > 0 && l.maxBytes < limit {
		limit = l.maxBytes - 1
	}
	if err == nil && int64(len(b)) > limit {
		err = fmt.Errorf("diagnostic record exceeds %d bytes", limit)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.status.Closed {
		return false
	}
	if err != nil {
		l.status.Dropped++
		l.status.LastError = "encode diagnostics: " + err.Error()
		return false
	}
	b = append(b, '\n')
	select {
	case l.queue <- b:
		return true
	default:
		l.status.Dropped++
		return false
	}
}

func (l *Logger) Status() Status {
	if l == nil {
		return Status{Closed: true, LastError: "diagnostics unavailable"}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.status
	s.QueueDepth = len(l.queue)
	return s
}

func (l *Logger) setFailure(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.status.LastError = err.Error()
	l.closeErr = errors.Join(l.closeErr, err)
}

func (l *Logger) rotate() error {
	if err := l.file.Close(); err != nil {
		return err
	}
	l.file = nil
	path := l.status.Path
	if err := os.Remove(fmt.Sprintf("%s.%d", path, l.backups)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for i := l.backups - 1; i >= 1; i-- {
		if err := os.Rename(fmt.Sprintf("%s.%d", path, i), fmt.Sprintf("%s.%d", path, i+1)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.Rename(path, path+".1"); err != nil {
		return err
	}
	f, size, err := openFile(path)
	if err != nil {
		return err
	}
	l.file, l.size = f, size
	return nil
}

func (l *Logger) run() {
	defer close(l.done)
	failed := false
	for b := range l.queue {
		if !failed && l.size+int64(len(b)) > l.maxBytes {
			if err := l.rotate(); err != nil {
				l.setFailure(fmt.Errorf("rotate diagnostics: %w", err))
				failed = true
			}
		}
		if !failed {
			n, err := l.file.Write(b)
			if err == nil && n != len(b) {
				err = io.ErrShortWrite
			}
			if err != nil {
				l.setFailure(fmt.Errorf("write diagnostics: %w", err))
				failed = true
			} else {
				l.size += int64(n)
			}
		}
		l.mu.Lock()
		if failed {
			l.status.Dropped++
		} else {
			l.status.Written++
		}
		l.mu.Unlock()
	}
	if l.file != nil {
		if err := l.file.Sync(); err != nil {
			l.setFailure(fmt.Errorf("flush diagnostics: %w", err))
		}
		if err := l.file.Close(); err != nil {
			l.setFailure(fmt.Errorf("close diagnostics: %w", err))
		}
	}
}

// Close stops acceptance, drains accepted records and reports disk failures.
// Concurrent or repeated calls wait for the same completion.
func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	if !l.status.Closed {
		l.status.Closed = true
		close(l.queue)
	}
	l.mu.Unlock()
	<-l.done
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closeErr
}

func ErrorData(code string, err error) map[string]any {
	d := map[string]any{"code": code}
	if err != nil {
		d["message"] = err.Error()
	}
	return d
}

// SupportRecord is an explicit export snapshot, not an automatic upload. It
// removes sensitive field names and absolute paths in text fields.
// Callers should export a deliberate status/config subset, never raw logs.
func SupportRecord(data any) ([]byte, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	if len(b) > 1<<20 {
		return nil, errors.New("support snapshot exceeds 1 MiB")
	}
	var v any
	if err = json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	home, _ := os.UserHomeDir()
	return json.MarshalIndent(sanitize(v, home), "", "  ")
}

func sanitize(v any, home string) any {
	switch x := v.(type) {
	case map[string]any:
		for k, value := range x {
			lower := strings.ToLower(strings.ReplaceAll(k, "_", ""))
			if strings.Contains(lower, "token") || strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "apikey") || strings.Contains(lower, "authorization") || strings.Contains(lower, "cookie") || strings.HasSuffix(lower, "title") || strings.Contains(lower, "username") || strings.HasSuffix(lower, "path") {
				x[k] = "[redacted]"
			} else {
				x[k] = sanitize(value, home)
			}
		}
	case []any:
		for i := range x {
			x[i] = sanitize(x[i], home)
		}
	case string:
		if home != "" {
			x = strings.ReplaceAll(x, home, "[home]")
			x = strings.ReplaceAll(x, strings.ReplaceAll(home, "\\", "/"), "[home]")
		}
		return redactUserPaths(x)
	}
	return v
}
