package devicebridge

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestRequestValidationAllowsOnlySemanticMethods(t *testing.T) {
	allowed := []Method{
		MethodHello, MethodCapabilities, MethodPrepare,
		MethodStart, MethodStatus, MethodStop, MethodClose,
	}
	for _, method := range allowed {
		request := Request{SchemaVersion: 1, RequestID: "request-123", Method: method}
		if err := request.Validate(); err != nil {
			t.Fatalf("%s rejected: %v", method, err)
		}
	}
	for _, method := range []Method{"shell", "exec", "read_file", "write_file", "download_url"} {
		request := Request{SchemaVersion: 1, RequestID: "request-123", Method: method}
		if err := request.Validate(); !errors.Is(err, ErrUnsupportedMethod) {
			t.Fatalf("%s must be rejected, got %v", method, err)
		}
	}
}

func TestRequestValidationRejectsInvalidEnvelopeAndPayload(t *testing.T) {
	tests := []Request{
		{SchemaVersion: 2, RequestID: "request-123", Method: MethodStatus},
		{SchemaVersion: 1, RequestID: "short", Method: MethodStatus},
		{SchemaVersion: 1, RequestID: "request-123", Method: MethodStatus, Payload: json.RawMessage("{")},
	}
	for _, request := range tests {
		if err := request.Validate(); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("request %#v accepted", request)
		}
	}
}

func TestJSONSchemaRoundTrip(t *testing.T) {
	request := Request{
		SchemaVersion: 1,
		RequestID:     "request-123",
		Method:        MethodPrepare,
		Payload:       json.RawMessage(`{"profile":"dev","encrypted_config":"ciphertext","expires_at":"2026-11-07T20:00:00Z"}`),
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Request
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := decoded.Validate(); err != nil {
		t.Fatal(err)
	}
}
