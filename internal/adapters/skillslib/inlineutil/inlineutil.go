package inlineutil

import "strings"

// Mode is the shared inline-output mode used by artifact-backed skills.
type Mode string

const (
	ModeAuto         Mode = "auto"
	ModeFull         Mode = "full"
	ModePreview      Mode = "preview"
	ModeArtifactOnly Mode = "artifact_only"
)

const ValidModes = "auto, full, preview, artifact_only"

// Parse returns the canonical mode and whether the input was valid.
func Parse(value string) (Mode, bool) {
	switch Mode(strings.ToLower(strings.TrimSpace(value))) {
	case "", ModeAuto:
		return ModeAuto, true
	case ModeFull:
		return ModeFull, true
	case ModePreview:
		return ModePreview, true
	case ModeArtifactOnly:
		return ModeArtifactOnly, true
	default:
		return ModeAuto, false
	}
}
