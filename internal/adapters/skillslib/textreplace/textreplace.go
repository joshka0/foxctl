package textreplace

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/fsutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/pathutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillcas"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
)

// LineRange describes a line range for replacements.
type LineRange struct {
	Start        int    `json:"start"`
	End          int    `json:"end"`
	StartPattern string `json:"start_pattern"`
	EndPattern   string `json:"end_pattern"`
}

// Change captures a single line replacement.
type Change struct {
	File             string `json:"file"`
	LineNumber       int    `json:"line_number"`
	OriginalLine     string `json:"original_line"`
	ModifiedLine     string `json:"modified_line"`
	ReplacementsMade int    `json:"replacements_in_line"`
	Diff             string `json:"diff,omitempty"`
}

// FileChange summarizes replacements within a file.
type FileChange struct {
	File         string   `json:"file"`
	Replacements int      `json:"replacements"`
	Changes      []Change `json:"changes,omitempty"`
	BackupPath   string   `json:"backup_path,omitempty"`
	CASDigest    string   `json:"cas_digest,omitempty"`
	Skipped      bool     `json:"skipped,omitempty"`
	SkipReason   string   `json:"skip_reason,omitempty"`
	Validated    bool     `json:"validated,omitempty"`
	ValidationOK bool     `json:"validation_ok,omitempty"`
}

// Replacer defines a per-line replacement operation.
type Replacer interface {
	Match(content string) bool
	Replace(content string) (string, int)
}

// Options configures file processing behavior.
type Options struct {
	DryRun              bool
	Backup              bool
	BackupSuffix        string
	CASBackup           bool
	PreserveLineEndings bool
	ShowDiff            bool
}

// IsBinaryFile checks if a file likely contains binary data.
func IsBinaryFile(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, skillerr.WrapIO(fmt.Sprintf("open %s", path), err)
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return false, skillerr.WrapIO(fmt.Sprintf("read %s", path), err)
	}
	buf = buf[:n]

	return fsutil.IsBinaryContent(buf), nil
}

// ProcessFile applies replacements to a file and returns the change summary.
func ProcessFile(
	ctx context.Context,
	rc *skillmain.RunContext,
	path, workspace string,
	replacers []Replacer,
	lineRange *LineRange,
	rangeStartRe, rangeEndRe *regexp.Regexp,
	opts Options,
) (FileChange, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return FileChange{}, skillerr.WrapIO(fmt.Sprintf("read %s", path), err)
	}

	lineEnding := "\n"
	if opts.PreserveLineEndings {
		lineEnding = detectLineEnding(string(content))
	}

	normalizedContent := strings.ReplaceAll(string(content), "\r\n", "\n")
	lines := strings.Split(normalizedContent, "\n")

	inRange := determineLineRange(lines, lineRange, rangeStartRe, rangeEndRe)

	var (
		changes       []Change
		modifiedLines []string
		modified      bool
	)

	for lineNo, line := range lines {
		lineNum := lineNo + 1
		processLine := inRange == nil || inRange[lineNo]

		if !processLine {
			modifiedLines = append(modifiedLines, line)
			continue
		}

		newLine := line
		lineReplacements := 0

		for _, r := range replacers {
			if r.Match(newLine) {
				var count int
				newLine, count = r.Replace(newLine)
				lineReplacements += count
			}
		}

		if lineReplacements > 0 {
			modified = true
			diff := ""
			if opts.ShowDiff {
				diff = fmt.Sprintf("- %s\n+ %s", truncateLine(line, 200), truncateLine(newLine, 200))
			}

			changes = append(changes, Change{
				LineNumber:       lineNum,
				OriginalLine:     truncateLine(line, 200),
				ModifiedLine:     truncateLine(newLine, 200),
				ReplacementsMade: lineReplacements,
				Diff:             diff,
			})
		}

		modifiedLines = append(modifiedLines, newLine)
	}

	totalReplacements := 0
	for _, c := range changes {
		totalReplacements += c.ReplacementsMade
	}

	result := FileChange{
		Replacements: totalReplacements,
		Changes:      changes,
	}

	if !opts.DryRun && modified {
		if opts.Backup {
			backupPath := path + backupSuffix(opts.BackupSuffix)
			if err := os.WriteFile(backupPath, content, 0o644); err != nil {
				return FileChange{}, skillerr.WrapIO(fmt.Sprintf("create backup %s", backupPath), err)
			}
			result.BackupPath = pathutil.RelTo(workspace, backupPath)
		}

		if opts.CASBackup && rc != nil && rc.ShouldStoreCAS() {
			artifact, err := skillcas.PersistBuffer(
				ctx, rc, bytes.NewBuffer(content), "text/plain",
				"backup", "text/replace", fmt.Sprintf("path:%s", pathutil.RelTo(workspace, path)),
			)
			if err != nil {
				return FileChange{}, skillerr.WrapIO(fmt.Sprintf("CAS backup %s", path), err)
			}
			result.CASDigest = artifact.Digest
		}

		newContent := strings.Join(modifiedLines, lineEnding)
		if err := writeFileAtomic(path, []byte(newContent)); err != nil {
			return FileChange{}, skillerr.WrapIO(fmt.Sprintf("write %s", path), err)
		}
	}

	return result, nil
}

func backupSuffix(suffix string) string {
	if suffix == "" {
		return ".bak"
	}
	return suffix
}

func determineLineRange(lines []string, lr *LineRange, startRe, endRe *regexp.Regexp) map[int]bool {
	if lr == nil {
		return nil
	}

	inRange := make(map[int]bool)

	if lr.Start > 0 || lr.End > 0 {
		start := lr.Start
		if start <= 0 {
			start = 1
		}
		end := lr.End
		if end <= 0 {
			end = len(lines)
		}

		for i := start - 1; i < end && i < len(lines); i++ {
			inRange[i] = true
		}
		return inRange
	}

	if startRe != nil || endRe != nil {
		insideRange := startRe == nil

		for i, line := range lines {
			if startRe != nil && startRe.MatchString(line) {
				insideRange = true
			}

			if insideRange {
				inRange[i] = true
			}

			if endRe != nil && endRe.MatchString(line) {
				insideRange = false
			}
		}
		return inRange
	}

	return nil
}

func detectLineEnding(content string) string {
	if strings.Contains(content, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

func truncateLine(line string, maxLen int) string {
	if len(line) <= maxLen {
		return line
	}
	return line[:maxLen] + "..."
}

func writeFileAtomic(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return skillerr.WrapIO(fmt.Sprintf("stat %s", path), err)
	}

	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, ".text_replace_tmp_*")
	if err != nil {
		return skillerr.WrapIO("create temp file", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()    //nolint:errcheck
		_ = os.Remove(tmpPath) //nolint:errcheck
		return skillerr.WrapIO("write temp file", err)
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath) //nolint:errcheck
		return skillerr.WrapIO("close temp file", err)
	}

	if err := os.Chmod(tmpPath, info.Mode()); err != nil {
		_ = os.Remove(tmpPath) //nolint:errcheck
		return skillerr.WrapIO("chmod temp file", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath) //nolint:errcheck
		return skillerr.WrapIO("rename temp file", err)
	}

	return nil
}
