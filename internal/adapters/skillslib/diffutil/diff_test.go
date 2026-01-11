package diffutil

import (
	"strings"
	"testing"
)

func TestUnifiedDiff(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		original     string
		modified     string
		contextLines int
		wantEmpty    bool
		wantContains []string
	}{
		{
			name:      "no changes",
			path:      "test.go",
			original:  "hello\nworld\n",
			modified:  "hello\nworld\n",
			wantEmpty: true,
		},
		{
			name:     "simple addition",
			path:     "test.go",
			original: "hello\n",
			modified: "hello\nworld\n",
			wantContains: []string{
				"--- a/test.go",
				"+++ b/test.go",
				"+world",
			},
		},
		{
			name:     "simple deletion",
			path:     "test.go",
			original: "hello\nworld\n",
			modified: "hello\n",
			wantContains: []string{
				"--- a/test.go",
				"+++ b/test.go",
				"-world",
			},
		},
		{
			name:     "modification",
			path:     "main.go",
			original: "func main() {\n\tfmt.Println(\"hello\")\n}\n",
			modified: "func main() {\n\tfmt.Println(\"world\")\n}\n",
			wantContains: []string{
				"--- a/main.go",
				"+++ b/main.go",
				"-\tfmt.Println(\"hello\")",
				"+\tfmt.Println(\"world\")",
			},
		},
		{
			name:         "custom context lines",
			path:         "test.go",
			original:     "line1\nline2\nline3\nline4\nline5\n",
			modified:     "line1\nline2\nCHANGED\nline4\nline5\n",
			contextLines: 1,
			wantContains: []string{
				"@@",
				"-line3",
				"+CHANGED",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff, err := UnifiedDiff(tt.path, tt.original, tt.modified, tt.contextLines)
			if err != nil {
				t.Errorf("UnifiedDiff() error = %v", err)
				return
			}
			if tt.wantEmpty {
				if diff != "" {
					t.Errorf("UnifiedDiff() expected empty, got:\n%s", diff)
				}
				return
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(diff, want) {
					t.Errorf("UnifiedDiff() missing %q in:\n%s", want, diff)
				}
			}
		})
	}
}

func TestUnifiedDiffWithPaths(t *testing.T) {
	diff, err := UnifiedDiffWithPaths(
		"old/file.go",
		"new/file.go",
		"hello\n",
		"world\n",
		3,
	)
	if err != nil {
		t.Errorf("UnifiedDiffWithPaths() error = %v", err)
		return
	}

	if !strings.Contains(diff, "--- old/file.go") {
		t.Errorf("UnifiedDiffWithPaths() missing from header")
	}
	if !strings.Contains(diff, "+++ new/file.go") {
		t.Errorf("UnifiedDiffWithPaths() missing to header")
	}
}

func TestContextDiff(t *testing.T) {
	diff, err := ContextDiff(
		"test.go",
		"hello\n",
		"world\n",
		3,
	)
	if err != nil {
		t.Errorf("ContextDiff() error = %v", err)
		return
	}

	if !strings.Contains(diff, "*** a/test.go") {
		t.Errorf("ContextDiff() missing from header in:\n%s", diff)
	}
	if !strings.Contains(diff, "--- b/test.go") {
		t.Errorf("ContextDiff() missing to header in:\n%s", diff)
	}
}

func TestHasChanges(t *testing.T) {
	if HasChanges("hello", "hello") {
		t.Error("HasChanges() should return false for identical strings")
	}
	if !HasChanges("hello", "world") {
		t.Error("HasChanges() should return true for different strings")
	}
}

func TestGetStats(t *testing.T) {
	diff := `--- a/test.go
+++ b/test.go
@@ -1,3 +1,4 @@
 line1
-line2
+line2a
+line2b
 line3
@@ -10,2 +11,1 @@
-old line
+new line
`
	stats := GetStats(diff)

	if stats.Additions != 3 {
		t.Errorf("GetStats().Additions = %d, want 3", stats.Additions)
	}
	if stats.Deletions != 2 {
		t.Errorf("GetStats().Deletions = %d, want 2", stats.Deletions)
	}
	if stats.Changes != 2 {
		t.Errorf("GetStats().Changes = %d, want 2", stats.Changes)
	}
}

func TestLinesAdded(t *testing.T) {
	diff := `--- a/test.go
+++ b/test.go
@@ -1,1 +1,3 @@
 existing
+new1
+new2
`
	if got := LinesAdded(diff); got != 2 {
		t.Errorf("LinesAdded() = %d, want 2", got)
	}
}

func TestLinesRemoved(t *testing.T) {
	diff := `--- a/test.go
+++ b/test.go
@@ -1,3 +1,1 @@
-old1
-old2
 existing
`
	if got := LinesRemoved(diff); got != 2 {
		t.Errorf("LinesRemoved() = %d, want 2", got)
	}
}

func TestSummary(t *testing.T) {
	tests := []struct {
		name string
		diff string
		want string
	}{
		{
			name: "no changes",
			diff: "",
			want: "no changes",
		},
		{
			name: "additions only",
			diff: `--- a/test.go
+++ b/test.go
@@ -1 +1,3 @@
 line
+new1
+new2
`,
			want: "+2 (1 hunk)",
		},
		{
			name: "deletions only",
			diff: `--- a/test.go
+++ b/test.go
@@ -1,3 +1 @@
-old1
-old2
 line
`,
			want: "-2 (1 hunk)",
		},
		{
			name: "mixed changes",
			diff: `--- a/test.go
+++ b/test.go
@@ -1,2 +1,3 @@
-old
+new1
+new2
 line
`,
			want: "+2 -1 (1 hunk)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Summary(tt.diff); got != tt.want {
				t.Errorf("Summary() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDefaultContextLines(t *testing.T) {
	// When 0 is passed, should use default
	diff, err := UnifiedDiff("test.go", "a\nb\nc\nd\ne\nf\n", "a\nb\nX\nd\ne\nf\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	// Default is 3, so we should see context lines
	if !strings.Contains(diff, " a") || !strings.Contains(diff, " b") {
		t.Error("UnifiedDiff with contextLines=0 should use default context")
	}
}

func TestItoa(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{10, "10"},
		{123, "123"},
		{-5, "-5"},
	}

	for _, tt := range tests {
		if got := itoa(tt.n); got != tt.want {
			t.Errorf("itoa(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
