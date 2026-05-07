// Package main implements the codemap/import skill.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/intelligence/codemap"
	"github.com/joshka0/foxctl/internal/storage/memory"
)

const command = "codemap/import"

// Input is the skill input schema for codemap/import operations.
type Input struct {
	Path         string `json:"path"`
	Workspace    string `json:"workspace,omitempty"`
	Recursive    bool   `json:"recursive,omitempty"`
	SkipExisting *bool  `json:"skip_existing,omitempty"`
	Embed        *bool  `json:"embed,omitempty"`
}

// Output is the skill output containing import statistics and results.
type Output struct {
	Imported       int      `json:"imported"`
	Skipped        int      `json:"skipped"`
	Errors         int      `json:"errors"`
	ImportedIDs    []string `json:"imported_ids,omitempty"`
	SkippedIDs     []string `json:"skipped_ids,omitempty"`
	ErrorDetails   []string `json:"error_details,omitempty"`
	EmbeddedChunks int      `json:"embedded_chunks,omitempty"`
	Message        string   `json:"message,omitempty"`
}

// main is the skill entry point for codemap/import.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates codemap import from files with optional embedding and deduplication.
//
// Index:
//
//	Purpose: Import .codemap files from disk into memory store with optional embedding generation
//	Flow: validate input → collect .codemap files → parse each file → store with deduplication → generate embeddings optionally
//	SideEffects: file system reads; database storage; embedding generation; path validation
//	FailureModes: invalid paths, file read errors, parse errors, storage errors, embedding failures
//	Observability: emits import statistics with imported/skipped/error counts and IDs
//	Related: collectCodemapFiles, parseCodemapPayload, buildSummary
//	Keywords: codemap/import, embedding, deduplication, batch_import, statistics
//
// [[domain:codemap-batch-import]]
// [[protocol:codemap-embedding-storage]]
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	if strings.TrimSpace(in.Path) == "" {
		return skillerr.Arg("path is required", skillerr.WithHint("Provide a .codemap file or a directory containing .codemap files."))
	}

	skipExisting := true
	if in.SkipExisting != nil {
		skipExisting = *in.SkipExisting
	}
	embed := true
	if in.Embed != nil {
		embed = *in.Embed
	}

	workspace := in.Workspace
	if workspace == "" {
		workspace = rc.PathValidator.Workspace()
	} else {
		validated, err := skillmain.ValidatePath(
			rc,
			workspace,
			skillmain.WithPathMessage("workspace validation failed"),
			skillmain.WithPathHint("Provide a workspace within the allowed roots."),
		)
		if err != nil {
			return err
		}
		workspace = validated
	}

	validatedPath, err := skillmain.ValidatePath(
		rc,
		in.Path,
		skillmain.WithPathMessage("codemap path validation failed"),
		skillmain.WithPathHint("Provide a path within the allowed roots."),
	)
	if err != nil {
		return err
	}

	files, err := collectCodemapFiles(validatedPath, in.Recursive)
	if err != nil {
		return skillerr.WrapIO("collect codemap files", err)
	}
	if len(files) == 0 {
		return skillerr.NotFound("no .codemap files found", skillerr.WithHint("Provide a file path or directory containing .codemap files."))
	}

	store, err := memory.OpenWithConfig(ctx, rc.Config)
	if err != nil {
		return skillerr.WrapIO("open memory store", err)
	}
	defer store.Close() //nolint:errcheck

	out := Output{}
	for _, path := range files {
		validatedFile, err := skillmain.ValidatePath(
			rc,
			path,
			skillmain.WithPathMessage("codemap file validation failed"),
			skillmain.WithPathHint("Provide a codemap file within the allowed roots."),
		)
		if err != nil {
			out.Errors++
			out.ErrorDetails = append(out.ErrorDetails, fmt.Sprintf("%s: %v", path, err))
			continue
		}

		raw, err := os.ReadFile(validatedFile)
		if err != nil {
			out.Errors++
			out.ErrorDetails = append(out.ErrorDetails, fmt.Sprintf("%s: %v", validatedFile, err))
			continue
		}

		plan, codemapID, createdAt, summary, err := parseCodemapPayload(raw, filepath.Base(validatedFile))
		if err != nil {
			out.Errors++
			out.ErrorDetails = append(out.ErrorDetails, fmt.Sprintf("%s: %v", validatedFile, err))
			continue
		}

		entryName := fmt.Sprintf("codemap://%s", codemapID)
		if skipExisting {
			if _, err := store.Get(ctx, entryName, workspace); err == nil {
				out.Skipped++
				out.SkippedIDs = append(out.SkippedIDs, codemapID)
				continue
			} else if !errors.Is(err, memory.ErrNotFound) {
				out.Errors++
				out.ErrorDetails = append(out.ErrorDetails, fmt.Sprintf("%s: %v", validatedFile, err))
				continue
			}
		}

		entry := memory.NamedEntry{
			Name:      entryName,
			Type:      "codemap",
			Summary:   summary,
			Result:    raw,
			Workspace: workspace,
			CreatedAt: createdAt,
		}

		if _, err := store.Save(ctx, entry); err != nil {
			out.Errors++
			out.ErrorDetails = append(out.ErrorDetails, fmt.Sprintf("%s: %v", validatedFile, err))
			continue
		}

		out.Imported++
		out.ImportedIDs = append(out.ImportedIDs, codemapID)

		if embed {
			chunksStored, err := codemap.StoreEmbeddingPlan(ctx, store, rc.Config, workspace, entryName, plan)
			if err != nil {
				out.ErrorDetails = append(out.ErrorDetails, fmt.Sprintf("%s: embed failed: %v", validatedFile, err))
				out.Errors++
			} else {
				out.EmbeddedChunks += chunksStored
			}
		}
	}

	out.Message = fmt.Sprintf("Imported %d codemap(s), skipped %d, errors %d", out.Imported, out.Skipped, out.Errors)
	return skillout.Emit(rc, command, out)
}

