package codefilter

import "testing"

func TestShouldSkipPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "internal/foo/bar.go", want: false},
		{path: "internal/foo/bar_test.go", want: true},
		{path: "pkg/tests/helper.go", want: true},
		{path: "pkg/testdata/input.json", want: true},
		{path: "web/__snapshots__/thing.ts", want: true},
		{path: "src/button.spec.ts", want: true},
		{path: "src/button.test.tsx", want: true},
		{path: "cmd/companion_mailbox_test/main.go", want: true},
		{path: "fixtures/data.go", want: true},
	}

	for _, tt := range tests {
		if got := ShouldSkipPath(tt.path); got != tt.want {
			t.Fatalf("ShouldSkipPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
