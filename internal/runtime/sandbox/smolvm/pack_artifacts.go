package smolvm

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

var (
	ErrInvalidPackArtifacts = errors.New("smolvm: expected pack artifacts are incomplete")
	ErrMissingPackArtifacts = errors.New("smolvm: missing expected pack artifacts")
	ErrPackOutputIsSidecar  = errors.New("smolvm: pack output path must name the packed binary, not the .smolmachine sidecar")
)

// PackArtifacts captures deterministic paths produced by smolvm pack create.
type PackArtifacts struct {
	OutputPath  string `json:"output_path"`
	StubPath    string `json:"stub_path"`
	SidecarPath string `json:"sidecar_path,omitempty"`
	SingleFile  bool   `json:"single_file,omitempty"`
}

// ExpectedPackArtifacts derives deterministic stub/sidecar paths for one
// smolvm pack create --output value.
func ExpectedPackArtifacts(outputPath string) (PackArtifacts, error) {
	return ExpectedPackArtifactsForMode(outputPath, false)
}

// ExpectedPackArtifactsForMode derives deterministic output paths for one
// smolvm pack create --output value. smolvm treats --output as the packed
// executable path; without --single-file, it writes the sidecar next to that
// executable with an additional .smolmachine suffix.
func ExpectedPackArtifactsForMode(outputPath string, singleFile bool) (PackArtifacts, error) {
	output := strings.TrimSpace(outputPath)
	if output == "" {
		return PackArtifacts{}, ErrInvalidPackOutput
	}
	output = filepath.Clean(output)
	if output == "." || output == string(filepath.Separator) {
		return PackArtifacts{}, ErrInvalidPackOutput
	}
	if strings.HasSuffix(output, ".smolmachine") {
		return PackArtifacts{}, ErrPackOutputIsSidecar
	}

	artifacts := PackArtifacts{
		OutputPath: output,
		StubPath:   output,
		SingleFile: singleFile,
	}
	if !singleFile {
		artifacts.SidecarPath = output + ".smolmachine"
	}

	if strings.TrimSpace(artifacts.StubPath) == "" || (!singleFile && strings.TrimSpace(artifacts.SidecarPath) == "") {
		return PackArtifacts{}, ErrInvalidPackArtifacts
	}
	return artifacts, nil
}

// ValidatePackArtifacts checks that expected stub/sidecar paths are present in
// a produced path list without touching the filesystem.
func ValidatePackArtifacts(expected PackArtifacts, producedPaths []string) error {
	if strings.TrimSpace(expected.StubPath) == "" || (!expected.SingleFile && strings.TrimSpace(expected.SidecarPath) == "") {
		return ErrInvalidPackArtifacts
	}

	producedSet := make(map[string]struct{}, len(producedPaths))
	for _, raw := range producedPaths {
		cleaned := strings.TrimSpace(raw)
		if cleaned == "" {
			continue
		}
		producedSet[filepath.Clean(cleaned)] = struct{}{}
	}

	required := []string{filepath.Clean(expected.StubPath)}
	if !expected.SingleFile {
		required = append(required, filepath.Clean(expected.SidecarPath))
	}
	missing := make([]string, 0, len(required))
	for _, want := range required {
		if _, ok := producedSet[want]; ok {
			continue
		}
		missing = append(missing, want)
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: %s", ErrMissingPackArtifacts, strings.Join(missing, ", "))
	}
	return nil
}
