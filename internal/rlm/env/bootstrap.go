package env

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/intelligence/repoquery"
	"github.com/joshka0/foxctl/internal/platform/config"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/rlm"
	"github.com/joshka0/foxctl/internal/storage/obsidianindex"
	"github.com/joshka0/foxctl/internal/storage/trajectory"
)

const (
	defaultRepoHandleLimit  = 5
	defaultVaultHandleLimit = 5
	defaultThreadLimit      = 5
	defaultArtifactLimit    = 5
)

// BootstrapConfig configures the environment bootstrap.
type BootstrapConfig struct {
	AppConfig        config.Config
	VaultPath        string
	CompanionDB      *sql.DB
	RepoHandleLimit  int
	VaultHandleLimit int
	ThreadLimit      int
	ArtifactLimit    int
}

// Bootstrapper builds a typed read-only RLM environment from current foxctl state.
type Bootstrapper struct {
	cfg BootstrapConfig
}

// NewBootstrapper creates a new environment bootstrapper.
func NewBootstrapper(cfg BootstrapConfig) *Bootstrapper {
	if cfg.RepoHandleLimit <= 0 {
		cfg.RepoHandleLimit = defaultRepoHandleLimit
	}
	if cfg.VaultHandleLimit <= 0 {
		cfg.VaultHandleLimit = defaultVaultHandleLimit
	}
	if cfg.ThreadLimit <= 0 {
		cfg.ThreadLimit = defaultThreadLimit
	}
	if cfg.ArtifactLimit <= 0 {
		cfg.ArtifactLimit = defaultArtifactLimit
	}
	return &Bootstrapper{cfg: cfg}
}

// Build prepares an RLM environment from ACA, repo index, vault index, and optional companion state.
func (b *Bootstrapper) Build(ctx context.Context, task rlm.Task) (rlm.Environment, error) {
	env := rlm.Environment{
		Tools: DefaultTools(),
	}

	workspaceRoot := ws.Normalize(strings.TrimSpace(task.WorkspaceRoot))
	if workspaceRoot == "" {
		return env, nil
	}
	store := contextplane.NewWorkspaceStore(workspaceRoot)
	layout := store.Layout()

	top, err := readJSONFile(layout.TopOfMindPath)
	if err != nil {
		return env, err
	}
	env.TopOfMind = top

	handoff, handoffRefs, err := latestHandoff(layout.HandoffsDir)
	if err != nil {
		return env, err
	}
	env.LatestHandoff = handoff
	env.ArtifactHandles = uniqueStrings(append(env.ArtifactHandles, handoffRefs...))
	env.ArtifactHandles = uniqueStrings(append(env.ArtifactHandles, topOfMindRefs(top)...))

	threadIDs, sceneHandles, err := loadCompanionHandles(ctx, b.cfg.CompanionDB, b.cfg.ThreadLimit)
	if err != nil {
		return env, err
	}
	env.ActiveThreadIDs = threadIDs
	env.SceneHandles = sceneHandles

	repoHandles, err := b.loadRepoHandles(ctx, workspaceRoot, firstNonEmpty(task.Prompt, fmt.Sprint(top["objective"])))
	if err != nil {
		return env, err
	}
	env.RepoHandles = repoHandles

	vaultHandles, err := b.loadVaultHandles(ctx, firstNonEmpty(task.Prompt, fmt.Sprint(top["objective"])))
	if err != nil {
		return env, err
	}
	env.VaultHandles = vaultHandles

	artifactHandles, err := b.loadTrajectoryHandles(ctx, workspaceRoot)
	if err != nil {
		return env, err
	}
	env.ArtifactHandles = uniqueStrings(append(env.ArtifactHandles, artifactHandles...))

	return env, nil
}

func (b *Bootstrapper) loadRepoHandles(ctx context.Context, workspaceRoot, query string) ([]string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	store, err := repoindex.Open(ctx, b.cfg.AppConfig.Storage.Root, workspaceRoot)
	if err != nil {
		return nil, nil
	}
	defer func() { _ = store.Close() }()
	output, err := repoquery.NewQueryService(repoindex.NewQueryEngine(store)).SearchWithProjection(ctx, repoquery.SearchRequest{
		Query: query,
		Limit: b.cfg.RepoHandleLimit,
	})
	if err != nil {
		return nil, nil
	}
	out := make([]string, 0, len(output.Anchors))
	for _, anchor := range output.Anchors {
		if strings.TrimSpace(anchor.Path) != "" {
			out = append(out, "path:"+filepath.ToSlash(anchor.Path))
		}
	}
	if len(out) == 0 {
		for _, node := range output.Nodes {
			if strings.TrimSpace(node.ID) != "" {
				out = append(out, "repo:"+node.ID)
			}
		}
	}
	return uniqueStrings(out), nil
}

