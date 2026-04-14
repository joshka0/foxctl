// Package main implements the code/snippet_extract skill as a thin wrapper
// around the shared internal/intelligence/codecontext extraction engine.
package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/intelligence/codecontext"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/platform/config"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/runtime/observability"
)

const (
	Command      = "code/snippet_extract"
	ArtifactKind = "application/x-swe-grep-snippets+ndjson"
)

// Error codes per Core Profile v1 §13 and spec §5.4.
const (
	ErrCodeArg              = "EARG"
	ErrCodeRuntime          = "ERUNTIME"
	ErrCodePolicy           = "EPOLICY"
	ErrCodeNotFound         = "ENOTFOUND"
	ErrCodeIO               = "EIO"
	ErrCodeNoCandidates     = "E_SWE_GREP_NO_CANDIDATES"
	ErrCodeCapabilityPolicy = "EPOLICY"
)

const (
	DefaultMaxRelatedSessions = 3
	ContextSearchTimeout      = 500 * time.Millisecond
	DefaultMaxFiles           = 50
	DefaultMaxSnippets        = 100
	DefaultMaxBytesPerFile    = 64 * 1024
)

type Limits struct {
	MaxFiles        int `json:"max_files,omitempty"`
	MaxSnippets     int `json:"max_snippets,omitempty"`
	MaxBytesPerFile int `json:"max_bytes_per_file,omitempty"`
}

type Input struct {
	WorkspaceID string                  `json:"workspace_id"`
	Question    string                  `json:"question"`
	Query       string                  `json:"query"`
	InlineMode  string                  `json:"inline_mode,omitempty"`
	Candidates  []codecontext.Candidate `json:"candidates"`
	Limits      Limits                  `json:"limits,omitempty"`
}

type InlineMode string

const (
	InlineModeAuto         InlineMode = "auto"
	InlineModeFull         InlineMode = "full"
	InlineModePreview      InlineMode = "preview"
	InlineModeArtifactOnly InlineMode = "artifact_only"
	defaultPreviewSnippets            = 12
)

type SessionContext struct {
	SessionID string   `json:"session_id"`
	Summary   string   `json:"summary"`
	Gotchas   []string `json:"gotchas,omitempty"`
	Decisions []string `json:"decisions,omitempty"`
	KeyFiles  []string `json:"key_files,omitempty"`
}

func newSkillError(code, message, hint string, opts ...skillerr.Option) *skillerr.Error {
	err := &skillerr.Error{Code: code, Message: message}
	if strings.TrimSpace(hint) != "" {
		err.Hint = hint
	}
	for _, opt := range opts {
		opt(err)
	}
	return err
}

