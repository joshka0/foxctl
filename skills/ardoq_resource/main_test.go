package main

import (
	"strings"
	"testing"
)

func TestBuildWorkspaceRowsSummarizesComponentTypes(t *testing.T) {
	workspaces := []map[string]any{
		{"_id": "w1", "workspaceKey": "APP", "name": "Applications"},
		{"_id": "w2", "workspaceKey": "EMPTY", "name": "Empty"},
	}
	components := []map[string]any{
		{"rootWorkspace": "w1", "type": "Application"},
		{"rootWorkspace": "w1", "type": "Application"},
		{"rootWorkspace": "w1", "type": "Module"},
	}

	rows := buildWorkspaceRows(workspaces, components, false)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if got := intValue(rows[0]["component_count"]); got != 3 {
		t.Fatalf("component_count = %d, want 3", got)
	}
	text := typeCountsText(rows[0]["types"])
	if !strings.Contains(text, "Application:2") || !strings.Contains(text, "Module:1") {
		t.Fatalf("types text = %q", text)
	}
}

func TestReferenceTypeMapUsesTypeField(t *testing.T) {
	got := referenceTypeMap(map[string]any{
		"referenceTypes": []any{
			map[string]any{"type": float64(13), "name": "Business owner of"},
		},
	})
	if got[13] != "Business owner of" {
		t.Fatalf("reference type 13 = %q", got[13])
	}
}

func TestFindPeopleMatchesEmailAndName(t *testing.T) {
	components := []map[string]any{
		{
			"_id":  "p1",
			"type": "Person",
			"name": "Robert Kaev",
			"customFields": map[string]any{
				"user_principal_name": "robert.kaev@foxway.com",
			},
		},
	}

	byEmail := findPeople(components, &ownerLookupReq{Email: "ROBERT.KAEV@FOXWAY.COM"})
	if len(byEmail) != 1 || stringValue(byEmail[0]["_id"]) != "p1" {
		t.Fatalf("byEmail = %#v", byEmail)
	}

	byName := findPeople(components, &ownerLookupReq{Name: "robert kaev"})
	if len(byName) != 1 || stringValue(byName[0]["_id"]) != "p1" {
		t.Fatalf("byName = %#v", byName)
	}
}

func TestOwnerReferenceTypeNamesDefaultAndExpert(t *testing.T) {
	names := ownerReferenceTypeNames(&ownerLookupReq{IncludeExpert: true})
	for _, want := range []string{"Owns", "Technical owner of", "Business owner of", "Product owner of", "Is expert in"} {
		if !names[want] {
			t.Fatalf("missing %q in %#v", want, names)
		}
	}
}

func TestFormatOutputMarkdownUsesRenderedSummary(t *testing.T) {
	out, err := formatOutput(input{Format: "markdown"}, map[string]any{
		"operation":          "owner_lookup",
		"summary":            "Found 1 owned item",
		"item_count":         1,
		"ownership_summary":  "| Relationship | Component |\n| --- | --- |\n| Owns | ArgoCD |\n",
		"reference_type_ids": []int{3},
	})
	if err != nil {
		t.Fatalf("formatOutput() error = %v", err)
	}
	if out["format"] != "markdown" {
		t.Fatalf("format = %v", out["format"])
	}
	if !strings.Contains(stringValue(out["rendered"]), "ArgoCD") {
		t.Fatalf("rendered = %q", out["rendered"])
	}
	if _, ok := out["ownership_summary"]; ok {
		t.Fatalf("formatted output should be slim, got ownership_summary")
	}
}

func TestFormatOutputTextRendersPlainRows(t *testing.T) {
	out, err := formatOutput(input{Format: "text"}, map[string]any{
		"operation":         "inventory",
		"summary":           "1 workspace",
		"component_summary": "| Workspace | Components |\n| --- | ---: |\n| Apps | 3 |\n",
	})
	if err != nil {
		t.Fatalf("formatOutput() error = %v", err)
	}
	rendered := stringValue(out["rendered"])
	if strings.Contains(rendered, "|") {
		t.Fatalf("text output still contains markdown pipes: %q", rendered)
	}
	if !strings.Contains(rendered, "Apps\t3") {
		t.Fatalf("rendered = %q", rendered)
	}
}

func TestFormatOutputRejectsUnknownFormat(t *testing.T) {
	_, err := formatOutput(input{Format: "html"}, map[string]any{"operation": "inventory"})
	if err == nil {
		t.Fatal("formatOutput() error = nil, want error")
	}
}

func TestFormatOutputGraphRendersMermaid(t *testing.T) {
	out, err := formatOutput(input{Format: "graph"}, map[string]any{
		"operation": "owner_lookup",
		"summary":   "Found 1 owned item",
		"owner": map[string]any{
			"name": "Robert Kaev",
		},
		"items": []map[string]any{
			{
				"relationship":   "Technical owner of",
				"component_name": "ArgoCD",
				"workspace_name": "Foxway applications",
			},
		},
	})
	if err != nil {
		t.Fatalf("formatOutput() error = %v", err)
	}
	if out["format"] != "graph" {
		t.Fatalf("format = %v", out["format"])
	}
	rendered := stringValue(out["rendered"])
	for _, want := range []string{"graph LR", "Robert Kaev", "Technical owner of", "ArgoCD"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered missing %q: %s", want, rendered)
		}
	}
}

func TestFormatOutputASCIIRendersTree(t *testing.T) {
	out, err := formatOutput(input{Format: "ascii"}, map[string]any{
		"operation": "owner_lookup",
		"summary":   "Found 1 owned item",
		"owner": map[string]any{
			"name": "Robert Kaev",
		},
		"items": []map[string]any{
			{
				"relationship":   "Technical owner of",
				"component_name": "ArgoCD",
				"component_key":  "HSAC-436",
				"component_type": "Application",
				"workspace_name": "Foxway applications",
			},
		},
	})
	if err != nil {
		t.Fatalf("formatOutput() error = %v", err)
	}
	if out["format"] != "ascii" {
		t.Fatalf("format = %v", out["format"])
	}
	rendered := stringValue(out["rendered"])
	for _, want := range []string{"Robert Kaev", "`-- Technical owner of", "ArgoCD [HSAC-436] (Application)"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered missing %q: %s", want, rendered)
		}
	}
}
