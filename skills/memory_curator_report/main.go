// Package main implements the memory/curator_report skill.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/mathutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/context/memorycore"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/joshka0/foxctl/internal/storage"
	contextstore "github.com/joshka0/foxctl/internal/storage/contextengine"
	"github.com/joshka0/foxctl/internal/storage/memory"
)

const (
	command      = "memory/curator_report"
	defaultLimit = 1000
	maxLimit     = 5000
)

type Mode string

const (
	ModeDryRun Mode = "dry_run"
	ModeApply  Mode = "apply"
)

type Input struct {
	Workspace                    string `json:"workspace,omitempty"`
	Mode                         string `json:"mode,omitempty"`
	ConfirmApply                 bool   `json:"confirm_apply,omitempty"`
	PersistReport                bool   `json:"persist_report,omitempty"`
	Limit                        int    `json:"limit,omitempty"`
	StaleAfterDays               int    `json:"stale_after_days,omitempty"`
	ArchiveAfterDays             int    `json:"archive_after_days,omitempty"`
	RevalidateAfterDays          int    `json:"revalidate_after_days,omitempty"`
	RevalidateEnvClaimsAfterDays int    `json:"revalidate_env_claims_after_days,omitempty"`
	MinUsesBeforeUtilityJudgment int    `json:"min_uses_before_utility_judgment,omitempty"`
}

type Output struct {
	Status             string                   `json:"status"`
	Message            string                   `json:"message"`
	Workspace          string                   `json:"workspace"`
	Mode               Mode                     `json:"mode"`
	Report             memorycore.CuratorReport `json:"report"`
	Apply              *ApplyReport             `json:"apply,omitempty"`
	ReportArtifact     string                   `json:"report_artifact,omitempty"`
	CASHint            *envelope.CASHint        `json:"cas_hint,omitempty"`
	SavedReportName    string                   `json:"saved_report_name,omitempty"`
	SourceCounts       map[string]int           `json:"source_counts,omitempty"`
	UnavailableSources []string                 `json:"unavailable_sources,omitempty"`
}

type ApplyStatus string

const (
	ApplyStatusApplied ApplyStatus = "applied"
	ApplyStatusSkipped ApplyStatus = "skipped"
	ApplyStatusFailed  ApplyStatus = "failed"
)

type ApplySummary struct {
	Attempted         int `json:"attempted"`
	Applied           int `json:"applied"`
	Skipped           int `json:"skipped"`
	Failed            int `json:"failed"`
	UnsupportedSource int `json:"unsupported_source"`
	UnsupportedAction int `json:"unsupported_action"`
}

type ApplyResult struct {
	RecordID        string                    `json:"record_id"`
	SourceLane      memorycore.SourceLane     `json:"source_lane"`
	Action          memorycore.CuratorAction  `json:"action"`
	Status          ApplyStatus               `json:"status"`
	Reason          string                    `json:"reason,omitempty"`
	FromState       memorycore.LifecycleState `json:"from_state,omitempty"`
	ToState         memorycore.LifecycleState `json:"to_state,omitempty"`
	FromClaimStatus contextengine.ClaimStatus `json:"from_claim_status,omitempty"`
	ToClaimStatus   contextengine.ClaimStatus `json:"to_claim_status,omitempty"`
}

type ApplyReport struct {
	Summary ApplySummary  `json:"summary"`
	Results []ApplyResult `json:"results,omitempty"`
}

type recordCollection struct {
	records            []memorycore.Record
	sourceCounts       map[string]int
	unavailableSources []string
}

func main() {
	skillmain.Main(command, skillmain.Chain(run,
		skillmain.WithRecover[Input](),
	))
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	start := time.Now()
	mode, err := normalizeInput(&in, rc)
	if err != nil {
		return err
	}

	records, err := collectCuratorRecords(ctx, rc, in)
	if err != nil {
		return err
	}

	cfg := memorycore.DefaultCuratorConfig(time.Now().UTC())
	applyConfigOverrides(&cfg, in)
	report := memorycore.PlanCuratorReport(records.records, cfg)
	report.Mode = string(mode)

	var apply *ApplyReport
	if mode == ModeApply {
		apply, err = applyCuratorReport(ctx, rc, in.Workspace, report)
		if err != nil {
			return err
		}
	}

	reportArtifact, casHint, savedReportName, err := maybePersistReport(ctx, rc, in, report, apply, records)
	if err != nil {
		return err
	}

	status := "ok"
	if apply != nil && apply.Summary.Failed > 0 {
		status = "partial"
	}
	message := curatorMessage(mode, report, apply)
	if len(records.unavailableSources) > 0 {
		message += "; some lanes were unavailable"
	}

	emitCuratorTelemetry(ctx, rc, in, mode, report, apply, records, status, time.Since(start))

	return skillout.Emit(rc, command, Output{
		Status:             status,
		Message:            message,
		Workspace:          in.Workspace,
		Mode:               mode,
		Report:             report,
		Apply:              apply,
		ReportArtifact:     reportArtifact,
		CASHint:            casHint,
		SavedReportName:    savedReportName,
		SourceCounts:       records.sourceCounts,
		UnavailableSources: records.unavailableSources,
	})
}

