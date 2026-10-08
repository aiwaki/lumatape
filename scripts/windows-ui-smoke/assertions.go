package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// Each expectation validates an observable result, not only message delivery.
func check(s step, controls []control, config json.RawMessage, log string) error {
	if !strings.HasPrefix(s.Op, "expect-") {
		return nil
	}
	if s.Op == "expect-log" {
		if s.Text == "" || !strings.Contains(log, s.Text) {
			return fmt.Errorf("log does not contain %q", s.Text)
		}
		return nil
	}
	if s.Op == "expect-config" {
		var actual, want any
		if err := json.Unmarshal(config, &actual); err != nil {
			return fmt.Errorf("config expectation needs -config: %w", err)
		}
		if err := json.Unmarshal(s.Value, &want); err != nil {
			return fmt.Errorf("expect-config needs JSON value: %w", err)
		}
		for _, field := range strings.Split(s.Field, ".") {
			m, ok := actual.(map[string]any)
			if !ok {
				return fmt.Errorf("config field %q not found", s.Field)
			}
			actual, ok = m[field]
			if !ok {
				return fmt.Errorf("config field %q not found", s.Field)
			}
		}
		if !reflect.DeepEqual(actual, want) {
			return fmt.Errorf("config %s: got %v want %v", s.Field, actual, want)
		}
		return nil
	}
	var c *control
	for i := range controls {
		if controls[i].ID == s.ID {
			c = &controls[i]
			break
		}
	}
	if c == nil {
		return fmt.Errorf("expectation control %d missing", s.ID)
	}
	switch s.Op {
	case "expect-select":
		if c.Selected == nil || *c.Selected != s.Index {
			return fmt.Errorf("control %d selected index differs from %d", s.ID, s.Index)
		}
	case "expect-text":
		if !strings.Contains(c.Text, s.Text) {
			return fmt.Errorf("control %d text %q does not contain %q", s.ID, c.Text, s.Text)
		}
	case "expect-check":
		if c.Checked == nil || *c.Checked != (s.Index == 1) {
			return fmt.Errorf("control %d checked state differs", s.ID)
		}
	case "expect-visible":
		if c.Visible != (s.Index == 1) {
			return fmt.Errorf("control %d visibility differs", s.ID)
		}
	case "expect-enabled":
		if c.Enabled != (s.Index == 1) {
			return fmt.Errorf("control %d enabled state differs", s.ID)
		}
	default:
		return fmt.Errorf("unknown expectation %q", s.Op)
	}
	return nil
}
