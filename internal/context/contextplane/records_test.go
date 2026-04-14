package contextplane

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceStoreSaveHandoffAndAppendRecords(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())

	handoffPath, err := store.SaveHandoff(Handoff{
		TaskID:       "T-1042",
		Phase:        "formalize",
		Outcome:      "partial",
		Summary:      "Defined planes and promotion rules.",
		EvidenceRefs: []string{"note:arscontexta-review"},
		NextActions:  []string{"Draft ADR-0001"},
	})
	if err != nil {
		t.Fatalf("SaveHandoff: %v", err)
	}
	if !strings.HasPrefix(handoffPath, filepath.Join(store.Layout().HandoffsDir, "t-1042-")) {
		t.Fatalf("unexpected handoff path: %s", handoffPath)
	}
	var handoff Handoff
	readJSONFile(t, handoffPath, &handoff)
	if handoff.TaskID != "T-1042" {
		t.Fatalf("task_id=%q", handoff.TaskID)
	}

	_, err = store.AppendObservation(Observation{
		Statement:    "Workers perform better with compact handoffs.",
		Confidence:   0.72,
		Count:        2,
		Project:      "foxctl",
		Area:         "runtime",
		EvidenceRefs: []string{"handoff:T-1038"},
	})
	if err != nil {
		t.Fatalf("AppendObservation: %v", err)
	}
	observations, err := store.ListObservations(10)
	if err != nil {
		t.Fatalf("ListObservations: %v", err)
	}
	if len(observations) != 1 {
		t.Fatalf("observations=%d", len(observations))
	}
	_, err = store.AppendObservation(Observation{
		Statement:    "Workers perform better with compact handoffs.",
		Confidence:   0.80,
		Count:        1,
		Project:      "foxctl",
		Area:         "runtime",
		EvidenceRefs: []string{"handoff:T-1041"},
	})
	if err != nil {
		t.Fatalf("AppendObservation merge: %v", err)
	}
	observations, err = store.ListObservations(10)
	if err != nil {
		t.Fatalf("ListObservations merge: %v", err)
	}
	if len(observations) != 1 {
		t.Fatalf("observations after merge=%d", len(observations))
	}
	mergedObs := observations[0]
	if mergedObs.Count != 3 {
		t.Fatalf("observation count=%d want 3", mergedObs.Count)
	}
	if mergedObs.Confidence != 0.80 {
		t.Fatalf("observation confidence=%.2f want 0.80", mergedObs.Confidence)
	}

	_, err = store.AppendTension(Tension{
		Kind:        "contradiction",
		Statement:   "Runtime writes are bypassing the promotion path.",
		Impact:      "medium",
		RelatedRefs: []string{"note:write-policy"},
	})
	if err != nil {
		t.Fatalf("AppendTension: %v", err)
	}
	tensions, err := store.ListTensions(10)
	if err != nil {
		t.Fatalf("ListTensions: %v", err)
	}
	if len(tensions) != 1 {
		t.Fatalf("tensions=%d", len(tensions))
	}
	_, err = store.AppendTension(Tension{
		Kind:        "contradiction",
		Statement:   "Runtime writes are bypassing the promotion path.",
		Impact:      "high",
		RelatedRefs: []string{"run:R-2026-03-09-02"},
	})
	if err != nil {
		t.Fatalf("AppendTension merge: %v", err)
	}
	tensions, err = store.ListTensions(10)
	if err != nil {
		t.Fatalf("ListTensions merge: %v", err)
	}
	if len(tensions) != 1 {
		t.Fatalf("tensions after merge=%d", len(tensions))
	}
	mergedTension := tensions[0]
	if mergedTension.Count != 2 {
		t.Fatalf("tension count=%d want 2", mergedTension.Count)
	}
	if mergedTension.Impact != "high" {
		t.Fatalf("tension impact=%q want high", mergedTension.Impact)
	}
}

func TestDraftPromotionFromObservation(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	if _, err := store.AppendObservation(Observation{
		ID:           "O-887",
		Statement:    "Workers perform better with compact handoffs.",
		Confidence:   0.72,
		Count:        2,
		Project:      "foxctl",
		Area:         "runtime",
		EvidenceRefs: []string{"handoff:T-1038"},
	}); err != nil {
		t.Fatalf("AppendObservation: %v", err)
	}

	result, err := store.DraftPromotionFromObservation("O-887", "pattern", "Compact Handoff Pattern")
	if err != nil {
		t.Fatalf("DraftPromotionFromObservation: %v", err)
	}
	if result.Job.SourceKind != "observation" {
		t.Fatalf("source kind=%q", result.Job.SourceKind)
	}
	if _, err := os.Stat(result.DraftPath); err != nil {
		t.Fatalf("draft path missing: %v", err)
	}
}

