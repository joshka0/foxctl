package taskhistory

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	ws "github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/transcriptpipeline"
)

type TranscriptFamilyOverview struct {
	RequestedScope     TranscriptHistoryScope            `json:"requested_scope,omitempty"`
	AppliedScope       TranscriptHistoryScope            `json:"applied_scope,omitempty"`
	WorkspacePath      string                            `json:"workspace_path,omitempty"`
	FamilyPath         string                            `json:"family_path,omitempty"`
	FocusQuery         string                            `json:"focus_query,omitempty"`
	DateFrom           string                            `json:"date_from,omitempty"`
	DateTo             string                            `json:"date_to,omitempty"`
	SummaryProvider    string                            `json:"summary_provider,omitempty"`
	SummaryModel       string                            `json:"summary_model,omitempty"`
	SummaryModelID     string                            `json:"summary_model_id,omitempty"`
	SummaryMode        string                            `json:"summary_mode,omitempty"`
	Overview           string                            `json:"overview,omitempty"`
	CurrentFocus       []string                          `json:"current_focus,omitempty"`
	RecentChanges      []string                          `json:"recent_changes,omitempty"`
	TopLearnings       []string                          `json:"top_learnings,omitempty"`
	RecurringLearnings []string                          `json:"recurring_learnings,omitempty"`
	TopRisks           []string                          `json:"top_risks,omitempty"`
	TopSurprises       []string                          `json:"top_surprises,omitempty"`
	NextWork           []string                          `json:"next_work,omitempty"`
	RecurringMistakes  []string                          `json:"recurring_mistakes,omitempty"`
	AgentBrief         string                            `json:"agent_brief,omitempty"`
	HumanBrief         []string                          `json:"human_brief,omitempty"`
	SupportMetadata    []TranscriptFamilySupportMetadata `json:"support_metadata,omitempty"`
	SourceOwners       []string                          `json:"source_owners,omitempty"`
	SourceNames        []string                          `json:"source_names,omitempty"`
}

type TranscriptFamilySupportMetadata struct {
	Category        string   `json:"category,omitempty"`
	Text            string   `json:"text,omitempty"`
	OwnerCount      int      `json:"owner_count,omitempty"`
	LatestUpdatedAt string   `json:"latest_updated_at,omitempty"`
	LatestAgeDays   int      `json:"latest_age_days,omitempty"`
	SourceOwners    []string `json:"source_owners,omitempty"`
}

type transcriptOwnerSummary struct {
	OwnerPrefix string
	UpdatedAt   time.Time
	Pack        *transcriptpipeline.HistoryPack
	Support     transcriptSupportBundle
	SourceNames []string
}

type TranscriptHistoryDateRange struct {
	DateFrom string
	DateTo   string
	from     time.Time
	to       time.Time
}

type transcriptFamilyOverviewPayload struct {
	Overview           string   `json:"overview"`
	CurrentFocus       []string `json:"current_focus"`
	RecentChanges      []string `json:"recent_changes"`
	TopLearnings       []string `json:"top_learnings"`
	RecurringLearnings []string `json:"recurring_learnings"`
	TopRisks           []string `json:"top_risks"`
	TopSurprises       []string `json:"top_surprises"`
	NextWork           []string `json:"next_work"`
	RecurringMistakes  []string `json:"recurring_mistakes"`
	Brief              string   `json:"brief"`
}

const transcriptFamilyOverviewPromptV3 = `Summarize recent transcript history across one repo family.
Return only valid JSON:
{"overview":"...","current_focus":["..."],"recent_changes":["..."],"top_learnings":["..."],"recurring_learnings":["..."],"top_risks":["..."],"top_surprises":["..."],"next_work":["..."],"recurring_mistakes":["..."],"brief":"..."}

Rules:
- Use only the owner summaries provided; do not invent context.
- Treat the deterministic_* hint lists as the preferred shortlist backbone; only replace them when the owner summaries offer a clearly better agent-usable phrasing.
- Prefer the most recent owners when choosing current_focus, recent_changes, and next_work.
- Prefer cross-owner themes over one-off chatter.
- Cross-owner repeated themes can matter, but do not let older lanes overshadow the latest work.
- Keep current_focus and next_work concrete.
- Keep recent_changes to recent transitions or meaningful completed changes.
- Keep only items that remain useful to a later agent without opening the original transcript.
- Drop generic progress chatter, vague cleanup asks, and worktree/commit status text unless they encode a distinct change, risk, or learning.
- Reuse canonical wording from the owner summaries when possible; avoid paraphrasing the same learning twice.
- brief should be a concise multiline family overview.`

const transcriptFamilyOverviewListCleanupPromptV1 = `Clean and tighten transcript family overview shortlists.
Return only valid JSON:
{"current_focus":["..."],"recent_changes":["..."],"top_learnings":["..."],"recurring_learnings":["..."],"top_risks":["..."],"top_surprises":["..."],"next_work":["..."],"brief":"..."}

Rules:
- Work only with the provided shortlist items; do not invent new history.
- Preserve the first item in each non-empty list unless it is empty or pure formatting noise.
- Remove vague filler, generic progress chatter, and redundant paraphrases.
- Prefer items that would still help a later agent understand what changed, what matters, and what to do next.
- Keep at most 3 items per list.
- brief should be a concise multiline summary built from the cleaned lists.`

func ParseTranscriptHistoryDateRange(dateFrom, dateTo string) (TranscriptHistoryDateRange, error) {
	dateFrom = strings.TrimSpace(dateFrom)
	dateTo = strings.TrimSpace(dateTo)
	out := TranscriptHistoryDateRange{
		DateFrom: dateFrom,
		DateTo:   dateTo,
	}
	parseDay := func(label, raw string) (time.Time, error) {
		parsed, err := time.ParseInLocation("2006-01-02", raw, time.UTC)
		if err != nil {
			return time.Time{}, fmt.Errorf("%s must be in YYYY-MM-DD format", label)
		}
		return parsed.UTC(), nil
	}
	var err error
	if dateFrom != "" {
		out.from, err = parseDay("date-from", dateFrom)
		if err != nil {
			return TranscriptHistoryDateRange{}, err
		}
	}
	if dateTo != "" {
		out.to, err = parseDay("date-to", dateTo)
		if err != nil {
			return TranscriptHistoryDateRange{}, err
		}
		out.to = out.to.Add(24 * time.Hour)
	}
	if !out.from.IsZero() && !out.to.IsZero() && !out.from.Before(out.to) {
		return TranscriptHistoryDateRange{}, fmt.Errorf("date-from must be on or before date-to")
	}
	return out, nil
}

func (r TranscriptHistoryDateRange) Active() bool {
	return !r.from.IsZero() || !r.to.IsZero()
}

func (r TranscriptHistoryDateRange) Contains(ts time.Time) bool {
	if !r.Active() {
		return true
	}
	if ts.IsZero() {
		return false
	}
	if !r.from.IsZero() && ts.Before(r.from) {
		return false
	}
	if !r.to.IsZero() && !ts.Before(r.to) {
		return false
	}
	return true
}

