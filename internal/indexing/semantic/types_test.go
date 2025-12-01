package semantic

import (
	"net/url"
	"strings"
	"testing"
)

// These tests focus on the stability and invariants of the semantic index
// naming and chunking configuration helpers.

func TestFileEmbeddingName_StableAndEscapesSpecialCharacters(t *testing.T) {
	workspace := "workspace with spaces"
	path := "dir with spaces/fi#le?.go"

	name1 := FileEmbeddingName(workspace, path)
	name2 := FileEmbeddingName(workspace, path)

	if name1 != name2 {
		t.Fatalf("FileEmbeddingName should be stable across calls, got %q and %q", name1, name2)
	}

	expectedPrefix := "file://" + url.PathEscape(workspace) + "/"
	if !strings.HasPrefix(name1, expectedPrefix) {
		t.Fatalf("expected name to start with %q, got %q", expectedPrefix, name1)
	}

	// The raw path contains '#' and '?', but these should be percent-encoded
	// in the name for the non-chunked case.
	if strings.Contains(name1, "#") {
		t.Fatalf("unexpected '#' in file embedding name %q", name1)
	}
	if strings.Contains(name1, "?") {
		t.Fatalf("unexpected '?' in file embedding name %q", name1)
	}
}

func TestChunkEmbeddingName_StableAndStructured(t *testing.T) {
	workspace := "workspace with spaces"
	path := "dir/fi#le.go"
	chunkID := "chunk id"
	cfgHash := "hash+with&chars"

	name1 := ChunkEmbeddingName(workspace, path, chunkID, cfgHash)
	name2 := ChunkEmbeddingName(workspace, path, chunkID, cfgHash)

	if name1 != name2 {
		t.Fatalf("ChunkEmbeddingName should be stable across calls, got %q and %q", name1, name2)
	}

	expectedPrefix := "file://" + url.PathEscape(workspace) + "/"
	if !strings.HasPrefix(name1, expectedPrefix) {
		t.Fatalf("expected name to start with %q, got %q", expectedPrefix, name1)
	}

	if !strings.Contains(name1, "#chunk-") {
		t.Fatalf("expected chunk name to contain '#chunk-', got %q", name1)
	}
	if !strings.Contains(name1, "?cfg=") {
		t.Fatalf("expected chunk name to contain '?cfg=', got %q", name1)
	}

	parts := strings.SplitN(name1, "#chunk-", 2)
	if len(parts) != 2 {
		t.Fatalf("expected chunk name to contain '#chunk-' once, got %q", name1)
	}
	// Path portion before '#chunk-' should not contain raw '#' or '?' from the original path.
	if strings.ContainsAny(parts[0], "#?") {
		t.Fatalf("unexpected '#' or '?' in path portion of chunk name %q", name1)
	}
}

func TestChunkingConfigHash_SensitivityToRelevantFields(t *testing.T) {
	base := Config{
		ChunkBytes:        1024,
		ChunkOverlapBytes: 128,
		ProviderModel:     "model-a",
	}
	same := Config{
		ChunkBytes:        1024,
		ChunkOverlapBytes: 128,
		ProviderModel:     "model-a",
	}

	baseHash := base.ChunkingConfigHash()
	sameHash := same.ChunkingConfigHash()

	if baseHash == "" {
		t.Fatal("expected non-empty hash for non-zero ChunkBytes")
	}
	if baseHash != sameHash {
		t.Fatalf("expected identical configs to have the same hash, got %q and %q", baseHash, sameHash)
	}

	cases := []struct {
		name string
		cfg  Config
	}{
		{
			name: "different chunk bytes",
			cfg: Config{
				ChunkBytes:        2048,
				ChunkOverlapBytes: 128,
				ProviderModel:     "model-a",
			},
		},
		{
			name: "different overlap",
			cfg: Config{
				ChunkBytes:        1024,
				ChunkOverlapBytes: 64,
				ProviderModel:     "model-a",
			},
		},
		{
			name: "different model",
			cfg: Config{
				ChunkBytes:        1024,
				ChunkOverlapBytes: 128,
				ProviderModel:     "model-b",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.cfg.ChunkingConfigHash() == baseHash {
				t.Fatalf("expected %s to change hash, but got same value %q", tc.name, baseHash)
			}
		})
	}

	noChunking := Config{
		ChunkBytes:        0,
		ChunkOverlapBytes: 128,
		ProviderModel:     "model-a",
	}
	if got := noChunking.ChunkingConfigHash(); got != "" {
		t.Fatalf("expected empty hash when ChunkBytes == 0, got %q", got)
	}
}
