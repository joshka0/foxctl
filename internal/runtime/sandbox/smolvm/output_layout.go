package smolvm

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

var (
	ErrInvalidOutputRoot = errors.New("smolvm: output root is required")
	ErrInvalidRunID      = errors.New("smolvm: run id resolves to empty readable identifier")
	ErrInvalidAgentID    = errors.New("smolvm: agent id resolves to empty readable identifier")
)

// OutputLayoutPlan captures deterministic run/agent output locations inside
// the shared guest output root.
type OutputLayoutPlan struct {
	RootDir         string          `json:"root_dir"`
	Run             RunOutputLayout `json:"run"`
	SharedReadPaths []string        `json:"shared_read_paths,omitempty"`
}

// RunOutputLayout captures run-level namespace paths.
type RunOutputLayout struct {
	ID             string              `json:"id"`
	Dir            string              `json:"dir"`
	ManifestPath   string              `json:"manifest_path"`
	EventsPath     string              `json:"events_path"`
	BlackboardPath string              `json:"blackboard_path"`
	AgentsDir      string              `json:"agents_dir"`
	Agents         []AgentOutputLayout `json:"agents,omitempty"`
}

// AgentOutputLayout captures per-agent namespace paths.
type AgentOutputLayout struct {
	OriginalID     string `json:"original_id"`
	ID             string `json:"id"`
	Dir            string `json:"dir"`
	TrajectoryPath string `json:"trajectory_path"`
	ArtifactsDir   string `json:"artifacts_dir"`
	ScratchDir     string `json:"scratch_dir"`
}

// PlanOutputLayout builds deterministic output paths for one run and its agents.
func PlanOutputLayout(outputRoot, runID string, agentIDs []string) (OutputLayoutPlan, error) {
	root := strings.TrimSpace(outputRoot)
	if root == "" {
		return OutputLayoutPlan{}, ErrInvalidOutputRoot
	}
	root = path.Clean(root)
	if root == "." {
		return OutputLayoutPlan{}, ErrInvalidOutputRoot
	}

	normalizedRunID := NormalizeReadableID(runID)
	if normalizedRunID == "" {
		return OutputLayoutPlan{}, ErrInvalidRunID
	}

	runDir := path.Join(root, "runs", normalizedRunID)
	agentsDir := path.Join(runDir, "agents")

	runLayout := RunOutputLayout{
		ID:             normalizedRunID,
		Dir:            runDir,
		ManifestPath:   path.Join(runDir, "manifest.json"),
		EventsPath:     path.Join(runDir, "events.jsonl"),
		BlackboardPath: path.Join(runDir, "blackboard.jsonl"),
		AgentsDir:      agentsDir,
	}

	sharedReadPaths := []string{
		runLayout.ManifestPath,
		runLayout.EventsPath,
		runLayout.BlackboardPath,
	}

	seenAgentIDs := make(map[string]int, len(agentIDs))
	for idx, rawID := range agentIDs {
		baseID := NormalizeReadableAgentID(rawID)
		if baseID == "" {
			return OutputLayoutPlan{}, fmt.Errorf("%w (index=%d)", ErrInvalidAgentID, idx)
		}

		agentID := baseID
		seenAgentIDs[baseID]++
		if seenAgentIDs[baseID] > 1 {
			agentID = appendReadableCollisionSuffix(baseID, seenAgentIDs[baseID])
		}

		agentDir := path.Join(agentsDir, agentID)
		agentLayout := AgentOutputLayout{
			OriginalID:     strings.TrimSpace(rawID),
			ID:             agentID,
			Dir:            agentDir,
			TrajectoryPath: path.Join(agentDir, "trajectory.jsonl"),
			ArtifactsDir:   path.Join(agentDir, "artifacts"),
			ScratchDir:     path.Join(agentDir, "scratch"),
		}

		runLayout.Agents = append(runLayout.Agents, agentLayout)
		sharedReadPaths = append(sharedReadPaths, agentLayout.TrajectoryPath, agentLayout.ArtifactsDir)
	}

	return OutputLayoutPlan{
		RootDir:         root,
		Run:             runLayout,
		SharedReadPaths: sharedReadPaths,
	}, nil
}

// NormalizeReadableID converts a human label into a filesystem-safe readable ID.
func NormalizeReadableID(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}

	var b strings.Builder
	lastWasDash := false
	for _, r := range raw {
		if isReadableIDRune(r) {
			b.WriteRune(r)
			lastWasDash = r == '-'
			continue
		}
		if !lastWasDash {
			b.WriteByte('-')
			lastWasDash = true
		}
	}

	return strings.Trim(b.String(), "-_")
}

// NormalizeReadableAgentID converts a human label into a filesystem-safe,
// slash-delimited readable agent path. Slash-delimited child IDs are preserved
// so logical IDs such as "agent-root/rlm-0001" map to nested namespaces.
func NormalizeReadableAgentID(raw string) string {
	normalized := strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")
	parts := strings.Split(normalized, "/")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		if segment := NormalizeReadableID(part); segment != "" {
			cleaned = append(cleaned, segment)
		}
	}
	return strings.Join(cleaned, "/")
}

func appendReadableCollisionSuffix(agentID string, suffix int) string {
	parts := strings.Split(agentID, "/")
	if len(parts) == 0 {
		return fmt.Sprintf("%s-%d", agentID, suffix)
	}
	last := len(parts) - 1
	parts[last] = fmt.Sprintf("%s-%d", parts[last], suffix)
	return strings.Join(parts, "/")
}

func isReadableIDRune(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= '0' && r <= '9') ||
		r == '-' ||
		r == '_'
}