func (c Collector) CollectTranscriptFamilyOverview(ctx context.Context, workspacePath string, scope TranscriptHistoryScope, ownerLimit int, focusQuery string, dateRange TranscriptHistoryDateRange) (*TranscriptFamilyOverview, error) {
	if c.MemoryStore == nil {
		return nil, nil
	}
	workspacePath = ws.Normalize(strings.TrimSpace(workspacePath))
	familyPath := ws.FamilyPath(workspacePath)
	focusQuery = strings.TrimSpace(focusQuery)
	if workspacePath == "" {
		return nil, nil
	}
	if ownerLimit <= 0 {
		ownerLimit = 6
	}
	scope = normalizeTranscriptHistoryScope(scope)
	searchScopes := []TranscriptHistoryScope{scope}
	if scope == TranscriptHistoryScopeAuto {
		searchScopes = []TranscriptHistoryScope{TranscriptHistoryScopeWorkspace}
		if familyPath != "" && ws.Normalize(familyPath) != ws.Normalize(workspacePath) {
			searchScopes = append(searchScopes, TranscriptHistoryScopeFamily)
		}
	}

	var selectedScope TranscriptHistoryScope
	var owners []string
	for _, candidateScope := range searchScopes {
		if focusQuery != "" {
			owners = c.searchTranscriptOwnerPrefixes(ctx, workspacePath, familyPath, focusQuery, candidateScope, ownerLimit, dateRange)
		} else {
			owners = c.recentTranscriptOwnerPrefixes(ctx, workspacePath, familyPath, candidateScope, ownerLimit, dateRange)
		}
		if len(owners) == 0 && focusQuery != "" {
			owners = c.recentTranscriptOwnerPrefixes(ctx, workspacePath, familyPath, candidateScope, ownerLimit, dateRange)
		}
		if len(owners) == 0 {
			continue
		}
		selectedScope = candidateScope
		break
	}
	if len(owners) == 0 {
		return nil, nil
	}

	summaries := make([]transcriptOwnerSummary, 0, len(owners))
	for _, ownerPrefix := range owners {
		summary, ok := c.buildTranscriptOwnerSummary(ctx, workspacePath, familyPath, ownerPrefix, selectedScope, dateRange)
		if !ok {
			continue
		}
		summaries = append(summaries, summary)
	}
	if len(summaries) == 0 {
		return nil, nil
	}
	summaries = sortTranscriptOwnerSummariesByRecency(summaries)
	recurring := c.collectTranscriptRecurringMistakes(ctx, workspacePath, familyPath, selectedScope, 4, dateRange)
	recurringLearnings := c.collectTranscriptRecurringLearnings(ctx, workspacePath, familyPath, selectedScope, 4, dateRange)
	overview := deterministicTranscriptFamilyOverview(workspacePath, familyPath, scope, selectedScope, summaries, recurringLearnings, recurring)
	overview.FocusQuery = focusQuery
	overview.DateFrom = dateRange.DateFrom
	overview.DateTo = dateRange.DateTo
	overview = c.refineTranscriptFamilyOverview(ctx, summaries, overview)
	overview = c.cleanupTranscriptFamilyOverview(ctx, overview)
	if overview == nil {
		return nil, nil
	}
	overview.SupportMetadata = buildTranscriptFamilySupportMetadata(summaries, overview)
	return overview, nil
}

