package contextplane

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/context/contextengine"
)

const agentBlurredTag = "agent_blurred"

type MemoryBlurAgent interface {
	BlurMemory(ctx context.Context, input MemoryBlurAgentPromptInput) (MemoryBlurAgentOutput, string, error)
}

// MemoryBlurAgentPromptInput is the deterministic contract handed to a real
// agent for lossy structural abstraction.
type MemoryBlurAgentPromptInput struct {
	ID             string                      `json:"id"`
	OriginalDomain string                      `json:"original_domain"`
	Summary        string                      `json:"summary"`
	LiteralText    string                      `json:"literal_text,omitempty"`
	Shape          MemoryStructuralShape       `json:"shape"`
	SourceRefs     []contextengine.EvidenceRef `json:"source_refs,omitempty"`
	SeedSchema     string                      `json:"seed_schema,omitempty"`
	ForbiddenTerms []string                    `json:"forbidden_terms,omitempty"`
}

type MemoryBlurAgentOutput struct {
	AbstractSchema string   `json:"abstract_schema"`
	MechanismTags  []string `json:"mechanism_tags"`
	DomainsToAvoid []string `json:"domains_to_avoid,omitempty"`
	Confidence     float64  `json:"confidence"`
	LeakageRisk    float64  `json:"leakage_risk"`
	Rationale      string   `json:"rationale,omitempty"`
}

type MemoryBlurValidation struct {
	Valid       bool     `json:"valid"`
	Errors      []string `json:"errors,omitempty"`
	LeakedTerms []string `json:"leaked_terms,omitempty"`
}

// BuildMemoryBlurAgentPrompt renders a strict prompt for the real blurring
// agent. It includes literal provenance so the agent can forget it deliberately,
// while the validator checks that the returned schema does not retain it.
func BuildMemoryBlurAgentPrompt(input MemoryBlurAgentPromptInput) (string, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.OriginalDomain = strings.TrimSpace(input.OriginalDomain)
	input.Summary = strings.TrimSpace(input.Summary)
	input.LiteralText = strings.TrimSpace(input.LiteralText)
	input.SeedSchema = strings.TrimSpace(input.SeedSchema)
	input.SourceRefs = compactEvidenceRefs(input.SourceRefs)
	input.ForbiddenTerms = compactStringsInOrder(input.ForbiddenTerms)
	if input.ID == "" {
		return "", fmt.Errorf("blur agent prompt: id is required")
	}
	if input.OriginalDomain == "" {
		return "", fmt.Errorf("blur agent prompt: original_domain is required")
	}
	if input.Summary == "" {
		return "", fmt.Errorf("blur agent prompt: summary is required")
	}
	if strings.TrimSpace(BuildBlurredMechanismSchema(input.Shape)) == "" {
		return "", fmt.Errorf("blur agent prompt: structural shape is required")
	}
	payload, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode blur agent payload: %w", err)
	}
	var b strings.Builder
	b.WriteString("You are the foxctl memory blurring agent. Convert the supplied memory into a lossy structural abstraction.\n")
	b.WriteString("\nRules:\n")
	b.WriteString("- Forget literal names, products, files, packages, symbols, people, and domain nouns.\n")
	b.WriteString("- Preserve the underlying mechanism: actors, operations, signals, constraints, and flow topology.\n")
	b.WriteString("- Do not use any term listed in forbidden_terms in abstract_schema, mechanism_tags, domains_to_avoid, or rationale.\n")
	b.WriteString("- Prefer compact generic language over topic-specific language.\n")
	b.WriteString("- mechanism_tags must be lower_snake_case structural tags.\n")
	b.WriteString("- Return exactly one JSON object and no markdown.\n")
	b.WriteString("\nReturn schema:\n")
	b.WriteString(`{"abstract_schema":"...","mechanism_tags":["..."],"domains_to_avoid":["..."],"confidence":0.0,"leakage_risk":0.0,"rationale":"..."}`)
	b.WriteString("\n\nMemory payload:\n")
	b.Write(payload)
	b.WriteString("\n")
	return b.String(), nil
}

func MemoryBlurPromptInputFromProjection(projection MechanismProjection, shape MemoryStructuralShape, forbiddenTerms []string) MemoryBlurAgentPromptInput {
	return MemoryBlurAgentPromptInput{
		ID:             strings.TrimSpace(projection.ID),
		OriginalDomain: strings.TrimSpace(projection.OriginalDomain),
		Summary:        strings.TrimSpace(projection.Summary),
		LiteralText:    strings.TrimSpace(projection.LiteralText),
		Shape:          shape,
		SourceRefs:     compactEvidenceRefs(projection.SourceRefs),
		SeedSchema:     strings.TrimSpace(projection.AbstractSchema),
		ForbiddenTerms: compactStringsInOrder(forbiddenTerms),
	}
}

