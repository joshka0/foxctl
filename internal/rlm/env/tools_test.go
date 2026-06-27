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

func TestDefaultToolsExposePlanContextQuery(t *testing.T) {
	t.Parallel()

	for _, tool := range DefaultTools() {
		if tool.Name != "plan_context_query" {
			continue
		}
		if !tool.ReadOnly {
			t.Fatal("plan_context_query must be read-only")
		}
		var schema struct {
			Required   []string               `json:"required"`
			Properties map[string]interface{} `json:"properties"`
		}
		if err := json.Unmarshal(tool.Parameters, &schema); err != nil {
			t.Fatalf("schema decode: %v", err)
		}
		for _, key := range []string{"question", "goal", "lanes", "limit"} {
			if _, ok := schema.Properties[key]; !ok {
				t.Fatalf("plan_context_query schema missing %s: %v", key, schema.Properties)
			}
		}
		for _, required := range schema.Required {
			if required == "question" {
				return
			}
		}
		t.Fatalf("plan_context_query required=%v want question", schema.Required)
	}
	t.Fatal("DefaultTools() missing plan_context_query")
}

func TestDefaultToolsExposeScopedMemoryFieldsOnCorrectTools(t *testing.T) {
	t.Parallel()

	props := map[string]map[string]interface{}{}
	for _, tool := range DefaultTools() {
		var schema struct {
			Properties map[string]interface{} `json:"properties"`
		}
		if err := json.Unmarshal(tool.Parameters, &schema); err != nil {
			t.Fatalf("%s schema decode: %v", tool.Name, err)
		}
		props[tool.Name] = schema.Properties
	}

	for _, name := range []string{"retrieve_memory", "retrieve_mixed"} {
		for _, key := range []string{"task_id", "session_id"} {
			if _, ok := props[name][key]; !ok {
				t.Fatalf("%s schema missing %s: %v", name, key, props[name])
			}
		}
		if _, ok := props[name]["memory_statuses"]; ok {
			t.Fatalf("%s schema exposes raw lifecycle statuses: %v", name, props[name])
		}
	}
	if _, ok := props["retrieve_task"]["task_id"]; !ok {
		t.Fatalf("retrieve_task schema missing task_id: %v", props["retrieve_task"])
	}
	for _, name := range []string{"retrieve_code", "retrieve_context"} {
		for _, key := range []string{"task_id", "session_id", "memory_statuses"} {
			if _, ok := props[name][key]; ok {
				t.Fatalf("%s schema unexpectedly exposes %s: %v", name, key, props[name])
			}
		}
	}
}

func TestDefaultToolsExposeSpecializedGatherContextSurfaces(t *testing.T) {
	t.Parallel()

	found := map[string]bool{}
	for _, tool := range DefaultTools() {
		switch tool.Name {
		case "gather_memory_context", "gather_test_context", "gather_docs_context":
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
	for _, name := range []string{"gather_memory_context", "gather_test_context", "gather_docs_context"} {
		if !found[name] {
			t.Fatalf("DefaultTools() missing %s", name)
		}
	}
}

func TestDefaultToolsExposeGatherMemoryContext(t *testing.T) {
	t.Parallel()

	for _, tool := range DefaultTools() {
		if tool.Name != "gather_memory_context" {
			continue
		}
		if !tool.ReadOnly {
			t.Fatal("gather_memory_context must be read-only")
		}
		var schema struct {
			Required   []string               `json:"required"`
			Properties map[string]interface{} `json:"properties"`
		}
		if err := json.Unmarshal(tool.Parameters, &schema); err != nil {
			t.Fatalf("schema decode: %v", err)
		}
		for _, key := range []string{"query", "required_evidence", "coverage_requirements", "limit", "response_mode"} {
			if _, ok := schema.Properties[key]; !ok {
				t.Fatalf("gather_memory_context schema missing %s: %v", key, schema.Properties)
			}
		}
		for _, required := range schema.Required {
			if required == "query" {
				return
			}
		}
		t.Fatalf("gather_memory_context required=%v want query", schema.Required)
	}
	t.Fatal("DefaultTools() missing gather_memory_context")
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

func TestDefaultToolsExposeAggregateEvidenceRefs(t *testing.T) {
	t.Parallel()

	for _, tool := range DefaultTools() {
		if tool.Name != "aggregate_evidence_refs" {
			continue
		}
		if !tool.ReadOnly {
			t.Fatal("aggregate_evidence_refs must be read-only")
		}
		var schema struct {
			Required   []string               `json:"required"`
			Properties map[string]interface{} `json:"properties"`
		}
		if err := json.Unmarshal(tool.Parameters, &schema); err != nil {
			t.Fatalf("schema decode: %v", err)
		}
		for _, key := range []string{"query", "refs", "required_evidence", "coverage_requirements", "max_refs", "max_text_chars", "max_tokens_per_ref"} {
			if _, ok := schema.Properties[key]; !ok {
				t.Fatalf("aggregate_evidence_refs schema missing %s: %v", key, schema.Properties)
			}
		}
		for _, required := range []string{"query", "refs"} {
			if !containsString(schema.Required, required) {
				t.Fatalf("aggregate_evidence_refs required=%v missing %s", schema.Required, required)
			}
		}
		return
	}
	t.Fatal("DefaultTools() missing aggregate_evidence_refs")
}

func TestDefaultToolsExposeEvidenceLedger(t *testing.T) {
	t.Parallel()

	for _, tool := range DefaultTools() {
		if tool.Name != "evidence_ledger" {
			continue
		}
		if !tool.ReadOnly {
			t.Fatal("evidence_ledger must be read-only")
		}
		var schema struct {
			Required   []string               `json:"required"`
			Properties map[string]interface{} `json:"properties"`
		}
		if err := json.Unmarshal(tool.Parameters, &schema); err != nil {
			t.Fatalf("schema decode: %v", err)
		}
		for _, key := range []string{"query", "refs", "required_evidence", "coverage_requirements", "max_refs", "max_text_chars", "max_tokens_per_ref"} {
			if _, ok := schema.Properties[key]; !ok {
				t.Fatalf("evidence_ledger schema missing %s: %v", key, schema.Properties)
			}
		}
		for _, required := range []string{"query", "refs"} {
			if !containsString(schema.Required, required) {
				t.Fatalf("evidence_ledger required=%v missing %s", schema.Required, required)
			}
		}
		return
	}
	t.Fatal("DefaultTools() missing evidence_ledger")
}
