package agentprompt

import (
	"strings"
	"testing"

	agenttypes "github.com/joshka0/foxctl/internal/agent/types"
)

func TestInstruction_CodeScoutsRequireStructuredJSON(t *testing.T) {
	t.Parallel()

	cases := []struct {
		role     agenttypes.AgentRole
		contains []string
	}{
		{
			role: agenttypes.RoleSemanticScout,
			contains: []string{
				"Return JSON only.",
				"If the caller provides a schema, satisfy it exactly.",
				"Verify the strongest 1-3 candidate files with smart_search",
			},
		},
		{
			role: agenttypes.RoleDAGScout,
			contains: []string{
				"Return JSON only.",
				"If the caller provides a schema, satisfy it exactly.",
				"Verify the most important 1-3 nodes with repo_index_open",
			},
		},
		{
			role: agenttypes.RoleSymbolScout,
			contains: []string{
				"Return JSON only.",
				"If the caller provides a schema, satisfy it exactly.",
				"Verify the strongest 1-3 symbols or caller sites with context_grep",
			},
		},
		{
			role: agenttypes.RoleAnnotationScout,
			contains: []string{
				"Return JSON only.",
				"If the caller provides a schema, satisfy it exactly.",
				"Verify the most important findings with a second recall pass",
			},
		},
	}

	for _, tc := range cases {
		instruction := Instruction(tc.role)
		for _, want := range tc.contains {
			if !strings.Contains(instruction, want) {
				t.Fatalf("%s instruction missing %q\n%s", tc.role, want, instruction)
			}
		}
	}
}
