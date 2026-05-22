package contextplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/context/memorycore"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	contextstore "github.com/joshka0/foxctl/internal/storage/contextengine"
	obsidiantool "github.com/joshka0/foxctl/internal/tooling/tools/obsidian"
)

const (
	autonomousMemoryDraftSourceLane = "contextengine_retrieval_feedback"
	defaultMemoryDraftLimit         = 20
)

// AutonomousMemoryDraftInput is the pure input for planning Obsidian-backed
// memory drafts from typed contextengine retrieval feedback.
type AutonomousMemoryDraftInput struct {
	WorkspaceID   string
	WorkspacePath string
	Now           time.Time
	Feedback      []contextengine.RetrievalFeedback
	Episodes      []contextengine.RetrievalEpisode
	Limit         int
}

// AutonomousMemoryDraftPlan is the draft-only output of one planning pass.
type AutonomousMemoryDraftPlan struct {
	Drafts  []AutonomousMemoryDraft `json:"drafts"`
	Skipped int                     `json:"skipped"`
}

// AutonomousMemoryDraftRunOptions configures one bounded curation pass.
type AutonomousMemoryDraftRunOptions struct {
	StorageRoot   string
	WorkspaceID   string
	WorkspacePath string
	VaultPath     string
	Now           time.Time
	Lookback      time.Duration
	Limit         int
	ApplyDrafts   bool
	DryRun        bool
	BlurWithAgent bool
	BlurAgent     MemoryBlurAgent
	BlurAgentName string
}

// AutonomousMemoryDraftRunReport summarizes one draft-only curation pass.
type AutonomousMemoryDraftRunReport struct {
	Enabled           bool     `json:"enabled"`
	ApplyDrafts       bool     `json:"apply_drafts"`
	FeedbackScanned   int      `json:"feedback_scanned"`
	DraftsPlanned     int      `json:"drafts_planned"`
	DraftsWritten     int      `json:"drafts_written"`
	ProposalsRecorded int      `json:"proposals_recorded"`
	Skipped           int      `json:"skipped"`
	Errors            int      `json:"errors"`
	BlurWithAgent     bool     `json:"blur_with_agent,omitempty"`
	BlurAgent         string   `json:"blur_agent,omitempty"`
	DraftsBlurred     int      `json:"drafts_blurred,omitempty"`
	BlurErrors        int      `json:"blur_errors,omitempty"`
	BlurRejected      int      `json:"blur_rejected,omitempty"`
	DraftPaths        []string `json:"draft_paths,omitempty"`
	ErrorMessages     []string `json:"error_messages,omitempty"`
}

type AutonomousMemoryDraftBlur struct {
	Agent          string                      `json:"agent"`
	AbstractSchema string                      `json:"abstract_schema"`
	MechanismTags  []string                    `json:"mechanism_tags,omitempty"`
	DomainsToAvoid []string                    `json:"domains_to_avoid,omitempty"`
	Confidence     float64                     `json:"confidence,omitempty"`
	LeakageRisk    float64                     `json:"leakage_risk,omitempty"`
	Validation     MemoryBlurValidation        `json:"validation"`
	SourceRefs     []contextengine.EvidenceRef `json:"source_refs,omitempty"`
}

// AutonomousMemoryDraft describes one inbox note and matching review proposal.
type AutonomousMemoryDraft struct {
	DedupeKey     string                      `json:"dedupe_key"`
	WorkspaceID   string                      `json:"workspace_id,omitempty"`
	Title         string                      `json:"title"`
	Summary       string                      `json:"summary"`
	MemoryKind    string                      `json:"memory_kind"`
	DraftPath     string                      `json:"draft_path"`
	TargetPath    string                      `json:"target_path"`
	TargetHeading string                      `json:"target_heading"`
	Content       string                      `json:"content"`
	Statement     string                      `json:"statement,omitempty"`
	Blur          *AutonomousMemoryDraftBlur  `json:"blur,omitempty"`
	SourceRefs    []contextengine.EvidenceRef `json:"source_refs"`
	EvidenceRefs  []contextengine.EvidenceRef `json:"evidence_refs"`
	FeedbackID    string                      `json:"feedback_id"`
	EpisodeID     string                      `json:"episode_id,omitempty"`
	Query         string                      `json:"query"`
	FeedbackKind  string                      `json:"feedback_kind"`
	CreatedAt     time.Time                   `json:"created_at"`
}

