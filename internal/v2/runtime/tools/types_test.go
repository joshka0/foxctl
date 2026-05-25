package tools

import (
	"encoding/json"
	"fmt"
	"testing"
	"testing/quick"
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

func TestCompileSchema_RejectsRequiredFieldWithoutProperty(t *testing.T) {
	t.Parallel()

	_, err := compileSchema(json.RawMessage(`{
		"type":"object",
		"required":["path"],
		"properties":{"limit":{"type":"integer"}}
	}`))
	if err == nil {
		t.Fatal("compileSchema() expected error for required field without declared property")
	}
}

func TestCompileSchema_RejectsDuplicateRequiredEntryAfterTrim(t *testing.T) {
	t.Parallel()

	_, err := compileSchema(json.RawMessage(`{
		"type":"object",
		"required":["path"," path "],
		"properties":{"path":{"type":"string"}}
	}`))
	if err == nil {
		t.Fatal("compileSchema() expected error for duplicate required entry")
	}
}

func TestCompileSchema_RejectsUnknownPropertyType(t *testing.T) {
	t.Parallel()

	_, err := compileSchema(json.RawMessage(`{
		"type":"object",
		"properties":{"id":{"type":"uuid"}}
	}`))
	if err == nil {
		t.Fatal("compileSchema() expected error for unknown property type")
	}
}

func TestCompileSchema_RejectsPropertyNameCollisionAfterTrim(t *testing.T) {
	t.Parallel()

	_, err := compileSchema(json.RawMessage(`{
		"type":"object",
		"properties":{
			"path":{"type":"string"},
			" path ":{"type":"integer"}
		}
	}`))
	if err == nil {
		t.Fatal("compileSchema() expected error for property name collision")
	}
}

func TestCompileSchemaPropertyKnownTypesValidateMatchingValues(t *testing.T) {
	t.Parallel()

	property := func(typeSeed uint8, required bool) bool {
		fixture := schemaTypeFixture(typeSeed)
		requiredJSON := "[]"
		if required {
			requiredJSON = `["value"]`
		}
		raw := json.RawMessage(fmt.Sprintf(`{
			"type":"object",
			"required":%s,
			"properties":{"value":{"type":%q}}
		}`, requiredJSON, fixture.typ))

		schema, err := compileSchema(raw)
		if err != nil {
			t.Logf("compileSchema(%s) error=%v", raw, err)
			return false
		}
		if err := validateArgs(schema, json.RawMessage(`{"value":`+fixture.validJSON+`}`)); err != nil {
			t.Logf("validateArgs valid %s for %s error=%v", fixture.validJSON, fixture.typ, err)
			return false
		}
		if err := validateArgs(schema, json.RawMessage(`{"value":null}`)); err == nil {
			t.Logf("validateArgs null for %s unexpectedly passed", fixture.typ)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatal(err)
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

	_, err := compileSchema(json.RawMessage(`{
		"type":"object",
		"properties":{"id":{"type":"uuid"}}
	}`))
	if err == nil {
		t.Fatal("compileSchema() expected error for unknown schema type")
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

type schemaFixture struct {
	typ       string
	validJSON string
}

func schemaTypeFixture(seed uint8) schemaFixture {
	fixtures := []schemaFixture{
		{typ: string(JSONSchemaTypeString), validJSON: `"fox"`},
		{typ: string(JSONSchemaTypeBoolean), validJSON: `true`},
		{typ: string(JSONSchemaTypeNumber), validJSON: `2.5`},
		{typ: string(JSONSchemaTypeInteger), validJSON: `2`},
		{typ: string(JSONSchemaTypeObject), validJSON: `{"nested":true}`},
		{typ: string(JSONSchemaTypeArray), validJSON: `["a","b"]`},
	}
	return fixtures[int(seed)%len(fixtures)]
}