func (c Collector) searchTranscriptOwnerPrefixes(ctx context.Context, workspacePath, familyPath, query string, scope TranscriptHistoryScope, limit int, dateRange TranscriptHistoryDateRange) []string {
	if c.MemoryStore == nil || strings.TrimSpace(query) == "" || limit <= 0 {
		return nil
	}
	type ownerBucket struct {
		score     float64
		updatedAt time.Time
		seen      map[string]struct{}
	}
	owners := map[string]*ownerBucket{}
	addEntry := func(entry storage.NamedEntry, score float64) {
		entryTime := transcriptHistoryEntryTime(entry)
		if !dateRange.Contains(entryTime) {
			return
		}
		owner := transcriptHistoryOwnerPrefix(entry.Name)
		if owner == "" {
			return
		}
		bucket, ok := owners[owner]
		if !ok {
			bucket = &ownerBucket{seen: map[string]struct{}{}}
			owners[owner] = bucket
		}
		key := strings.TrimSpace(entry.Name) + "|" + strings.TrimSpace(entry.Workspace)
		if _, ok := bucket.seen[key]; ok {
			return
		}
		bucket.seen[key] = struct{}{}
		bucket.score += score + transcriptRecordEntryWeight(entry)
		if entryTime.After(bucket.updatedAt) {
			bucket.updatedAt = entryTime
		}
	}
	addScoredEntries := func(scored []storage.ScoredEntry) {
		for _, item := range scored {
			addEntry(item.Entry, item.Score)
		}
	}
	entryTypes := []string{"history_answer", "history_notable", "history_insight"}
	if c.SemanticProvider != nil {
		if vec, err := c.SemanticProvider.Embed(ctx, query); err == nil && len(vec) > 0 {
			for _, entryType := range entryTypes {
				if scored, err := c.MemoryStore.SearchSimilarByType(ctx, workspacePath, entryType, vec, limit*3); err == nil {
					addScoredEntries(filterTranscriptEntriesByScope(scored, workspacePath, familyPath, scope))
				}
			}
		}
	}
	addScoredEntries(c.lexicalHistoryEntries(ctx, workspacePath, familyPath, entryTypes, query, "", scope, limit*8))
	if len(owners) == 0 {
		return nil
	}
	type rankedOwner struct {
		prefix    string
		score     float64
		updatedAt time.Time
	}
	ranked := make([]rankedOwner, 0, len(owners))
	for prefix, bucket := range owners {
		ranked = append(ranked, rankedOwner{prefix: prefix, score: bucket.score, updatedAt: bucket.updatedAt})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		if !ranked[i].updatedAt.Equal(ranked[j].updatedAt) {
			return ranked[i].updatedAt.After(ranked[j].updatedAt)
		}
		return ranked[i].prefix < ranked[j].prefix
	})
	out := make([]string, 0, minInt(limit, len(ranked)))
	for _, item := range ranked {
		out = append(out, item.prefix)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func (c Collector) recentTranscriptOwnerPrefixes(ctx context.Context, workspacePath, familyPath string, scope TranscriptHistoryScope, limit int, dateRange TranscriptHistoryDateRange) []string {
	if c.MemoryStore == nil || limit <= 0 {
		return nil
	}
	entries, _, err := c.MemoryStore.ListFiltered(ctx, workspacePath, storage.MemoryListFilter{Types: []string{"history_answer"}}, 400, 0)
	if err != nil || len(entries) == 0 {
		return nil
	}
	out := make([]string, 0, limit)
	seen := make(map[string]struct{}, limit)
	for _, entry := range entries {
		if !matchesTranscriptHistoryScope(entry, workspacePath, familyPath, scope) {
			continue
		}
		if !dateRange.Contains(transcriptHistoryEntryTime(entry)) {
			continue
		}
		owner := transcriptHistoryOwnerPrefix(entry.Name)
		if owner == "" {
			continue
		}
		if _, ok := seen[owner]; ok {
			continue
		}
		seen[owner] = struct{}{}
		out = append(out, owner)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func (c Collector) buildTranscriptOwnerSummary(ctx context.Context, workspacePath, familyPath, ownerPrefix string, scope TranscriptHistoryScope, dateRange TranscriptHistoryDateRange) (transcriptOwnerSummary, bool) {
	answerEntries := c.ownerHistoryEntries(ctx, workspacePath, familyPath, []string{"history_answer"}, ownerPrefix, scope, len(transcriptpipeline.DefaultHistoryProfile().Questions)+4, dateRange)
	if len(answerEntries) == 0 {
		return transcriptOwnerSummary{}, false
	}
	answers := make([]transcriptpipeline.HistoryAnswer, 0, len(answerEntries))
	sourceNames := make([]string, 0, len(answerEntries))
	updatedAt := transcriptHistoryEntryTime(answerEntries[0])
	for _, entry := range answerEntries {
		answer, ok := historyAnswerFromMemoryEntry(entry)
		if !ok {
			continue
		}
		answers = append(answers, answer)
		sourceNames = append(sourceNames, entry.Name)
		if entryTime := transcriptHistoryEntryTime(entry); entryTime.After(updatedAt) {
			updatedAt = entryTime
		}
	}
	pack := transcriptpipeline.BuildHistoryPack(answers)
	if pack == nil {
		return transcriptOwnerSummary{}, false
	}
	query := strings.Join([]string{
		pack.CurrentObjective,
		strings.Join(pack.ContinueWith, " | "),
		strings.Join(pack.AcceptedLearnings, " | "),
	}, " ")
	support := c.collectTranscriptSupport(ctx, workspacePath, familyPath, query, ownerPrefix, scope, 6)
	sourceNames = appendUniqueLocalStrings(sourceNames, support.sourceNames, 8)
	return transcriptOwnerSummary{
		OwnerPrefix: ownerPrefix,
		UpdatedAt:   updatedAt,
		Pack:        pack,
		Support:     support,
		SourceNames: sourceNames,
	}, true
}

func deterministicTranscriptFamilyOverview(workspacePath, familyPath string, requested, applied TranscriptHistoryScope, summaries []transcriptOwnerSummary, recurringLearnings, recurring []string) *TranscriptFamilyOverview {
	out := &TranscriptFamilyOverview{
		RequestedScope:     requested,
		AppliedScope:       applied,
		WorkspacePath:      workspacePath,
		FamilyPath:         familyPath,
		SummaryMode:        "deterministic",
		RecurringLearnings: append([]string(nil), recurringLearnings...),
		RecurringMistakes:  append([]string(nil), recurring...),
	}
	for _, summary := range summaries {
		out.SourceOwners = appendUniqueLocalStrings(out.SourceOwners, []string{transcriptHistoryOwnerID(summary.OwnerPrefix)}, 8)
		out.SourceNames = appendUniqueLocalStrings(out.SourceNames, summary.SourceNames, 12)
	}
	out.CurrentFocus = rankTranscriptFamilyItems(summaries, 4, func(summary transcriptOwnerSummary) []string {
		if summary.Pack == nil {
			return nil
		}
		focus := localFirstNonEmpty(strings.TrimSpace(summary.Pack.ObjectiveLabel), strings.TrimSpace(summary.Pack.CurrentObjective))
		if strings.TrimSpace(focus) == "" {
			return nil
		}
		return []string{focus}
	})
	out.RecentChanges = rankTranscriptFamilyItems(summaries, 5, func(summary transcriptOwnerSummary) []string {
		if summary.Pack == nil {
			return nil
		}
		if strings.TrimSpace(summary.Pack.RecentEpisode) != "" {
			return []string{summary.Pack.RecentEpisode}
		}
		if len(summary.Pack.ContinueWith) > 0 {
			return []string{summary.Pack.ContinueWith[0]}
		}
		return nil
	})
	out.TopLearnings = rankTranscriptFamilyItems(summaries, 5, func(summary transcriptOwnerSummary) []string {
		if summary.Pack == nil {
			return append([]string(nil), summary.Support.recentLearnings...)
		}
		return append(append([]string(nil), summary.Pack.AcceptedLearnings...), summary.Support.recentLearnings...)
	})
	out.TopRisks = rankTranscriptFamilyItems(summaries, 5, func(summary transcriptOwnerSummary) []string {
		if summary.Pack == nil {
			return nil
		}
		return append(append([]string(nil), summary.Pack.WatchOutFor...), summary.Pack.Regressions...)
	})
	out.NextWork = rankTranscriptFamilyItems(summaries, 5, func(summary transcriptOwnerSummary) []string {
		if summary.Pack == nil {
			return nil
		}
		return append([]string(nil), summary.Pack.ContinueWith...)
	})
	out.TopSurprises = rankTranscriptFamilyItems(summaries, 4, func(summary transcriptOwnerSummary) []string {
		if summary.Pack == nil {
			return append([]string(nil), summary.Support.recentSurprises...)
		}
		return append(append([]string(nil), summary.Pack.RecentSurprises...), summary.Support.recentSurprises...)
	})
	cleanTranscriptFamilyOverviewLists(out)
	out.Overview = buildTranscriptFamilyOverviewSummary(out)
	out.AgentBrief = buildTranscriptFamilyOverviewBrief(out)
	out.HumanBrief = buildTranscriptFamilyHumanBrief(out)
	if out.Overview == "" &&
		len(out.CurrentFocus) == 0 &&
		len(out.RecentChanges) == 0 &&
		len(out.TopLearnings) == 0 &&
		len(out.TopRisks) == 0 &&
		len(out.TopSurprises) == 0 &&
		len(out.NextWork) == 0 &&
		len(out.RecurringMistakes) == 0 {
		return nil
	}
	return out
}

func (c Collector) refineTranscriptFamilyOverview(ctx context.Context, summaries []transcriptOwnerSummary, overview *TranscriptFamilyOverview) *TranscriptFamilyOverview {
	if overview == nil || c.TranscriptWorker == nil || len(summaries) == 0 {
		return overview
	}
	artifact := buildTranscriptFamilyOverviewArtifact(summaries, overview)
	if strings.TrimSpace(artifact) == "" {
		return overview
	}
	run := c.TranscriptRun
	if run == nil {
		run = transcriptpipeline.RunLLMTask
	}
	result, err := run(ctx, *c.TranscriptWorker, transcriptpipeline.Task{
		Stage:         transcriptpipeline.StageReview,
		InputKind:     "transcript_family_overview",
		PromptVersion: "transcript_family_overview_v3",
		SystemPrompt:  transcriptFamilyOverviewPromptV3,
		ArtifactText:  artifact,
		MaxTokens:     420,
	})
	if err != nil {
		return overview
	}
	payload, ok := parseTranscriptFamilyOverviewPayload(result.OutputText)
	if !ok {
		return overview
	}
	overview.SummaryProvider = strings.TrimSpace(c.TranscriptWorker.Provider)
	overview.SummaryModel = strings.TrimSpace(c.TranscriptWorker.Model)
	overview.SummaryModelID = strings.TrimSpace(result.ModelID)
	overview.SummaryMode = "llm"
	if strings.TrimSpace(payload.Overview) != "" {
		overview.Overview = strings.TrimSpace(payload.Overview)
	}
	if len(payload.CurrentFocus) > 0 {
		overview.CurrentFocus = mergeTranscriptFamilyOverviewItems(overview.CurrentFocus, payload.CurrentFocus, 4, 1)
	}
	if len(payload.RecentChanges) > 0 {
		overview.RecentChanges = mergeTranscriptFamilyOverviewItems(overview.RecentChanges, payload.RecentChanges, 5, 1)
	}
	if len(payload.TopLearnings) > 0 {
		overview.TopLearnings = mergeTranscriptFamilyOverviewItems(overview.TopLearnings, payload.TopLearnings, 5, 1)
	}
	if len(payload.RecurringLearnings) > 0 {
		overview.RecurringLearnings = mergeTranscriptFamilyOverviewItems(overview.RecurringLearnings, payload.RecurringLearnings, 4, 1)
	}
	if len(payload.TopRisks) > 0 {
		overview.TopRisks = mergeTranscriptFamilyOverviewItems(overview.TopRisks, payload.TopRisks, 5, 1)
	}
	if len(payload.TopSurprises) > 0 {
		overview.TopSurprises = mergeTranscriptFamilyOverviewItems(overview.TopSurprises, payload.TopSurprises, 4, 1)
	}
	if len(payload.NextWork) > 0 {
		overview.NextWork = mergeTranscriptFamilyOverviewItems(overview.NextWork, payload.NextWork, 5, 1)
	}
	if len(payload.RecurringMistakes) > 0 {
		overview.RecurringMistakes = mergeTranscriptFamilyOverviewItems(overview.RecurringMistakes, payload.RecurringMistakes, 4, 0)
	}
	if strings.TrimSpace(payload.Brief) != "" {
		overview.AgentBrief = strings.TrimSpace(payload.Brief)
	} else {
		overview.AgentBrief = buildTranscriptFamilyOverviewBrief(overview)
	}
	cleanTranscriptFamilyOverviewLists(overview)
	overview.HumanBrief = buildTranscriptFamilyHumanBrief(overview)
	return overview
}

func (c Collector) cleanupTranscriptFamilyOverview(ctx context.Context, overview *TranscriptFamilyOverview) *TranscriptFamilyOverview {
	if overview == nil || c.TranscriptWorker == nil {
		return overview
	}
	artifact := buildTranscriptFamilyOverviewListCleanupArtifact(overview)
	if strings.TrimSpace(artifact) == "" {
		return overview
	}
	run := c.TranscriptRun
	if run == nil {
		run = transcriptpipeline.RunLLMTask
	}
	result, err := run(ctx, *c.TranscriptWorker, transcriptpipeline.Task{
		Stage:         transcriptpipeline.StageReview,
		InputKind:     "transcript_family_overview_lists",
		PromptVersion: "transcript_family_overview_lists_v1",
		SystemPrompt:  transcriptFamilyOverviewListCleanupPromptV1,
		ArtifactText:  artifact,
		MaxTokens:     220,
	})
	if err != nil {
		return overview
	}
	payload, ok := parseTranscriptFamilyOverviewPayload(result.OutputText)
	if !ok {
		return overview
	}
	overview.SummaryProvider = strings.TrimSpace(c.TranscriptWorker.Provider)
	overview.SummaryModel = strings.TrimSpace(c.TranscriptWorker.Model)
	overview.SummaryModelID = strings.TrimSpace(result.ModelID)
	if overview.SummaryMode == "" || overview.SummaryMode == "deterministic" {
		overview.SummaryMode = "llm_cleanup"
	}
	if len(payload.CurrentFocus) > 0 {
		overview.CurrentFocus = mergeTranscriptFamilyOverviewItems(overview.CurrentFocus, payload.CurrentFocus, 3, 1)
	}
	if len(payload.RecentChanges) > 0 {
		overview.RecentChanges = mergeTranscriptFamilyOverviewItems(overview.RecentChanges, payload.RecentChanges, 3, 1)
	}
	if len(payload.TopLearnings) > 0 {
		overview.TopLearnings = mergeTranscriptFamilyOverviewItems(overview.TopLearnings, payload.TopLearnings, 3, 1)
	}
	if len(payload.RecurringLearnings) > 0 {
		overview.RecurringLearnings = mergeTranscriptFamilyOverviewItems(overview.RecurringLearnings, payload.RecurringLearnings, 3, 1)
	}
	if len(payload.TopRisks) > 0 {
		overview.TopRisks = mergeTranscriptFamilyOverviewItems(overview.TopRisks, payload.TopRisks, 3, 1)
	}
	if len(payload.TopSurprises) > 0 {
		overview.TopSurprises = mergeTranscriptFamilyOverviewItems(overview.TopSurprises, payload.TopSurprises, 3, 1)
	}
	if len(payload.NextWork) > 0 {
		overview.NextWork = mergeTranscriptFamilyOverviewItems(overview.NextWork, payload.NextWork, 3, 1)
	}
	if strings.TrimSpace(payload.Brief) != "" {
		overview.AgentBrief = strings.TrimSpace(payload.Brief)
	} else {
		overview.AgentBrief = buildTranscriptFamilyOverviewBrief(overview)
	}
	cleanTranscriptFamilyOverviewLists(overview)
	overview.Overview = buildTranscriptFamilyOverviewSummary(overview)
	overview.HumanBrief = buildTranscriptFamilyHumanBrief(overview)
	return overview
}

func buildTranscriptFamilyOverviewArtifact(summaries []transcriptOwnerSummary, overview *TranscriptFamilyOverview) string {
	var b strings.Builder
	newestUpdatedAt := transcriptOwnerNewestUpdatedAt(summaries)
	for idx, summary := range summaries {
		b.WriteString("owner ")
		b.WriteString(fmt.Sprintf("%d", idx+1))
		b.WriteString(": ")
		b.WriteString(transcriptHistoryOwnerID(summary.OwnerPrefix))
		b.WriteString("\nrecency_rank: ")
		b.WriteString(fmt.Sprintf("%d", idx+1))
		if summary.UpdatedAt.Unix() > 0 {
			b.WriteString("\nupdated_at: ")
			b.WriteString(summary.UpdatedAt.UTC().Format(time.RFC3339))
			if !newestUpdatedAt.IsZero() && !summary.UpdatedAt.After(newestUpdatedAt) {
				ageDays := int(newestUpdatedAt.Sub(summary.UpdatedAt).Hours() / 24)
				if ageDays < 0 {
					ageDays = 0
				}
				b.WriteString("\ndays_from_latest: ")
				b.WriteString(fmt.Sprintf("%d", ageDays))
			}
		}
		if summary.Pack != nil {
			if text := localFirstNonEmpty(summary.Pack.ObjectiveLabel, summary.Pack.CurrentObjective); strings.TrimSpace(text) != "" {
				b.WriteString("\nobjective: ")
				b.WriteString(strings.TrimSpace(text))
			}
			if len(summary.Pack.ContinueWith) > 0 {
				b.WriteString("\ncontinue_with: ")
				b.WriteString(strings.Join(summary.Pack.ContinueWith, " | "))
			}
			if len(summary.Pack.AcceptedLearnings) > 0 {
				b.WriteString("\naccepted_learnings: ")
				b.WriteString(strings.Join(summary.Pack.AcceptedLearnings, " | "))
			}
			if len(summary.Pack.WatchOutFor) > 0 {
				b.WriteString("\nwatch_out_for: ")
				b.WriteString(strings.Join(summary.Pack.WatchOutFor, " | "))
			}
			if len(summary.Pack.RecentSurprises) > 0 {
				b.WriteString("\nrecent_surprises: ")
				b.WriteString(strings.Join(summary.Pack.RecentSurprises, " | "))
			}
			if summary.Pack.RecentEpisode != "" {
				b.WriteString("\nrecent_episode: ")
				b.WriteString(summary.Pack.RecentEpisode)
			}
		}
		b.WriteString("\n\n")
	}
	appendTranscriptFamilyClusterHints(&b, "deterministic_current_focus", rankTranscriptFamilyItemClusters(summaries, 4, func(summary transcriptOwnerSummary) []string {
		if summary.Pack == nil {
			return nil
		}
		focus := localFirstNonEmpty(strings.TrimSpace(summary.Pack.ObjectiveLabel), strings.TrimSpace(summary.Pack.CurrentObjective))
		if strings.TrimSpace(focus) == "" {
			return nil
		}
		return []string{focus}
	}))
	appendTranscriptFamilyClusterHints(&b, "deterministic_top_learnings", rankTranscriptFamilyItemClusters(summaries, 4, func(summary transcriptOwnerSummary) []string {
		if summary.Pack == nil {
			return append([]string(nil), summary.Support.recentLearnings...)
		}
		return append(append([]string(nil), summary.Pack.AcceptedLearnings...), summary.Support.recentLearnings...)
	}))
	if overview != nil && len(overview.RecurringLearnings) > 0 {
		b.WriteString("\n\n")
		b.WriteString("recurring_learnings: ")
		b.WriteString(strings.Join(overview.RecurringLearnings, " | "))
	}
	appendTranscriptFamilyClusterHints(&b, "deterministic_top_risks", rankTranscriptFamilyItemClusters(summaries, 4, func(summary transcriptOwnerSummary) []string {
		if summary.Pack == nil {
			return nil
		}
		return append(append([]string(nil), summary.Pack.WatchOutFor...), summary.Pack.Regressions...)
	}))
	appendTranscriptFamilyClusterHints(&b, "deterministic_top_surprises", rankTranscriptFamilyItemClusters(summaries, 4, func(summary transcriptOwnerSummary) []string {
		if summary.Pack == nil {
			return append([]string(nil), summary.Support.recentSurprises...)
		}
		return append(append([]string(nil), summary.Pack.RecentSurprises...), summary.Support.recentSurprises...)
	}))
	appendTranscriptFamilyClusterHints(&b, "deterministic_next_work", rankTranscriptFamilyItemClusters(summaries, 4, func(summary transcriptOwnerSummary) []string {
		if summary.Pack == nil {
			return nil
		}
		return append([]string(nil), summary.Pack.ContinueWith...)
	}))
	if overview != nil && len(overview.RecurringMistakes) > 0 {
		b.WriteString("\n\n")
		b.WriteString("recurring_mistakes: ")
		b.WriteString(strings.Join(overview.RecurringMistakes, " | "))
	}
	return strings.TrimSpace(b.String())
}

func buildTranscriptFamilyOverviewListCleanupArtifact(overview *TranscriptFamilyOverview) string {
	if overview == nil {
		return ""
	}
	var b strings.Builder
	if len(overview.CurrentFocus) > 0 {
		b.WriteString("current_focus: ")
		b.WriteString(strings.Join(overview.CurrentFocus, " | "))
	}
	if len(overview.RecentChanges) > 0 {
		b.WriteString("\nrecent_changes: ")
		b.WriteString(strings.Join(overview.RecentChanges, " | "))
	}
	if len(overview.TopLearnings) > 0 {
		b.WriteString("\ntop_learnings: ")
		b.WriteString(strings.Join(overview.TopLearnings, " | "))
	}
	if len(overview.RecurringLearnings) > 0 {
		b.WriteString("\nrecurring_learnings: ")
		b.WriteString(strings.Join(overview.RecurringLearnings, " | "))
	}
	if len(overview.TopRisks) > 0 {
		b.WriteString("\ntop_risks: ")
		b.WriteString(strings.Join(overview.TopRisks, " | "))
	}
	if len(overview.TopSurprises) > 0 {
		b.WriteString("\ntop_surprises: ")
		b.WriteString(strings.Join(overview.TopSurprises, " | "))
	}
	if len(overview.NextWork) > 0 {
		b.WriteString("\nnext_work: ")
		b.WriteString(strings.Join(overview.NextWork, " | "))
	}
	return strings.TrimSpace(b.String())
}

type transcriptFamilyRankedItem struct {
	text      string
	score     float64
	updatedAt time.Time
	owners    map[string]struct{}
}

func sortTranscriptOwnerSummariesByRecency(summaries []transcriptOwnerSummary) []transcriptOwnerSummary {
	if len(summaries) <= 1 {
		return append([]transcriptOwnerSummary(nil), summaries...)
	}
	out := append([]transcriptOwnerSummary(nil), summaries...)
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].OwnerPrefix < out[j].OwnerPrefix
	})
	return out
}

