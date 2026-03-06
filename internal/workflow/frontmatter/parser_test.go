package frontmatter

import (
	"errors"
	"testing"
)

func TestParse_NoFrontMatter(t *testing.T) {
	doc, err := Parse([]byte("# Title\n\nHello"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if doc.HasFrontMatter {
		t.Fatalf("HasFrontMatter = true, want false")
	}
	if len(doc.Config) != 0 {
		t.Fatalf("Config len = %d, want 0", len(doc.Config))
	}
	if doc.PromptTemplate != "# Title\n\nHello" {
		t.Fatalf("PromptTemplate = %q", doc.PromptTemplate)
	}
}

func TestParse_WithFrontMatter(t *testing.T) {
	data := []byte(`---
tracker:
  kind: linear
  project_slug: AG-12
polling:
  interval_ms: 15000
---
# Prompt

Do the work.
`)
	doc, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !doc.HasFrontMatter {
		t.Fatalf("HasFrontMatter = false, want true")
	}
	if doc.PromptTemplate != "# Prompt\n\nDo the work." {
		t.Fatalf("PromptTemplate = %q", doc.PromptTemplate)
	}
	if _, ok := doc.Config["tracker"]; !ok {
		t.Fatalf("tracker missing in config")
	}
}

func TestParse_MissingClosingFence(t *testing.T) {
	_, err := Parse([]byte(`---
tracker:
  kind: linear
`))
	if err == nil {
		t.Fatal("expected error for missing closing fence")
	}
	if !errors.Is(err, ErrInvalidFrontMatter) {
		t.Fatalf("error = %v, want ErrInvalidFrontMatter", err)
	}
}

func TestParse_FrontMatterMustBeMap(t *testing.T) {
	_, err := Parse([]byte(`---
- one
- two
---
body`))
	if err == nil {
		t.Fatal("expected error for non-map frontmatter")
	}
	if !errors.Is(err, ErrFrontMatterNotMap) {
		t.Fatalf("error = %v, want ErrFrontMatterNotMap", err)
	}
}

func TestParse_EmptyFrontMatterBlock(t *testing.T) {
	doc, err := Parse([]byte(`---
---
Prompt`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !doc.HasFrontMatter {
		t.Fatalf("HasFrontMatter = false, want true")
	}
	if len(doc.Config) != 0 {
		t.Fatalf("Config len = %d, want 0", len(doc.Config))
	}
	if doc.PromptTemplate != "Prompt" {
		t.Fatalf("PromptTemplate = %q", doc.PromptTemplate)
	}
}

func TestParse_HandlesUTF8BOM(t *testing.T) {
	data := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`---
tracker:
  kind: linear
---
Prompt`)...)
	doc, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !doc.HasFrontMatter {
		t.Fatalf("HasFrontMatter = false, want true")
	}
	if doc.PromptTemplate != "Prompt" {
		t.Fatalf("PromptTemplate = %q", doc.PromptTemplate)
	}
}

func TestParse_DoesNotCloseOnIndentedFenceLikeText(t *testing.T) {
	data := []byte(`---
hooks:
  before_run: |
    echo "start"
    ---
tracker:
  kind: linear
---
Prompt`)
	doc, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !doc.HasFrontMatter {
		t.Fatal("expected frontmatter")
	}
	if doc.PromptTemplate != "Prompt" {
		t.Fatalf("PromptTemplate = %q", doc.PromptTemplate)
	}
}
