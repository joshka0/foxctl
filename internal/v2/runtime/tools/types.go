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
	propTypes map[string]JSONSchemaType
}

// JSONSchemaType is the supported subset of JSON Schema primitive and object
// types used by v2 tool parameter definitions.
type JSONSchemaType string

const (
	// JSONSchemaTypeObject validates JSON object values.
	JSONSchemaTypeObject JSONSchemaType = "object"
	// JSONSchemaTypeString validates JSON string values.
	JSONSchemaTypeString JSONSchemaType = "string"
	// JSONSchemaTypeBoolean validates JSON boolean values.
	JSONSchemaTypeBoolean JSONSchemaType = "boolean"
	// JSONSchemaTypeNumber validates JSON number values.
	JSONSchemaTypeNumber JSONSchemaType = "number"
	// JSONSchemaTypeInteger validates JSON number values without fractions.
	JSONSchemaTypeInteger JSONSchemaType = "integer"
	// JSONSchemaTypeArray validates JSON array values.
	JSONSchemaTypeArray JSONSchemaType = "array"
)

// JSONSchema is the narrow JSON-schema subset foxctl accepts for v2 tool
// parameter contracts.
type JSONSchema struct {
	Type        JSONSchemaType             `json:"type,omitempty"`
	Description string                     `json:"description,omitempty"`
	Required    []string                   `json:"required,omitempty"`
	Properties  map[string]JSONSchemaField `json:"properties"`
}

// JSONSchemaField describes one property in a v2 tool parameter schema.
type JSONSchemaField struct {
	Type        JSONSchemaType `json:"type,omitempty"`
	Description string         `json:"description,omitempty"`
}

func compileSchema(raw json.RawMessage) (*toolSchema, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var schema JSONSchema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("decode tool schema: %w", err)
	}
	if schema.Type != "" && schema.Type != JSONSchemaTypeObject {
		return nil, fmt.Errorf("tool schema type must be %q, got %q", JSONSchemaTypeObject, schema.Type)
	}

	out := &toolSchema{
		required:  []string{},
		propTypes: map[string]JSONSchemaType{},
	}

	for i, s := range schema.Required {
		s = strings.TrimSpace(s)
		if s == "" {
			return nil, fmt.Errorf("invalid required entry at index %d: empty string", i)
		}
		out.required = append(out.required, s)
	}

	for name, prop := range schema.Properties {
		key := strings.TrimSpace(name)
		if key == "" {
			return nil, fmt.Errorf("invalid property name %q: empty string", name)
		}
		propType := JSONSchemaType(strings.TrimSpace(strings.ToLower(string(prop.Type))))
		if propType == "" {
			continue
		}
		out.propTypes[key] = propType
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

func matchesJSONType(raw json.RawMessage, typ JSONSchemaType) bool {
	if isJSONNull(raw) {
		return false
	}
	switch typ {
	case JSONSchemaTypeString:
		var s string
		return json.Unmarshal(raw, &s) == nil
	case JSONSchemaTypeBoolean:
		var b bool
		return json.Unmarshal(raw, &b) == nil
	case JSONSchemaTypeNumber:
		var n json.Number
		return decodeJSONNumber(raw, &n) == nil
	case JSONSchemaTypeInteger:
		var n json.Number
		if err := decodeJSONNumber(raw, &n); err != nil {
			return false
		}
		f, err := n.Float64()
		if err != nil {
			return false
		}
		return math.Trunc(f) == f
	case JSONSchemaTypeObject:
		var obj map[string]json.RawMessage
		return json.Unmarshal(raw, &obj) == nil && obj != nil
	case JSONSchemaTypeArray:
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
