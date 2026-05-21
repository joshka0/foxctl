package contextplane

import (
	"encoding/json"
	"fmt"
	"strings"
)

const defaultMemoryCollisionAgentRole = "structural_translator"

const (
	MemoryCollisionAgentModeBalanced = "balanced"
	MemoryCollisionAgentModeFar      = "far"
	MemoryCollisionAgentModeAlien    = "alien"
	MemoryCollisionAgentModeFarAlien = "far-alien"
)

type MemoryCollisionAgentPromptInput struct {
	AgentIndex      int                 `json:"agent_index"`
	AgentRole       string              `json:"agent_role"`
	BisociationMode string              `json:"bisociation_mode,omitempty"`
	Query           MechanismQuery      `json:"query"`
	Collision       MemoryCollisionCell `json:"collision"`
}

type MemoryCollisionAgentOutput struct {
	BridgeSchema      string   `json:"bridge_schema"`
	NewCollision      string   `json:"new_collision"`
	TransferSteps     []string `json:"transfer_steps"`
	StressTest        string   `json:"stress_test,omitempty"`
	Risks             []string `json:"risks,omitempty"`
	Confidence        float64  `json:"confidence"`
	NoveltyConfidence float64  `json:"novelty_confidence,omitempty"`
}

type MemoryCollisionAgentValidation struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors,omitempty"`
}

// BuildMemoryCollisionAgentPrompt gives one agent one compact collision packet.
// The caller does fan-out; the agent does not receive the whole corpus.
func BuildMemoryCollisionAgentPrompt(input MemoryCollisionAgentPromptInput) (string, error) {
	input.AgentRole = strings.TrimSpace(input.AgentRole)
	if input.AgentRole == "" {
		input.AgentRole = defaultMemoryCollisionAgentRole
	}
	input.BisociationMode = NormalizeMemoryCollisionAgentMode(input.BisociationMode)
	if input.AgentIndex <= 0 {
		input.AgentIndex = 1
	}
	if strings.TrimSpace(input.Query.Domain) == "" {
		return "", fmt.Errorf("collision agent prompt: query domain is required")
	}
	if strings.TrimSpace(input.Query.Text) == "" {
		return "", fmt.Errorf("collision agent prompt: query text is required")
	}
	if strings.TrimSpace(input.Collision.AbstractSchema) == "" {
		return "", fmt.Errorf("collision agent prompt: abstract schema is required")
	}

	payload := memoryCollisionAgentPromptPayload(input)
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode collision agent payload: %w", err)
	}

	var b strings.Builder
	b.WriteString("You are one bounded foxctl bisociation agent. Your job is to create one useful new collision from one structural memory match.\n")
	b.WriteString("Mode: ")
	b.WriteString(input.BisociationMode)
	b.WriteString("\n")
	b.WriteString("\nRules:\n")
	b.WriteString("- Work only from the supplied query and one collision packet.\n")
	b.WriteString("- Do not inspect files, call tools, ask follow-ups, or invent extra source facts.\n")
	b.WriteString("- Bridge the query domain and memory domain through the abstract schema, not through literal topic overlap.\n")
	b.WriteString("- Do not return IDs, source refs, file paths, or provenance fields; foxctl will attach provenance after validation.\n")
	switch input.BisociationMode {
	case MemoryCollisionAgentModeFar:
		b.WriteString("- Prefer a farther structural analogy over a near literal implementation pattern, while keeping the transfer actionable.\n")
	case MemoryCollisionAgentModeAlien:
		b.WriteString("- Treat concrete names as unavailable; reason only from abstract dynamics, constraints, and signals.\n")
		b.WriteString("- Produce a surprising transfer, then ground it as a concrete implementation move for the target shape.\n")
	case MemoryCollisionAgentModeFarAlien:
		b.WriteString("- Prefer a far structural analogy and ignore near literal implementation patterns.\n")
		b.WriteString("- Treat concrete names as unavailable; reason only from abstract dynamics, constraints, and signals.\n")
		b.WriteString("- Produce a surprising transfer, then ground it as a concrete implementation move for the target shape.\n")
	default:
		b.WriteString("- Keep the answer compact and implementation-oriented.\n")
	}
	b.WriteString("- Return exactly one JSON object and no markdown.\n")
	b.WriteString("\nReturn schema:\n")
	b.WriteString(`{"bridge_schema":"...","new_collision":"...","transfer_steps":["..."],"stress_test":"...","risks":["..."],"confidence":0.0,"novelty_confidence":0.0}`)
	b.WriteString("\n\nCollision payload:\n")
	b.Write(body)
	b.WriteString("\n")
	return b.String(), nil
}

func NormalizeMemoryCollisionAgentMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", MemoryCollisionAgentModeBalanced:
		return MemoryCollisionAgentModeBalanced
	case MemoryCollisionAgentModeFar:
		return MemoryCollisionAgentModeFar
	case MemoryCollisionAgentModeAlien:
		return MemoryCollisionAgentModeAlien
	case MemoryCollisionAgentModeFarAlien:
		return MemoryCollisionAgentModeFarAlien
	default:
		return MemoryCollisionAgentModeBalanced
	}
}

