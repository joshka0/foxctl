package tools

import (
	"encoding/json"
	"testing"
)

func TestCompileSchema_InvalidRequiredEntry(t *testing.T) {
	t.Parallel()

	_, err := compileSchema(json.RawMessage(`{
		"type":"object",
		"required":["path", 1],
		"properties":{"path":{"type":"string"}}
	}`))
	if err == nil {
		t.Fatal("compileSchema() expected error for non-string required entry")
	}
}

func TestCompileSchema_RejectsEmptyRequiredEntry(t *testing.T) {
	t.Parallel()

	_, err := compileSchema(json.RawMessage(`{
		"type":"object",
		"required":[" "]
	}`))
	if err == nil {
		t.Fatal("compileSchema() expected error for empty required entry")
	}
}

func TestCompileSchema_RejectsMalformedProperty(t *testing.T) {
	t.Parallel()

	_, err := compileSchema(json.RawMessage(`{
		"type":"object",
		"properties":{"path":"string"}
	}`))
	if err == nil {
		t.Fatal("compileSchema() expected error for malformed property")
	}
}

func TestCompileSchema_RejectsNonObjectSchema(t *testing.T) {
	t.Parallel()

	_, err := compileSchema(json.RawMessage(`{
		"type":"array",
		"properties":{"path":{"type":"string"}}
	}`))
	if err == nil {
		t.Fatal("compileSchema() expected error for non-object schema")
	}
}

func TestSchemaObjectBuildsTypedSchema(t *testing.T) {
	t.Parallel()

	raw := schemaObject(
		req("path", JSONSchemaTypeString, "Path to read"),
		prop("max_bytes", JSONSchemaTypeInteger, "Maximum bytes"),
	)

	var schema JSONSchema
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal schemaObject output: %v", err)
	}
	if schema.Type != JSONSchemaTypeObject {
		t.Fatalf("schema type=%q want %q", schema.Type, JSONSchemaTypeObject)
	}
	if got := schema.Properties["path"].Type; got != JSONSchemaTypeString {
		t.Fatalf("path type=%q want %q", got, JSONSchemaTypeString)
	}
	if got := schema.Properties["max_bytes"].Type; got != JSONSchemaTypeInteger {
		t.Fatalf("max_bytes type=%q want %q", got, JSONSchemaTypeInteger)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "path" {
		t.Fatalf("required=%v want [path]", schema.Required)
	}
}

func TestValidateArgs_UnknownTypeRejected(t *testing.T) {
	t.Parallel()

	schema, err := compileSchema(json.RawMessage(`{
		"type":"object",
		"properties":{"id":{"type":"uuid"}}
	}`))
	if err != nil {
		t.Fatalf("compileSchema() error = %v", err)
	}

	err = validateArgs(schema, json.RawMessage(`{"id":"abc-123"}`))
	if err == nil {
		t.Fatal("validateArgs() expected error for unknown schema type")
	}
}

func TestValidateArgs_JSONTypes(t *testing.T) {
	t.Parallel()

	schema, err := compileSchema(json.RawMessage(`{
		"type":"object",
		"required":["name","count","ratio","enabled","meta","tags"],
		"properties":{
			"name":{"type":"string"},
			"count":{"type":"integer"},
			"ratio":{"type":"number"},
			"enabled":{"type":"boolean"},
			"meta":{"type":"object"},
			"tags":{"type":"array"}
		}
	}`))
	if err != nil {
		t.Fatalf("compileSchema() error = %v", err)
	}

	err = validateArgs(schema, json.RawMessage(`{
		"name":"fox",
		"count":2,
		"ratio":2.5,
		"enabled":true,
		"meta":{"source":"test"},
		"tags":["a","b"]
	}`))
	if err != nil {
		t.Fatalf("validateArgs() error = %v", err)
	}
}

func TestValidateArgs_NullDoesNotMatchType(t *testing.T) {
	t.Parallel()

	schema, err := compileSchema(json.RawMessage(`{
		"type":"object",
		"properties":{"name":{"type":"string"}}
	}`))
	if err != nil {
		t.Fatalf("compileSchema() error = %v", err)
	}

	err = validateArgs(schema, json.RawMessage(`{"name":null}`))
	if err == nil {
		t.Fatal("validateArgs() expected null type mismatch")
	}
}
