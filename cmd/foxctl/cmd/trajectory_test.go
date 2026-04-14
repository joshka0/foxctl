package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/cas"
	"github.com/joshka0/foxctl/internal/storage/trajectory"
)

func TestTrajectoryExport_ToCAS_EmitsArtifact(t *testing.T) {
	ctx := context.Background()
	cfg := newSkillTestConfig(t)
	tmp := filepath.Dir(cfg.Home)

	store, err := trajectory.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open trajectory store: %v", err)
	}
	defer store.Close()

	ws := filepath.Join(tmp, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("workspace dir: %v", err)
	}
	traj, err := store.InsertTrajectory(ctx, trajectory.Trajectory{WorkspaceID: ws, Status: trajectory.StatusOK, TraceID: "trace-1"})
	if err != nil {
		t.Fatalf("InsertTrajectory: %v", err)
	}
	ur, err := store.InsertUserRequest(ctx, trajectory.UserRequestCapture{WorkspaceID: ws, Actor: "actor:human:test", Source: trajectory.SourceCLI, Text: "hello"})
	if err != nil {
		t.Fatalf("InsertUserRequest: %v", err)
	}
	traj.RootRequestID = ur.ID
	if err := store.UpdateTrajectory(ctx, traj); err != nil {
		t.Fatalf("UpdateTrajectory: %v", err)
	}
	_, err = store.InsertEvent(ctx, trajectory.Event{TrajectoryID: traj.ID, Kind: trajectory.EventKindToolCall, Command: "code.symbol_search", Status: "ok", DataInline: map[string]any{"subkind": "graph_search"}, Meta: &trajectory.EventMeta{TraceID: "trace-1"}})
	if err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}

	cmd := newTrajectoryCommand()
	cmd.SetContext(config.WithContext(ctx, cfg))
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"export", "--workspace", ws, "--to-cas"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("trajectory export: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Meta.JobID == "" {
		t.Fatalf("expected meta.job_id to be set")
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected object data, got %T", env.Data)
	}
	artifact, _ := data["artifact"].(string)
	if artifact == "" {
		t.Fatalf("expected data.artifact to be set")
	}
	if env.Meta.CASDigest != "" && artifact != env.Meta.CASDigest {
		t.Fatalf("meta.cas_digest %q does not match artifact %q", env.Meta.CASDigest, artifact)
	}

	casStore, err := cas.NewStore(cfg.Paths.CAS)
	if err != nil {
		t.Fatalf("open cas store: %v", err)
	}
	rc, _, err := casStore.Get(ctx, artifact)
	if err != nil {
		t.Fatalf("cas get: %v", err)
	}
	defer rc.Close()

	scanner := bufio.NewScanner(rc)
	lines := 0
	for scanner.Scan() {
		lines++
		if lines > 1 {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan ndjson: %v", err)
	}
	if lines == 0 {
		t.Fatalf("expected at least one episode line in ndjson")
	}
}

func TestTrajectoryExport_Inline_EmitsEpisodesAndFinalSummary(t *testing.T) {
	ctx := context.Background()
	cfg := newSkillTestConfig(t)
	tmp := filepath.Dir(cfg.Home)

	store, err := trajectory.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open trajectory store: %v", err)
	}
	defer store.Close()

	ws := filepath.Join(tmp, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("workspace dir: %v", err)
	}
	traj, err := store.InsertTrajectory(ctx, trajectory.Trajectory{WorkspaceID: ws, Status: trajectory.StatusOK, TraceID: "trace-2"})
	if err != nil {
		t.Fatalf("InsertTrajectory: %v", err)
	}
	_, err = store.InsertEvent(ctx, trajectory.Event{TrajectoryID: traj.ID, Kind: trajectory.EventKindToolCall, Command: "code.swe_grep", Status: "ok", DataInline: map[string]any{"subkind": "swe_grep"}, Meta: &trajectory.EventMeta{TraceID: "trace-2"}})
	if err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}

	cmd := newTrajectoryCommand()
	cmd.SetContext(config.WithContext(ctx, cfg))
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"export", "--workspace", ws})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("trajectory export: %v", err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(stdout.Bytes()))
	var envs []envelope.Envelope
	for scanner.Scan() {
		var env envelope.Envelope
		if err := json.Unmarshal(scanner.Bytes(), &env); err != nil {
			t.Fatalf("decode envelope: %v", err)
		}
		envs = append(envs, env)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan output: %v", err)
	}
	if len(envs) < 2 {
		t.Fatalf("expected at least 2 envelopes (episode + final summary), got %d", len(envs))
	}

	last := envs[len(envs)-1]
	if last.Meta.Final == nil || !*last.Meta.Final {
		t.Fatalf("expected final envelope to have meta.final=true")
	}
	data, ok := last.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected object data for final envelope")
	}
	if _, ok := data["summary"].(map[string]any); !ok {
		t.Fatalf("expected final envelope data.summary")
	}
}
