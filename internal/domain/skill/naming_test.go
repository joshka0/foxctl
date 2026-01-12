package skill

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeSkillName_Exported(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "canonical with slash",
			input:    "code/semantic_search",
			expected: "code_semantic_search",
		},
		{
			name:     "nested category",
			input:    "code/context/ripgrep",
			expected: "code_context_ripgrep",
		},
		{
			name:     "dash to underscore",
			input:    "my-skill",
			expected: "my_skill",
		},
		{
			name:     "mixed slash and dash",
			input:    "text/find-replace",
			expected: "text_find_replace",
		},
		{
			name:     "already normalized",
			input:    "code_semantic_search",
			expected: "code_semantic_search",
		},
		{
			name:     "simple name",
			input:    "simple",
			expected: "simple",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeSkillName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNormalizeSkillName_ConsistentWithInternal(t *testing.T) {
	// Ensure the exported function matches the internal alias
	testCases := []string{
		"code/semantic_search",
		"text/grep",
		"my-skill",
		"already_normalized",
	}

	for _, tc := range testCases {
		exported := NormalizeSkillName(tc)
		internal := normalizeSkillName(tc)
		assert.Equal(t, exported, internal, "exported and internal should match for %q", tc)
	}
}
