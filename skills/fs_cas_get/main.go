// Package main implements the fs/cas_get skill.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
)

const command = "fs/cas_get"

// input is the skill input schema for fs/cas_get operations.
type input struct {
	Digest string `json:"digest"`
	Output string `json:"output,omitempty"`
}

// main is the skill entry point for fs/cas_get.
func main() {
	skillmain.Main(command, run)
}

// run retrieves content from CAS and writes it to file system.
//
// Index:
//   Purpose: Retrieve CAS artifacts by digest and write to file system with proper extension handling
//   Flow: validate digest → get metadata → retrieve content → determine output path → write file → emit results
//   SideEffects: file creation; CAS store access; temporary file creation; directory creation
//   FailureModes: invalid digest format, CAS access errors, file write errors, permission errors
//   Observability: emits file path, digest, size, kind, and summary information
//   Related: extensionFromKind
//   Keywords: fs/cas_get, cas, content_addressable_storage, file_retrieval
//
// [[domain:cas-artifact-retrieval]]
// [[protocol:digest-validation-and-extension-mapping]]
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Validate digest format
	if strings.TrimSpace(in.Digest) == "" {
		return skillerr.Arg("digest is required")
	}
	if !strings.HasPrefix(in.Digest, "sha256:") {
		return skillerr.Validation("digest must start with 'sha256:'")
	}
	if len(in.Digest) != 71 { // "sha256:" (7) + hex (64)
		return skillerr.Validation("invalid digest length")
	}

	// Get object metadata first
	obj, err := rc.CASStore.Head(ctx, in.Digest)
	if err != nil {
		return skillerr.WrapIO("cas head "+in.Digest, err)
	}

	// Get the content
	reader, meta, err := rc.CASStore.Get(ctx, in.Digest)
	if err != nil {
		return skillerr.WrapIO("cas get "+in.Digest, err)
	}
	defer func() {
		errs.Ignore(reader.Close(), "close cas reader")
	}()

	// Determine output path
	outputPath := in.Output
	if outputPath == "" {
		// Create temp file with appropriate extension
		ext := extensionFromKind(meta.Kind)
		tmpFile, err := os.CreateTemp("", "cas-*"+ext)
		if err != nil {
			return skillerr.WrapIO("create temp file", err)
		}
		outputPath = tmpFile.Name()
		defer func() {
			errs.Ignore(tmpFile.Close(), "close temp file")
		}()

		// Copy content to temp file
		if _, err := io.Copy(tmpFile, reader); err != nil {
			return skillerr.WrapIO("write temp file", err)
		}
	} else {
		// Validate output path
		validPath, err := skillmain.ValidatePath(rc, outputPath)
		if err != nil {
			return err
		}
		outputPath = validPath

		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
			return skillerr.WrapIO("create parent dir", err)
		}

		// Write content to output file
		outFile, err := os.Create(outputPath)
		if err != nil {
			return skillerr.WrapIO("create output file", err)
		}
		defer func() {
			errs.Ignore(outFile.Close(), "close output file")
		}()

		if _, err := io.Copy(outFile, reader); err != nil {
			return skillerr.WrapIO("write output file", err)
		}
	}

	data := map[string]any{
		"path":    outputPath,
		"digest":  in.Digest,
		"size":    obj.Size,
		"kind":    meta.Kind,
		"tags":    meta.Tags,
		"pinned":  obj.Pinned,
		"summary": fmt.Sprintf("Retrieved %d bytes from CAS to %s", obj.Size, filepath.Base(outputPath)),
	}

	return skillout.Emit(rc, command, data)
}

// extensionFromKind maps content types to file extensions.
func extensionFromKind(kind string) string {
	// Extract base type from content-type
	if idx := strings.Index(kind, ";"); idx != -1 {
		kind = strings.TrimSpace(kind[:idx])
	}

	switch kind {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "text/plain":
		return ".txt"
	case "text/markdown":
		return ".md"
	case "application/json":
		return ".json"
	case "application/yaml":
		return ".yaml"
	case "text/html":
		return ".html"
	case "application/pdf":
		return ".pdf"
	default:
		return ""
	}
}