func main() {
	skillmain.Main(Command, skillmain.Chain(run,
		skillmain.WithRecover[Input](),
	))
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	query := strings.TrimSpace(in.Question)
	if query == "" {
		query = strings.TrimSpace(in.Query)
	}
	if query == "" {
		return newSkillError(ErrCodeArg, "question or query is required", "provide a non-empty question or query to guide snippet extraction")
	}

	if in.WorkspaceID == "" {
		in.WorkspaceID = ws.ID(rc.PathValidator.Workspace())
	}

	usable := 0
	for _, c := range in.Candidates {
		if strings.TrimSpace(c.Path) != "" {
			usable++
		}
	}
	if usable == 0 {
		return newSkillError(
			ErrCodeNoCandidates,
			"no usable candidates provided. Hint: use code/smart_search if you don't have candidates - it auto-generates them from indexes",
			"pass at least one candidate path or use code/smart_search to generate candidates automatically",
		)
	}

	start := time.Now()
	inlineMode, err := parseInlineMode(in.InlineMode)
	if err != nil {
		return err
	}

	limits := applyDefaultLimits(in.Limits)
	inlineKB := rc.InlineKB
	if inlineKB <= 0 {
		inlineKB = config.DefaultInlineOutputKB
	}

	evidence, err := codecontext.Collect(ctx, codecontext.CollectOpts{
		Candidates:      in.Candidates,
		Query:           query,
		PathValidator:   rc.PathValidator,
		MaxFiles:        limits.MaxFiles,
		MaxSnippets:     limits.MaxSnippets,
		MaxBytesPerFile: limits.MaxBytesPerFile,
		ContextLines:    3,
		Mode:            codecontext.ModeSnippets,
	})
	if err != nil {
		return skillerr.WrapRuntime("collect code evidence", err)
	}
	if err := fatalErrorForEvidence(evidence); err != nil {
		return err
	}

	output, artifactPayload, err := codecontext.PrepareOutputWithArtifact(
		evidence,
		inlineKB,
		512,
		codecontext.RenderNDJSON,
	)
	if err != nil {
		return skillerr.WrapRuntime("prepare codecontext output", err)
	}

	inlinePreviews := output.SnippetsInline
	if artifactPayload != nil && len(inlinePreviews) == 0 && len(evidence.Snippets) > 0 {
		inlinePreviews = codecontext.MakePreviews(evidence.Snippets, 512)
	}

	data := map[string]any{
		"summary": map[string]int{
			"files_considered": evidence.Stats.FilesProcessed + evidence.Stats.FilesSkipped,
			"files_relevant":   evidence.Stats.FilesProcessed,
			"snippets_emitted": len(evidence.Snippets),
		},
		"snippets_inline": inlinePreviews,
		"snippets_total":  len(evidence.Snippets),
		"inline_mode":     string(inlineMode),
		"truncated":       output.Truncated || artifactPayload != nil,
	}
	if len(output.Hints) > 0 {
		data["hints"] = output.Hints
	}

	workspacePath := rc.PathValidator.Workspace()
	if relatedSessions, hint := searchRelatedSessions(ctx, rc, workspacePath, query, DefaultMaxRelatedSessions); len(relatedSessions) > 0 {
		data["related_sessions"] = relatedSessions
	} else if hint != "" {
		data["related_sessions_hint"] = hint
	}

	if artifactPayload != nil {
		artifact, err := skillmain.PersistBuffer(ctx, rc, bytes.NewBuffer(artifactPayload.Data), artifactPayload.Kind, "code_snippet_extract")
		if err != nil {
			return skillerr.WrapIO("persist snippets artifact", err)
		}
		skillout.AddArtifact(data, &artifact)
	}

	applySnippetInlineMode(data, inlineMode)

	hasArtifact := data["artifact"] != nil
	durationMS := time.Since(start).Milliseconds()

	logSummary(
		in.WorkspaceID,
		query,
		len(in.Candidates),
		evidence.Stats.FilesProcessed+evidence.Stats.FilesSkipped,
		evidence.Stats.FilesProcessed,
		len(evidence.Snippets),
		hasArtifact,
	)

	ev := observability.NewSweGrepEvent(
		in.WorkspaceID,
		query,
		len(in.Candidates),
		evidence.Stats.FilesProcessed+evidence.Stats.FilesSkipped,
		evidence.Stats.FilesProcessed,
		len(evidence.Snippets),
		hasArtifact,
		durationMS,
		"run",
	)
	_ = observability.WriteSweGrepEvent(ctx, ev)

	return skillout.Emit(rc, Command, data)
}

func parseInlineMode(value string) (InlineMode, error) {
	switch InlineMode(strings.ToLower(strings.TrimSpace(value))) {
	case "", InlineModeAuto:
		return InlineModeAuto, nil
	case InlineModeFull:
		return InlineModeFull, nil
	case InlineModePreview:
		return InlineModePreview, nil
	case InlineModeArtifactOnly:
		return InlineModeArtifactOnly, nil
	default:
		return InlineModeAuto, skillerr.Validationf("invalid inline_mode: %s (valid: auto, full, preview, artifact_only)", strings.TrimSpace(value))
	}
}

func applySnippetInlineMode(data map[string]any, requested InlineMode) {
	raw, _ := data["snippets_inline"].([]codecontext.SnippetPreview)
	hasArtifact := data["artifact"] != nil
	resolved := requested
	if resolved == InlineModeAuto {
		if hasArtifact || len(raw) > defaultPreviewSnippets {
			resolved = InlineModePreview
		} else {
			resolved = InlineModeFull
		}
	}
	if resolved == InlineModeArtifactOnly && !hasArtifact {
		resolved = InlineModePreview
	}
	data["inline_mode"] = string(resolved)

	switch resolved {
	case InlineModeFull:
		return
	case InlineModePreview:
		if len(raw) > defaultPreviewSnippets {
			data["snippets_inline"] = append([]codecontext.SnippetPreview(nil), raw[:defaultPreviewSnippets]...)
			data["truncated"] = true
		}
	case InlineModeArtifactOnly:
		data["snippets_inline"] = []codecontext.SnippetPreview{}
		data["truncated"] = true
	}
}

