package inlineutil

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  Mode
		ok    bool
	}{
		{name: "empty defaults to auto", value: "", want: ModeAuto, ok: true},
		{name: "auto", value: "auto", want: ModeAuto, ok: true},
		{name: "full", value: "full", want: ModeFull, ok: true},
		{name: "preview", value: "preview", want: ModePreview, ok: true},
		{name: "artifact only", value: "artifact_only", want: ModeArtifactOnly, ok: true},
		{name: "trim and lower", value: " PREVIEW ", want: ModePreview, ok: true},
		{name: "invalid", value: "bad", want: ModeAuto, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Parse(tt.value)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("Parse(%q)=(%q,%v), want (%q,%v)", tt.value, got, ok, tt.want, tt.ok)
			}
		})
	}
}
