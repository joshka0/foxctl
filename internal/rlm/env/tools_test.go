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
		if _, ok := schema.Properties["graph_mode"]; !ok {
			t.Fatalf("gather_context schema missing graph_mode: %v", schema.Properties)
		}
		graphMode, _ := schema.Properties["graph_mode"].(map[string]interface{})
		enumValues, _ := graphMode["enum"].([]interface{})
		if len(enumValues) != 3 {
			t.Fatalf("gather_context graph_mode enum=%v", graphMode["enum"])
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

func TestDefaultToolsExposeSpecializedGatherContextSurfaces(t *testing.T) {
	t.Parallel()

	found := map[string]bool{}
	for _, tool := range DefaultTools() {
		switch tool.Name {
		case "gather_test_context", "gather_docs_context":
			found[tool.Name] = true
			if !tool.ReadOnly {
				t.Fatalf("%s must be read-only", tool.Name)
			}
			var schema struct {
				Required   []string               `json:"required"`
				Properties map[string]interface{} `json:"properties"`
			}
			if err := json.Unmarshal(tool.Parameters, &schema); err != nil {
				t.Fatalf("%s schema decode: %v", tool.Name, err)
			}
			if _, ok := schema.Properties["query"]; !ok {
				t.Fatalf("%s schema missing query: %v", tool.Name, schema.Properties)
			}
			requiredQuery := false
			for _, required := range schema.Required {
				if required == "query" {
					requiredQuery = true
					break
				}
			}
			if !requiredQuery {
				t.Fatalf("%s required=%v want query", tool.Name, schema.Required)
			}
		}
	}
	for _, name := range []string{"gather_test_context", "gather_docs_context"} {
		if !found[name] {
			t.Fatalf("DefaultTools() missing %s", name)
		}
	}
}

func TestDefaultToolsExposeExpandContextGraph(t *testing.T) {
	t.Parallel()

	for _, tool := range DefaultTools() {
		if tool.Name != "expand_context_graph" {
			continue
		}
		if !tool.ReadOnly {
			t.Fatal("expand_context_graph must be read-only")
		}
		var schema struct {
			Required   []string               `json:"required"`
			Properties map[string]interface{} `json:"properties"`
		}
		if err := json.Unmarshal(tool.Parameters, &schema); err != nil {
			t.Fatalf("schema decode: %v", err)
		}
		for _, key := range []string{"roots", "depth", "direction", "budget"} {
			if _, ok := schema.Properties[key]; !ok {
				t.Fatalf("expand_context_graph schema missing %s: %v", key, schema.Properties)
			}
		}
		for _, required := range schema.Required {
			if required == "roots" {
				return
			}
		}
		t.Fatalf("expand_context_graph required=%v want roots", schema.Required)
	}
	t.Fatal("DefaultTools() missing expand_context_graph")
}
