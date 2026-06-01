package textreplace_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/textreplace"
)

type literalReplacer struct {
	old string
	new string
}

func (r literalReplacer) Match(content string) bool {
	return strings.Contains(content, r.old)
}

func (r literalReplacer) Replace(content string) (string, int) {
	return strings.ReplaceAll(content, r.old, r.new), strings.Count(content, r.old)
}

func TestProcessFileBackupWithEmptySuffixCreatesDistinctBackup(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "notes.txt")
	original := "alpha\nbeta\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	change, err := textreplace.ProcessFile(
		context.Background(),
		nil,
		path,
		workspace,
		[]textreplace.Replacer{literalReplacer{old: "alpha", new: "omega"}},
		nil,
		nil,
		nil,
		textreplace.Options{Backup: true},
	)
	if err != nil {
		t.Fatalf("ProcessFile: %v", err)
	}
	if change.BackupPath == "" {
		t.Fatal("expected backup path")
	}
	if change.BackupPath == "notes.txt" {
		t.Fatalf("backup path aliases target file: %q", change.BackupPath)
	}

	backupPath := filepath.Join(workspace, change.BackupPath)
	backup, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backup) != original {
		t.Fatalf("backup = %q, want original %q", string(backup), original)
	}

	modified, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read modified: %v", err)
	}
	if string(modified) != "omega\nbeta\n" {
		t.Fatalf("modified = %q", string(modified))
	}
}

func TestProcessFileDryRunPropertyNeverMutatesFile(t *testing.T) {
	property := func(raw string) bool {
		content := "prefix old suffix\n" + strings.ReplaceAll(raw, "\x00", "") + "\n"
		workspace := t.TempDir()
		path := filepath.Join(workspace, "file.txt")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Logf("write fixture: %v", err)
			return false
		}

		change, err := textreplace.ProcessFile(
			context.Background(),
			nil,
			path,
			workspace,
			[]textreplace.Replacer{literalReplacer{old: "old", new: "new"}},
			nil,
			nil,
			nil,
			textreplace.Options{DryRun: true, Backup: true, BackupSuffix: ".bak"},
		)
		if err != nil {
			t.Logf("ProcessFile: %v", err)
			return false
		}
		if change.Replacements == 0 {
			t.Log("expected dry-run replacement count")
			return false
		}
		if change.BackupPath != "" || change.CASDigest != "" {
			t.Logf("dry run created side effects: backup=%q cas=%q", change.BackupPath, change.CASDigest)
			return false
		}

		got, err := os.ReadFile(path)
		if err != nil {
			t.Logf("read file: %v", err)
			return false
		}
		if string(got) != content {
			t.Logf("dry run mutated file: got %q want %q", string(got), content)
			return false
		}
		if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
			t.Logf("dry run created backup or unexpected stat error: %v", err)
			return false
		}

		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatal(err)
	}
}
