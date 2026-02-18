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
