package contextplane

import (
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/context/contextengine"
)

func TestBuildMemoryBlurAgentPromptIncludesStrictContract(t *testing.T) {
	projection := MechanismProjection{
		ID:             "memory:1",
		OriginalDomain: "Immunology",
		Summary:        "White blood cells coordinate local defense.",
		LiteralText:    "White blood cells detect virus particles and emit antibodies.",
		AbstractSchema: "mechanism: local autonomous response",
		SourceRefs: []contextengine.EvidenceRef{{
			Type: contextengine.RefTypeMemoryClaim,
			Ref:  "memory:raw",
		}},
	}
	shape := MemoryStructuralShape{
		Mechanism:  "distributed local response",
		Actors:     []string{"edge actor", "local signal"},
		Operations: []string{"detect local signal", "coordinate response"},
	}
	prompt, err := BuildMemoryBlurAgentPrompt(MemoryBlurPromptInputFromProjection(projection, shape, []string{"Immunology", "White blood cells", "antibodies"}))
	if err != nil {
		t.Fatalf("BuildMemoryBlurAgentPrompt() error = %v", err)
	}
	for _, want := range []string{
		"Return exactly one JSON object",
		"forbidden_terms",
		"White blood cells",
		"mechanism_tags",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestParseMemoryBlurAgentOutputExtractsJSONFromNoisyText(t *testing.T) {
	raw := `Warning: ignored config
{"abstract_schema":"local actors detect bounded contention and coordinate a constrained response","mechanism_tags":["Local Coordination","bounded-response"],"domains_to_avoid":["biology"],"confidence":0.81,"leakage_risk":0.02}`
	got, err := ParseMemoryBlurAgentOutput(raw)
	if err != nil {
		t.Fatalf("ParseMemoryBlurAgentOutput() error = %v", err)
	}
	if got.AbstractSchema == "" {
		t.Fatal("abstract schema is empty")
	}
	if len(got.MechanismTags) != 2 || got.MechanismTags[0] != "bounded_response" || got.MechanismTags[1] != "local_coordination" {
		t.Fatalf("mechanism tags=%v", got.MechanismTags)
	}
}

func TestValidateMemoryBlurAgentOutputRejectsLiteralLeakage(t *testing.T) {
	input := MemoryBlurAgentPromptInput{
		ID:             "memory:1",
		OriginalDomain: "Immunology",
		Summary:        "literal source",
		Shape: MemoryStructuralShape{
			Mechanism: "distributed local response",
		},
		ForbiddenTerms: []string{"Immunology", "White blood cells", "antibodies"},
	}
	output := MemoryBlurAgentOutput{
		AbstractSchema: "White blood cells implement a distributed local response",
		MechanismTags:  []string{"distributed_response"},
		Confidence:     0.8,
		LeakageRisk:    0.2,
	}
	validation := ValidateMemoryBlurAgentOutput(input, output)
	if validation.Valid {
		t.Fatal("validation should reject leaked literal term")
	}
	if len(validation.LeakedTerms) != 1 || validation.LeakedTerms[0] != "White blood cells" {
		t.Fatalf("leaked terms=%v", validation.LeakedTerms)
	}
}

func TestApplyMemoryBlurAgentOutputReplacesSchemaAndAddsTags(t *testing.T) {
	projection := MechanismProjection{
		ID:             "memory:1",
		OriginalDomain: "Repo",
		Summary:        "literal source",
		AbstractSchema: "old schema",
		Tags:           []string{"mechanism"},
	}
	input := MemoryBlurAgentPromptInput{
		ID:             projection.ID,
		OriginalDomain: projection.OriginalDomain,
		Summary:        projection.Summary,
		Shape: MemoryStructuralShape{
			Mechanism: "local response",
		},
	}
	output := MemoryBlurAgentOutput{
		AbstractSchema: "local actors transform incoming signals into bounded coordination",
		MechanismTags:  []string{"bounded_coordination"},
		Confidence:     0.9,
		LeakageRisk:    0.01,
	}
	got, validation := ApplyMemoryBlurAgentOutput(projection, input, output)
	if !validation.Valid {
		t.Fatalf("validation errors=%v", validation.Errors)
	}
	if got.AbstractSchema != output.AbstractSchema {
		t.Fatalf("abstract schema=%q want %q", got.AbstractSchema, output.AbstractSchema)
	}
	if !containsString(got.Tags, agentBlurredTag) || !containsString(got.Tags, "bounded_coordination") {
		t.Fatalf("tags=%v", got.Tags)
	}
}
