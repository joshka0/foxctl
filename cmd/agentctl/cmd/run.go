package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/envelope"
	errs "github.com/jkatigb/agentctl/internal/errors"
	memstore "github.com/jkatigb/agentctl/internal/memory"
	"github.com/spf13/cobra"
)

func newRunCommand() *cobra.Command {
	var flags runCommandFlags
	cmd := &cobra.Command{
		Use:   "run <skill-name>",
		Short: "Run a skill and record the result as a job",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeRunCommand(cmd, args, flags)
		},
	}
	bindRunFlags(cmd, &flags)
	return cmd
}

func init() {
	rootCmd.AddCommand(newRunCommand())
}

func annotateRunMeta(result []byte, workspacePath, skillVersion string) []byte {
	var env envelope.Envelope
	if err := json.Unmarshal(result, &env); err != nil {
		return result
	}
	env.Meta.Source = "run"
	if workspacePath != "" {
		env.Meta.Workspace = workspacePath
	}
	if skillVersion != "" {
		env.Meta.SkillVer = skillVersion
	}
	data, err := json.Marshal(env)
	if err != nil {
		return result
	}
	return data
}

// RememberOptions contains parameters for saving execution results to memory.
type RememberOptions struct {
	Name      string
	Type      string
	Summary   string
	Workspace string
	Result    []byte
}

func rememberResult(ctx context.Context, cfg config.Config, opts RememberOptions) error {
	name := strings.TrimSpace(strings.TrimPrefix(opts.Name, "memory:"))
	if name == "" {
		return fmt.Errorf("memory name cannot be empty")
	}
	store, err := memstore.Open(ctx, cfg.Paths.Cache, cfg.Paths.CAS)
	if err != nil {
		return err
	}
	defer func() {
		errs.Ignore(store.Close(), "close memory store after remember")
	}()
	summary := opts.Summary
	if summary == "" {
		summary = summarizeResult(opts.Result)
	}
	_, err = store.SaveResult(ctx, memstore.SaveOptions{
		Name:      name,
		Type:      opts.Type,
		Workspace: opts.Workspace,
		Summary:   summary,
		Result:    opts.Result,
	})
	return err
}
