package observability

import (
	"strings"
	"testing"
)

func TestRedactString_BearerToken(t *testing.T) {
	got := RedactString("Authorization: bearer abc123def456")
	if strings.Contains(got, "abc123def456") {
		t.Errorf("bearer token not redacted: %s", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in output: %s", got)
	}
}

func TestRedactString_PasswordAssignment(t *testing.T) {
	got := RedactString("password=secret123")
	if strings.Contains(got, "secret123") {
		t.Errorf("password not redacted: %s", got)
	}
}

func TestRedactString_APIKeyAssignment(t *testing.T) {
	got := RedactString(`api_key: "sk-abc123def456ghi789jkl"`)
	if strings.Contains(got, "sk-abc123") {
		t.Errorf("api key not redacted: %s", got)
	}
}

func TestRedactString_NonSensitivePreserved(t *testing.T) {
	input := "operation completed successfully in 150ms"
	got := RedactString(input)
	if got != input {
		t.Errorf("non-sensitive string was modified: %s", got)
	}
}

func TestRedactString_Base64Long(t *testing.T) {
	// 44-char base64 string should be redacted
	longB64 := "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY3ODkw"
	got := RedactString("key=" + longB64)
	if strings.Contains(got, longB64) {
		t.Errorf("long base64 not redacted: %s", got)
	}
}

func TestRedactString_ShortBase64Preserved(t *testing.T) {
	// Short base64 (< 40 chars) should be preserved
	input := "short ABC123== value"
	got := RedactString(input)
	if got != input {
		t.Errorf("short base64 was incorrectly redacted: %s", got)
	}
}

func TestRedactEvent_NilEvent(t *testing.T) {
	RedactEvent(nil) // should not panic
}

func TestRedactEvent_ErrorMessage(t *testing.T) {
	event := &Event{
		ErrorMessage: "auth failed: bearer tok_abc123xyz",
	}
	RedactEvent(event)
	if strings.Contains(event.ErrorMessage, "tok_abc123xyz") {
		t.Errorf("error message not redacted: %s", event.ErrorMessage)
	}
}

func TestRedactEvent_SensitiveDataKeys(t *testing.T) {
	event := &Event{
		Data: map[string]any{
			"token":      "secret-value",
			"api_key":    "sk-12345",
			"count":      42,
			"safe_field": "hello world",
			"password":   "hunter2",
			"nested": map[string]any{
				"credential": "nested-secret",
				"name":       "preserved",
			},
		},
	}

	RedactEvent(event)

	if event.Data["token"] != "[REDACTED]" {
		t.Errorf("token not fully redacted: %v", event.Data["token"])
	}
	if event.Data["api_key"] != "[REDACTED]" {
		t.Errorf("api_key not fully redacted: %v", event.Data["api_key"])
	}
	if event.Data["password"] != "[REDACTED]" {
		t.Errorf("password not fully redacted: %v", event.Data["password"])
	}
	if event.Data["count"] != 42 {
		t.Errorf("non-string count was modified: %v", event.Data["count"])
	}
	if event.Data["safe_field"] != "hello world" {
		t.Errorf("safe field was modified: %v", event.Data["safe_field"])
	}

	nested, ok := event.Data["nested"].(map[string]any)
	if !ok {
		t.Fatal("nested map lost its type")
	}
	if nested["credential"] != "[REDACTED]" {
		t.Errorf("nested credential not redacted: %v", nested["credential"])
	}
	if nested["name"] != "preserved" {
		t.Errorf("nested name was modified: %v", nested["name"])
	}
}

func TestRedactEvent_EmptyData(t *testing.T) {
	event := &Event{
		ErrorMessage: "simple error",
	}
	RedactEvent(event)
	if event.ErrorMessage != "simple error" {
		t.Errorf("non-sensitive error message was modified: %s", event.ErrorMessage)
	}
}

func TestRedactEvent_NilData(t *testing.T) {
	event := &Event{
		ErrorMessage: "context deadline exceeded",
	}
	RedactEvent(event)
	if event.Data != nil {
		t.Errorf("nil data should remain nil")
	}
}

func TestIsSensitiveKey(t *testing.T) {
	tests := []struct {
		key       string
		sensitive bool
	}{
		{"password", true},
		{"api_key", true},
		{"API_KEY", true},
		{"X-Auth-Token", true},
		{"user_credential", true},
		{"private_key", true},
		{"access_key", true},
		{"session_key", true},
		{"name", false},
		{"count", false},
		{"duration_ms", false},
		{"workspace_id", false},
	}

	for _, tt := range tests {
		got := isSensitiveKey(tt.key)
		if got != tt.sensitive {
			t.Errorf("isSensitiveKey(%q) = %v, want %v", tt.key, got, tt.sensitive)
		}
	}
}
