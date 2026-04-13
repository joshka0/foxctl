package expander

import (
	"strings"
)

// BraceStyle defines the brace matching rules for a language.
type BraceStyle struct {
	// Open is the opening brace character (usually '{').
	Open rune

	// Close is the closing brace character (usually '}').
	Close rune

	// StringDelimiters are characters that start string literals.
	// Braces inside strings are ignored.
	StringDelimiters []rune

	// LineComment is the line comment prefix (e.g., "//").
	LineComment string

	// BlockCommentStart is the block comment start (e.g., "/*").
	BlockCommentStart string

	// BlockCommentEnd is the block comment end (e.g., "*/").
	BlockCommentEnd string

	// SupportsRawStrings indicates if the language has raw/template strings.
	SupportsRawStrings bool

	// RawStringDelimiter is the raw string delimiter (e.g., "`" in Go).
	RawStringDelimiter rune
}

// DefaultBraceStyle returns the standard C-like brace style.
func DefaultBraceStyle() BraceStyle {
	return BraceStyle{
		Open:               '{',
		Close:              '}',
		StringDelimiters:   []rune{'"', '\''},
		LineComment:        "//",
		BlockCommentStart:  "/*",
		BlockCommentEnd:    "*/",
		SupportsRawStrings: false,
	}
}

// GoBraceStyle returns the brace style for Go.
func GoBraceStyle() BraceStyle {
	return BraceStyle{
		Open:               '{',
		Close:              '}',
		StringDelimiters:   []rune{'"', '\''},
		LineComment:        "//",
		BlockCommentStart:  "/*",
		BlockCommentEnd:    "*/",
		SupportsRawStrings: true,
		RawStringDelimiter: '`',
	}
}

// JSBraceStyle returns the brace style for JavaScript/TypeScript.
func JSBraceStyle() BraceStyle {
	return BraceStyle{
		Open:               '{',
		Close:              '}',
		StringDelimiters:   []rune{'"', '\''},
		LineComment:        "//",
		BlockCommentStart:  "/*",
		BlockCommentEnd:    "*/",
		SupportsRawStrings: true,
		RawStringDelimiter: '`',
	}
}

// FindBraceEnd finds the line containing the closing brace that matches
// the opening brace on the given start line.
//
// Parameters:
//   - lines: the source code lines (0-indexed)
//   - startLine: the 0-indexed line containing the opening brace
//   - style: brace matching rules for the language
//
// Returns the 0-indexed line number of the closing brace, or -1 if not found.
func FindBraceEnd(lines []string, startLine int, style BraceStyle) int {
	if startLine < 0 || startLine >= len(lines) {
		return -1
	}

	braceCount := 0
	inString := false
	inRawString := false
	inBlockComment := false
	stringChar := rune(0)

	for lineIdx := startLine; lineIdx < len(lines); lineIdx++ {
		line := lines[lineIdx]

		for i := 0; i < len(line); i++ {
			ch := rune(line[i])

			// Check for block comment start
			if !inString && !inRawString && !inBlockComment {
				if style.BlockCommentStart != "" && i+len(style.BlockCommentStart) <= len(line) {
					if line[i:i+len(style.BlockCommentStart)] == style.BlockCommentStart {
						inBlockComment = true
						i += len(style.BlockCommentStart) - 1
						continue
					}
				}
			}

			// Check for block comment end
			if inBlockComment {
				if style.BlockCommentEnd != "" && i+len(style.BlockCommentEnd) <= len(line) {
					if line[i:i+len(style.BlockCommentEnd)] == style.BlockCommentEnd {
						inBlockComment = false
						i += len(style.BlockCommentEnd) - 1
					}
				}
				continue
			}

			// Check for line comment
			if !inString && !inRawString {
				if style.LineComment != "" && i+len(style.LineComment) <= len(line) {
					if line[i:i+len(style.LineComment)] == style.LineComment {
						break // Skip rest of line
					}
				}
			}

			// Check for raw string delimiter
			if style.SupportsRawStrings && ch == style.RawStringDelimiter {
				if !inString {
					inRawString = !inRawString
				}
				continue
			}

			// Skip if in raw string
			if inRawString {
				continue
			}

			// Check for string delimiters
			isStringDelim := false
			for _, delim := range style.StringDelimiters {
				if ch == delim {
					isStringDelim = true
					break
				}
			}

			if isStringDelim {
				if !inString {
					inString = true
					stringChar = ch
				} else if ch == stringChar {
					// Check for escape
					escapes := 0
					for j := i - 1; j >= 0 && line[j] == '\\'; j-- {
						escapes++
					}
					if escapes%2 == 0 {
						inString = false
					}
				}
				continue
			}

			// Skip if inside string
			if inString {
				continue
			}

			// Count braces
			switch ch {
			case style.Open:
				braceCount++
			case style.Close:
				braceCount--
				if braceCount == 0 {
					return lineIdx
				}
			}
		}
	}

	return -1
}

