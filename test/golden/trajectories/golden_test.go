package trajectories_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/cas"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
)

var updateGolden = flag.Bool("update", false, "update golden files")

func TestGoldenTrajectoryExportEpisodes(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	t.Setenv("AGENTCTL_HOME", filepath.Join(tmp, "agentctl"))
	t.Setenv("AGENTCTL_PATHS_CAS", filepath.Join(tmp, "cas"))
	t.Setenv("AGENTCTL_PATHS_JOBS", filepath.Join(tmp, "jobs"))
	t.Setenv("AGENTCTL_PATHS_CACHE", filepath.Join(tmp, "cache"))
	t.Setenv("AGENTCTL_PATHS_SKILLS", filepath.Join(tmp, "skills"))
	t.Setenv("AGENTCTL_STORAGE_ROOT", filepath.Join(tmp, "storage"))

	cfg, err := config.Load(ctx)
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	if err := os.MkdirAll(cfg.Home, 0o755); err != nil {
		t.Fatalf("ensure agentctl home: %v", err)
	}
	if err := os.MkdirAll(cfg.Paths.CAS, 0o755); err != nil {
		t.Fatalf("ensure cas dir: %v", err)
	}
	if err := os.MkdirAll(cfg.Paths.Jobs, 0o755); err != nil {
		t.Fatalf("ensure jobs dir: %v", err)
	}
	if err := os.MkdirAll(cfg.Paths.Cache, 0o755); err != nil {
		t.Fatalf("ensure cache dir: %v", err)
	}
	if err := os.MkdirAll(cfg.Paths.Skills, 0o755); err != nil {
		t.Fatalf("ensure skills dir: %v", err)
	}
	if err := os.MkdirAll(cfg.Storage.Root, 0o755); err != nil {
		t.Fatalf("ensure storage root: %v", err)
	}

	workspaceID := "ws-golden"
	workspaceDir := filepath.Join(tmp, "ws")
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatalf("workspace dir: %v", err)
	}

	seedTrajectoryStore(t, ctx, cfg, workspaceID)

	binPath := filepath.Join(tmp, "trajectory_export")
	buildSkillBinary(t, binPath, repoRoot(t), "./skills/trajectory_export")

	input := map[string]any{
		"workspace_id":       workspaceID,
		"limit":              10,
		"include_raw_traces": true,
		"pin":                false,
		"dry_run":            false,
	}
	inBytes, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	envOut, stderr := runSkill(t, ctx, binPath, workspaceDir, inBytes)
	if stderr != "" {
		_ = stderr
	}

	artifact := extractArtifactDigest(t, envOut)
	casStore, err := cas.NewStore(cfg.Paths.CAS)
	if err != nil {
		t.Fatalf("open cas store: %v", err)
	}
	reader, _, err := casStore.Get(ctx, artifact)
	if err != nil {
		t.Fatalf("cas get %s: %v", artifact, err)
	}
	defer func() { _ = reader.Close() }()

	buf := &bytes.Buffer{}
	if _, err := buf.ReadFrom(reader); err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	artifactBytes := buf.Bytes()

	validateEpisodeNDJSON(t, artifactBytes)

	goldenPath := filepath.Join("fixtures", "episodes.ndjson")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir fixtures: %v", err)
		}
		if err := os.WriteFile(goldenPath, artifactBytes, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v\n\nGot:\n%s", err, string(artifactBytes))
	}

	if string(want) != string(artifactBytes) {
		t.Fatalf("trajectory export episodes differ from golden.\n\nGot:\n%s\n\nWant:\n%s\n\nRun with -update to update golden.", string(artifactBytes), string(want))
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	cur := wd
	for {
		if _, err := os.Stat(filepath.Join(cur, "go.mod")); err == nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	t.Fatalf("repo root not found from %s", wd)
	return ""
}

func buildSkillBinary(t *testing.T, outPath string, repoRoot string, pkg string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-trimpath", "-o", outPath, pkg)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=-buildvcs=false")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build %s: %v\n%s", pkg, err, string(out))
	}
}

func runSkill(t *testing.T, ctx context.Context, binPath string, workspace string, input []byte) (envelope.Envelope, string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, binPath)
	cmd.Env = append(os.Environ(), "AGENTCTL_WORKSPACE="+workspace)
	cmd.Stdin = bytes.NewReader(input)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("skill run failed: %v; stdout: %s; stderr: %s", err, stdout.String(), stderr.String())
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v\nstdout=%s", err, stdout.String())
	}
	return env, stderr.String()
}

func extractArtifactDigest(t *testing.T, env envelope.Envelope) string {
	t.Helper()
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected object data, got %T", env.Data)
	}
	digest, _ := data["artifact"].(string)
	if strings.TrimSpace(digest) == "" {
		t.Fatalf("missing data.artifact")
	}
	if env.Meta.CASDigest != "" && env.Meta.CASDigest != digest {
		t.Fatalf("meta.cas_digest %q does not match artifact %q", env.Meta.CASDigest, digest)
	}
	return digest
}

func validateEpisodeNDJSON(t *testing.T, payload []byte) {
	t.Helper()
	scanner := bufio.NewScanner(bytes.NewReader(payload))
	lines := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lines++
		var ep map[string]any
		if err := json.Unmarshal([]byte(line), &ep); err != nil {
			t.Fatalf("decode episode line %d: %v", lines, err)
		}
		if _, ok := ep["episode_id"].(string); !ok {
			t.Fatalf("episode line %d missing episode_id", lines)
		}
		if _, ok := ep["workspace_id"].(string); !ok {
			t.Fatalf("episode line %d missing workspace_id", lines)
		}
		if _, ok := ep["input"].(map[string]any); !ok {
			t.Fatalf("episode line %d missing input object", lines)
		}
		if _, ok := ep["output"].(map[string]any); !ok {
			t.Fatalf("episode line %d missing output object", lines)
		}
		if _, ok := ep["meta"].(map[string]any); !ok {
			t.Fatalf("episode line %d missing meta object", lines)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan ndjson: %v", err)
	}
	if lines == 0 {
		t.Fatalf("expected at least one episode")
	}
}

