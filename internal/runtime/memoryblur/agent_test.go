package memoryblur

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextplane"
)

func TestCommandAgentBlurMemoryParsesAgentJSON(t *testing.T) {
	agent, err := NewCommandAgent(CommandAgentOptions{
		Name:       "shell",
		Bin:        "sh",
		Args:       []string{"-c", `printf '%s\n' '{"abstract_schema":"local actors transform bounded signals into coordinated response","mechanism_tags":["bounded_coordination"],"confidence":0.82,"leakage_risk":0.01}'`},
		PromptMode: PromptModeStdin,
		Timeout:    5 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewCommandAgent() error = %v", err)
	}
	output, raw, err := agent.BlurMemory(context.Background(), contextplane.MemoryBlurAgentPromptInput{
		ID:             "test",
		OriginalDomain: "literal domain",
		Summary:        "Literal source",
		Shape: contextplane.MemoryStructuralShape{
			Mechanism: "bounded coordination",
		},
		ForbiddenTerms: []string{"literal domain"},
	})
	if err != nil {
		t.Fatalf("BlurMemory() error = %v raw=%s", err, raw)
	}
	if output.AbstractSchema == "" || output.MechanismTags[0] != "bounded_coordination" {
		t.Fatalf("unexpected output=%#v", output)
	}
}

func TestNewAgentBuildsConfiguredBackends(t *testing.T) {
	tests := []struct {
		name    string
		opts    AgentOptions
		wantErr bool
	}{
		{name: "pi", opts: AgentOptions{Backend: BackendPi}},
		{name: "claude", opts: AgentOptions{Backend: BackendClaude}},
		{name: "hermes", opts: AgentOptions{Backend: BackendHermes}},
		{name: "command missing bin", opts: AgentOptions{Backend: BackendCommand}, wantErr: true},
		{name: "foxctl missing agent", opts: AgentOptions{Backend: BackendFoxctl}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent, err := NewAgent(tt.opts)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got agent=%T", agent)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewAgent() error = %v", err)
			}
			if agent == nil {
				t.Fatal("agent is nil")
			}
		})
	}
}

func TestNewPiAgentDefaultsToSDKRunner(t *testing.T) {
	agent := NewPiAgent(PiAgentOptions{})
	if agent.opts.Name != "pi-sdk" {
		t.Fatalf("agent name=%q want pi-sdk", agent.opts.Name)
	}
	if agent.opts.Bin != "bun" {
		t.Fatalf("agent bin=%q want bun", agent.opts.Bin)
	}
	if len(agent.opts.Args) < 2 || agent.opts.Args[0] != "run" || !strings.Contains(agent.opts.Args[1], "memory-blur-agent.ts") {
		t.Fatalf("unexpected args=%v", agent.opts.Args)
	}
	if _, err := os.Stat(agent.opts.Args[1]); err != nil {
		t.Fatalf("default pi sdk script path is not runnable from package tests: %v", err)
	}
}

func TestExtractFoxctlAgentResponse(t *testing.T) {
	raw := strings.Join([]string{
		`{"version":1,"status":"ok","command":"agent/ask","data":{"ask_id":"a"},"meta":{"ts":"2026-05-21T00:00:00Z"},"error":{}}`,
		`{"version":1,"status":"ok","command":"agent.reply","data":{"ask_id":"a","answer":{"response":"{\"abstract_schema\":\"shape\",\"mechanism_tags\":[\"tag\"],\"confidence\":0.8,\"leakage_risk\":0.1}"}},"meta":{"ts":"2026-05-21T00:00:00Z"},"error":{}}`,
	}, "\n")
	got, err := extractFoxctlAgentResponse(raw)
	if err != nil {
		t.Fatalf("extractFoxctlAgentResponse() error = %v", err)
	}
	if !strings.Contains(got, `"abstract_schema":"shape"`) {
		t.Fatalf("response=%q", got)
	}
}
