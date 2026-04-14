package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestContextRecordCommandFlags(t *testing.T) {
	tests := []struct {
		name  string
		cmd   func() *cobra.Command
		flags []string
	}{
		{
			name: "capture",
			cmd:  newCaptureCommand,
			flags: []string{
				"workspace", "task-id", "phase", "outcome", "summary",
				"evidence-ref", "file-touched", "observation", "tension", "next-action", "promotion-candidate", "dry-run",
			},
		},
		{
			name:  "observe",
			cmd:   newObserveCommand,
			flags: []string{"workspace", "statement", "confidence", "count", "project", "area", "evidence-ref", "dry-run"},
		},
		{
			name:  "tension",
			cmd:   newTensionCommand,
			flags: []string{"workspace", "kind", "statement", "impact", "status", "related-ref", "dry-run"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.cmd()
			for _, flag := range tt.flags {
				if cmd.Flags().Lookup(flag) == nil {
					t.Fatalf("expected --%s flag", flag)
				}
			}
		})
	}
}
