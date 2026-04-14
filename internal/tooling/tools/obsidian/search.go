package obsidian

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// SearchOptions configures an Obsidian vault search.
type SearchOptions struct {
	BinaryPath string
	VaultName  string
	VaultPath  string
	Query      string
	ScopePath  string
	Limit      int
}

// SearchMatch is a best-effort structured search hit.
type SearchMatch struct {
	Path string `json:"path,omitempty"`
	Line int    `json:"line,omitempty"`
	Text string `json:"text,omitempty"`
}

// SearchResult captures raw output plus any structured matches we could parse.
type SearchResult struct {
	VaultName string        `json:"vault_name"`
	Query     string        `json:"query"`
	Raw       string        `json:"raw"`
	Matches   []SearchMatch `json:"matches,omitempty"`
}

// Search executes `obsidian search` with JSON output when available.
func Search(ctx context.Context, opts SearchOptions) (*SearchResult, error) {
	if strings.TrimSpace(opts.Query) == "" {
		return nil, fmt.Errorf("obsidian: query required")
	}

	binary := resolveBinary(opts.BinaryPath)
	vaultName, err := ResolveVaultName(ctx, binary, opts.VaultName, opts.VaultPath)
	if err != nil {
		return nil, err
	}

	args := []string{"search", "vault=" + vaultName, "query=" + opts.Query, "format=json"}
	if trimmed := strings.TrimSpace(opts.ScopePath); trimmed != "" {
		args = append(args, "path="+trimmed)
	}
	if opts.Limit > 0 {
		args = append(args, "limit="+strconv.Itoa(opts.Limit))
	}

	stdout, stderr, err := runCLI(ctx, binary, args...)
	if err != nil {
		return nil, formatCLIError("search", err, stderr)
	}

	raw := strings.TrimRight(string(stdout), "\n")
	return &SearchResult{
		VaultName: vaultName,
		Query:     opts.Query,
		Raw:       raw,
		Matches:   parseSearchMatches(stdout),
	}, nil
}

func parseSearchMatches(data []byte) []SearchMatch {
	var list []map[string]any
	if err := json.Unmarshal(data, &list); err == nil {
		return parseMatchMaps(list)
	}

	var wrapped struct {
		Matches []map[string]any `json:"matches"`
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(data, &wrapped); err == nil {
		if len(wrapped.Matches) > 0 {
			return parseMatchMaps(wrapped.Matches)
		}
		if len(wrapped.Results) > 0 {
			return parseMatchMaps(wrapped.Results)
		}
	}

	return nil
}

func parseMatchMaps(items []map[string]any) []SearchMatch {
	out := make([]SearchMatch, 0, len(items))
	for _, item := range items {
		match := SearchMatch{}
		if v, ok := item["path"].(string); ok {
			match.Path = strings.TrimSpace(v)
		}
		if v, ok := item["text"].(string); ok {
			match.Text = strings.TrimSpace(v)
		}
		if v, ok := item["line"].(float64); ok {
			match.Line = int(v)
		}
		if match.Path == "" && match.Text == "" && match.Line == 0 {
			continue
		}
		out = append(out, match)
	}
	return out
}
