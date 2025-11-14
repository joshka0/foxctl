package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestCompletionCommandMetadata(t *testing.T) {
	cmd := newCompletionCommand()

	if cmd.Use != "completion [bash|zsh|fish|powershell]" {
		t.Fatalf("unexpected use: %s", cmd.Use)
	}

	expectedArgs := []string{"bash", "zsh", "fish", "powershell"}
	if len(cmd.ValidArgs) != len(expectedArgs) {
		t.Fatalf("expected %d valid args, got %d", len(expectedArgs), len(cmd.ValidArgs))
	}
	for i, arg := range expectedArgs {
		if cmd.ValidArgs[i] != arg {
			t.Fatalf("expected arg %s at index %d, got %s", arg, i, cmd.ValidArgs[i])
		}
	}
}

func runCompletion(t *testing.T, shell string) string {
	t.Helper()
	buf := &bytes.Buffer{}
	cmd := newCompletionCommand()
	cmd.SetOut(buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{shell})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("completion %s failed: %v", shell, err)
	}
	return buf.String()
}

func TestCompletionBash(t *testing.T) {
	out := runCompletion(t, "bash")
	if out == "" {
		t.Fatal("expected bash completion output")
	}
	if !strings.Contains(out, "bash") {
		t.Fatal("bash completion output missing bash markers")
	}
}

func TestCompletionZsh(t *testing.T) {
	if out := runCompletion(t, "zsh"); out == "" {
		t.Fatal("expected zsh completion output")
	}
}

func TestCompletionFish(t *testing.T) {
	if out := runCompletion(t, "fish"); out == "" {
		t.Fatal("expected fish completion output")
	}
}

func TestCompletionPowershell(t *testing.T) {
	if out := runCompletion(t, "powershell"); out == "" {
		t.Fatal("expected powershell completion output")
	}
}

func TestCompletionInvalidArgs(t *testing.T) {
	cases := [][]string{{"invalid"}, {}, {"bash", "zsh"}}
	for _, args := range cases {
		t.Run(strings.Join(args, ","), func(t *testing.T) {
			cmd := newCompletionCommand()
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(args)
			if err := cmd.Execute(); err == nil {
				t.Fatalf("expected error for args %v", args)
			}
		})
	}
}
