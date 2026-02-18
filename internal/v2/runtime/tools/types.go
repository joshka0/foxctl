package tools

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	coretool "github.com/jkatigb/agentctl/internal/v2/core/tool"
)

type compiledTool struct {
	def    coretool.ToolDef
	schema *toolSchema
}

type toolSchema struct {
	required  []string
	propTypes map[string]string
}

func compileSchema(raw json.RawMessage) (*toolSchema, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("decode tool schema: %w", err)
	}

	out := &toolSchema{
		required:  []string{},
		propTypes: map[string]string{},
	}

	if required, ok := schema["required"].([]any); ok {
		for _, v := range required {
			s, ok := v.(string)
			if !ok {
				continue
			}
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			out.required = append(out.required, s)
		}
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		return out, nil
	}
	for name, value := range props {
		propObj, ok := value.(map[string]any)
		if !ok {
			continue
		}
		propType, _ := propObj["type"].(string)
		propType = strings.TrimSpace(strings.ToLower(propType))
		if propType == "" {
			continue
		}
		out.propTypes[name] = propType
	}

	return out, nil
}

func validateArgs(schema *toolSchema, args json.RawMessage) error {
	if schema == nil {
		return nil
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}

	var obj map[string]any
	if err := json.Unmarshal(args, &obj); err != nil {
		return fmt.Errorf("invalid args json: %w", err)
	}

	for _, key := range schema.required {
		if _, ok := obj[key]; !ok {
			return fmt.Errorf("missing required field %q", key)
		}
	}

	for key, expected := range schema.propTypes {
		val, ok := obj[key]
		if !ok {
			continue
		}
		if !matchesJSONType(val, expected) {
			return fmt.Errorf("field %q must be %s", key, expected)
		}
	}
	return nil
}

func matchesJSONType(v any, typ string) bool {
	switch typ {
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "number":
		_, ok := v.(float64)
		return ok
	case "integer":
		f, ok := v.(float64)
		if !ok {
			return false
		}
		return math.Trunc(f) == f
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	default:
		return true
	}
}
