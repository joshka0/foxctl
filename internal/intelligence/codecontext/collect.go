package codecontext

import (
	"context"
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/intelligence/codecontext/files"
	"github.com/joshka0/foxctl/internal/intelligence/codecontext/guard"
)

// Collect is the single extraction engine for retrieval-backed code evidence.
//
// Pipeline:
//  1. Normalize options / query
//  2. Merge duplicate file candidates
//  3. Safe-read files
//  4. Secret-scan files
//  5. Generate snippet proposals (anchors, query matches, fallback)
//  6. Score + dedupe proposals
//  7. Return ranked evidence
func Collect(ctx context.Context, opts CollectOpts) (*Evidence, error) {
	opts = normalizeCollectOpts(opts)

	if opts.PathValidator == nil {
		return nil, &CollectError{Message: "path validator is required"}
	}

	plan := ParseQuery(opts.Query)
	candidates := mergeCandidates(opts.Candidates, opts.MaxAnchorsPerFile)

	reader := files.NewSafeReader(opts.PathValidator, opts.MaxBytesPerFile)
	scanner := guard.New(guard.Opts{Mode: opts.SecretMode})

	evidence := &Evidence{
		Query:    opts.Query,
		Snippets: []Snippet{},
		Stats: EvidenceStats{
			FileErrors: []FileError{},
		},
		Warnings: []string{},
	}

	var proposals []snippetProposal
	filesSeen := 0

	for _, cand := range candidates {
		if ctx.Err() != nil {
			break
		}
		if cand.Path == "" {
			continue
		}
		if filesSeen >= opts.MaxFiles {
			evidence.Truncated = true
			break
		}
		filesSeen++

		content, err := reader.Read(ctx, cand.Path)
		if err != nil {
			evidence.Stats.FilesSkipped++
			evidence.Stats.FileErrors = append(evidence.Stats.FileErrors, classifyReadErr(cand.Path, err))
			continue
		}

		scan := scanner.Scan(ctx, cand.Path, content.Content)
		if scan.Blocked {
			evidence.Stats.FilesSkipped++
			evidence.Stats.FileErrors = append(evidence.Stats.FileErrors, FileError{
				Path:    cand.Path,
				Code:    "EPOLICY",
				Message: scan.Reason,
			})
			continue
		}

		evidence.Stats.FilesProcessed++
		evidence.Stats.TotalBytes += int64(len(content.Content))
		if len(scan.Findings) > 0 {
			evidence.Warnings = append(evidence.Warnings,
				fmt.Sprintf("%s: %d potential secrets detected", cand.Path, len(scan.Findings)))
		}

		fileProps := proposeForFile(content, cand, plan, opts)
		proposals = append(proposals, fileProps...)
	}

	final := finalizeProposals(proposals, opts.MaxSnippets)
	evidence.Snippets = final
	evidence.Stats.SnippetsExtracted = len(final)

	if len(proposals) > len(final) {
		evidence.Truncated = true
	}
	evidence.Warnings = trimAndDedupWarnings(evidence.Warnings)

	return evidence, nil
}

// CollectError represents an error during evidence collection.
type CollectError struct {
	Message string
}

func (e *CollectError) Error() string {
	return e.Message
}

func mergeCandidates(in []Candidate, maxAnchors int) []Candidate {
	type acc struct {
		c Candidate
	}

	byPath := map[string]*acc{}

	for _, c := range in {
		if strings.TrimSpace(c.Path) == "" {
			continue
		}
		c = normalizeLegacyCandidate(c)

		a, ok := byPath[c.Path]
		if !ok {
			cc := c
			a = &acc{c: cc}
			byPath[c.Path] = a
			continue
		}

		if c.Priority > a.c.Priority {
			a.c.Priority = c.Priority
		}
		if a.c.Summary == "" && c.Summary != "" {
			a.c.Summary = c.Summary
		}
		a.c.Anchors = append(a.c.Anchors, c.Anchors...)
	}

	out := make([]Candidate, 0, len(byPath))
	for _, a := range byPath {
		a.c.Anchors = dedupeAnchors(a.c.Anchors)
		sortAnchorsByScore(a.c.Anchors)
		if maxAnchors > 0 && len(a.c.Anchors) > maxAnchors {
			a.c.Anchors = a.c.Anchors[:maxAnchors]
		}
		out = append(out, a.c)
	}

	sortCandidatesByPriority(out)
	return out
}
