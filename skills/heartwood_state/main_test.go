package main

import "testing"

func TestExtractJSONPayload_LastLineJSON(t *testing.T) {
	raw := []byte("ℹ️ INFO Connecting to SpacetimeDB WS...\n{\"ok\":true,\"kind\":\"state\"}\n")
	got, err := extractJSONPayload(raw)
	if err != nil {
		t.Fatalf("extractJSONPayload() error = %v", err)
	}
	if string(got) != "{\"ok\":true,\"kind\":\"state\"}" {
		t.Fatalf("extractJSONPayload() = %q", string(got))
	}
}
