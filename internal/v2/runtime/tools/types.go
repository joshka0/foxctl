package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	coretool "github.com/joshka0/foxctl/internal/v2/core/tool"
)

type compiledTool struct {
	def    coretool.ToolDef
	schema *toolSchema
}

type toolSchema struct {
	required  []string
	propTypes map[string]string
}

type rawToolSchema struct {
	Required   []string                  `json:"required"`
	Properties map[string]schemaProperty `json:"properties"`
}

type schemaProperty struct {
	Type string `json:"type"`
}

func compileSchema(raw json.RawMessage) (*toolSchema, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var schema rawToolSchema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("decode tool schema: %w", err)
	}

	out := &toolSchema{
		required:  []string{},
		propTypes: map[string]string{},
	}

	for i, s := range schema.Required {
		s = strings.TrimSpace(s)
		if s == "" {
			return nil, fmt.Errorf("invalid required entry at index %d: empty string", i)
		}
		out.required = append(out.required, s)
	}

	for name, prop := range schema.Properties {
		propType := strings.TrimSpace(strings.ToLower(prop.Type))
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

	var obj map[string]json.RawMessage
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

func matchesJSONType(raw json.RawMessage, typ string) bool {
	if isJSONNull(raw) {
		return false
	}
	switch typ {
	case "string":
		var s string
		return json.Unmarshal(raw, &s) == nil
	case "boolean":
		var b bool
		return json.Unmarshal(raw, &b) == nil
	case "number":
		var n json.Number
		return decodeJSONNumber(raw, &n) == nil
	case "integer":
		var n json.Number
		if err := decodeJSONNumber(raw, &n); err != nil {
			return false
		}
		f, err := n.Float64()
		if err != nil {
			return false
		}
		return math.Trunc(f) == f
	case "object":
		var obj map[string]json.RawMessage
		return json.Unmarshal(raw, &obj) == nil && obj != nil
	case "array":
		var arr []json.RawMessage
		return json.Unmarshal(raw, &arr) == nil && arr != nil
	default:
		return false
	}
}

func decodeJSONNumber(raw json.RawMessage, dst *json.Number) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(dst)
}

func isJSONNull(raw json.RawMessage) bool {
	return string(bytes.TrimSpace(raw)) == "null"
}
