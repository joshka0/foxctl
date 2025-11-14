package runservice

import "github.com/jkatigb/agentctl/internal/domain/skill"

// SkillHandle captures manifest and artifact metadata required for execution.
type SkillHandle struct {
	Manifest     skill.Manifest
	ManifestPath string
	ArtifactPath string
}