// MemoryProposal returns the prepared review proposal corresponding to this draft.
func (d AutonomousMemoryDraft) MemoryProposal() MemoryProposal {
	proposal := MemoryProposal{
		DedupeKey:      d.DedupeKey,
		Kind:           PolicyKindMemoryDraft,
		Classification: "autonomous_memory_draft",
		Status:         "prepared",
		ReviewRequired: true,
		Confidence:     0.7,
		BlastRadius:    "medium",
		Summary:        d.Summary,
		SourceRefs:     d.SourceRefs,
		ProposedChange: map[string]any{
			"title":                      d.Title,
			"draft_path":                 d.DraftPath,
			"suggested_target_note_path": d.TargetPath,
			"suggested_target_heading":   d.TargetHeading,
			"memory_kind":                d.MemoryKind,
			"feedback_id":                d.FeedbackID,
			"episode_id":                 d.EpisodeID,
			"query":                      d.Query,
			"source_lane":                autonomousMemoryDraftSourceLane,
		},
		EvaluationStatus: "accepted",
		ApplyStatus:      "review_prepared",
		CreatedAt:        d.CreatedAt,
		UpdatedAt:        d.CreatedAt,
	}
	if d.Blur != nil {
		proposal.ProposedChange["agent_blurred"] = true
		proposal.ProposedChange["blur_agent"] = d.Blur.Agent
		proposal.ProposedChange["blurred_schema"] = d.Blur.AbstractSchema
		proposal.ProposedChange["mechanism_tags"] = d.Blur.MechanismTags
	}
	return proposal
}

// PlanAutonomousMemoryDrafts converts typed retrieval feedback into deterministic
// Obsidian inbox drafts. It does not mutate stores or the vault.
func PlanAutonomousMemoryDrafts(input AutonomousMemoryDraftInput) AutonomousMemoryDraftPlan {
	now := input.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	workspaceSlug := workspaceMemorySlug(input.WorkspacePath, workspaceID)
	if workspaceID == "" {
		workspaceID = workspaceSlug
	}
	limit := input.Limit
	if limit <= 0 {
		limit = defaultMemoryDraftLimit
	}

	episodes := make(map[string]contextengine.RetrievalEpisode, len(input.Episodes))
	for _, episode := range input.Episodes {
		if strings.TrimSpace(episode.ID) != "" {
			episodes[episode.ID] = episode
		}
	}

	var plan AutonomousMemoryDraftPlan
	seen := map[string]struct{}{}
	for _, feedback := range input.Feedback {
		if len(plan.Drafts) >= limit {
			break
		}
		draft, ok := planMemoryDraftForFeedback(now, workspaceID, workspaceSlug, feedback, episodes[feedback.EpisodeID])
		if !ok {
			plan.Skipped++
			continue
		}
		if _, exists := seen[draft.DedupeKey]; exists {
			plan.Skipped++
			continue
		}
		seen[draft.DedupeKey] = struct{}{}
		plan.Drafts = append(plan.Drafts, draft)
	}
	return plan
}