func (b *Bootstrapper) loadVaultHandles(ctx context.Context, query string) ([]string, error) {
	query = strings.TrimSpace(query)
	vaultPath := firstNonEmpty(strings.TrimSpace(b.cfg.VaultPath),
		strings.TrimSpace(os.Getenv("AGENTCTL_RLM_VAULT_PATH")),
		strings.TrimSpace(os.Getenv("AGENTCTL_ACA_VAULT_PATH")),
		strings.TrimSpace(os.Getenv("AGENTCTL_OBSIDIAN_VAULT_PATH")),
	)
	if vaultPath == "" || query == "" {
		return nil, nil
	}
	index, err := obsidianindex.Open(ctx, b.cfg.AppConfig.Storage.Root, vaultPath)
	if err != nil {
		return nil, nil
	}
	defer func() { _ = index.Close() }()
	hits, err := index.SearchNotes(ctx, query, b.cfg.VaultHandleLimit)
	if err != nil {
		return nil, nil
	}
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		if strings.TrimSpace(hit.Path) != "" {
			out = append(out, "note:"+filepath.ToSlash(hit.Path))
		}
	}
	return uniqueStrings(out), nil
}

func readJSONFile(path string) (map[string]any, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(body) == 0 {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func latestHandoff(dir string) (map[string]any, []string, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() > entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		var handoff contextplane.Handoff
		if err := json.Unmarshal(body, &handoff); err != nil {
			return nil, nil, err
		}
		payload := map[string]any{
			"path":       path,
			"task_id":    handoff.TaskID,
			"phase":      handoff.Phase,
			"outcome":    handoff.Outcome,
			"summary":    handoff.Summary,
			"created_at": handoff.CreatedAt,
		}
		return payload, append([]string(nil), handoff.EvidenceRefs...), nil
	}
	return nil, nil, nil
}

func topOfMindRefs(top map[string]any) []string {
	if top == nil {
		return nil
	}
	raw, ok := top["relevant_refs"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func loadCompanionHandles(ctx context.Context, db *sql.DB, limit int) ([]string, []string, error) {
	if db == nil || limit <= 0 {
		return nil, nil, nil
	}
	rows, err := db.QueryContext(ctx, `
		SELECT conversation_id, MAX(created_at) AS latest
		FROM companion_turns
		GROUP BY conversation_id
		ORDER BY latest DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, nil, nil
	}
	defer rows.Close()
	threadIDs := make([]string, 0, limit)
	sceneHandles := make([]string, 0, limit*2)
	for rows.Next() {
		var conversationID string
		var latest string
		if err := rows.Scan(&conversationID, &latest); err != nil {
			return nil, nil, err
		}
		conversationID = strings.TrimSpace(conversationID)
		if conversationID == "" {
			continue
		}
		threadIDs = append(threadIDs, conversationID)
		sceneHandles = append(sceneHandles, "conversation:"+conversationID)
		episodes, err := loadEpisodeHandles(ctx, db, conversationID, 2)
		if err == nil {
			sceneHandles = append(sceneHandles, episodes...)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return uniqueStrings(threadIDs), uniqueStrings(sceneHandles), nil
}

func loadEpisodeHandles(ctx context.Context, db *sql.DB, conversationID string, limit int) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id
		FROM companion_soft_episodes
		WHERE conversation_id = ?
		ORDER BY end_event_id DESC
		LIMIT ?`, conversationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, fmt.Sprintf("episode:%d", id))
	}
	return out, rows.Err()
}

func (b *Bootstrapper) loadTrajectoryHandles(ctx context.Context, workspaceRoot string) ([]string, error) {
	if strings.TrimSpace(b.cfg.AppConfig.Storage.Root) == "" || strings.TrimSpace(workspaceRoot) == "" {
		return nil, nil
	}
	store, err := trajectory.Open(ctx, b.cfg.AppConfig.Storage.Root)
	if err != nil {
		return nil, nil
	}
	defer func() { _ = store.Close() }()
	trajs, err := store.ListTrajectories(ctx, trajectory.ListFilter{
		WorkspaceID: ws.ID(workspaceRoot),
		Limit:       b.cfg.ArtifactLimit,
	})
	if err != nil {
		return nil, nil
	}
	out := make([]string, 0, len(trajs)*2)
	for _, traj := range trajs {
		if strings.TrimSpace(traj.ID) != "" {
			out = append(out, "trajectory:"+traj.ID)
		}
		if strings.TrimSpace(traj.ArtifactDigest) != "" {
			out = append(out, "artifact:"+traj.ArtifactDigest)
		}
	}
	return uniqueStrings(out), nil
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
