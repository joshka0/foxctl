package codecontext

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// RenderOpts configures evidence rendering.
type RenderOpts struct {
	Mode            RenderMode
	MaxPreviewBytes int
	IncludeStats    bool
}

// Render formats evidence for output based on the render mode.
func Render(evidence *Evidence, opts RenderOpts) string {
	if evidence == nil {
		return ""
	}
	if opts.Mode == "" {
		opts.Mode = ModeSnippets
	}

	switch opts.Mode {
	case ModeSnippets, ModeMasked, ModeStructure, ModeFlow:
		return renderSnippets(evidence, opts)
	default:
		return renderSnippets(evidence, opts)
	}
}

func renderSnippets(evidence *Evidence, opts RenderOpts) string {
	var buf strings.Builder

	for i, snippet := range evidence.Snippets {
		if i > 0 {
			buf.WriteString("\n")
		}

		if snippet.StartLine == snippet.EndLine {
			buf.WriteString(fmt.Sprintf("### %s:%d\n", snippet.File, snippet.StartLine))
		} else {
			buf.WriteString(fmt.Sprintf("### %s:%d-%d\n", snippet.File, snippet.StartLine, snippet.EndLine))
		}

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
func RenderJSON(evidence *Evidence) ([]byte, error) {
	return json.Marshal(evidence)
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

// PrepareOutput creates an OutputPayload from evidence, deciding between
// inline and artifact storage based on size.
func PrepareOutput(evidence *Evidence, inlineKB int, previewBytes int) *OutputPayload {
	if evidence == nil {
		return &OutputPayload{}
	}

	output := &OutputPayload{
		Query:     evidence.Query,
		Stats:     evidence.Stats,
		Truncated: evidence.Truncated,
		Warnings:  evidence.Warnings,
	}

	totalBytes := 0
	for _, s := range evidence.Snippets {
		totalBytes += len(s.Text)
	}

	inlineLimit := inlineKB * 1024
	if inlineLimit <= 0 {
		inlineLimit = 32 * 1024
	}

	if totalBytes <= inlineLimit {
		output.SnippetsInline = MakePreviews(evidence.Snippets, previewBytes)
	}

	if evidence.Truncated {
		output.Hints = append(output.Hints, "Results were truncated. Consider narrowing the query or increasing limits.")
	}
	if evidence.Stats.FilesSkipped > 0 {
		output.Hints = append(output.Hints, fmt.Sprintf("%d files could not be read. Check file errors for details.", evidence.Stats.FilesSkipped))
	}

	return output
}