func transcriptOwnerNewestUpdatedAt(summaries []transcriptOwnerSummary) time.Time {
	var newest time.Time
	for _, summary := range summaries {
		if summary.UpdatedAt.After(newest) {
			newest = summary.UpdatedAt
		}
	}
	return newest
}

func transcriptOwnerRecencyScores(summaries []transcriptOwnerSummary) map[string]float64 {
	sorted := sortTranscriptOwnerSummariesByRecency(summaries)
	newestUpdatedAt := transcriptOwnerNewestUpdatedAt(sorted)
	scores := make(map[string]float64, len(sorted))
	total := len(sorted)
	for idx, summary := range sorted {
		rankBoost := float64(total - idx)
		recencyBoost := transcriptOwnerRecencyScore(summary.UpdatedAt, newestUpdatedAt)
		score := rankBoost + recencyBoost
		if score < 1 {
			score = 1
		}
		scores[summary.OwnerPrefix] = score
	}
	return scores
}

func transcriptOwnerRecencyScore(updatedAt, newestUpdatedAt time.Time) float64 {
	if updatedAt.IsZero() || newestUpdatedAt.IsZero() {
		return 1
	}
	if updatedAt.After(newestUpdatedAt) {
		return 100
	}
	ageDays := newestUpdatedAt.Sub(updatedAt).Hours() / 24
	if ageDays <= 0 {
		return 100
	}
	return 100 / (1 + (ageDays / 7))
}

