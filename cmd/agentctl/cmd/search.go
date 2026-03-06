package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newSearchCommand())
}

func newSearchCommand() *cobra.Command {
	var scopes []string
	var limit int
	var summarize bool
	var workspace string

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Semantic search across code, sessions, memories, and tasks",
		Long: `Unified semantic search using vector embeddings.

Searches across multiple scopes:
  - symbols:  Code symbols (functions, types) from indexed files
  - sessions: Session artifacts (v2 turn artifacts when available; legacy summaries fallback)
  - memories: Named memories and gotchas
  - tasks:    Task descriptions and notes

Examples:
  # Search all scopes
  agentctl search "authentication flow"

  # Search only code symbols
  agentctl search "error handling" --scope symbols

  # Search with summarization (requires OpenRouter API key)
  agentctl search "database migrations" --summarize

  # Limit results per scope
  agentctl search "API endpoints" --limit 5`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			normalizedScopes := normalizeSearchScopes(scopes)

			if isSessionsOnlyScope(normalizedScopes) {
				cfg, err := loadConfig(cmd.Context())
				if err != nil {
					return err
				}
				started := time.Now()
				v2Sessions, err := searchV2Sessions(cmd.Context(), cfg.Storage.Root, query, limit)
				if err != nil {
					return fmt.Errorf("v2 session search: %w", err)
				}
				results := make([]map[string]any, 0, len(v2Sessions))
				for i, item := range v2Sessions {
					results = append(results, map[string]any{
						"source":      "sessions_v2",
						"id":          item.ID,
						"name":        item.ProjectName,
						"path":        "",
						"summary":     item.Summary,
						"rank":        i + 1,
						"final_score": 0.0,
						"metadata": map[string]any{
							"source_provider": item.SourceProvider,
							"v2":              item.V2,
							"started_at":      item.StartedAt,
						},
					})
				}
				stats := map[string]any{
					"total_results": len(results),
					"source_counts": map[string]any{"sessions_v2": len(results)},
					"source_latencies_ms": map[string]any{
						"sessions_v2": time.Since(started).Milliseconds(),
					},
				}
				if len(results) == 0 {
					stats["hint"] = "No v2 session results found; resynthesize sessions or try a broader query."
				}

				resolvedWorkspace := workspace
				if strings.TrimSpace(resolvedWorkspace) == "" {
					if wd, wdErr := os.Getwd(); wdErr == nil {
						resolvedWorkspace = wd
					}
				}

				payload := map[string]any{
					"query":   query,
					"results": buildV2SessionResults(v2Sessions, map[string]struct{}{}),
					"stats":   stats,
				}
				return writeSearchEnvelope(cmd, payload, resolvedWorkspace)
			}

			if scopesIncludeSessions(normalizedScopes) {
				cfg, err := loadConfig(cmd.Context())
				if err != nil {
					return err
				}
				v2StartedAt := time.Now()
				v2Sessions, err := searchV2Sessions(cmd.Context(), cfg.Storage.Root, query, limit)
				if err != nil {
					return fmt.Errorf("v2 session search: %w", err)
				}
				v2LatencyMS := time.Since(v2StartedAt).Milliseconds()

				input := map[string]any{
					"query":     query,
					"limit":     limit,
					"summarize": summarize,
					"scope":     removeSearchScope(normalizedScopes, "sessions"),
				}
				if len(input["scope"].([]string)) == 0 {
					input["scope"] = []string{"symbols", "memories", "tasks"}
				}
				if workspace != "" {
					input["workspace"] = workspace
				}

				baseData, emitted, err := runSemanticSearchData(cmd, input)
				if err != nil {
					return err
				}
				if emitted {
					return nil
				}
				mergeSearchPayloadWithV2(baseData, v2Sessions, v2LatencyMS)

				resolvedWorkspace := workspace
				if strings.TrimSpace(resolvedWorkspace) == "" {
					if wd, wdErr := os.Getwd(); wdErr == nil {
						resolvedWorkspace = wd
					}
				}
				return writeSearchEnvelope(cmd, baseData, resolvedWorkspace)
			}

			// Build input for code/semantic_search skill
			input := map[string]any{
				"query":     query,
				"limit":     limit,
				"summarize": summarize,
			}

			if len(normalizedScopes) > 0 {
				input["scope"] = normalizedScopes
			} else {
				// Default to all scopes
				input["scope"] = []string{"symbols", "sessions", "memories", "tasks"}
			}

			if workspace != "" {
				input["workspace"] = workspace
			}

			inputBytes, err := json.Marshal(input)
			if err != nil {
				return fmt.Errorf("marshal input: %w", err)
			}

			// Use the run command to execute the skill
			runCmd := newRunCommand()
			runCmd.SetContext(cmd.Context())
			runCmd.SetOut(cmd.OutOrStdout())
			runCmd.SetErr(cmd.ErrOrStderr())
			runCmd.SetArgs([]string{"--input", string(inputBytes), "code/semantic_search"})
			return runCmd.Execute()
		},
	}

	cmd.Flags().StringSliceVar(&scopes, "scope", nil, "Scopes to search (symbols, sessions, memories, tasks)")
	cmd.Flags().IntVar(&limit, "limit", 10, "Max results per scope")
	cmd.Flags().BoolVar(&summarize, "summarize", false, "Generate AI summary of results (requires OPENROUTER_API_KEY)")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace path (default: current directory)")

	return cmd
}

