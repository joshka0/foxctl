package oputil

import (
	"errors"
	"testing"
)

func TestOp(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"list", "list"},
		{"LIST", "list"},
		{"  List  ", "list"},
		{"", ""},
	}

	for _, tt := range tests {
		if got := Op(tt.input); got != tt.want {
			t.Errorf("Op(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		op      string
		allowed []string
		wantErr bool
	}{
		{"list", []string{"list", "get", "add"}, false},
		{"LIST", []string{"list", "get", "add"}, false},
		{"  list  ", []string{"list", "get", "add"}, false},
		{"delete", []string{"list", "get", "add"}, true},
		{"", []string{"list"}, true},
	}

	for _, tt := range tests {
		err := Validate(tt.op, tt.allowed...)
		if (err != nil) != tt.wantErr {
			t.Errorf("Validate(%q, %v) error = %v, wantErr = %v", tt.op, tt.allowed, err, tt.wantErr)
		}
		if tt.wantErr && err != nil {
			var opErr *InvalidOpError
			if !errors.As(err, &opErr) {
				t.Errorf("Validate() error type = %T, want *InvalidOpError", err)
			}
		}
	}
}

func TestInvalidOpError(t *testing.T) {
	err := &InvalidOpError{
		Operation: "foo",
		Allowed:   []string{"list", "get", "add"},
	}
	msg := err.Error()
	if msg == "" {
		t.Error("InvalidOpError.Error() should not be empty")
	}
	// Check that allowed ops are mentioned
	for _, op := range []string{"add", "get", "list"} {
		if !containsSubstring(msg, op) {
			t.Errorf("InvalidOpError.Error() should mention %q", op)
		}
	}
}

func TestRequire(t *testing.T) {
	tests := []struct {
		value   string
		wantErr bool
	}{
		{"hello", false},
		{"", true},
		{"   ", true},
	}

	for _, tt := range tests {
		err := Require(tt.value, "test_field")
		if (err != nil) != tt.wantErr {
			t.Errorf("Require(%q) error = %v, wantErr = %v", tt.value, err, tt.wantErr)
		}
	}
}

func TestRequireInt(t *testing.T) {
	if err := RequireInt(0, "count"); err == nil {
		t.Error("RequireInt(0) should return error")
	}
	if err := RequireInt(1, "count"); err != nil {
		t.Errorf("RequireInt(1) unexpected error: %v", err)
	}
}

func TestRequirePtr(t *testing.T) {
	if err := RequirePtr[int](nil, "ptr"); err == nil {
		t.Error("RequirePtr(nil) should return error")
	}
	val := 42
	if err := RequirePtr(&val, "ptr"); err != nil {
		t.Errorf("RequirePtr(&val) unexpected error: %v", err)
	}
}

func TestMissingFieldError(t *testing.T) {
	err := &MissingFieldError{Field: "path"}
	if !containsSubstring(err.Error(), "path") {
		t.Error("MissingFieldError.Error() should mention field name")
	}
}

func TestDispatcher(t *testing.T) {
	called := ""
	d := NewDispatcher().
		On("list", func() (map[string]any, error) {
			called = "list"
			return map[string]any{"op": "list"}, nil
		}).
		On("add", func() (map[string]any, error) {
			called = "add"
			return map[string]any{"op": "add"}, nil
		}).
		Alias("ls", "list")

	// Test basic dispatch
	result, err := d.Dispatch("list")
	if err != nil {
		t.Errorf("Dispatch(list) error = %v", err)
	}
	if called != "list" {
		t.Errorf("Dispatch(list) called = %q, want 'list'", called)
	}
	if result["op"] != "list" {
		t.Errorf("Dispatch(list) result = %v", result)
	}

	// Test alias
	called = ""
	_, err = d.Dispatch("ls")
	if err != nil {
		t.Errorf("Dispatch(ls) error = %v", err)
	}
	if called != "list" {
		t.Errorf("Dispatch(ls) called = %q, want 'list'", called)
	}

	// Test case insensitive
	called = ""
	_, err = d.Dispatch("ADD")
	if err != nil {
		t.Errorf("Dispatch(ADD) error = %v", err)
	}
	if called != "add" {
		t.Errorf("Dispatch(ADD) called = %q, want 'add'", called)
	}

	// Test unknown operation
	_, err = d.Dispatch("delete")
	if err == nil {
		t.Error("Dispatch(delete) should return error")
	}
}

func TestDispatcher_AllowedOps(t *testing.T) {
	d := NewDispatcher().
		On("list", func() (map[string]any, error) { return nil, nil }).
		On("add", func() (map[string]any, error) { return nil, nil }).
		On("get", func() (map[string]any, error) { return nil, nil })

	ops := d.AllowedOps()
	if len(ops) != 3 {
		t.Errorf("AllowedOps() = %v, want 3 items", ops)
	}
	// Should be sorted
	if ops[0] != "add" || ops[1] != "get" || ops[2] != "list" {
		t.Errorf("AllowedOps() = %v, should be sorted", ops)
	}
}

func TestDefaultOp(t *testing.T) {
	tests := []struct {
		op        string
		defaultOp string
		want      string
	}{
		{"", "list", "list"},
		{"   ", "list", "list"},
		{"add", "list", "add"},
	}

	for _, tt := range tests {
		if got := DefaultOp(tt.op, tt.defaultOp); got != tt.want {
			t.Errorf("DefaultOp(%q, %q) = %q, want %q", tt.op, tt.defaultOp, got, tt.want)
		}
	}
}

func TestRequireForOp(t *testing.T) {
	tests := []struct {
		op          string
		value       string
		requiredOps []string
		wantErr     bool
	}{
		{"add", "value", []string{"add", "remove"}, false},
		{"add", "", []string{"add", "remove"}, true},
		{"list", "", []string{"add", "remove"}, false}, // not required for list
		{"list", "value", []string{"add", "remove"}, false},
	}

	for _, tt := range tests {
		err := RequireForOp(tt.op, tt.value, "path", tt.requiredOps...)
		if (err != nil) != tt.wantErr {
			t.Errorf("RequireForOp(%q, %q, %v) error = %v, wantErr = %v",
				tt.op, tt.value, tt.requiredOps, err, tt.wantErr)
		}
	}
}

func TestRequireIntForOp(t *testing.T) {
	// Required for "get"
	err := RequireIntForOp("get", 0, "id", "get", "delete")
	if err == nil {
		t.Error("RequireIntForOp(get, 0) should return error")
	}

	// Not required for "list"
	err = RequireIntForOp("list", 0, "id", "get", "delete")
	if err != nil {
		t.Errorf("RequireIntForOp(list, 0) unexpected error: %v", err)
	}
}

func TestSwitch(t *testing.T) {
	listCalled := false
	addCalled := false

	result, err := NewSwitch("list").
		Case("list", func() (map[string]any, error) {
			listCalled = true
			return map[string]any{"items": []string{}}, nil
		}).
		Case("add", func() (map[string]any, error) {
			addCalled = true
			return map[string]any{"added": true}, nil
		}).
		Run()
	if err != nil {
		t.Errorf("Switch.Run() error = %v", err)
	}
	if !listCalled {
		t.Error("Switch.Run() should have called list handler")
	}
	if addCalled {
		t.Error("Switch.Run() should not have called add handler")
	}
	if result["items"] == nil {
		t.Error("Switch.Run() result missing items")
	}
}

func TestSwitch_Default(t *testing.T) {
	called := ""

	_, err := NewSwitch("").
		Case("list", func() (map[string]any, error) {
			called = "list"
			return nil, nil
		}).
		Default("list").
		Run()
	if err != nil {
		t.Errorf("Switch.Run() with default error = %v", err)
	}
	if called != "list" {
		t.Errorf("Switch.Run() with default called = %q, want 'list'", called)
	}
}

func TestSwitch_Alias(t *testing.T) {
	called := ""

	_, err := NewSwitch("ls").
		Case("list", func() (map[string]any, error) {
			called = "list"
			return nil, nil
		}).
		Alias("ls", "list").
		Run()
	if err != nil {
		t.Errorf("Switch.Run() with alias error = %v", err)
	}
	if called != "list" {
		t.Errorf("Switch.Run() with alias called = %q, want 'list'", called)
	}
}

func TestSwitch_InvalidOp(t *testing.T) {
	_, err := NewSwitch("delete").
		Case("list", func() (map[string]any, error) { return nil, nil }).
		Case("add", func() (map[string]any, error) { return nil, nil }).
		Run()

	if err == nil {
		t.Error("Switch.Run() with invalid op should return error")
	}

	var opErr *InvalidOpError
	if !errors.As(err, &opErr) {
		t.Errorf("Switch.Run() error type = %T, want *InvalidOpError", err)
	}
}

func TestSwitch_HandlerError(t *testing.T) {
	expectedErr := errors.New("handler failed")

	_, err := NewSwitch("fail").
		Case("fail", func() (map[string]any, error) {
			return nil, expectedErr
		}).
		Run()

	if !errors.Is(err, expectedErr) {
		t.Errorf("Switch.Run() error = %v, want %v", err, expectedErr)
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
