package main

import "testing"

func TestExtractJSONPayload_LastLineJSON(t *testing.T) {
	raw := []byte("ℹ️ INFO Calling reducer...\n{\"ok\":true,\"kind\":\"action\"}\n")
	got, err := extractJSONPayload(raw)
	if err != nil {
		t.Fatalf("extractJSONPayload() error = %v", err)
	}
	if string(got) != "{\"ok\":true,\"kind\":\"action\"}" {
		t.Fatalf("extractJSONPayload() = %q", string(got))
	}
}