func emitCuratorTelemetry(ctx context.Context, rc *skillmain.RunContext, in Input, mode Mode, report memorycore.CuratorReport, apply *ApplyReport, records recordCollection, status string, duration time.Duration) {
	sessionID := ""
	agentID := ""
	if rc != nil {
		sessionID = rc.SessionID
		agentID = rc.AgentID
	}
	builder := observability.NewEvent(observability.OpMemoryCuratorReport).
		WithComponent(observability.ComponentSkill).
		WithCommand(command).
		WithWorkspace(in.Workspace).
		WithSession(sessionID, agentID).
		EnrichFromContext(ctx).
		EnrichFromEnv().
		WithData("always_sample", true).
		WithData("mode", string(mode)).
		WithData("status", status).
		WithData("persist_report", in.PersistReport).
		WithData("limit", in.Limit).
		WithData("source_counts", records.sourceCounts).
		WithData("unavailable_sources", len(records.unavailableSources)).
		WithData("total_records", report.Summary.TotalRecords).
		WithData("by_lifecycle", report.Summary.ByLifecycle).
		WithData("by_kind", report.Summary.ByKind).
		WithData("by_source_lane", report.Summary.BySourceLane).
		WithData("proposals", len(report.Proposals)).
		WithData("proposed_demotions", report.Summary.ProposedDemotions).
		WithData("proposed_archives", report.Summary.ProposedArchives).
		WithData("proposed_deprecations", report.Summary.ProposedDeprecations).
		WithData("revalidation_candidates", report.Summary.RevalidationCandidates).
		WithData("pinned_skipped", report.Summary.PinnedSkipped).
		WithData("quarantined_records", report.Summary.QuarantinedRecords).
		WithData("duplicate_clusters", report.Summary.DuplicateClusters).
		WithData("overlap_clusters", report.Summary.OverlapClusters).
		WithData("supersession_proposals", report.Summary.SupersessionProposals)
	if apply != nil {
		builder.WithData("apply_attempted", apply.Summary.Attempted).
			WithData("apply_applied", apply.Summary.Applied).
			WithData("apply_skipped", apply.Summary.Skipped).
			WithData("apply_failed", apply.Summary.Failed).
			WithData("apply_unsupported_source", apply.Summary.UnsupportedSource).
			WithData("apply_unsupported_action", apply.Summary.UnsupportedAction)
	}
	observability.Emit(ctx, builder.Success(duration))
}

func normalizeInput(in *Input, rc *skillmain.RunContext) (Mode, error) {
	in.Limit = mathutil.DefaultPositiveInt(in.Limit, defaultLimit)
	if in.Limit > maxLimit {
		in.Limit = maxLimit
	}
	if in.Workspace == "" {
		in.Workspace = rc.Workspace
	}
	in.Workspace = workspace.CanonicalID(in.Workspace)
	mode := Mode(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = ModeDryRun
	}
	switch mode {
	case ModeDryRun:
	case ModeApply:
		if !in.ConfirmApply {
			return "", skillerr.Validation("mode=apply requires confirm_apply=true")
		}
		in.PersistReport = true
	default:
		return "", skillerr.Validationf("mode must be one of: %s, %s", ModeDryRun, ModeApply)
	}
	return mode, nil
}

