package embeddingtext

import (
	"strings"
	"unicode"
)

// NormalizeDoc cleans documentation text for embedding/search.
//
// Goals:
//   - Strip common comment markers (//, /* */, leading "*", "#", "--") when present
//   - Normalize newlines to "\n"
//   - Preserve paragraph boundaries (blank lines), but "unwrap" line-wrapped paragraphs
//     by joining consecutive non-empty lines with spaces.
//   - Preserve fenced code blocks (``` or ~~~) verbatim (no line-joining inside fences)
//   - Collapse runs of spaces/tabs (outside fenced blocks)
//   - Collapse repeated blank lines and trim leading/trailing blanks
//
// This is safe to run on raw comment strings OR on ast.CommentGroup.Text() output.
// It should be stable across formatting-only edits.
func NormalizeDoc(raw string) string {
	s := normalizeNewlines(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}

	s, blockStyle := stripBlockWrappers(s)

	lines := strings.Split(s, "\n")

	var out []string
	var para []string

	inFence := false
	fence := ""

	flushPara := func() {
		if len(para) == 0 {
			return
		}
		joined := strings.TrimSpace(strings.Join(para, " "))
		if joined != "" {
			out = append(out, joined)
		}
		para = para[:0]
	}

	appendBlank := func() {
		flushPara()
		// Only one blank line between paragraphs.
		if len(out) > 0 && out[len(out)-1] != "" {
			out = append(out, "")
		}
	}

	for _, line := range lines {
		// Remove trailing whitespace early.
		line = strings.TrimRight(line, " \t")

		// Strip comment markers if present (keepIndent=false for docs).
		line = stripLineCommentPrefix(line, blockStyle, false)

		// Fence toggle is checked AFTER stripping comment markers.
		if marker, ok := fenceMarker(line); ok {
			flushPara()
			out = append(out, markerLineCanonical(line))

			if !inFence {
				inFence = true
				fence = marker
			} else if marker == fence {
				inFence = false
				fence = ""
			}
			continue
		}

		if inFence {
			// Inside fenced blocks: preserve lines (but trim trailing whitespace).
			out = append(out, line)
			continue
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			appendBlank()
			continue
		}

		// Outside fences: collapse runs of spaces/tabs within the line.
		clean := collapseSpacesTabs(trimmed)
		if clean != "" {
			para = append(para, clean)
		}
	}

	flushPara()

	out = trimAndCollapseBlankLines(out)
	return strings.Join(out, "\n")
}

// NormalizeFirstComment normalizes the first file comment block / header comment.
//
// Differences vs NormalizeDoc:
// - Preserves line structure (does NOT unwrap paragraphs)
// - Preserves indentation after the comment marker (useful for nested lists)
// - Still preserves fenced blocks verbatim
// - Collapses repeated blank lines and trims leading/trailing blanks
func NormalizeFirstComment(raw string) string {
	s := normalizeNewlines(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}

	s, blockStyle := stripBlockWrappers(s)
	lines := strings.Split(s, "\n")

	var out []string
	inFence := false
	fence := ""

	for _, line := range lines {
		line = strings.TrimRight(line, " \t")

		// keepIndent=true for file headers (preserve indentation after marker).
		line = stripLineCommentPrefix(line, blockStyle, true)

		if marker, ok := fenceMarker(line); ok {
			out = append(out, markerLineCanonical(line))
			if !inFence {
				inFence = true
				fence = marker
			} else if marker == fence {
				inFence = false
				fence = ""
			}
			continue
		}

		if inFence {
			out = append(out, line)
			continue
		}

		// For headers: collapse interior spaces/tabs, but preserve leading indentation.
		line = collapseSpacesTabsPreserveLeading(line)
		out = append(out, strings.TrimRight(line, " \t"))
	}

	out = trimAndCollapseBlankLines(out)
	return strings.Join(out, "\n")
}

// NormalizeForDigest canonicalizes text for hashing.
//
// Unlike NormalizeDoc, this does NOT unwrap paragraphs; it preserves line boundaries.
// It:
// - normalizes newlines
// - trims trailing whitespace per line
// - collapses spaces/tabs within each line (preserving leading indentation)
// - collapses repeated blank lines
// - trims leading/trailing blank lines
//
// Use this on already-constructed embedding text (fielded "Kind:", "Signature:", etc.).
func NormalizeForDigest(raw string) string {
	s := normalizeNewlines(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		ln = strings.TrimRight(ln, " \t")
		ln = collapseSpacesTabsPreserveLeading(ln)
		out = append(out, ln)
	}
	out = trimAndCollapseBlankLines(out)
	return strings.Join(out, "\n")
}

//
// Helpers
//

func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

