package editutil

import (
	"bytes"
	"context"
	"os"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/diffutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillcas"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
)

// FileOptions configures how edits are applied to a file.
type FileOptions struct {
	DryRun           bool
	CreateBackup     bool
	BackupTags       []string
	DiffContext      int
	AllowDiffFailure bool
}

// FileResult captures the edit outcome for a file.
type FileResult struct {
	Diff           string
	Edited         bool
	BackupDigest   string
	BackupArtifact *skillmain.Artifact
}

// ApplyFunc transforms the original file contents and returns the modified text.
type ApplyFunc func(original string) (string, error)

// ApplyFile reads a file, applies edits, generates a diff, optionally backs up, and writes changes.
func ApplyFile(ctx context.Context, rc *skillmain.RunContext, path string, opts FileOptions, apply ApplyFunc) (FileResult, error) {
	if apply == nil {
		return FileResult{}, skillerr.Arg("apply function is required")
	}

	originalBytes, err := os.ReadFile(path)
	if err != nil {
		return FileResult{}, skillerr.WrapIO("read file", err)
	}
	original := string(originalBytes)

	modified, err := apply(original)
	if err != nil {
		return FileResult{}, err
	}

	diff, err := diffutil.UnifiedDiff(path, original, modified, opts.DiffContext)
	if err != nil {
		if !opts.AllowDiffFailure {
			return FileResult{}, skillerr.WrapRuntime("generate diff", err)
		}
		diff = ""
	}

	result := FileResult{Diff: diff}
	if !opts.DryRun && original != modified {
		if opts.CreateBackup {
			if rc == nil || rc.CASStore == nil {
				return FileResult{}, skillerr.Runtime("cas store not configured")
			}
			tags := opts.BackupTags
			if len(tags) == 0 {
				tags = []string{"backup"}
			}
			artifact, err := skillcas.PersistBuffer(ctx, rc, bytes.NewBuffer(originalBytes), "text/plain", tags...)
			if err != nil {
				return FileResult{}, skillerr.WrapIO("backup to CAS", err)
			}
			result.BackupDigest = artifact.Digest
			result.BackupArtifact = &artifact
		}
		if err := os.WriteFile(path, []byte(modified), 0o644); err != nil {
			return FileResult{}, skillerr.WrapIO("write file", err)
		}
		result.Edited = true
	}

	return result, nil
}
