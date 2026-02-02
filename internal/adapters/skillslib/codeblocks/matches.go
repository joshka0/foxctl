package codeblocks

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ExpandOptions configures how raw matches are expanded into blocks.
type ExpandOptions struct {
	MaxBlocks        int
	MaxBlockLines    int
	MaxBlocksPerFile int
	MaxBytesPerFile  int64
	Target           Target
}

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
	return ExpandMatchesWithOptions(workspace, rawMatches, ExpandOptions{
		MaxBlocks:     maxBlocks,
		MaxBlockLines: maxBlockLines,
	})
}

// ExpandMatchesWithOptions groups and expands matches into code blocks.
func ExpandMatchesWithOptions(workspace string, rawMatches []RawMatch, opts ExpandOptions) []Block {
	if len(rawMatches) == 0 {
		return nil
	}

	if opts.MaxBlocks <= 0 {
		opts.MaxBlocks = len(rawMatches)
	}
	if opts.MaxBlocksPerFile <= 0 {
		opts.MaxBlocksPerFile = opts.MaxBlocks
	}
	if opts.MaxBlocksPerFile > opts.MaxBlocks {
		opts.MaxBlocksPerFile = opts.MaxBlocks
	}

	matchesByFile := GroupMatchesByFile(rawMatches)
	fileKeys := make([]string, 0, len(matchesByFile))
	for file := range matchesByFile {
		fileKeys = append(fileKeys, file)
	}
	sort.Strings(fileKeys)

	var blocks []Block
	for _, file := range fileKeys {
		if len(blocks) >= opts.MaxBlocks {
			break
		}

		fileMatches := matchesByFile[file]
		if len(fileMatches) == 0 {
			continue
		}

		cleanFile, ok := cleanMatchPath(workspace, file)
		if !ok {
			continue
		}
		absPath := filepath.Join(workspace, cleanFile)
		if opts.MaxBytesPerFile > 0 {
			info, err := os.Stat(absPath)
			if err != nil {
				continue
			}
			if info.Size() > opts.MaxBytesPerFile {
				continue
			}
		}

		content, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}
		if opts.MaxBytesPerFile > 0 && int64(len(content)) > opts.MaxBytesPerFile {
			continue
		}

		lines := strings.Split(string(content), "\n")
		lang := DetectLanguage(cleanFile)
		expander := NewExpander(lang, opts.MaxBlockLines, WithTarget(opts.Target))

		fileBlocks := expander.ExpandMatches(cleanFile, lines, fileMatches)
		if opts.MaxBlocksPerFile > 0 && len(fileBlocks) > opts.MaxBlocksPerFile {
			fileBlocks = fileBlocks[:opts.MaxBlocksPerFile]
		}

		for _, block := range fileBlocks {
			if len(blocks) >= opts.MaxBlocks {
				break
			}
			blocks = append(blocks, block)
		}
	}

	return blocks
}

func cleanMatchPath(workspace, file string) (string, bool) {
	if file == "" {
		return "", false
	}
	clean := filepath.Clean(file)
	if filepath.IsAbs(clean) {
		rel, ok := relToWorkspace(workspace, clean)
		if !ok {
			return "", false
		}
		clean = rel
	}
	if clean == "." {
		return "", false
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", false
	}
	return clean, true
}

func relToWorkspace(workspace, absPath string) (string, bool) {
	if workspace == "" {
		return "", false
	}
	wsClean := filepath.Clean(workspace)
	absClean := filepath.Clean(absPath)
	combos := [][2]string{{wsClean, absClean}}
	wsReal, wsErr := filepath.EvalSymlinks(wsClean)
	absReal, absErr := filepath.EvalSymlinks(absClean)
	if wsErr == nil {
		combos = append(combos, [2]string{wsReal, absClean})
		if absErr == nil {
			combos = append(combos, [2]string{wsReal, absReal})
		}
	}
	if absErr == nil {
		combos = append(combos, [2]string{wsClean, absReal})
	}
	for _, combo := range combos {
		rel, err := filepath.Rel(combo[0], combo[1])
		if err != nil {
			continue
		}
		if rel == "." {
			continue
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			continue
		}
		return rel, true
	}
	return "", false
}
