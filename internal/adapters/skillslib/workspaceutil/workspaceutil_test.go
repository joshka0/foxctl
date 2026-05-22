package workspaceutil

import (
	"path/filepath"
	"testing"
)

func TestResolvePath(t *testing.T) {
	base := t.TempDir()
	absoluteOverride := t.TempDir()

	tests := []struct {
		name     string
		base     string
		override string
		want     string
		wantErr  bool
	}{
		{
			name: "uses base when override is empty",
			base: base,
			want: base,
		},
		{
			name:     "resolves relative override under base",
			base:     base,
			override: "subdir",
			want:     filepath.Join(base, "subdir"),
		},
		{
			name:     "keeps absolute override",
			base:     base,
			override: absoluteOverride,
			want:     absoluteOverride,
		},
		{
			name:    "requires workspace",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolvePath(tt.base, tt.override)
			if tt.wantErr {
				if err == nil {
					t.Fatal("ResolvePath returned nil error, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolvePath returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ResolvePath()=%q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolvePathWithFallback(t *testing.T) {
	base := t.TempDir()
	cwd := t.TempDir()
	t.Chdir(cwd)

	tests := []struct {
		name     string
		base     string
		override string
		fallback string
		want     string
	}{
		{
			name:     "uses fallback when base and override are empty",
			fallback: ".",
			want:     cwd,
		},
		{
			name:     "base wins over fallback",
			base:     base,
			fallback: ".",
			want:     base,
		},
		{
			name:     "relative override resolves under base before fallback",
			base:     base,
			override: "nested",
			fallback: ".",
			want:     filepath.Join(base, "nested"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolvePathWithFallback(tt.base, tt.override, tt.fallback)
			if err != nil {
				t.Fatalf("ResolvePathWithFallback returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ResolvePathWithFallback()=%q, want %q", got, tt.want)
			}
		})
	}
}