func rankTranscriptFamilyItems(summaries []transcriptOwnerSummary, limit int, items func(transcriptOwnerSummary) []string) []string {
	clusters := rankTranscriptFamilyItemClusters(summaries, limit, items)
	if len(clusters) == 0 {
		return nil
	}
	out := make([]string, 0, minInt(limit, len(clusters)))
	for _, item := range clusters {
		out = append(out, item.text)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func rankTranscriptFamilyItemClusters(summaries []transcriptOwnerSummary, limit int, items func(transcriptOwnerSummary) []string) []transcriptFamilyRankedItem {
	if limit <= 0 || len(summaries) == 0 {
		return nil
	}
	sorted := sortTranscriptOwnerSummariesByRecency(summaries)
	recencyScores := transcriptOwnerRecencyScores(sorted)
	ranked := make(map[string]*transcriptFamilyRankedItem, limit*2)
	for _, summary := range sorted {
		candidates := normalizeTranscriptSummaryList(items(summary), limit*3)
		if len(candidates) == 0 {
			continue
		}
		seenWithinOwner := make(map[string]struct{}, len(candidates))
		for idx, candidate := range candidates {
			key := normalizeRecurringText(candidate)
			if key == "" {
				continue
			}
			if _, ok := seenWithinOwner[key]; ok {
				continue
			}
			seenWithinOwner[key] = struct{}{}
			score := recencyScores[summary.OwnerPrefix] - float64(idx)
			if score < 0 {
				score = 0
			}
			cluster, ok := ranked[key]
			if !ok {
				cluster = &transcriptFamilyRankedItem{
					text:      candidate,
					score:     score,
					updatedAt: summary.UpdatedAt,
					owners:    map[string]struct{}{summary.OwnerPrefix: {}},
				}
				ranked[key] = cluster
				continue
			}
			cluster.score += score
			cluster.owners[summary.OwnerPrefix] = struct{}{}
			if summary.UpdatedAt.After(cluster.updatedAt) {
				cluster.updatedAt = summary.UpdatedAt
				cluster.text = candidate
			}
		}
	}
	if len(ranked) == 0 {
		return nil
	}
	ordered := make([]transcriptFamilyRankedItem, 0, len(ranked))
	for _, item := range ranked {
		item.score += transcriptFamilySupportBonus(len(item.owners))
		ordered = append(ordered, *item)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].score != ordered[j].score {
			return ordered[i].score > ordered[j].score
		}
		if len(ordered[i].owners) != len(ordered[j].owners) {
			return len(ordered[i].owners) > len(ordered[j].owners)
		}
		if !ordered[i].updatedAt.Equal(ordered[j].updatedAt) {
			return ordered[i].updatedAt.After(ordered[j].updatedAt)
		}
		return ordered[i].text < ordered[j].text
	})
	if len(ordered) > limit {
		return ordered[:limit]
	}
	return ordered
}