func ParseMemoryBlurAgentOutput(text string) (MemoryBlurAgentOutput, error) {
	raw, err := extractFirstJSONObject(text)
	if err != nil {
		return MemoryBlurAgentOutput{}, err
	}
	var output MemoryBlurAgentOutput
	if err := json.Unmarshal([]byte(raw), &output); err != nil {
		return MemoryBlurAgentOutput{}, fmt.Errorf("decode blur agent output: %w", err)
	}
	output.AbstractSchema = strings.TrimSpace(output.AbstractSchema)
	output.MechanismTags = normalizeMechanismTags(output.MechanismTags)
	output.DomainsToAvoid = compactStrings(output.DomainsToAvoid)
	output.Rationale = strings.TrimSpace(output.Rationale)
	return output, nil
}

func ApplyMemoryBlurAgentOutput(projection MechanismProjection, input MemoryBlurAgentPromptInput, output MemoryBlurAgentOutput) (MechanismProjection, MemoryBlurValidation) {
	validation := ValidateMemoryBlurAgentOutput(input, output)
	if !validation.Valid {
		return projection, validation
	}
	projection.AbstractSchema = strings.TrimSpace(output.AbstractSchema)
	projection.MechanismTags = normalizeMechanismTags(output.MechanismTags)
	projection.Tags = compactStrings(append(projection.Tags, agentBlurredTag))
	projection.Tags = compactStrings(append(projection.Tags, output.MechanismTags...))
	return projection, validation
}

func ValidateMemoryBlurAgentOutput(input MemoryBlurAgentPromptInput, output MemoryBlurAgentOutput) MemoryBlurValidation {
	var validation MemoryBlurValidation
	if strings.TrimSpace(output.AbstractSchema) == "" {
		validation.Errors = append(validation.Errors, "abstract_schema is required")
	}
	if len(output.MechanismTags) == 0 {
		validation.Errors = append(validation.Errors, "mechanism_tags are required")
	}
	if output.Confidence <= 0 || output.Confidence > 1 {
		validation.Errors = append(validation.Errors, "confidence must be in (0,1]")
	}
	if output.LeakageRisk < 0 || output.LeakageRisk > 1 {
		validation.Errors = append(validation.Errors, "leakage_risk must be in [0,1]")
	}
	leaked := leakedForbiddenTerms(input.ForbiddenTerms, output)
	if len(leaked) > 0 {
		validation.LeakedTerms = leaked
		validation.Errors = append(validation.Errors, "agent output leaked forbidden literal terms")
	}
	validation.Valid = len(validation.Errors) == 0
	return validation
}

func leakedForbiddenTerms(forbidden []string, output MemoryBlurAgentOutput) []string {
	forbidden = compactStringsInOrder(forbidden)
	if len(forbidden) == 0 {
		return nil
	}
	text := strings.ToLower(strings.Join([]string{
		output.AbstractSchema,
		strings.Join(output.MechanismTags, " "),
		strings.Join(output.DomainsToAvoid, " "),
		output.Rationale,
	}, "\n"))
	seen := map[string]struct{}{}
	var leaked []string
	for _, term := range forbidden {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		if strings.Contains(text, strings.ToLower(term)) {
			if _, ok := seen[term]; ok {
				continue
			}
			seen[term] = struct{}{}
			leaked = append(leaked, term)
		}
	}
	sort.Strings(leaked)
	return leaked
}

func normalizeMechanismTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		tag = strings.ReplaceAll(tag, "-", "_")
		tag = strings.ReplaceAll(tag, " ", "_")
		if tag != "" {
			out = append(out, tag)
		}
	}
	return compactStrings(out)
}

func extractFirstJSONObject(text string) (string, error) {
	start := strings.IndexByte(text, '{')
	if start < 0 {
		return "", fmt.Errorf("blur agent output did not contain a JSON object")
	}
	inString := false
	escaped := false
	depth := 0
	for i := start; i < len(text); i++ {
		ch := text[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start : i+1], nil
			}
			if depth < 0 {
				return "", fmt.Errorf("blur agent output has unbalanced JSON braces")
			}
		}
	}
	return "", fmt.Errorf("blur agent output has no complete JSON object")
}
