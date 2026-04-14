// Package main implements the session/query skill for structured annotation queries.
package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/annotations"
	"github.com/joshka0/foxctl/internal/storage/sessions"
)

const (
	commandName       = "session/query"
	defaultLimit      = 20
	defaultLookahead  = 3
	defaultLookahead2 = 5
	defaultTopK       = 1
)

// Input defines the session/query input.
type Input struct {
	FilePath       string `json:"file_path,omitempty"`
	ErrorChains    bool   `json:"error_chains,omitempty"`
	ListCategories bool   `json:"list_categories,omitempty"`

	SessionID string `json:"session_id,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	Project   string `json:"project,omitempty"`
	Limit     int    `json:"limit,omitempty"`

	Detail bool `json:"detail,omitempty"`

	Query        string `json:"query,omitempty"`
	LookaheadMin int    `json:"lookahead_min,omitempty"`
	LookaheadMax int    `json:"lookahead_max,omitempty"`
	TopK         int    `json:"top_k,omitempty"`
}

// Output defines the session/query output.
type Output struct {
	Status  string `json:"status"`
	Message string `json:"message"`

	FileTrackingSummaries   []FileTrackingSummary    `json:"file_tracking_summaries,omitempty"`
	FileTrackingAnnotations []FileTrackingAnnotation `json:"file_tracking_annotations,omitempty"`
	ErrorChains             []ErrorChain             `json:"error_chains,omitempty"`
	CategoryCounts          []CategoryCount          `json:"category_counts,omitempty"`
}

// FileTrackingSummary aliases the storage summary type.
type FileTrackingSummary = annotations.FileTrackingSummary

// CategoryCount aliases the storage category count type.
type CategoryCount = annotations.CategoryCount

// FileTrackingAnnotation is an annotation-level file tracking result.
type FileTrackingAnnotation struct {
	SessionID      string   `json:"session_id"`
	TurnIndex      int      `json:"turn_index"`
	WindowIndex    int      `json:"window_index"`
	MatchedAt      string   `json:"matched_at,omitempty"`
	Role           string   `json:"role,omitempty"`
	ChunkType      string   `json:"chunk_type,omitempty"`
	TOCCategory    string   `json:"toc_category,omitempty"`
	TOCLabel       string   `json:"toc_label,omitempty"`
	Intent         string   `json:"intent,omitempty"`
	ContentPreview string   `json:"content_preview,omitempty"`
	FilePaths      []string `json:"file_paths,omitempty"`
	ToolsUsed      []string `json:"tools_used,omitempty"`
	HasError       bool     `json:"has_error"`
}

// ErrorAnnotation captures a matched error annotation.
type ErrorAnnotation struct {
	SessionID      string   `json:"session_id"`
	TurnIndex      int      `json:"turn_index"`
	WindowIndex    int      `json:"window_index"`
	MatchedAt      string   `json:"matched_at,omitempty"`
	TOCCategory    string   `json:"toc_category,omitempty"`
	TOCLabel       string   `json:"toc_label,omitempty"`
	Intent         string   `json:"intent,omitempty"`
	ContentPreview string   `json:"content_preview,omitempty"`
	FilePaths      []string `json:"file_paths,omitempty"`
	ToolsUsed      []string `json:"tools_used,omitempty"`
	HasError       bool     `json:"has_error"`
	Similarity     float64  `json:"similarity,omitempty"`
}

// ErrorFixCandidate describes a likely fix for an error turn.
type ErrorFixCandidate struct {
	SessionID      string   `json:"session_id"`
	TurnIndex      int      `json:"turn_index"`
	WindowIndex    int      `json:"window_index"`
	MatchedAt      string   `json:"matched_at,omitempty"`
	TOCCategory    string   `json:"toc_category,omitempty"`
	TOCLabel       string   `json:"toc_label,omitempty"`
	Intent         string   `json:"intent,omitempty"`
	ContentPreview string   `json:"content_preview,omitempty"`
	FilePaths      []string `json:"file_paths,omitempty"`
	OverlapScore   int      `json:"overlap_score"`
	Distance       int      `json:"distance"`
}

// ErrorChain links one error turn with ranked fix candidates.
type ErrorChain struct {
	SessionID string              `json:"session_id"`
	Error     ErrorAnnotation     `json:"error"`
	Fixes     []ErrorFixCandidate `json:"fixes,omitempty"`
	Resolved  bool                `json:"resolved"`
}

func main() {
	config.LoadDotEnv()
	skillmain.Main(commandName, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	normalizeInput(&in, rc)

	modeCount := 0
	if strings.TrimSpace(in.FilePath) != "" {
		modeCount++
	}
	if in.ErrorChains {
		modeCount++
	}
	if in.ListCategories {
		modeCount++
	}
	if modeCount != 1 {
		return skillerr.Arg("exactly one mode is required: file_path, error_chains, or list_categories")
	}
	if in.ErrorChains && strings.TrimSpace(in.Query) == "" {
		return skillerr.Arg("query is required when error_chains is true")
	}
	if in.LookaheadMin < 0 {
		return skillerr.Arg("lookahead_min must be >= 0")
	}
	if in.LookaheadMax <= 0 {
		return skillerr.Arg("lookahead_max must be > 0")
	}
	if in.LookaheadMax < in.LookaheadMin {
		return skillerr.Arg("lookahead_max must be >= lookahead_min")
	}
	if in.TopK <= 0 {
		return skillerr.Arg("top_k must be > 0")
	}

	sessionStore, err := rc.Stores.Sessions(ctx)
	if err != nil {
		return skillerr.WrapIO("open sessions store", err)
	}

	sessionIDs, err := resolveSessionScope(ctx, sessionStore, in)
	if err != nil {
		return err
	}

	hasScope := strings.TrimSpace(in.SessionID) != "" || strings.TrimSpace(in.Workspace) != "" || strings.TrimSpace(in.Project) != ""
	if hasScope && len(sessionIDs) == 0 {
		return skillout.Emit(rc, commandName, Output{
			Status:  "no_matches",
			Message: "No sessions matched the requested scope",
		})
	}

	annStore, err := annotations.Open(ctx, "")
	if err != nil {
		return skillerr.WrapIO("open annotations store", err)
	}
	defer annStore.Close()

	output := Output{Status: "ok"}

	switch {
	case strings.TrimSpace(in.FilePath) != "":
		if in.Detail {
			anns, err := annStore.ListByFilePath(ctx, strings.TrimSpace(in.FilePath), sessionIDs, in.Limit)
			if err != nil {
				return skillerr.WrapIO("query annotations by file path", err)
			}
			output.FileTrackingAnnotations = make([]FileTrackingAnnotation, 0, len(anns))
			for _, ann := range anns {
				output.FileTrackingAnnotations = append(output.FileTrackingAnnotations, fileTrackingAnnotationFromTurn(ann))
			}
			if len(output.FileTrackingAnnotations) == 0 {
				output.Status = "no_matches"
				output.Message = "No annotations matched the file path"
			} else {
				output.Message = fmt.Sprintf("Found %d annotations for file path", len(output.FileTrackingAnnotations))
			}
		} else {
			summaries, err := annStore.SummarizeByFilePath(ctx, strings.TrimSpace(in.FilePath), sessionIDs)
			if err != nil {
				return skillerr.WrapIO("summarize annotations by file path", err)
			}
			output.FileTrackingSummaries = summaries
			if len(summaries) == 0 {
				output.Status = "no_matches"
				output.Message = "No sessions matched the file path"
			} else {
				output.Message = fmt.Sprintf("Found %d session summaries for file path", len(summaries))
			}
		}

	case in.ListCategories:
		counts, err := annStore.CountByCategory(ctx, sessionIDs)
		if err != nil {
			return skillerr.WrapIO("count annotations by category", err)
		}
		output.CategoryCounts = counts
		if len(counts) == 0 {
			output.Status = "no_matches"
			output.Message = "No categories found"
		} else {
			output.Message = fmt.Sprintf("Found %d categories", len(counts))
		}

	case in.ErrorChains:
		voyageKey := os.Getenv("VOYAGE_API_KEY")
		geminiKey := os.Getenv("GEMINI_API_KEY")
		if semantic.DetectProviderForConfig(rc.Config, voyageKey, geminiKey) == "" {
			return skillerr.Arg("error_chains requires embeddings (configure a repo embedding provider or set VOYAGE_API_KEY / GEMINI_API_KEY)")
		}

		provider, err := semantic.NewProviderForScope(
			semantic.ScopeSessions,
			rc.Config,
			semantic.WithVoyageKey(voyageKey),
			semantic.WithGeminiKey(geminiKey),
			semantic.WithRateLimitWait(true),
		)
		if err != nil {
			return skillerr.WrapRuntime("embedding provider", err)
		}

		enrichedQuery := semantic.EnrichQuery(in.Query)
		var queryEmbedding []float32
		if queryProvider, ok := provider.(semantic.QueryEmbeddingProvider); ok {
			queryEmbedding, err = queryProvider.EmbedQuery(ctx, enrichedQuery)
		} else {
			queryEmbedding, err = provider.Embed(ctx, enrichedQuery)
		}
		if err != nil {
			return skillerr.WrapRuntime("generate query embedding", err)
		}
		if len(queryEmbedding) == 0 {
			return skillerr.Arg("query embedding is empty")
		}

		errorsOnly, err := annStore.SearchSimilarFiltered(ctx, queryEmbedding, annotations.AnnotationSearchOptions{
			Limit:       in.Limit * 4,
			TOCCategory: "debug",
			HasErrors:   true,
			SessionIDs:  sessionIDs,
		})
		if err != nil {
			return skillerr.WrapIO("search debug annotations", err)
		}

		type scoredError struct {
			ann        *annotations.TurnAnnotation
			similarity float64
		}
		errorTurns := make([]scoredError, 0, len(errorsOnly))
		seen := make(map[string]struct{}, len(errorsOnly))
		for _, scored := range errorsOnly {
			if scored.TurnAnnotation == nil {
				continue
			}
			key := fmt.Sprintf("%s:%d", scored.SessionID, scored.TurnIndex)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			errorTurns = append(errorTurns, scoredError{ann: scored.TurnAnnotation, similarity: scored.Similarity})
			if len(errorTurns) >= in.Limit {
				break
			}
		}

		chains := make([]ErrorChain, 0, len(errorTurns))
		for _, scored := range errorTurns {
			startTurn := scored.ann.TurnIndex
			fixes, err := annStore.ListBySessionTurnRange(ctx, scored.ann.SessionID, startTurn, startTurn+in.LookaheadMax, "code_change", 8)
			if err != nil {
				return skillerr.WrapIO("load error chain candidates", err)
			}

			minFixTurn := startTurn + in.LookaheadMin
			windowFixes := make([]*annotations.TurnAnnotation, 0, len(fixes))
			for _, fix := range fixes {
				if fix.TurnIndex < minFixTurn {
					continue
				}
				windowFixes = append(windowFixes, fix)
			}

			rankedFixes := rankFixes(scored.ann, windowFixes, in.TopK)
			chains = append(chains, ErrorChain{
				SessionID: scored.ann.SessionID,
				Error:     errorAnnotationFromTurn(scored.ann, scored.similarity),
				Fixes:     rankedFixes,
				Resolved:  len(rankedFixes) > 0,
			})
		}

		output.ErrorChains = chains
		if len(chains) == 0 {
			output.Status = "no_matches"
			output.Message = "No matching error turns found"
		} else {
			output.Message = fmt.Sprintf("Found %d error chains", len(chains))
		}
	}

	return skillout.Emit(rc, commandName, output)
}

func normalizeInput(in *Input, rc *skillmain.RunContext) {
	if in.Limit <= 0 {
		in.Limit = defaultLimit
	}
	if in.LookaheadMin == 0 {
		in.LookaheadMin = defaultLookahead
	}
	if in.LookaheadMax == 0 {
		in.LookaheadMax = defaultLookahead2
	}
	if in.TopK == 0 {
		in.TopK = defaultTopK
	}
	if strings.TrimSpace(in.Workspace) == "" {
		in.Workspace = rc.Workspace
	}
}

func fileTrackingAnnotationFromTurn(ann *annotations.TurnAnnotation) FileTrackingAnnotation {
	matchAt := annotationMatchTime(ann)
	out := FileTrackingAnnotation{
		SessionID:      ann.SessionID,
		TurnIndex:      ann.TurnIndex,
		WindowIndex:    ann.ContextWindowIndex,
		Role:           ann.Role,
		ChunkType:      ann.ChunkType,
		TOCCategory:    ann.TOCCategory,
		TOCLabel:       ann.TOCLabel,
		Intent:         ann.Intent,
		ContentPreview: ann.ContentPreview,
		FilePaths:      ann.FilePaths,
		ToolsUsed:      ann.ToolsUsed,
		HasError:       ann.HasError,
	}
	if !matchAt.IsZero() {
		out.MatchedAt = matchAt.Format(time.RFC3339)
	}
	return out
}

func errorAnnotationFromTurn(ann *annotations.TurnAnnotation, similarity float64) ErrorAnnotation {
	matchAt := annotationMatchTime(ann)
	out := ErrorAnnotation{
		SessionID:      ann.SessionID,
		TurnIndex:      ann.TurnIndex,
		WindowIndex:    ann.ContextWindowIndex,
		TOCCategory:    ann.TOCCategory,
		TOCLabel:       ann.TOCLabel,
		Intent:         ann.Intent,
		ContentPreview: ann.ContentPreview,
		FilePaths:      ann.FilePaths,
		ToolsUsed:      ann.ToolsUsed,
		HasError:       ann.HasError,
		Similarity:     similarity,
	}
	if !matchAt.IsZero() {
		out.MatchedAt = matchAt.Format(time.RFC3339)
	}
	return out
}

func annotationMatchTime(ann *annotations.TurnAnnotation) time.Time {
	if ann == nil {
		return time.Time{}
	}
	if !ann.Timestamp.IsZero() {
		return ann.Timestamp
	}
	return ann.CreatedAt
}

func overlapScore(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	left := make(map[string]struct{}, len(a))
	for _, item := range a {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		left[item] = struct{}{}
	}
	score := 0
	for _, item := range b {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := left[item]; ok {
			score++
		}
	}
	return score
}

func rankFixes(errorAnn *annotations.TurnAnnotation, fixes []*annotations.TurnAnnotation, topK int) []ErrorFixCandidate {
	type scoredFix struct {
		ann      *annotations.TurnAnnotation
		overlap  int
		distance int
	}
	if errorAnn == nil || len(fixes) == 0 {
		return nil
	}

	scored := make([]scoredFix, 0, len(fixes))
	for _, fix := range fixes {
		if fix == nil {
			continue
		}
		scored = append(scored, scoredFix{
			ann:      fix,
			overlap:  overlapScore(errorAnn.FilePaths, fix.FilePaths),
			distance: fix.TurnIndex - errorAnn.TurnIndex,
		})
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].overlap != scored[j].overlap {
			return scored[i].overlap > scored[j].overlap
		}
		if scored[i].distance != scored[j].distance {
			return scored[i].distance < scored[j].distance
		}
		if scored[i].ann.SessionID != scored[j].ann.SessionID {
			return scored[i].ann.SessionID < scored[j].ann.SessionID
		}
		return scored[i].ann.TurnIndex < scored[j].ann.TurnIndex
	})

	if topK > len(scored) {
		topK = len(scored)
	}
	out := make([]ErrorFixCandidate, 0, topK)
	for i := 0; i < topK; i++ {
		fix := scored[i]
		matchAt := annotationMatchTime(fix.ann)
		candidate := ErrorFixCandidate{
			SessionID:      fix.ann.SessionID,
			TurnIndex:      fix.ann.TurnIndex,
			WindowIndex:    fix.ann.ContextWindowIndex,
			TOCCategory:    fix.ann.TOCCategory,
			TOCLabel:       fix.ann.TOCLabel,
			Intent:         fix.ann.Intent,
			ContentPreview: fix.ann.ContentPreview,
			FilePaths:      fix.ann.FilePaths,
			OverlapScore:   fix.overlap,
			Distance:       fix.distance,
		}
		if !matchAt.IsZero() {
			candidate.MatchedAt = matchAt.Format(time.RFC3339)
		}
		out = append(out, candidate)
	}
	return out
}

func resolveSessionScope(ctx context.Context, sessionStore *sessions.Store, in Input) ([]string, error) {
	sessionID := strings.TrimSpace(in.SessionID)
	workspace := strings.TrimSpace(in.Workspace)
	project := strings.TrimSpace(in.Project)

	if sessionID != "" {
		sess, err := sessionStore.Get(ctx, sessionID)
		if err != nil {
			return nil, skillerr.WrapArg("session_id not found", err)
		}
		if workspace != "" && sess.WorkspacePath != workspace {
			return []string{}, nil
		}
		if project != "" && sess.ProjectName != project {
			return []string{}, nil
		}
		return []string{sessionID}, nil
	}

	if workspace == "" && project == "" {
		return nil, nil
	}

	const pageSize = 200
	offset := 0
	ids := make([]string, 0, pageSize)
	for {
		list, err := sessionStore.List(ctx, sessions.ListOptions{
			WorkspacePath: workspace,
			ProjectName:   project,
			Limit:         pageSize,
			Offset:        offset,
		})
		if err != nil {
			return nil, skillerr.WrapIO("list sessions for scope", err)
		}
		for _, sess := range list {
			ids = append(ids, sess.ID)
		}
		if len(list) < pageSize {
			break
		}
		offset += len(list)
	}
	return ids, nil
}
