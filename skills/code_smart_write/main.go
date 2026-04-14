// Package main implements the code/smart_write skill.
// Provides symbol-aware file editing with diff preview, backup, and restore capabilities.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/codeedit"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/pathutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
)

const command = "code/smart_write"

// input defines the parameters for code/smart_write operations.
type input struct {
	Path          string   `json:"path"`  // Single file (backward compat)
	Paths         []string `json:"paths"` // Multiple files or globs
	Edits         []edit   `json:"edits" validate:"omitempty,min=1,dive"`
	DryRun        bool     `json:"dry_run"`
	ContextLines  int      `json:"context_lines"`
	CreateBackup  bool     `json:"create_backup"`  // Backup to CAS before editing
	RestoreDigest string   `json:"restore_digest"` // CAS digest to restore from (undo mode)
}

// fileResult holds the result for a single file edit operation.
type fileResult struct {
	Path         string   `json:"path"`
	Edited       bool     `json:"edited"`
	EditCount    int      `json:"edit_count"`
	SymbolsFound []string `json:"symbols_found,omitempty"`
	Diff         string   `json:"diff,omitempty"`
	BackupDigest string   `json:"backup_digest,omitempty"` // CAS digest for undo
	Error        string   `json:"error,omitempty"`
}

// edit is an alias for codeedit.Edit for convenience.
type edit = codeedit.Edit

// main is the skill entry point for code/smart_write.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates code/smart_write edits and delegates restore mode.
//
// Index:
// - Purpose: Apply edits to files with optional backup and restore capabilities
// - Flow: validate input → if restore_digest delegate to handleRestore → else resolve paths → apply edits → emit response
// - SideEffects: writes files unless dry_run; optional CAS backup writes; CAS reads for restore
// - FailureModes: invalid edits, path resolution errors, file I/O errors, CAS errors
// - Observability: emits dry_run/total_edits/files_edited/files_checked; results/combined_diff in multi-file dry_run
// - Related: handleRestore, codeedit.ApplyEditsToFile, skillmain.ResolvePaths, skillout.Emit
// - Keywords: code/smart_write, dry_run, create_backup, restore_digest, total_edits, files_edited, files_checked, codeedit.ApplyEditsToFile
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Apply defaults
	if in.ContextLines <= 0 {
		in.ContextLines = 3
	}

	// Handle restore mode (undo from CAS backup)
	if in.RestoreDigest != "" {
		return handleRestore(ctx, rc, in)
	}

	// Validate edits (required for edit mode, not restore mode)
	if len(in.Edits) == 0 {
		return skillerr.Arg(
			"edits is required",
			skillerr.WithHint("Provide at least one edit operation, or use restore_digest for undo."),
		)
	}
	if err := codeedit.ValidateEdits(in.Edits); err != nil {
		return err
	}

	// Resolve paths: support single Path, multiple Paths, or globs
	paths, err := skillmain.ResolvePaths(rc, in.Path, in.Paths)
	if err != nil {
		return err
	}

	if len(paths) == 0 {
		return skillerr.Arg(
			"no files specified",
			skillerr.WithHint("Provide 'path' for a single file or 'paths' for multiple files/globs."),
		)
	}

	// Process each file
	results := make([]fileResult, 0, len(paths))
	totalEdits := 0
	filesEdited := 0

	for _, absPath := range paths {
		relPath := pathutil.RelTo(rc.PathValidator.Workspace(), absPath)
		result := fileResult{Path: relPath}

		editResult, err := codeedit.ApplyEditsToFile(ctx, rc, absPath, in.Edits, codeedit.FileEditOptions{
			DryRun:       in.DryRun,
			CreateBackup: in.CreateBackup,
			BackupTags:   []string{"backup", "smart_write"},
			ContextLines: in.ContextLines,
		})
		if err != nil {
			var skillErr *skillerr.Error
			if errors.As(err, &skillErr) {
				result.Error = skillErr.Error()
			} else {
				result.Error = fmt.Sprintf("edit failed: %v", err)
			}
			results = append(results, result)
			continue
		}

		result.EditCount = editResult.EditCount
		result.SymbolsFound = editResult.SymbolsFound
		result.Diff = editResult.Diff
		result.BackupDigest = editResult.BackupDigest
		result.Edited = editResult.Edited

		results = append(results, result)
		totalEdits += result.EditCount
		if result.Edited {
			filesEdited++
		}
	}

	// Build response
	data := map[string]any{
		"dry_run":       in.DryRun,
		"total_edits":   totalEdits,
		"files_edited":  filesEdited,
		"files_checked": len(paths),
	}

	// Single file: flatten result (backward compat)
	if len(results) == 1 {
		r := results[0]
		data["path"] = r.Path
		data["edited"] = r.Edited
		data["edit_count"] = r.EditCount
		data["symbols_found"] = r.SymbolsFound
		if r.Diff != "" {
			data["diff"] = r.Diff
		}
		if r.Error != "" {
			data["error"] = r.Error
		}
		if r.BackupDigest != "" {
			data["backup_digest"] = r.BackupDigest
		}
	} else {
		// Multiple files: return array
		data["results"] = results
		// Combine all diffs for preview
		if in.DryRun {
			var allDiffs []string
			for _, r := range results {
				if r.Diff != "" {
					allDiffs = append(allDiffs, r.Diff)
				}
			}
			if len(allDiffs) > 0 {
				data["combined_diff"] = strings.Join(allDiffs, "\n")
			}
		}
	}

	return skillout.Emit(rc, command, data)
}

// handleRestore restores a file from CAS backup.
//
// Index:
// - Purpose: Restore file content from CAS to a specified path
// - Flow: validate path → fetch from CAS → write unless dry_run → emit result
// - SideEffects: CAS read; optional file write
// - FailureModes: invalid digest, CAS read errors, file write errors
// - Observability: emits path/restored/restore_digest/dry_run/size
// - Related: skillmain.ValidatePath, CASStore.Get, skillout.Emit
// - Keywords: restore_digest, dry_run, CASStore.Get, restored, size
func handleRestore(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Require exactly one path for restore
	if in.Path == "" {
		return skillerr.Arg(
			"path is required for restore",
			skillerr.WithHint("Provide the path of the file to restore."),
		)
	}

	// Validate and resolve path
	absPath, err := skillmain.ValidatePath(rc, in.Path)
	if err != nil {
		return err
	}

	// Retrieve content from CAS
	reader, _, err := rc.CASStore.Get(ctx, in.RestoreDigest)
	if err != nil {
		return skillerr.IO(
			fmt.Sprintf("failed to retrieve backup %s: %v", in.RestoreDigest, err),
			skillerr.WithHint("Check that the digest is valid and exists in CAS."),
		)
	}
	defer reader.Close()

	content, err := io.ReadAll(reader)
	if err != nil {
		return skillerr.WrapIO("read CAS content", err)
	}

	// Write restored content to file
	if !in.DryRun {
		if err := os.WriteFile(absPath, content, 0o644); err != nil {
			return skillerr.WrapIO("write restored file", err)
		}
	}

	relPath := pathutil.RelTo(rc.PathValidator.Workspace(), absPath)
	data := map[string]any{
		"path":           relPath,
		"restored":       !in.DryRun,
		"restore_digest": in.RestoreDigest,
		"dry_run":        in.DryRun,
		"size":           len(content),
	}

	return skillout.Emit(rc, command, data)
}
