//go:build windows

// This developer-only helper uses messages to the explicitly identified native
// Settings window. It is not input injection or proof of physical hotkey delivery.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var user32 = syscall.NewLazyDLL("user32.dll")
var kernel32 = syscall.NewLazyDLL("kernel32.dll")

//go:uintptrescapes
func call(name string, a ...uintptr) uintptr { r, _, _ := user32.NewProc(name).Call(a...); return r }

type result struct {
	Exited    bool            `json:"exited,omitempty"`
	ExitCode  *uint32         `json:"exit_code,omitempty"`
	Step      step            `json:"step"`
	ElapsedMS int64           `json:"elapsed_ms"`
	Controls  []control       `json:"controls,omitempty"`
	Config    json.RawMessage `json:"config,omitempty"`
	LogTail   string          `json:"log_tail,omitempty"`
	Error     string          `json:"error,omitempty"`
}

func main() {
	runtime.LockOSThread()
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	pid := flag.Uint("pid", 0, "explicit application PID; quit requests graceful WM_CLOSE only")
	exe := flag.String("exe", "", "exact full application executable path")
	stepsPath := flag.String("steps", "", "JSON array of explicit actions: inspect/select/text/check/click/foreground/wait")
	configPath := flag.String("config", "", "optional exact test config path to read after steps")
	logPath := flag.String("log", "", "optional test log path, last 32 KiB read after steps")
	outputPath := flag.String("output", "", "write UTF-8 JSONL directly to this file (avoids shell transcoding)")
	flag.Parse()
	if *pid == 0 || *exe == "" {
		return errors.New("-pid and -exe are required")
	}
	want, err := filepath.Abs(*exe)
	if err != nil {
		return err
	}
	process, _, e := kernel32.NewProc("OpenProcess").Call(0x1000|0x100000, 0, uintptr(*pid))
	if process == 0 {
		return fmt.Errorf("open explicit PID: %w", e)
	}
	defer kernel32.NewProc("CloseHandle").Call(process)
	validate := func() error {
		var b [32768]uint16
		n := uint32(len(b))
		ok, _, e := kernel32.NewProc("QueryFullProcessImageNameW").Call(process, 0, uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&n)))
		if ok == 0 {
			return fmt.Errorf("process path: %w", e)
		}
		actual := syscall.UTF16ToString(b[:n])
		if !strings.EqualFold(filepath.Clean(actual), filepath.Clean(want)) {
			return fmt.Errorf("PID path does not match -exe")
		}
		state, _, _ := kernel32.NewProc("WaitForSingleObject").Call(process, 0)
		if state != 0x102 {
			return errors.New("target process exited")
		}
		return nil
	}
	if err = validate(); err != nil {
		return err
	}
	var window, controller uintptr
	cb := syscall.NewCallback(func(h, l uintptr) uintptr {
		var p uint32
		call("GetWindowThreadProcessId", h, uintptr(unsafe.Pointer(&p)))
		if p == uint32(*pid) && class(h) == "LumaTape.Settings" {
			window = h
		}
		if p == uint32(*pid) && class(h) == "LumaTape.Control" {
			controller = h
		}
		return 1
	})
	call("EnumWindows", cb, 0)

	actions := []step{{Op: "inspect"}}
	if *stepsPath != "" {
		b, e := os.ReadFile(*stepsPath)
		if e != nil {
			return e
		}
		if len(b) > 64<<10 {
			return errors.New("steps file too large")
		}
		if e = json.Unmarshal(bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf}), &actions); e != nil {
			return e
		}
		if len(actions) > 200 {
			return errors.New("at most 200 steps")
		}
	}
	output := os.Stdout
	if *outputPath != "" {
		var e error
		output, e = os.Create(*outputPath)
		if e != nil {
			return e
		}
		defer output.Close()
	}
	enc := json.NewEncoder(output)
	enc.SetEscapeHTML(false)
	for index, s := range actions {
		start := time.Now()
		r := result{Step: s}
		if err = validate(); err == nil && s.Op != "quit" {
			var owner uint32
			call("GetWindowThreadProcessId", window, uintptr(unsafe.Pointer(&owner)))
			if owner != uint32(*pid) || class(window) != "LumaTape.Settings" {
				err = errors.New("Settings ownership changed")
			}
		}
		if err == nil {
			if s.Op == "quit" {
				if index != len(actions)-1 {
					err = errors.New("quit must be the last step")
				} else {
					var owner uint32
					call("GetWindowThreadProcessId", controller, uintptr(unsafe.Pointer(&owner)))
					if controller == 0 || owner != uint32(*pid) || class(controller) != "LumaTape.Control" {
						err = errors.New("no validated same-PID LumaTape.Control")
					} else {
						_, err = send(controller, 0x10, 0, 0)
						if err == nil {
							state, _, _ := kernel32.NewProc("WaitForSingleObject").Call(process, 5000)
							if state != 0 {
								err = errors.New("graceful quit did not exit within 5 seconds; process was not terminated")
							} else {
								var code uint32
								ok, _, _ := kernel32.NewProc("GetExitCodeProcess").Call(process, uintptr(unsafe.Pointer(&code)))
								if ok == 0 {
									err = errors.New("cannot read exit code")
								} else {
									r.Exited = true
									r.ExitCode = &code
									if code != 0 {
										err = fmt.Errorf("application exited with code %d", code)
									}
								}
							}
						}
					}
				}
			} else {
				err = execute(window, s)
			}
		}
		if err == nil && s.Op != "quit" {
			r.Controls, err = inspect(window)
		}
		if err == nil && *configPath != "" {
			b, e := os.ReadFile(*configPath)
			if e != nil {
				err = e
			} else if len(b) > 1<<20 || !json.Valid(b) {
				err = errors.New("test config must be JSON <=1 MiB")
			} else {
				r.Config = b
			}
		}
		if err == nil && *logPath != "" {
			r.LogTail, err = tail(*logPath, 32<<10)
		}
		if err == nil {
			err = check(s, r.Controls, r.Config, r.LogTail)
		}
		r.ElapsedMS = time.Since(start).Milliseconds()
		if err != nil {
			r.Error = err.Error()
		}
		if e = enc.Encode(r); e != nil {
			return e
		}
		if err != nil {
			return err
		}
	}
	return nil
}
func class(h uintptr) string {
	var b [256]uint16
	call("GetClassNameW", h, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	return syscall.UTF16ToString(b[:])
}

//go:uintptrescapes
func send(h, msg, w, l uintptr) (uintptr, error) {
	var result uintptr
	r, _, e := user32.NewProc("SendMessageTimeoutW").Call(h, msg, w, l, 0x22, 2000, uintptr(unsafe.Pointer(&result)))
	if r == 0 {
		return 0, fmt.Errorf("message %#x failed/timed out (%v)", msg, e)
	}
	return result, nil
}
func textOf(h uintptr) (string, error) {
	var b [16384]uint16
	_, e := send(h, 0xd, uintptr(len(b)), uintptr(unsafe.Pointer(&b[0])))
	return syscall.UTF16ToString(b[:]), e
}
func execute(h uintptr, s step) error {
	if s.Op == "inspect" || strings.HasPrefix(s.Op, "expect-") {
		return nil
	}
	if s.Op == "wait" {
		if s.Millis < 0 || s.Millis > 5000 {
			return errors.New("wait must be 0..5000ms")
		}
		time.Sleep(time.Duration(s.Millis) * time.Millisecond)
		return nil
	}
	if s.Op == "foreground" {
		call("ShowWindow", h, 5)
		if call("SetForegroundWindow", h) == 0 {
			return errors.New("foreground request rejected")
		}
		return nil
	}
	if s.ID < 3000 || s.ID > 4200 {
		return errors.New("control ID outside app-owned range")
	}
	c := call("GetDlgItem", h, uintptr(s.ID))
	if c == 0 {
		return fmt.Errorf("control %d missing", s.ID)
	}
	var err error
	switch s.Op {
	case "select":
		if class(c) != "ComboBox" {
			return errors.New("select requires ComboBox")
		}
		n, e := send(c, 0x146, 0, 0)
		if e != nil {
			return e
		}
		if s.Index < 0 || uintptr(s.Index) >= n {
			return errors.New("combo index out of range")
		}
		_, err = send(c, 0x14e, uintptr(s.Index), 0)
		if err == nil {
			_, err = send(h, 0x111, uintptr(s.ID)|1<<16, c)
		}
	case "text":
		if class(c) != "Edit" {
			return errors.New("text requires Edit")
		}
		if len(s.Text) > 256 {
			return errors.New("edit text too long")
		}
		b, e := syscall.UTF16FromString(s.Text)
		if e != nil {
			return e
		}
		_, err = send(c, 0xc, 0, uintptr(unsafe.Pointer(&b[0])))
		runtime.KeepAlive(b)
	case "check":
		if class(c) != "Button" {
			return errors.New("check requires Button")
		}
		if s.Index < 0 || s.Index > 1 {
			return errors.New("check index must be 0 or 1")
		}
		_, err = send(c, 0xf1, uintptr(s.Index), 0)
		if err == nil {
			_, err = send(h, 0x111, uintptr(s.ID), c)
		}
	case "click":
		if class(c) != "Button" {
			return errors.New("click requires Button")
		}
		if call("IsWindowEnabled", c) == 0 {
			return errors.New("button disabled")
		}
		_, err = send(c, 0xf5, 0, 0)
	default:
		return fmt.Errorf("unknown op %q", s.Op)
	}
	return err
}
func inspect(h uintptr) ([]control, error) {
	var all []control
	for _, span := range [][2]int{{3000, 3033}, {4100, 4111}} {
		for id := span[0]; id <= span[1]; id++ {
			c := call("GetDlgItem", h, uintptr(id))
			if c == 0 {
				continue
			}
			t, e := textOf(c)
			if e != nil {
				return nil, e
			}
			r := control{ID: id, Class: class(c), Text: t, Visible: call("IsWindowVisible", c) != 0, Enabled: call("IsWindowEnabled", c) != 0}
			if r.Class == "ComboBox" {
				sel, e := send(c, 0x147, 0, 0)
				if e != nil {
					return nil, e
				}
				i := int(int32(sel))
				r.Selected = &i
				n, e := send(c, 0x146, 0, 0)
				if e != nil {
					return nil, e
				}
				if n > 256 {
					return nil, errors.New("unexpected combo length")
				}
				for j := uintptr(0); j < n; j++ {
					length, e := send(c, 0x149, j, 0)
					if e != nil || length > 4096 {
						return nil, errors.New("unexpected combo text length")
					}
					b := make([]uint16, length+1)
					_, e = send(c, 0x148, j, uintptr(unsafe.Pointer(&b[0])))
					if e != nil {
						return nil, e
					}
					r.Options = append(r.Options, syscall.UTF16ToString(b))
				}
			}
			if id == 3010 {
				v, e := send(c, 0xf0, 0, 0)
				if e != nil {
					return nil, e
				}
				b := v == 1
				r.Checked = &b
			}
			all = append(all, r)
		}
	}
	return all, nil
}
func tail(path string, max int64) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return "", e
	}
	start := st.Size() - max
	if start < 0 {
		start = 0
	}
	b := make([]byte, st.Size()-start)
	_, e = f.ReadAt(b, start)
	return string(b), e
}
