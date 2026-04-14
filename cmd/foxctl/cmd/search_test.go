package cmd

import (
	"reflect"
	"testing"
)

func TestNormalizeSearchScopes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{
			name:  "dedupe lower trim",
			input: []string{" Sessions ", "sessions", "MEMORIES", "memories", ""},
			want:  []string{"sessions", "memories"},
		},
		{
			name:  "empty input",
			input: nil,
			want:  nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeSearchScopes(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("normalizeSearchScopes(%v)=%v want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsSessionsOnlyScope(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input []string
		want  bool
	}{
		{name: "sessions only", input: []string{"sessions"}, want: true},
		{name: "empty", input: nil, want: false},
		{name: "mixed", input: []string{"sessions", "symbols"}, want: false},
		{name: "other", input: []string{"symbols"}, want: false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isSessionsOnlyScope(tc.input); got != tc.want {
				t.Fatalf("isSessionsOnlyScope(%v)=%v want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestScopesIncludeSessions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input []string
		want  bool
	}{
		{name: "empty means all includes sessions", input: nil, want: true},
		{name: "sessions included", input: []string{"symbols", "sessions"}, want: true},
		{name: "sessions only", input: []string{"sessions"}, want: true},
		{name: "no sessions", input: []string{"symbols", "tasks"}, want: false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := scopesIncludeSessions(tc.input); got != tc.want {
				t.Fatalf("scopesIncludeSessions(%v)=%v want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestRemoveSearchScope(t *testing.T) {
	t.Parallel()

	got := removeSearchScope([]string{"symbols", "sessions", "tasks"}, "sessions")
	want := []string{"symbols", "tasks"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("removeSearchScope()=%v want %v", got, want)
	}
}
