package codecontext

import "fmt"

// RenderFunc serializes evidence for CAS artifacts.
//
// Keep this as a function type to make output preparation reusable in consumers
// that may want to persist JSON, NDJSON, or other artifact formats.
type RenderFunc func(*Evidence) ([]byte, error)

// ArtifactPayload stores bytes that should be written to CAS.
//
// Digest assignment stays in the caller after persistence.
//
// Kind should be a MIME-like type such as "application/x-ndjson".
type ArtifactPayload struct {
	Data  []byte `json:"-"`
	Kind  string `json:"kind"`
	Count int    `json:"count,omitempty"`
}

// PrepareOutputWithArtifact prepares output while deciding whether
// to return inline previews or a materialized artifact payload.
//
// If total snippet text fits in inlineKB, SnippetsInline is populated and
// no artifact is returned. Otherwise, it returns an artifact payload.
//
// This helper is intentionally reusable for multiple codecontext callers.
func PrepareOutputWithArtifact(
	evidence *Evidence,
	inlineKB int,
	previewBytes int,
	render RenderFunc,
) (*OutputPayload, *ArtifactPayload, error) {
	if evidence == nil {
		return &OutputPayload{}, nil, nil
	}

	output := &OutputPayload{
		Query:     evidence.Query,
		Stats:     evidence.Stats,
		Truncated: evidence.Truncated,
		Warnings:  evidence.Warnings,
	}

	totalBytes := 0
	for _, snippet := range evidence.Snippets {
		totalBytes += len(snippet.Text)
	}

	inlineLimit := inlineKB * 1024
	if inlineLimit <= 0 {
		inlineLimit = 32 * 1024
	}

	if render == nil {
		render = RenderNDJSON
	}
	artifactData, err := render(evidence)
	if err != nil {
		return nil, nil, fmt.Errorf("prepare artifact: %w", err)
	}

	if totalBytes <= inlineLimit && len(artifactData) <= inlineLimit {
		output.SnippetsInline = MakePreviews(evidence.Snippets, previewBytes)
		addOutputHints(output)
		return output, nil, nil
	}

	addOutputHints(output)

	return output, &ArtifactPayload{
		Data:  artifactData,
		Kind:  "application/x-ndjson",
		Count: len(evidence.Snippets),
	}, nil
}

func addOutputHints(output *OutputPayload) {
	if output.Truncated {
		output.Hints = append(output.Hints, "Results were truncated. Consider narrowing the query or increasing limits.")
	}
	if output.Stats.FilesSkipped > 0 {
		output.Hints = append(output.Hints, fmt.Sprintf("%d files could not be read. Check file errors for details.", output.Stats.FilesSkipped))
	}
}
