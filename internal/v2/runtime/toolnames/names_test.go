package toolnames

import "testing"

func TestCanonical(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{in: "code/search", want: "code_search"},
		{in: "code.search", want: "code_search"},
		{in: "code_search", want: "code_search"},
		{in: " Code//Search ", want: "code_search"},
		{in: "memory/query", want: "memory_query"},
		{in: "repo/index/dag/grep", want: "repo_index_dag_grep"},
		{in: "fs.read_file", want: "fs_read_file"},
		{in: "", want: ""},
		{in: "   ", want: ""},
		{in: "___", want: ""},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := Canonical(tc.in); got != tc.want {
				t.Fatalf("Canonical(%q)=%q want %q", tc.in, got, tc.want)
			}
		})
	}
}
