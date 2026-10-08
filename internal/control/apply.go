package control

import "encoding/json"

// WindowSource is an opaque, live identity obtained from the sources command.
// HWND and creation time remain strings so clients cannot lose integer precision.
type WindowSource struct {
	ID             string `json:"id"`
	HWND           string `json:"hwnd"`
	PID            uint32 `json:"pid"`
	ProcessCreated string `json:"process_created"`
	Title          string `json:"title"`
}

// ApplyPayload commits a complete draft. ExpectedConfig is optional for older
// clients; new clients echo the full config snapshot used to prepare the draft.
// RawMessage distinguishes an absent precondition from an invalid JSON null.
type ApplyPayload struct {
	Config                    json.RawMessage `json:"config"`
	Source                    *WindowSource   `json:"source"`
	ExpectedEmergencySequence *uint64         `json:"expected_emergency_sequence"`
	ExpectedConfig            json.RawMessage `json:"expected_config,omitempty"`
}
