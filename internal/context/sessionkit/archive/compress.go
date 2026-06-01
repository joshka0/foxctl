package archive

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CompressFile compresses a file using gzip.
// Returns the compressed file size.
func CompressFile(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, fmt.Errorf("open source: %w", err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return 0, fmt.Errorf("create destination: %w", err)
	}
	defer out.Close()

	gw := gzip.NewWriter(out)
	gw.Name = filepath.Base(src)
	gw.ModTime = time.Now()

	if _, err := io.Copy(gw, in); err != nil {
		return 0, fmt.Errorf("compress: %w", err)
	}

	if err := gw.Close(); err != nil {
		return 0, fmt.Errorf("close gzip writer: %w", err)
	}

	info, err := out.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat output: %w", err)
	}

	return info.Size(), nil
}

// DecompressFile decompresses a gzip file.
func DecompressFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer in.Close()

	gr, err := gzip.NewReader(in)
	if err != nil {
		return fmt.Errorf("create gzip reader: %w", err)
	}
	defer gr.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, gr); err != nil {
		return fmt.Errorf("decompress: %w", err)
	}

	return nil
}

// ArchivePath returns the archive path for a session ID in the given archives directory.
func ArchivePath(archivesDir, sessionID string) string {
	filename := strings.NewReplacer("/", "_", `\`, "_").Replace(sessionID)
	return filepath.Join(archivesDir, fmt.Sprintf("%s.jsonl.gz", filename))
}
