// Package control is the bounded, local JSONL transport between the desktop
// host and the rendering process. It never executes native or rendering work.
package control

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

const Version = 1
const MaxRequestBytes = 128 << 10
const MaxResponseBytes = 2 << 20
const QueueCapacity = 16

type Request struct {
	Version int             `json:"v"`
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type Failure struct {
	Code             string `json:"code"`
	Message          string `json:"message"`
	Applied          bool   `json:"applied"`
	Unsaved          bool   `json:"unsaved"`
	SuggestedBackend string `json:"suggested_backend,omitempty"`
}

func (f *Failure) Error() string { return f.Message }

type Response struct {
	Version int      `json:"v"`
	ID      string   `json:"id"`
	OK      bool     `json:"ok"`
	Result  any      `json:"result,omitempty"`
	Error   *Failure `json:"error,omitempty"`
}

type Event struct {
	Version int    `json:"v"`
	Event   string `json:"event"`
	Data    any    `json:"data,omitempty"`
}

type Server struct {
	in                  io.ReadCloser
	out                 io.WriteCloser
	requests            chan Request
	emergency, quit     chan Request
	writes              chan []byte
	done, written       chan struct{}
	stopOnce, closeOnce sync.Once
	mu                  sync.Mutex
	err                 error
	wake                func()
	writeMu             sync.Mutex
	closed              bool
}

func New(in io.ReadCloser, out io.WriteCloser) *Server {
	s := &Server{in: in, out: out, requests: make(chan Request, QueueCapacity), emergency: make(chan Request, 1), quit: make(chan Request, 1), writes: make(chan []byte, QueueCapacity), done: make(chan struct{}), written: make(chan struct{})}
	go s.readLoop()
	go s.writeLoop()
	return s
}

func (s *Server) Done() <-chan struct{} { return s.done }
func (s *Server) Err() error            { s.mu.Lock(); defer s.mu.Unlock(); return s.err }
func (s *Server) SetWake(wake func())   { s.mu.Lock(); s.wake = wake; s.mu.Unlock() }
func (s *Server) notify() {
	s.mu.Lock()
	wake := s.wake
	s.mu.Unlock()
	if wake != nil {
		wake()
	}
}
func (s *Server) stop(err error) {
	if err != nil {
		s.mu.Lock()
		if s.err == nil {
			s.err = err
		}
		s.mu.Unlock()
	}
	s.stopOnce.Do(func() { close(s.done); s.notify() })
}

func DecodePayload(data json.RawMessage, destination any) error {
	if len(data) == 0 {
		data = json.RawMessage(`{}`)
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(destination); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("payload contains trailing data")
	}
	return nil
}

func (s *Server) readLoop() {
	scanner := bufio.NewScanner(s.in)
	scanner.Buffer(make([]byte, 4096), MaxRequestBytes)
	seen := make(map[string]bool)
	var recent [256]string
	next := 0
	for scanner.Scan() {
		select {
		case <-s.done:
			return
		default:
		}
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		var r Request
		err := DecodePayload(scanner.Bytes(), &r)
		if err != nil || r.Version != Version || len(r.ID) == 0 || len(r.ID) > 64 || len(r.Type) == 0 || len(r.Type) > 32 {
			message := "invalid v1 request envelope"
			if err != nil {
				message = err.Error()
			}
			s.Reply(r.ID, nil, &Failure{Code: "invalid_request", Message: message})
			continue
		}
		if seen[r.ID] {
			s.Reply(r.ID, nil, &Failure{Code: "duplicate_id", Message: "request ID was already received"})
			continue
		}
		delete(seen, recent[next])
		recent[next] = r.ID
		next = (next + 1) % len(recent)
		seen[r.ID] = true
		queue := s.requests
		if r.Type == "emergency" {
			queue = s.emergency
		}
		if r.Type == "quit" {
			queue = s.quit
		}
		select {
		case queue <- r:
			s.notify()
		default:
			s.Reply(r.ID, nil, &Failure{Code: "busy", Message: "engine command queue is full"})
		}
	}
	select {
	case <-s.done:
		return
	default:
	} // intentional Close is not an input failure
	if err := scanner.Err(); err != nil {
		s.stop(fmt.Errorf("read controller: %w", err))
	} else {
		s.stop(nil)
	}
}

// Next is called only by the locked rendering thread. Terminal and emergency
// actions have dedicated slots, so a full normal queue cannot postpone them.
func (s *Server) Next() (Request, bool) {
	select {
	case r := <-s.quit:
		return r, true
	default:
	}
	select {
	case r := <-s.emergency:
		return r, true
	default:
	}
	select {
	case r := <-s.requests:
		return r, true
	default:
		return Request{}, false
	}
}

func (s *Server) CancelQueued() {
	for {
		select {
		case r := <-s.requests:
			s.Reply(r.ID, nil, &Failure{Code: "cancelled_by_emergency", Message: "queued action cancelled by emergency stop"})
		default:
			return
		}
	}
}

func (s *Server) send(value any, critical bool) bool {
	data, err := json.Marshal(value)
	if err != nil || len(data) > MaxResponseBytes {
		s.stop(errors.New("controller response could not be encoded within the size limit"))
		return false
	}
	data = append(data, '\n')
	s.writeMu.Lock()
	if s.closed {
		s.writeMu.Unlock()
		return false
	}
	select {
	case s.writes <- data:
		s.writeMu.Unlock()
		return true
	default:
		s.writeMu.Unlock()
		if critical {
			s.stop(errors.New("controller stopped reading responses"))
		}
		return false
	}
}
func (s *Server) Reply(id string, result any, failure *Failure) bool {
	return s.send(Response{Version: Version, ID: id, OK: failure == nil, Result: result, Error: failure}, true)
}
func (s *Server) Emit(event string, data any) bool {
	return s.send(Event{Version: Version, Event: event, Data: data}, event != "state_changed")
}

func (s *Server) writeLoop() {
	defer close(s.written)
	for data := range s.writes {
		n, err := s.out.Write(data)
		if err == nil && n != len(data) {
			err = io.ErrShortWrite
		}
		if err != nil {
			s.stop(fmt.Errorf("write controller: %w", err))
			return
		}
	}
}

// Close drains accepted replies with a bounded wait. The render thread never
// waits indefinitely for an unresponsive UI to consume its pipe.
func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		s.stop(nil)
		_ = s.in.Close()
		s.writeMu.Lock()
		s.closed = true
		close(s.writes)
		s.writeMu.Unlock()
		select {
		case <-s.written:
		case <-time.After(time.Second):
			s.stop(errors.New("controller response flush timed out"))
		}
		_ = s.out.Close()
	})
	return s.Err()
}
