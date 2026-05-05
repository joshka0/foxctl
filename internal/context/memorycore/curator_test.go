package memorycore

import (
	"testing"
	"time"
)

func TestPlanCuratorReportDemotesLowUtilityActiveRecord(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	record := curatorTestRecord("record-low-utility", LifecycleStateActive, now.AddDate(0, 0, -40))
	record.Telemetry.UseCount = 4
	record.Telemetry.SuccessCount = 1
	record.Telemetry.FailureCount = 3

	report := PlanCuratorReport([]Record{record}, DefaultCuratorConfig(now))

	if report.Summary.ProposedDemotions != 1 {
		t.Fatalf("proposed_demotions=%d want 1", report.Summary.ProposedDemotions)
	}
	if len(report.Proposals) != 1 {
		t.Fatalf("proposals=%d want 1", len(report.Proposals))
	}
	if report.Proposals[0].Action != CuratorActionDemoteStale {
		t.Fatalf("action=%q want %q", report.Proposals[0].Action, CuratorActionDemoteStale)
	}
	if report.Proposals[0].ProposedState != LifecycleStateStale {
		t.Fatalf("proposed_state=%q want %q", report.Proposals[0].ProposedState, LifecycleStateStale)
	}
}

func TestPlanCuratorReportSkipsPinnedMutation(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	record := curatorTestRecord("record-pinned", LifecycleStateStale, now.AddDate(0, 0, -120))
	record.Lifecycle.Pinned = true

	report := PlanCuratorReport([]Record{record}, DefaultCuratorConfig(now))

	if report.Summary.PinnedSkipped != 1 {
		t.Fatalf("pinned_skipped=%d want 1", report.Summary.PinnedSkipped)
	}
	if len(report.Proposals) != 0 {
		t.Fatalf("mutation proposals=%d want 0", len(report.Proposals))
	}
	if len(report.PinnedSkipped) != 1 || report.PinnedSkipped[0].Action != CuratorActionSkipPinned {
		t.Fatalf("pinned skipped=%v want skip_pinned proposal", report.PinnedSkipped)
	}
}

func TestPlanCuratorReportArchivesOldStaleRecord(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	record := curatorTestRecord("record-stale", LifecycleStateStale, now.AddDate(0, 0, -120))

	report := PlanCuratorReport([]Record{record}, DefaultCuratorConfig(now))

	if report.Summary.ProposedArchives != 1 {
		t.Fatalf("proposed_archives=%d want 1", report.Summary.ProposedArchives)
	}
	if report.Proposals[0].Action != CuratorActionArchive {
		t.Fatalf("action=%q want %q", report.Proposals[0].Action, CuratorActionArchive)
	}
}

func TestPlanCuratorReportReportsSupersededActiveRecordWithoutMutationProposal(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	record := curatorTestRecord("record-superseded", LifecycleStateActive, now.AddDate(0, 0, -5))
	record.Lifecycle.SupersededBy = "record-new"

	report := PlanCuratorReport([]Record{record}, DefaultCuratorConfig(now))

	if report.Summary.ProposedDeprecations != 0 {
		t.Fatalf("proposed_deprecations=%d want 0", report.Summary.ProposedDeprecations)
	}
	if len(report.Proposals) != 0 {
		t.Fatalf("mutation proposals=%d want 0", len(report.Proposals))
	}
	if report.Summary.SupersessionProposals != 1 {
		t.Fatalf("supersession_proposals=%d want 1", report.Summary.SupersessionProposals)
	}
	if report.SupersessionProposals[0].TargetMissing != true {
		t.Fatalf("target_missing=false want true")
	}
}

func TestPlanCuratorReportFindsRevalidationCandidate(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	record := curatorTestRecord("record-revalidate", LifecycleStateActive, now.AddDate(0, 0, -20))
	record.Temporal.RequiresValidation = true

	report := PlanCuratorReport([]Record{record}, DefaultCuratorConfig(now))

	if report.Summary.RevalidationCandidates != 1 {
		t.Fatalf("revalidation_candidates=%d want 1", report.Summary.RevalidationCandidates)
	}
	if report.Proposals[0].Action != CuratorActionRevalidate {
		t.Fatalf("action=%q want %q", report.Proposals[0].Action, CuratorActionRevalidate)
	}
}