func applyConfigOverrides(cfg *memorycore.CuratorConfig, in Input) {
	if in.StaleAfterDays > 0 {
		cfg.StaleAfterDays = in.StaleAfterDays
	}
	if in.ArchiveAfterDays > 0 {
		cfg.ArchiveAfterDays = in.ArchiveAfterDays
	}
	if in.RevalidateAfterDays > 0 {
		cfg.RevalidateAfterDays = in.RevalidateAfterDays
	}
	if in.RevalidateEnvClaimsAfterDays > 0 {
		cfg.RevalidateEnvClaimsAfterDays = in.RevalidateEnvClaimsAfterDays
	}
	if in.MinUsesBeforeUtilityJudgment > 0 {
		cfg.MinUsesBeforeUtilityJudgment = in.MinUsesBeforeUtilityJudgment
	}
}

func collectCuratorRecords(ctx context.Context, rc *skillmain.RunContext, in Input) (recordCollection, error) {
	out := recordCollection{
		sourceCounts: map[string]int{},
	}

	memStore, err := rc.Stores.Memory(ctx)
	if err != nil {
		return out, skillerr.WrapIO("open memory store", err)
	}
	entries, err := memStore.List(ctx, in.Workspace, in.Limit)
	if err != nil {
		return out, skillerr.WrapIO("list named memory", err)
	}
	for _, entry := range entries {
		if skipInternalMemoryEntry(entry.Type) {
			continue
		}
		record := memorycore.RecordFromNamedEntry(entry, memorycore.NamedEntryOptions{
			Summary:  skillout.TruncateString(entry.Summary, 500),
			FileRefs: namedEntryFileRefs(entry.Name),
		})
		out.records = append(out.records, record)
		out.sourceCounts[string(record.SourceLane)]++
	}

	claimStore, err := contextstore.Open(ctx, rc.Config.Storage.Root)
	if err != nil {
		out.unavailableSources = append(out.unavailableSources, string(memorycore.SourceLaneContextClaim))
		return out, nil
	}
	defer claimStore.Close()

	claims, err := claimStore.ListClaims(ctx, contextengine.ClaimFilter{
		WorkspaceID: in.Workspace,
		Limit:       in.Limit,
	})
	if err != nil {
		out.unavailableSources = append(out.unavailableSources, string(memorycore.SourceLaneContextClaim))
		return out, nil
	}
	for _, claim := range claims {
		record := memorycore.RecordFromContextClaim(claim, memorycore.ContextClaimOptions{
			Summary: skillout.TruncateString(claim.Summary, 500),
		})
		out.records = append(out.records, record)
		out.sourceCounts[string(record.SourceLane)]++
	}

	return out, nil
}

func skipInternalMemoryEntry(entryType string) bool {
	return entryType == "symbol" || entryType == "code_symbol" || entryType == "curator_report"
}

func namedEntryFileRefs(name string) []string {
	if !strings.HasPrefix(name, "edit:") {
		return nil
	}
	parts := strings.SplitN(name, ":", 3)
	if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
		return nil
	}
	return []string{parts[1]}
}

func applyCuratorReport(ctx context.Context, rc *skillmain.RunContext, workspace string, report memorycore.CuratorReport) (*ApplyReport, error) {
	memStore, err := rc.Stores.Memory(ctx)
	if err != nil {
		return nil, skillerr.WrapIO("open memory store for curator apply", err)
	}
	claimStore, err := contextstore.Open(ctx, rc.Config.Storage.Root)
	if err != nil {
		return nil, skillerr.WrapIO("open context claim store for curator apply", err)
	}
	defer claimStore.Close()

	out := &ApplyReport{}
	for _, proposal := range report.Proposals {
		out.Summary.Attempted++
		result := applyCuratorProposal(ctx, memStore, claimStore, workspace, proposal)
		out.Results = append(out.Results, result)
		switch result.Status {
		case ApplyStatusApplied:
			out.Summary.Applied++
		case ApplyStatusFailed:
			out.Summary.Failed++
		case ApplyStatusSkipped:
			out.Summary.Skipped++
			if result.Reason == "unsupported source lane" {
				out.Summary.UnsupportedSource++
			}
			if result.Reason == "unsupported curator action" || result.Reason == "invalid claim transition" {
				out.Summary.UnsupportedAction++
			}
		}
	}
	return out, nil
}

