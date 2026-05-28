package main

import (
	"reflect"
	"testing"
)

const testModule = "github.com/example/foxctl"

func TestTopLevelSkillNameFiltersSubpackages(t *testing.T) {
	tests := []struct {
		name string
		pkg  goListPackage
		want string
		ok   bool
	}{
		{
			name: "top-level main skill",
			pkg:  goListPackage{Name: "main", ImportPath: testModule + "/skills/web_search"},
			want: "web_search",
			ok:   true,
		},
		{
			name: "nested skill helper package",
			pkg:  goListPackage{Name: "helpers", ImportPath: testModule + "/skills/code_refactor_scout/parser"},
		},
		{
			name: "non-skill package",
			pkg:  goListPackage{Name: "main", ImportPath: testModule + "/cmd/foxctl"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := topLevelSkillName(testModule, tt.pkg)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("topLevelSkillName() = %q, %v; want %q, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestClassifySkill(t *testing.T) {
	tests := []struct {
		name           string
		skill          string
		directInternal []string
		directBlockers []string
		classification string
	}{
		{
			name:           "sdk candidate",
			skill:          "web_search",
			directInternal: []string{testModule + "/internal/adapters/skillslib/skillmain"},
			classification: "sdk-candidate",
		},
		{
			name:           "hook stays core",
			skill:          "hooks_bash_guard",
			directInternal: []string{testModule + "/internal/runtime/hooks"},
			classification: "core-hook",
		},
		{
			name:           "intelligence stays core",
			skill:          "code_dag_grep",
			directInternal: []string{testModule + "/internal/intelligence/repoquery"},
			classification: "core-intelligence",
		},
		{
			name:           "provider client moves with pack",
			skill:          "social_reddit_collect",
			directInternal: []string{testModule + "/internal/providers/social"},
			classification: "pack-with-client",
		},
		{
			name:           "unknown internal blocks extraction",
			skill:          "lsp_gopls",
			directInternal: []string{testModule + "/internal/platform/lsp/gopls"},
			directBlockers: []string{testModule + "/internal/platform/lsp/gopls"},
			classification: "blocked-internal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifySkill(testModule, tt.skill, tt.directInternal, tt.directBlockers)
			if got != tt.classification {
				t.Fatalf("classifySkill() = %q, want %q", got, tt.classification)
			}
		})
	}
}

func TestBuildReportsSeparatesDirectAndTransitiveCoupling(t *testing.T) {
	pkgs := []goListPackage{
		{
			Name:       "main",
			ImportPath: testModule + "/skills/web_search",
			Imports: []string{
				testModule + "/internal/adapters/skillslib/skillmain",
				testModule + "/internal/adapters/skillslib/skillout",
			},
			Deps: []string{
				testModule + "/internal/adapters/skillslib/skillmain",
				testModule + "/internal/storage/memory",
				testModule + "/internal/intelligence/turbovec",
			},
		},
	}

	reports := buildReports(testModule, pkgs)
	if len(reports) != 1 {
		t.Fatalf("reports len = %d, want 1", len(reports))
	}
	got := reports[0]
	if got.Classification != "sdk-candidate" {
		t.Fatalf("classification = %q, want sdk-candidate", got.Classification)
	}
	if got.TransitiveInternal != 3 {
		t.Fatalf("transitive internal = %d, want 3", got.TransitiveInternal)
	}
	if got.TransitiveIntelligence != 1 {
		t.Fatalf("transitive intelligence = %d, want 1", got.TransitiveIntelligence)
	}
	if !reflect.DeepEqual(got.DirectBlockers, []string{}) {
		t.Fatalf("direct blockers = %v, want none", got.DirectBlockers)
	}
}
