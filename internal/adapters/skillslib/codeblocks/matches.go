package codeblocks

import (
	"os"
	"path/filepath"
	"strings"
)

// GroupMatchesByFile groups raw matches by file path.
func GroupMatchesByFile(matches []RawMatch) map[string][]RawMatch {
	result := make(map[string][]RawMatch)
	for _, m := range matches {
		result[m.File] = append(result[m.File], m)
	}
	return result
}

// ExpandMatches groups and expands matches into code blocks, respecting max limits.
func ExpandMatches(workspace string, rawMatches []RawMatch, maxBlocks, maxBlockLines int) []Block {
	if len(rawMatches) == 0 {
		return nil
	}

	matchesByFile := GroupMatchesByFile(rawMatches)
	var blocks []Block

	for file, fileMatches := range matchesByFile {
		if len(blocks) >= maxBlocks {
			break
		}

		absPath := filepath.Join(workspace, file)
		content, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}

		lines := strings.Split(string(content), "\n")
		lang := DetectLanguage(file)
		expander := NewExpander(lang, maxBlockLines)

		fileBlocks := expander.ExpandMatches(file, lines, fileMatches)
		for _, block := range fileBlocks {
			if len(blocks) >= maxBlocks {
				break
			}
			blocks = append(blocks, block)
		}
	}

	return blocks
}
