package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/cmd/agentctl/cmd/memorycmd"
	"github.com/jkatigb/agentctl/internal/openapi/loader"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/cas"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newOpenAPICommand())
}

func newOpenAPICommand() *cobra.Command {
	cmd := &cobra.Command{ //nolint:exhaustruct
		Use:   "openapi",
		Short: "Manage OpenAPI specifications",
	}
	cmd.AddCommand(newOpenAPIImportCommand())
	return cmd
}

func newOpenAPIImportCommand() *cobra.Command {
	var (
		importAs     string
		workspaceArg string
		strict       bool
	)
	cmd := &cobra.Command{ //nolint:exhaustruct
		Use:   "import <path|url|sha256|memory:name>",
		Short: "Validate and store an OpenAPI specification",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			specRef := args[0]
			return memorycmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				ws := resolveWorkspace(cfg, workspaceArg)
				return memorycmd.WithMemoryStore(ctx, cfg, func(mem storage.MemoryStore) error {
					casStore, err := cas.NewStore(cfg.Paths.CAS)
					if err != nil {
						return err
					}
					defer func() {
						errs.Ignore(casStore.Close(), "close openapi import cas store")
					}()

					httpClient := &http.Client{ //nolint:exhaustruct
						Timeout: defaultHTTPTimeout,
					}
					l := loader.New(casStore, mem, loader.WithWorkspace(ws), loader.WithStrictValidation(strict), loader.WithHTTPClient(httpClient))
					spec, err := l.Load(ctx, specRef)
					if err != nil {
						return err
					}

					digest := spec.Digest
					if digest == "" {
						mime := detectSpecMediaType(spec.Raw)
						obj, err := casStore.Put(ctx, bytes.NewReader(spec.Raw), mime, []string{"openapi"})
						if err != nil {
							return fmt.Errorf("store spec in cas: %w", err)
						}
						digest = obj.Digest
					}

					var memoryInfo map[string]any
					if importAs != "" {
						entry, err := saveSpecMemory(ctx, mem, importAs, ws, spec, digest)
						if err != nil {
							return err
						}
						memoryInfo = map[string]any{ //nolint:exhaustruct
							"name":      entry.Name,
							"workspace": entry.Workspace,
							"summary":   entry.Summary,
						}
					}

					operations := summarizeOperations(spec)
					data := map[string]any{ //nolint:exhaustruct
						"digest":            digest,
						"version":           spec.Version,
						"source":            spec.Source,
						"operation_count":   len(spec.Operations),
						"operations":        operations,
						"strict_validation": strict,
					}
					if memoryInfo != nil {
						data["memory"] = memoryInfo
					}

					return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.openapi.import", data,
						protocol.WithSource("run"),
						protocol.WithWorkspace(ws),
					)
				})
			})
		},
	}
	cmd.Flags().StringVar(&importAs, "as", "", "Store spec as named memory")
	cmd.Flags().StringVar(&workspaceArg, "workspace", "", "Workspace override for named memory lookup")
	cmd.Flags().BoolVar(&strict, "strict", false, "Require strict validation (no schema skips)")
	return cmd
}

func saveSpecMemory(ctx context.Context, store storage.MemoryStore, name, ws string, spec *loader.Spec, digest string) (storage.NamedEntry, error) {
	summary := buildSpecSummary(spec)
	payload := protocol.OK("agentctl.openapi.import", map[string]any{ //nolint:exhaustruct
		"digest":          digest,
		"version":         spec.Version,
		"operation_count": len(spec.Operations),
		"source":          spec.Source,
	}, protocol.WithSource("run"), protocol.WithWorkspace(ws))
	encoded, err := json.Marshal(payload)
	if err != nil {
		return storage.NamedEntry{}, fmt.Errorf("encode memory payload: %w", err)
	}
	entry := storage.NamedEntry{ //nolint:exhaustruct
		Name:      name,
		Type:      "openapi_spec",
		Workspace: workspace.Normalize(ws),
		Summary:   summary,
		Result:    encoded,
		Digests:   []string{digest},
	}
	saved, err := store.Save(ctx, entry)
	if err != nil {
		return storage.NamedEntry{}, fmt.Errorf("save memory: %w", err)
	}
	return saved, nil
}

func buildSpecSummary(spec *loader.Spec) string {
	if spec == nil {
		return "OpenAPI"
	}
	title := ""
	infoVersion := ""
	if spec.Doc != nil && spec.Doc.Info != nil {
		title = strings.TrimSpace(spec.Doc.Info.Title)
		infoVersion = strings.TrimSpace(spec.Doc.Info.Version)
	}
	version := strings.TrimSpace(spec.Version)
	if infoVersion != "" {
		version = infoVersion
	}
	switch {
	case title != "" && version != "":
		return fmt.Sprintf("%s %s", title, version)
	case title != "":
		return title
	case version != "":
		return fmt.Sprintf("OpenAPI %s", version)
	default:
		return fmt.Sprintf("OpenAPI %s", spec.Version)
	}
}

func summarizeOperations(spec *loader.Spec) []map[string]string {
	ids := make([]string, 0, len(spec.Operations))
	for id := range spec.Operations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	summaries := make([]map[string]string, 0, len(ids))
	for _, id := range ids {
		op := spec.Operations[id]
		summaries = append(summaries, map[string]string{ //nolint:exhaustruct
			"id":      op.ID,
			"method":  op.Method,
			"path":    op.Path,
			"summary": strings.TrimSpace(op.Summary),
		})
	}
	return summaries
}

func detectSpecMediaType(data []byte) string {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return "application/octet-stream"
	}
	if trimmed[0] == '{' {
		return "application/openapi+json"
	}
	return "application/openapi+yaml"
}

const defaultHTTPTimeout = 30 * time.Second