func applyCuratorProposal(ctx context.Context, memStore *memory.Store, claimStore contextstore.Store, workspace string, proposal memorycore.CuratorProposal) ApplyResult {
	result := ApplyResult{
		RecordID:   proposal.RecordID,
		SourceLane: proposal.SourceLane,
		Action:     proposal.Action,
		Status:     ApplyStatusSkipped,
		FromState:  proposal.CurrentState,
		ToState:    proposal.ProposedState,
	}
	switch proposal.SourceLane {
	case memorycore.SourceLaneContextClaim:
		return applyContextClaimProposal(ctx, claimStore, workspace, proposal, result)
	case memorycore.SourceLaneNamedMemory:
		return applyNamedMemoryProposal(ctx, memStore, workspace, proposal, result)
	default:
		result.Reason = "unsupported source lane"
		return result
	}
}

func applyContextClaimProposal(ctx context.Context, claimStore contextstore.Store, workspace string, proposal memorycore.CuratorProposal, result ApplyResult) ApplyResult {
	target, ok := claimStatusForProposal(proposal)
	if !ok {
		result.Reason = "unsupported curator action"
		return result
	}
	claim, err := claimStore.GetClaim(ctx, proposal.RecordID)
	if err != nil {
		result.Status = ApplyStatusFailed
		result.Reason = err.Error()
		return result
	}
	if claim.WorkspaceID != workspace {
		result.Status = ApplyStatusFailed
		result.Reason = "claim workspace mismatch"
		return result
	}
	result.FromClaimStatus = claim.Status
	result.ToClaimStatus = target
	currentState := memorycore.LifecycleForClaimStatus(claim.Status).State
	if currentState != proposal.CurrentState {
		result.Reason = "stale proposal"
		result.FromState = currentState
		return result
	}
	if claim.Status == target {
		result.Reason = "already in target state"
		return result
	}
	updated, err := contextengine.ApplyClaimTransition(claim, target, curatorApplyReason(reportReason(proposal)), time.Now().UTC())
	if err != nil {
		result.Reason = "invalid claim transition"
		return result
	}
	if _, err := claimStore.UpsertClaim(ctx, updated); err != nil {
		result.Status = ApplyStatusFailed
		result.Reason = err.Error()
		return result
	}
	result.Status = ApplyStatusApplied
	result.ToState = memorycore.LifecycleForClaimStatus(updated.Status).State
	return result
}

func applyNamedMemoryProposal(ctx context.Context, memStore *memory.Store, workspace string, proposal memorycore.CuratorProposal, result ApplyResult) ApplyResult {
	target, ok := namedMemoryLifecycleForProposal(proposal)
	if !ok {
		result.Reason = "unsupported curator action"
		return result
	}
	sourceID := strings.TrimSpace(proposal.SourceID)
	if sourceID == "" {
		sourceID = strings.TrimSpace(proposal.RecordID)
	}
	entry, err := memStore.Get(ctx, sourceID, workspace)
	if err != nil {
		result.Status = ApplyStatusFailed
		result.Reason = err.Error()
		return result
	}
	currentState := memorycore.RecordFromNamedEntry(entry, memorycore.NamedEntryOptions{}).Lifecycle.State
	result.FromState = currentState
	result.ToState = target
	if currentState != proposal.CurrentState {
		result.Reason = "stale proposal"
		return result
	}
	update := storage.MemoryLifecycleUpdate{
		LifecycleState: string(target),
		ReviewStatus:   string(namedMemoryReviewStatusForProposal(proposal)),
		ReviewNotes:    curatorApplyReason(reportReason(proposal)),
	}
	if proposal.Action == memorycore.CuratorActionRevalidate {
		update.LifecycleState = string(currentState)
	}
	updated, err := memStore.UpdateLifecycle(ctx, sourceID, workspace, update)
	if err != nil {
		result.Status = ApplyStatusFailed
		result.Reason = err.Error()
		return result
	}
	result.Status = ApplyStatusApplied
	result.ToState = memorycore.RecordFromNamedEntry(updated, memorycore.NamedEntryOptions{}).Lifecycle.State
	return result
}

func claimStatusForProposal(proposal memorycore.CuratorProposal) (contextengine.ClaimStatus, bool) {
	switch proposal.Action {
	case memorycore.CuratorActionDemoteStale:
		return contextengine.ClaimStatusStale, true
	case memorycore.CuratorActionDeprecate:
		return contextengine.ClaimStatusSuperseded, true
	case memorycore.CuratorActionRevalidate:
		return contextengine.ClaimStatusNeedsRevalidation, true
	default:
		return "", false
	}
}

