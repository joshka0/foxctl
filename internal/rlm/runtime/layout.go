package runtime

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	runManifestFilename  = "run.json"
	treeManifestFilename = "tree.json"
	nodeResultFilename   = "result.json"
	trajectoryFilename   = "trajectory.jsonl"
	artifactsDirName     = "artifacts"
	scratchDirName       = "scratch"
)

// NodeLayout is the deterministic filesystem layout plan for one run/node pair.
type NodeLayout struct {
	OutputRoot      string
	RunID           string
	NodeID          string
	RunRoot         string
	TreeJSON        string
	RunJSON         string
	NodeDir         string
	ResultJSON      string
	TrajectoryJSONL string
	ArtifactsDir    string
	ScratchDir      string
}

// PlanNodeLayout returns readable, host-path-safe output locations rooted at:
// <output_root>/runs/<run_id>/nodes/<node_id>/...
func PlanNodeLayout(outputRoot, runID, nodeID string) (NodeLayout, error) {
	normalizedRunID, err := normalizeLayoutID(runID, "run ID")
	if err != nil {
		return NodeLayout{}, err
	}
	normalizedNodeID, err := normalizeLayoutID(nodeID, "node ID")
	if err != nil {
		return NodeLayout{}, err
	}

	root := strings.TrimSpace(outputRoot)
	if root == "" {
		root = outputRootDir
	}
	root = filepath.Clean(root)

	runRoot := filepath.Join(root, "runs", normalizedRunID)
	nodeDir := filepath.Join(runRoot, "nodes", normalizedNodeID)

	return NodeLayout{
		OutputRoot:      root,
		RunID:           normalizedRunID,
		NodeID:          normalizedNodeID,
		RunRoot:         runRoot,
		TreeJSON:        filepath.Join(runRoot, treeManifestFilename),
		RunJSON:         filepath.Join(runRoot, runManifestFilename),
		NodeDir:         nodeDir,
		ResultJSON:      filepath.Join(nodeDir, nodeResultFilename),
		TrajectoryJSONL: filepath.Join(nodeDir, trajectoryFilename),
		ArtifactsDir:    filepath.Join(nodeDir, artifactsDirName),
		ScratchDir:      filepath.Join(nodeDir, scratchDirName),
	}, nil
}

func normalizeLayoutID(raw, label string) (string, error) {
	normalized := sanitizeLayoutID(raw)
	if normalized == "" {
		return "", fmt.Errorf("%s must contain at least one [a-z0-9_-] character after normalization", label)
	}
	return normalized, nil
}

func sanitizeLayoutID(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(raw))
	lastDash := false

	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}

	return strings.Trim(b.String(), "-")
}
