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
	expected := []string{"add", "complete", "list", "active", "insights", "recommend", "plan", "search <query>"}
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
