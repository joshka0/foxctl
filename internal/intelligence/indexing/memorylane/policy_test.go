package memorylane

import "testing"

func TestEligibleTypeSkipsCodeOwnedTypesAndKeepsHumanMemoryTypes(t *testing.T) {
	t.Parallel()

	for _, memoryType := range []string{
		"code_symbol",
		"code_symbol_call",
		"code_symbol_file_meta",
		"file_summary",
		"symbol_summary",
		"file_embedding",
		"file_embedding_chunk",
		"edit",
		"symbol",
		" File_Summary ",
	} {
		if EligibleType(memoryType) {
			t.Fatalf("EligibleType(%q)=true want false", memoryType)
		}
	}

	for _, memoryType := range []string{"decision", "gotcha", "learning", "note", "fact", ""} {
		if !EligibleType(memoryType) {
			t.Fatalf("EligibleType(%q)=false want true", memoryType)
		}
	}
}