// FindBraceStart finds the line containing the opening brace that starts
// a block containing the given line.
//
// This searches backwards from the given line to find an unmatched opening brace.
// Returns the 0-indexed line number of the opening brace, or -1 if not found.
func FindBraceStart(lines []string, fromLine int, style BraceStyle) int {
	if fromLine < 0 || fromLine >= len(lines) {
		return -1
	}

	// Search backwards for unmatched opening brace
	braceCount := 0

	for lineIdx := fromLine; lineIdx >= 0; lineIdx-- {
		line := lines[lineIdx]

		// Process line in reverse
		inString := false
		stringChar := rune(0)

		// First, count braces normally to handle the line
		for i := len(line) - 1; i >= 0; i-- {
			ch := rune(line[i])

			// Simple string detection going backwards
			isStringDelim := false
			for _, delim := range style.StringDelimiters {
				if ch == delim {
					isStringDelim = true
					break
				}
			}

			if isStringDelim {
				if !inString {
					inString = true
					stringChar = ch
				} else if ch == stringChar {
					inString = false
				}
				continue
			}

			if inString {
				continue
			}

			// Skip comments (simplified - just skip if line starts with //)
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, style.LineComment) {
				break
			}

			switch ch {
			case style.Close:
				braceCount++
			case style.Open:
				braceCount--
				if braceCount < 0 {
					return lineIdx
				}
			}
		}
	}

	return -1
}

// CountLeadingWhitespace returns the number of leading whitespace characters.
func CountLeadingWhitespace(line string) int {
	count := 0
	for _, ch := range line {
		switch ch {
		case ' ':
			count++
		case '\t':
			count += 4 // Treat tab as 4 spaces
		default:
			return count
		}
	}
	return count
}

// IsBlankOrComment returns true if the line is blank or only contains a comment.
func IsBlankOrComment(line, lineComment string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return true
	}
	if lineComment != "" && strings.HasPrefix(trimmed, lineComment) {
		return true
	}
	return false
}

// FindBlockByIndentation finds the end of an indentation-based block.
// Used for Python, GDScript, and similar languages.
//
// Parameters:
//   - lines: source code lines (0-indexed)
//   - startLine: 0-indexed line containing the block header (def, class, etc.)
//   - lineComment: the line comment prefix (e.g., "#")
//
// Returns the 0-indexed line number of the last line in the block.
func FindBlockByIndentation(lines []string, startLine int, lineComment string) int {
	if startLine < 0 || startLine >= len(lines) {
		return startLine
	}

	// Get the indentation of the block header
	headerIndent := CountLeadingWhitespace(lines[startLine])

	// Find the first non-blank line after the header to determine body indent
	bodyIndent := -1
	for i := startLine + 1; i < len(lines); i++ {
		if !IsBlankOrComment(lines[i], lineComment) {
			bodyIndent = CountLeadingWhitespace(lines[i])
			break
		}
	}

	// If no body found, return the header line
	if bodyIndent < 0 || bodyIndent <= headerIndent {
		return startLine
	}

	// Find where indentation returns to header level or less
	lastContentLine := startLine
	for i := startLine + 1; i < len(lines); i++ {
		line := lines[i]

		// Skip blank lines and comments
		if IsBlankOrComment(line, lineComment) {
			continue
		}

		indent := CountLeadingWhitespace(line)
		if indent <= headerIndent {
			// We've exited the block
			break
		}

		lastContentLine = i
	}

	return lastContentLine
}

// FindBlockStartByIndentation finds the start of an indentation-based block
// that contains the given line.
//
// Returns the 0-indexed line number of the block header (def, class, etc.).
func FindBlockStartByIndentation(lines []string, fromLine int, headerPatterns []string, lineComment string) int {
	if fromLine < 0 || fromLine >= len(lines) {
		return -1
	}

	targetIndent := CountLeadingWhitespace(lines[fromLine])

	// Search backwards for a block header with less indentation
	for i := fromLine - 1; i >= 0; i-- {
		line := lines[i]

		if IsBlankOrComment(line, lineComment) {
			continue
		}

		indent := CountLeadingWhitespace(line)
		if indent >= targetIndent {
			continue
		}

		// Check if this line matches any header pattern
		trimmed := strings.TrimSpace(line)
		for _, pattern := range headerPatterns {
			if strings.HasPrefix(trimmed, pattern) {
				return i
			}
		}
	}

	return -1
}
