package archive

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"
)

func TestCompressDecompressFilePropertyRoundTripsBytes(t *testing.T) {
	t.Parallel()

	property := func(raw []byte) bool {
		if len(raw) > 4096 {
			raw = raw[:4096]
		}
		dir := t.TempDir()
		src := filepath.Join(dir, "source.jsonl")
		archivePath := filepath.Join(dir, "source.jsonl.gz")
		restored := filepath.Join(dir, "restored.jsonl")
		if err := os.WriteFile(src, raw, 0o644); err != nil {
			t.Logf("write source: %v", err)
			return false
		}

		size, err := CompressFile(src, archivePath)
		if err != nil {
			t.Logf("CompressFile error: %v", err)
			return false
		}
		if size <= 0 {
			t.Logf("compressed size = %d", size)
			return false
		}
		if gzipName, err := gzipHeaderName(archivePath); err != nil || gzipName != filepath.Base(src) {
			t.Logf("gzip name = %q err=%v want %q", gzipName, err, filepath.Base(src))
			return false
		}

		if err := DecompressFile(archivePath, restored); err != nil {
			t.Logf("DecompressFile error: %v", err)
			return false
		}
		got, err := os.ReadFile(restored)
		if err != nil {
			t.Logf("read restored: %v", err)
			return false
		}
		return bytes.Equal(got, raw)
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 50}); err != nil {
		t.Fatal(err)
	}
}

func TestReadChunksFromArchiveReturnsOnlyRequestedLines(t *testing.T) {
	t.Parallel()

	path := writeGzipLines(t, []string{"zero", "one", "two", "three"})
	got, err := ReadChunksFromArchive(path, []int{3, 1, 3, -1, 99})
	if err != nil {
		t.Fatalf("ReadChunksFromArchive() error = %v", err)
	}
	if string(got[1]) != "one" {
		t.Fatalf("line 1 = %q, want one", got[1])
	}
	if string(got[3]) != "three" {
		t.Fatalf("line 3 = %q, want three", got[3])
	}
	if _, ok := got[-1]; ok {
		t.Fatalf("negative index unexpectedly returned")
	}
	if _, ok := got[99]; ok {
		t.Fatalf("missing high index unexpectedly returned")
	}

	got[1][0] = 'X'
	again, err := ReadChunksFromArchive(path, []int{1})
	if err != nil {
		t.Fatalf("second ReadChunksFromArchive() error = %v", err)
	}
	if string(again[1]) != "one" {
		t.Fatalf("returned line was not independent copy: %q", again[1])
	}
}

func TestReadChunksFromArchivePropertyMatchesSourceLines(t *testing.T) {
	t.Parallel()

	property := func(raw []byte, countSeed, firstSeed, secondSeed uint8) bool {
		lineCount := int(countSeed%12) + 1
		lines := make([]string, lineCount)
		for i := range lines {
			lines[i] = archiveTestLine(raw, i)
		}
		first := int(firstSeed % uint8(lineCount))
		second := int(secondSeed % uint8(lineCount))
		path := writeGzipLines(t, lines)

		got, err := ReadChunksFromArchive(path, []int{second, -1, first, lineCount + 10})
		if err != nil {
			t.Logf("ReadChunksFromArchive error: %v", err)
			return false
		}
		if string(got[first]) != lines[first] {
			t.Logf("line %d = %q want %q", first, got[first], lines[first])
			return false
		}
		if string(got[second]) != lines[second] {
			t.Logf("line %d = %q want %q", second, got[second], lines[second])
			return false
		}
		_, hasNegative := got[-1]
		_, hasMissing := got[lineCount+10]
		return !hasNegative && !hasMissing
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 75}); err != nil {
		t.Fatal(err)
	}
}

func TestArchivePathKeepsSessionIDInsideArchiveDirectory(t *testing.T) {
	t.Parallel()

	archivesDir := filepath.Join(t.TempDir(), "archives")
	tests := []struct {
		name      string
		sessionID string
		wantBase  string
	}{
		{name: "normal", sessionID: "session-123", wantBase: "session-123.jsonl.gz"},
		{name: "slash traversal", sessionID: "../escape", wantBase: ".._escape.jsonl.gz"},
		{name: "absolute path", sessionID: "/tmp/session", wantBase: "_tmp_session.jsonl.gz"},
		{name: "backslash traversal", sessionID: `..\escape`, wantBase: ".._escape.jsonl.gz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ArchivePath(archivesDir, tt.sessionID)
			if filepath.Dir(got) != archivesDir {
				t.Fatalf("ArchivePath() dir = %q, want %q", filepath.Dir(got), archivesDir)
			}
			if filepath.Base(got) != tt.wantBase {
				t.Fatalf("ArchivePath() base = %q, want %q", filepath.Base(got), tt.wantBase)
			}
			rel, err := filepath.Rel(archivesDir, got)
			if err != nil {
				t.Fatalf("filepath.Rel() error = %v", err)
			}
			if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
				t.Fatalf("ArchivePath() escaped archive dir: %q rel %q", got, rel)
			}
		})
	}
}

func gzipHeaderName(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	reader, err := gzip.NewReader(file)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	return reader.Name, nil
}

func writeGzipLines(t *testing.T, lines []string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "archive.jsonl.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	writer := gzip.NewWriter(file)
	_, writeErr := io.WriteString(writer, strings.Join(lines, "\n"))
	closeErr := writer.Close()
	fileErr := file.Close()
	if writeErr != nil {
		t.Fatalf("write archive: %v", writeErr)
	}
	if closeErr != nil {
		t.Fatalf("close gzip writer: %v", closeErr)
	}
	if fileErr != nil {
		t.Fatalf("close archive: %v", fileErr)
	}
	return path
}

func archiveTestLine(raw []byte, index int) string {
	if len(raw) > 32 {
		raw = raw[:32]
	}
	replacer := strings.NewReplacer("\n", "_", "\r", "_")
	return replacer.Replace(string(raw)) + "-" + string(rune('a'+index))
}
