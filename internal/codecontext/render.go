package codecontext

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// RenderOpts configures evidence rendering.
type RenderOpts struct {
	// Mode determines the output format.
	Mode RenderMode

	// MaxPreviewBytes limits preview text size for inline output.
	MaxPreviewBytes int

	// IncludeStats includes extraction statistics in output.
	IncludeStats bool
}

// Render formats evidence for output based on the render mode.
//
// Output formats:
//   - ModeSnippets: Markdown code blocks with file paths and line numbers
//   - ModeMasked: Full file with irrelevant sections marked as [...] (TODO: Phase 2)
//   - ModeStructure: Only signatures and imports (TODO: Phase 2)
//   - ModeFlow: Control-flow excerpts (TODO: Phase 2)
//
// Render formats evidence according to the selected render mode.
//
// Index:
// - Purpose: Render collected evidence into a user-facing format
// - Flow: apply defaults → switch on mode → render snippets
// - Related: RenderNDJSON, RenderJSON
// - Keywords: render, evidence, snippets, mode, output
func Render(evidence *Evidence, opts RenderOpts) string {
	if evidence == nil {
		return ""
	}

	if opts.Mode == "" {
		opts.Mode = ModeSnippets
	}

	switch opts.Mode {
	case ModeSnippets:
		return renderSnippets(evidence, opts)
	case ModeMasked:
		// TODO: Implement in Phase 2 with proper block expansion
		return renderSnippets(evidence, opts) // Fallback
	case ModeStructure:
		// TODO: Implement in Phase 2
		return renderSnippets(evidence, opts) // Fallback
	case ModeFlow:
		// TODO: Implement in Phase 2
		return renderSnippets(evidence, opts) // Fallback
	default:
		return renderSnippets(evidence, opts)
	}
}

// renderSnippets formats snippets as markdown code blocks.
func renderSnippets(evidence *Evidence, opts RenderOpts) string {
	var buf strings.Builder

	for i, snippet := range evidence.Snippets {
		if i > 0 {
			buf.WriteString("\n")
		}

		// Header with file path and line range
		if snippet.StartLine == snippet.EndLine {
			buf.WriteString(fmt.Sprintf("### %s:%d\n", snippet.File, snippet.StartLine))
		} else {
			buf.WriteString(fmt.Sprintf("### %s:%d-%d\n", snippet.File, snippet.StartLine, snippet.EndLine))
		}

		// Code block with language hint
		lang := snippet.Language
		if lang == "" || lang == "text" {
			lang = ""
		}
		buf.WriteString(fmt.Sprintf("```%s\n", lang))
		buf.WriteString(snippet.Text)
		if !strings.HasSuffix(snippet.Text, "\n") {
			buf.WriteString("\n")
		}
		buf.WriteString("```\n")
	}

	if opts.IncludeStats {
		buf.WriteString("\n---\n")
		buf.WriteString(fmt.Sprintf("**Stats:** %d files, %d snippets, %d bytes\n",
			evidence.Stats.FilesProcessed,
			evidence.Stats.SnippetsExtracted,
			evidence.Stats.TotalBytes))
		if evidence.Truncated {
			buf.WriteString("*Results truncated due to limits*\n")
		}
	}

	return buf.String()
}

