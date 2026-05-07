package dbdriver

import (
	"testing"
)

func TestPgCosineSimilarity(t *testing.T) {
	vec := Vector{0.1, 0.2, 0.3}
	got := pgCosineSimilarity("embedding", vec)
	want := "(1 - (\"embedding\" <=> '[0.100000,0.200000,0.300000]'))"
	if got != want {
		t.Errorf("pgCosineSimilarity()\n  got:  %q\n  want: %q", got, want)
	}
}

func TestPgCosineSimilarityScore(t *testing.T) {
	vec := Vector{0.1, 0.2, 0.3}
	got := pgCosineSimilarityScore("embedding", vec)
	want := "((1 - (\"embedding\" <=> '[0.100000,0.200000,0.300000]')) + 1.0) / 2.0"
	if got != want {
		t.Errorf("pgCosineSimilarityScore()\n  got:  %q\n  want: %q", got, want)
	}
}

func TestCosineSimilarityExpr(t *testing.T) {
	vec := Vector{0.1, 0.2, 0.3}
	tests := []struct {
		driver DriverType
		want   string
	}{
		{
			driver: DriverPostgres,
			want:   "(1 - (\"embedding\" <=> '[0.100000,0.200000,0.300000]'))",
		},
		{
			driver: DriverTurso,
			want:   "vector_distance_cos(embedding, vector32('[0.100000,0.200000,0.300000]'))",
		},
	}

	for _, tt := range tests {
		got := cosineSimilarityExpr(tt.driver, "embedding", vec)
		if got != tt.want {
			t.Errorf("cosineSimilarityExpr(%s)\n  got:  %q\n  want: %q", tt.driver, got, tt.want)
		}
	}
}

func TestCosineSimilarityScoreExpr(t *testing.T) {
	vec := Vector{0.1, 0.2, 0.3}
	tests := []struct {
		driver DriverType
		want   string
	}{
		{
			driver: DriverPostgres,
			want:   "((1 - (\"embedding\" <=> '[0.100000,0.200000,0.300000]')) + 1.0) / 2.0",
		},
		{
			driver: DriverTurso,
			want:   "(1 - (vector_distance_cos(embedding, vector32('[0.100000,0.200000,0.300000]')) / 2.0))",
		},
	}

	for _, tt := range tests {
		got := cosineSimilarityScoreExpr(tt.driver, "embedding", vec)
		if got != tt.want {
			t.Errorf("cosineSimilarityScoreExpr(%s)\n  got:  %q\n  want: %q", tt.driver, got, tt.want)
		}
	}
}

func TestPgEuclideanDistance(t *testing.T) {
	vec := Vector{0.1, 0.2, 0.3}
	got := pgEuclideanDistance("embedding", vec)
	want := "(\"embedding\" <-> '[0.100000,0.200000,0.300000]')"
	if got != want {
		t.Errorf("pgEuclideanDistance()\n  got:  %q\n  want: %q", got, want)
	}
}

func TestPgVectorExpression(t *testing.T) {
	vec := Vector{0.5, 0.6}
	got := pgVectorExpression(vec)
	want := "'[0.500000,0.600000]'"
	if got != want {
		t.Errorf("pgVectorExpression()\n  got:  %q\n  want: %q", got, want)
	}
}

func TestPgCreateVectorColumnSQL(t *testing.T) {
	got := pgCreateVectorColumnSQL("named_memory", "embedding", 1024)
	want := "ALTER TABLE \"named_memory\" ADD COLUMN IF NOT EXISTS \"embedding\" vector(1024)"
	if got != want {
		t.Errorf("pgCreateVectorColumnSQL()\n  got:  %q\n  want: %q", got, want)
	}
}

func TestPgCreateVectorIndexSQL(t *testing.T) {
	got := pgCreateVectorIndexSQL("named_memory", "embedding", "idx_memory_embedding_hnsw")
	want := "CREATE INDEX IF NOT EXISTS \"idx_memory_embedding_hnsw\" ON \"named_memory\" USING hnsw (\"embedding\" vector_cosine_ops) WITH (m = 16, ef_construction = 64)"
	if got != want {
		t.Errorf("pgCreateVectorIndexSQL()\n  got:  %q\n  want: %q", got, want)
	}
}

func TestPgQuoteIdent(t *testing.T) {
	tests := []struct {
		name, input, want string
	}{
		{"simple", "embedding", `"embedding"`},
		{"with_dot", "t.embedding", `"t.embedding"`},
		{"with_quote", `my"col`, `"my""col"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pgQuoteIdent(tt.input)
			if got != tt.want {
				t.Errorf("pgQuoteIdent(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
