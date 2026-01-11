// Package main implements the fs/read skill.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

// Input defines the input parameters for fs/read.
type Input struct {
	Path     string `json:"path" validate:"required"`
	MaxBytes int    `json:"max_bytes" validate:"gte=0"`
}

func main() {
	skillmain.Main("fs/read", run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	validPath, err := rc.PathValidator.ValidatePath(in.Path)
	if err != nil {
		return fmt.Errorf("path validation failed: %w", err)
	}

	info, err := os.Stat(validPath)
	if err != nil {
		return fmt.Errorf("stat %s: %w", validPath, err)
	}
	if info.IsDir() {
		return fmt.Errorf("path %s is a directory", validPath)
	}

	file, err := os.Open(validPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", validPath, err)
	}
	defer func() {
		errs.Ignore(file.Close(), "close input file")
	}()

	kind := detectKind(validPath)
	obj, err := rc.CASStore.Put(ctx, file, kind, []string{"fs_read"})
	if err != nil {
		return fmt.Errorf("cas put %s: %w", validPath, err)
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind %s: %w", validPath, err)
	}

	limit := previewLimit(rc, in)
	previewBytes, more, err := readPreview(file, limit)
	if err != nil {
		return err
	}

	isText := utf8.Valid(previewBytes)
	lineCount := 0
	if isText {
		lineCount = countLines(previewBytes)
	}

	data := map[string]any{
		"path":              validPath,
		"size_bytes":        obj.Size,
		"mode":              info.Mode().String(),
		"mod_time":          info.ModTime().UTC().Format(time.RFC3339),
		"max_preview_bytes": limit,
		"preview_bytes":     len(previewBytes),
		"binary":            !isText,
		"artifact":          obj.Digest,
		"truncated":         more || obj.Size > int64(len(previewBytes)),
	}
	if isText {
		data["preview"] = string(previewBytes)
		data["preview_line_count"] = lineCount
	}
	if !isText {
		data["hint"] = "content stored in CAS; fetch via agentctl cas get <digest>"
	}
	data["summary"] = fmt.Sprintf("Read %d bytes from %s", obj.Size, filepath.Base(validPath))

	return skillout.Emit(rc, "fs/read", data)
}

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
		return nil, false, fmt.Errorf("read preview: %w", err)
	default:
		return buf[:limit], n > limit, nil
	}
}

func countLines(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	lines := bytes.Count(b, []byte{'\n'})
	if len(b) > 0 && b[len(b)-1] != '\n' {
		lines++
	}
	return lines
}

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
