// Package main implements the fs/write skill.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

type input struct {
	Path        string `json:"path"`
	Content     string `json:"content"`
	Digest      string `json:"digest"`
	Mode        string `json:"mode"`
	Permissions string `json:"permissions"`
	CreateDirs  bool   `json:"create_dirs"`
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("fs/write", "ERUNTIME", err)
	}
	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("fs/write", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	in, err := parseInput(os.Stdin)
	if err != nil {
		fail("fs/write", "EARG", err)
	}
	if err := run(ctx, rc, in); err != nil {
		fail("fs/write", "ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *runner.RunnerContext, in input) error {
	// Validate path
	targetPath, err := resolveTargetPath(rc, in.Path)
	if err != nil {
		return fmt.Errorf("resolve target path: %w", err)
	}

	// Get content
	content, err := getContent(ctx, rc, in)
	if err != nil {
		return err
	}

	// Parse permissions
	perm, err := parsePermissions(in.Permissions)
	if err != nil {
		return fmt.Errorf("invalid permissions: %w", err)
	}

	// Check write mode
	if err := checkWriteMode(targetPath, in.Mode); err != nil {
		return err
	}

	// Create parent directories if requested
	if in.CreateDirs {
		dir := filepath.Dir(targetPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create parent directories: %w", err)
		}
	}

	// Perform write operation
	bytesWritten, _, err := performWrite(targetPath, content, in.Mode, perm)
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
	}

	return rc.Emit("fs/write", data, "application/json", envelope.Meta{
		Source: "run",
		Runner: "exec",
	})
}

func parseInput(r io.Reader) (input, error) {
	var in input
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return input{}, fmt.Errorf("decode input: %w", err)
	}

	if strings.TrimSpace(in.Path) == "" {
		return input{}, fmt.Errorf("path is required")
	}

	if in.Content == "" && in.Digest == "" {
		return input{}, fmt.Errorf("either content or digest is required")
	}

	if in.Content != "" && in.Digest != "" {
		return input{}, fmt.Errorf("cannot specify both content and digest")
	}

	if in.Mode == "" {
		in.Mode = "create"
	}

	if in.Permissions == "" {
		in.Permissions = "0644"
	}

	return in, nil
}

func resolveTargetPath(rc *runner.RunnerContext, path string) (string, error) {
	valid, err := rc.PathValidator.ValidatePath(path)
	if err != nil {
		return "", fmt.Errorf("path validation failed: %w", err)
	}
	return valid, nil
}

func getContent(ctx context.Context, rc *runner.RunnerContext, in input) ([]byte, error) {
	if in.Content != "" {
		return []byte(in.Content), nil
	}

	// Read from CAS
	if in.Digest != "" {
		reader, _, err := rc.CASStore.Get(ctx, in.Digest)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve content from CAS: %w", err)
		}
		defer func() {
			errs.Ignore(reader.Close(), "close CAS reader")
		}()

		content, err := io.ReadAll(reader)
		if err != nil {
			return nil, fmt.Errorf("failed to read content from CAS: %w", err)
		}
		return content, nil
	}

	return nil, fmt.Errorf("no content provided")
}

func parsePermissions(perm string) (fs.FileMode, error) {
	// Remove leading 0 if present for parsing
	perm = strings.TrimPrefix(perm, "0")
	mode, err := strconv.ParseUint(perm, 8, 32)
	if err != nil {
		return 0, err
	}
	return fs.FileMode(mode), nil
}

func checkWriteMode(path, mode string) error {
	_, err := os.Stat(path)
	exists := err == nil

	switch mode {
	case "create":
		if exists {
			return fmt.Errorf("file already exists (use 'overwrite' mode to replace)")
		}
	case "overwrite":
		// OK to overwrite
	case "append":
		// OK to append (will create if doesn't exist)
	default:
		return fmt.Errorf("invalid mode: %s (must be create, overwrite, or append)", mode)
	}

	return nil
}

func performWrite(path string, content []byte, mode string, perm fs.FileMode) (int, string, error) {
	var f *os.File
	var err error

	switch mode {
	case "create", "overwrite":
		f, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	case "append":
		f, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, perm)
	default:
		return 0, "", fmt.Errorf("invalid mode: %s", mode)
	}

	if err != nil {
		return 0, "", fmt.Errorf("open file: %w", err)
	}
	defer func() {
		errs.Ignore(f.Close(), "close file")
	}()

	n, err := f.Write(content)
	if err != nil {
		return 0, "", fmt.Errorf("write file: %w", err)
	}

	// Calculate checksum
	var checksum string
	if mode == "append" {
		// For append mode, checksum should cover the entire final file
		if err := f.Close(); err != nil {
			return n, "", fmt.Errorf("close file before checksum: %w", err)
		}

		finalContent, err := os.ReadFile(path)
		if err != nil {
			return n, "", fmt.Errorf("read file for checksum: %w", err)
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

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit fs/write failure")
	os.Exit(1)
}
