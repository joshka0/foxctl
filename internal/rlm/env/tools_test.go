package env

import (
	"encoding/json"
	"testing"
)

func TestDefaultToolsExposeSchemas(t *testing.T) {
	t.Parallel()

	tools := DefaultTools()
	if len(tools) == 0 {
		t.Fatal("expected non-empty tool catalog")
	}
	for _, tool := range tools {
		if len(tool.Parameters) == 0 {
			t.Fatalf("tool %s missing parameter schema", tool.Name)
		}
		var schema struct {
			Type       string                 `json:"type"`
			Properties map[string]interface{} `json:"properties"`
		}
		if err := json.Unmarshal(tool.Parameters, &schema); err != nil {
			t.Fatalf("tool %s schema decode: %v", tool.Name, err)
		}
		if schema.Type != "object" {
			t.Fatalf("tool %s schema type=%q want object", tool.Name, schema.Type)
		}
		if schema.Properties == nil {
			t.Fatalf("tool %s schema missing properties", tool.Name)
		}
	}
}

func TestDefaultToolsExposeGatherContext(t *testing.T) {
	t.Parallel()

	for _, tool := range DefaultTools() {
		if tool.Name != "gather_context" {
			continue
		}
		if !tool.ReadOnly {
			t.Fatal("gather_context must be read-only")
		}
		var schema struct {
			Required   []string               `json:"required"`
			Properties map[string]interface{} `json:"properties"`
		}
		if err := json.Unmarshal(tool.Parameters, &schema); err != nil {
			t.Fatalf("schema decode: %v", err)
		}
		if _, ok := schema.Properties["query"]; !ok {
			t.Fatalf("gather_context schema missing query: %v", schema.Properties)
		}
		if _, ok := schema.Properties["lanes"]; !ok {
			t.Fatalf("gather_context schema missing lanes: %v", schema.Properties)
		}
		if _, ok := schema.Properties["task_type"]; !ok {
			t.Fatalf("gather_context schema missing task_type: %v", schema.Properties)
		}
		if _, ok := schema.Properties["memory_statuses"]; !ok {
			t.Fatalf("gather_context schema missing memory_statuses: %v", schema.Properties)
		}
		for _, required := range schema.Required {
			if required == "query" {
				return
			}
		}
		t.Fatalf("gather_context required=%v want query", schema.Required)
	}
	t.Fatal("DefaultTools() missing gather_context")
}