// stripBlockWrappers removes /* */ wrapper if present and returns (cleaned, blockStyle).
// blockStyle indicates we should strip leading '*' decoration on subsequent lines.
func stripBlockWrappers(s string) (string, bool) {
	t := strings.TrimSpace(s)
	blockStyle := false
	if strings.HasPrefix(t, "/*") {
		blockStyle = true
		t = strings.TrimPrefix(t, "/*")
		// common style: /*\n ... */
		t = strings.TrimLeft(t, "\n")
	}
	if strings.HasSuffix(t, "*/") {
		blockStyle = true
		t = strings.TrimSuffix(t, "*/")
	}
	return strings.TrimSpace(t), blockStyle
}

// stripLineCommentPrefix removes a leading comment marker if present.
// If keepIndent=true, preserves indentation after the comment marker (useful for lists).
func stripLineCommentPrefix(line string, blockStyle bool, keepIndent bool) string {
	orig := line

	// Keep any indentation before the marker if keepIndent is requested.
	prefixIndent := ""
	if keepIndent {
		trimmedLeft := strings.TrimLeft(orig, " \t")
		prefixIndent = orig[:len(orig)-len(trimmedLeft)]
		line = trimmedLeft
	} else {
		line = strings.TrimLeft(orig, " \t")
	}

	// Line comment styles.
	switch {
	case strings.HasPrefix(line, "//"):
		line = line[2:]
		line = stripOneLeadingSpace(line)
		return prefixIndent + line
	case strings.HasPrefix(line, "#"):
		line = line[1:]
		line = stripOneLeadingSpace(line)
		return prefixIndent + line
	case strings.HasPrefix(line, "--") && (len(line) == 2 || line[2] == ' ' || line[2] == '\t'):
		line = line[2:]
		line = stripOneLeadingSpace(line)
		return prefixIndent + line
	}

	// Block comment decoration lines: leading '*' (only if we detected block wrapper).
	if blockStyle {
		l2 := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(l2, "*") {
			l2 = strings.TrimPrefix(l2, "*")
			l2 = stripOneLeadingSpace(l2)
			// If keepIndent, preserve indent after removing marker; we keep prefixIndent.
			return prefixIndent + l2
		}
	}

	return prefixIndent + line
}

// stripOneLeadingSpace removes a single leading space/tab if present.
func stripOneLeadingSpace(s string) string {
	if s == "" {
		return s
	}
	if s[0] == ' ' || s[0] == '\t' {
		return s[1:]
	}
	return s
}

// fenceMarker returns ("```" or "~~~", true) if the line opens/closes a fenced code block.
func fenceMarker(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if strings.HasPrefix(t, "```") {
		return "```", true
	}
	if strings.HasPrefix(t, "~~~") {
		return "~~~", true
	}
	return "", false
}

// markerLineCanonical trims trailing whitespace but otherwise preserves the fence line.
// We keep the original line (which may include language hint like ```go).
func markerLineCanonical(line string) string {
	return strings.TrimRight(line, " \t")
}

// collapseSpacesTabs collapses runs of spaces/tabs to a single space.
func collapseSpacesTabs(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return strings.TrimSpace(b.String())
}

// collapseSpacesTabsPreserveLeading collapses runs of spaces/tabs AFTER leading indentation.
// Leading indentation is preserved (spaces/tabs at the start).
func collapseSpacesTabsPreserveLeading(s string) string {
	if s == "" {
		return ""
	}

	// Preserve leading whitespace exactly.
	i := 0
	for i < len(s) {
		if s[i] != ' ' && s[i] != '\t' {
			break
		}
		i++
	}
	lead := s[:i]
	rest := s[i:]

	// Collapse spaces/tabs in rest.
	var b strings.Builder
	b.Grow(len(rest))
	prevSpace := false
	for _, r := range rest {
		if r == ' ' || r == '\t' {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}

	// Keep lead, but trim right on the whole line is handled by caller.
	return lead + strings.TrimRightFunc(b.String(), unicode.IsSpace)
}

// trimAndCollapseBlankLines removes leading/trailing blank lines and collapses consecutive blanks to one.
func trimAndCollapseBlankLines(lines []string) []string {
	// Trim leading blanks.
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	lines = lines[start:]

	// Trim trailing blanks.
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	lines = lines[:end]

	// Collapse consecutive blanks.
	out := make([]string, 0, len(lines))
	prevBlank := false
	for _, ln := range lines {
		isBlank := strings.TrimSpace(ln) == ""
		if isBlank {
			if prevBlank {
				continue
			}
			out = append(out, "")
			prevBlank = true
			continue
		}
		out = append(out, ln)
		prevBlank = false
	}
	return out
}
