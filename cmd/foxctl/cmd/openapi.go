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

	"github.com/joshka0/foxctl/cmd/foxctl/cmd/memorycmd"
	"github.com/joshka0/foxctl/internal/interfaces/openapi/loader"
	"github.com/joshka0/foxctl/internal/platform/config"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/cas"
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
	cmd.AddCommand(newOpenAPIDescribeCommand())
	cmd.AddCommand(newOpenAPIValidateCommand())
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
				workspaceID := resolveWorkspaceID(cfg, workspaceArg)
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
					l := loader.New(casStore, mem, loader.WithWorkspace(workspaceID), loader.WithStrictValidation(strict), loader.WithHTTPClient(httpClient))
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
						entry, err := saveSpecMemory(ctx, mem, importAs, workspaceID, spec, digest)
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

					return protocol.WriteOK(
						cmd.OutOrStdout(), "foxctl.openapi.import", data,
						protocol.WithSource("run"),
						protocol.WithWorkspace(workspaceID),
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
	payload := protocol.OK("foxctl.openapi.import", map[string]any{ //nolint:exhaustruct
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
		Workspace: ws,
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

func newOpenAPIDescribeCommand() *cobra.Command {
	var (
		workspaceArg string
		tag          string
	)
	cmd := &cobra.Command{ //nolint:exhaustruct
		Use:   "describe <path|url|sha256|memory:name>",
		Short: "List all operations in an OpenAPI specification",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			specRef := args[0]
			return memorycmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				workspaceID := resolveWorkspaceID(cfg, workspaceArg)
				return memorycmd.WithMemoryStore(ctx, cfg, func(mem storage.MemoryStore) error {
					casStore, err := cas.NewStore(cfg.Paths.CAS)
					if err != nil {
						return err
					}
					defer func() { errs.Ignore(casStore.Close(), "close openapi describe cas store") }()

					httpClient := &http.Client{Timeout: defaultHTTPTimeout}
					l := loader.New(casStore, mem, loader.WithWorkspace(workspaceID), loader.WithHTTPClient(httpClient))
					spec, err := l.Load(ctx, specRef)
					if err != nil {
						return err
					}

					title := "OpenAPI Specification"
					version := spec.Version
					if spec.Doc != nil && spec.Doc.Info != nil {
						if spec.Doc.Info.Title != "" {
							title = spec.Doc.Info.Title
						}
						if spec.Doc.Info.Version != "" {
							version = spec.Doc.Info.Version
						}
					}

					// Filter operations by tag if specified
					operations := make([]map[string]any, 0)
					for _, op := range spec.Operations {
						if tag != "" {
							hasTag := false
							for _, t := range op.Tags {
								if t == tag {
									hasTag = true
									break
								}
							}
							if !hasTag {
								continue
							}
						}
						operations = append(operations, map[string]any{
							"id":      op.ID,
							"method":  strings.ToUpper(op.Method),
							"path":    op.Path,
							"summary": op.Summary,
							"tags":    op.Tags,
						})
					}

					// Sort by operationId
					sort.Slice(operations, func(i, j int) bool {
						return operations[i]["id"].(string) < operations[j]["id"].(string)
					})

					data := map[string]any{
						"title":           title,
						"version":         version,
						"openapi_version": spec.Version,
						"source":          spec.Source,
						"operation_count": len(operations),
						"operations":      operations,
					}

					return protocol.WriteOK(
						cmd.OutOrStdout(), "foxctl.openapi.describe", data,
						protocol.WithSource("run"),
						protocol.WithWorkspace(workspaceID),
					)
				})
			})
		},
	}
	cmd.Flags().StringVar(&workspaceArg, "workspace", "", "Workspace override for named memory lookup")
	cmd.Flags().StringVar(&tag, "tag", "", "Filter operations by tag")
	return cmd
}

func newOpenAPIValidateCommand() *cobra.Command {
	var (
		workspaceArg string
		strict       bool
	)
	cmd := &cobra.Command{ //nolint:exhaustruct
		Use:   "validate <path|url|sha256|memory:name>",
		Short: "Validate an OpenAPI specification",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			specRef := args[0]
			return memorycmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				workspaceID := resolveWorkspaceID(cfg, workspaceArg)
				return memorycmd.WithMemoryStore(ctx, cfg, func(mem storage.MemoryStore) error {
					casStore, err := cas.NewStore(cfg.Paths.CAS)
					if err != nil {
						return err
					}
					defer func() { errs.Ignore(casStore.Close(), "close openapi validate cas store") }()

					httpClient := &http.Client{Timeout: defaultHTTPTimeout}
					l := loader.New(casStore, mem, loader.WithWorkspace(workspaceID), loader.WithStrictValidation(strict), loader.WithHTTPClient(httpClient))
					spec, err := l.Load(ctx, specRef)
					if err != nil {
						return fmt.Errorf("validation failed: %w", err)
					}

					// Check for common issues
					warnings := make([]string, 0)
					errors := make([]string, 0)

					// Check for operations without operationId
					for path, pathItem := range spec.Doc.Paths.Map() {
						for method, op := range pathItem.Operations() {
							if op == nil {
								continue
							}
							if op.OperationID == "" {
								warnings = append(warnings, fmt.Sprintf("%s %s: missing operationId", strings.ToUpper(method), path))
							}
						}
					}

					// Check for duplicate operationIds
					seen := make(map[string]bool)
					for path, pathItem := range spec.Doc.Paths.Map() {
						for method, op := range pathItem.Operations() {
							if op == nil || op.OperationID == "" {
								continue
							}
							if seen[op.OperationID] {
								errors = append(errors, fmt.Sprintf("duplicate operationId: %s (found in %s %s)", op.OperationID, strings.ToUpper(method), path))
							}
							seen[op.OperationID] = true
						}
					}

					valid := len(errors) == 0
					data := map[string]any{
						"valid":   valid,
						"source":  spec.Source,
						"version": spec.Version,
						"strict":  strict,
					}

					if len(warnings) > 0 {
						data["warnings"] = warnings
						data["warning_count"] = len(warnings)
					}

					if len(errors) > 0 {
						data["errors"] = errors
						data["error_count"] = len(errors)
					}

					return protocol.WriteOK(
						cmd.OutOrStdout(), "foxctl.openapi.validate", data,
						protocol.WithSource("run"),
						protocol.WithWorkspace(workspaceID),
					)
				})
			})
		},
	}
	cmd.Flags().StringVar(&workspaceArg, "workspace", "", "Workspace override for named memory lookup")
	cmd.Flags().BoolVar(&strict, "strict", false, "Require strict validation (no schema skips)")
	return cmd
}

const defaultHTTPTimeout = 30 * time.Second
