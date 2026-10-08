package main

import (
	"encoding/json"
	"testing"
)

func TestAssertionsRequireObservedResults(t *testing.T) {
	i := 4
	b := true
	c := []control{{ID: 3007, Text: "VHS Tape", Selected: &i, Checked: &b, Visible: true}}
	config := json.RawMessage(`{"mode":"full","capture":{"transfer":"compatibility"}}`)
	for _, s := range []step{{Op: "expect-select", ID: 3007, Index: 4}, {Op: "expect-text", ID: 3007, Text: "Tape"}, {Op: "expect-config", Field: "capture.transfer", Value: json.RawMessage(`"compatibility"`)}, {Op: "expect-log", Text: "capture_opened"}} {
		if e := check(s, c, config, "capture_opened"); e != nil {
			t.Fatal(e)
		}
	}
	for _, s := range []step{{Op: "expect-select", ID: 3007, Index: 0}, {Op: "expect-text", ID: 3007, Text: "missing"}, {Op: "expect-config", Field: "capture.transfer", Value: json.RawMessage(`"gpu"`)}, {Op: "expect-log", Text: "absent"}, {Op: "expect-text", ID: 999, Text: "Tape"}} {
		if e := check(s, c, config, "capture_opened"); e == nil {
			t.Fatalf("false pass %+v", s)
		}
	}
}
