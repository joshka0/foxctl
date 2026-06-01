// Package main implements the fs/read skill.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/textutil"
	"github.com/joshka0/foxctl/internal/platform/config"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
)

// Input defines the input parameters for fs/read operations.
type Input struct {
	Path     string `json:"path" validate:"required"`
	MaxBytes int    `json:"max_bytes" validate:"gte=0"`
}

// main is the skill entry point for fs/read.
func main() {
	skillmain.Main("fs/read", run)
}

// run orchestrates file reading with symlink safety, CAS storage, and preview generation.
//
// Index:
//
//	Purpose: Read files safely with symlink protection, CAS storage, and text preview generation
//	Flow: validate path → resolve symlinks → open file → store in CAS → generate preview → emit results
//	SideEffects: file system access; CAS storage; symlink resolution; preview generation
//	FailureModes: invalid paths, symlink attacks, permission errors, file not found, CAS errors
//	Observability: emits file metadata, preview content, CAS artifacts, and binary/text detection
//	Related: previewLimit, readPreview, formatPreviewWithLineNumbers, detectKind
//	Keywords: fs/read, file_reading, symlink_safety, cas_storage, preview_generation
//
// [[domain:safe-file-reading]]
// [[invariant:symlink-resolution-before-access]]
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	validPath, err := skillmain.ValidatePath(rc, in.Path)
	if err != nil {
		return err
	}

	// Resolve symlinks BEFORE opening to prevent TOCTOU race conditions
	resolved, err := filepath.EvalSymlinks(validPath)
	if err != nil {
		return skillerr.WrapIO("resolve symlink "+validPath, err)
	}
	// Validate the resolved path if it differs from the original
	if resolved != validPath {
		if _, err := skillmain.ValidatePath(rc, resolved, skillmain.WithPathMessage("resolved path validation failed")); err != nil {
			return err
		}
	}

	// Now open the resolved path (safe from symlink changes)
	file, err := os.Open(resolved)
	if err != nil {
		return skillerr.WrapIO("open "+resolved, err)
	}
	defer func() {
		errs.Ignore(file.Close(), "close input file")
	}()

	info, err := file.Stat()
	if err != nil {
		return skillerr.WrapIO("stat "+validPath, err)
	}
	if info.IsDir() {
		return skillerr.Validationf("path %s is a directory", validPath)
	}

	kind := detectKind(validPath)
	artifact, err := rc.PutArtifact(ctx, file, kind, []string{"fs_read"})
	if err != nil {
		return skillerr.WrapIO("cas put "+validPath, err)
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return skillerr.WrapIO("rewind "+validPath, err)
	}

	limit := previewLimit(rc, in)
	previewBytes, more, err := readPreview(file, limit)
	if err != nil {
		return err
	}

	isText := utf8.Valid(previewBytes)
	lineCount := 0
	if isText {
		lineCount = textutil.CountLinesBytes(previewBytes)
	}

	data := map[string]any{
		"path":              validPath,
		"size_bytes":        artifact.Size,
		"mode":              info.Mode().String(),
		"mod_time":          info.ModTime().UTC().Format(time.RFC3339),
		"max_preview_bytes": limit,
		"preview_bytes":     len(previewBytes),
		"binary":            !isText,
		"artifact":          artifact.Digest,
		"truncated":         more || artifact.Size > int64(len(previewBytes)),
	}
	data["cas_hint"] = skillout.DefaultCASHint(artifact)
	if isText {
		preview := string(previewBytes)
		numbered := formatPreviewWithLineNumbers(preview, lineCount)
		data["preview"] = preview
		data["preview_raw"] = preview
		data["preview_line_count"] = lineCount
		data["preview_numbered"] = numbered
	}
	if !isText {
		data["hint"] = "content stored in CAS; fetch via foxctl cas get <digest>"
	}
	data["summary"] = fmt.Sprintf("Read %d bytes from %s", artifact.Size, filepath.Base(validPath))

	return skillout.Emit(rc, "fs/read", data)
}

// previewLimit determines the maximum bytes to preview based on config and input.
func previewLimit(rc *skillmain.RunContext, in Input) int {
	maxInline := rc.InlineKB * 1024
	if maxInline <= 0 {
		maxInline = config.DefaultInlineOutputKB * 1024
	}
	if in.MaxBytes > 0 && in.MaxBytes < maxInline {
		return in.MaxBytes
	}
	return maxInline
}

// readPreview reads up to limit bytes from reader and indicates if more data exists.
func readPreview(r io.Reader, limit int) ([]byte, bool, error) {
	if limit <= 0 {
		limit = 1
	}
	buf := make([]byte, limit+1)
	n, err := io.ReadFull(r, buf)
	switch {
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return buf[:n], false, nil
	case err != nil:
		return nil, false, skillerr.WrapIO("read preview", err)
	default:
		return buf[:limit], n > limit, nil
	}
}

// formatPreviewWithLineNumbers adds line numbers to text preview.
//
// Index:
// - Purpose: Add line numbers to text preview
// - Flow: count lines → add line numbers → return formatted preview
// - SideEffects: none
// - FailureModes: none
// - Observability: none
// - Related: readPreview, detectKind
// - Keywords: format_preview, line_numbers
func formatPreviewWithLineNumbers(preview string, lineCount int) string {
	if preview == "" {
		return ""
	}
	width := len(strconv.Itoa(lineCount))
	if width == 0 {
		width = 1
	}

	var out strings.Builder
	reader := bufio.NewReader(strings.NewReader(preview))
	lineNum := 1
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			hasNewline := strings.HasSuffix(line, "\n")
			if hasNewline {
				line = strings.TrimSuffix(line, "\n")
			}
			fmt.Fprintf(&out, "%*d | %s", width, lineNum, line)
			if hasNewline {
				out.WriteString("\n")
			}
			lineNum++
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
	}
	return out.String()
}

// detectKind determines the MIME type based on file extension.
//
// Index:
// - Purpose: Determine the MIME type based on file extension
// - Flow: check extension → return MIME type
// - SideEffects: none
// - FailureModes: none
// - Observability: none
// - Related: readPreview, formatPreviewWithLineNumbers
// - Keywords: detect_kind, mime_type, extension
func detectKind(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md":
		return "text/markdown; charset=utf-8"
	case ".json":
		return "application/json"
	case ".yaml", ".yml":
		return "application/yaml"
	}
	if ext != "" {
		if kind := mime.TypeByExtension(ext); kind != "" {
			if strings.HasPrefix(kind, "text/") && !strings.Contains(kind, "charset") {
				return kind + "; charset=utf-8"
			}
			return kind
		}
	}
	return "text/plain; charset=utf-8"
}
