package builtin

import (
	"context"
	"testing"

	"github.com/jkatigb/agentctl/internal/storage/knowledge"
)

func TestListFactoryDroids(t *testing.T) {
	droids, err := ListFactoryDroids()
	if err != nil {
		t.Fatalf("ListFactoryDroids() error = %v", err)
	}

	if len(droids) == 0 {
		t.Error("ListFactoryDroids() returned no droids")
	}

	// Verify expected droids are present
	expected := map[string]bool{
		"factory/droid/orchestrator":       false,
		"factory/droid/backend-architect":  false,
		"factory/droid/frontend-developer": false,
	}

	for _, d := range droids {
		if _, ok := expected[d.Name]; ok {
			expected[d.Name] = true
		}
		verifyAsset(t, d, "factory/droid/")
	}

	for name, found := range expected {
		if !found {
			t.Errorf("expected droid %s not found", name)
		}
	}
}

func TestListCoreAgents(t *testing.T) {
	agents, err := ListCoreAgents()
	if err != nil {
		t.Fatalf("ListCoreAgents() error = %v", err)
	}

	if len(agents) == 0 {
		t.Error("ListCoreAgents() returned no agents")
	}

	// Verify expected agents are present
	expected := map[string]bool{
		"core/agent/code-reviewer":  false,
		"core/agent/debugger":       false,
		"core/agent/refactorer":     false,
		"core/agent/test-automator": false,
	}

	for _, a := range agents {
		if _, ok := expected[a.Name]; ok {
			expected[a.Name] = true
		}
		verifyAsset(t, a, "core/agent/")
	}

	for name, found := range expected {
		if !found {
			t.Errorf("expected agent %s not found", name)
		}
	}
}

func verifyAsset(t *testing.T, a BuiltinAsset, expectedPrefix string) {
	t.Helper()
	if a.Name == "" {
		t.Error("asset has empty Name")
	}
	if !contains(a.Name, expectedPrefix) {
		t.Errorf("asset %s does not have expected prefix %s", a.Name, expectedPrefix)
	}
	if a.Kind != knowledge.KindAgent {
		t.Errorf("asset %s has kind %s, want %s", a.Name, a.Kind, knowledge.KindAgent)
	}
	if a.Description == "" {
		t.Errorf("asset %s has empty Description", a.Name)
	}
	if a.SourcePath == "" {
		t.Errorf("asset %s has empty SourcePath", a.Name)
	}
	if a.Body == "" {
		t.Errorf("asset %s has empty Body", a.Name)
	}
	if len(a.Keywords) == 0 {
		t.Errorf("asset %s has no Keywords", a.Name)
	}
}

func TestParseYAMLFrontmatter(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantName string
		wantDesc string
	}{
		{
			name: "standard frontmatter",
			content: `---
name: test-droid
description: A test droid for testing
model: claude-sonnet
---

Body content here.`,
			wantName: "test-droid",
			wantDesc: "A test droid for testing",
		},
		{
			name:     "no frontmatter",
			content:  `Just some markdown content.`,
			wantName: "",
			wantDesc: "",
		},
		{
			name: "incomplete frontmatter",
			content: `---
name: partial
---

Content.`,
			wantName: "partial",
			wantDesc: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotDesc := parseYAMLFrontmatter(tt.content)
			if gotName != tt.wantName {
				t.Errorf("parseYAMLFrontmatter() name = %q, want %q", gotName, tt.wantName)
			}
			if gotDesc != tt.wantDesc {
				t.Errorf("parseYAMLFrontmatter() desc = %q, want %q", gotDesc, tt.wantDesc)
			}
		})
	}
}

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		desc        string
		wantMinimum int // At least this many keywords
		wantContain []string
	}{
		{
			desc:        "Design RESTful APIs, microservice boundaries, and database schemas.",
			wantMinimum: 3,
			wantContain: []string{"design", "restful", "apis"},
		},
		{
			desc:        "Build Next.js applications with React components",
			wantMinimum: 3,
			wantContain: []string{"build", "next.js", "react"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc[:20], func(t *testing.T) {
			keywords := extractKeywords(tt.desc)
			if len(keywords) < tt.wantMinimum {
				t.Errorf("extractKeywords() got %d keywords, want at least %d", len(keywords), tt.wantMinimum)
			}

			kwSet := make(map[string]bool)
			for _, kw := range keywords {
				kwSet[kw] = true
			}

			for _, want := range tt.wantContain {
				if !kwSet[want] {
					t.Errorf("extractKeywords() missing expected keyword %q, got %v", want, keywords)
				}
			}
		})
	}
}

