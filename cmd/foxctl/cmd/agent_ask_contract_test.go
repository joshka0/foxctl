package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/agents"
	"github.com/spf13/cobra"
)

func TestAgentAskDispatcherRoutingContract(t *testing.T) {
	cfg := newSkillTestConfig(t)
	seedAgentForAskContract(t, cfg, "agent-ask-contract-1", "agent:ask:contract:1")

	t.Run("mailbox default", func(t *testing.T) {
		t.Setenv(envAskDispatcherMode, "")
		env := runAskDryRunForContract(t, cfg, []string{
			"--question", "status?",
			"--dry-run",
			"agent-ask-contract-1",
		})
		data, _ := env.Data.(map[string]any)
		if got := data["dispatcher"]; got != askDispatchModeMailbox {
			t.Fatalf("dispatcher=%v want %s", got, askDispatchModeMailbox)
		}
	})

	t.Run("override path", func(t *testing.T) {
		t.Setenv(envAskDispatcherMode, "")
		env := runAskDryRunForContract(t, cfg, []string{
			"--question", "status?",
			"--dispatcher", askDispatchModeJido,
			"--dry-run",
			"agent-ask-contract-1",
		})
		data, _ := env.Data.(map[string]any)
		if got := data["dispatcher"]; got != askDispatchModeJido {
			t.Fatalf("dispatcher=%v want %s", got, askDispatchModeJido)
		}
	})

	t.Run("timeout parse behavior at command boundary", func(t *testing.T) {
		cmd := newAgentAskContractCommand()
		cmd.SetContext(config.WithContext(context.Background(), cfg))
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{
			"--question", "status?",
			"--timeout", "not-a-duration",
			"--dry-run",
			"agent-ask-contract-1",
		})

		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected timeout parse error")
		}
		if !strings.Contains(err.Error(), "invalid argument") || !strings.Contains(err.Error(), "timeout") {
			t.Fatalf("unexpected parse error: %v", err)
		}
	})
}

func seedAgentForAskContract(t *testing.T, cfg config.Config, id, namespace string) {
	t.Helper()
	store, err := agents.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open agent store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Create(context.Background(), agent.Agent{
		ID:          id,
		Namespace:   namespace,
		Name:        "Ask Contract Agent",
		Role:        "worker",
		SkillsAllow: []string{},
		Policy:      agent.Policy{},
		ShareBB:     "scoped",
		State:       agent.StateRunning,
		CreatedAt:   time.Date(2026, time.March, 6, 18, 0, 0, 0, time.UTC),
		ExecMode:    agent.ModeReactive,
	}); err != nil {
		t.Fatalf("create agent: %v", err)
	}
}

func runAskDryRunForContract(t *testing.T, cfg config.Config, args []string) envelope.Envelope {
	t.Helper()
	cmd := newAgentAskContractCommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("agent ask command failed: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	return env
}

func newAgentAskContractCommand() *cobra.Command {
	askDryRun = false
	askDispatcherMode = ""

	cmd := &cobra.Command{
		Use:  "ask <agent-id>",
		Args: cobra.ExactArgs(1),
		RunE: runAgentAsk,
	}
	cmd.Flags().String("question", "", "The question to ask (required)")
	cmd.Flags().String("kind", "context", "Ask kind: context|secret|approval|toolhint|other")
	cmd.Flags().String("conversation-id", "", "Conversation ID for memory continuity (default: unique per call)")
	cmd.Flags().Bool("wait", false, "Wait for reply before returning")
	cmd.Flags().Duration("timeout", 5*time.Minute, "Timeout for --wait")
	cmd.Flags().BoolVar(&askDryRun, "dry-run", false, "Preview what would be sent without sending the message")
	cmd.Flags().StringVar(&askDispatcherMode, "dispatcher", "", "Ask dispatcher backend: mailbox|jido (default from FOXCTL_V2_ASK_DISPATCHER)")
	_ = cmd.MarkFlagRequired("question")
	return cmd
}
