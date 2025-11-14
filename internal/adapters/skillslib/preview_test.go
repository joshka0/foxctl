package skillslib

import "testing"

func TestPreparePreview(t *testing.T) {
	type item struct{ v int }

	tests := []struct {
		name  string
		max   int
		in    []item
		want  []item
		trunc bool
	}{
		{
			name: "short list", in: []item{{1}, {2}}, max: 5,
			want: []item{{1}, {2}}, trunc: false,
		},
		{
			name: "exact length", in: []item{{1}, {2}}, max: 2,
			want: []item{{1}, {2}}, trunc: false,
		},
		{
			name: "truncate", in: []item{{1}, {2}, {3}}, max: 2,
			want: []item{{1}, {2}}, trunc: true,
		},
		{
			name: "zero max", in: []item{{1}, {2}}, max: 0,
			want: []item{{1}, {2}}, trunc: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, truncated := PreparePreview(tt.in, tt.max)
			if truncated != tt.trunc {
				t.Fatalf("truncated=%v want %v", truncated, tt.trunc)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("len=%d want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("item[%d]=%v want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}