func TestPlanCuratorReportFindsExpiredTTLCandidate(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	record := curatorTestRecord("record-ttl", LifecycleStateActive, now.AddDate(0, 0, -2))
	record.Temporal.TTLSeconds = int64((24 * time.Hour).Seconds())

	report := PlanCuratorReport([]Record{record}, DefaultCuratorConfig(now))

	if report.Summary.RevalidationCandidates != 1 {
		t.Fatalf("revalidation_candidates=%d want 1", report.Summary.RevalidationCandidates)
	}
	if report.Proposals[0].Action != CuratorActionRevalidate {
		t.Fatalf("action=%q want %q", report.Proposals[0].Action, CuratorActionRevalidate)
	}
}

func TestPlanCuratorReportReportsQuarantinedRecord(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	record := curatorTestRecord("record-quarantined", LifecycleStateQuarantined, now.AddDate(0, 0, -1))

	report := PlanCuratorReport([]Record{record}, DefaultCuratorConfig(now))

	if report.Summary.QuarantinedRecords != 1 {
		t.Fatalf("quarantined_records=%d want 1", report.Summary.QuarantinedRecords)
	}
	if len(report.Quarantined) != 1 {
		t.Fatalf("quarantined=%d want 1", len(report.Quarantined))
	}
	if report.Quarantined[0].Action != CuratorActionReviewQuarantined {
		t.Fatalf("action=%q want %q", report.Quarantined[0].Action, CuratorActionReviewQuarantined)
	}
}

func TestPlanCuratorReportReportsDuplicateCluster(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	left := curatorTestRecord("record-a", LifecycleStateActive, now.AddDate(0, 0, -2))
	left.Summary = "Use repo graph for call reference navigation"
	left.Tags = []string{"repoindex", "navigation"}
	left.Links.FileRefs = []string{"internal/context/memorycore/curator.go"}
	right := curatorTestRecord("record-b", LifecycleStateActive, now.AddDate(0, 0, -1))
	right.Summary = "Use repo graph for call reference navigation"
	right.Tags = []string{"repoindex", "navigation"}
	right.Links.FileRefs = []string{"internal/context/memorycore/curator.go"}

	report := PlanCuratorReport([]Record{right, left}, DefaultCuratorConfig(now))

	if report.Summary.DuplicateClusters != 1 {
		t.Fatalf("duplicate_clusters=%d want 1", report.Summary.DuplicateClusters)
	}
	if len(report.ConsolidationClusters) != 1 {
		t.Fatalf("consolidation_clusters=%d want 1", len(report.ConsolidationClusters))
	}
	cluster := report.ConsolidationClusters[0]
	if cluster.Kind != ConsolidationKindDuplicate {
		t.Fatalf("cluster kind=%q want %q", cluster.Kind, ConsolidationKindDuplicate)
	}
	if got, want := cluster.RecordIDs, []string{"record-a", "record-b"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("record_ids=%v want %v", got, want)
	}
	if !cluster.ManualReview {
		t.Fatalf("manual_review=false want true")
	}
	if len(report.Proposals) != 0 {
		t.Fatalf("mutation proposals=%d want 0", len(report.Proposals))
	}
}

