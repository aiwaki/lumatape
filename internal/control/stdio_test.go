package control

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryOutput struct {
	mu sync.Mutex
	bytes.Buffer
}

func (w *memoryOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.Buffer.Write(p)
}
func (w *memoryOutput) Close() error { return nil }
func (w *memoryOutput) data() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.Buffer.Bytes()...)
}

func pipeServer(t *testing.T) (*Server, *io.PipeWriter, *memoryOutput) {
	t.Helper()
	in, writer := io.Pipe()
	out := &memoryOutput{}
	server := New(in, out)
	t.Cleanup(func() { _ = writer.Close(); _ = server.Close() })
	return server, writer, out
}
func sendRequest(t *testing.T, w io.Writer, id, kind string) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(Request{Version: 1, ID: id, Type: kind, Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
}
func nextRequest(t *testing.T, s *Server) Request {
	t.Helper()
	until := time.Now().Add(time.Second)
	for time.Now().Before(until) {
		if r, ok := s.Next(); ok {
			return r
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no request dispatched")
	return Request{}
}

func TestStdioRoundTripHasSingleProtocolWriter(t *testing.T) {
	s, w, out := pipeServer(t)
	sendRequest(t, w, "one", "snapshot")
	r := nextRequest(t, s)
	if r.ID != "one" || r.Type != "snapshot" {
		t.Fatalf("wrong command: %+v", r)
	}
	if !s.Reply(r.ID, map[string]any{"hwnd": "0xffffffffffffffff", "value": 1}, nil) {
		t.Fatal("reply rejected")
	}
	if !s.Emit("state_changed", map[string]string{"phase": "active"}) {
		t.Fatal("event rejected")
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(out.data()), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("not JSONL: %q", out.data())
	}
	var reply map[string]any
	if err := json.Unmarshal(lines[0], &reply); err != nil {
		t.Fatal(err)
	}
	if reply["id"] != "one" || reply["ok"] != true {
		t.Fatalf("bad response: %v", reply)
	}
	if strings.Contains(string(out.data()), "LumaTape:") {
		t.Fatal("human output polluted protocol")
	}
}

func TestEOFIsIndependentOfFullQueueAndUrgentSlots(t *testing.T) {
	s, w, _ := pipeServer(t)
	for i := 0; i < QueueCapacity; i++ {
		sendRequest(t, w, fmt.Sprint(i), "apply")
	}
	sendRequest(t, w, "stop", "emergency")
	sendRequest(t, w, "exit", "quit")
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Done():
	case <-time.After(time.Second):
		t.Fatal("EOF blocked behind full command queue")
	}
	if r := nextRequest(t, s); r.Type != "quit" {
		t.Fatalf("quit not first: %+v", r)
	}
	if r := nextRequest(t, s); r.Type != "emergency" {
		t.Fatalf("emergency not before queued applies: %+v", r)
	}
	if s.Err() != nil {
		t.Fatalf("ordinary EOF is not protocol corruption: %v", s.Err())
	}
}

func TestEmergencyCancelsQueuedMutableCommands(t *testing.T) {
	s, w, out := pipeServer(t)
	for i, kind := range []string{"apply", "toggle", "reload"} {
		sendRequest(t, w, fmt.Sprint(i), kind)
	}
	sendRequest(t, w, "stop", "emergency")
	until := time.Now().Add(time.Second)
	for len(s.emergency) == 0 && time.Now().Before(until) {
		time.Sleep(time.Millisecond)
	}
	if r := nextRequest(t, s); r.Type != "emergency" {
		t.Fatalf("wrong priority: %+v", r)
	}
	s.CancelQueued()
	if _, ok := s.Next(); ok {
		t.Fatal("old preferences could re-enable after emergency")
	}
	_ = w.Close()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(out.data()), "cancelled_by_emergency") != 3 {
		t.Fatalf("cancelled requests lost their response: %s", out.data())
	}
}

func TestDuplicateAndInvalidRequestsCannotRepeatMutation(t *testing.T) {
	s, w, out := pipeServer(t)
	sendRequest(t, w, "same", "apply")
	sendRequest(t, w, "same", "apply")
	_, _ = io.WriteString(w, "{\"v\":2,\"id\":\"old\",\"type\":\"apply\",\"payload\":{}}\n")
	_, _ = io.WriteString(w, "{\"v\":1,\"id\":\"unknown\",\"type\":\"apply\",\"extra\":1}\n")
	_ = w.Close()
	<-s.Done()
	r := nextRequest(t, s)
	if r.ID != "same" {
		t.Fatalf("wrong accepted command: %+v", r)
	}
	if _, ok := s.Next(); ok {
		t.Fatal("invalid or duplicate mutation dispatched")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	text := string(out.data())
	if !strings.Contains(text, "duplicate_id") || strings.Count(text, "invalid_request") != 2 {
		t.Fatalf("missing rejection responses: %s", text)
	}
}

func TestOversizedInputStopsWithoutUnboundedAllocation(t *testing.T) {
	s := New(io.NopCloser(strings.NewReader(strings.Repeat("x", MaxRequestBytes+100))), &memoryOutput{})
	select {
	case <-s.Done():
	case <-time.After(time.Second):
		t.Fatal("oversized frame did not terminate")
	}
	if s.Err() == nil {
		t.Fatal("oversized frame was accepted")
	}
	_ = s.Close()
}

type blockedOutput struct {
	once    sync.Once
	started chan struct{}
	closed  chan struct{}
}

func (w *blockedOutput) Write([]byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.closed
	return 0, io.ErrClosedPipe
}
func (w *blockedOutput) Close() error {
	select {
	case <-w.closed:
	default:
		close(w.closed)
	}
	return nil
}

func TestUnresponsiveHostCannotBlockRenderThreadReply(t *testing.T) {
	in, w := io.Pipe()
	out := &blockedOutput{started: make(chan struct{}), closed: make(chan struct{})}
	s := New(in, out)
	if !s.Reply("first", nil, nil) {
		t.Fatal("first reply failed")
	}
	<-out.started
	for i := 0; i < QueueCapacity; i++ {
		if !s.Reply(fmt.Sprint(i), nil, nil) {
			t.Fatal("queue smaller than contract")
		}
	}
	start := time.Now()
	if s.Reply("overflow", nil, nil) {
		t.Fatal("unbounded output accepted")
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("response enqueue blocked rendering")
	}
	select {
	case <-s.Done():
	default:
		t.Fatal("broken host did not request cleanup")
	}
	_ = w.Close()
	if err := s.Close(); err == nil {
		t.Fatal("lost output failure")
	}
}

func TestPayloadStrictness(t *testing.T) {
	for _, input := range []string{`{"unknown":1}`, `{} {}`, `{"value":"not integer"}`} {
		var v struct {
			Value int `json:"value"`
		}
		if DecodePayload(json.RawMessage(input), &v) == nil {
			t.Fatalf("accepted malformed payload: %s", input)
		}
	}
}

func TestOrderlyCloseDoesNotTurnBlockedReaderIntoFailure(t *testing.T) {
	for i := 0; i < 20; i++ {
		in, w := io.Pipe()
		s := New(in, &memoryOutput{})
		if err := s.Close(); err != nil {
			t.Fatalf("normal close became an input error: %v", err)
		}
		_ = w.Close()
	}
}

type shortOutput struct{}

func (shortOutput) Write(p []byte) (int, error) { return len(p) / 2, nil }
func (shortOutput) Close() error                { return nil }
func TestShortOutputCannotClaimDeliveredResponse(t *testing.T) {
	in, w := io.Pipe()
	s := New(in, shortOutput{})
	s.Reply("one", true, nil)
	select {
	case <-s.Done():
	case <-time.After(time.Second):
		t.Fatal("short write did not stop transport")
	}
	if !errors.Is(s.Err(), io.ErrShortWrite) {
		t.Fatalf("short response was accepted: %v", s.Err())
	}
	_ = w.Close()
	_ = s.Close()
}
