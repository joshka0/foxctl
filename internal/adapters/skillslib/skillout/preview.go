package skillout

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillcas"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
)

// PreviewOpts configures preview output behavior.
type PreviewOpts struct {
	// MaxItems limits the number of items to include.
	// 0 means no limit.
	MaxItems int

	// MaxBytes limits the total output size in bytes.
	// 0 means no limit.
	MaxBytes int

	// TruncateMsg is appended when output is truncated.
	// Defaults to "... (truncated)"
	TruncateMsg string
}

// DefaultPreviewOpts returns sensible default preview options.
func DefaultPreviewOpts() PreviewOpts {
	return PreviewOpts{
		MaxItems:    100,
		MaxBytes:    32 * 1024, // 32KB
		TruncateMsg: "... (truncated)",
	}
}

// EmitPreview writes items as NDJSON (newline-delimited JSON) for streaming results.
// Each item is written as a separate JSON line.
// Returns the number of items written and whether output was truncated.
func EmitPreview(w io.Writer, items []any, opts PreviewOpts) (int, bool, error) {
	if opts.TruncateMsg == "" {
		opts.TruncateMsg = "... (truncated)"
	}

	var totalBytes int
	var written int
	truncated := false

	for i, item := range items {
		// Check item limit
		if opts.MaxItems > 0 && i >= opts.MaxItems {
			truncated = true
			break
		}

		// Marshal item
		line, err := json.Marshal(item)
		if err != nil {
			return written, truncated, fmt.Errorf("marshal item %d: %w", i, err)
		}

		// Check byte limit
		lineLen := len(line) + 1 // +1 for newline
		if opts.MaxBytes > 0 && totalBytes+lineLen > opts.MaxBytes {
			truncated = true
			break
		}

		// Write line
		if _, err := w.Write(line); err != nil {
			return written, truncated, fmt.Errorf("write item %d: %w", i, err)
		}
		if _, err := w.Write([]byte("\n")); err != nil {
			return written, truncated, fmt.Errorf("write newline %d: %w", i, err)
		}

		totalBytes += lineLen
		written++
	}

	// Write truncation message if needed
	if truncated && opts.TruncateMsg != "" {
		msg := map[string]any{
			"_truncated": true,
			"_message":   opts.TruncateMsg,
			"_total":     len(items),
			"_shown":     written,
		}
		line, _ := json.Marshal(msg)
		_, _ = w.Write(line)
		_, _ = w.Write([]byte("\n"))
	}

	return written, truncated, nil
}

// PreparePreview truncates a slice to maxItems and returns whether it was truncated.
// This is useful for preparing data before emitting.
func PreparePreview[T any](items []T, maxItems int) ([]T, bool) {
	if maxItems <= 0 || len(items) <= maxItems {
		return items, false
	}
	return items[:maxItems], true
}

// PreviewResult holds the result of a preview operation.
type PreviewResult[T any] struct {
	Items     []T  `json:"items"`
	Total     int  `json:"total"`
	Shown     int  `json:"shown"`
	Truncated bool `json:"truncated,omitempty"`
}

// NewPreviewResult creates a preview result from a slice.
func NewPreviewResult[T any](items []T, maxItems int) PreviewResult[T] {
	shown, truncated := PreparePreview(items, maxItems)
	return PreviewResult[T]{
		Items:     shown,
		Total:     len(items),
		Shown:     len(shown),
		Truncated: truncated,
	}
}

// TruncateString truncates a string to maxLen, adding ellipsis if truncated.
func TruncateString(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// TruncateStringWithSuffix truncates a string to maxLen and appends suffix when truncated.
func TruncateStringWithSuffix(s string, maxLen int, suffix string) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + suffix
}

// TruncateSingleLine trims whitespace, replaces newlines, and truncates with ellipsis.
func TruncateSingleLine(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	return TruncateString(s, maxLen)
}

// TruncateRunes truncates a string by rune count, adding ellipsis if truncated.
func TruncateRunes(s string, maxLen int) string {
	if maxLen <= 3 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}

