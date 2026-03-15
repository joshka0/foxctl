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