func namedMemoryLifecycleForProposal(proposal memorycore.CuratorProposal) (memorycore.LifecycleState, bool) {
	switch proposal.Action {
	case memorycore.CuratorActionDemoteStale:
		return memorycore.LifecycleStateStale, true
	case memorycore.CuratorActionArchive:
		return memorycore.LifecycleStateArchived, true
	case memorycore.CuratorActionDeprecate:
		return memorycore.LifecycleStateDeprecated, true
	case memorycore.CuratorActionRevalidate:
		return proposal.CurrentState, true
	default:
		return "", false
	}
}

func namedMemoryReviewStatusForProposal(proposal memorycore.CuratorProposal) memorycore.ReviewStatus {
	switch proposal.Action {
	case memorycore.CuratorActionArchive, memorycore.CuratorActionDeprecate:
		return memorycore.ReviewStatusReviewed
	case memorycore.CuratorActionDemoteStale, memorycore.CuratorActionRevalidate:
		return memorycore.ReviewStatusNeedsReview
	default:
		return memorycore.ReviewStatusUnreviewed
	}
}

func reportReason(proposal memorycore.CuratorProposal) string {
	reason := strings.Join(proposal.Reasons, "; ")
	if strings.TrimSpace(reason) != "" {
		return reason
	}
	return fmt.Sprintf("curator action %s", proposal.Action)
}

func curatorApplyReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "memory curator apply"
	}
	return "memory curator apply: " + reason
}

func maybePersistReport(ctx context.Context, rc *skillmain.RunContext, in Input, report memorycore.CuratorReport, apply *ApplyReport, records recordCollection) (string, *envelope.CASHint, string, error) {
	if !in.PersistReport {
		return "", nil, "", nil
	}
	if rc.CASStore == nil {
		return "", nil, "", skillerr.Runtime("CAS store not available for curator report persistence")
	}
	payload := map[string]any{
		"workspace":           in.Workspace,
		"report":              report,
		"apply":               apply,
		"source_counts":       records.sourceCounts,
		"unavailable_sources": records.unavailableSources,
	}
	artifact, hint, err := skillout.PersistJSONWithHint(ctx, rc, payload, "memory_curator_report", skillout.DefaultCASHintLines)
	if err != nil {
		return "", nil, "", skillerr.WrapIO("persist curator report artifact", err)
	}

	name := "curator_report:" + report.ID
	if err := saveCuratorReportMemory(ctx, rc, in.Workspace, name, report, apply, artifact.Digest); err != nil {
		return "", nil, "", err
	}
	return artifact.Digest, hint, name, nil
}

func saveCuratorReportMemory(ctx context.Context, rc *skillmain.RunContext, workspace, name string, report memorycore.CuratorReport, apply *ApplyReport, artifact string) error {
	memStore, err := rc.Stores.Memory(ctx)
	if err != nil {
		return skillerr.WrapIO("open memory store for curator report persistence", err)
	}
	summary := fmt.Sprintf("Memory curator %s inspected %d records", report.Mode, report.Summary.TotalRecords)
	if apply != nil {
		summary = fmt.Sprintf("%s; applied=%d skipped=%d failed=%d", summary, apply.Summary.Applied, apply.Summary.Skipped, apply.Summary.Failed)
	}
	payload := map[string]any{
		"report_id": report.ID,
		"mode":      report.Mode,
		"artifact":  artifact,
		"summary":   report.Summary,
		"apply":     apply,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return skillerr.WrapRuntime("marshal curator report memory", err)
	}
	if _, err := memStore.SaveResult(ctx, storage.MemorySaveOptions{
		Name:      name,
		Type:      "curator_report",
		Workspace: workspace,
		Summary:   summary,
		Result:    body,
	}); err != nil {
		return skillerr.WrapIO("save curator report memory", err)
	}
	return nil
}

func curatorMessage(mode Mode, report memorycore.CuratorReport, apply *ApplyReport) string {
	switch mode {
	case ModeApply:
		if apply == nil {
			return fmt.Sprintf("curator apply inspected %d memory records", report.Summary.TotalRecords)
		}
		return fmt.Sprintf("curator apply inspected %d memory records; applied %d, skipped %d, failed %d", report.Summary.TotalRecords, apply.Summary.Applied, apply.Summary.Skipped, apply.Summary.Failed)
	default:
		return fmt.Sprintf("curator dry-run inspected %d memory records", report.Summary.TotalRecords)
	}
}