func ParseMemoryCollisionAgentOutput(text string) (MemoryCollisionAgentOutput, error) {
	raw, err := extractFirstJSONObject(text)
	if err != nil {
		return MemoryCollisionAgentOutput{}, err
	}
	var output MemoryCollisionAgentOutput
	if err := json.Unmarshal([]byte(raw), &output); err != nil {
		return MemoryCollisionAgentOutput{}, fmt.Errorf("decode collision agent output: %w", err)
	}
	output.BridgeSchema = strings.TrimSpace(output.BridgeSchema)
	output.NewCollision = strings.TrimSpace(output.NewCollision)
	output.TransferSteps = compactStrings(output.TransferSteps)
	output.StressTest = strings.TrimSpace(output.StressTest)
	output.Risks = compactStrings(output.Risks)
	return output, nil
}

func ValidateMemoryCollisionAgentOutput(input MemoryCollisionAgentPromptInput, output MemoryCollisionAgentOutput) MemoryCollisionAgentValidation {
	var validation MemoryCollisionAgentValidation
	if strings.TrimSpace(output.BridgeSchema) == "" {
		validation.Errors = append(validation.Errors, "bridge_schema is required")
	}
	if strings.TrimSpace(output.NewCollision) == "" {
		validation.Errors = append(validation.Errors, "new_collision is required")
	}
	if len(output.TransferSteps) == 0 {
		validation.Errors = append(validation.Errors, "transfer_steps are required")
	}
	if output.Confidence <= 0 || output.Confidence > 1 {
		validation.Errors = append(validation.Errors, "confidence must be in (0,1]")
	}
	if output.NoveltyConfidence < 0 || output.NoveltyConfidence > 1 {
		validation.Errors = append(validation.Errors, "novelty_confidence must be in [0,1]")
	}
	validation.Valid = len(validation.Errors) == 0
	return validation
}

func memoryCollisionAgentPromptPayload(input MemoryCollisionAgentPromptInput) map[string]any {
	mode := NormalizeMemoryCollisionAgentMode(input.BisociationMode)
	query := input.Query
	query.LiteralVector = nil
	query.StructuralVector = nil
	payload := map[string]any{
		"bisociation_mode":  mode,
		"agent_perspective": input.AgentRole,
	}
	switch mode {
	case MemoryCollisionAgentModeAlien, MemoryCollisionAgentModeFarAlien:
		payload["query"] = map[string]any{
			"domain_alias":    "target-domain",
			"abstract_schema": firstNonEmpty(strings.TrimSpace(query.AbstractSchema), strings.TrimSpace(query.Text)),
			"mechanism_tags":  normalizeMechanismTags(query.MechanismTags),
		}
		payload["collision"] = map[string]any{
			"domain_alias":          "source-domain",
			"abstract_schema":       strings.TrimSpace(input.Collision.AbstractSchema),
			"mechanism_tags":        normalizeMechanismTags(input.Collision.MechanismTags),
			"literal_similarity":    input.Collision.LiteralSimilarity,
			"structural_similarity": input.Collision.StructuralSimilarity,
			"collision_score":       input.Collision.CollisionScore,
		}
	case MemoryCollisionAgentModeFar:
		payload["query"] = map[string]any{
			"domain":          strings.TrimSpace(query.Domain),
			"text":            strings.TrimSpace(query.Text),
			"abstract_schema": strings.TrimSpace(query.AbstractSchema),
			"mechanism_tags":  normalizeMechanismTags(query.MechanismTags),
		}
		payload["collision"] = map[string]any{
			"memory_domain":         strings.TrimSpace(input.Collision.MemoryDomain),
			"abstract_schema":       strings.TrimSpace(input.Collision.AbstractSchema),
			"mechanism_tags":        normalizeMechanismTags(input.Collision.MechanismTags),
			"literal_similarity":    input.Collision.LiteralSimilarity,
			"structural_similarity": input.Collision.StructuralSimilarity,
			"collision_score":       input.Collision.CollisionScore,
			"reason":                strings.TrimSpace(input.Collision.Reason),
		}
	default:
		payload["query"] = map[string]any{
			"domain":          strings.TrimSpace(query.Domain),
			"text":            strings.TrimSpace(query.Text),
			"abstract_schema": strings.TrimSpace(query.AbstractSchema),
			"mechanism_tags":  normalizeMechanismTags(query.MechanismTags),
		}
		payload["collision"] = map[string]any{
			"memory_domain":         strings.TrimSpace(input.Collision.MemoryDomain),
			"memory_summary":        strings.TrimSpace(input.Collision.MemorySummary),
			"abstract_schema":       strings.TrimSpace(input.Collision.AbstractSchema),
			"mechanism_tags":        normalizeMechanismTags(input.Collision.MechanismTags),
			"literal_similarity":    input.Collision.LiteralSimilarity,
			"structural_similarity": input.Collision.StructuralSimilarity,
			"collision_score":       input.Collision.CollisionScore,
			"reason":                strings.TrimSpace(input.Collision.Reason),
		}
	}
	return payload
}