func transcriptFamilySupportBonus(ownerCount int) float64 {
	if ownerCount <= 1 {
		return 0
	}
	return float64(ownerCount-1) * 20
}

func appendTranscriptFamilyClusterHints(b *strings.Builder, label string, clusters []transcriptFamilyRankedItem) {
	if b == nil || strings.TrimSpace(label) == "" || len(clusters) == 0 {
		return
	}
	hints := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		text := strings.TrimSpace(cluster.text)
		if text == "" {
			continue
		}
		hints = append(hints, fmt.Sprintf("%s [owners=%d]", text, len(cluster.owners)))
	}
	if len(hints) == 0 {
		return
	}
	b.WriteString("\n\n")
	b.WriteString(label)
	b.WriteString(": ")
	b.WriteString(strings.Join(hints, " | "))
}

func parseTranscriptFamilyOverviewPayload(raw string) (transcriptFamilyOverviewPayload, bool) {
	var payload transcriptFamilyOverviewPayload
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &payload); err != nil {
		return transcriptFamilyOverviewPayload{}, false
	}
	payload.Overview = strings.TrimSpace(payload.Overview)
	payload.CurrentFocus = normalizeTranscriptSummaryList(payload.CurrentFocus, 4)
	payload.RecentChanges = normalizeTranscriptSummaryList(payload.RecentChanges, 5)
	payload.TopLearnings = normalizeTranscriptSummaryList(payload.TopLearnings, 5)
	payload.RecurringLearnings = normalizeTranscriptSummaryList(payload.RecurringLearnings, 4)
	payload.TopRisks = normalizeTranscriptSummaryList(payload.TopRisks, 5)
	payload.TopSurprises = normalizeTranscriptSummaryList(payload.TopSurprises, 4)
	payload.NextWork = normalizeTranscriptSummaryList(payload.NextWork, 5)
	payload.RecurringMistakes = normalizeTranscriptSummaryList(payload.RecurringMistakes, 4)
	payload.Brief = strings.TrimSpace(payload.Brief)
	return payload, true
}

func mergeTranscriptFamilyOverviewItems(existing, refined []string, limit int, preserve int) []string {
	base := normalizeTranscriptSummaryList(existing, limit)
	incoming := normalizeTranscriptSummaryList(refined, limit)
	if len(base) == 0 {
		return incoming
	}
	if len(incoming) == 0 {
		return base
	}
	if preserve < 0 {
		preserve = 0
	}
	if preserve > len(base) {
		preserve = len(base)
	}
	out := append([]string(nil), base[:preserve]...)
	for _, item := range incoming {
		if transcriptFamilyOverviewContainsNormalized(out, item) {
			continue
		}
		out = append(out, item)
		if limit > 0 && len(out) >= limit {
			return out[:limit]
		}
	}
	return out
}

func transcriptFamilyOverviewContainsNormalized(items []string, target string) bool {
	target = normalizeHistoryRetrievalText(target)
	if target == "" {
		return false
	}
	for _, item := range items {
		if normalizeHistoryRetrievalText(item) == target {
			return true
		}
	}
	return false
}

func cleanTranscriptFamilyOverviewLists(overview *TranscriptFamilyOverview) {
	if overview == nil {
		return
	}
	overview.CurrentFocus = filterTranscriptFamilyOverviewItems(overview.CurrentFocus, 4)
	overview.RecentChanges = filterTranscriptFamilyOverviewItems(overview.RecentChanges, 4)
	overview.TopLearnings = filterTranscriptFamilyOverviewItems(overview.TopLearnings, 4)
	overview.RecurringLearnings = filterTranscriptFamilyOverviewItems(overview.RecurringLearnings, 4)
	overview.TopRisks = filterTranscriptFamilyOverviewItems(overview.TopRisks, 4)
	overview.TopSurprises = filterTranscriptFamilyOverviewItems(overview.TopSurprises, 4)
	overview.NextWork = filterTranscriptFamilyOverviewItems(overview.NextWork, 4)
	overview.RecurringMistakes = filterTranscriptFamilyOverviewItems(overview.RecurringMistakes, 4)
}

