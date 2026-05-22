package contextplane

import (
	"strings"
	"testing"
)

func TestBuildMemoryCollisionAgentPromptKeepsPacketBounded(t *testing.T) {
	input := validMemoryCollisionAgentPromptInput()
	input.Query.LiteralVector = []float32{1, 0, 0}
	input.Query.StructuralVector = []float32{0, 1, 0}

	prompt, err := BuildMemoryCollisionAgentPrompt(input)
	if err != nil {
		t.Fatalf("BuildMemoryCollisionAgentPrompt: %v", err)
	}
	if !strings.Contains(prompt, `"agent_perspective": "constraint_translator"`) {
		t.Fatalf("prompt missing agent perspective:\n%s", prompt)
	}
	if strings.Contains(prompt, "literal_vector") || strings.Contains(prompt, "structural_vector") {
		t.Fatalf("prompt leaked vector payload:\n%s", prompt)
	}
	for _, leaked := range []string{"collision_id", "memory_id", "source_refs", input.Collision.CollisionID, input.Collision.MemoryID, input.Query.ID} {
		if strings.Contains(prompt, leaked) {
			t.Fatalf("prompt leaked orchestration/provenance field %q:\n%s", leaked, prompt)
		}
	}
	if !strings.Contains(prompt, input.Collision.AbstractSchema) {
		t.Fatalf("prompt missing collision packet:\n%s", prompt)
	}
}

func TestBuildMemoryCollisionAgentPromptAlienModeHidesConcreteLabels(t *testing.T) {
	input := validMemoryCollisionAgentPromptInput()
	input.BisociationMode = MemoryCollisionAgentModeFarAlien
	input.Query.AbstractSchema = "local priority clearing through decentralized pressure relief"

	prompt, err := BuildMemoryCollisionAgentPrompt(input)
	if err != nil {
		t.Fatalf("BuildMemoryCollisionAgentPrompt: %v", err)
	}
	required := []string{
		"Mode: far-alien",
		`"domain_alias": "target-domain"`,
		`"domain_alias": "source-domain"`,
		input.Query.AbstractSchema,
		input.Collision.AbstractSchema,
	}
	for _, needle := range required {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("alien prompt missing %q:\n%s", needle, prompt)
		}
	}
	for _, leaked := range []string{
		input.Query.Domain,
		input.Query.Text,
		input.Collision.MemoryDomain,
		input.Collision.MemorySummary,
		input.Collision.MemoryID,
		input.Collision.CollisionID,
	} {
		if strings.Contains(prompt, leaked) {
			t.Fatalf("alien prompt leaked concrete label %q:\n%s", leaked, prompt)
		}
	}
}

func TestParseValidateMemoryCollisionAgentOutput(t *testing.T) {
	input := validMemoryCollisionAgentPromptInput()
	raw := `preface
{"bridge_schema":"local pressure relief","new_collision":"Use local queues as pressure valves.","transfer_steps":["detect local pressure","grant bounded bypass"],"stress_test":"central outage","risks":["overrouting"],"confidence":0.83,"novelty_confidence":0.72}`

	output, err := ParseMemoryCollisionAgentOutput(raw)
	if err != nil {
		t.Fatalf("ParseMemoryCollisionAgentOutput: %v", err)
	}
	validation := ValidateMemoryCollisionAgentOutput(input, output)
	if !validation.Valid {
		t.Fatalf("validation failed: %#v", validation)
	}
	if output.NewCollision == "" || len(output.TransferSteps) != 2 {
		t.Fatalf("unexpected output: %#v", output)
	}
}

func TestValidateMemoryCollisionAgentOutputDoesNotRequireProvenanceFields(t *testing.T) {
	input := validMemoryCollisionAgentPromptInput()
	validation := ValidateMemoryCollisionAgentOutput(input, MemoryCollisionAgentOutput{
		BridgeSchema:      "local pressure relief",
		NewCollision:      "Use local queues as pressure valves.",
		TransferSteps:     []string{"detect local pressure"},
		Confidence:        0.8,
		NoveltyConfidence: 0.5,
	})
	if !validation.Valid {
		t.Fatalf("validation should not require orchestration provenance fields: %#v", validation)
	}
}

func validMemoryCollisionAgentPromptInput() MemoryCollisionAgentPromptInput {
	return MemoryCollisionAgentPromptInput{
		AgentIndex: 1,
		AgentRole:  "constraint_translator",
		Query: MechanismQuery{
			ID:            "query-traffic",
			Domain:        "Urban Logistics",
			Text:          "Clear emergency vehicles without central dispatch.",
			MechanismTags: []string{"local_clearance"},
		},
		Collision: MemoryCollisionCell{
			CollisionID:          "memory_collision:test",
			MemoryID:             "memory-immunology",
			MemoryDomain:         "Immunology",
			MemorySummary:        "White cells coordinate locally.",
			AbstractSchema:       "Decentralized edge-node response with local escalation.",
			MechanismTags:        []string{"distributed_response"},
			LiteralSimilarity:    0.12,
			StructuralSimilarity: 0.98,
			CollisionScore:       1.20,
			Reason:               "structural match across domains",
		},
	}
}