// RenderNDJSON formats evidence as newline-delimited JSON for CAS artifacts.
// Each snippet is serialized as a separate JSON line.
// RenderNDJSON renders evidence snippets as newline-delimited JSON.
func RenderNDJSON(evidence *Evidence) ([]byte, error) {
	var buf bytes.Buffer

	for _, snippet := range evidence.Snippets {
		data, err := json.Marshal(snippet)
		if err != nil {
			return nil, fmt.Errorf("marshal snippet: %w", err)
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}

	return buf.Bytes(), nil
}

// RenderJSON formats evidence as a single JSON object.
// RenderJSON renders evidence as a JSON object.
func RenderJSON(evidence *Evidence) ([]byte, error) {
	return json.Marshal(evidence)
}

// SnippetPreview is a truncated version of Snippet for inline responses.
type SnippetPreview struct {
	File      string  `json:"file"`
	SymbolID  string  `json:"symbol_id,omitempty"`
	StartLine int     `json:"start_line"`
	EndLine   int     `json:"end_line"`
	Preview   string  `json:"preview"`
	Priority  float64 `json:"priority,omitempty"`
	Language  string  `json:"language,omitempty"`
}

// MakePreviews creates truncated previews of snippets for inline output.
func MakePreviews(snippets []Snippet, maxBytes int) []SnippetPreview {
	if maxBytes <= 0 {
		maxBytes = 512
	}

	previews := make([]SnippetPreview, len(snippets))
	for i, s := range snippets {
		preview := s.Text
		if len(preview) > maxBytes {
			preview = preview[:maxBytes] + "..."
		}

		previews[i] = SnippetPreview{
			File:      s.File,
			SymbolID:  s.SymbolID,
			StartLine: s.StartLine,
			EndLine:   s.EndLine,
			Preview:   preview,
			Priority:  s.Priority,
			Language:  s.Language,
		}
	}

	return previews
}

// OutputPayload is the standard output structure for skills using codecontext.
type OutputPayload struct {
	// Query is the original question (for context).
	Query string `json:"query,omitempty"`

	// SnippetsInline contains truncated snippet previews.
	// Included when total size is below inline threshold.
	SnippetsInline []SnippetPreview `json:"snippets_inline,omitempty"`

	// Artifact contains the CAS digest for full snippets.
	// Used when total size exceeds inline threshold.
	Artifact *ArtifactRef `json:"artifact,omitempty"`

	// Stats contains extraction statistics.
	Stats EvidenceStats `json:"stats"`

	// Truncated indicates results were limited.
	Truncated bool `json:"truncated,omitempty"`

	// Hints provides guidance to the AI model.
	Hints []string `json:"hints,omitempty"`
}

// ArtifactRef references a CAS-stored artifact.
type ArtifactRef struct {
	// Digest is the content-addressable hash (e.g., "sha256:...").
	Digest string `json:"digest"`

	// Size is the artifact size in bytes.
	Size int64 `json:"size"`

	// Kind is the MIME type or artifact kind.
	Kind string `json:"kind"`

	// Count is the number of items in the artifact (e.g., snippet count).
	Count int `json:"count,omitempty"`
}

// PrepareOutput creates an OutputPayload from evidence, deciding between
// inline and artifact storage based on size.
//
// If totalBytes exceeds inlineKB*1024, snippets should be stored as a CAS
// artifact and referenced. Otherwise, previews are included inline.
//
// The caller is responsible for persisting the artifact if needed.
func PrepareOutput(evidence *Evidence, inlineKB int, previewBytes int) *OutputPayload {
	if evidence == nil {
		return &OutputPayload{}
	}

	output := &OutputPayload{
		Query:     evidence.Query,
		Stats:     evidence.Stats,
		Truncated: evidence.Truncated,
	}

	// Calculate total size
	totalBytes := 0
	for _, s := range evidence.Snippets {
		totalBytes += len(s.Text)
	}

	// Decide inline vs artifact
	inlineLimit := inlineKB * 1024
	if inlineLimit <= 0 {
		inlineLimit = 32 * 1024 // 32KB default
	}

	if totalBytes <= inlineLimit {
		// Include inline previews
		output.SnippetsInline = MakePreviews(evidence.Snippets, previewBytes)
	}
	// Note: If totalBytes > inlineLimit, caller should:
	// 1. Call RenderNDJSON(evidence)
	// 2. Store in CAS
	// 3. Set output.Artifact

	// Add hints based on evidence
	if evidence.Truncated {
		output.Hints = append(output.Hints, "Results were truncated. Consider narrowing the query or increasing limits.")
	}
	if evidence.Stats.FilesSkipped > 0 {
		output.Hints = append(output.Hints, fmt.Sprintf("%d files could not be read. Check file errors for details.", evidence.Stats.FilesSkipped))
	}

	return output
}