// RunAutonomousMemoryDrafts performs one bounded, draft-only memory curation pass.
// It reads typed retrieval feedback, plans Obsidian inbox drafts, and optionally
// writes those inbox drafts plus prepared review proposals. It never merges into
// canonical vault notes.
func RunAutonomousMemoryDrafts(ctx context.Context, opts AutonomousMemoryDraftRunOptions) (AutonomousMemoryDraftRunReport, error) {
	now := opts.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	workspacePath := ws.Normalize(strings.TrimSpace(opts.WorkspacePath))
	workspaceID := strings.TrimSpace(opts.WorkspaceID)
	if workspaceID == "" && workspacePath != "" {
		workspaceID = ws.CanonicalID(workspacePath)
	}
	if workspaceID == "" {
		return AutonomousMemoryDraftRunReport{}, fmt.Errorf("workspace_id or workspace_path is required")
	}
	lookback := opts.Lookback
	if lookback <= 0 {
		lookback = 24 * time.Hour
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultMemoryDraftLimit
	}

	report := AutonomousMemoryDraftRunReport{
		Enabled:     true,
		ApplyDrafts: opts.ApplyDrafts && !opts.DryRun,
	}
	if opts.BlurWithAgent {
		report.BlurWithAgent = true
		report.BlurAgent = firstNonEmpty(strings.TrimSpace(opts.BlurAgentName), "agent")
	}
	ctxStore, err := contextstore.Open(ctx, opts.StorageRoot)
	if err != nil {
		return report, fmt.Errorf("open contextengine store: %w", err)
	}
	defer ctxStore.Close()

	feedback, err := ctxStore.ListRetrievalFeedback(ctx, contextengine.RetrievalFeedbackFilter{
		WorkspaceID: workspaceID,
		Kinds: []contextengine.RetrievalFeedbackKind{
			contextengine.RetrievalFeedbackKindAnswerCorrected,
			contextengine.RetrievalFeedbackKindRetrievalMissed,
			contextengine.RetrievalFeedbackKindWrongFileRetrieved,
			contextengine.RetrievalFeedbackKindStaleContextUsed,
			contextengine.RetrievalFeedbackKindGapCreated,
		},
		Since: now.Add(-lookback),
		Limit: limit * 4,
	})
	if err != nil {
		return report, fmt.Errorf("list retrieval feedback: %w", err)
	}
	report.FeedbackScanned = len(feedback)

	episodes := make([]contextengine.RetrievalEpisode, 0, len(feedback))
	seenEpisodes := map[string]struct{}{}
	for _, item := range feedback {
		episodeID := strings.TrimSpace(item.EpisodeID)
		if episodeID == "" {
			continue
		}
		if _, ok := seenEpisodes[episodeID]; ok {
			continue
		}
		seenEpisodes[episodeID] = struct{}{}
		episode, err := ctxStore.GetRetrievalEpisode(ctx, episodeID)
		if err != nil {
			continue
		}
		episodes = append(episodes, episode)
	}

	plan := PlanAutonomousMemoryDrafts(AutonomousMemoryDraftInput{
		WorkspaceID:   workspaceID,
		WorkspacePath: workspacePath,
		Now:           now,
		Feedback:      feedback,
		Episodes:      episodes,
		Limit:         limit,
	})
	if opts.BlurWithAgent {
		if opts.BlurAgent == nil {
			report.Errors++
			report.BlurErrors++
			report.ErrorMessages = append(report.ErrorMessages, "blur_with_agent requires a configured blur agent")
		} else {
			blurReport := blurAutonomousMemoryDraftPlan(ctx, opts.BlurAgent, firstNonEmpty(strings.TrimSpace(opts.BlurAgentName), "agent"), &plan)
			report.DraftsBlurred += blurReport.DraftsBlurred
			report.BlurErrors += blurReport.Errors
			report.BlurRejected += blurReport.Rejected
			report.Errors += blurReport.Errors
			report.ErrorMessages = append(report.ErrorMessages, blurReport.ErrorMessages...)
		}
	}
	report.DraftsPlanned = len(plan.Drafts)
	report.Skipped = plan.Skipped
	for _, draft := range plan.Drafts {
		report.DraftPaths = append(report.DraftPaths, draft.DraftPath)
	}
	if !report.ApplyDrafts || len(plan.Drafts) == 0 {
		return report, nil
	}
	vaultPath := strings.TrimSpace(opts.VaultPath)
	if vaultPath == "" {
		return report, fmt.Errorf("vault_path is required when apply_drafts is true")
	}
	if workspacePath == "" {
		workspacePath = workspaceID
	}
	writer := obsidiantool.NewWriter("", filepath.Base(vaultPath), obsidiantool.DefaultPolicy())
	writer.VaultPath = vaultPath
	store := NewWorkspaceStore(workspacePath)
	if _, err := store.EnsureLayout(); err != nil {
		return report, fmt.Errorf("ensure contextplane layout: %w", err)
	}
	for _, draft := range plan.Drafts {
		if err := writer.CreateNote(ctx, draft.DraftPath, draft.Content, true); err != nil {
			report.Errors++
			report.ErrorMessages = append(report.ErrorMessages, fmt.Sprintf("create %s: %v", draft.DraftPath, err))
			continue
		}
		report.DraftsWritten++
		if _, err := store.RecordMemoryProposal(ctx, draft.MemoryProposal()); err != nil {
			report.Errors++
			report.ErrorMessages = append(report.ErrorMessages, fmt.Sprintf("record proposal for %s: %v", draft.DraftPath, err))
			continue
		}
		report.ProposalsRecorded++
	}
	return report, nil
}

