package autoselect

import (
	"testing"

	ws "github.com/jkatigb/agentctl/internal/platform/workspace"
)

func TestNormalizeRepoIndexMode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{in: "", want: "auto"},
		{in: "auto", want: "auto"},
		{in: "search", want: "search"},
		{in: "dag", want: "dag"},
		{in: "dag_grep", want: "dag"},
		{in: "repo_index_dag", want: "dag"},
		{in: "off", want: "off"},
		{in: "none", want: "off"},
		{in: "disabled", want: "off"},
		{in: "weird", want: "auto"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeRepoIndexMode(tc.in); got != tc.want {
				t.Fatalf("NormalizeRepoIndexMode(%q)=%q want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestResolveWorkspaceID(t *testing.T) {
	t.Parallel()

	workspacePath := "/tmp/agentctl"
	if got := resolveWorkspaceID(workspacePath, "custom-id"); got != "custom-id" {
		t.Fatalf("override workspace id=%q want custom-id", got)
	}

	want := ws.ID(workspacePath)
	if got := resolveWorkspaceID(workspacePath, ""); got != want {
		t.Fatalf("resolved workspace id=%q want %q", got, want)
	}
}
