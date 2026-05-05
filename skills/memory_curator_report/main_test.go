package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skilltest"
	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/joshka0/foxctl/internal/storage"
	contextstore "github.com/joshka0/foxctl/internal/storage/contextengine"
	"github.com/stretchr/testify/require"
)

func newTestContext(t *testing.T, buf *bytes.Buffer) (*skillmain.RunContext, func()) {
	t.Helper()
	rc, cleanup := skilltest.NewTestRunContext(t, buf, nil)
	rc.Workspace = workspace.CanonicalID(rc.Workspace)
	return rc, cleanup
}

func decodeEnvelope(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var env map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env), "buffer: %s", buf.String())
	return env
}

func readWideEvents(t *testing.T, dir string) []observability.WideEvent {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, "events", observability.WideEventFileName+".ndjson"))
	require.NoError(t, err)
	lines := bytes.Split(bytes.TrimSpace(body), []byte("\n"))
	events := make([]observability.WideEvent, 0, len(lines))
	for _, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event observability.WideEvent
		require.NoError(t, json.Unmarshal(line, &event))
		events = append(events, event)
	}
	return events
}

func openContextStore(t *testing.T, rc *skillmain.RunContext) contextstore.Store {
	t.Helper()
	store, err := contextstore.Open(context.Background(), rc.Config.Storage.Root)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func seedContextClaim(t *testing.T, store contextstore.Store, workspace string, claim contextengine.MemoryClaim) contextengine.MemoryClaim {
	t.Helper()
	if claim.ID == "" {
		claim.ID = "claim-test"
	}
	if claim.WorkspaceID == "" {
		claim.WorkspaceID = workspace
	}
	if claim.Status == "" {
		claim.Status = contextengine.ClaimStatusCurrent
	}
	if claim.ClaimType == "" {
		claim.ClaimType = "semantic_fact"
	}
	if claim.Summary == "" {
		claim.Summary = "Context claim record"
	}
	saved, err := store.UpsertClaim(context.Background(), claim)
	require.NoError(t, err)
	return saved
}

func TestMemoryCuratorReportNormalizeWorkspacePreservesWorkspaceID(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := Input{Workspace: "ws-golden"}
	mode, err := normalizeInput(&in, rc)
	require.NoError(t, err)
	require.Equal(t, ModeDryRun, mode)
	require.Equal(t, "ws-golden", in.Workspace)
}

func TestMemoryCuratorReportNormalizeWorkspaceCanonicalizesPath(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	root := t.TempDir()
	in := Input{Workspace: root}
	_, err := normalizeInput(&in, rc)
	require.NoError(t, err)
	require.Equal(t, workspace.CanonicalID(root), in.Workspace)
}

func TestMemoryCuratorReportPlansDemotion(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	claimStore := openContextStore(t, rc)
	seedContextClaim(t, claimStore, rc.Workspace, contextengine.MemoryClaim{
		ID:        "claim-old-current",
		ClaimType: "semantic_fact",
		Status:    contextengine.ClaimStatusCurrent,
		Summary:   "Old current claim without recorded use",
		CreatedAt: time.Now().AddDate(0, 0, -45),
	})

	err := run(context.Background(), rc, Input{
		Limit:          100,
		StaleAfterDays: 30,
	})
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	require.Equal(t, "ok", env["status"])
	data := env["data"].(map[string]any)
	report := data["report"].(map[string]any)
	summary := report["summary"].(map[string]any)
	require.Equal(t, float64(1), summary["proposed_demotions"])

	proposals := report["proposals"].([]any)
	require.Len(t, proposals, 1)
	proposal := proposals[0].(map[string]any)
	require.Equal(t, "claim-old-current", proposal["record_id"])
	require.Equal(t, "demote_stale", proposal["action"])

	got, err := claimStore.GetClaim(context.Background(), "claim-old-current")
	require.NoError(t, err)
	require.Equal(t, contextengine.ClaimStatusCurrent, got.Status)
}

func TestMemoryCuratorReportEmitsWideEventTelemetry(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	obsDir := t.TempDir()
	observability.SetObsDirForTesting(obsDir)
	observability.SetSamplerForTesting(observability.SampleAll{})
	t.Cleanup(func() {
		observability.SetObsDirForTesting("")
		observability.SetSamplerForTesting(nil)
	})

	claimStore := openContextStore(t, rc)
	seedContextClaim(t, claimStore, rc.Workspace, contextengine.MemoryClaim{
		ID:        "claim-telemetry",
		ClaimType: "semantic_fact",
		Status:    contextengine.ClaimStatusCurrent,
		Summary:   "Old current claim without recorded use",
		CreatedAt: time.Now().AddDate(0, 0, -45),
	})

	err := run(context.Background(), rc, Input{
		Limit:          100,
		StaleAfterDays: 30,
	})
	require.NoError(t, err)

	var found *observability.WideEvent
	for _, event := range readWideEvents(t, obsDir) {
		if event.Operation == observability.OpMemoryCuratorReport {
			found = &event
			break
		}
	}
	require.NotNil(t, found, "memory.curator_report event not emitted")
	require.Equal(t, observability.ComponentSkill, found.Component)
	require.Equal(t, command, found.Command)
	require.Equal(t, rc.Workspace, found.WorkspaceID)
	require.Equal(t, observability.StatusOK, found.Status)
	require.Equal(t, "dry_run", found.Data["mode"])
	require.Equal(t, float64(1), found.Data["total_records"])
	require.Equal(t, float64(1), found.Data["proposed_demotions"])
}

func TestMemoryCuratorReportIsReadOnlyForQuarantinedClaims(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	claimStore := openContextStore(t, rc)
	seedContextClaim(t, claimStore, rc.Workspace, contextengine.MemoryClaim{
		ID:        "claim-rejected",
		ClaimType: "semantic_fact",
		Status:    contextengine.ClaimStatusRejected,
		Summary:   "Rejected claim",
		Reason:    "contradicted trusted state",
	})

	err := run(context.Background(), rc, Input{Limit: 100})
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	require.Equal(t, "ok", env["status"])
	data := env["data"].(map[string]any)
	report := data["report"].(map[string]any)
	summary := report["summary"].(map[string]any)
	require.Equal(t, float64(1), summary["quarantined_records"])

	quarantined := report["quarantined"].([]any)
	require.Len(t, quarantined, 1)
	proposal := quarantined[0].(map[string]any)
	require.Equal(t, "claim-rejected", proposal["record_id"])
}

func TestMemoryCuratorReportApplyDemotesContextClaim(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	claimStore := openContextStore(t, rc)
	seedContextClaim(t, claimStore, rc.Workspace, contextengine.MemoryClaim{
		ID:        "claim-apply-stale",
		ClaimType: "semantic_fact",
		Status:    contextengine.ClaimStatusCurrent,
		Summary:   "Old current claim ready for stale demotion",
		CreatedAt: time.Now().AddDate(0, 0, -45),
	})

	err := run(context.Background(), rc, Input{
		Mode:           "apply",
		ConfirmApply:   true,
		Limit:          100,
		StaleAfterDays: 30,
	})
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	require.Equal(t, "ok", env["status"])
	data := env["data"].(map[string]any)
	require.Equal(t, "apply", data["mode"])
	require.NotEmpty(t, data["report_artifact"])
	require.Contains(t, data["saved_report_name"].(string), "curator_report:curator-")

	apply := data["apply"].(map[string]any)
	summary := apply["summary"].(map[string]any)
	require.Equal(t, float64(1), summary["applied"])
	require.Equal(t, float64(0), summary["failed"])

	got, err := claimStore.GetClaim(context.Background(), "claim-apply-stale")
	require.NoError(t, err)
	require.Equal(t, contextengine.ClaimStatusStale, got.Status)
}

func TestMemoryCuratorReportApplyDemotesNamedMemory(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	memStore, err := rc.Stores.Memory(context.Background())
	require.NoError(t, err)
	_, err = memStore.Save(context.Background(), storage.NamedEntry{
		ID:        "named-old-memory",
		Name:      "decision:old-runner",
		Type:      "decision",
		Workspace: rc.Workspace,
		Summary:   "Old runner decision with no recorded use",
		Result:    []byte(`{"note":"old runner"}`),
		CreatedAt: time.Now().AddDate(0, 0, -45),
	})
	require.NoError(t, err)

	err = run(context.Background(), rc, Input{
		Mode:           "apply",
		ConfirmApply:   true,
		Limit:          100,
		StaleAfterDays: 30,
	})
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	require.Equal(t, "ok", env["status"])
	data := env["data"].(map[string]any)
	apply := data["apply"].(map[string]any)
	summary := apply["summary"].(map[string]any)
	require.Equal(t, float64(1), summary["applied"])
	require.Equal(t, float64(0), summary["unsupported_source"])

	got, err := memStore.Get(context.Background(), "decision:old-runner", rc.Workspace)
	require.NoError(t, err)
	require.Equal(t, "stale", got.LifecycleState)
	require.Equal(t, "needs_review", got.ReviewStatus)
	require.Contains(t, got.ReviewNotes, "memory curator apply")
}

func TestMemoryCuratorReportApplyRequiresConfirmation(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	err := run(context.Background(), rc, Input{Mode: "apply"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "confirm_apply=true")
}

func TestMemoryCuratorReportPersistDryRunStoresAuditMemory(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	claimStore := openContextStore(t, rc)
	seedContextClaim(t, claimStore, rc.Workspace, contextengine.MemoryClaim{
		ID:        "claim-persist-report",
		ClaimType: "semantic_fact",
		Status:    contextengine.ClaimStatusCurrent,
		Summary:   "Claim used for report persistence",
		CreatedAt: time.Now().AddDate(0, 0, -45),
	})

	err := run(context.Background(), rc, Input{
		Limit:          100,
		StaleAfterDays: 30,
		PersistReport:  true,
	})
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	data := env["data"].(map[string]any)
	name := data["saved_report_name"].(string)
	require.NotEmpty(t, data["report_artifact"])

	memStore, err := rc.Stores.Memory(context.Background())
	require.NoError(t, err)
	entry, err := memStore.Get(context.Background(), name, rc.Workspace)
	require.NoError(t, err)
	require.Equal(t, "curator_report", entry.Type)
}