func planMemoryDraftForFeedback(now time.Time, workspaceID, workspaceSlug string, feedback contextengine.RetrievalFeedback, episode contextengine.RetrievalEpisode) (AutonomousMemoryDraft, bool) {
	statement, memoryKind, classification := memoryStatementFromFeedback(feedback)
	if strings.TrimSpace(statement) == "" {
		return AutonomousMemoryDraft{}, false
	}
	query := strings.TrimSpace(feedback.Query)
	if query == "" {
		query = strings.TrimSpace(episode.Query)
	}
	if query == "" || strings.TrimSpace(feedback.ID) == "" {
		return AutonomousMemoryDraft{}, false
	}

	createdAt := feedback.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = now
	}
	title := firstNonEmpty(titleForMemoryDraft(feedback.Kind, query), "Context memory draft")
	summary := fmt.Sprintf("%s: %s", classification, statement)
	dedupeKey := memoryDraftDedupeKey(workspaceID, feedback.Kind, query, statement)
	draftSlug := safeMemorySlug(title + " " + dedupeKey[len(dedupeKey)-12:])
	draftPath := filepath.ToSlash(filepath.Join("inbox/drafted-from-foxctl/memory", workspaceSlug, createdAt.Format("2006-01-02"), draftSlug+".md"))
	targetPath := filepath.ToSlash(filepath.Join("notes/memory", workspaceSlug+".md"))
	sourceRefs := []contextengine.EvidenceRef{
		{Type: contextengine.RefTypeEvent, Ref: "retrieval_feedback:" + feedback.ID},
	}
	if strings.TrimSpace(feedback.EpisodeID) != "" {
		sourceRefs = append(sourceRefs, contextengine.EvidenceRef{Type: contextengine.RefTypeEvent, Ref: "retrieval_episode:" + feedback.EpisodeID})
	}
	evidenceRefs := append([]contextengine.EvidenceRef(nil), feedback.UsedRefs...)
	if len(evidenceRefs) == 0 {
		evidenceRefs = append(evidenceRefs, sourceRefs...)
	}

	draft := AutonomousMemoryDraft{
		DedupeKey:     dedupeKey,
		WorkspaceID:   workspaceID,
		Title:         title,
		Summary:       summary,
		MemoryKind:    string(memoryKind),
		DraftPath:     draftPath,
		TargetPath:    targetPath,
		TargetHeading: "Review",
		SourceRefs:    sourceRefs,
		EvidenceRefs:  evidenceRefs,
		FeedbackID:    feedback.ID,
		EpisodeID:     feedback.EpisodeID,
		Query:         query,
		FeedbackKind:  string(feedback.Kind),
		CreatedAt:     createdAt,
		Statement:     statement,
	}
	draft.Content = renderAutonomousMemoryDraft(workspaceID, draft, statement)
	return draft, true
}

type autonomousMemoryDraftBlurReport struct {
	DraftsBlurred int
	Rejected      int
	Errors        int
	ErrorMessages []string
}

func blurAutonomousMemoryDraftPlan(ctx context.Context, agent MemoryBlurAgent, agentName string, plan *AutonomousMemoryDraftPlan) autonomousMemoryDraftBlurReport {
	var report autonomousMemoryDraftBlurReport
	if agent == nil || plan == nil {
		return report
	}
	agentName = firstNonEmpty(strings.TrimSpace(agentName), "agent")
	for i := range plan.Drafts {
		draft := &plan.Drafts[i]
		blur, ok, err := blurAutonomousMemoryDraft(ctx, agent, agentName, *draft)
		if err != nil {
			report.Errors++
			report.ErrorMessages = append(report.ErrorMessages, fmt.Sprintf("blur %s: %v", draft.DraftPath, err))
			continue
		}
		if !ok {
			report.Rejected++
			continue
		}
		draft.Blur = &blur
		draft.Content = renderAutonomousMemoryDraft(draft.WorkspaceID, *draft, draft.Statement)
		report.DraftsBlurred++
	}
	return report
}

