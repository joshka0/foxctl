package deps

import (
	"context"
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/langutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/pathutil"
	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/intelligence/repoquery"
	refscope "github.com/jkatigb/agentctl/internal/intelligence/refactor/scope"
	refstatus "github.com/jkatigb/agentctl/internal/intelligence/refactor/status"
)

// Searcher is the minimal repo query search surface used by refactor deps.
type Searcher interface {
	SearchWithProjection(ctx context.Context, req repoquery.SearchRequest) (repoquery.SearchOutput, error)
}

// BuildError reports a user-correctable deps request failure.
type BuildError struct {
	Message string
	Hint    string
}

func (e *BuildError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// Input captures the user-facing refactor deps request.
type Input struct {
	Scope      refscope.Scope
	Status     refstatus.Status
	Seeds      []string
	Query      string
	SeedLimit  int
	EdgeSets   []string
	EdgeTypes  []string
	Direction  string
	Depth      int
	Budget     int
	PerNodeCap int
}

// BuildResult captures the resolved deps request and seed discovery context.
type BuildResult struct {
	Request        repoquery.ExpandRequest
	IndexMode      refstatus.Mode
	Reasons        []string
	SeedQuery      string
	SeedCandidates []repoindex.Node
}

// BuildRequest validates and resolves a refactor deps request.
func BuildRequest(ctx context.Context, searcher Searcher, in Input) (BuildResult, error) {
	if len(in.Seeds) > 0 && strings.TrimSpace(in.Query) != "" {
		return BuildResult{}, &BuildError{
			Message: "use either explicit seeds or a search query, not both",
			Hint:    "Pass repeated --seed values for raw repoindex node IDs, or use --query to resolve seeds from the index.",
		}
	}

	seeds := normalizeSeeds(in.Seeds)
	candidates := []repoindex.Node(nil)
	seedQuery := ""
	if len(seeds) == 0 {
		if strings.TrimSpace(in.Query) == "" {
			return BuildResult{}, &BuildError{
				Message: "a deps query needs either --seed or --query",
				Hint:    "Use --query with a symbol/file search term, or pass explicit repoindex node IDs with --seed.",
			}
		}
		if searcher == nil {
			return BuildResult{}, fmt.Errorf("deps searcher is required when resolving query seeds")
		}
		req, err := repoquery.NewSearchRequest(in.Query, in.SeedLimit)
		if err != nil {
			return BuildResult{}, err
		}
		seedQuery = req.Query
		output, err := searcher.SearchWithProjection(ctx, req)
		if err != nil {
			return BuildResult{}, err
		}
		candidates = filterNodesToScope(output.Nodes, in.Scope)
		if len(candidates) == 0 {
			return BuildResult{}, &BuildError{
				Message: fmt.Sprintf("no repoindex nodes matched query %q within scope %q", strings.TrimSpace(in.Query), in.Scope.Path),
				Hint:    "Try a broader query, widen --path, or rebuild the repo index for this workspace.",
			}
		}
		limit := in.SeedLimit
		if limit <= 0 {
			limit = 5
		}
		for _, node := range candidates {
			if strings.TrimSpace(node.ID) == "" {
				continue
			}
			seeds = append(seeds, node.ID)
			if len(seeds) >= limit {
				break
			}
		}
	}

	if len(seeds) == 0 {
		return BuildResult{}, &BuildError{
			Message: "no dependency seeds resolved",
			Hint:    "Verify the query or seed IDs, and make sure the repo index has nodes for this scope.",
		}
	}

	mergedEdges, err := repoquery.MergeEdgeTypes(in.EdgeSets, in.EdgeTypes)
	if err != nil {
		return BuildResult{}, err
	}
	req, err := repoquery.NewExpandRequest(seeds, repoquery.EdgeTypeValues(mergedEdges), in.Direction, in.Depth, in.Budget, in.PerNodeCap)
	if err != nil {
		return BuildResult{}, err
	}

	return BuildResult{
		Request:        req,
		IndexMode:      in.Status.Mode,
		Reasons:        append([]string(nil), in.Status.Reasons...),
		SeedQuery:      seedQuery,
		SeedCandidates: candidates[:min(len(candidates), len(seeds))],
	}, nil
}

func normalizeSeeds(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func filterNodesToScope(nodes []repoindex.Node, scope refscope.Scope) []repoindex.Node {
	out := make([]repoindex.Node, 0, len(nodes))
	for _, node := range nodes {
		if nodeMatchesScope(node, scope) {
			out = append(out, node)
		}
	}
	return out
}

func nodeMatchesScope(node repoindex.Node, scope refscope.Scope) bool {
	if !nodeMatchesLanguage(node, scope.Language) {
		return false
	}
	scopePath := pathutil.ToSlash(strings.TrimSpace(scope.Path))
	if scopePath == "" || scopePath == "." {
		return true
	}
	if file := pathutil.ToSlash(strings.TrimSpace(node.File)); file != "" {
		return file == scopePath || strings.HasPrefix(file, scopePath+"/")
	}
	if pkg := pathutil.ToSlash(strings.TrimSpace(node.Pkg)); pkg != "" {
		return strings.Contains(pkg, scopePath)
	}
	return false
}

func nodeMatchesLanguage(node repoindex.Node, language string) bool {
	language = strings.TrimSpace(language)
	if language == "" || language == "auto" {
		return true
	}
	if file := strings.TrimSpace(node.File); file != "" {
		return langutil.DetectAllowedWithHint(language, file, langutil.CommonCodeLanguages) != ""
	}
	switch language {
	case "go":
		return strings.HasPrefix(strings.TrimSpace(node.Pkg), "go:")
	case "typescript", "javascript":
		return strings.HasPrefix(strings.TrimSpace(node.Pkg), "ts:")
	case "python":
		return strings.HasPrefix(strings.TrimSpace(node.Pkg), "py:")
	case "elixir":
		return strings.HasPrefix(strings.TrimSpace(node.Pkg), "ex:")
	default:
		return true
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
