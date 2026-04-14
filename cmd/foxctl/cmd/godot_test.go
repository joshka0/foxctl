package cmd

import (
	"testing"
)

func TestGodotCommandStructure(t *testing.T) {
	cmd := newGodotCommand()

	if cmd.Use != "godot" {
		t.Errorf("expected Use=godot, got %s", cmd.Use)
	}

	// Check subcommands exist
	subcommands := map[string]bool{
		"ping":           false,
		"scene-tree":     false,
		"inspect":        false,
		"create":         false,
		"set":            false,
		"attach-script":  false,
		"connect-signal": false,
		"run":            false,
		"errors":         false,
	}

	for _, sub := range cmd.Commands() {
		if _, ok := subcommands[sub.Use]; ok {
			subcommands[sub.Use] = true
		} else {
			// Check for commands with args like "inspect <node-path>"
			for name := range subcommands {
				if len(sub.Use) >= len(name) && sub.Use[:len(name)] == name {
					subcommands[name] = true
					break
				}
			}
		}
	}

	for name, found := range subcommands {
		if !found {
			t.Errorf("missing subcommand: %s", name)
		}
	}
}

func TestGodotPingFlags(t *testing.T) {
	cmd := newGodotPingCommand()

	// Check common flags exist
	flags := []string{"host", "port", "timeout", "data-only", "skip-cache"}
	for _, name := range flags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing flag: %s", name)
		}
	}

	// Check defaults
	hostFlag := cmd.Flags().Lookup("host")
	if hostFlag.DefValue != "127.0.0.1" {
		t.Errorf("expected host default=127.0.0.1, got %s", hostFlag.DefValue)
	}

	portFlag := cmd.Flags().Lookup("port")
	if portFlag.DefValue != "7777" {
		t.Errorf("expected port default=7777, got %s", portFlag.DefValue)
	}
}

func TestGodotSceneTreeFlags(t *testing.T) {
	cmd := newGodotSceneTreeCommand()

	// Check scene-tree specific flags
	if cmd.Flags().Lookup("max-depth") == nil {
		t.Error("missing flag: max-depth")
	}
	if cmd.Flags().Lookup("max-nodes") == nil {
		t.Error("missing flag: max-nodes")
	}

	// Check defaults
	maxDepthFlag := cmd.Flags().Lookup("max-depth")
	if maxDepthFlag.DefValue != "10" {
		t.Errorf("expected max-depth default=10, got %s", maxDepthFlag.DefValue)
	}
}

func TestGodotInspectArgs(t *testing.T) {
	cmd := newGodotInspectCommand()

	// Should require exactly 1 arg
	if cmd.Args == nil {
		t.Error("expected Args validator")
	}
}

func TestGodotCreateArgs(t *testing.T) {
	cmd := newGodotCreateCommand()

	// Should require exactly 3 args
	if cmd.Args == nil {
		t.Error("expected Args validator")
	}
}

func TestGodotSetArgs(t *testing.T) {
	cmd := newGodotSetCommand()

	// Should require exactly 3 args
	if cmd.Args == nil {
		t.Error("expected Args validator")
	}
}

func TestGodotConnectSignalArgs(t *testing.T) {
	cmd := newGodotConnectSignalCommand()

	// Should require exactly 4 args
	if cmd.Args == nil {
		t.Error("expected Args validator")
	}
}

func TestGodotErrorsFlags(t *testing.T) {
	cmd := newGodotErrorsCommand()

	if cmd.Flags().Lookup("limit") == nil {
		t.Error("missing flag: limit")
	}

	limitFlag := cmd.Flags().Lookup("limit")
	if limitFlag.DefValue != "50" {
		t.Errorf("expected limit default=50, got %s", limitFlag.DefValue)
	}
}
