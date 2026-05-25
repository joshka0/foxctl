package cmd

import (
	"bytes"
	"context"
	"testing"

	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/spf13/cobra"
)

func TestRunQuotasSetRejectsNegativeLimitsAsArgError(t *testing.T) {
	oldMaxJobs := quotasSetMaxJobs
	oldCPU := quotasSetCPU
	oldMemMB := quotasSetMemMB
	oldLLMPerMin := quotasSetLLMPerMin
	oldEgressPerMin := quotasSetEgressPerMin
	t.Cleanup(func() {
		quotasSetMaxJobs = oldMaxJobs
		quotasSetCPU = oldCPU
		quotasSetMemMB = oldMemMB
		quotasSetLLMPerMin = oldLLMPerMin
		quotasSetEgressPerMin = oldEgressPerMin
	})

	tests := []struct {
		name string
		set  func()
	}{
		{name: "max jobs", set: func() { quotasSetMaxJobs = -1 }},
		{name: "cpu", set: func() { quotasSetCPU = -1 }},
		{name: "memory", set: func() { quotasSetMemMB = -1 }},
		{name: "llm calls", set: func() { quotasSetLLMPerMin = -1 }},
		{name: "egress", set: func() { quotasSetEgressPerMin = -1 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			quotasSetMaxJobs = 0
			quotasSetCPU = 0
			quotasSetMemMB = 0
			quotasSetLLMPerMin = 0
			quotasSetEgressPerMin = 0
			tt.set()

			cmd := &cobra.Command{}
			out := &bytes.Buffer{}
			cmd.SetOut(out)
			cmd.SetContext(config.WithContext(context.Background(), config.Config{
				Storage: config.StorageSettings{Root: t.TempDir()},
			}))

			err := runQuotasSet(cmd, []string{"org/team/project"})
			if err == nil {
				t.Fatal("expected invalid quota limits to fail")
			}

			env := decodeEnvelope(t, out.Bytes())
			if status := mustString(t, env["status"]); status != "error" {
				t.Fatalf("status = %q, want error", status)
			}
			errMap := mustMap(t, env["error"])
			if code := mustString(t, errMap["code"]); code != string(protocol.ErrorCodeEARG) {
				t.Fatalf("error.code = %s, want %s", code, protocol.ErrorCodeEARG)
			}
		})
	}
}
