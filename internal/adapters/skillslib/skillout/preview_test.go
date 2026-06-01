package skillout

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/cas"
)

func TestEmitPreview(t *testing.T) {
	items := []any{
		map[string]string{"name": "item1"},
		map[string]string{"name": "item2"},
		map[string]string{"name": "item3"},
	}

	t.Run("all items", func(t *testing.T) {
		buf := &bytes.Buffer{}
		opts := PreviewOpts{MaxItems: 10}
		written, truncated, err := EmitPreview(buf, items, opts)
		if err != nil {
			t.Fatalf("EmitPreview error: %v", err)
		}
		if written != 3 {
			t.Errorf("written = %d, want 3", written)
		}
		if truncated {
			t.Error("should not be truncated")
		}

		lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
		if len(lines) != 3 {
			t.Errorf("expected 3 lines, got %d", len(lines))
		}
	})

	t.Run("item limit", func(t *testing.T) {
		buf := &bytes.Buffer{}
		opts := PreviewOpts{MaxItems: 2, TruncateMsg: "truncated"}
		written, truncated, err := EmitPreview(buf, items, opts)
		if err != nil {
			t.Fatalf("EmitPreview error: %v", err)
		}
		if written != 2 {
			t.Errorf("written = %d, want 2", written)
		}
		if !truncated {
			t.Error("should be truncated")
		}

		output := buf.String()
		if !strings.Contains(output, "_truncated") {
			t.Error("should contain truncation marker")
		}
	})

	t.Run("byte limit", func(t *testing.T) {
		buf := &bytes.Buffer{}
		opts := PreviewOpts{MaxBytes: 30} // Very small
		_, truncated, err := EmitPreview(buf, items, opts)
		if err != nil {
			t.Fatalf("EmitPreview error: %v", err)
		}
		if !truncated {
			t.Error("should be truncated due to byte limit")
		}
	})
}

func TestPreparePreview(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}

	t.Run("no truncation", func(t *testing.T) {
		result, truncated := PreparePreview(items, 10)
		if truncated {
			t.Error("should not be truncated")
		}
		if len(result) != 5 {
			t.Errorf("len = %d, want 5", len(result))
		}
	})

	t.Run("with truncation", func(t *testing.T) {
		result, truncated := PreparePreview(items, 3)
		if !truncated {
			t.Error("should be truncated")
		}
		if len(result) != 3 {
			t.Errorf("len = %d, want 3", len(result))
		}
	})

	t.Run("zero limit", func(t *testing.T) {
		result, truncated := PreparePreview(items, 0)
		if truncated {
			t.Error("should not be truncated with zero limit")
		}
		if len(result) != 5 {
			t.Errorf("len = %d, want 5", len(result))
		}
	})
}

func TestNewPreviewResult(t *testing.T) {
	items := []string{"a", "b", "c", "d", "e"}

	t.Run("no truncation", func(t *testing.T) {
		result := NewPreviewResult(items, 10)
		if result.Truncated {
			t.Error("should not be truncated")
		}
		if result.Total != 5 {
			t.Errorf("Total = %d, want 5", result.Total)
		}
		if result.Shown != 5 {
			t.Errorf("Shown = %d, want 5", result.Shown)
		}
	})

	t.Run("with truncation", func(t *testing.T) {
		result := NewPreviewResult(items, 3)
		if !result.Truncated {
			t.Error("should be truncated")
		}
		if result.Total != 5 {
			t.Errorf("Total = %d, want 5", result.Total)
		}
		if result.Shown != 3 {
			t.Errorf("Shown = %d, want 3", result.Shown)
		}
		if len(result.Items) != 3 {
			t.Errorf("len(Items) = %d, want 3", len(result.Items))
		}
	})
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"no truncation", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"truncate", "hello world", 8, "hello..."},
		{"very short max", "hello", 3, "hel"},
		{"zero max", "hello", 0, "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateString(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("TruncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestDefaultPreviewOpts(t *testing.T) {
	opts := DefaultPreviewOpts()

	if opts.MaxItems != 100 {
		t.Errorf("MaxItems = %d, want 100", opts.MaxItems)
	}
	if opts.MaxBytes != 32*1024 {
		t.Errorf("MaxBytes = %d, want %d", opts.MaxBytes, 32*1024)
	}
	if opts.TruncateMsg == "" {
		t.Error("TruncateMsg should not be empty")
	}
}

func TestPreviewAndPersistNDJSONPersistsResultArtifactWhenNoCASDisablesTruncation(t *testing.T) {
	ctx := context.Background()
	store, err := cas.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new cas store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close cas store: %v", err)
		}
	})

	rc := &skillmain.RunContext{
		Config: config.Config{
			CAS: config.CASPolicy{Store: true},
		},
		CASStore:   store,
		MaxPreview: 2,
		NoCAS:      true,
	}

	result, err := PreviewAndPersistNDJSON(ctx, rc, []int{1, 2, 3}, 0, "numbers", true)
	if err != nil {
		t.Fatalf("PreviewAndPersistNDJSON returned error: %v", err)
	}
	if result.Artifact == nil || result.Artifact.Digest == "" {
		t.Fatalf("expected result artifact despite NoCAS, got %+v", result.Artifact)
	}

	reader, _, err := store.Get(ctx, result.Artifact.Digest)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read artifact body: %v", err)
	}
	if got := strings.TrimSpace(string(body)); got != "1\n2\n3" {
		t.Fatalf("artifact body = %q, want NDJSON numbers", got)
	}
}
