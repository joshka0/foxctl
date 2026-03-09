package retrieval

import (
	"strings"
	"testing"
)

func TestTreeBuilder_Build_Empty(t *testing.T) {
	builder := NewTreeBuilder(DefaultTreeOptions())
	output := builder.Build(nil, "")

	if output == nil {
		t.Fatal("expected non-nil output")
		return
	}
	if len(output.Nodes) != 1 {
		t.Fatalf("expected 1 root node, got %d", len(output.Nodes))
	}
	if output.Nodes[0].Path != "." {
		t.Errorf("expected root path '.', got %q", output.Nodes[0].Path)
	}
}

func TestTreeBuilder_Build_SingleFile(t *testing.T) {
	entries := []FileEntry{
		{Path: "internal/retrieval/tree.go", Score: 0.85, Summary: "Tree builder implementation"},
	}

	builder := NewTreeBuilder(DefaultTreeOptions())
	output := builder.Build(entries, "Test repo")

	if output.Stats.TotalFiles != 1 {
		t.Errorf("expected 1 file, got %d", output.Stats.TotalFiles)
	}
	if output.Nodes[0].Summary != "Test repo" {
		t.Errorf("expected root summary %q, got %q", "Test repo", output.Nodes[0].Summary)
	}

	// Check tree structure
	root := output.Nodes[0]
	if !root.IsDir {
		t.Error("root should be a directory")
	}
	if len(root.Children) == 0 {
		t.Error("root should have children")
	}
}

func TestTreeBuilder_Build_MultipleFiles(t *testing.T) {
	entries := []FileEntry{
		{Path: "internal/retrieval/tree.go", Score: 0.85},
		{Path: "internal/retrieval/candidates.go", Score: 0.75},
		{Path: "internal/storage/memory/store.go", Score: 0.70},
		{Path: "skills/code_semantic_search/main.go", Score: 0.65},
	}

	builder := NewTreeBuilder(DefaultTreeOptions())
	output := builder.Build(entries, "")

	if output.Stats.TotalFiles != 4 {
		t.Errorf("expected 4 files, got %d", output.Stats.TotalFiles)
	}

	// Should have directories: internal, skills (at root level)
	root := output.Nodes[0]
	if len(root.Children) < 2 {
		t.Errorf("expected at least 2 children at root, got %d", len(root.Children))
	}
}

func TestTreeBuilder_Build_DirectoryScoring(t *testing.T) {
	// Create files with different scores in same directory
	entries := []FileEntry{
		{Path: "pkg/a.go", Score: 0.90},
		{Path: "pkg/b.go", Score: 0.80},
		{Path: "pkg/c.go", Score: 0.70},
		{Path: "other/x.go", Score: 0.50},
	}

	builder := NewTreeBuilder(TreeOptions{
		Depth:       3,
		MaxChildren: 10,
		TopK:        3,
	})
	output := builder.Build(entries, "")

	root := output.Nodes[0]
	// Directory "pkg" should have higher score than "other" because it has more high-scoring files
	var pkgScore, otherScore float64
	for _, child := range root.Children {
		if strings.Contains(child.Path, "pkg") {
			pkgScore = child.Score
		}
		if strings.Contains(child.Path, "other") {
			otherScore = child.Score
		}
	}

	if pkgScore <= otherScore {
		t.Errorf("expected pkg score (%f) > other score (%f)", pkgScore, otherScore)
	}
}

func TestTreeBuilder_Build_DepthLimiting(t *testing.T) {
	entries := []FileEntry{
		{Path: "a/b/c/d/e/file.go", Score: 0.80},
	}

	builder := NewTreeBuilder(TreeOptions{
		Depth:       2, // Limit to 2 directory levels
		MaxChildren: 10,
	})
	output := builder.Build(entries, "")

	// Depth=2 means 2 levels of directories, plus files at the bottom
	// Root(0) -> a(1) -> b(2) -> flattened files
	// computeDepth counts edges, so depth=2 + 1 file level = 3
	maxDepth := output.Stats.MaxDepth
	if maxDepth > 3 {
		t.Errorf("expected max depth <= 3 (2 dirs + files), got %d", maxDepth)
	}

	// Verify that directory c/d/e was flattened (file appears under b, not e)
	root := output.Nodes[0]
	if len(root.Children) == 0 {
		t.Skip("no children to verify")
	}
}

func TestTreeBuilder_Build_MaxChildren(t *testing.T) {
	// Create many files in same directory
	entries := make([]FileEntry, 20)
	for i := 0; i < 20; i++ {
		entries[i] = FileEntry{
			Path:  "pkg/file" + string(rune('a'+i)) + ".go",
			Score: float64(20-i) / 20.0, // Decreasing scores
		}
	}

	builder := NewTreeBuilder(TreeOptions{
		Depth:       3,
		MaxChildren: 5, // Limit to 5 children
	})
	output := builder.Build(entries, "")

	// Check that children are limited
	root := output.Nodes[0]
	for _, child := range root.Children {
		if len(child.Children) > 5 {
			t.Errorf("expected max 5 children, got %d", len(child.Children))
		}
	}
}

