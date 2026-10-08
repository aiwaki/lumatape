package display

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// RunWatchdog is the companion process's entry point. The inherited anonymous
// pipes are private to these two processes; nothing is read from a shared file.
func RunWatchdog(input io.Reader, output io.Writer) error {
	if !supported() {
		return ErrUnsupported
	}
	return runWatchdog(input, output, newDriver)
}

func runWatchdog(input io.Reader, output io.Writer, factory func(string) (driver, error)) (result error) {
	encoder := json.NewEncoder(output)
	var g *guard
	send := func(event string, err error) error {
		r := response{Event: event}
		if err != nil {
			r.Error = err.Error()
		}
		if event == "applied" && err == nil && g != nil {
			r.DeadlineUnixNano = g.deadline.UnixNano()
		}
		return encoder.Encode(r)
	}
	// A bounded queue prevents an input flood from postponing the timer.
	commands := make(chan request, 1)
	inputEnded := make(chan struct{})
	stopReader := make(chan struct{})
	defer close(stopReader)
	go func() {
		defer close(inputEnded)
		decoder := json.NewDecoder(io.LimitReader(input, 1<<20))
		for {
			var command request
			if decoder.Decode(&command) != nil {
				return
			}
			select {
			case commands <- command:
			case <-stopReader:
				return
			}
		}
	}()
	defer func() {
		if g != nil {
			// Display reconfiguration can briefly make enumeration fail. Stay
			// alive for bounded retries instead of abandoning the original mode
			// on the first transient driver error; each attempt compares current
			// state again and permanently respects any independent change.
			var restoreErr error
			for attempt := 0; attempt < 20; attempt++ {
				restoreErr = g.restore()
				if restoreErr == nil {
					break
				}
				if attempt < 19 {
					time.Sleep(250 * time.Millisecond)
				}
			}
			result = errors.Join(result, restoreErr)
		}
		// Publish completion only after the final recovery attempt. A parent
		// that already observed EOF/timeout must never infer restoration merely
		// from a process exit or from a reply sent before failed recovery.
		_ = send("restored", result)
	}()
	if err := send("ready", nil); err != nil {
		return err
	}
	// A ready watchdog abandoned before apply also exits without waiting forever.
	startup := time.NewTimer(10 * time.Second)
	defer startup.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-inputEnded:
			return nil // The deferred conditional restoration handles crashes.
		case <-startup.C:
			if g == nil {
				return errors.New("watchdog startup request timed out")
			}
		case now := <-ticker.C:
			if g != nil {
				finished, err := g.tick(now)
				if err != nil {
					return err
				}
				if finished {
					return nil
				}
			}
		case command := <-commands:
			// Priority check closes the buffered-request-after-EOF race. If
			// the parent disappeared, do not start a new display transition.
			select {
			case <-inputEnded:
				return nil
			default:
			}
			switch command.Command {
			case "apply":
				if g != nil {
					return errors.New("watchdog accepts one display session")
				}
				if command.TimeoutMS < 5000 || command.TimeoutMS > 60000 {
					return errors.New("invalid confirmation deadline")
				}
				d, err := factory(command.Device)
				if err == nil {
					g = &guard{driver: d}
					err = g.start(command.Mode, time.Now(), time.Duration(command.TimeoutMS)*time.Millisecond)
				}
				if writeErr := send("applied", err); writeErr != nil {
					return writeErr
				}
				if err != nil {
					return err
				}
				startup.Stop()
			case "confirm":
				if g == nil {
					return errors.New("no display session to confirm")
				}
				err := g.confirm(time.Now())
				if writeErr := send("confirmed", err); writeErr != nil {
					return writeErr
				}
				if err != nil {
					return err
				}
			case "restore":
				var err error
				if g != nil {
					err = g.restore()
				}
				return err
			default:
				return fmt.Errorf("unknown watchdog command %q", command.Command)
			}
		}
	}
}