func blurAutonomousMemoryDraft(ctx context.Context, agent MemoryBlurAgent, agentName string, draft AutonomousMemoryDraft) (AutonomousMemoryDraftBlur, bool, error) {
	input := MemoryBlurAgentPromptInput{
		ID:             "memory-draft:" + strings.TrimSpace(draft.DedupeKey),
		OriginalDomain: "contextengine:" + strings.TrimSpace(draft.FeedbackKind),
		Summary:        draft.Summary,
		LiteralText:    memoryDraftBlurLiteralText(draft),
		Shape:          memoryDraftBlurShape(draft),
		SourceRefs:     compactEvidenceRefs(append(append([]contextengine.EvidenceRef(nil), draft.SourceRefs...), draft.EvidenceRefs...)),
		ForbiddenTerms: memoryDraftBlurForbiddenTerms(draft),
	}
	output, _, err := agent.BlurMemory(ctx, input)
	if err != nil {
		return AutonomousMemoryDraftBlur{}, false, err
	}
	validation := ValidateMemoryBlurAgentOutput(input, output)
	if !validation.Valid {
		return AutonomousMemoryDraftBlur{
			Agent:      agentName,
			Validation: validation,
			SourceRefs: input.SourceRefs,
		}, false, nil
	}
	return AutonomousMemoryDraftBlur{
		Agent:          agentName,
		AbstractSchema: output.AbstractSchema,
		MechanismTags:  output.MechanismTags,
		DomainsToAvoid: output.DomainsToAvoid,
		Confidence:     output.Confidence,
		LeakageRisk:    output.LeakageRisk,
		Validation:     validation,
		SourceRefs:     input.SourceRefs,
	}, true, nil
}

func memoryDraftBlurLiteralText(draft AutonomousMemoryDraft) string {
	var b strings.Builder
	writeBlurredLine(&b, "summary", draft.Summary)
	writeBlurredLine(&b, "proposed_memory", draft.Statement)
	writeBlurredLine(&b, "query", draft.Query)
	writeBlurredLine(&b, "feedback_kind", draft.FeedbackKind)
	for _, ref := range draft.EvidenceRefs {
		writeBlurredLine(&b, "evidence_ref", contextengine.FormatEvidenceRef(ref))
	}
	return strings.TrimSpace(b.String())
}

func memoryDraftBlurShape(draft AutonomousMemoryDraft) MemoryStructuralShape {
	shape := MemoryStructuralShape{
		Mechanism:   "retrieval feedback correction loop",
		Actors:      []string{"retrieval request", "evidence set", "correction signal", "memory draft"},
		Operations:  []string{"observe retrieval outcome", "compare outcome with correction signal", "prepare review-gated memory"},
		Flows:       []string{"query to evidence set", "feedback signal to draft proposal", "draft proposal to review queue"},
		Constraints: []string{"review-gated persistence", "evidence-bound provenance"},
		Signals:     []string{"typed feedback class", "evidence reference count", "source event count"},
	}
	switch contextengine.RetrievalFeedbackKind(strings.TrimSpace(draft.FeedbackKind)) {
	case contextengine.RetrievalFeedbackKindAnswerCorrected:
		shape.Operations = append(shape.Operations, "preserve correction as candidate semantic memory")
	case contextengine.RetrievalFeedbackKindGapCreated:
		shape.Operations = append(shape.Operations, "capture missing context as candidate semantic memory")
	case contextengine.RetrievalFeedbackKindRetrievalMissed:
		shape.Operations = append(shape.Operations, "mark missing evidence path as episodic trace")
	case contextengine.RetrievalFeedbackKindWrongFileRetrieved:
		shape.Operations = append(shape.Operations, "separate incorrect evidence from requested context")
	case contextengine.RetrievalFeedbackKindStaleContextUsed:
		shape.Operations = append(shape.Operations, "identify stale evidence and preserve freshness constraint")
	}
	shape.Graph = &MemoryGraphShape{
		NodeKind: "memory_draft",
		Outgoing: map[string]int{
			"EVIDENCE_REF": len(draft.EvidenceRefs),
			"SOURCE_REF":   len(draft.SourceRefs),
		},
		Incoming: map[string]int{
			"FEEDBACK_SIGNAL": 1,
		},
		NeighborMix: len(draft.EvidenceRefs) + len(draft.SourceRefs),
	}
	return shape
}

