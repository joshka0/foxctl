// Package main implements the fs/cas_get skill.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

const command = "fs/cas_get"

type input struct {
	Digest string `json:"digest"`
	Output string `json:"output,omitempty"`
}

func main() {
	skillmain.Main(command, run)
}

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