func buildTranscriptFamilySupportMetadata(summaries []transcriptOwnerSummary, overview *TranscriptFamilyOverview) []TranscriptFamilySupportMetadata {
	if overview == nil || len(summaries) == 0 {
		return nil
	}
	newestUpdatedAt := transcriptOwnerNewestUpdatedAt(summaries)
	out := make([]TranscriptFamilySupportMetadata, 0, 12)
	appendCategory := func(category string, items []string) {
		startLen := len(out)
		for _, item := range items {
			meta, ok := transcriptFamilySupportMetadataForItem(category, item, summaries, newestUpdatedAt)
			if !ok {
				continue
			}
			out = append(out, meta)
		}
		if len(out) > startLen {
			return
		}
		for _, cluster := range transcriptFamilyCategoryClusters(summaries, category, 2) {
			out = append(out, transcriptFamilySupportMetadataFromCluster(category, cluster, newestUpdatedAt))
		}
	}
	appendCategory("current_focus", overview.CurrentFocus)
	appendCategory("recent_changes", overview.RecentChanges)
	appendCategory("top_learnings", overview.TopLearnings)
	appendCategory("recurring_learnings", overview.RecurringLearnings)
	appendCategory("top_risks", overview.TopRisks)
	appendCategory("top_surprises", overview.TopSurprises)
	appendCategory("next_work", overview.NextWork)
	appendCategory("recurring_mistakes", overview.RecurringMistakes)
	return out
}

func transcriptFamilySupportMetadataForItem(category, item string, summaries []transcriptOwnerSummary, newestUpdatedAt time.Time) (TranscriptFamilySupportMetadata, bool) {
	item = summarizeTranscriptSummary(item)
	if strings.TrimSpace(category) == "" || item == "" {
		return TranscriptFamilySupportMetadata{}, false
	}
	sourceOwners := make([]string, 0, len(summaries))
	latestUpdatedAt := time.Time{}
	for _, summary := range summaries {
		candidates := transcriptFamilyCategoryItems(summary, category)
		if len(candidates) == 0 {
			continue
		}
		if !transcriptFamilyItemMatchesCandidates(item, candidates) {
			continue
		}
		sourceOwners = appendUniqueLocalStrings(sourceOwners, []string{transcriptHistoryOwnerID(summary.OwnerPrefix)}, 8)
		if summary.UpdatedAt.After(latestUpdatedAt) {
			latestUpdatedAt = summary.UpdatedAt
		}
	}
	if len(sourceOwners) == 0 {
		return TranscriptFamilySupportMetadata{}, false
	}
	meta := TranscriptFamilySupportMetadata{
		Category:     category,
		Text:         item,
		OwnerCount:   len(sourceOwners),
		SourceOwners: sourceOwners,
	}
	if !latestUpdatedAt.IsZero() {
		meta.LatestUpdatedAt = latestUpdatedAt.UTC().Format(time.RFC3339)
		if !newestUpdatedAt.IsZero() && !latestUpdatedAt.After(newestUpdatedAt) {
			ageDays := int(newestUpdatedAt.Sub(latestUpdatedAt).Hours() / 24)
			if ageDays < 0 {
				ageDays = 0
			}
			meta.LatestAgeDays = ageDays
		}
	}
	return meta, true
}

func transcriptFamilySupportMetadataFromCluster(category string, cluster transcriptFamilyRankedItem, newestUpdatedAt time.Time) TranscriptFamilySupportMetadata {
	sourceOwners := make([]string, 0, len(cluster.owners))
	for owner := range cluster.owners {
		sourceOwners = append(sourceOwners, transcriptHistoryOwnerID(owner))
	}
	sort.Strings(sourceOwners)
	meta := TranscriptFamilySupportMetadata{
		Category:     category,
		Text:         cluster.text,
		OwnerCount:   len(sourceOwners),
		SourceOwners: sourceOwners,
	}
	if !cluster.updatedAt.IsZero() {
		meta.LatestUpdatedAt = cluster.updatedAt.UTC().Format(time.RFC3339)
		if !newestUpdatedAt.IsZero() && !cluster.updatedAt.After(newestUpdatedAt) {
			ageDays := int(newestUpdatedAt.Sub(cluster.updatedAt).Hours() / 24)
			if ageDays < 0 {
				ageDays = 0
			}
			meta.LatestAgeDays = ageDays
		}
	}
	return meta
}

func transcriptFamilyCategoryItems(summary transcriptOwnerSummary, category string) []string {
	switch category {
	case "current_focus":
		if summary.Pack == nil {
			return nil
		}
		focus := localFirstNonEmpty(strings.TrimSpace(summary.Pack.ObjectiveLabel), strings.TrimSpace(summary.Pack.CurrentObjective))
		if focus == "" {
			return nil
		}
		return []string{focus}
	case "recent_changes":
		if summary.Pack == nil {
			return nil
		}
		if strings.TrimSpace(summary.Pack.RecentEpisode) != "" {
			return []string{summary.Pack.RecentEpisode}
		}
		if len(summary.Pack.ContinueWith) > 0 {
			return []string{summary.Pack.ContinueWith[0]}
		}
		return nil
	case "top_learnings", "recurring_learnings":
		if summary.Pack == nil {
			return append([]string(nil), summary.Support.recentLearnings...)
		}
		return append(append([]string(nil), summary.Pack.AcceptedLearnings...), summary.Support.recentLearnings...)
	case "top_risks", "recurring_mistakes":
		if summary.Pack == nil {
			return nil
		}
		return append(append([]string(nil), summary.Pack.WatchOutFor...), summary.Pack.Regressions...)
	case "top_surprises":
		if summary.Pack == nil {
			return append([]string(nil), summary.Support.recentSurprises...)
		}
		return append(append([]string(nil), summary.Pack.RecentSurprises...), summary.Support.recentSurprises...)
	case "next_work":
		if summary.Pack == nil {
			return nil
		}
		return append([]string(nil), summary.Pack.ContinueWith...)
	default:
		return nil
	}
}

