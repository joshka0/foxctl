package secrets

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	input := "Authorization: Bearer abcdefghijklmnopqrstuvwxyz"
	got := Redact(input)
	if got == input {
		t.Fatalf("expected redaction; got %q", got)
	}
	if got != "Authorization: Bearer ***" {
		t.Fatalf("unexpected redact output: %q", got)
	}
}

func TestRedactMap(t *testing.T) {
	m := map[string]any{
		"password": "supersecret",
		"nested":   map[string]any{"token": "abc"},
		"names":    []any{"value", map[string]any{"api_key": "123456789012345678901234"}},
	}
	redacted := RedactMap(m)
	if redacted["password"].(string) != "***" {
		t.Fatalf("expected password redacted")
	}
	nested := redacted["nested"].(map[string]any)
	if nested["token"].(string) != "***" {
		t.Fatalf("expected nested token redacted")
	}
	slice := redacted["names"].([]any)
	nestedMap := slice[1].(map[string]any)
	if nestedMap["api_key"].(string) != "***" {
		t.Fatalf("expected api key redacted in slice")
	}
}

func TestRedactHeaders(t *testing.T) {
	headers := map[string]string{
		"Authorization": "Bearer foo",
		"X-Trace":       "abc",
	}
	got := RedactHeaders(headers)
	if got["Authorization"] != "***" {
		t.Fatalf("expected auth header redacted")
	}
	if got["X-Trace"] != "abc" {
		t.Fatalf("expected non-secret header unchanged")
	}
}

func TestRedactJSONStyleKeys(t *testing.T) {
	input := `{"password":"secret","api_key":"ABCDEFGHIJKLMNOPQRST","token":"tok12345678901234567890"}`
	got := Redact(input)
	if got == input {
		t.Fatalf("expected redaction for JSON-style keys")
	}
	if want := `"password":"***"`; !strings.Contains(got, want) {
		t.Fatalf("expected password redaction, got %q", got)
	}
	if want := `"api_key":"***"`; !strings.Contains(got, want) {
		t.Fatalf("expected api_key redaction, got %q", got)
	}
	if want := `"token":"***"`; !strings.Contains(got, want) {
		t.Fatalf("expected token redaction, got %q", got)
	}
}
