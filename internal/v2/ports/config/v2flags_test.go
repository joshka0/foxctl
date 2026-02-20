package config

import (
	"slices"
	"testing"
)

func TestParseV2Commands_DefaultOff(t *testing.T) {
	t.Parallel()

	flags, err := ParseV2Commands("")
	if err != nil {
		t.Fatalf("ParseV2Commands returned error: %v", err)
	}
	if !flags.Empty() {
		t.Fatalf("expected no enabled commands, got %v", flags.Commands())
	}
}

func TestParseV2Commands_ExplicitNone(t *testing.T) {
	t.Parallel()

	flags, err := ParseV2Commands("none")
	if err != nil {
		t.Fatalf("ParseV2Commands returned error: %v", err)
	}
	if !flags.Empty() {
		t.Fatalf("expected no enabled commands for none, got %v", flags.Commands())
	}
}

func TestParseV2Commands_CommandSetNormalization(t *testing.T) {
	t.Parallel()

	flags, err := ParseV2Commands(" spawn,ASK, run, spawn , list ")
	if err != nil {
		t.Fatalf("ParseV2Commands returned error: %v", err)
	}

	got := flags.Commands()
	want := []string{"ask", "list", "run", "spawn"}
	if !slices.Equal(got, want) {
		t.Fatalf("commands mismatch: got %v, want %v", got, want)
	}

	if !flags.Enabled("ASK") {
		t.Fatal("expected ASK to be enabled after normalization")
	}
}

func TestParseV2Commands_RejectsUnknownCommand(t *testing.T) {
	t.Parallel()

	if _, err := ParseV2Commands("spawn,unknown"); err == nil {
		t.Fatal("expected unknown command error, got nil")
	}
}

func TestParseV2CommandsFromEnv(t *testing.T) {
	t.Setenv(envV2Commands, "spawn,kill")

	flags, err := ParseV2CommandsFromEnv()
	if err != nil {
		t.Fatalf("ParseV2CommandsFromEnv returned error: %v", err)
	}
	if !flags.Enabled("spawn") || !flags.Enabled("kill") {
		t.Fatalf("expected spawn and kill enabled, got %v", flags.Commands())
	}
}

func TestParseV2CommandsFromEnv_DefaultsToAllWhenUnset(t *testing.T) {
	t.Setenv(envV2Commands, "")

	flags, err := ParseV2CommandsFromEnv()
	if err != nil {
		t.Fatalf("ParseV2CommandsFromEnv returned error: %v", err)
	}
	want := SupportedCommands()
	got := flags.Commands()
	if !slices.Equal(got, want) {
		t.Fatalf("default commands mismatch: got %v, want %v", got, want)
	}
}

func TestParseV2CommandsFromEnv_NoneDisablesAll(t *testing.T) {
	t.Setenv(envV2Commands, "none")

	flags, err := ParseV2CommandsFromEnv()
	if err != nil {
		t.Fatalf("ParseV2CommandsFromEnv returned error: %v", err)
	}
	if !flags.Empty() {
		t.Fatalf("expected no enabled commands for env=none, got %v", flags.Commands())
	}
}

func TestParseV2ShadowCommandsFromEnv(t *testing.T) {
	t.Setenv(envV2ShadowCommands, "ask")

	flags, err := ParseV2ShadowCommandsFromEnv()
	if err != nil {
		t.Fatalf("ParseV2ShadowCommandsFromEnv returned error: %v", err)
	}
	if !flags.Enabled("ask") {
		t.Fatalf("expected ask enabled for shadow flags, got %v", flags.Commands())
	}
}

func TestParseV2FreezeCommandsFromEnv(t *testing.T) {
	t.Setenv(envV2FreezeCommands, "spawn,kill")

	flags, err := ParseV2FreezeCommandsFromEnv()
	if err != nil {
		t.Fatalf("ParseV2FreezeCommandsFromEnv returned error: %v", err)
	}
	if !flags.Enabled("spawn") || !flags.Enabled("kill") {
		t.Fatalf("expected spawn/kill enabled for freeze flags, got %v", flags.Commands())
	}
}

func TestSanitizeShadowFlags_DefaultBlocksMutating(t *testing.T) {
	t.Parallel()

	flags, err := ParseV2ShadowCommands("spawn,run,kill,ask,list")
	if err != nil {
		t.Fatalf("ParseV2ShadowCommands returned error: %v", err)
	}

	sanitized := SanitizeShadowFlags(flags, false)
	got := sanitized.Commands()
	want := []string{"ask", "list"}
	if !slices.Equal(got, want) {
		t.Fatalf("shadow commands mismatch: got %v, want %v", got, want)
	}
}

func TestShadowMutatingEnabledFromEnv(t *testing.T) {
	t.Setenv(envV2ShadowMutating, "true")
	if !ShadowMutatingEnabledFromEnv() {
		t.Fatal("expected mutating shadow enabled for env=true")
	}

	t.Setenv(envV2ShadowMutating, "0")
	if ShadowMutatingEnabledFromEnv() {
		t.Fatal("expected mutating shadow disabled for env=0")
	}
}

func TestSupportedCommands_CanonicalSet(t *testing.T) {
	t.Parallel()

	got := SupportedCommands()
	want := []string{"ask", "kill", "list", "run", "spawn"}
	if !slices.Equal(got, want) {
		t.Fatalf("supported commands mismatch: got %v, want %v", got, want)
	}
}
