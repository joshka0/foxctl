package golden

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/protocol"
)

func TestGoldenEnvelopes(t *testing.T) {
	root := "."
	var files []string
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".json") {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk golden fixtures: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no golden JSON fixtures discovered")
	}

	for _, path := range files {
		path := path
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatalf("rel path: %v", err)
		}
		t.Run(filepath.ToSlash(rel), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			var env envelope.Envelope
			if err := json.Unmarshal(data, &env); err != nil {
				t.Fatalf("decode %s: %v", path, err)
			}
			if err := protocol.Validate(env); err != nil {
				t.Fatalf("protocol validation failed for %s: %v", path, err)
			}
		})
	}
}

func TestGoldenProgressStream(t *testing.T) {
	path := filepath.Join("envelopes", "progress-stream.ndjson")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open progress stream: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event struct {
			TS string `json:"ts"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode progress line %d: %v", lineNo, err)
		}
		if event.TS == "" {
			t.Fatalf("progress line %d missing ts", lineNo)
		}
		if _, err := time.Parse(time.RFC3339, event.TS); err != nil {
			t.Fatalf("progress line %d invalid ts: %v", lineNo, err)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan progress stream: %v", err)
	}
	if lineNo == 0 {
		t.Fatal("progress stream fixture empty")
	}
}
