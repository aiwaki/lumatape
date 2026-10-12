package control

import (
	"encoding/json"
	"testing"
)

func TestApplyPayloadPreservesOptionalPreconditionAndWindowIdentity(t *testing.T) {
	var legacy ApplyPayload
	if err := DecodePayload([]byte(`{"config":{},"expected_emergency_sequence":0}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.ExpectedConfig != nil || legacy.ExpectedEmergencySequence == nil || *legacy.ExpectedEmergencySequence != 0 {
		t.Fatal("legacy payload lost its absence/current-sequence distinction")
	}
	var guarded ApplyPayload
	data := []byte(`{"config":{},"expected_emergency_sequence":9,"expected_config":null,"source":{"id":"window:0xffff:42:18446744073709551615","hwnd":"0xffff","pid":42,"process_created":"18446744073709551615","title":"Игра"}}`)
	if err := DecodePayload(data, &guarded); err != nil {
		t.Fatal(err)
	}
	if string(guarded.ExpectedConfig) != "null" {
		t.Fatal("explicit null must reach validation rather than disable CAS")
	}
	if guarded.Source == nil || guarded.Source.HWND != "0xffff" || guarded.Source.ProcessCreated != "18446744073709551615" || guarded.Source.Title != "Игра" {
		t.Fatal("window identity changed during payload decoding")
	}
	roundTrip, err := json.Marshal(guarded)
	if err != nil {
		t.Fatal(err)
	}
	var restored ApplyPayload
	if err := DecodePayload(roundTrip, &restored); err != nil || *restored.Source != *guarded.Source {
		t.Fatalf("source roundtrip failed: %v", err)
	}
}

func TestWindowFormatRetryPayloadIsExplicitAndStrict(t *testing.T) {
	for _, input := range []string{
		`{"config":{},"reapply_window_format":true}`,
		`{"config":{},"reapply_window_format":false}`,
		`{"config":{}}`,
	} {
		var p ApplyPayload
		if err := DecodePayload([]byte(input), &p); err != nil {
			t.Fatal(err)
		}
		if p.ReapplyWindowFormat != (input == `{"config":{},"reapply_window_format":true}`) {
			t.Fatal("ordinary apply became an explicit window retry")
		}
	}
	for _, invalid := range []string{
		`{"config":{},"reapply_window_format":"true"}`,
		`{"config":{},"reapply_window_format":1}`,
		`{"config":{},"reapply_system_format":true}`,
	} {
		var p ApplyPayload
		if err := DecodePayload([]byte(invalid), &p); err == nil {
			t.Fatalf("accepted malformed retry payload: %s", invalid)
		}
	}
}