func TestPlanCuratorReportReportsOverlapCluster(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	left := curatorTestRecord("record-a", LifecycleStateActive, now.AddDate(0, 0, -2))
	left.Summary = "Run semantic tree before editing memory curator"
	left.Tags = []string{"curator", "memory"}
	left.Links.FileRefs = []string{"internal/context/memorycore/curator.go"}
	right := curatorTestRecord("record-b", LifecycleStateActive, now.AddDate(0, 0, -1))
	right.Summary = "Run semantic search tree before changing memory curator"
	right.Tags = []string{"curator", "memory"}
	right.Links.FileRefs = []string{"internal/context/memorycore/curator.go"}
	right.SourceLane = SourceLaneNamedMemory

	report := PlanCuratorReport([]Record{left, right}, DefaultCuratorConfig(now))

	if report.Summary.OverlapClusters != 1 {
		t.Fatalf("overlap_clusters=%d want 1", report.Summary.OverlapClusters)
	}
	if len(report.ConsolidationClusters) != 1 {
		t.Fatalf("consolidation_clusters=%d want 1", len(report.ConsolidationClusters))
	}
	if report.ConsolidationClusters[0].Kind != ConsolidationKindOverlap {
		t.Fatalf("cluster kind=%q want %q", report.ConsolidationClusters[0].Kind, ConsolidationKindOverlap)
	}
}

func TestPlanCuratorReportReportsSupersessionProposal(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	old := curatorTestRecord("record-old", LifecycleStateActive, now.AddDate(0, 0, -5))
	old.Lifecycle.SupersededBy = "record-new"
	newer := curatorTestRecord("record-new", LifecycleStateActive, now.AddDate(0, 0, -1))

	report := PlanCuratorReport([]Record{old, newer}, DefaultCuratorConfig(now))

	if report.Summary.SupersessionProposals != 1 {
		t.Fatalf("supersession_proposals=%d want 1", report.Summary.SupersessionProposals)
	}
	if len(report.SupersessionProposals) != 1 {
		t.Fatalf("supersession_proposals len=%d want 1", len(report.SupersessionProposals))
	}
	proposal := report.SupersessionProposals[0]
	if proposal.RecordID != "record-old" || proposal.SupersededBy != "record-new" {
		t.Fatalf("proposal=%+v want record-old superseded by record-new", proposal)
	}
	if !proposal.ManualReview || proposal.TargetMissing {
		t.Fatalf("proposal manual_review/target_missing=%v/%v want true/false", proposal.ManualReview, proposal.TargetMissing)
	}
}

func TestPlanCuratorReportReportsPinnedClusterForManualReview(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	pinned := curatorTestRecord("record-pinned", LifecycleStateActive, now.AddDate(0, 0, -2))
	pinned.Summary = "Keep stable policy memory for curator"
	pinned.Lifecycle.Pinned = true
	duplicate := curatorTestRecord("record-copy", LifecycleStateActive, now.AddDate(0, 0, -1))
	duplicate.Summary = "Keep stable policy memory for curator"

	report := PlanCuratorReport([]Record{duplicate, pinned}, DefaultCuratorConfig(now))

	if report.Summary.DuplicateClusters != 1 {
		t.Fatalf("duplicate_clusters=%d want 1", report.Summary.DuplicateClusters)
	}
	cluster := report.ConsolidationClusters[0]
	if len(cluster.PinnedRecordIDs) != 1 || cluster.PinnedRecordIDs[0] != "record-pinned" {
		t.Fatalf("pinned_record_ids=%v want [record-pinned]", cluster.PinnedRecordIDs)
	}
	if cluster.PrimaryRecordID != "record-pinned" {
		t.Fatalf("primary_record_id=%q want record-pinned", cluster.PrimaryRecordID)
	}
	if len(report.Proposals) != 0 || len(report.PinnedSkipped) != 0 {
		t.Fatalf("mutation proposals=%d pinned_skipped=%d want 0/0", len(report.Proposals), len(report.PinnedSkipped))
	}
}

func curatorTestRecord(id string, state LifecycleState, at time.Time) Record {
	return Record{
		ID:         id,
		Kind:       KindSemanticFact,
		SourceLane: SourceLaneContextClaim,
		SourceID:   id,
		Summary:    "test record",
		Temporal: TemporalEnvelope{
			ObservedAt:    at.UTC().Format(time.RFC3339),
			IngestedAt:    at.UTC().Format(time.RFC3339),
			ValidFrom:     at.UTC().Format(time.RFC3339),
			TemporalScope: "durative",
		},
		Lifecycle: LifecycleEnvelope{
			State:        state,
			ReviewStatus: ReviewStatusUnreviewed,
		},
	}
}