func seedTrajectoryStore(t *testing.T, ctx context.Context, cfg config.Config, workspaceID string) {
	t.Helper()
	store, err := trajectory.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open trajectory store: %v", err)
	}
	defer func() {
		// Test cleanup; error is not actionable.
		_ = store.Close() //nolint:errcheck
	}()

	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// Newer trajectory (appears first).
	traj2, err := store.InsertTrajectory(ctx, trajectory.Trajectory{
		ID:             "traj-002",
		WorkspaceID:    workspaceID,
		TaskIDs:        []string{"task-2"},
		EpicID:         "epic-2",
		AgentRole:      "reviewer",
		JobID:          "job-002",
		TraceID:        "trace-002",
		Status:         trajectory.StatusOK,
		CreatedAt:      base.Add(2 * time.Hour),
		ArtifactDigest: "sha256:2222222222222222222222222222222222222222222222222222222222222222",
	})
	if err != nil {
		t.Fatalf("InsertTrajectory traj-002: %v", err)
	}
	ur2, err := store.InsertUserRequest(ctx, trajectory.UserRequestCapture{
		ID:          "req-002",
		WorkspaceID: workspaceID,
		Actor:       "actor:human:test",
		Source:      trajectory.SourceCLI,
		TS:          base.Add(2 * time.Hour),
		Text:        "export this",
	})
	if err != nil {
		t.Fatalf("InsertUserRequest req-002: %v", err)
	}
	traj2.RootRequestID = ur2.ID
	if err := store.UpdateTrajectory(ctx, traj2); err != nil {
		t.Fatalf("UpdateTrajectory traj-002: %v", err)
	}
	_, err = store.InsertEvent(ctx, trajectory.Event{
		ID:           "evt-002-a",
		TrajectoryID: traj2.ID,
		TS:           base.Add(2*time.Hour + 1*time.Second),
		Kind:         trajectory.EventKindToolCall,
		Command:      "code.symbol_search",
		Status:       "ok",
		DataInline:   map[string]any{"subkind": "graph_search"},
		DataArtifact: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Meta:         &trajectory.EventMeta{TraceID: traj2.TraceID, CASDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
	})
	if err != nil {
		t.Fatalf("InsertEvent evt-002-a: %v", err)
	}
	_, err = store.InsertEvent(ctx, trajectory.Event{
		ID:           "evt-002-b",
		TrajectoryID: traj2.ID,
		TS:           base.Add(2*time.Hour + 3*time.Second),
		Kind:         trajectory.EventKindToolResult,
		Command:      "code.symbol_search",
		Status:       "ok",
		DataInline:   map[string]any{"result": "none"},
		Meta:         &trajectory.EventMeta{TraceID: traj2.TraceID},
	})
	if err != nil {
		t.Fatalf("InsertEvent evt-002-b: %v", err)
	}

	// Older trajectory (appears second).
	traj1, err := store.InsertTrajectory(ctx, trajectory.Trajectory{
		ID:          "traj-001",
		WorkspaceID: workspaceID,
		TaskIDs:     []string{"task-1"},
		EpicID:      "epic-1",
		AgentRole:   "coder",
		JobID:       "job-001",
		TraceID:     "trace-001",
		Status:      trajectory.StatusOK,
		CreatedAt:   base.Add(1 * time.Hour),
	})
	if err != nil {
		t.Fatalf("InsertTrajectory traj-001: %v", err)
	}
	ur1, err := store.InsertUserRequest(ctx, trajectory.UserRequestCapture{
		ID:          "req-001",
		WorkspaceID: workspaceID,
		Actor:       "actor:human:test",
		Source:      trajectory.SourceCLI,
		TS:          base.Add(1 * time.Hour),
		Text:        "hello",
	})
	if err != nil {
		t.Fatalf("InsertUserRequest req-001: %v", err)
	}
	traj1.RootRequestID = ur1.ID
	if err := store.UpdateTrajectory(ctx, traj1); err != nil {
		t.Fatalf("UpdateTrajectory traj-001: %v", err)
	}
	_, err = store.InsertEvent(ctx, trajectory.Event{
		ID:           "evt-001-a",
		TrajectoryID: traj1.ID,
		TS:           base.Add(1*time.Hour + 1*time.Second),
		Kind:         trajectory.EventKindToolCall,
		Command:      "code.swe_grep",
		Status:       "ok",
		DataInline:   map[string]any{"subkind": "swe_grep"},
		Meta:         &trajectory.EventMeta{TraceID: traj1.TraceID},
	})
	if err != nil {
		t.Fatalf("InsertEvent evt-001-a: %v", err)
	}
	_, err = store.InsertEvent(ctx, trajectory.Event{
		ID:           "evt-001-b",
		TrajectoryID: traj1.ID,
		TS:           base.Add(1*time.Hour + 2*time.Second),
		Kind:         trajectory.EventKindToolResult,
		Command:      "code.swe_grep",
		Status:       "ok",
		DataInline:   map[string]any{"matches": 1},
		Meta:         &trajectory.EventMeta{TraceID: traj1.TraceID},
	})
	if err != nil {
		t.Fatalf("InsertEvent evt-001-b: %v", err)
	}
}