func runSemanticSearchData(cmd *cobra.Command, input map[string]any) (map[string]any, bool, error) {
	inputBytes, err := json.Marshal(input)
	if err != nil {
		return nil, false, fmt.Errorf("marshal input: %w", err)
	}

	var out bytes.Buffer
	runCmd := newRunCommand()
	runCmd.SetContext(cmd.Context())
	runCmd.SetOut(&out)
	runCmd.SetErr(cmd.ErrOrStderr())
	runCmd.SetArgs([]string{"--input", string(inputBytes), "code/semantic_search"})
	if err := runCmd.Execute(); err != nil {
		return nil, false, err
	}

	var env map[string]any
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		return nil, false, fmt.Errorf("parse semantic_search envelope: %w", err)
	}
	status, _ := env["status"].(string)
	if strings.TrimSpace(strings.ToLower(status)) != "ok" {
		if _, err := io.Copy(cmd.OutOrStdout(), bytes.NewReader(out.Bytes())); err != nil {
			return nil, false, fmt.Errorf("forward semantic_search envelope: %w", err)
		}
		return nil, true, nil
	}
	data, _ := env["data"].(map[string]any)
	if data == nil {
		data = map[string]any{}
	}
	return data, false, nil
}

func mergeSearchPayloadWithV2(baseData map[string]any, v2Sessions []sessionSummary, v2LatencyMS int64) {
	if baseData == nil {
		return
	}
	results := toObjectSlice(baseData["results"])
	existingIDs := make(map[string]struct{}, len(results))
	for _, result := range results {
		if id := mapString(result, "id"); id != "" {
			existingIDs[id] = struct{}{}
		}
	}
	v2Results := buildV2SessionResults(v2Sessions, existingIDs)
	results = append(results, v2Results...)
	baseData["results"] = results

	stats := toObjectMap(baseData["stats"])
	sourceCounts := toObjectMap(stats["source_counts"])
	sourceCounts["sessions_v2"] = len(v2Results)
	stats["source_counts"] = sourceCounts

	sourceLatencies := toObjectMap(stats["source_latencies_ms"])
	sourceLatencies["sessions_v2"] = v2LatencyMS
	stats["source_latencies_ms"] = sourceLatencies

	stats["total_results"] = len(results)
	if len(results) == 0 {
		stats["hint"] = "No results found; try a different query or broader scope."
	} else {
		delete(stats, "hint")
	}
	baseData["stats"] = stats
}

func buildV2SessionResults(v2Sessions []sessionSummary, existingIDs map[string]struct{}) []map[string]any {
	results := make([]map[string]any, 0, len(v2Sessions))
	for _, item := range v2Sessions {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		if _, dup := existingIDs[id]; dup {
			continue
		}
		existingIDs[id] = struct{}{}
		results = append(results, map[string]any{
			"source":      "sessions_v2",
			"id":          id,
			"name":        item.ProjectName,
			"path":        "",
			"summary":     item.Summary,
			"rank":        len(results) + 1,
			"final_score": 0.0,
			"metadata": map[string]any{
				"source_provider": item.SourceProvider,
				"v2":              item.V2,
				"started_at":      item.StartedAt,
			},
		})
	}
	return results
}

func writeSearchEnvelope(cmd *cobra.Command, payload map[string]any, workspace string) error {
	return protocol.WriteOK(
		cmd.OutOrStdout(),
		"code/semantic_search",
		payload,
		protocol.WithSource("run"),
		protocol.WithWorkspace(workspace),
	)
}

func normalizeSearchScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return nil
	}
	out := make([]string, 0, len(scopes))
	seen := make(map[string]struct{}, len(scopes))
	for _, raw := range scopes {
		scope := strings.ToLower(strings.TrimSpace(raw))
		if scope == "" {
			continue
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		out = append(out, scope)
	}
	return out
}

func isSessionsOnlyScope(scopes []string) bool {
	return len(scopes) == 1 && scopes[0] == "sessions"
}

func scopesIncludeSessions(scopes []string) bool {
	if len(scopes) == 0 {
		return true
	}
	for _, scope := range scopes {
		if scope == "sessions" {
			return true
		}
	}
	return false
}

func removeSearchScope(scopes []string, target string) []string {
	target = strings.TrimSpace(strings.ToLower(target))
	if target == "" {
		return append([]string(nil), scopes...)
	}
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		if strings.TrimSpace(strings.ToLower(scope)) == target {
			continue
		}
		out = append(out, scope)
	}
	return out
}

func toObjectSlice(value any) []map[string]any {
	raw, ok := value.([]any)
	if !ok {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		typed, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, typed)
	}
	return out
}

func toObjectMap(value any) map[string]any {
	typed, ok := value.(map[string]any)
	if !ok || typed == nil {
		return map[string]any{}
	}
	return typed
}
