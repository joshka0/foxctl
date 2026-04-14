package runservice

import "github.com/joshka0/foxctl/internal/domain/skill"

// SkillHandle captures manifest and artifact metadata required for execution.
type SkillHandle struct {
	Manifest     skill.Manifest
	ManifestPath string
	ArtifactPath string
}