func memoryDraftBlurForbiddenTerms(draft AutonomousMemoryDraft) []string {
	values := []string{
		draft.Query,
		draft.DedupeKey,
		draft.FeedbackID,
		draft.EpisodeID,
		draft.DraftPath,
		draft.TargetPath,
	}
	for _, ref := range append(append([]contextengine.EvidenceRef(nil), draft.SourceRefs...), draft.EvidenceRefs...) {
		values = append(values, contextengine.FormatEvidenceRef(ref), ref.Ref, ref.Title)
	}
	return compactStringsInOrder(values)
}

func memoryStatementFromFeedback(feedback contextengine.RetrievalFeedback) (statement string, kind memorycore.Kind, classification string) {
	switch feedback.Kind {
	case contextengine.RetrievalFeedbackKindAnswerCorrected:
		return strings.TrimSpace(feedback.CorrectionStmt), memorycore.KindSemanticFact, "Retrieval correction"
	case contextengine.RetrievalFeedbackKindGapCreated:
		return strings.TrimSpace(feedback.GapStmt), memorycore.KindSemanticFact, "Retrieval gap"
	case contextengine.RetrievalFeedbackKindRetrievalMissed:
		return firstNonEmpty(strings.TrimSpace(feedback.GapStmt), "Retrieval missed relevant context for this query."), memorycore.KindEpisodicTrace, "Retrieval miss"
	case contextengine.RetrievalFeedbackKindWrongFileRetrieved:
		return firstNonEmpty(strings.TrimSpace(feedback.CorrectionStmt), "Retrieved evidence pointed at the wrong file for this query."), memorycore.KindEpisodicTrace, "Wrong retrieval"
	case contextengine.RetrievalFeedbackKindStaleContextUsed:
		return firstNonEmpty(strings.TrimSpace(feedback.CorrectionStmt), "Retrieved evidence was stale for this query."), memorycore.KindSemanticFact, "Stale context"
	default:
		return "", "", ""
	}
}

func titleForMemoryDraft(kind contextengine.RetrievalFeedbackKind, query string) string {
	prefix := "Memory draft"
	switch kind {
	case contextengine.RetrievalFeedbackKindAnswerCorrected:
		prefix = "Retrieval correction"
	case contextengine.RetrievalFeedbackKindGapCreated:
		prefix = "Retrieval gap"
	case contextengine.RetrievalFeedbackKindRetrievalMissed:
		prefix = "Retrieval miss"
	case contextengine.RetrievalFeedbackKindWrongFileRetrieved:
		prefix = "Wrong retrieval"
	case contextengine.RetrievalFeedbackKindStaleContextUsed:
		prefix = "Stale context"
	}
	return prefix + ": " + truncatePlain(query, 72)
}

func renderAutonomousMemoryDraft(workspaceID string, draft AutonomousMemoryDraft, statement string) string {
	var b strings.Builder
	writeMemoryFrontmatter(&b, workspaceID, draft)
	b.WriteString("# ")
	b.WriteString(draft.Title)
	b.WriteString("\n\n")
	b.WriteString("## Summary\n")
	b.WriteString(draft.Summary)
	b.WriteString("\n\n")
	b.WriteString("## Proposed Memory\n")
	b.WriteString(statement)
	b.WriteString("\n\n")
	if draft.Blur != nil {
		b.WriteString("## Blurred Mechanism\n")
		b.WriteString(draft.Blur.AbstractSchema)
		b.WriteString("\n\n")
		if len(draft.Blur.MechanismTags) > 0 {
			b.WriteString("Mechanism tags: ")
			b.WriteString(strings.Join(draft.Blur.MechanismTags, ", "))
			b.WriteString("\n\n")
		}
		b.WriteString(fmt.Sprintf("Confidence: %.2f; leakage risk: %.2f; agent: `%s`\n\n", draft.Blur.Confidence, draft.Blur.LeakageRisk, draft.Blur.Agent))
	}
	b.WriteString("## Retrieval Evidence\n")
	b.WriteString("- Feedback: `")
	b.WriteString(draft.FeedbackID)
	b.WriteString("` (`")
	b.WriteString(draft.FeedbackKind)
	b.WriteString("`)\n")
	if strings.TrimSpace(draft.EpisodeID) != "" {
		b.WriteString("- Episode: `")
		b.WriteString(draft.EpisodeID)
		b.WriteString("`\n")
	}
	b.WriteString("- Query: ")
	b.WriteString(draft.Query)
	b.WriteString("\n")
	if len(draft.EvidenceRefs) > 0 {
		b.WriteString("\n## Evidence Refs\n")
		for _, ref := range draft.EvidenceRefs {
			b.WriteString("- `")
			b.WriteString(contextengine.FormatEvidenceRef(ref))
			b.WriteString("`\n")
		}
	}
	b.WriteString("\n## Review Notes\n")
	b.WriteString("Review before merging into the canonical memory note. Generated memories remain evidence-only until validated.\n")
	return b.String()
}