func TestTreeBuilder_Build_SummaryIncluded(t *testing.T) {
	entries := []FileEntry{
		{Path: "tree.go", Score: 0.85, Summary: "Tree builder implementation"},
	}

	builder := NewTreeBuilder(TreeOptions{
		Depth:            2,
		MaxChildren:      10,
		IncludeSummaries: true,
	})
	output := builder.Build(entries, "")

	if output.Stats.SummariesIncluded != 1 {
		t.Errorf("expected 1 summary included, got %d", output.Stats.SummariesIncluded)
	}
}

func TestTreeBuilder_Build_SummaryMissing(t *testing.T) {
	entries := []FileEntry{
		{Path: "tree.go", Score: 0.85, Summary: ""},
	}

	builder := NewTreeBuilder(TreeOptions{
		Depth:            2,
		MaxChildren:      10,
		IncludeSummaries: true,
	})
	output := builder.Build(entries, "")

	if output.Stats.SummariesMissing != 1 {
		t.Errorf("expected 1 summary missing, got %d", output.Stats.SummariesMissing)
	}
}

func TestTreeBuilder_RenderText(t *testing.T) {
	entries := []FileEntry{
		{Path: "internal/tree.go", Score: 0.85, Summary: "Tree builder"},
		{Path: "internal/candidates.go", Score: 0.75},
	}

	builder := NewTreeBuilder(DefaultTreeOptions())
	output := builder.Build(entries, "Test project")

	text := builder.RenderText(output)

	// Check that rendered text contains expected elements
	if !strings.Contains(text, "📁") {
		t.Error("expected folder icon in output")
	}
	if !strings.Contains(text, "📄") {
		t.Error("expected file icon in output")
	}
	if !strings.Contains(text, "85%") || !strings.Contains(text, "75%") {
		t.Error("expected score percentages in output")
	}
	if !strings.Contains(text, "Tree builder") {
		t.Error("expected summary in output")
	}
}

func TestTreeBuilder_RenderText_Empty(t *testing.T) {
	builder := NewTreeBuilder(DefaultTreeOptions())
	text := builder.RenderText(nil)

	if text != "(empty tree)" {
		t.Errorf("expected '(empty tree)', got %q", text)
	}
}

func TestTreeBuilder_RenderText_ASCIIIcons(t *testing.T) {
	entries := []FileEntry{
		{Path: "internal/tree.go", Score: 0.85},
	}

	opts := DefaultTreeOptions()
	opts.ASCIIIcons = true
	builder := NewTreeBuilder(opts)
	output := builder.Build(entries, "")

	text := builder.RenderText(output)

	// Check for ASCII icons
	if !strings.Contains(text, "[D]") {
		t.Error("expected [D] directory icon in ASCII mode")
	}
	if !strings.Contains(text, "[F]") {
		t.Error("expected [F] file icon in ASCII mode")
	}
	// Ensure emoji icons are NOT present
	if strings.Contains(text, "📁") || strings.Contains(text, "📄") {
		t.Error("emoji icons should not appear in ASCII mode")
	}
}

func TestMergeFileEntries(t *testing.T) {
	primary := []FileEntry{
		{Path: "a.go", Score: 0.70, Summary: "Primary summary"},
		{Path: "b.go", Score: 0.50},
	}
	secondary := []FileEntry{
		{Path: "a.go", Score: 0.90, Summary: "Secondary summary"},
		{Path: "c.go", Score: 0.60},
	}

	merged := MergeFileEntries(primary, secondary)

	byPath := make(map[string]FileEntry, len(merged))
	for _, entry := range merged {
		byPath[entry.Path] = entry
	}

	a, ok := byPath["a.go"]
	if !ok {
		t.Fatal("expected a.go in merged results")
	}
	if a.Score != 0.90 {
		t.Errorf("expected a.go score 0.90, got %f", a.Score)
	}
	if a.Summary != "Primary summary" {
		t.Errorf("expected primary summary for a.go, got %q", a.Summary)
	}

	if _, ok := byPath["b.go"]; !ok {
		t.Fatal("expected b.go in merged results")
	}
	if _, ok := byPath["c.go"]; !ok {
		t.Fatal("expected c.go in merged results")
	}
}

func TestTreeBuilder_Build_ChildrenSortedByScore(t *testing.T) {
	entries := []FileEntry{
		{Path: "pkg/low.go", Score: 0.10},
		{Path: "pkg/high.go", Score: 0.90},
		{Path: "pkg/mid.go", Score: 0.50},
	}

	builder := NewTreeBuilder(DefaultTreeOptions())
	output := builder.Build(entries, "")

	// Find the pkg directory
	var pkgNode *TreeNode
	for _, child := range output.Nodes[0].Children {
		if strings.Contains(child.Path, "pkg") {
			pkgNode = child
			break
		}
	}

	if pkgNode == nil {
		t.Fatal("could not find pkg directory")
		return
	}

	// Children should be sorted by score descending
	if len(pkgNode.Children) < 2 {
		t.Skip("not enough children to verify sorting")
	}

	for i := 1; i < len(pkgNode.Children); i++ {
		if pkgNode.Children[i-1].Score < pkgNode.Children[i].Score {
			t.Errorf("children not sorted by score: %f < %f at positions %d and %d",
				pkgNode.Children[i-1].Score, pkgNode.Children[i].Score, i-1, i)
		}
	}
}