func transcriptFamilyCategoryClusters(summaries []transcriptOwnerSummary, category string, limit int) []transcriptFamilyRankedItem {
	switch category {
	case "current_focus":
		return rankTranscriptFamilyItemClusters(summaries, limit, func(summary transcriptOwnerSummary) []string {
			if summary.Pack == nil {
				return nil
			}
			focus := localFirstNonEmpty(strings.TrimSpace(summary.Pack.ObjectiveLabel), strings.TrimSpace(summary.Pack.CurrentObjective))
			if focus == "" {
				return nil
			}
			return []string{focus}
		})
	case "recent_changes":
		return rankTranscriptFamilyItemClusters(summaries, limit, func(summary transcriptOwnerSummary) []string {
			if summary.Pack == nil {
				return nil
			}
			if strings.TrimSpace(summary.Pack.RecentEpisode) != "" {
				return []string{summary.Pack.RecentEpisode}
			}
			if len(summary.Pack.ContinueWith) > 0 {
				return []string{summary.Pack.ContinueWith[0]}
			}
			return nil
		})
	case "top_learnings", "recurring_learnings":
		return rankTranscriptFamilyItemClusters(summaries, limit, func(summary transcriptOwnerSummary) []string {
			if summary.Pack == nil {
				return append([]string(nil), summary.Support.recentLearnings...)
			}
			return append(append([]string(nil), summary.Pack.AcceptedLearnings...), summary.Support.recentLearnings...)
		})
	case "top_risks", "recurring_mistakes":
		return rankTranscriptFamilyItemClusters(summaries, limit, func(summary transcriptOwnerSummary) []string {
			if summary.Pack == nil {
				return nil
			}
			return append(append([]string(nil), summary.Pack.WatchOutFor...), summary.Pack.Regressions...)
		})
	case "top_surprises":
		return rankTranscriptFamilyItemClusters(summaries, limit, func(summary transcriptOwnerSummary) []string {
			if summary.Pack == nil {
				return append([]string(nil), summary.Support.recentSurprises...)
			}
			return append(append([]string(nil), summary.Pack.RecentSurprises...), summary.Support.recentSurprises...)
		})
	case "next_work":
		return rankTranscriptFamilyItemClusters(summaries, limit, func(summary transcriptOwnerSummary) []string {
			if summary.Pack == nil {
				return nil
			}
			return append([]string(nil), summary.Pack.ContinueWith...)
		})
	default:
		return nil
	}
}

func transcriptFamilyItemMatchesCandidates(item string, candidates []string) bool {
	itemNorm := normalizeHistoryRetrievalText(item)
	for _, candidate := range normalizeTranscriptSummaryList(candidates, len(candidates)) {
		candidateNorm := normalizeHistoryRetrievalText(candidate)
		if itemNorm != "" && itemNorm == candidateNorm {
			return true
		}
		if recurringTranscriptSimilarity(item, candidate) >= 0.5 {
			return true
		}
	}
	return false
}

func filterTranscriptFamilyOverviewItems(items []string, limit int) []string {
	normalizeLimit := len(items)
	if limit*2 > normalizeLimit {
		normalizeLimit = limit * 2
	}
	normalized := normalizeTranscriptSummaryList(items, normalizeLimit)
	if len(normalized) == 0 {
		return nil
	}
	out := make([]string, 0, minInt(limit, len(normalized)))
	for _, item := range normalized {
		if transcriptFamilyOverviewItemIsFragmentary(item) {
			continue
		}
		out = append(out, item)
		if len(out) >= limit {
			break
		}
	}
	if len(out) == 0 {
		return shortenStrings(normalized, limit)
	}
	return out
}

func transcriptFamilyOverviewItemIsFragmentary(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return true
	}
	return strings.HasSuffix(text, ":") || strings.HasSuffix(text, "...") || strings.HasSuffix(text, "…")
}

func transcriptHistoryOwnerID(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	prefix = strings.TrimPrefix(prefix, "transcript-history:")
	prefix = strings.TrimSuffix(prefix, ":")
	return strings.TrimSpace(prefix)
}

func buildTranscriptFamilyOverviewSummary(overview *TranscriptFamilyOverview) string {
	if overview == nil {
		return ""
	}
	parts := make([]string, 0, 3)
	if len(overview.CurrentFocus) > 0 {
		parts = append(parts, "Focus: "+overview.CurrentFocus[0])
	}
	if len(overview.TopLearnings) > 0 {
		parts = append(parts, "Learning: "+overview.TopLearnings[0])
	} else if len(overview.RecurringLearnings) > 0 {
		parts = append(parts, "Recurring learning: "+overview.RecurringLearnings[0])
	}
	if len(overview.TopRisks) > 0 {
		parts = append(parts, "Risk: "+overview.TopRisks[0])
	}
	return strings.Join(parts, " | ")
}

func buildTranscriptFamilyOverviewBrief(overview *TranscriptFamilyOverview) string {
	if overview == nil {
		return ""
	}
	lines := make([]string, 0, 6)
	if len(overview.CurrentFocus) > 0 {
		lines = append(lines, "Current focus: "+strings.Join(shortenStrings(overview.CurrentFocus, 3), " | "))
	}
	if len(overview.RecentChanges) > 0 {
		lines = append(lines, "Recent changes: "+strings.Join(shortenStrings(overview.RecentChanges, 3), " | "))
	}
	if len(overview.TopLearnings) > 0 {
		lines = append(lines, "Top learnings: "+strings.Join(shortenStrings(overview.TopLearnings, 3), " | "))
	}
	if len(overview.RecurringLearnings) > 0 {
		lines = append(lines, "Recurring learnings: "+strings.Join(shortenStrings(overview.RecurringLearnings, 3), " | "))
	}
	if len(overview.TopRisks) > 0 {
		lines = append(lines, "Top risks: "+strings.Join(shortenStrings(overview.TopRisks, 3), " | "))
	}
	if len(overview.NextWork) > 0 {
		lines = append(lines, "Next work: "+strings.Join(shortenStrings(overview.NextWork, 3), " | "))
	}
	if len(overview.RecurringMistakes) > 0 {
		lines = append(lines, "Recurring mistakes: "+strings.Join(shortenStrings(overview.RecurringMistakes, 3), " | "))
	}
	return strings.Join(lines, "\n")
}

func buildTranscriptFamilyHumanBrief(overview *TranscriptFamilyOverview) []string {
	if overview == nil {
		return nil
	}
	out := make([]string, 0, 6)
	if len(overview.CurrentFocus) > 0 {
		out = append(out, "Current focus: "+strings.Join(overview.CurrentFocus, " | "))
	}
	if len(overview.RecentChanges) > 0 {
		out = append(out, "Recent changes: "+strings.Join(overview.RecentChanges, " | "))
	}
	if len(overview.TopLearnings) > 0 {
		out = append(out, "Top learnings: "+strings.Join(overview.TopLearnings, " | "))
	}
	if len(overview.RecurringLearnings) > 0 {
		out = append(out, "Recurring learnings: "+strings.Join(overview.RecurringLearnings, " | "))
	}
	if len(overview.TopRisks) > 0 {
		out = append(out, "Top risks: "+strings.Join(overview.TopRisks, " | "))
	}
	if len(overview.TopSurprises) > 0 {
		out = append(out, "Top surprises: "+strings.Join(overview.TopSurprises, " | "))
	}
	if len(overview.NextWork) > 0 {
		out = append(out, "Next work: "+strings.Join(overview.NextWork, " | "))
	}
	if len(overview.RecurringMistakes) > 0 {
		out = append(out, "Recurring mistakes: "+strings.Join(overview.RecurringMistakes, " | "))
	}
	return out
}

func localFirstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
