package cmd

import "testing"

func TestRejectBackticks(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		value   string
		wantErr bool
	}{
		{name: "no backticks", field: "title", value: "normal title"},
		{name: "empty value", field: "description", value: ""},
		{name: "contains backtick", field: "title", value: "value with ` backtick", wantErr: true},
		{name: "multiple backticks", field: "notes", value: "`foo` and `bar`", wantErr: true},
		{name: "special chars", field: "title", value: "Title with !@#$%^&*()", wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := rejectBackticks(tt.field, tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("rejectBackticks() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewTodoCommand(t *testing.T) {
	cmd := newTodoCommand()
	if cmd.Use != "todo" {
		t.Fatalf("expected use todo, got %s", cmd.Use)
	}
	subs := cmd.Commands()
	expected := []string{"add", "complete", "list", "active"}
	if len(subs) != len(expected) {
		t.Fatalf("expected %d subcommands, got %d", len(expected), len(subs))
	}
	got := map[string]bool{}
	for _, sub := range subs {
		got[sub.Use] = true
	}
	for _, name := range expected {
		if !got[name] {
			t.Fatalf("expected subcommand %s to exist", name)
		}
	}
}

func TestTodoAddFlags(t *testing.T) {
	cmd := newTodoAddCommand()
	if cmd.Use != "add" {
		t.Fatalf("expected add command, got %s", cmd.Use)
	}
	for _, flag := range []string{"title", "description", "parent", "depends-on", "scope", "workspace"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Fatalf("expected flag --%s", flag)
		}
	}
}

func TestTodoCompleteFlags(t *testing.T) {
	cmd := newTodoCompleteCommand()
	if cmd.Use != "complete" {
		t.Fatalf("expected complete command, got %s", cmd.Use)
	}
	for _, flag := range []string{"id", "notes", "gotchas", "workspace"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Fatalf("expected flag --%s", flag)
		}
	}
}

func TestTodoListFlags(t *testing.T) {
	cmd := newTodoListCommand()
	if cmd.Use != "list" {
		t.Fatalf("expected list command, got %s", cmd.Use)
	}
	if cmd.Flags().Lookup("workspace") == nil {
		t.Fatalf("expected --workspace flag")
	}
}