// PreviewArtifact holds the result of a preview-and-persist operation.
type PreviewArtifact[T any] struct {
	// Preview contains the first N items for inline display.
	Preview []T `json:"preview"`

	// Total is the total number of items.
	Total int `json:"total"`

	// Truncated is true if there are more items than the preview.
	Truncated bool `json:"truncated,omitempty"`

	// Artifact contains the CAS reference if items were persisted.
	// Nil if persist=false or total <= previewLimit.
	Artifact *skillcas.Artifact `json:"artifact,omitempty"`
}

// PreviewAndPersistNDJSON previews items inline and optionally persists all items to CAS as NDJSON.
// This is the recommended pattern for skills that return large result sets.
//
// Parameters:
//   - ctx: context for CAS operations
//   - rc: run context with CAS store
//   - items: all items to process
//   - previewLimit: maximum items to include in preview (0 uses rc.MaxPreview)
//   - artifactName: name for the CAS artifact tags
//   - persist: if true and total > previewLimit, store all items in CAS
//
// Returns:
//   - PreviewArtifact containing preview items and optional CAS reference
//   - error if CAS persistence fails
//
// Example usage in a skill:
//
//	result, err := skillout.PreviewAndPersistNDJSON(ctx, rc, matches, 50, "ripgrep-matches", true)
//	if err != nil {
//	    return err
//	}
//	return skillout.Emit(rc, "code/ripgrep", map[string]any{
//	    "matches":   result.Preview,
//	    "total":     result.Total,
//	    "truncated": result.Truncated,
//	    "artifact":  result.Artifact,
//	})
func PreviewAndPersistNDJSON[T any](
	ctx context.Context,
	rc *skillmain.RunContext,
	items []T,
	previewLimit int,
	artifactName string,
	persist bool,
) (PreviewArtifact[T], error) {
	// Apply default preview limit
	if previewLimit <= 0 {
		previewLimit = rc.MaxPreview
	}
	var writer skillcas.Writer
	// NoCAS disables automatic output truncation, but result-set artifacts are
	// explicit skill output and should still be persisted when CAS is configured.
	if rc != nil && rc.Config.CAS.Store && rc.CASStore != nil {
		writer = rc
	}
	return PreviewAndPersistNDJSONContext(ctx, writer, items, previewLimit, artifactName, persist)
}

// PreviewAndPersistNDJSONContext previews items inline and optionally persists all items through a CAS writer.
func PreviewAndPersistNDJSONContext[T any](
	ctx context.Context,
	writer skillcas.Writer,
	items []T,
	previewLimit int,
	artifactName string,
	persist bool,
) (PreviewArtifact[T], error) {
	if previewLimit <= 0 {
		previewLimit = 100 // fallback default
	}

	total := len(items)
	truncated := total > previewLimit

	// Prepare preview
	preview := items
	if truncated {
		preview = items[:previewLimit]
	}

	result := PreviewArtifact[T]{
		Preview:   preview,
		Total:     total,
		Truncated: truncated,
	}

	// Persist to CAS if needed
	if persist && truncated && writer != nil {
		artifact, err := persistNDJSON(ctx, writer, items, artifactName)
		if err != nil {
			return result, fmt.Errorf("persist NDJSON to CAS: %w", err)
		}
		result.Artifact = &artifact
	}

	return result, nil
}

// AddArtifact adds the artifact digest to the data map when present.
func AddArtifact(data map[string]any, artifact *skillcas.Artifact) {
	if artifact != nil && artifact.Digest != "" {
		data["artifact"] = artifact.Digest
	}
}

// persistNDJSON writes items as NDJSON to CAS.
func persistNDJSON[T any](
	ctx context.Context,
	writer skillcas.Writer,
	items []T,
	artifactName string,
) (skillcas.Artifact, error) {
	var buf bytes.Buffer

	for _, item := range items {
		line, err := json.Marshal(item)
		if err != nil {
			return skillcas.Artifact{}, fmt.Errorf("marshal item: %w", err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}

	return skillcas.PersistBuffer(ctx, writer, &buf, "application/x-ndjson", artifactName)
}
