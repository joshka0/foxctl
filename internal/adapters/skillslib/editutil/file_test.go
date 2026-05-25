package editutil

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/storage/cas"
)

func TestApplyFileDryRunDoesNotWriteOrBackup(t *testing.T) {
	ctx := context.Background()
	rc := testRunContext(t)
	path := filepath.Join(t.TempDir(), "note.txt")
	original := "alpha\nbeta\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("write original: %v", err)
	}

	result, err := ApplyFile(ctx, rc, path, FileOptions{
		DryRun:       true,
		CreateBackup: true,
		DiffContext:  1,
	}, func(string) (string, error) {
		return "alpha\ngamma\n", nil
	})
	if err != nil {
		t.Fatalf("ApplyFile: %v", err)
	}

	if result.Edited {
		t.Fatalf("dry-run result marked file edited")
	}
	if result.BackupDigest != "" || result.BackupArtifact != nil {
		t.Fatalf("dry-run created backup: %+v", result)
	}
	assertFileContent(t, path, original)
	assertCASObjectCount(t, rc, 0)
}

func TestApplyFileDryRunGeneratedInputsHaveNoSideEffects(t *testing.T) {
	prop := func(raw string) bool {
		ctx := context.Background()
		rc := testRunContext(t)
		path := filepath.Join(t.TempDir(), "generated.txt")
		original := shortText(raw)
		if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
			t.Logf("write original: %v", err)
			return false
		}

		result, err := ApplyFile(ctx, rc, path, FileOptions{
			DryRun:       true,
			CreateBackup: true,
			DiffContext:  1,
		}, func(s string) (string, error) {
			return s + "\nchanged", nil
		})
		if err != nil {
			t.Logf("ApplyFile: %v", err)
			return false
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Logf("read result: %v", err)
			return false
		}
		objects, err := rc.CASStore.List(ctx)
		if err != nil {
			t.Logf("list CAS: %v", err)
			return false
		}
		if result.Edited || result.BackupDigest != "" || result.BackupArtifact != nil {
			t.Logf("dry-run reported edit or backup: %+v", result)
			return false
		}
		if string(content) != original {
			t.Logf("dry-run changed file: got %q want %q", string(content), original)
			return false
		}
		return len(objects) == 0
	}

	if err := quick.Check(prop, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatal(err)
	}
}

func TestApplyFileWritesBackupOfOriginalContent(t *testing.T) {
	ctx := context.Background()
	rc := testRunContext(t)
	path := filepath.Join(t.TempDir(), "note.txt")
	original := "before\n"
	modified := "after\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("write original: %v", err)
	}

	result, err := ApplyFile(ctx, rc, path, FileOptions{
		CreateBackup: true,
		BackupTags:   []string{"test-backup"},
		DiffContext:  1,
	}, func(string) (string, error) {
		return modified, nil
	})
	if err != nil {
		t.Fatalf("ApplyFile: %v", err)
	}

	if !result.Edited {
		t.Fatalf("expected write to mark file edited")
	}
	if result.BackupDigest == "" || result.BackupArtifact == nil {
		t.Fatalf("expected backup artifact, got %+v", result)
	}
	assertFileContent(t, path, modified)

	reader, meta, err := rc.CASStore.Get(ctx, result.BackupDigest)
	if err != nil {
		t.Fatalf("get backup: %v", err)
	}
	defer reader.Close()
	backup, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backup) != original {
		t.Fatalf("backup content = %q, want %q", string(backup), original)
	}
	if meta.Size != int64(len(original)) {
		t.Fatalf("backup size = %d, want %d", meta.Size, len(original))
	}
	if !contains(meta.Tags, "test-backup") {
		t.Fatalf("backup tags = %v, want test-backup", meta.Tags)
	}
}

func testRunContext(t *testing.T) *skillmain.RunContext {
	t.Helper()
	store, err := cas.NewStore(filepath.Join(t.TempDir(), "cas"))
	if err != nil {
		t.Fatalf("new CAS store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close CAS store: %v", err)
		}
	})
	return &skillmain.RunContext{CASStore: store}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(got) != want {
		t.Fatalf("file content = %q, want %q", string(got), want)
	}
}

func assertCASObjectCount(t *testing.T, rc *skillmain.RunContext, want int) {
	t.Helper()
	objects, err := rc.CASStore.List(context.Background())
	if err != nil {
		t.Fatalf("list CAS: %v", err)
	}
	if len(objects) != want {
		t.Fatalf("CAS object count = %d, want %d", len(objects), want)
	}
}

func shortText(raw string) string {
	text := strings.Map(func(r rune) rune {
		if r == 0 {
			return -1
		}
		return r
	}, raw)
	if text == "" {
		return "x"
	}
	runes := []rune(text)
	if len(runes) > 128 {
		text = string(runes[:128])
	}
	return text
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
