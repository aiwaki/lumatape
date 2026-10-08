package main

import "encoding/json"

type step struct {
	Field  string          `json:"field,omitempty"`
	Value  json.RawMessage `json:"value,omitempty"`
	Op     string          `json:"op"`
	ID     int             `json:"id"`
	Index  int             `json:"index"`
	Text   string          `json:"text"`
	Millis int             `json:"ms"`
}
type control struct {
	ID       int      `json:"id"`
	Class    string   `json:"class"`
	Text     string   `json:"text"`
	Visible  bool     `json:"visible"`
	Enabled  bool     `json:"enabled"`
	Selected *int     `json:"selected,omitempty"`
	Options  []string `json:"options,omitempty"`
	Checked  *bool    `json:"checked,omitempty"`
}