func TestSeedFactoryKnowledge(t *testing.T) {
	// Create a temporary store
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := knowledge.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("knowledge.Open() error = %v", err)
	}
	defer store.Close()

	// First seed
	count1, err := SeedFactoryKnowledge(ctx, store)
	if err != nil {
		t.Fatalf("SeedFactoryKnowledge() error = %v", err)
	}
	if count1 == 0 {
		t.Error("SeedFactoryKnowledge() seeded 0 items")
	}

	// Verify items are in the store
	items, err := store.ListItems(ctx, knowledge.KindAgent)
	if err != nil {
		t.Fatalf("ListItems() error = %v", err)
	}
	if len(items) != count1 {
		t.Errorf("ListItems() returned %d items, want %d", len(items), count1)
	}

	// Verify a specific item
	item, found, err := store.GetItemByName(ctx, "factory/droid/orchestrator")
	if err != nil {
		t.Fatalf("GetItemByName() error = %v", err)
	}
	if !found {
		t.Fatal("orchestrator droid not found")
	}
	if item.Kind != knowledge.KindAgent {
		t.Errorf("orchestrator kind = %s, want agent", item.Kind)
	}
	if !contains(item.SourcePath, "builtin://") {
		t.Errorf("orchestrator SourcePath = %s, want builtin:// prefix", item.SourcePath)
	}

	// Verify triggers exist
	triggers, err := store.ListTriggers(ctx, item.ID)
	if err != nil {
		t.Fatalf("ListTriggers() error = %v", err)
	}
	if len(triggers) == 0 {
		t.Error("orchestrator has no triggers")
	}

	// Verify documents exist
	docs, err := store.ListDocuments(ctx, item.ID)
	if err != nil {
		t.Fatalf("ListDocuments() error = %v", err)
	}
	if len(docs) != 1 {
		t.Errorf("orchestrator has %d documents, want 1", len(docs))
	}

	// Second seed should be idempotent
	count2, err := SeedFactoryKnowledge(ctx, store)
	if err != nil {
		t.Fatalf("SeedFactoryKnowledge() second run error = %v", err)
	}
	if count2 != count1 {
		t.Errorf("SeedFactoryKnowledge() second run = %d, want %d (idempotent)", count2, count1)
	}

	// Verify no duplicates
	items2, err := store.ListItems(ctx, knowledge.KindAgent)
	if err != nil {
		t.Fatalf("ListItems() error = %v", err)
	}
	if len(items2) != len(items) {
		t.Errorf("ListItems() after second seed = %d, want %d (no duplicates)", len(items2), len(items))
	}
}

func TestSeedBuiltinKnowledge(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	store, err := knowledge.Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("knowledge.Open() error = %v", err)
	}
	defer store.Close()

	// Seed all builtin knowledge
	count, err := SeedBuiltinKnowledge(ctx, store)
	if err != nil {
		t.Fatalf("SeedBuiltinKnowledge() error = %v", err)
	}

	// Should include both Factory droids and core agents
	droids, _ := ListFactoryDroids()
	agents, _ := ListCoreAgents()
	expectedCount := len(droids) + len(agents)

	if count != expectedCount {
		t.Errorf("SeedBuiltinKnowledge() seeded %d items, want %d", count, expectedCount)
	}

	// Verify both types are present
	items, err := store.ListItems(ctx, knowledge.KindAgent)
	if err != nil {
		t.Fatalf("ListItems() error = %v", err)
	}

	factoryCount := 0
	coreCount := 0
	for _, item := range items {
		if contains(item.Name, "factory/droid/") {
			factoryCount++
		}
		if contains(item.Name, "core/agent/") {
			coreCount++
		}
	}

	if factoryCount != len(droids) {
		t.Errorf("Found %d factory droids, want %d", factoryCount, len(droids))
	}
	if coreCount != len(agents) {
		t.Errorf("Found %d core agents, want %d", coreCount, len(agents))
	}

	// Verify a core agent is present
	_, found, err := store.GetItemByName(ctx, "core/agent/code-reviewer")
	if err != nil {
		t.Fatalf("GetItemByName() error = %v", err)
	}
	if !found {
		t.Error("code-reviewer agent not found")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s[:len(substr)] == substr || contains(s[1:], substr))
}