func fatalErrorForEvidence(evidence *codecontext.Evidence) *skillerr.Error {
	if evidence == nil {
		return newSkillError(ErrCodeRuntime, "failed to collect code evidence", "")
	}
	if evidence.Stats.FilesProcessed > 0 {
		return nil
	}
	if len(evidence.Stats.FileErrors) == 0 {
		return nil
	}

	for _, fe := range evidence.Stats.FileErrors {
		if fe.Code == ErrCodePolicy {
			return newSkillError(ErrCodePolicy, fe.Message, "")
		}
	}
	for _, fe := range evidence.Stats.FileErrors {
		if fe.Code == ErrCodeNotFound {
			return newSkillError(ErrCodeNotFound, fe.Message, "")
		}
	}
	for _, fe := range evidence.Stats.FileErrors {
		if fe.Code == ErrCodeIO || fe.Code == "EIO" || fe.Code == ErrCodeCapabilityPolicy {
			return newSkillError(ErrCodeIO, fe.Message, "")
		}
	}
	return newSkillError(ErrCodeArg, evidence.Stats.FileErrors[0].Message, "")
}

func applyDefaultLimits(l Limits) Limits {
	if l.MaxFiles <= 0 {
		l.MaxFiles = DefaultMaxFiles
	}
	if l.MaxSnippets <= 0 {
		l.MaxSnippets = DefaultMaxSnippets
	}
	if l.MaxBytesPerFile <= 0 {
		l.MaxBytesPerFile = DefaultMaxBytesPerFile
	}
	return l
}

func logSummary(workspaceID, question string, numCandidates, filesConsidered, filesRelevant, snippetsEmitted int, hasArtifact bool) {
	log := zerolog.New(os.Stderr).With().Timestamp().Logger()
	log.Info().
		Str("skill", Command).
		Str("workspace_id", workspaceID).
		Str("question_hash", observability.HashQuestion(question)).
		Int("candidates", numCandidates).
		Int("files_considered", filesConsidered).
		Int("files_relevant", filesRelevant).
		Int("snippets_emitted", snippetsEmitted).
		Bool("has_artifact", hasArtifact).
		Msg("swe_grep_complete")
}

// searchRelatedSessions searches for sessions related to the question using embeddings.
// Returns sessions and a hint when unavailable/non-fatal.
func searchRelatedSessions(ctx context.Context, rc *skillmain.RunContext, workspaceID, question string, limit int) ([]SessionContext, string) {
	provider, hint := createEmbeddingProvider(rc.Config)
	if provider == nil {
		return nil, hint
	}

	embedCtx, embedCancel := context.WithTimeout(ctx, ContextSearchTimeout)
	defer embedCancel()

	queryVec, err := provider.Embed(embedCtx, question)
	if err != nil {
		return nil, fmt.Sprintf("session context unavailable: embedding failed: %v", err)
	}

	sessionStore, err := rc.Stores.Sessions(ctx)
	if err != nil {
		return nil, "session context unavailable: open sessions store failed"
	}

	searchCtx, searchCancel := context.WithTimeout(ctx, ContextSearchTimeout)
	defer searchCancel()

	results, err := sessionStore.SearchSimilar(searchCtx, workspaceID, queryVec, limit*2)
	if err != nil {
		return nil, "session context unavailable: search failed"
	}

	contexts := make([]SessionContext, 0, len(results))
	for _, r := range results {
		s := r.Session
		if workspaceID != "" && s.WorkspacePath != "" && s.WorkspacePath != workspaceID {
			continue
		}
		sc := SessionContext{
			SessionID: s.ID,
			Summary:   s.Summary,
		}
		if len(s.Gotchas) > 0 {
			sc.Gotchas = s.Gotchas
		}
		if len(s.Decisions) > 0 {
			sc.Decisions = s.Decisions
		}
		if len(s.KeyFiles) > 0 {
			sc.KeyFiles = s.KeyFiles
		}
		contexts = append(contexts, sc)
		if len(contexts) >= limit {
			break
		}
	}

	if len(contexts) == 0 {
		return nil, "no related sessions found for this workspace"
	}
	return contexts, ""
}

// createEmbeddingProvider creates an embedding provider from config/env.
// Returns provider (or nil) and a hint when unavailable.
func createEmbeddingProvider(cfg config.Config) (semantic.EmbeddingProvider, string) {
	voyageKey := os.Getenv("VOYAGE_API_KEY")
	geminiKey := os.Getenv("GEMINI_API_KEY")

	switch semantic.DetectProviderForConfig(cfg, voyageKey, geminiKey) {
	case "":
		return nil, "no embedding provider configured; session context disabled"
	case "openai_compat":
		// Local/OpenAI-compatible providers are configured via embedding.provider/base_url/model
		// and do not require VOYAGE_API_KEY or GEMINI_API_KEY.
	}

	provider, err := semantic.NewProviderForScope(
		semantic.ScopeSessions,
		cfg,
		semantic.WithVoyageKey(voyageKey),
		semantic.WithGeminiKey(geminiKey),
	)
	if err != nil {
		return nil, fmt.Sprintf("failed to create embedding provider: %v; session context disabled", err)
	}
	return provider, ""
}