func TestMergePromotionDraft(t *testing.T) {
	workspace := t.TempDir()
	store := NewWorkspaceStore(workspace)
	if _, err := store.AppendObservation(Observation{
		ID:           "O-887",
		Statement:    "Workers perform better with compact handoffs.",
		Confidence:   0.72,
		Count:        2,
		Project:      "foxctl",
		Area:         "runtime",
		EvidenceRefs: []string{"handoff:T-1038"},
	}); err != nil {
		t.Fatalf("AppendObservation: %v", err)
	}
	draft, err := store.DraftPromotionFromObservation("O-887", "pattern", "Compact Handoff Pattern")
	if err != nil {
		t.Fatalf("DraftPromotionFromObservation: %v", err)
	}

	vaultRoot := filepath.Join(t.TempDir(), "vault")
	if err := os.MkdirAll(vaultRoot, 0o755); err != nil {
		t.Fatalf("mkdir vault: %v", err)
	}
	script := filepath.Join(t.TempDir(), "obsidian")
	content := `#!/bin/sh
cmd="$1"; shift
path=""
payload=""
for arg in "$@"; do
  case "$arg" in
    path=*) path="${arg#path=}" ;;
    content=*) payload="${arg#content=}" ;;
  esac
done
root="` + vaultRoot + `"
full="$root/$path"
case "$cmd" in
  create)
    mkdir -p "$(dirname "$full")"
    printf "%s" "$payload" > "$full"
    ;;
  read)
    if [ ! -f "$full" ]; then
      echo "File not found." 1>&2
      exit 1
    fi
    cat "$full"
    ;;
  vaults)
    printf "TestVault\t%s\n" "$root"
    ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}

	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", filepath.Dir(script)+string(os.PathListSeparator)+oldPath)
	result, err := store.MergePromotionDraft(context.Background(), "TestVault", vaultRoot, draft.DraftPath, "notes/patterns/compact-handoff-pattern.md", "")
	if err != nil {
		t.Fatalf("MergePromotionDraft: %v", err)
	}
	if result.MergedAs != "create" {
		t.Fatalf("mergedAs=%q want create", result.MergedAs)
	}
	if result.Job.Status != "reviewed_merged" {
		t.Fatalf("job status=%q want reviewed_merged", result.Job.Status)
	}

	jobs, err := store.ListPromotionJobs(10)
	if err != nil {
		t.Fatalf("ListPromotionJobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Status != "reviewed_merged" {
		t.Fatalf("unexpected jobs: %#v", jobs)
	}
}

func TestBuildReportAndGenerateMaintenanceTasks(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	if _, err := store.SaveTopOfMind(TopOfMind{
		WorkspaceID:   "ws-test",
		Objective:     "Formalize ACA",
		Phase:         "design",
		ActiveTaskIDs: []string{"T-1042"},
		NextActions:   []string{"Draft ADR-0001"},
		UpdatedAt:     time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("SaveTopOfMind: %v", err)
	}
	if _, err := store.SaveHandoff(Handoff{
		TaskID:  "T-1042",
		Phase:   "formalize",
		Outcome: "partial",
		Summary: "Defined planes and promotion rules.",
	}); err != nil {
		t.Fatalf("SaveHandoff: %v", err)
	}
	if _, err := store.AppendObservation(Observation{
		Statement:    "Compact handoffs work better than swollen transcripts.",
		Confidence:   0.72,
		Count:        2,
		Project:      "foxctl",
		Area:         "runtime",
		EvidenceRefs: []string{"handoff:T-1038"},
	}); err != nil {
		t.Fatalf("AppendObservation: %v", err)
	}
	if _, err := store.AppendTension(Tension{
		Kind:        "contradiction",
		Statement:   "Runtime writes are bypassing the promotion path.",
		Impact:      "high",
		Count:       2,
		RelatedRefs: []string{"note:write-policy"},
	}); err != nil {
		t.Fatalf("AppendTension: %v", err)
	}

	report, err := store.BuildReport()
	if err != nil {
		t.Fatalf("BuildReport: %v", err)
	}
	if report.Objective != "Formalize ACA" {
		t.Fatalf("objective=%q", report.Objective)
	}
	if report.LatestHandoff == nil {
		t.Fatalf("expected latest handoff")
	}
	if len(report.TopObservations) == 0 {
		t.Fatalf("expected top observations")
	}

	tasks, err := store.GenerateMaintenanceTasks(context.Background(), 10)
	if err != nil {
		t.Fatalf("GenerateMaintenanceTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("maintenance tasks=%d want 1", len(tasks))
	}
	if tasks[0].Priority <= 0 {
		t.Fatalf("priority=%d", tasks[0].Priority)
	}
}

func TestLoadHandoffAcceptsWorkspaceRelativePath(t *testing.T) {
	workspace := t.TempDir()
	t.Chdir(workspace)

	store := NewWorkspaceStore(".")
	handoffPath, err := store.SaveHandoff(Handoff{
		TaskID:  "T-2048",
		Phase:   "formalize",
		Outcome: "complete",
		Summary: "Validated relative handoff loading.",
	})
	if err != nil {
		t.Fatalf("SaveHandoff: %v", err)
	}

	relativePath, err := filepath.Rel(".", handoffPath)
	if err != nil {
		t.Fatalf("filepath.Rel: %v", err)
	}

	handoff, err := store.LoadHandoff(relativePath)
	if err != nil {
		t.Fatalf("LoadHandoff: %v", err)
	}
	if handoff.TaskID != "T-2048" {
		t.Fatalf("task_id=%q want T-2048", handoff.TaskID)
	}
}

func TestGenerateMaintenanceTasksIncludesPreparedLowRiskProposalMerge(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	if _, err := store.RecordMemoryProposal(context.Background(), MemoryProposal{
		DedupeKey:      "external_evidence_import|aca-vocabulary",
		Kind:           "external_evidence_import",
		Classification: "external_evidence",
		Status:         "prepared",
		ReviewRequired: true,
		Confidence:     0.72,
		BlastRadius:    "medium",
		Summary:        "Review imported evidence draft for merge consideration: ACA Vocabulary Review. Suggested target: notes/repo/aca-inspect/semantic-and-memory.md.",
		ProposedChange: map[string]any{
			"draft_path":                 "inbox/drafted-from-foxctl/external-evidence/aca-inspect/aca-vocabulary-review.md",
			"suggested_target_note_path": "notes/repo/aca-inspect/semantic-and-memory.md",
			"suggested_target_heading":   "Review",
		},
		EvaluationStatus: "accepted",
		ApplyStatus:      "review_prepared",
		Count:            2,
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatalf("RecordMemoryProposal: %v", err)
	}
	if _, err := store.RecordMemoryProposal(context.Background(), MemoryProposal{
		DedupeKey:      "methodology_draft|aca-doctrine",
		Kind:           "methodology_draft",
		Classification: "external_evidence",
		Status:         "prepared",
		ReviewRequired: true,
		Confidence:     0.72,
		BlastRadius:    "high",
		Summary:        "Review imported evidence for a methodology or doctrine update: ACA Doctrine Review.",
		ProposedChange: map[string]any{
			"draft_path":                 "inbox/drafted-from-foxctl/external-evidence/aca-inspect/aca-doctrine-review.md",
			"suggested_target_note_path": "notes/repo/aca-inspect/semantic-and-memory.md",
			"suggested_target_heading":   "Review",
		},
		EvaluationStatus: "accepted",
		ApplyStatus:      "review_prepared",
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatalf("RecordMemoryProposal high-risk: %v", err)
	}

	tasks, err := store.GenerateMaintenanceTasks(context.Background(), 10)
	if err != nil {
		t.Fatalf("GenerateMaintenanceTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("maintenance tasks=%d want 1", len(tasks))
	}
	task := tasks[0]
	if task.Kind != "proposal_merge" {
		t.Fatalf("kind=%q want proposal_merge", task.Kind)
	}
	if task.WorkPacket == nil {
		t.Fatal("expected work packet")
	}
	if task.WorkPacket.TargetPath != "notes/repo/aca-inspect/semantic-and-memory.md" {
		t.Fatalf("target=%q", task.WorkPacket.TargetPath)
	}
}

func readJSONFile(t *testing.T, path string, out any) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(body, out); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
}
