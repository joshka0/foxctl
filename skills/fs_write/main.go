// Package main implements the fs/write skill.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
)

// Input defines the input parameters for fs/write operations.
type Input struct {
	Path        string `json:"path" validate:"required"`
	Content     string `json:"content"`
	Digest      string `json:"digest"`
	Mode        string `json:"mode" validate:"omitempty,oneof=create overwrite append"`
	Permissions string `json:"permissions"`
	CreateDirs  bool   `json:"create_dirs"`
}

// main is the skill entry point for fs/write.
func main() {
	skillmain.Main("fs/write", run)
}

// run orchestrates file writing with content validation, mode checking, and checksum generation.
//
// Index:
// - Purpose: Write files with content validation, multiple modes, and checksum generation
// - Flow: validate input → resolve path → get content → check mode → write file → generate checksum → emit results
// - SideEffects: file creation/modification; directory creation; CAS access; checksum generation
// - FailureModes: invalid paths, permission errors, mode conflicts, write errors, CAS errors
// - Observability: emits write statistics, file metadata, checksum, and mode information
// - Related: getContent, parsePermissions, checkWriteMode, performWrite
// - Keywords: fs/write, file_writing, content_validation, checksum_generation, file_modes
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Apply defaults
	if in.Mode == "" {
		in.Mode = "create"
	}
	if in.Permissions == "" {
		in.Permissions = "0644"
	}

	// Custom validation: either content or digest required
	if in.Content == "" && in.Digest == "" {
		return skillerr.Arg("either content or digest is required")
	}
	if in.Content != "" && in.Digest != "" {
		return skillerr.Arg("cannot specify both content and digest")
	}

	// Validate path
	targetPath, err := skillmain.ValidatePath(rc, in.Path, skillmain.WithPathMessage("resolve target path"))
	if err != nil {
		return err
	}

	// Get content
	content, err := getContent(ctx, rc, in)
	if err != nil {
		return err
	}

	// Parse permissions
	perm, err := parsePermissions(in.Permissions)
	if err != nil {
		return err
	}

	// Check write mode
	if err := checkWriteMode(targetPath, in.Mode); err != nil {
		return err
	}

	// Create parent directories if requested
	if in.CreateDirs {
		dir := filepath.Dir(targetPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return skillerr.WrapIO("create parent directories", err)
		}
	}

	// Perform write operation
	bytesWritten, checksum, err := performWrite(targetPath, content, in.Mode, perm)
	if err != nil {
		return err
	}

	// Get file info
	var fileSize int64
	if info, err := os.Stat(targetPath); err == nil {
		fileSize = info.Size()
	}

	data := map[string]any{
		"path":          targetPath,
		"mode":          in.Mode,
		"bytes_written": bytesWritten,
		"file_size":     fileSize,
		"permissions":   fmt.Sprintf("%04o", perm),
		"checksum":      checksum,
	}

	return skillout.Emit(rc, "fs/write", data)
}

// getContent retrieves content from input string or CAS digest.
func getContent(ctx context.Context, rc *skillmain.RunContext, in Input) ([]byte, error) {
	if in.Content != "" {
		return []byte(in.Content), nil
	}

	// Read from CAS
	if in.Digest != "" {
		reader, _, err := rc.CASStore.Get(ctx, in.Digest)
		if err != nil {
			return nil, skillerr.WrapIO("retrieve content from CAS", err)
		}
		defer func() {
			errs.Ignore(reader.Close(), "close CAS reader")
		}()

		content, err := io.ReadAll(reader)
		if err != nil {
			return nil, skillerr.WrapIO("read content from CAS", err)
		}
		return content, nil
	}

	return nil, skillerr.Arg("no content provided")
}

// parsePermissions converts octal permission string to FileMode.
func parsePermissions(perm string) (fs.FileMode, error) {
	// Remove leading 0 if present for parsing
	perm = strings.TrimPrefix(perm, "0")
	mode, err := strconv.ParseUint(perm, 8, 32)
	if err != nil {
		return 0, skillerr.WrapValidation("invalid permissions", err, skillerr.WithHint("Use an octal string like \"0644\"."))
	}
	return fs.FileMode(mode), nil
}

// checkWriteMode validates write mode against file existence.
func checkWriteMode(path, mode string) error {
	_, err := os.Stat(path)
	exists := err == nil

	switch mode {
	case "create":
		if exists {
			return skillerr.Validation("file already exists (use 'overwrite' mode to replace)")
		}
	case "overwrite":
		// OK to overwrite
	case "append":
		// OK to append (will create if doesn't exist)
	default:
		return skillerr.Validationf("invalid mode: %s (must be create, overwrite, or append)", mode)
	}

	return nil
}

// performWrite executes the actual file write operation with checksum generation.
func performWrite(path string, content []byte, mode string, perm fs.FileMode) (int, string, error) {
	var f *os.File
	var err error

	switch mode {
	case "create", "overwrite":
		f, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	case "append":
		f, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, perm)
	default:
		return 0, "", skillerr.Validationf("invalid mode: %s", mode)
	}

	if err != nil {
		return 0, "", skillerr.WrapIO("open file", err)
	}
	defer func() {
		if f != nil {
			errs.Ignore(f.Close(), "close file")
		}
	}()

	n, err := f.Write(content)
	if err != nil {
		return 0, "", skillerr.WrapIO("write file", err)
	}

	// Calculate checksum
	var checksum string
	if mode == "append" {
		// For append mode, checksum should cover the entire final file
		if err := f.Close(); err != nil {
			return n, "", skillerr.WrapIO("close file before checksum", err)
		}
		f = nil

		finalContent, err := os.ReadFile(path)
		if err != nil {
			return n, "", skillerr.WrapIO("read file for checksum", err)
		}
		hash := sha256.Sum256(finalContent)
		checksum = hex.EncodeToString(hash[:])
	} else {
		// For create/overwrite, checksum only needs to cover written content
		hash := sha256.Sum256(content)
		checksum = hex.EncodeToString(hash[:])
	}

	return n, checksum, nil
}