func writeMemoryFrontmatter(b *strings.Builder, workspaceID string, draft AutonomousMemoryDraft) {
	b.WriteString("---\n")
	writeYAMLString(b, "type", "memory")
	writeYAMLString(b, "status", "draft")
	writeYAMLString(b, "trust", "raw")
	writeYAMLString(b, "memory_kind", draft.MemoryKind)
	writeYAMLString(b, "lifecycle", string(memorycore.LifecycleStateCandidate))
	writeYAMLString(b, "review_status", string(memorycore.ReviewStatusNeedsReview))
	writeYAMLString(b, "source_lane", autonomousMemoryDraftSourceLane)
	writeYAMLString(b, "workspace_id", workspaceID)
	writeYAMLString(b, "dedupe_key", draft.DedupeKey)
	if draft.Blur != nil {
		writeYAMLString(b, "agent_blurred", "true")
		writeYAMLString(b, "blur_agent", draft.Blur.Agent)
		writeYAMLString(b, "blur_confidence", fmt.Sprintf("%.2f", draft.Blur.Confidence))
		writeYAMLStringSlice(b, "mechanism_tags", draft.Blur.MechanismTags)
	}
	writeYAMLString(b, "feedback_kind", draft.FeedbackKind)
	writeYAMLString(b, "feedback_id", draft.FeedbackID)
	writeYAMLString(b, "episode_id", draft.EpisodeID)
	writeYAMLString(b, "query", draft.Query)
	writeYAMLStringSlice(b, "source_refs", formatEvidenceRefs(draft.SourceRefs))
	writeYAMLStringSlice(b, "evidence_refs", formatEvidenceRefs(draft.EvidenceRefs))
	tags := []string{"foxctl/memory-draft", "foxctl/contextengine"}
	if draft.Blur != nil {
		tags = append(tags, "foxctl/agent-blurred")
	}
	writeYAMLStringSlice(b, "tags", tags)
	b.WriteString("---\n\n")
}

func writeYAMLString(b *strings.Builder, key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	b.WriteString(key)
	b.WriteString(": ")
	b.WriteString(fmt.Sprintf("%q", value))
	b.WriteString("\n")
}

func writeYAMLStringSlice(b *strings.Builder, key string, values []string) {
	values = compactStrings(values)
	if len(values) == 0 {
		return
	}
	b.WriteString(key)
	b.WriteString(":\n")
	for _, value := range values {
		b.WriteString("  - ")
		b.WriteString(fmt.Sprintf("%q", value))
		b.WriteString("\n")
	}
}

func formatEvidenceRefs(refs []contextengine.EvidenceRef) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		formatted := strings.TrimSpace(contextengine.FormatEvidenceRef(ref))
		if formatted != "" {
			out = append(out, formatted)
		}
	}
	return out
}

func memoryDraftDedupeKey(workspaceID string, kind contextengine.RetrievalFeedbackKind, query, statement string) string {
	hash := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(workspaceID),
		string(kind),
		strings.TrimSpace(query),
		strings.TrimSpace(statement),
	}, "\x00")))
	return "memory_draft:" + hex.EncodeToString(hash[:])
}

func workspaceMemorySlug(workspacePath, workspaceID string) string {
	base := strings.TrimSpace(workspacePath)
	if base != "" {
		base = filepath.Base(base)
	}
	if base == "" {
		base = strings.TrimSpace(workspaceID)
	}
	if slug := safeMemorySlug(base); slug != "" {
		return slug
	}
	return "workspace"
}

func safeMemorySlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func truncatePlain(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit])
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