// collectCodemapFiles finds .codemap files in a directory or validates a single file.
func collectCodemapFiles(path string, recursive bool) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		if strings.HasSuffix(path, ".codemap") {
			return []string{path}, nil
		}
		return nil, fmt.Errorf("not a .codemap file: %s", path)
	}

	var files []string
	if !recursive {
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if strings.HasSuffix(entry.Name(), ".codemap") {
				files = append(files, filepath.Join(path, entry.Name()))
			}
		}
		return files, nil
	}

	err = filepath.WalkDir(path, func(candidate string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".codemap") {
			files = append(files, candidate)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// parseCodemapPayload parses raw codemap data and extracts metadata for storage.
func parseCodemapPayload(raw []byte, filename string) (codemap.EmbeddingPlan, string, time.Time, string, error) {
	if cm, ok, err := codemap.ParseWindsurfCodemap(raw); err != nil {
		return codemap.EmbeddingPlan{}, "", time.Time{}, "", err
	} else if ok {
		id := cm.ID
		if id == "" {
			id = cm.StableID
		}
		if id == "" {
			id = strings.TrimSuffix(filename, filepath.Ext(filename))
		}
		plan := codemap.BuildEmbeddingPlanFromWindsurf(cm)
		summary := buildSummary(cm.Title, cm.Description, id)
		return plan, id, cm.GenerationTime(), summary, nil
	}

	var cm codemap.Codemap
	if err := json.Unmarshal(raw, &cm); err != nil {
		return codemap.EmbeddingPlan{}, "", time.Time{}, "", err
	}
	id := cm.ID
	if id == "" {
		id = strings.TrimSuffix(filename, filepath.Ext(filename))
	}
	plan := codemap.BuildEmbeddingPlan(&cm)
	summary := buildSummary(cm.Title, cm.Description, id)
	return plan, id, cm.CreatedAt, summary, nil
}

// buildSummary creates a summary string from title, description, and fallback.
func buildSummary(title, description, fallback string) string {
	if title == "" && description == "" {
		return fallback
	}
	if title == "" {
		return description
	}
	if description == "" {
		return title
	}
	return fmt.Sprintf("%s - %s", title, description)
}
