package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/executil"
	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/evals/retrievaleval"
	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/storage/obsidianindex"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
	"github.com/spf13/cobra"
)

func newEvalCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Run lightweight evaluation suites",
	}
	cmd.AddCommand(newEvalRetrievalCommand())
	return cmd
}

func newEvalRetrievalCommand() *cobra.Command {
	var suiteRef string
	var workspacePath string
	var vaultPath string
	var limit int
	var format string
	var modes []string
	var rebuildIndex bool
	var canonicalOnly bool
	var save bool
	var saveDir string

	cmd := &cobra.Command{
		Use:   "retrieval",
		Short: "Evaluate lexical, semantic, and blended retrieval against a curated query suite",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(suiteRef) == "" {
				return fmt.Errorf("--suite is required")
			}
			if strings.TrimSpace(vaultPath) == "" {
				return fmt.Errorf("--vault-path is required")
			}
			target := resolveContextWorkspace(workspacePath)
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			suitePath, err := resolveEvalSuitePath(suiteRef)
			if err != nil {
				return err
			}
			suite, err := retrievaleval.LoadSuite(suitePath)
			if err != nil {
				return err
			}
			index, err := obsidianindex.Open(ctx, cfg.Storage.Root, vaultPath)
			if err != nil {
				return err
			}
			defer func() { _ = index.Close() }()
			if rebuildIndex {
				if _, err := index.Rebuild(ctx, vaultPath); err != nil {
					return err
				}
			}
			repo, err := repoindex.Open(ctx, cfg.Storage.Root, target)
			if err != nil {
				return err
			}
			defer func() { _ = repo.Close() }()
			semanticProvider := openObsidianSemanticProvider(cfg)
			workspaceStore := contextplane.NewWorkspaceStore(target)
			if err := ensureTopOfMindForEval(ctx, cfg.Storage.Root, target, workspaceStore); err != nil {
				return err
			}
			selectedModes := normalizeEvalModes(modes)
			results := make([]retrievaleval.QueryResult, 0, len(suite.Queries))
			for _, q := range suite.Queries {
				qr := retrievaleval.QueryResult{
					ID:    q.ID,
					Query: q.Query,
					Notes: q.Notes,
					Modes: map[string]retrievaleval.ModeResult{},
				}
				if hasMode(selectedModes, "baseline") {
					hits, err := index.SearchNotes(ctx, q.Query, limit)
					paths := extractSearchHitPaths(hits)
					paths = filterEvalPaths(paths, true)
					qr.Modes["baseline"] = retrievaleval.EvaluateMode("baseline", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "lexical") {
					hits, err := index.SearchNotes(ctx, q.Query, limit)
					paths := filterEvalPaths(extractSearchHitPaths(hits), canonicalOnly)
					qr.Modes["lexical"] = retrievaleval.EvaluateMode("lexical", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "semantic") {
					if semanticProvider == nil {
						qr.Modes["semantic"] = retrievaleval.EvaluateMode("semantic", nil, q.ExpectedAnyOf, 0, fmt.Errorf("semantic provider unavailable"))
					} else {
						hits, err := index.SearchNotesSemantic(ctx, q.Query, semanticProvider, limit)
						paths := filterEvalPaths(extractSearchHitPaths(hits), canonicalOnly)
						qr.Modes["semantic"] = retrievaleval.EvaluateMode("semantic", paths, q.ExpectedAnyOf, len(paths), err)
					}
				}
				if hasMode(selectedModes, "blended") {
					result, err := workspaceStore.Retrieve(ctx, index, repo, semanticProvider, q.Query, limit)
					paths := filterEvalPaths(extractRetrievalHitPaths(result.VaultHits), canonicalOnly)
					qr.Modes["blended"] = retrievaleval.EvaluateMode("blended", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "skill_default") {
					paths, err := runSemanticSearchEvalMode(ctx, target, vaultPath, q.Query, limit, []string{"symbols", "sessions", "memories", "tasks", "codemaps"})
					qr.Modes["skill_default"] = retrievaleval.EvaluateMode("skill_default", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "skill_context") {
					paths, err := runSemanticSearchEvalMode(ctx, target, vaultPath, q.Query, limit, []string{"context"})
					qr.Modes["skill_context"] = retrievaleval.EvaluateMode("skill_context", paths, q.ExpectedAnyOf, len(paths), err)
				}
				if hasMode(selectedModes, "skill_default_plus_context") {
					paths, err := runSemanticSearchEvalMode(ctx, target, vaultPath, q.Query, limit, []string{"symbols", "sessions", "memories", "tasks", "codemaps", "context"})
					qr.Modes["skill_default_plus_context"] = retrievaleval.EvaluateMode("skill_default_plus_context", paths, q.ExpectedAnyOf, len(paths), err)
				}
				results = append(results, qr)
			}
			runResult := retrievaleval.RunResult{
				Suite:       suite.Name,
				Workspace:   target,
				VaultPath:   vaultPath,
				Limit:       limit,
				Queries:     results,
				Summaries:   retrievaleval.Summarize(results, selectedModes),
				GeneratedAt: time.Now().UTC(),
			}
			data := map[string]any{
				"suite_path": suitePath,
				"result":     runResult,
			}
			markdown := retrievaleval.RenderMarkdown(runResult)
			if strings.EqualFold(format, "markdown") {
				data["markdown"] = markdown
			}
			if save {
				jsonPath, markdownPath, err := saveEvalOutputs(target, saveDir, suite.Name, runResult, markdown)
				if err != nil {
					return err
				}
				data["saved"] = map[string]any{
					"json_path":     jsonPath,
					"markdown_path": markdownPath,
				}
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("eval/retrieval", data, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&suiteRef, "suite", "", "Suite name (agentctl, praze) or explicit YAML path")
	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path")
	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum hits per retrieval mode")
	cmd.Flags().StringVar(&format, "format", "markdown", "Output companion format: markdown or json")
	cmd.Flags().StringSliceVar(&modes, "mode", []string{"baseline", "lexical", "semantic", "blended"}, "Retrieval modes to evaluate (also available: skill_default, skill_context, skill_default_plus_context)")
	cmd.Flags().BoolVar(&rebuildIndex, "rebuild-index", true, "Rebuild the vault index before evaluation")
	cmd.Flags().BoolVar(&canonicalOnly, "canonical-only", true, "Exclude inbox draft hits from evaluation paths")
	cmd.Flags().BoolVar(&save, "save", false, "Save JSON and Markdown eval outputs under the workspace exports directory")
	cmd.Flags().StringVar(&saveDir, "save-dir", "", "Override save directory (default: <workspace>/.agentctl/exports/evals)")
	return cmd
}

func resolveEvalSuitePath(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("suite reference required")
	}
	baseDir := filepath.Join("testdata", "evals", "retrieval")
	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return "", err
	}
	if strings.Contains(ref, "/") || strings.HasSuffix(ref, ".yaml") || strings.HasSuffix(ref, ".yml") {
		candidateAbs, err := filepath.Abs(filepath.Clean(ref))
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(baseAbs, candidateAbs)
		if err != nil {
			return "", err
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("suite path must stay under %s", baseDir)
		}
		return candidateAbs, nil
	}
	return filepath.Join(baseDir, ref+".yaml"), nil
}

func normalizeEvalModes(modes []string) []string {
	if len(modes) == 0 {
		return []string{"baseline", "lexical", "semantic", "blended"}
	}
	out := make([]string, 0, len(modes))
	seen := map[string]struct{}{}
	for _, mode := range modes {
		mode = strings.ToLower(strings.TrimSpace(mode))
		if mode == "" {
			continue
		}
		if _, ok := seen[mode]; ok {
			continue
		}
		seen[mode] = struct{}{}
		out = append(out, mode)
	}
	return out
}

type semanticSearchEvalOutput struct {
	Results []struct {
		Path string `json:"path"`
	} `json:"results"`
}

func runSemanticSearchEvalMode(ctx context.Context, workspacePath, vaultPath, query string, limit int, scope []string) ([]string, error) {
	input := map[string]any{
		"query":     query,
		"scope":     scope,
		"workspace": resolveContextWorkspace(workspacePath),
		"limit":     limit,
	}
	if slicesContain(scope, "context") && strings.TrimSpace(vaultPath) != "" {
		input["vault_path"] = strings.TrimSpace(vaultPath)
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	repoRoot, err := resolveAgentctlRepoRoot()
	if err != nil {
		return nil, err
	}
	result := executil.RunWithInput(ctx, repoRoot, "go", body, "run", "./skills/code_semantic_search")
	if result.Err != nil {
		return nil, fmt.Errorf("run current code/semantic_search source: %w", result.Err)
	}
	env, err := protocol.DecodeEnvelope(result.Stdout)
	if err != nil {
		return nil, err
	}
	if env.Status == envelope.StatusError {
		return nil, protocol.EnvelopeStatusErrorFromEnvelope(env)
	}
	var out semanticSearchEvalOutput
	if err := protocol.DecodeEnvelopeDataInto(env, &out); err != nil {
		return nil, err
	}
	return extractSemanticSearchResultPaths(out.Results), nil
}

func resolveAgentctlRepoRoot() (string, error) {
	if _, file, _, ok := runtime.Caller(0); ok {
		candidate := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
		if _, err := os.Stat(filepath.Join(candidate, "skills", "code_semantic_search", "main.go")); err == nil {
			return candidate, nil
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "skills", "code_semantic_search", "main.go")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("resolve agentctl repo root: could not locate skills/code_semantic_search/main.go")
}

func extractSemanticSearchResultPaths(results []struct {
	Path string `json:"path"`
},
) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(results))
	for _, result := range results {
		path := filepath.ToSlash(strings.TrimSpace(result.Path))
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}

func slicesContain(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func hasMode(modes []string, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	for _, mode := range modes {
		if mode == target {
			return true
		}
	}
	return false
}

func ensureTopOfMindForEval(ctx context.Context, storageRoot, workspacePath string, store *contextplane.WorkspaceStore) error {
	if _, err := store.LoadTopOfMind(); err == nil {
		return nil
	}
	taskStore, err := tasks.Open(ctx, storageRoot)
	if err != nil {
		return err
	}
	defer func() { _ = taskStore.Close() }()
	sessionStore, err := sessions.Open(ctx, storageRoot)
	if err != nil {
		return err
	}
	defer func() { _ = sessionStore.Close() }()
	orienter := contextplane.NewOrienter(taskStore, sessionStore)
	top, err := orienter.Build(ctx, workspacePath)
	if err != nil {
		return err
	}
	_, err = store.SaveTopOfMind(top)
	return err
}

func extractSearchHitPaths(hits []obsidianindex.SearchHit) []string {
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		out = append(out, filepath.ToSlash(hit.Path))
	}
	return out
}

func extractRetrievalHitPaths(hits []contextplane.RetrievalHit) []string {
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		out = append(out, filepath.ToSlash(hit.Path))
	}
	return out
}

func filterEvalPaths(paths []string, canonicalOnly bool) []string {
	if !canonicalOnly {
		return paths
	}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = filepath.ToSlash(strings.TrimSpace(path))
		if path == "" {
			continue
		}
		if strings.HasPrefix(path, "notes/") || strings.HasPrefix(path, "00-home/") || strings.HasPrefix(path, "atlas/") {
			out = append(out, path)
		}
	}
	return out
}

func saveEvalOutputs(workspacePath, saveDir, suite string, result retrievaleval.RunResult, markdown string) (string, string, error) {
	dir := strings.TrimSpace(saveDir)
	if dir == "" {
		dir = filepath.Join(resolveContextWorkspace(workspacePath), ".agentctl", "exports", "evals")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	stamp := result.GeneratedAt.UTC().Format("20060102T150405Z")
	base := sanitizeEvalName(suite)
	jsonPath := filepath.Join(dir, base+"-"+stamp+".json")
	markdownPath := filepath.Join(dir, base+"-"+stamp+".md")
	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", "", err
	}
	body = append(body, '\n')
	if err := os.WriteFile(jsonPath, body, 0o644); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(markdownPath, []byte(markdown), 0o644); err != nil {
		return "", "", err
	}
	return jsonPath, markdownPath, nil
}

func sanitizeEvalName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "-")
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-':
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "eval"
	}
	return out
}

func init() {
	rootCmd.AddCommand(newEvalCommand())
}
