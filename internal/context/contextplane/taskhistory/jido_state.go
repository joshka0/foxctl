package taskhistory

import (
	"context"
	"strings"

	ws "github.com/joshka0/foxctl/internal/platform/workspace"
)

// RefreshJidoRuntimeState enriches one decoded Jido runtime state map with a
// freshly collected task continuity block when the state includes a
// workspace-local root path.
func RefreshJidoRuntimeState(ctx context.Context, storageRoot, casRoot string, state map[string]any) map[string]any {
	if len(state) == 0 {
		return state
	}
	workspaceRoot := strings.TrimSpace(stringValue(state["workspace_root"]))
	if workspaceRoot == "" {
		return state
	}
	collector, cleanup, err := OpenCollector(ctx, storageRoot, workspaceRoot, "")
	if err != nil {
		return state
	}
	defer cleanup()

	pack, err := collector.Collect(ctx, Options{
		WorkspacePath:          workspaceRoot,
		WorkspaceID:            ws.CanonicalID(workspaceRoot),
		TranscriptHistoryScope: DefaultTranscriptHistoryScope(),
	})
	if err != nil {
		return state
	}

	artifact, err := PersistPack(ctx, casRoot, pack)
	if err != nil {
		artifact = ""
	}

	refreshed := cloneAnyMap(state)
	refreshed["task_continuity"] = RenderJidoStateWithArtifact(pack, artifact)
	return refreshed
}

func cloneAnyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	default:
		return ""
	}
}
