package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/joshka0/foxctl/internal/rlm"
	"github.com/joshka0/foxctl/internal/runtime/engine"
	"github.com/joshka0/foxctl/internal/tooling/skillrun/ephemeral"
)

const (
	EphemeralSkillDraftToolName = "ephemeral_skill_draft"
	EphemeralSkillRunToolName   = "ephemeral_skill_run"
)

type EphemeralSkillTools struct {
	mu     sync.Mutex
	runner *ephemeral.GoSkillRunner
}

var _ engine.ToolExecutor = (*EphemeralSkillTools)(nil)

func (e *EphemeralSkillTools) List() []engine.ToolDef {
	return []engine.ToolDef{
		{
			Name:        EphemeralSkillDraftToolName,
			Description: "Register a short-lived Go skill for this attempt. Source must define Solve(input map[string]any) map[string]any.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Optional tool name for metadata. The callable tool remains ephemeral_skill_run."},"description":{"type":"string"},"source":{"type":"string","description":"Go source defining Solve(input map[string]any) map[string]any."},"parameters":{"type":"object","description":"Optional JSON schema for the Solve input."}},"required":["source"],"additionalProperties":false}`),
		},
		{
			Name:        EphemeralSkillRunToolName,
			Description: "Run the registered short-lived Go skill with JSON input.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"input":{"type":"object","description":"JSON input passed to Solve."}},"additionalProperties":false}`),
		},
	}
}

func (e *EphemeralSkillTools) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	switch strings.TrimSpace(name) {
	case EphemeralSkillDraftToolName:
		return e.executeDraft(ctx, args)
	case EphemeralSkillRunToolName:
		return e.executeRun(ctx, args)
	default:
		return "", fmt.Errorf("unknown ephemeral skill tool %q", name)
	}
}

func (e *EphemeralSkillTools) executeDraft(_ context.Context, args json.RawMessage) (string, error) {
	var input struct {
		Name        string          `json:"name,omitempty"`
		Description string          `json:"description,omitempty"`
		Source      flexibleString  `json:"source"`
		Parameters  json.RawMessage `json:"parameters,omitempty"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return marshalEphemeralToolOutput(map[string]any{
			"ok":    false,
			"error": "decode ephemeral skill draft args: " + err.Error(),
		})
	}
	runner, err := ephemeral.NewGoSkillRunner(ephemeral.GoSkillSpec{
		Name:        firstNonEmptyString(input.Name, EphemeralSkillRunToolName),
		Description: input.Description,
		Source:      string(input.Source),
		Parameters:  input.Parameters,
	})
	if err != nil {
		return marshalEphemeralToolOutput(map[string]any{
			"ok":    false,
			"error": err.Error(),
		})
	}
	e.mu.Lock()
	e.runner = runner
	e.mu.Unlock()
	return marshalEphemeralToolOutput(map[string]any{
		"ok":          true,
		"tool":        EphemeralSkillRunToolName,
		"name":        runner.ToolDef().Name,
		"description": runner.ToolDef().Description,
	})
}

func (e *EphemeralSkillTools) executeRun(ctx context.Context, args json.RawMessage) (string, error) {
	var input struct {
		Input map[string]any `json:"input,omitempty"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &input); err != nil {
			return "", fmt.Errorf("decode ephemeral skill run args: %w", err)
		}
	}
	e.mu.Lock()
	runner := e.runner
	e.mu.Unlock()
	if runner == nil {
		return marshalEphemeralToolOutput(map[string]any{
			"ok":    false,
			"error": "no ephemeral Go skill has been drafted",
		})
	}
	result, err := runner.Run(ctx, input.Input)
	if err != nil {
		return marshalEphemeralToolOutput(map[string]any{
			"ok":    false,
			"error": err.Error(),
		})
	}
	return marshalEphemeralToolOutput(result)
}

func marshalEphemeralToolOutput(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func finalAnswerFromEphemeralSkillRun(results []engine.ToolResult) (answer string, raw string, ok bool) {
	for i := len(results) - 1; i >= 0; i-- {
		result := results[i]
		if result.IsError || strings.TrimSpace(result.Content) == "" {
			continue
		}
		answer, ok := finalAnswerFromEphemeralSkillContent(result.Content)
		if ok {
			return answer, result.Content, true
		}
	}
	return "", "", false
}

func finalAnswerFromEphemeralSkillContent(content string) (string, bool) {
	var result struct {
		OK       bool           `json:"ok"`
		Answer   string         `json:"answer"`
		Solution string         `json:"solution"`
		Output   map[string]any `json:"output"`
	}
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return "", false
	}
	if !result.OK {
		return "", false
	}
	for _, value := range []string{result.Answer, result.Solution} {
		value = strings.TrimSpace(value)
		if line, ok := rlm.ExtractSolutionLine(value); ok {
			return line, true
		}
		if strings.HasPrefix(strings.ToLower(value), "solution =") {
			return value, true
		}
	}
	if result.Output == nil {
		return "", false
	}
	for _, key := range []string{"answer", "solution"} {
		value, ok := result.Output[key].(string)
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if line, ok := rlm.ExtractSolutionLine(value); ok {
			return line, true
		}
		if strings.HasPrefix(strings.ToLower(value), "solution =") {
			return value, true
		}
	}
	return "", false
}

type flexibleString string

func (s *flexibleString) UnmarshalJSON(body []byte) error {
	var text string
	if err := json.Unmarshal(body, &text); err == nil {
		*s = flexibleString(text)
		return nil
	}
	var object map[string]any
	if err := json.Unmarshal(body, &object); err != nil {
		return err
	}
	for _, key := range []string{"code", "source", "text"} {
		if value, ok := object[key].(string); ok {
			*s = flexibleString(value)
			return nil
		}
	}
	return fmt.Errorf("source object must contain string code/source/text")
}
