package context

import (
	"reflect"
	"testing"
)

func TestExtractTerms(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "simple query",
			query: "how does authentication work",
			want:  []string{"authentication", "work"},
		},
		{
			name:  "technical terms",
			query: "SearchNodes GetEdgesBetween graph store",
			want:  []string{"searchnodes", "getedgesbetween", "graph", "store"},
		},
		{
			name:  "with punctuation",
			query: "find user.Create() and user.Delete() methods",
			want:  []string{"find", "user", "create", "delete", "methods"},
		},
		{
			name:  "mostly stop words",
			query: "what is the function",
			want:  []string{"function"},
		},
		{
			name:  "empty query",
			query: "",
			want:  nil,
		},
		{
			name:  "only stop words",
			query: "the and or is",
			want:  nil,
		},
		{
			name:  "camelCase terms",
			query: "getUserByID createTask",
			want:  []string{"getuserbyid", "createtask"},
		},
		{
			name:  "snake_case terms",
			query: "get_user_by_id create_task",
			want:  []string{"get_user_by_id", "create_task"},
		},
		{
			name:  "duplicate terms",
			query: "auth auth authentication auth",
			want:  []string{"auth", "authentication"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractTerms(tt.query)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractTerms(%q) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}

func TestIsWordChar(t *testing.T) {
	tests := []struct {
		char rune
		want bool
	}{
		{'a', true},
		{'z', true},
		{'A', true},
		{'Z', true},
		{'0', true},
		{'9', true},
		{'_', true},
		{'-', false},
		{'.', false},
		{' ', false},
		{'(', false},
		{')', false},
	}

	for _, tt := range tests {
		t.Run(string(tt.char), func(t *testing.T) {
			got := isWordChar(tt.char)
			if got != tt.want {
				t.Errorf("isWordChar(%q) = %v, want %v", tt.char, got, tt.want)
			}
		})
	}
}

func TestBuildCrossReferences(t *testing.T) {
	tests := []struct {
		name          string
		matchesByTerm map[string][]Block
		wantCount     int
	}{
		{
			name:          "empty matches",
			matchesByTerm: map[string][]Block{},
			wantCount:     0,
		},
		{
			name: "no shared symbols",
			matchesByTerm: map[string][]Block{
				"term1": {
					{File: "a.go", SymbolName: "FuncA", StartLine: 10},
				},
				"term2": {
					{File: "b.go", SymbolName: "FuncB", StartLine: 20},
				},
			},
			wantCount: 0,
		},
		{
			name: "shared symbol across files",
			matchesByTerm: map[string][]Block{
				"term1": {
					{File: "a.go", SymbolName: "SharedFunc", StartLine: 10},
					{File: "b.go", SymbolName: "SharedFunc", StartLine: 20},
				},
			},
			wantCount: 1,
		},
		{
			name: "multiple shared symbols",
			matchesByTerm: map[string][]Block{
				"term1": {
					{File: "a.go", SymbolName: "Func1", StartLine: 10},
					{File: "b.go", SymbolName: "Func1", StartLine: 20},
				},
				"term2": {
					{File: "a.go", SymbolName: "Func2", StartLine: 30},
					{File: "c.go", SymbolName: "Func2", StartLine: 40},
				},
			},
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCrossReferences(tt.matchesByTerm)
			if len(got) != tt.wantCount {
				t.Errorf("buildCrossReferences() returned %d refs, want %d", len(got), tt.wantCount)
			}
		})
	}
}

func TestNewGatherer(t *testing.T) {
	t.Run("default options", func(t *testing.T) {
		g := NewGatherer()
		if g == nil {
			t.Fatal("NewGatherer() returned nil")
			return
		}
		if g.skillResolver == nil {
			t.Error("expected default skill resolver to be set")
		}
	})

	t.Run("with workspace", func(t *testing.T) {
		g := NewGatherer(WithWorkspace("/test/workspace"))
		if g.workspace != "/test/workspace" {
			t.Errorf("workspace = %q, want '/test/workspace'", g.workspace)
		}
	})
}
