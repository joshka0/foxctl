package codeedit

import (
	"context"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/editutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
)

// FileEditOptions configures how edits are applied to a file.
type FileEditOptions struct {
	DryRun       bool
	CreateBackup bool
	BackupTags   []string
	ContextLines int
}

// FileEditResult captures the edit outcome for a file.
type FileEditResult struct {
	Diff         string
	Edited       bool
	BackupDigest string
	EditCount    int
	SymbolsFound []string
}

// ApplyEditsToFile applies a list of edits to a file and returns the outcome.
func ApplyEditsToFile(
	ctx context.Context,
	rc *skillmain.RunContext,
	path string,
	edits []Edit,
	opts FileEditOptions,
) (FileEditResult, error) {
	editCount := 0
	var symbolsFound []string

	apply := func(original string) (string, error) {
		lines := strings.Split(original, "\n")
		lang := DetectLanguage(path)

		currentLines := lines
		for _, e := range edits {
			edited, found, applied, err := ApplyEdit(currentLines, lang, e)
			if err != nil {
				return "", err
			}
			if applied {
				currentLines = edited
				editCount++
				symbolsFound = append(symbolsFound, found...)
			}
		}

		return strings.Join(currentLines, "\n"), nil
	}

	fileResult, err := editutil.ApplyFile(ctx, rc, path, editutil.FileOptions{
		DryRun:       opts.DryRun,
		CreateBackup: opts.CreateBackup,
		BackupTags:   opts.BackupTags,
		DiffContext:  opts.ContextLines,
	}, apply)
	if err != nil {
		return FileEditResult{}, err
	}

	return FileEditResult{
		Diff:         fileResult.Diff,
		Edited:       fileResult.Edited,
		BackupDigest: fileResult.BackupDigest,
		EditCount:    editCount,
		SymbolsFound: symbolsFound,
	}, nil
}
