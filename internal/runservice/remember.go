package runservice

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	memstore "github.com/jkatigb/agentctl/internal/storage/memory"
)

func (e *Executor) remember(result []byte) error {
	if strings.TrimSpace(e.options.RememberName) == "" {
		return nil
	}
	return rememberResult(e.ctx, e.cfg, RememberOptions{
		Name:      e.options.RememberName,
		Type:      e.options.RememberType,
		Summary:   e.options.RememberSummary,
		Workspace: e.options.Workspace,
		Result:    result,
	})
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

func summarizeResult(result []byte) string {
	var env envelope.Envelope
	if err := json.Unmarshal(result, &env); err == nil {
		if env.Meta.Workspace != "" {
			return fmt.Sprintf("%s (%s)", env.Command, filepath.Base(env.Meta.Workspace))
		}
		return env.Command
	}
	return ""
}
