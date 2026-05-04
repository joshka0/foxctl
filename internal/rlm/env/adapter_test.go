package env

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/domain/skill"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/platform/config"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/rlm"
	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/joshka0/foxctl/internal/storage/cas"
	ctxengstore "github.com/joshka0/foxctl/internal/storage/contextengine"
	"github.com/joshka0/foxctl/internal/storage/obsidianindex"
	"github.com/joshka0/foxctl/internal/storage/sessions"
	taskstore "github.com/joshka0/foxctl/internal/storage/tasks"
	"github.com/joshka0/foxctl/internal/storage/trajectory"
)

func writeFakeExecSkill(t *testing.T, workspaceRoot, skillName, artifactBody string) string {
	t.Helper()

	dir := filepath.Join(workspaceRoot, "skills", skill.NormalizeSkillName(skillName))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `apiVersion: foxctl/v1
kind: Skill
metadata:
  name: ` + skillName + `
  version: 0.1.0
  description: fake skill
distribution:
  type: exec
  exec:
    entry: ` + skill.NormalizeSkillName(skillName) + `
io:
  format: JSON
signature:
  command: ` + skillName + `
  parameters: []
  returns: []
`
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(dir, skill.NormalizeSkillName(skillName))
	if err := os.WriteFile(artifact, []byte(artifactBody), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestMergeCodeSearchHitsAnnotatesRoleBuckets(t *testing.T) {
	hits := mergeCodeSearchHitsWithOptions(4, codeSearchRequestOptions{}, nil,
		[]rankedCodeSearchHit{{
			Hit: contextengine.CodeSearchHit{
				Path:  "backend/src/index.ts",
				Score: 0.8,
				Metadata: map[string]any{
					"candidate_role": "import_reference",
					"source":         "local_import_mount_closure",
				},
			},
			Priority: 10,
		}},
		[]rankedCodeSearchHit{{
			Hit: contextengine.CodeSearchHit{
				Path:  "backend/src/index.ts",
				Score: 0.7,
				Metadata: map[string]any{
					"candidate_role": "direct_dispatch_file",
					"evidence_class": "route_action",
				},
			},
			Priority: 9,
		}},
	)
	if len(hits) != 1 {
		t.Fatalf("expected one merged hit, got %d", len(hits))
	}
	buckets := metadataStringSliceEnv(hits[0].Metadata, "role_buckets")
	for _, want := range []string{"mount", "route_action"} {
		if !stringSliceHas(buckets, want) {
			t.Fatalf("expected role bucket %q in %v", want, buckets)
		}
	}
}

func TestExpandContextGraphResolvesRootsAndEdges(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "app.go"), "package app\n\nimport \"example.com/project/dep\"\n\nfunc Run() { dep.Helper() }\n")
	writeTestFile(t, filepath.Join(workspace, "dep.go"), "package dep\n\nfunc Helper() {}\n")

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatalf("repoindex.Open: %v", err)
	}
	defer store.Close()
	repoKey := store.RepoKey()
	appNode := repoindex.Node{
		ID:   repoindex.FileID(repoKey, "example.com/project", "app.go"),
		Kind: repoindex.NodeFile,
		Pkg:  "example.com/project",
		File: "app.go",
	}
	depNode := repoindex.Node{
		ID:   repoindex.FileID(repoKey, "example.com/project", "dep.go"),
		Kind: repoindex.NodeFile,
		Pkg:  "example.com/project",
		File: "dep.go",
	}
	if err := store.ReplaceAll(ctx, []repoindex.Node{appNode, depNode}, []repoindex.Edge{{
		Src:    appNode.ID,
		Dst:    depNode.ID,
		Type:   repoindex.EdgeImports,
		Weight: 1,
	}}); err != nil {
		t.Fatalf("ReplaceAll: %v", err)
	}
	if err := store.SetMeta(ctx, repoindex.IndexMeta{RepoRoot: workspace}); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}

	adapter := NewReadOnlyAdapter(config.Config{Storage: config.StorageSettings{Root: storageRoot}}, workspace, "", nil, rlm.Environment{
		Tools: []rlm.Tool{{Name: "expand_context_graph", ReadOnly: true}},
	})
	out, err := adapter.Execute(ctx, "expand_context_graph", mustJSON(map[string]any{
		"depth": 1,
		"roots": []string{"path:app.go"},
		"budget": map[string]any{
			"max_nodes":    10,
			"max_edges":    10,
			"per_node_cap": 5,
		},
	}))
	if err != nil {
		t.Fatalf("expand_context_graph: %v", err)
	}
	nodes := out["nodes"].([]any)
	if len(nodes) != 2 {
		t.Fatalf("nodes=%v want two graph nodes", out["nodes"])
	}
	edges := out["edges"].([]any)
	if len(edges) != 1 {
		t.Fatalf("edges=%v want one graph edge", out["edges"])
	}
	confidence := out["confidence"].(map[string]any)
	if confidence["trusted_for_proceed"] != true {
		t.Fatalf("confidence=%v want trusted_for_proceed", confidence)
	}
}

func TestExpandContextGraphAddsRequestedLocalFallbacks(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "src", "service.ts"), "export function service() { return 1 }\n")
	writeTestFile(t, filepath.Join(workspace, "src", "service.test.ts"), "import { service } from './service'\n")
	writeTestFile(t, filepath.Join(workspace, "src", "service.config.json"), "{}\n")
	writeTestFile(t, filepath.Join(workspace, "src", "consumer.ts"), "import { service } from './service'\n")

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatalf("repoindex.Open: %v", err)
	}
	defer store.Close()
	repoKey := store.RepoKey()
	serviceNode := repoindex.Node{
		ID:   repoindex.FileID(repoKey, "app", "src/service.ts"),
		Kind: repoindex.NodeFile,
		Pkg:  "app",
		File: "src/service.ts",
	}
	if err := store.ReplaceAll(ctx, []repoindex.Node{serviceNode}, nil); err != nil {
		t.Fatalf("ReplaceAll: %v", err)
	}
	if err := store.SetMeta(ctx, repoindex.IndexMeta{RepoRoot: workspace}); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}

	adapter := NewReadOnlyAdapter(config.Config{Storage: config.StorageSettings{Root: storageRoot}}, workspace, "", nil, rlm.Environment{
		Tools: []rlm.Tool{{Name: "expand_context_graph", ReadOnly: true}},
	})
	out, err := adapter.Execute(ctx, "expand_context_graph", mustJSON(map[string]any{
		"roots":            []string{"path:src/service.ts"},
		"include_tests":    true,
		"include_adjacent": true,
		"budget": map[string]any{
			"max_nodes":       20,
			"max_edges":       20,
			"per_node_cap":    10,
			"max_local_files": 20,
			"max_local_bytes": 100000,
		},
	}))
	if err != nil {
		t.Fatalf("expand_context_graph: %v", err)
	}
	if !graphOutputHasPath(out, "src/service.test.ts") {
		t.Fatalf("nodes=%v want test companion", out["nodes"])
	}
	if !graphOutputHasPath(out, "src/service.config.json") {
		t.Fatalf("nodes=%v want adjacent config", out["nodes"])
	}
	if !graphOutputHasPath(out, "src/consumer.ts") {
		t.Fatalf("nodes=%v want reverse-reference fallback", out["nodes"])
	}
	if !graphOutputHasEdgeType(out, "tests") {
		t.Fatalf("edges=%v want tests edge", out["edges"])
	}
}

func TestMergeCodeSearchHitsSuppressesExcludedPaths(t *testing.T) {
	hits := mergeCodeSearchHitsWithOptions(4, codeSearchRequestOptions{
		ExcludedPaths: []string{"worktrees/", "client/Scripts/Autogen/"},
	}, nil,
		[]rankedCodeSearchHit{{
			Hit: contextengine.CodeSearchHit{
				Path:  "worktrees/test-integrations/apps/mobile/src/services/powersync.ts",
				Score: 0.99,
			},
			Priority: 20,
		}},
		[]rankedCodeSearchHit{{
			Hit: contextengine.CodeSearchHit{
				Path:  "client/Scripts/Autogen/Tables/Player.g.cs",
				Score: 0.98,
			},
			Priority: 20,
		}},
		[]rankedCodeSearchHit{{
			Hit: contextengine.CodeSearchHit{
				Path:  "apps/mobile/src/services/powersync.ts",
				Score: 0.7,
			},
			Priority: 10,
		}},
	)
	if len(hits) != 1 {
		t.Fatalf("expected one unsuppressed hit, got %d: %#v", len(hits), hits)
	}
	if hits[0].Path != "apps/mobile/src/services/powersync.ts" {
		t.Fatalf("unexpected remaining path %q", hits[0].Path)
	}
}

func TestMergeCodeSearchHitsReservesCoverageAdmissionSlots(t *testing.T) {
	hits := mergeCodeSearchHitsWithOptions(3, normalizeCodeSearchRequestOptions(codeSearchRequestOptions{
		RequiredEvidence: []string{"controller policy"},
	}), nil,
		[]rankedCodeSearchHit{{Priority: 99, Hit: contextengine.CodeSearchHit{Path: "client/Scripts/Gameplay/PlayerController.cs", Symbol: "PlayerController", Score: 0.99}}},
		[]rankedCodeSearchHit{{Priority: 98, Hit: contextengine.CodeSearchHit{Path: "server/src/tests.rs", Symbol: "controller_tests", Score: 0.98}}},
		[]rankedCodeSearchHit{{Priority: 10, Hit: contextengine.CodeSearchHit{
			Path:   "client/Scripts/Core/ControllerPolicy.cs",
			Symbol: "ControllerPolicy",
			Score:  0.50,
			Metadata: map[string]any{
				"candidate_role": "symbol_definition",
			},
		}}},
	)
	got := extractCodeSearchHitPaths(hits)
	if !containsString(got, "client/Scripts/Core/ControllerPolicy.cs") {
		t.Fatalf("paths=%v missing coverage admission path", got)
	}
}

func TestReadOnlyAdapterModuleEntrypointClosureFindsGenericEntryFiles(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "server", "src", "lib.rs"), "pub mod tables;\npub mod protocol;\n")
	writeTestFile(t, filepath.Join(workspace, "server", "src", "tables.rs"), "pub struct Player {}\n")
	writeTestFile(t, filepath.Join(workspace, "server", "src", "protocol.rs"), "pub struct Message {}\n")
	writeTestFile(t, filepath.Join(workspace, "apps", "api", "lib", "app_web", "router.ex"), "defmodule AppWeb.Router do\nend\n")
	writeTestFile(t, filepath.Join(workspace, "apps", "api", "lib", "app_web", "controllers", "page_controller.ex"), "defmodule AppWeb.PageController do\nend\n")
	writeTestFile(t, filepath.Join(workspace, "web", "src", "router.ts"), "export const router = true\n")
	writeTestFile(t, filepath.Join(workspace, "web", "src", "features", "billing", "index.ts"), "export * from './service'\n")
	writeTestFile(t, filepath.Join(workspace, "web", "src", "features", "billing", "service.ts"), "export const service = true\n")
	writeTestFile(t, filepath.Join(workspace, "api", "app", "main.py"), "from .dependencies import get_db\n")
	writeTestFile(t, filepath.Join(workspace, "api", "app", "dependencies.py"), "def get_db(): pass\n")
	writeTestFile(t, filepath.Join(workspace, "api", "app", "users.py"), "def list_users(): pass\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{})
	hits := adapter.localModuleEntrypointClosureHits([]contextengine.CodeSearchHit{
		{Path: "server/src/lib.rs", Score: 0.2},
		{Path: "server/src/tables.rs", Score: 0.9},
		{Path: "apps/api/lib/app_web/controllers/page_controller.ex", Score: 0.9},
		{Path: "web/src/features/billing/service.ts", Score: 0.9},
		{Path: "api/app/users.py", Score: 0.9},
	}, 8, codeSearchRequestOptions{})
	got := codeSearchHitPaths(hits)
	for _, want := range []string{
		"server/src/lib.rs",
		"apps/api/lib/app_web/router.ex",
		"web/src/router.ts",
		"web/src/features/billing/index.ts",
		"api/app/main.py",
		"api/app/dependencies.py",
	} {
		if !containsString(got, want) {
			t.Fatalf("paths=%v missing %s", got, want)
		}
	}
}

func TestMergeCodeSearchHitsKeepsModuleEntrypointPromotion(t *testing.T) {
	options := normalizeCodeSearchRequestOptions(codeSearchRequestOptions{
		RequiredEvidence: []string{"server module", "tables", "protocol"},
	})
	hits := mergeCodeSearchHitsWithOptions(32, options,
		[]contextengine.CodeSearchHit{
			{Path: "server/src/lib.rs", Score: 0.1, Language: "rust"},
			{Path: "server/src/tables.rs", Score: 0.9, Language: "rust"},
			{Path: "server/src/protocol.rs", Score: 0.9, Language: "rust"},
		},
		[]rankedCodeSearchHit{{
			Priority: 84,
			Hit: contextengine.CodeSearchHit{
				Path:     "server/src/lib.rs",
				Score:    0.92,
				Language: "rust",
				Metadata: map[string]any{
					"candidate_role": "module_entrypoint",
					"source":         "local_module_entrypoint_closure",
					"source_profile": "repo_code",
				},
			},
		}},
	)
	got := extractCodeSearchHitPaths(hits)
	if !containsString(got, "server/src/lib.rs") {
		t.Fatalf("paths=%v missing promoted module entrypoint", got)
	}
}

func TestAppendMissingRankedCodeSearchHitsPreservesEntrypointWithinBudget(t *testing.T) {
	options := normalizeCodeSearchRequestOptions(codeSearchRequestOptions{})
	hits := []contextengine.CodeSearchHit{
		{Path: "server/src/tables.rs", Symbol: "Table", Language: "rust"},
		{Path: "server/src/tables.rs", Symbol: "Player", Language: "rust"},
	}
	hits = appendMissingRankedCodeSearchHits(hits, 2, options, []rankedCodeSearchHit{{
		Priority: 84,
		Hit: contextengine.CodeSearchHit{
			Path:     "server/src/lib.rs",
			Language: "rust",
			Metadata: map[string]any{
				"candidate_role": "module_entrypoint",
			},
		},
	}})
	got := extractCodeSearchHitPaths(hits)
	if !containsString(got, "server/src/lib.rs") {
		t.Fatalf("paths=%v missing appended module entrypoint", got)
	}
}

func TestReadOnlyAdapterImportMountClosureFindsGenericReferences(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not installed")
	}
	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "web", "src", "features", "billing", "service.ts"), "export const chargeAccount = true\n")
	writeTestFile(t, filepath.Join(workspace, "web", "src", "features", "billing", "index.ts"), "export * from './service'\n")
	writeTestFile(t, filepath.Join(workspace, "web", "src", "router.ts"), "import { chargeAccount } from '@/features/billing/service'\n")
	writeTestFile(t, filepath.Join(workspace, "web", "src", "features", "billing", "service.test.ts"), "import { chargeAccount } from './service'\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{})
	hits := adapter.localImportMountClosureHits(context.Background(), []contextengine.CodeSearchHit{
		{Path: "web/src/features/billing/service.ts", Score: 0.9},
	}, 8, codeSearchRequestOptions{})
	got := codeSearchHitPaths(hits)
	for _, want := range []string{
		"web/src/features/billing/index.ts",
		"web/src/router.ts",
	} {
		if !containsString(got, want) {
			t.Fatalf("paths=%v missing %s", got, want)
		}
	}
	if containsString(got, "web/src/features/billing/service.test.ts") {
		t.Fatalf("paths=%v should omit tests by default", got)
	}
}

func TestReadOnlyAdapterImportMountClosureRequiresGeneratedProtocolRole(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not installed")
	}
	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "src", "proto", "account.pb.go"), "package proto\n\ntype Account struct{}\n")
	writeTestFile(t, filepath.Join(workspace, "src", "handler.go"), "package app\n\nconst binding = \"src/proto/account.pb\"\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{})
	withoutRole := adapter.localImportMountClosureHits(context.Background(), []contextengine.CodeSearchHit{
		{Path: "src/proto/account.pb.go", Score: 0.9},
	}, 8, codeSearchRequestOptions{})
	if len(withoutRole) != 0 {
		t.Fatalf("hits=%v want no generated protocol closure without explicit role", codeSearchHitPaths(withoutRole))
	}

	withRole := adapter.localImportMountClosureHits(context.Background(), []contextengine.CodeSearchHit{
		{
			Path:  "src/proto/account.pb.go",
			Score: 0.9,
			Metadata: map[string]any{
				"candidate_role": "generated_protocol_binding",
			},
		},
	}, 8, codeSearchRequestOptions{})
	got := codeSearchHitPaths(withRole)
	if !containsString(got, "src/handler.go") {
		t.Fatalf("paths=%v missing generated protocol consumer", got)
	}
}

func TestReadOnlyAdapterRouteFamilyClosureFindsGenericRouteFiles(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "apps", "mobile", "app", "_layout.tsx"), "export default function RootLayout() { return null }\n")
	writeTestFile(t, filepath.Join(workspace, "apps", "mobile", "app", "connection", "_layout.tsx"), "export default function ConnectionLayout() { return null }\n")
	writeTestFile(t, filepath.Join(workspace, "apps", "mobile", "app", "connection", "index.tsx"), "export default function ConnectionIndex() { return null }\n")
	writeTestFile(t, filepath.Join(workspace, "apps", "mobile", "app", "connection", "[connectionId].tsx"), "export default function ConnectionDetail() { return null }\n")
	writeTestFile(t, filepath.Join(workspace, "apps", "mobile", "src", "connection.ts"), "export const connection = true\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{})
	hits := adapter.localRouteFamilyClosureHits([]contextengine.CodeSearchHit{
		{Path: "apps/mobile/app/connection/[connectionId].tsx", Score: 0.9},
	}, 8, codeSearchRequestOptions{})
	got := codeSearchHitPaths(hits)
	for _, want := range []string{
		"apps/mobile/app/_layout.tsx",
		"apps/mobile/app/connection/_layout.tsx",
		"apps/mobile/app/connection/index.tsx",
	} {
		if !containsString(got, want) {
			t.Fatalf("paths=%v missing %s", got, want)
		}
	}
	for _, hit := range hits {
		if hit.Hit.Path == "apps/mobile/app/connection/_layout.tsx" {
			if hit.Hit.Metadata["candidate_role"] != "route_layout" || hit.Hit.Metadata["source"] != "local_route_family_closure" {
				t.Fatalf("metadata=%v want route family metadata", hit.Hit.Metadata)
			}
			return
		}
	}
	t.Fatalf("hits=%v missing connection layout hit", got)
}

func TestReadOnlyAdapterRouteFamilyClosureFindsPagesRouteRoots(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "web", "pages", "_app.tsx"), "export default function App() { return null }\n")
	writeTestFile(t, filepath.Join(workspace, "web", "pages", "index.tsx"), "export default function Home() { return null }\n")
	writeTestFile(t, filepath.Join(workspace, "web", "pages", "posts", "[id].tsx"), "export default function Post() { return null }\n")
	writeTestFile(t, filepath.Join(workspace, "web", "src", "posts.tsx"), "export const Posts = null\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{})
	hits := adapter.localRouteFamilyClosureHits([]contextengine.CodeSearchHit{
		{Path: "web/pages/posts/[id].tsx", Score: 0.9},
	}, 8, codeSearchRequestOptions{})
	got := codeSearchHitPaths(hits)
	for _, want := range []string{
		"web/pages/_app.tsx",
		"web/pages/index.tsx",
	} {
		if !containsString(got, want) {
			t.Fatalf("paths=%v missing %s", got, want)
		}
	}
	if containsString(got, "web/src/posts.tsx") {
		t.Fatalf("paths=%v should not include unrelated src file", got)
	}
}

func TestReadOnlyAdapterRouteFamilyClosureRespectsExcludedPaths(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "web", "pages", "_app.tsx"), "export default function App() { return null }\n")
	writeTestFile(t, filepath.Join(workspace, "web", "pages", "index.tsx"), "export default function Home() { return null }\n")
	writeTestFile(t, filepath.Join(workspace, "web", "pages", "settings", "index.tsx"), "export default function Settings() { return null }\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{})
	hits := adapter.localRouteFamilyClosureHits([]contextengine.CodeSearchHit{
		{Path: "web/pages/settings/index.tsx", Score: 0.9},
	}, 8, codeSearchRequestOptions{ExcludedPaths: []string{"web/pages"}})
	if len(hits) != 0 {
		t.Fatalf("hits=%v want no excluded route-family hits", codeSearchHitPaths(hits))
	}
}

func TestReadOnlyAdapterRouteFamilyClosureIgnoresComponentOnlyAppDirs(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "web", "src", "app", "components", "Button.tsx"), "export function Button() { return null }\n")
	writeTestFile(t, filepath.Join(workspace, "web", "src", "app", "components", "index.tsx"), "export { Button } from './Button'\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{})
	hits := adapter.localRouteFamilyClosureHits([]contextengine.CodeSearchHit{
		{Path: "web/src/app/components/Button.tsx", Score: 0.9},
		{Path: "web/src/app/components/index.tsx", Score: 0.8},
	}, 8, codeSearchRequestOptions{})
	if len(hits) != 0 {
		t.Fatalf("hits=%v want no route-family hits for component-only app dir", codeSearchHitPaths(hits))
	}
}

func TestCodeSearchProviderTelemetryIncludesCappedPaths(t *testing.T) {
	var telemetry []codeSearchProviderTelemetryItem
	hits := []rankedCodeSearchHit{
		{Hit: contextengine.CodeSearchHit{Path: "b/file.ts"}},
		{Hit: contextengine.CodeSearchHit{Path: "a/file.ts"}},
		{Hit: contextengine.CodeSearchHit{Path: "b/file.ts"}},
	}
	recordCodeSearchProviderTelemetry(&telemetry, "local", time.Millisecond, len(hits), pathsFromRankedCodeSearchHits(hits), nil)
	if len(telemetry) != 1 {
		t.Fatalf("expected one telemetry item, got %d", len(telemetry))
	}
	want := []string{"a/file.ts", "b/file.ts"}
	if !reflect.DeepEqual(telemetry[0].Paths, want) {
		t.Fatalf("unexpected telemetry paths: got %v want %v", telemetry[0].Paths, want)
	}
}

func TestReadOnlyAdapterBasicTools(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()

	if err := os.WriteFile(filepath.Join(workspace, "main.tf"), []byte("resource \"aws_s3_bucket\" \"app\" {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	repoStore, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer repoStore.Close()
	repoKey := repoStore.RepoKey()
	if err := repoStore.ReplaceAll(ctx, []repoindex.Node{
		{
			ID:      repoindex.NamespacedID(repoKey, "res:tf:root:resource:aws_s3_bucket.app"),
			Kind:    repoindex.NodeConcept,
			Pkg:     "tf:root",
			File:    "main.tf",
			Name:    "resource aws_s3_bucket.app",
			Summary: "Terraform resource aws_s3_bucket app.",
		},
		{
			ID:      repoindex.FileID(repoKey, "tf:root", "main.tf"),
			Kind:    repoindex.NodeFile,
			Pkg:     "tf:root",
			File:    "main.tf",
			Name:    "main.tf",
			Summary: "Terraform file main.tf.",
		},
	}, nil); err != nil {
		t.Fatal(err)
	}

	var cfg config.Config
	cfg.Storage.Root = storageRoot
	adapter := NewReadOnlyAdapter(cfg, workspace, "", nil, rlm.Environment{
		TopOfMind: map[string]any{
			"objective": "trace terraform",
		},
	})

	top, err := adapter.ExecuteInternal(ctx, "get_top_of_mind", nil)
	if err != nil {
		t.Fatalf("get_top_of_mind: %v", err)
	}
	if top["top_of_mind"] == nil {
		t.Fatalf("top_of_mind=%v", top)
	}

	repoResult, err := adapter.ExecuteInternal(ctx, "search_repo", mustJSON(map[string]any{
		"query": "terraform bucket",
		"limit": 3,
	}))
	if err != nil {
		t.Fatalf("search_repo: %v", err)
	}
	rawResults, ok := repoResult["results"].([]map[string]any)
	if ok {
		_ = rawResults
	}
	raw, _ := json.Marshal(repoResult["results"])
	var results []map[string]any
	if err := json.Unmarshal(raw, &results); err != nil {
		t.Fatalf("decode repo results: %v", err)
	}
	if len(results) == 0 || results[0]["path"] != "main.tf" {
		t.Fatalf("repo results=%v", results)
	}

	fileResult, err := adapter.ExecuteInternal(ctx, "load_file", mustJSON(map[string]any{
		"path": "main.tf",
	}))
	if err != nil {
		t.Fatalf("load_file: %v", err)
	}
	if fileResult["content"] == "" {
		t.Fatalf("file result=%v", fileResult)
	}
}

func TestReadOnlyAdapterLoadEvidenceRefLoadsTaskAndBoundsPath(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	workspaceID := ws.ID(workspace)

	if err := os.WriteFile(filepath.Join(workspace, "long.txt"), []byte(strings.Repeat("abcdef", 40)), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := taskstore.Open(ctx, storageRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	_, err = store.Add(ctx, taskstore.Task{
		ID:          "task-1",
		WorkspaceID: workspaceID,
		Title:       "Wire evidence loading",
		Status:      taskstore.StatusInProgress,
		ScopePath:   "internal/rlm/env",
		CreatedAt:   time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{})
	adapter.SetTaskStore(store)

	out, err := adapter.ExecuteInternal(ctx, "load_evidence_ref", mustJSON(map[string]any{
		"ref":        "path:long.txt",
		"max_tokens": 5,
	}))
	if err != nil {
		t.Fatalf("load path: %v", err)
	}
	content, _ := out["content"].(string)
	if len(content) > 20 {
		t.Fatalf("content length=%d want bounded; out=%v", len(content), out)
	}
	if out["truncated"] != true {
		t.Fatalf("expected truncated=true: %v", out)
	}

	out, err = adapter.ExecuteInternal(ctx, "load_evidence_ref", mustJSON(map[string]any{
		"ref": "task:task-1",
	}))
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	if out["loaded"] != true {
		t.Fatalf("loaded=%v out=%v", out["loaded"], out)
	}
	if out["task_context"] == nil {
		t.Fatalf("missing task_context: %v", out)
	}
}

func TestReadOnlyAdapterLoadEvidenceRefLoadsSymbolAndContextEvent(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	workspaceID := ws.ID(workspace)

	repoFile := filepath.Join(workspace, "internal", "rlm", "env", "tool_exec.go")
	if err := os.MkdirAll(filepath.Dir(repoFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repoFile, []byte("package env\n\nfunc gatherContext() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repoStore, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	repoKey := repoStore.RepoKey()
	err = repoStore.ReplaceAll(ctx, []repoindex.Node{{
		ID:        repoindex.SymbolID(repoKey, "go:internal/rlm/env", "func:gatherContext"),
		Kind:      repoindex.NodeSymbol,
		Pkg:       "go:internal/rlm/env",
		File:      "internal/rlm/env/tool_exec.go",
		Name:      "gatherContext",
		Summary:   "loads gather_context arguments",
		SpanStart: 3,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := repoStore.Close(); err != nil {
		t.Fatal(err)
	}

	ceStore, err := ctxengstore.Open(ctx, storageRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ceStore.Close() })
	_, err = ceStore.AppendEvent(ctx, contextengine.ContextEvent{
		ID:          "evt-1",
		WorkspaceID: workspaceID,
		Kind:        contextengine.EventKindRetrievalExecuted,
		Source:      "test",
		Data:        map[string]any{"query": "gather_context"},
		CreatedAt:   time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{}
	cfg.Storage.Root = storageRoot
	adapter := NewReadOnlyAdapter(cfg, workspace, "", nil, rlm.Environment{})
	adapter.SetContextEngineStore(ceStore)

	out, err := adapter.ExecuteInternal(ctx, "load_evidence_ref", mustJSON(map[string]any{
		"ref": "symbol:gatherContext",
	}))
	if err != nil {
		t.Fatalf("load symbol: %v", err)
	}
	if out["loaded"] != true {
		t.Fatalf("symbol loaded=%v out=%v", out["loaded"], out)
	}
	rawAnchors, _ := json.Marshal(out["anchors"])
	var anchors []map[string]any
	if err := json.Unmarshal(rawAnchors, &anchors); err != nil {
		t.Fatalf("decode anchors: %v", err)
	}
	if len(anchors) == 0 || anchors[0]["path"] != "internal/rlm/env/tool_exec.go" {
		t.Fatalf("anchors=%v", anchors)
	}

	out, err = adapter.ExecuteInternal(ctx, "load_evidence_ref", mustJSON(map[string]any{
		"ref": "event:evt-1",
	}))
	if err != nil {
		t.Fatalf("load event: %v", err)
	}
	if out["loaded"] != true {
		t.Fatalf("event loaded=%v out=%v", out["loaded"], out)
	}
}

func TestReadOnlyAdapterGatherContextIncludesSessionRecall(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	workspaceID := ws.ID(workspace)
	sessionStore, err := sessions.Open(ctx, storageRoot)
	if err != nil {
		t.Fatal(err)
	}
	_, err = sessionStore.Save(ctx, sessions.Session{
		ID:            "sess-rlm-1",
		WorkspaceID:   workspaceID,
		WorkspacePath: workspace,
		ProjectName:   "foxctl",
		Summary:       "Implemented gather_context certification for RLM.",
		Decisions:     []string{"Use ContextBundle as the bounded answering surface."},
		KeyFiles:      []string{"internal/context/contextengine/context_gather.go"},
		StartedAt:     time.Date(2026, 4, 30, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sessionStore.Close(); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{}
	cfg.Storage.Root = storageRoot
	adapter := NewReadOnlyAdapter(cfg, workspace, "", nil, rlm.Environment{
		Tools: []rlm.Tool{{Name: "gather_context", ReadOnly: true}},
	})
	out, err := adapter.Execute(ctx, "gather_context", mustJSON(map[string]any{
		"query":         "gather_context certification",
		"lanes":         []string{"context"},
		"limit":         3,
		"response_mode": "full",
	}))
	if err != nil {
		t.Fatalf("gather_context: %v", err)
	}
	body, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal output: %v", err)
	}
	var bundle contextengine.ContextBundle
	if err := json.Unmarshal(body, &bundle); err != nil {
		t.Fatalf("decode bundle: %v", err)
	}
	found := false
	for _, node := range bundle.Evidence {
		if node.Ref.Type == contextengine.RefTypeSession && node.Ref.Ref == "sess-rlm-1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("session recall evidence missing: %#v", bundle.Evidence)
	}
}

func TestReadOnlyAdapterExecuteDeniesToolsOutsideAllowlist(t *testing.T) {
	t.Parallel()

	adapter := NewReadOnlyAdapter(config.Config{}, t.TempDir(), "", nil, rlm.Environment{
		Tools: []rlm.Tool{
			{Name: "load_file", ReadOnly: true},
		},
	})

	_, err := adapter.Execute(context.Background(), "get_top_of_mind", nil)
	if err == nil {
		t.Fatal("Execute() error = nil, want allowlist denial")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestReadOnlyAdapterExecuteInternalBypassesAllowlist(t *testing.T) {
	t.Parallel()

	adapter := NewReadOnlyAdapter(config.Config{}, t.TempDir(), "", nil, rlm.Environment{
		TopOfMind: map[string]any{"objective": "verify bypass"},
		Tools: []rlm.Tool{
			{Name: "load_file", ReadOnly: true},
		},
	})

	out, err := adapter.ExecuteInternal(context.Background(), "get_top_of_mind", nil)
	if err != nil {
		t.Fatalf("ExecuteInternal() error = %v", err)
	}
	if out["top_of_mind"] == nil {
		t.Fatalf("ExecuteInternal() output=%v", out)
	}
}

func TestDirectContextPackFromObsidianHits(t *testing.T) {
	t.Parallel()

	pack := directContextPackFromObsidianHits("ws-test", "context bundle docs", []obsidianindex.SearchHit{{
		Path:    "docs/architecture/rlm-gather-context.md",
		Title:   "RLM Gather Context",
		Type:    "architecture",
		Project: "foxctl",
		Status:  "current",
		Trust:   "verified",
		Score:   900,
		Snippet: "ContextBundle certification bridge",
	}})
	if pack.Lane != contextengine.LaneContext || len(pack.Nodes) != 1 {
		t.Fatalf("pack=%+v", pack)
	}
	node := pack.Nodes[0]
	if node.Ref.Type != contextengine.RefTypePath || node.Ref.Ref != "docs/architecture/rlm-gather-context.md" {
		t.Fatalf("ref=%+v", node.Ref)
	}
	if node.Statement != "ContextBundle certification bridge" {
		t.Fatalf("statement=%q", node.Statement)
	}
}

func TestReadOnlyAdapterGatherContext(t *testing.T) {
	t.Parallel()

	adapter := NewReadOnlyAdapter(config.Config{}, t.TempDir(), "", nil, rlm.Environment{
		TopOfMind: map[string]any{"objective": "certify context", "phase": "implementation"},
		Tools: []rlm.Tool{
			{Name: "gather_context", ReadOnly: true},
		},
	})

	out, err := adapter.ExecuteInternal(context.Background(), "gather_context", mustJSON(map[string]any{
		"query":         "certify context",
		"lanes":         []string{"context"},
		"limit":         3,
		"response_mode": "full",
	}))
	if err != nil {
		t.Fatalf("gather_context: %v", err)
	}
	if out["certificate"] == nil {
		t.Fatalf("output missing certificate: %v", out)
	}
	if out["answerable"] != true {
		t.Fatalf("answerable=%v output=%v", out["answerable"], out)
	}

	defaultOut, err := adapter.Execute(context.Background(), "gather_context", mustJSON(map[string]any{"query": "certify"}))
	if err != nil {
		t.Fatalf("allowlisted gather_context Execute: %v", err)
	}
	if got := defaultOut["schema_version"]; got != "context_answer_surface/v2" {
		t.Fatalf("default gather_context schema_version=%v output=%v", got, defaultOut)
	}
}

func TestReadOnlyAdapterGatherContextAnswerSurfaceOmitsRawEvidence(t *testing.T) {
	t.Parallel()

	adapter := NewReadOnlyAdapter(config.Config{}, t.TempDir(), "", nil, rlm.Environment{
		TopOfMind: map[string]any{
			"objective": "certify context",
			"relevant_refs": []map[string]any{{
				"type": "path",
				"ref":  "internal/context/contextengine/context_bundle.go",
			}},
		},
		Tools: []rlm.Tool{{Name: "gather_context", ReadOnly: true}},
	})

	out, err := adapter.ExecuteInternal(context.Background(), "gather_context", mustJSON(map[string]any{
		"query":           "certify context",
		"task_type":       "subsystem_map",
		"source_profiles": []string{"repo_code", "codemaps", "cochange_history"},
		"lanes":           []string{"context"},
		"limit":           3,
		"response_mode":   "answer_surface",
	}))
	if err != nil {
		t.Fatalf("gather_context answer_surface: %v", err)
	}
	if _, ok := out["evidence"]; ok {
		t.Fatalf("answer_surface should omit raw evidence: %v", out)
	}
	if out["selected_paths"] == nil {
		t.Fatalf("answer_surface missing selected_paths: %v", out)
	}
	certificate, _ := out["certificate"].(map[string]any)
	if certificate == nil {
		t.Fatalf("answer_surface missing certificate: %v", out)
	}
	if certificate["status"] == "certified" {
		t.Fatalf("missing selected path should not certify: %v", out)
	}
	contract, _ := out["answer_contract"].(map[string]any)
	if contract["copy_answer_seed"] == true {
		t.Fatalf("missing selected path should not be copyable: %v", out)
	}
	if out["answer_candidates"] == nil {
		t.Fatalf("answer_surface missing answer_candidates: %v", out)
	}
	if out["schema_version"] != "context_answer_surface/v2" {
		t.Fatalf("schema_version=%v output=%v", out["schema_version"], out)
	}
	seed, ok := out["answer_seed"].(map[string]any)
	if !ok || seed["paths"] == nil {
		t.Fatalf("answer_surface missing answer_seed.paths: %v", out)
	}
	pathSet, ok := out["path_set"].(map[string]any)
	if !ok || pathSet["must"] == nil {
		t.Fatalf("answer_surface missing path_set.must: %v", out)
	}
	loadQueue, ok := out["load_queue"].([]map[string]any)
	if !ok || len(loadQueue) == 0 || loadQueue[0]["ref"] == "" {
		t.Fatalf("answer_surface missing load_queue refs: %T %v", out["load_queue"], out["load_queue"])
	}
	categories, ok := out["categories"].([]contextengine.ContextCategory)
	if !ok || len(categories) == 0 {
		t.Fatalf("answer_surface missing subsystem categories: %T %v", out["categories"], out["categories"])
	}
	contract, ok = out["answer_contract"].(map[string]any)
	if !ok || contract["mode"] != "repo_subsystem_map" {
		t.Fatalf("answer_contract=%v", out["answer_contract"])
	}
	trust, ok := out["trust"].(map[string]any)
	if !ok {
		t.Fatalf("answer_surface missing trust: %v", out)
	}
	confidence, _ := trust["confidence"].(map[string]any)
	if confidence == nil || confidence["level"] == "" || confidence["trusted_for_proceed"] == nil {
		t.Fatalf("trust confidence missing fields: %v", trust)
	}
	if freshness, _ := trust["freshness"].(map[string]any); freshness == nil || freshness["repoindex"] == "" || freshness["live_overlay_used"] == nil {
		t.Fatalf("trust freshness missing fields: %v", trust)
	}
	if tests, _ := trust["tests"].(map[string]any); tests == nil || tests["policy"] != "omitted_by_default" {
		t.Fatalf("trust tests missing policy: %v", trust)
	}
	if loadability, _ := trust["loadability"].(map[string]any); loadability == nil || loadability["selected_refs_loadable"] == nil {
		t.Fatalf("trust loadability missing fields: %v", trust)
	}
	if trust["next_action"] == "" {
		t.Fatalf("trust next_action missing: %v", trust)
	}
}

func TestReadOnlyAdapterGatherContextAnswerSurfaceGraphSummaryOptIn(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "app.go"), "package app\n\nimport \"example.com/project/dep\"\n\nfunc Run() { dep.Helper() }\n")
	writeTestFile(t, filepath.Join(workspace, "dep.go"), "package dep\n\nfunc Helper() {}\n")

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatalf("repoindex.Open: %v", err)
	}
	defer store.Close()
	repoKey := store.RepoKey()
	appNode := repoindex.Node{
		ID:   repoindex.FileID(repoKey, "example.com/project", "app.go"),
		Kind: repoindex.NodeFile,
		Pkg:  "example.com/project",
		File: "app.go",
	}
	depNode := repoindex.Node{
		ID:   repoindex.FileID(repoKey, "example.com/project", "dep.go"),
		Kind: repoindex.NodeFile,
		Pkg:  "example.com/project",
		File: "dep.go",
	}
	if err := store.ReplaceAll(ctx, []repoindex.Node{appNode, depNode}, []repoindex.Edge{{
		Src:    appNode.ID,
		Dst:    depNode.ID,
		Type:   repoindex.EdgeImports,
		Weight: 1,
	}}); err != nil {
		t.Fatalf("ReplaceAll: %v", err)
	}
	if err := store.SetMeta(ctx, repoindex.IndexMeta{RepoRoot: workspace}); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}

	adapter := NewReadOnlyAdapter(config.Config{Storage: config.StorageSettings{Root: storageRoot}}, workspace, "", nil, rlm.Environment{
		TopOfMind: map[string]any{
			"objective": "find app context",
			"relevant_refs": []map[string]any{{
				"type": "path",
				"ref":  "app.go",
			}},
		},
		Tools: []rlm.Tool{{Name: "gather_context", ReadOnly: true}},
	})
	defaultOut, err := adapter.ExecuteInternal(ctx, "gather_context", mustJSON(map[string]any{
		"query":         "trace app dependency context",
		"lanes":         []string{"context"},
		"task_type":     "execution_trace",
		"response_mode": "answer_surface",
	}))
	if err != nil {
		t.Fatalf("gather_context default: %v", err)
	}
	if _, ok := defaultOut["context_graph"]; ok {
		t.Fatalf("context_graph should be opt-in by graph_mode: %v", defaultOut["context_graph"])
	}
	graphMeta, ok := defaultOut["graph"].(map[string]any)
	if !ok {
		t.Fatalf("default answer_surface missing graph metadata: %v", defaultOut)
	}
	if graphMeta["used"] != false || graphMeta["graph_required"] != true || graphMeta["recommended_next_tool"] != "expand_context_graph" {
		t.Fatalf("default graph metadata=%v", graphMeta)
	}
	if refs, ok := graphMeta["root_refs"].([]string); !ok || len(refs) == 0 {
		t.Fatalf("default graph metadata missing root refs: %T %v", graphMeta["root_refs"], graphMeta["root_refs"])
	}

	out, err := adapter.ExecuteInternal(ctx, "gather_context", mustJSON(map[string]any{
		"query":         "trace app dependency context",
		"lanes":         []string{"context"},
		"task_type":     "execution_trace",
		"response_mode": "answer_surface",
		"graph_mode":    "summary",
	}))
	if err != nil {
		t.Fatalf("gather_context graph summary: %v", err)
	}
	graph, ok := out["context_graph"].(map[string]any)
	if !ok {
		t.Fatalf("missing context_graph summary: %v", out)
	}
	if graph["roots"] == nil || graph["top_nodes"] == nil || graph["top_edges"] == nil || graph["confidence"] == nil || graph["missing"] == nil {
		t.Fatalf("context_graph missing compact fields: %v", graph)
	}
	if nodes, ok := graph["top_nodes"].([]map[string]any); !ok || len(nodes) == 0 {
		t.Fatalf("context_graph missing top_nodes: %T %v", graph["top_nodes"], graph["top_nodes"])
	}
	if _, ok := graph["nodes"]; ok {
		t.Fatalf("context_graph should not include full nodes: %v", graph)
	}
	if _, ok := graph["edges"]; ok {
		t.Fatalf("context_graph should not include full edges: %v", graph)
	}
	graphMeta, ok = out["graph"].(map[string]any)
	if !ok || graphMeta["used"] != true || graphMeta["mode"] != "summary" || graphMeta["recommended_next_tool"] != nil {
		t.Fatalf("summary graph metadata=%v", graphMeta)
	}
}

func TestReadOnlyAdapterGatherContextIncludesACAContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	vault := t.TempDir()
	notePath := filepath.Join(vault, "notes", "repo", "foxctl", "contextbundle-certification.md")
	if err := os.MkdirAll(filepath.Dir(notePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(notePath, []byte(`# ContextBundle Certification

The certified bundle gate requires runtime certification before an answerer trusts gathered context.
`), 0o644); err != nil {
		t.Fatal(err)
	}
	index, err := obsidianindex.Open(ctx, storageRoot, vault)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := index.Rebuild(ctx, vault); err != nil {
		t.Fatal(err)
	}
	if err := index.Close(); err != nil {
		t.Fatal(err)
	}

	workspaceID := ws.ID(workspace)
	acaStore := contextplane.NewWorkspaceStore(workspace)
	if _, err := acaStore.SaveTopOfMind(contextplane.TopOfMind{
		WorkspaceID: workspaceID,
		Objective:   "Wire ACA context into gather_context",
		Phase:       "implementation",
		UpdatedAt:   time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}

	var cfg config.Config
	cfg.Storage.Root = storageRoot
	adapter := NewReadOnlyAdapter(cfg, workspace, vault, nil, rlm.Environment{
		TopOfMind: map[string]any{
			"workspace_id": workspaceID,
			"objective":    "Wire ACA context into gather_context",
			"phase":        "implementation",
		},
		LatestHandoff: map[string]any{
			"summary": "Runtime certification belongs to ContextCertificate.",
			"ref":     "note:handoff:contextbundle-certification",
			"file_refs": []any{
				map[string]any{"type": "path", "ref": "internal/context/contextengine/context_certify.go"},
			},
		},
		Tools: []rlm.Tool{{Name: "gather_context", ReadOnly: true}},
	})

	out, err := adapter.ExecuteInternal(ctx, "gather_context", mustJSON(map[string]any{
		"query":         "ContextBundle certified bundle gate runtime certification",
		"lanes":         []string{"context"},
		"limit":         8,
		"response_mode": "full",
	}))
	if err != nil {
		t.Fatalf("gather_context: %v", err)
	}
	var bundle contextengine.ContextBundle
	body, _ := json.Marshal(out)
	if err := json.Unmarshal(body, &bundle); err != nil {
		t.Fatal(err)
	}
	var facts []string
	for _, fact := range bundle.Facts {
		facts = append(facts, fact.Fact)
	}
	joined := strings.Join(facts, "\n")
	if !strings.Contains(joined, "Runtime certification belongs to ContextCertificate") {
		t.Fatalf("missing handoff fact: %s", joined)
	}
	if !strings.Contains(joined, "certified bundle gate requires runtime certification") {
		t.Fatalf("missing ACA vault fact: %s", joined)
	}
	if bundle.SourceCoverage["context"] == 0 {
		t.Fatalf("source coverage missing context lane: %#v", bundle.SourceCoverage)
	}
}

func TestReadOnlyAdapterLoadsTrajectoryAndCASArtifacts(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	casRoot := filepath.Join(storageRoot, "cas")
	if err := os.MkdirAll(casRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	var cfg config.Config
	cfg.Storage.Root = storageRoot
	cfg.Paths.CAS = casRoot

	casStore, err := cas.NewStore(casRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer casStore.Close()
	obj, err := casStore.Put(ctx, strings.NewReader("artifact body"), "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}

	trajStore, err := trajectory.Open(ctx, storageRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer trajStore.Close()
	traj, err := trajStore.InsertTrajectory(ctx, trajectory.Trajectory{
		ID:             "traj-1",
		WorkspaceID:    ws.ID(workspace),
		Status:         trajectory.StatusOK,
		Summary:        "artifact summary",
		ArtifactDigest: obj.Digest,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	adapter := NewReadOnlyAdapter(cfg, workspace, "", nil, rlm.Environment{
		ArtifactHandles: []string{"trajectory:" + traj.ID, "artifact:" + obj.Digest},
	})

	search, err := adapter.ExecuteInternal(ctx, "search_artifacts", mustJSON(map[string]any{
		"query": "artifact",
		"limit": 5,
	}))
	if err != nil {
		t.Fatalf("search_artifacts: %v", err)
	}
	raw, _ := json.Marshal(search["results"])
	var handles []string
	if err := json.Unmarshal(raw, &handles); err != nil {
		t.Fatalf("decode handles: %v", err)
	}
	if len(handles) == 0 {
		t.Fatalf("expected artifact handles")
	}

	loaded, err := adapter.ExecuteInternal(ctx, "load_artifact", mustJSON(map[string]any{
		"handle": "artifact:" + obj.Digest,
	}))
	if err != nil {
		t.Fatalf("load_artifact: %v", err)
	}
	artifact, ok := loaded["artifact"].(map[string]any)
	if !ok || artifact["content"] != "artifact body" {
		t.Fatalf("artifact=%v", loaded)
	}
}

func TestReadOnlyAdapterSubcallCarriesRole(t *testing.T) {
	t.Parallel()

	adapter := NewReadOnlyAdapter(config.Config{}, t.TempDir(), "", nil, rlm.Environment{})
	var gotTask rlm.Task
	adapter.SetSubcall(func(_ context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
		gotTask = task
		return rlm.Result{Answer: "ok"}, nil
	})

	_, err := adapter.ExecuteInternal(context.Background(), "subcall", mustJSON(map[string]any{
		"prompt": "Find the latest codename update.",
		"role":   ScoutRoleMemoryTimeline,
	}))
	if err != nil {
		t.Fatalf("subcall: %v", err)
	}
	if gotTask.Role != ScoutRoleMemoryTimeline {
		t.Fatalf("role=%q want %q", gotTask.Role, ScoutRoleMemoryTimeline)
	}
}

func TestReadOnlyAdapterMemoryEnsembleRetrieve(t *testing.T) {
	t.Parallel()

	adapter := NewReadOnlyAdapter(config.Config{}, t.TempDir(), "", nil, rlm.Environment{})
	var seenRoles []string
	adapter.SetSubcall(func(_ context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
		seenRoles = append(seenRoles, task.Role)
		return rlm.Result{
			Answer:         "summary for " + task.Role,
			EvidenceRefs:   []string{"evidence:" + task.Role},
			RetrievedPaths: []string{"path:" + task.Role},
		}, nil
	})

	out, err := adapter.ExecuteInternal(context.Background(), "memory_ensemble_retrieve", mustJSON(map[string]any{
		"query": "What changed about the codename over time?",
		"lanes": []string{"timeline", "facts"},
	}))
	if err != nil {
		t.Fatalf("memory_ensemble_retrieve: %v", err)
	}
	if len(seenRoles) != 2 {
		t.Fatalf("roles=%v want 2 roles", seenRoles)
	}
	if out["recommended_answer_basis"] != "combined" {
		t.Fatalf("recommended_answer_basis=%v want combined", out["recommended_answer_basis"])
	}
	metadata, ok := out["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata=%T", out["metadata"])
	}
	if metadata["scouts_run"] == nil {
		t.Fatalf("metadata=%v missing scouts_run", metadata)
	}
}

func TestReadOnlyAdapterMemoryEnsembleRetrieveStructuredFindings(t *testing.T) {
	t.Parallel()

	adapter := NewReadOnlyAdapter(config.Config{}, t.TempDir(), "", nil, rlm.Environment{})
	adapter.SetSubcall(func(_ context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
		switch task.Role {
		case ScoutRoleMemoryFact:
			return rlm.Result{
				Answer:       `{"summary":"Current codename is amber-river-19.","claims":[{"key":"codename","value":"amber-river-19","status":"current","source":"agent_memory_search"}]}`,
				EvidenceRefs: []string{"artifact:fact"},
			}, nil
		case ScoutRoleMemoryTimeline:
			return rlm.Result{
				Answer:       `{"summary":"The codename changed once.","current_best_view":"Current codename is amber-river-19.","timeline":[{"ts":"2026-03-20T10:00:00Z","kind":"update","value":"codename changed to amber-river-19","source":"session_timeline"}]}`,
				EvidenceRefs: []string{"artifact:timeline"},
			}, nil
		default:
			return rlm.Result{
				Answer:       `{"summary":"No durable ACA context beyond the active codename update.","context_blocks":[{"lane":"top_of_mind","summary":"codename rollout in progress","refs":["note:aca"]}]}`,
				EvidenceRefs: []string{"artifact:aca"},
			}, nil
		}
	})

	out, err := adapter.ExecuteInternal(context.Background(), "memory_ensemble_retrieve", mustJSON(map[string]any{
		"query":      "What is the current codename?",
		"max_scouts": 3,
	}))
	if err != nil {
		t.Fatalf("memory_ensemble_retrieve: %v", err)
	}
	if out["recommended_answer_basis"] != "timeline" {
		t.Fatalf("recommended_answer_basis=%v want timeline", out["recommended_answer_basis"])
	}
	if summary, _ := out["summary"].(string); !strings.Contains(summary, "amber-river-19") {
		t.Fatalf("summary=%q want codename", summary)
	}
	if claims, ok := out["claims"].([]memoryScoutClaim); !ok || len(claims) != 1 {
		t.Fatalf("claims=%T %v", out["claims"], out["claims"])
	}
	if timeline, ok := out["timeline"].([]memoryScoutTimelineItem); !ok || len(timeline) != 1 {
		t.Fatalf("timeline=%T %v", out["timeline"], out["timeline"])
	}
}

func TestReadOnlyAdapterResolvePreferredSkillArtifactPrefersWorkspaceLocalSkill(t *testing.T) {
	t.Setenv("FOXCTL_SKILLS_PATH", "")
	workspace := t.TempDir()
	skillDir := writeFakeExecSkill(t, workspace, "code/semantic_search", "#!/bin/sh\nprintf '{}'\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{})
	manifestPath, artifactPath, err := adapter.resolvePreferredSkillArtifact("code/semantic_search")
	if err != nil {
		t.Fatalf("resolvePreferredSkillArtifact: %v", err)
	}
	if got, want := filepath.Clean(manifestPath), filepath.Join(skillDir, "skill.yaml"); got != want {
		t.Fatalf("manifest=%q want %q", got, want)
	}
	if got, want := filepath.Clean(artifactPath), filepath.Join(skillDir, "code_semantic_search"); got != want {
		t.Fatalf("artifact=%q want %q", got, want)
	}
}

func TestReadOnlyAdapterSemanticSearchCodeDecodesCandidateBundlesFromWorkspaceSkill(t *testing.T) {
	t.Setenv("FOXCTL_SKILLS_PATH", "")
	workspace := t.TempDir()
	writeFakeExecSkill(t, workspace, "code/semantic_search", `#!/bin/sh
cat <<'EOF'
{"version":1,"status":"ok","command":"code/semantic_search","data":{"query":"skill manifest","results":[{"path":"skills/example/main.go","name":"Example","source":"symbols","summary":"implementation","similarity":0.91}],"candidate_bundles":[{"key":"skills/example","primary_path":"skills/example/skill.yaml","related_paths":["skills/example/main.go"],"symbols":["Example"],"sources":["symbols"],"score":0.91,"match_reason":"manifest bundle","ambiguity":"single_file_with_companions"}]},"meta":{"ts":"2026-03-23T00:00:00Z"},"error":{}}
EOF
`)

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{})
	out, err := adapter.semanticSearchCode(context.Background(), mustJSON(map[string]any{
		"query":   "skill manifest",
		"profile": "code",
		"scope":   []string{"symbols"},
		"limit":   8,
	}))
	if err != nil {
		t.Fatalf("semanticSearchCode: %v", err)
	}
	raw, err := json.Marshal(out["candidate_bundles"])
	if err != nil {
		t.Fatalf("marshal candidate_bundles: %v", err)
	}
	var bundles []map[string]any
	if err := json.Unmarshal(raw, &bundles); err != nil {
		t.Fatalf("decode candidate_bundles: %v", err)
	}
	if len(bundles) != 1 {
		t.Fatalf("candidate_bundles=%v want 1 bundle", bundles)
	}
	if got, want := bundles[0]["primary_path"], "skills/example/skill.yaml"; got != want {
		t.Fatalf("primary_path=%v want %q", got, want)
	}
	relatedRaw, err := json.Marshal(bundles[0]["related_paths"])
	if err != nil {
		t.Fatalf("marshal related_paths: %v", err)
	}
	var related []string
	if err := json.Unmarshal(relatedRaw, &related); err != nil {
		t.Fatalf("decode related_paths: %v", err)
	}
	if len(related) != 1 || related[0] != "skills/example/main.go" {
		t.Fatalf("related_paths=%v", related)
	}
}

func TestReadOnlyAdapterResolvePreferredSkillArtifactPrefersWorkspaceOverConfiguredSkillsRoot(t *testing.T) {
	t.Setenv("FOXCTL_SKILLS_PATH", "")
	workspace := t.TempDir()
	configuredSkills := t.TempDir()

	workspaceSkillDir := writeFakeExecSkill(t, workspace, "code/semantic_search", "#!/bin/sh\nprintf '{}'\n")
	configuredSkillDir := filepath.Join(configuredSkills, skill.NormalizeSkillName("code/semantic_search"))
	if err := os.MkdirAll(configuredSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configuredSkillDir, "skill.yaml"), []byte(`apiVersion: foxctl/v1
kind: Skill
metadata:
  name: code/semantic_search
  version: 0.1.0
  description: configured fake skill
distribution:
  type: exec
  exec:
    entry: code_semantic_search
io:
  format: JSON
signature:
  command: code/semantic_search
  parameters: []
  returns: []
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configuredSkillDir, "code_semantic_search"), []byte("#!/bin/sh\nprintf '{}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	var cfg config.Config
	cfg.Paths.Skills = configuredSkills
	adapter := NewReadOnlyAdapter(cfg, workspace, "", nil, rlm.Environment{})

	manifestPath, _, err := adapter.resolvePreferredSkillArtifact("code/semantic_search")
	if err != nil {
		t.Fatalf("resolvePreferredSkillArtifact: %v", err)
	}
	if got, want := filepath.Clean(manifestPath), filepath.Join(workspaceSkillDir, "skill.yaml"); got != want {
		t.Fatalf("manifest=%q want workspace skill %q", got, want)
	}
}

func TestReadOnlyAdapterCodeSearchEnsembleFileLocate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()

	writeTestFile(t, filepath.Join(workspace, "internal", "agent", "types", "types.go"), "package types\n\ntype AgentRole string\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "agent", "runtime", "runtime.go"), "package runtime\n\nfunc BuildToolDefsForRole() {}\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "env", "scout_roles.go"), "package env\n\nconst ScoutRole = \"experimental\"\n")

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repoKey := store.RepoKey()
	nodes := []repoindex.Node{
		{
			ID:        repoindex.FileID(repoKey, "agent/types", "internal/agent/types/types.go"),
			Kind:      repoindex.NodeFile,
			Pkg:       "agent/types",
			File:      "internal/agent/types/types.go",
			Name:      "types.go",
			Summary:   "Legacy agent role definitions for the classic runtime.",
			UpdatedAt: time.Now().UTC(),
		},
		{
			ID:        repoindex.NamespacedID(repoKey, "sym:agent/runtime:BuildToolDefsForRole"),
			Kind:      repoindex.NodeSymbol,
			Pkg:       "agent/runtime",
			File:      "internal/agent/runtime/runtime.go",
			Name:      "BuildToolDefsForRole",
			Summary:   "Role-specific runtime tool wiring for the classic agent runtime.",
			SpanStart: 3,
			SpanEnd:   3,
			UpdatedAt: time.Now().UTC(),
		},
		{
			ID:        repoindex.FileID(repoKey, "agent/runtime", "internal/agent/runtime/runtime.go"),
			Kind:      repoindex.NodeFile,
			Pkg:       "agent/runtime",
			File:      "internal/agent/runtime/runtime.go",
			Name:      "runtime.go",
			Summary:   "Classic runtime role wiring.",
			UpdatedAt: time.Now().UTC(),
		},
		{
			ID:        repoindex.FileID(repoKey, "rlm/env", "internal/rlm/env/scout_roles.go"),
			Kind:      repoindex.NodeFile,
			Pkg:       "rlm/env",
			File:      "internal/rlm/env/scout_roles.go",
			Name:      "scout_roles.go",
			Summary:   "Experimental RLM scout role routing.",
			UpdatedAt: time.Now().UTC(),
		},
	}
	if err := store.ReplaceAll(ctx, nodes, nil); err != nil {
		t.Fatal(err)
	}

	var cfg config.Config
	cfg.Storage.Root = storageRoot
	adapter := NewReadOnlyAdapter(cfg, workspace, "", nil, rlm.Environment{})

	out, err := adapter.ExecuteInternal(ctx, "code_search_ensemble", mustJSON(map[string]any{
		"query":     "legacy scout role classic runtime",
		"task_type": "file_locate",
		"constraints": map[string]any{
			"exclude_paths":     []string{"internal/rlm/env/**"},
			"require_grounding": true,
		},
	}))
	if err != nil {
		t.Fatalf("code_search_ensemble: %v", err)
	}
	if out["task_type"] != codeSearchTaskFileLocate {
		t.Fatalf("task_type=%v", out["task_type"])
	}
	metadata, ok := out["metadata"].(map[string]any)
	if !ok || metadata["grounded"] != true {
		t.Fatalf("metadata=%v", out["metadata"])
	}
	rawFiles, _ := json.Marshal(out["files"])
	var files []codeSearchEvidenceFile
	if err := json.Unmarshal(rawFiles, &files); err != nil {
		t.Fatalf("decode files: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("files=%v", out["files"])
	}
	foundRuntime := false
	for _, file := range files {
		if strings.HasPrefix(file.Path, "internal/rlm/env/") {
			t.Fatalf("excluded file leaked into output: %+v", files)
		}
		if file.Path == "internal/agent/runtime/runtime.go" {
			foundRuntime = true
		}
	}
	if !foundRuntime {
		t.Fatalf("expected runtime.go in files: %+v", files)
	}
}

func TestReadOnlyAdapterCodeSearchEnsembleExecutionTrace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()

	writeTestFile(t, filepath.Join(workspace, "internal", "agent", "daemon", "handlers.go"), "package daemon\n\nfunc HandleAsk() {\n\trunEngine()\n}\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "agent", "runtime", "runtime.go"), "package runtime\n\nfunc runEngine() {}\n")

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repoKey := store.RepoKey()

	handlerID := repoindex.NamespacedID(repoKey, "sym:agent/daemon:HandleAsk")
	runnerID := repoindex.NamespacedID(repoKey, "sym:agent/runtime:runEngine")
	nodes := []repoindex.Node{
		{
			ID:        handlerID,
			Kind:      repoindex.NodeSymbol,
			Pkg:       "agent/daemon",
			File:      "internal/agent/daemon/handlers.go",
			Name:      "HandleAsk",
			Summary:   "Mailbox ask handler entrypoint.",
			SpanStart: 3,
			SpanEnd:   5,
			UpdatedAt: time.Now().UTC(),
		},
		{
			ID:        runnerID,
			Kind:      repoindex.NodeSymbol,
			Pkg:       "agent/runtime",
			File:      "internal/agent/runtime/runtime.go",
			Name:      "runEngine",
			Summary:   "Runs the classic runtime engine.",
			SpanStart: 3,
			SpanEnd:   3,
			UpdatedAt: time.Now().UTC(),
		},
	}
	edges := []repoindex.Edge{
		{
			Src:    handlerID,
			Dst:    runnerID,
			Type:   repoindex.EdgeCalls,
			Weight: 1,
		},
	}
	if err := store.ReplaceAll(ctx, nodes, edges); err != nil {
		t.Fatal(err)
	}

	var cfg config.Config
	cfg.Storage.Root = storageRoot
	adapter := NewReadOnlyAdapter(cfg, workspace, "", nil, rlm.Environment{})

	out, err := adapter.ExecuteInternal(ctx, "code_search_ensemble", mustJSON(map[string]any{
		"query":     "HandleAsk execution path",
		"task_type": "execution_trace",
		"budget": map[string]any{
			"max_candidates": 4,
			"max_files":      2,
		},
	}))
	if err != nil {
		t.Fatalf("code_search_ensemble: %v", err)
	}
	if out["task_type"] != codeSearchTaskExecutionTrace {
		t.Fatalf("task_type=%v", out["task_type"])
	}
	metadata, ok := out["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata=%T", out["metadata"])
	}
	lanesRaw, _ := json.Marshal(metadata["lanes_used"])
	var lanes []string
	if err := json.Unmarshal(lanesRaw, &lanes); err != nil {
		t.Fatalf("decode lanes: %v", err)
	}
	foundGraph := false
	for _, lane := range lanes {
		if lane == "repo_graph" {
			foundGraph = true
			break
		}
	}
	if !foundGraph {
		t.Fatalf("expected repo_graph lane: %v", lanes)
	}
	rawCallPaths, _ := json.Marshal(out["call_paths"])
	var callPaths []map[string]any
	if err := json.Unmarshal(rawCallPaths, &callPaths); err != nil {
		t.Fatalf("decode call_paths: %v", err)
	}
	if len(callPaths) == 0 {
		t.Fatalf("call_paths=%v", out["call_paths"])
	}
}

func TestReadOnlyAdapterCodeSearchEnsembleUsesExactCodeProbe(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()

	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "env", "memory_ensemble.go"), "package env\n\nfunc describe() string { return \"memory_ensemble_retrieve\" }\n")

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.ReplaceAll(ctx, nil, nil); err != nil {
		t.Fatal(err)
	}

	var cfg config.Config
	cfg.Storage.Root = storageRoot
	adapter := NewReadOnlyAdapter(cfg, workspace, "", nil, rlm.Environment{})

	out, err := adapter.ExecuteInternal(ctx, "code_search_ensemble", mustJSON(map[string]any{
		"query":     "Where does memory_ensemble_retrieve live?",
		"task_type": "file_locate",
		"constraints": map[string]any{
			"require_grounding": true,
		},
	}))
	if err != nil {
		t.Fatalf("code_search_ensemble: %v", err)
	}
	rawFiles, _ := json.Marshal(out["files"])
	var files []codeSearchEvidenceFile
	if err := json.Unmarshal(rawFiles, &files); err != nil {
		t.Fatalf("decode files: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("files=%v", out["files"])
	}
	if files[0].Path != "internal/rlm/env/memory_ensemble.go" {
		t.Fatalf("files=%+v", files)
	}
	metadata, ok := out["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata=%T", out["metadata"])
	}
	rawLanes, _ := json.Marshal(metadata["lanes_used"])
	var lanes []string
	if err := json.Unmarshal(rawLanes, &lanes); err != nil {
		t.Fatalf("decode lanes: %v", err)
	}
	found := false
	for _, lane := range lanes {
		if lane == "exact_probe" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected exact_probe lane in %v", lanes)
	}
}

func TestReadOnlyAdapterCodeSearchEnsembleEmitsTelemetry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	obsDir := t.TempDir()
	observability.SetObsDirForTesting(obsDir)
	defer observability.SetObsDirForTesting("")

	workspace := t.TempDir()
	storageRoot := t.TempDir()

	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "env", "memory_ensemble.go"), "package env\n\nfunc describe() string { return \"memory_ensemble_retrieve\" }\n")
	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.ReplaceAll(ctx, nil, nil); err != nil {
		t.Fatal(err)
	}

	var cfg config.Config
	cfg.Storage.Root = storageRoot
	adapter := NewReadOnlyAdapter(cfg, workspace, "", nil, rlm.Environment{})

	out, err := adapter.ExecuteInternal(ctx, "code_search_ensemble", mustJSON(map[string]any{
		"query":     "Where does memory_ensemble_retrieve live?",
		"task_type": "file_locate",
		"constraints": map[string]any{
			"require_grounding": true,
		},
	}))
	if err != nil {
		t.Fatalf("code_search_ensemble: %v", err)
	}
	metadata, ok := out["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata=%T", out["metadata"])
	}
	telemetry, ok := metadata["telemetry"].(map[string]any)
	if !ok {
		t.Fatalf("telemetry=%T", metadata["telemetry"])
	}
	if intValue(telemetry["total_tool_calls"]) == 0 {
		t.Fatalf("telemetry=%v", telemetry)
	}
	entries, err := observability.QueryEventRecords(ctx, observability.EventQueryOptions{
		ObsDir:          obsDir,
		OperationPrefix: "context.code_search_ensemble",
		Limit:           10,
	})
	if err != nil {
		t.Fatalf("QueryEventRecords() error = %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected code_search_ensemble wide event")
	}
	if entries[0].Data["total_tool_calls"] == nil {
		t.Fatalf("event data=%v", entries[0].Data)
	}
}

func TestExtractSkillTokenUsage(t *testing.T) {
	t.Parallel()

	usage := extractSkillTokenUsage(map[string]any{
		"summary": map[string]any{
			"model":       "test-model",
			"tokens_used": 123,
		},
	})
	if usage == nil {
		t.Fatal("expected usage")
		return
	}
	if usage.Model != "test-model" || usage.TotalTokens != 123 {
		t.Fatalf("usage=%+v", usage)
	}
}

func TestReadOnlyAdapterCodeSearchEnsembleRegistrationTrace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()

	writeTestFile(t, filepath.Join(workspace, "cmd", "foxctl", "cmd", "eval.go"), "package cmd\n\nfunc newEvalCommand() *cobra.Command {\n\tcmd := &cobra.Command{}\n\tcmd.AddCommand(newEvalCodeSearchEnsembleCommand())\n\treturn cmd\n}\n")
	writeTestFile(t, filepath.Join(workspace, "cmd", "foxctl", "cmd", "eval_code_search_ensemble.go"), "package cmd\n\nfunc newEvalCodeSearchEnsembleCommand() *cobra.Command {\n\treturn &cobra.Command{}\n}\n")

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.ReplaceAll(ctx, nil, nil); err != nil {
		t.Fatal(err)
	}

	var cfg config.Config
	cfg.Storage.Root = storageRoot
	adapter := NewReadOnlyAdapter(cfg, workspace, "", nil, rlm.Environment{})

	out, err := adapter.ExecuteInternal(ctx, "code_search_ensemble", mustJSON(map[string]any{
		"query":     "Where does the eval command register code-search-ensemble?",
		"task_type": "registration_trace",
		"constraints": map[string]any{
			"require_grounding": true,
		},
	}))
	if err != nil {
		t.Fatalf("code_search_ensemble: %v", err)
	}
	rawFiles, _ := json.Marshal(out["files"])
	var files []codeSearchEvidenceFile
	if err := json.Unmarshal(rawFiles, &files); err != nil {
		t.Fatalf("decode files: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("files=%v", out["files"])
	}
	if files[0].Path != "cmd/foxctl/cmd/eval.go" {
		t.Fatalf("files=%+v", files)
	}
}

func TestReadOnlyAdapterCodeSearchEnsembleChangeImpact(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()

	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "env", "code_search_ensemble.go"), "package env\n\nfunc codeSearchEnsemble() {}\nfunc helper() {}\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "env", "adapter.go"), "package env\n\nfunc execute() { codeSearchEnsemble() }\n")
	writeTestFile(t, filepath.Join(workspace, "cmd", "foxctl", "cmd", "eval_code_search_ensemble.go"), "package cmd\n\nfunc runSingleCodeSearchEnsembleEval() {}\n")

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repoKey := store.RepoKey()
	targetID := repoindex.NamespacedID(repoKey, "sym:env:codeSearchEnsemble")
	adapterID := repoindex.NamespacedID(repoKey, "sym:env:execute")
	evalID := repoindex.NamespacedID(repoKey, "sym:cmd:runSingleCodeSearchEnsembleEval")
	nodes := []repoindex.Node{
		{
			ID:        targetID,
			Kind:      repoindex.NodeSymbol,
			Pkg:       "env",
			File:      "internal/rlm/env/code_search_ensemble.go",
			Name:      "codeSearchEnsemble",
			Summary:   "Main ensemble implementation.",
			SpanStart: 3,
			SpanEnd:   3,
			UpdatedAt: time.Now().UTC(),
		},
		{
			ID:        adapterID,
			Kind:      repoindex.NodeSymbol,
			Pkg:       "env",
			File:      "internal/rlm/env/adapter.go",
			Name:      "execute",
			Summary:   "Adapter entrypoint calling codeSearchEnsemble.",
			SpanStart: 3,
			SpanEnd:   3,
			UpdatedAt: time.Now().UTC(),
		},
		{
			ID:        evalID,
			Kind:      repoindex.NodeSymbol,
			Pkg:       "cmd",
			File:      "cmd/foxctl/cmd/eval_code_search_ensemble.go",
			Name:      "runSingleCodeSearchEnsembleEval",
			Summary:   "CLI eval path for code_search_ensemble.",
			SpanStart: 3,
			SpanEnd:   3,
			UpdatedAt: time.Now().UTC(),
		},
	}
	edges := []repoindex.Edge{
		{Src: adapterID, Dst: targetID, Type: repoindex.EdgeCalls, Weight: 1},
		{Src: evalID, Dst: adapterID, Type: repoindex.EdgeCalls, Weight: 1},
	}
	if err := store.ReplaceAll(ctx, nodes, edges); err != nil {
		t.Fatal(err)
	}

	var cfg config.Config
	cfg.Storage.Root = storageRoot
	adapter := NewReadOnlyAdapter(cfg, workspace, "", nil, rlm.Environment{})

	out, err := adapter.ExecuteInternal(ctx, "code_search_ensemble", mustJSON(map[string]any{
		"query":     "If you change codeSearchEnsemble, which files are directly impacted?",
		"task_type": "change_impact",
		"constraints": map[string]any{
			"require_grounding": true,
		},
	}))
	if err != nil {
		t.Fatalf("code_search_ensemble: %v", err)
	}
	rawFiles, _ := json.Marshal(out["files"])
	var files []codeSearchEvidenceFile
	if err := json.Unmarshal(rawFiles, &files); err != nil {
		t.Fatalf("decode files: %v", err)
	}
	if len(files) < 2 {
		t.Fatalf("files=%+v", files)
	}
	paths := map[string]bool{}
	for _, file := range files {
		paths[file.Path] = true
	}
	if !paths["internal/rlm/env/code_search_ensemble.go"] || !paths["internal/rlm/env/adapter.go"] {
		t.Fatalf("files=%+v", files)
	}
	metadata, ok := out["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata=%T", out["metadata"])
	}
	rawLanes, _ := json.Marshal(metadata["lanes_used"])
	var lanes []string
	if err := json.Unmarshal(rawLanes, &lanes); err != nil {
		t.Fatalf("decode lanes: %v", err)
	}
	foundImpact := false
	for _, lane := range lanes {
		if lane == "impact_graph" {
			foundImpact = true
			break
		}
	}
	if !foundImpact {
		t.Fatalf("expected impact_graph lane in %v", lanes)
	}
}

func TestReadOnlyAdapterCodeSearchEnsembleExecutionTracePromotesBridgeFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()

	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "env", "code_search_ensemble.go"), "package env\n\nfunc codeSearchEnsemble() {}\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "env", "adapter.go"), "package env\n\ntype ReadOnlyAdapter struct{}\n\nfunc NewReadOnlyAdapter() *ReadOnlyAdapter { return &ReadOnlyAdapter{} }\n\nfunc (a *ReadOnlyAdapter) Execute(name string) { if name == \"code_search_ensemble\" { codeSearchEnsemble() } }\n")
	writeTestFile(t, filepath.Join(workspace, "cmd", "foxctl", "cmd", "eval_code_search_ensemble.go"), "package cmd\n\nfunc runSingleCodeSearchEnsembleEval() { adapter := NewReadOnlyAdapter(); adapter.Execute(\"code_search_ensemble\") }\n")

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repoKey := store.RepoKey()
	targetID := repoindex.NamespacedID(repoKey, "sym:env:codeSearchEnsemble")
	adapterID := repoindex.NamespacedID(repoKey, "sym:env:execute")
	evalID := repoindex.NamespacedID(repoKey, "sym:cmd:runSingleCodeSearchEnsembleEval")
	nodes := []repoindex.Node{
		{
			ID:        targetID,
			Kind:      repoindex.NodeSymbol,
			Pkg:       "env",
			File:      "internal/rlm/env/code_search_ensemble.go",
			Name:      "codeSearchEnsemble",
			Summary:   "Main ensemble implementation.",
			SpanStart: 3,
			SpanEnd:   3,
			UpdatedAt: time.Now().UTC(),
		},
		{
			ID:        adapterID,
			Kind:      repoindex.NodeSymbol,
			Pkg:       "env",
			File:      "internal/rlm/env/adapter.go",
			Name:      "execute",
			Summary:   "Adapter entrypoint calling codeSearchEnsemble.",
			SpanStart: 3,
			SpanEnd:   3,
			UpdatedAt: time.Now().UTC(),
		},
		{
			ID:        evalID,
			Kind:      repoindex.NodeSymbol,
			Pkg:       "cmd",
			File:      "cmd/foxctl/cmd/eval_code_search_ensemble.go",
			Name:      "runSingleCodeSearchEnsembleEval",
			Summary:   "CLI eval path for code_search_ensemble.",
			SpanStart: 3,
			SpanEnd:   3,
			UpdatedAt: time.Now().UTC(),
		},
	}
	edges := []repoindex.Edge{
		{Src: evalID, Dst: adapterID, Type: repoindex.EdgeCalls, Weight: 1},
		{Src: adapterID, Dst: targetID, Type: repoindex.EdgeCalls, Weight: 1},
	}
	if err := store.ReplaceAll(ctx, nodes, edges); err != nil {
		t.Fatal(err)
	}

	var cfg config.Config
	cfg.Storage.Root = storageRoot
	adapter := NewReadOnlyAdapter(cfg, workspace, "", nil, rlm.Environment{})

	out, err := adapter.ExecuteInternal(ctx, "code_search_ensemble", mustJSON(map[string]any{
		"query":     "Which files connect runSingleCodeSearchEnsembleEval to code_search_ensemble execution?",
		"task_type": "execution_trace",
		"constraints": map[string]any{
			"require_grounding": true,
		},
		"budget": map[string]any{
			"max_candidates": 8,
			"max_files":      4,
		},
	}))
	if err != nil {
		t.Fatalf("code_search_ensemble: %v", err)
	}
	rawFiles, _ := json.Marshal(out["files"])
	var files []codeSearchEvidenceFile
	if err := json.Unmarshal(rawFiles, &files); err != nil {
		t.Fatalf("decode files: %v", err)
	}
	paths := map[string]bool{}
	for _, file := range files {
		paths[file.Path] = true
	}
	if !paths["cmd/foxctl/cmd/eval_code_search_ensemble.go"] || !paths["internal/rlm/env/adapter.go"] || !paths["internal/rlm/env/code_search_ensemble.go"] {
		t.Fatalf("files=%+v", files)
	}
	metadata, ok := out["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata=%T", out["metadata"])
	}
	rawLanes, _ := json.Marshal(metadata["lanes_used"])
	var lanes []string
	if err := json.Unmarshal(rawLanes, &lanes); err != nil {
		t.Fatalf("decode lanes: %v", err)
	}
	foundExecutionGraph := false
	for _, lane := range lanes {
		if lane == "execution_graph" {
			foundExecutionGraph = true
			break
		}
	}
	if !foundExecutionGraph {
		t.Fatalf("expected execution_graph lane: %v", lanes)
	}
	rawTrace, _ := json.Marshal(metadata["candidate_trace"])
	var candidateTrace []codeSearchCandidateTrace
	if err := json.Unmarshal(rawTrace, &candidateTrace); err != nil {
		t.Fatalf("decode candidate_trace: %v", err)
	}
	if len(candidateTrace) == 0 {
		t.Fatalf("candidate_trace=%v", metadata["candidate_trace"])
	}
	rawBridgeQueries, _ := json.Marshal(metadata["bridge_queries"])
	var bridgeQueries []string
	if err := json.Unmarshal(rawBridgeQueries, &bridgeQueries); err != nil {
		t.Fatalf("decode bridge_queries: %v", err)
	}
	if len(bridgeQueries) == 0 {
		t.Fatalf("bridge_queries=%v", metadata["bridge_queries"])
	}
}

func TestReadOnlyAdapterCodeSearchEnsembleSymbolInspectUsesGoDefinitions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()

	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "env", "code_search_ensemble.go"), "package env\n\ntype codeSearchEnsembleInput struct {\n\tQuery string\n}\n\ntype codeSearchEvidenceFile struct{}\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "env", "tool_exec.go"), "package env\n\nfunc useEvidence() { _ = \"codeSearchEnsembleInput\" }\n")

	var cfg config.Config
	cfg.Storage.Root = storageRoot
	adapter := NewReadOnlyAdapter(cfg, workspace, "", nil, rlm.Environment{})

	out, err := adapter.ExecuteInternal(ctx, "code_search_ensemble", mustJSON(map[string]any{
		"query":     "Which file defines codeSearchEnsembleInput?",
		"task_type": "symbol_inspect",
		"constraints": map[string]any{
			"require_grounding": true,
		},
		"budget": map[string]any{
			"max_candidates": 8,
			"max_files":      3,
		},
	}))
	if err != nil {
		t.Fatalf("code_search_ensemble: %v", err)
	}
	rawFiles, _ := json.Marshal(out["files"])
	var files []codeSearchEvidenceFile
	if err := json.Unmarshal(rawFiles, &files); err != nil {
		t.Fatalf("decode files: %v", err)
	}
	if len(files) == 0 || files[0].Path != "internal/rlm/env/code_search_ensemble.go" {
		t.Fatalf("files=%+v", files)
	}
	if !stringSliceHas(files[0].ConfirmedBy, "symbol_definition") {
		t.Fatalf("confirmed_by=%v", files[0].ConfirmedBy)
	}
	rawSymbols, _ := json.Marshal(out["symbols"])
	var symbols []codeSearchEvidenceSymbol
	if err := json.Unmarshal(rawSymbols, &symbols); err != nil {
		t.Fatalf("decode symbols: %v", err)
	}
	found := false
	for _, symbol := range symbols {
		if symbol.Path == "internal/rlm/env/code_search_ensemble.go" && symbol.Symbol == "codeSearchEnsembleInput" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("symbols=%+v", symbols)
	}
}

func TestReadOnlyAdapterLocalCodeProbeSearchSymbolInspectUsesDefinition(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "env", "adapter.go"), "package env\n\ntype ReadOnlyAdapter struct{}\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "env", "tool_exec.go"), "package env\n\nfunc useAdapter(value ReadOnlyAdapter) {}\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{})
	hits, err := adapter.localCodeProbeSearch(context.Background(), "Which file defines ReadOnlyAdapter?", codeSearchTaskSymbolInspect, nil, 4, codeSearchRequestOptions{}, nil)
	if err != nil {
		t.Fatalf("localCodeProbeSearch: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("hits=%v", hits)
	}
	if got := hits[0].Hit.Path; got != "internal/rlm/env/adapter.go" {
		t.Fatalf("top hit=%q hits=%+v", got, hits)
	}
	if hits[0].Hit.Symbol != "ReadOnlyAdapter" {
		t.Fatalf("symbol=%q", hits[0].Hit.Symbol)
	}
}

func TestReadOnlyAdapterLocalCodeProbeSearchSymbolInspectUsesPolyglotDefinitions(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		path    string
		content string
		query   string
		symbol  string
	}{
		"typescript": {
			path:    filepath.Join("src", "payment_router.ts"),
			content: "export class PaymentRouter {\n  dispatch() {}\n}\n",
			query:   "Which file defines PaymentRouter?",
			symbol:  "PaymentRouter",
		},
		"python": {
			path:    filepath.Join("services", "payment_router.py"),
			content: "class PaymentRouter:\n    pass\n",
			query:   "Which file defines PaymentRouter?",
			symbol:  "PaymentRouter",
		},
		"elixir": {
			path:    filepath.Join("lib", "payment_router.ex"),
			content: "defmodule PaymentRouter do\n  def dispatch(ctx), do: ctx\nend\n",
			query:   "Which file defines PaymentRouter?",
			symbol:  "PaymentRouter",
		},
		"csharp": {
			path:    filepath.Join("src", "PaymentRouter.cs"),
			content: "namespace Billing;\n\npublic sealed class PaymentRouter\n{\n    public Receipt Dispatch(PaymentRequest request) => new();\n}\n",
			query:   "Which file defines PaymentRouter?",
			symbol:  "PaymentRouter",
		},
	} {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			workspace := t.TempDir()
			writeTestFile(t, filepath.Join(workspace, tc.path), tc.content)
			adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{})
			hits, err := adapter.localCodeProbeSearch(context.Background(), tc.query, codeSearchTaskSymbolInspect, nil, 4, codeSearchRequestOptions{}, nil)
			if err != nil {
				t.Fatalf("localCodeProbeSearch: %v", err)
			}
			if len(hits) == 0 {
				t.Fatalf("hits=%v", hits)
			}
			want := filepath.ToSlash(tc.path)
			if got := hits[0].Hit.Path; got != want {
				t.Fatalf("top hit=%q want %q hits=%+v", got, want, hits)
			}
			if hits[0].Hit.Symbol != tc.symbol {
				t.Fatalf("symbol=%q want %q", hits[0].Hit.Symbol, tc.symbol)
			}
		})
	}
}

func TestCSharpLocalProviderFacts(t *testing.T) {
	t.Parallel()

	source := `namespace Billing;

public interface IPaymentRouter {}
public readonly record struct PaymentId(string Value);
internal enum PaymentState { Pending, Complete }

public sealed class PaymentRouter
{
    public Receipt Dispatch(PaymentRequest request) => new();
    public string CurrentState { get; init; }
}
`
	for symbol, wantLine := range map[string]int{
		"IPaymentRouter": 3,
		"PaymentId":      4,
		"PaymentState":   5,
		"PaymentRouter":  7,
		"Dispatch":       9,
		"CurrentState":   10,
	} {
		if got := findCSharpDefinitionLine(source, symbol); got != wantLine {
			t.Fatalf("findCSharpDefinitionLine(%q)=%d want %d", symbol, got, wantLine)
		}
	}
	if got := languageFromPath("src/Billing/PaymentRouter.cs"); got != "csharp" {
		t.Fatalf("languageFromPath=.cs got %q", got)
	}
	if !isLikelyLocalProviderCodeFile("src/Billing/PaymentRouter.cs") {
		t.Fatalf(".cs should be included in cheap local provider code files")
	}
	if !isTestLikeCodeSearchPath("tests/Billing/PaymentRouterTests.cs") {
		t.Fatalf("*Tests.cs should be test-like")
	}
	if !isTestLikeCodeSearchPath("tests/Billing/PaymentRouterTest.cs") {
		t.Fatalf("*Test.cs should be test-like")
	}
	companions := testCompanionPaths("src/Billing/PaymentRouter.cs")
	for _, want := range []string{
		"src/Billing/PaymentRouterTest.cs",
		"src/Billing/PaymentRouterTests.cs",
		"src/BillingTests/PaymentRouterTests.cs",
		"tests/Billing/PaymentRouterTests.cs",
	} {
		if !containsString(companions, want) {
			t.Fatalf("companions=%v missing %s", companions, want)
		}
	}
	if got := productionCounterpartPath("src/Billing/PaymentRouterTests.cs"); got != "src/Billing/PaymentRouter.cs" {
		t.Fatalf("productionCounterpartPath=%q", got)
	}
}

func TestReadOnlyAdapterSubsystemSiblingClosureFindsRequiredRole(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "internal", "context", "contextengine", "context_gather.go"), "package contextengine\n\nfunc GatherContext() {}\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "context", "contextengine", "context_reduce.go"), "package contextengine\n\nfunc ReduceEvidencePacksToBundle() {}\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "context", "contextengine", "context_verify.go"), "package contextengine\n\ntype RuntimeCertificate struct{}\n\nfunc CertifyRuntimeBundle() {}\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{})
	seeds := []contextengine.CodeSearchHit{
		{Path: "internal/context/contextengine/context_gather.go", Score: 0.9},
		{Path: "internal/context/contextengine/context_reduce.go", Score: 0.85},
	}
	hits := adapter.localSubsystemSiblingClosureHits(
		context.Background(),
		"map the context runtime subsystem",
		"subsystem_map",
		[]string{"runtime certification"},
		seeds,
		6,
		codeSearchRequestOptions{},
		nil,
	)
	if len(hits) == 0 {
		t.Fatalf("closure hits empty")
	}
	if got := hits[0].Hit.Path; got != "internal/context/contextengine/context_verify.go" {
		t.Fatalf("top closure hit=%q hits=%+v", got, hits)
	}
	if hits[0].Hit.Metadata["candidate_role"] != "structural_support" {
		t.Fatalf("metadata=%v", hits[0].Hit.Metadata)
	}
	matches, ok := hits[0].Hit.Metadata["matched_terms"].([]string)
	if !ok || !containsString(matches, "certify") {
		t.Fatalf("matched_terms=%T %v", hits[0].Hit.Metadata["matched_terms"], hits[0].Hit.Metadata["matched_terms"])
	}
}

func TestReadOnlyAdapterLiveOverlayFindsUntrackedCoverageFile(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	runGitForTest(t, workspace, "init")
	writeTestFile(t, filepath.Join(workspace, "internal", "context", "contextengine", "coverage.go"), "package contextengine\n\ntype CoverageRequirement struct{}\n\nfunc selectEvidenceCoverageAware() {}\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{})
	hits, err := adapter.liveOverlayCodeSearchHits(context.Background(), "coverage-aware selector", []string{"CoverageRequirement"}, 8, nil)
	if err != nil {
		t.Fatalf("liveOverlayCodeSearchHits: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("hits empty")
	}
	if got := hits[0].Hit.Path; got != "internal/context/contextengine/coverage.go" {
		t.Fatalf("top hit=%q hits=%+v", got, hits)
	}
	if hits[0].Hit.Metadata["source"] != "live_overlay" {
		t.Fatalf("metadata=%v", hits[0].Hit.Metadata)
	}
	coverageIDs, ok := hits[0].Hit.Metadata["coverage_requirement_ids"].([]string)
	if !ok || len(coverageIDs) == 0 {
		t.Fatalf("coverage ids=%T %v", hits[0].Hit.Metadata["coverage_requirement_ids"], hits[0].Hit.Metadata["coverage_requirement_ids"])
	}
}

func TestReadOnlyAdapterRequiredPathRepairFindsProductionSubsystemSiblings(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "internal", "context", "contextengine", "context_gather.go"), "package contextengine\n\nfunc GatherContext() {}\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "context", "contextengine", "context_bundle.go"), "package contextengine\n\ntype ContextBundle struct{}\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "context", "contextengine", "context_bundle_test.go"), "package contextengine\n\nfunc TestContextBundle() {}\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "context", "contextengine", "context_certify.go"), "package contextengine\n\nfunc CertifyContextBundle() {}\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{})
	hits := adapter.localRequiredPathRepairHits(
		[]string{"context_bundle", "context_certify"},
		[]contextengine.CodeSearchHit{
			{Path: "internal/context/contextengine/context_gather.go", Score: 0.9},
			{Path: "internal/context/contextengine/context_bundle.go", Score: 0.4},
		},
		8,
	)
	got := codeSearchHitPaths(hits)
	for _, want := range []string{
		"internal/context/contextengine/context_bundle.go",
		"internal/context/contextengine/context_certify.go",
	} {
		if !containsString(got, want) {
			t.Fatalf("paths=%v missing %s", got, want)
		}
	}
	if containsString(got, "internal/context/contextengine/context_bundle_test.go") {
		t.Fatalf("repair should prefer production file over test: %v", got)
	}
	for _, hit := range hits {
		if hit.Hit.Metadata["source"] != "local_required_path_repair" {
			t.Fatalf("metadata=%v", hit.Hit.Metadata)
		}
	}
}

func TestReadOnlyAdapterRequiredDefinitionRepairFindsSiblingPolicyDefinition(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "env", "tools.go"), "package env\n\nfunc DefaultTools() {}\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "env", "tool_exec.go"), "package env\n\nfunc executeInternal() {}\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "run_spec.go"), "package rlm\n\nfunc ResolveToolPolicy() {}\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{})
	hits := adapter.localRequiredDefinitionRepairHits(context.Background(), []string{"DefaultTools", "executeInternal", "ResolveToolPolicy"}, 8, codeSearchRequestOptions{}, nil)
	got := codeSearchHitPaths(hits)
	for _, want := range []string{
		"internal/rlm/env/tools.go",
		"internal/rlm/env/tool_exec.go",
		"internal/rlm/run_spec.go",
	} {
		if !containsString(got, want) {
			t.Fatalf("paths=%v missing %s", got, want)
		}
	}
	for _, hit := range hits {
		if hit.Hit.Metadata["source"] != "local_required_definition_repair" {
			t.Fatalf("metadata=%v", hit.Hit.Metadata)
		}
	}
}

func TestMergeCodeSearchHitsAppliesLanguageAndPathConstraints(t *testing.T) {
	t.Parallel()

	hits := mergeCodeSearchHitsWithOptions(8, normalizeCodeSearchRequestOptions(codeSearchRequestOptions{
		Languages:    []string{"elixir"},
		PathPrefixes: []string{"apps/api"},
	}), nil,
		[]rankedCodeSearchHit{{Priority: 90, Hit: contextengine.CodeSearchHit{Path: "apps/api/lib/payments.ex", Score: 0.9}}},
		[]rankedCodeSearchHit{{Priority: 95, Hit: contextengine.CodeSearchHit{Path: "apps/api/deps/phoenix/lib/router.ex", Score: 0.99}}},
		[]rankedCodeSearchHit{{Priority: 94, Hit: contextengine.CodeSearchHit{Path: "apps/web/src/router.ts", Score: 0.99}}},
	)
	got := extractCodeSearchHitPaths(hits)
	if !reflect.DeepEqual(got, []string{"apps/api/lib/payments.ex"}) {
		t.Fatalf("paths=%v", got)
	}
}

func TestCodeSearchCandidatePoolLimitExpandsForRequiredEvidence(t *testing.T) {
	t.Parallel()

	if got := codeSearchCandidatePoolLimit(8, "execution_trace", 0, 0); got != 8 {
		t.Fatalf("without requirements got %d", got)
	}
	if got := codeSearchCandidatePoolLimit(8, "execution_trace", 4, 0); got <= 8 {
		t.Fatalf("with requirements got %d", got)
	}
}

func TestCodeSearchHitsCoverRequirementsUsesStrictPathShapedCoverage(t *testing.T) {
	t.Parallel()

	hits := []contextengine.CodeSearchHit{
		{
			Path:    "cmd/foxctl/cmd/eval_gather_context_test.go",
			Snippet: "mentions internal/context/contextengine/context_gather.go",
			Score:   0.9,
		},
	}
	if codeSearchHitsCoverRequirements(hits, []string{"context_gather"}) {
		t.Fatalf("snippet mention should not cover path-shaped requirement")
	}
	hits = append(hits, contextengine.CodeSearchHit{Path: "internal/context/contextengine/context_gather.go", Score: 0.8})
	if !codeSearchHitsCoverRequirements(hits, []string{"context_gather"}) {
		t.Fatalf("path-shaped requirement should be covered by matching path")
	}
}

func TestReadOnlyAdapterRepoDocsSourceProfileFindsDocs(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "docs", "README.md"), "# Docs\n\nArchitecture docs are indexed from this map.\n")
	writeTestFile(t, filepath.Join(workspace, "docs", "architecture", "README.md"), "# Architecture\n\nThe gather context architecture lives here.\n")
	writeTestFile(t, filepath.Join(workspace, "docs", "architecture", "rlm-gather-context.md"), "# RLM gather_context\n\nContextBundle certification and answer surfaces.\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "env", "tool_exec.go"), "package env\n\nfunc gatherContext() {}\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{})
	hits, err := adapter.repoDocsSearchHits(
		context.Background(),
		"Which repo documentation files explain gather_context architecture?",
		[]string{"rlm-gather-context", "architecture", "docs"},
		[]contextengine.SourceProfile{contextengine.SourceProfileRepoDocs},
		8,
		codeSearchRequestOptions{},
		nil,
	)
	if err != nil {
		t.Fatalf("repoDocsSearchHits: %v", err)
	}
	got := codeSearchHitPaths(hits)
	for _, want := range []string{
		"docs/architecture/rlm-gather-context.md",
		"docs/architecture/README.md",
		"docs/README.md",
	} {
		if !containsString(got, want) {
			t.Fatalf("paths=%v missing %s", got, want)
		}
	}
	for _, hit := range hits {
		if hit.Hit.Path == "internal/rlm/env/tool_exec.go" {
			t.Fatalf("repo docs provider returned code hit: %+v", hit)
		}
	}
}

func TestReadOnlyAdapterRepoDocsSkipsExcludedPaths(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "docs", "README.md"), "# Docs\n\nArchitecture docs are indexed from this map.\n")
	writeTestFile(t, filepath.Join(workspace, "docs", "architecture", "rlm-gather-context.md"), "# RLM gather_context\n\nContextBundle certification and answer surfaces.\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{})
	hits, err := adapter.repoDocsSearchHits(
		context.Background(),
		"Which repo documentation files explain gather_context architecture?",
		[]string{"rlm-gather-context", "architecture", "docs"},
		[]contextengine.SourceProfile{contextengine.SourceProfileRepoDocs},
		8,
		codeSearchRequestOptions{ExcludedPaths: []string{"docs/architecture/"}},
		nil,
	)
	if err != nil {
		t.Fatalf("repoDocsSearchHits: %v", err)
	}
	got := codeSearchHitPaths(hits)
	if containsString(got, "docs/architecture/rlm-gather-context.md") {
		t.Fatalf("repo docs provider returned excluded path: %v", got)
	}
	if !containsString(got, "docs/README.md") {
		t.Fatalf("paths=%v missing docs/README.md", got)
	}
}

func TestReadOnlyAdapterCoverageFanoutFindsPathTermFile(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "internal", "storage", "memory", "factory.go"), "package memory\n\nfunc NewStoreFactory() {}\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "storage", "memory", "vector.go"), "package memory\n\ntype VectorDimensionConfig struct{}\n")
	writeTestFile(t, filepath.Join(workspace, "docs", "memory.md"), "# vector\n\nDocumentation should not satisfy repo_code coverage fanout.\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{})
	hits, err := adapter.localCoverageCodeSearchHits(
		context.Background(),
		"Which files configure memory vector storage dimensions?",
		[]string{"vector"},
		8,
		codeSearchRequestOptions{},
		nil,
	)
	if err != nil {
		t.Fatalf("localCoverageCodeSearchHits: %v", err)
	}
	got := codeSearchHitPaths(hits)
	if !containsString(got, "internal/storage/memory/vector.go") {
		t.Fatalf("paths=%v missing vector.go", got)
	}
	if containsString(got, "docs/memory.md") {
		t.Fatalf("coverage code fanout included docs path: %v", got)
	}
}

func TestReadOnlyAdapterLocalProvidersSkipExcludedPaths(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "generated", "adapter.go"), "package generated\n\ntype ReadOnlyAdapter struct{}\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "storage", "memory", "vector.go"), "package memory\n\ntype VectorDimensionConfig struct{}\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{})
	options := codeSearchRequestOptions{ExcludedPaths: []string{"generated/", "internal/storage/memory/"}}

	probeHits, err := adapter.localCodeProbeSearch(context.Background(), "Which file defines ReadOnlyAdapter?", codeSearchTaskSymbolInspect, nil, 4, options, nil)
	if err != nil {
		t.Fatalf("localCodeProbeSearch: %v", err)
	}
	if got := codeSearchHitPaths(probeHits); containsString(got, "generated/adapter.go") {
		t.Fatalf("probe provider returned excluded path: %v", got)
	}

	coverageHits, err := adapter.localCoverageCodeSearchHits(
		context.Background(),
		"Which files configure memory vector storage dimensions?",
		[]string{"vector"},
		8,
		options,
		nil,
	)
	if err != nil {
		t.Fatalf("localCoverageCodeSearchHits: %v", err)
	}
	if got := codeSearchHitPaths(coverageHits); containsString(got, "internal/storage/memory/vector.go") {
		t.Fatalf("coverage provider returned excluded path: %v", got)
	}
}

func TestReadOnlyAdapterNonCodeConfigDataProviderFindsGenericFixtures(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "tests_data", "physics_scenarios.json"), `{
  "scenarios": [{"name": "inclined_plane", "gravity": 9.81}]
}
`)
	writeTestFile(t, filepath.Join(workspace, "config", "simulation.toml"), "solver = \"rk4\"\nphysics_profile = \"lab\"\n")
	writeTestFile(t, filepath.Join(workspace, "docs", "data", "fixtures.yaml"), "fixture_index:\n  - physics scenarios\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "physics", "solver.go"), "package physics\n\nfunc Solver() {}\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{})
	hits := adapter.localNonCodeConfigDataSearchHits(
		context.Background(),
		"Which data fixtures define physics scenarios?",
		[]string{"physics scenarios"},
		[]contextengine.SourceProfile{contextengine.SourceProfileRepoCode},
		8,
		codeSearchRequestOptions{},
		nil,
	)
	got := codeSearchHitPaths(hits)
	if !containsString(got, "tests_data/physics_scenarios.json") {
		t.Fatalf("paths=%v missing tests_data/physics_scenarios.json", got)
	}
	if containsString(got, "docs/data/fixtures.yaml") {
		t.Fatalf("repo_code provider returned repo docs fixture: %v", got)
	}
	var found contextengine.CodeSearchHit
	for _, hit := range hits {
		if hit.Hit.Path == "tests_data/physics_scenarios.json" {
			found = hit.Hit
			break
		}
	}
	if found.Metadata["candidate_role"] != "test_data_support" {
		t.Fatalf("candidate_role=%v metadata=%v", found.Metadata["candidate_role"], found.Metadata)
	}
	if found.Metadata["source_profile"] != "repo_code" {
		t.Fatalf("source_profile=%v metadata=%v", found.Metadata["source_profile"], found.Metadata)
	}
	if ids := metadataStringSliceEnv(found.Metadata, "coverage_requirement_ids"); len(ids) == 0 {
		t.Fatalf("coverage ids missing: %v", found.Metadata)
	}

	configHits := adapter.localNonCodeConfigDataSearchHits(
		context.Background(),
		"Which simulation config sets physics profile?",
		[]string{"simulation config"},
		[]contextengine.SourceProfile{contextengine.SourceProfileRepoCode},
		8,
		codeSearchRequestOptions{},
		nil,
	)
	for _, hit := range configHits {
		if hit.Hit.Path == "config/simulation.toml" && hit.Hit.Metadata["candidate_role"] != "config_support" {
			t.Fatalf("config metadata=%v", hit.Hit.Metadata)
		}
	}

	docHits := adapter.localNonCodeConfigDataSearchHits(
		context.Background(),
		"Which docs data fixture indexes physics scenarios?",
		[]string{"fixture index"},
		[]contextengine.SourceProfile{contextengine.SourceProfileRepoDocs},
		8,
		codeSearchRequestOptions{},
		nil,
	)
	got = codeSearchHitPaths(docHits)
	if !containsString(got, "docs/data/fixtures.yaml") {
		t.Fatalf("repo_docs paths=%v missing docs/data/fixtures.yaml", got)
	}
	for _, hit := range docHits {
		if hit.Hit.Path == "docs/data/fixtures.yaml" && hit.Hit.Metadata["source_profile"] != "repo_docs" {
			t.Fatalf("docs metadata=%v", hit.Hit.Metadata)
		}
	}
}

func TestGatherContextSelectsNonCodeTestDataWithCoverage(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "tests_data", "physics_scenarios.json"), `{
  "scenarios": [{"name": "inclined_plane", "gravity": 9.81}]
}
`)
	writeTestFile(t, filepath.Join(workspace, "internal", "physics", "solver.go"), "package physics\n\nfunc Solver() {}\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{
		Tools: []rlm.Tool{{Name: "gather_context", ReadOnly: true}},
	})
	out, err := adapter.Execute(context.Background(), "gather_context", mustJSON(map[string]any{
		"query":             "Which task-relevant test data defines physics scenarios?",
		"lanes":             []string{"code"},
		"source_profiles":   []string{"repo_code"},
		"required_evidence": []string{"physics scenarios"},
		"limit":             4,
		"max_context_chars": 3000,
		"response_mode":     "full",
	}))
	if err != nil {
		t.Fatalf("gather_context: %v", err)
	}
	body, _ := json.Marshal(out)
	var bundle contextengine.ContextBundle
	if err := json.Unmarshal(body, &bundle); err != nil {
		t.Fatalf("decode bundle: %v", err)
	}
	var selected *contextengine.ContextSelectedPath
	for i := range bundle.SelectedPaths {
		if bundle.SelectedPaths[i].Path == "tests_data/physics_scenarios.json" {
			selected = &bundle.SelectedPaths[i]
			break
		}
	}
	if selected == nil {
		t.Fatalf("selected paths=%v missing tests_data/physics_scenarios.json", selectedPathStringsForEnvTest(bundle.SelectedPaths))
	}
	if selected.Metadata["candidate_role"] != "test_data_support" {
		t.Fatalf("selected metadata=%v", selected.Metadata)
	}
	if selected.Metadata["source_profile"] != "repo_code" {
		t.Fatalf("selected metadata=%v", selected.Metadata)
	}
	if len(selected.CoverageIDs) == 0 {
		t.Fatalf("selected coverage ids missing: %#v", selected)
	}
}

func TestGatherContextPolyglotFactsPreserveMatchedProbeTerms(t *testing.T) {
	t.Parallel()

	workspace := filepath.Join("..", "..", "..", "testdata", "fixtures", "gather-context", "polyglot-repo")
	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{
		Tools: []rlm.Tool{{Name: "gather_context", ReadOnly: true}},
	})
	out, err := adapter.Execute(context.Background(), "gather_context", mustJSON(map[string]any{
		"query":             "Where is the deploy command declared, registered, and dispatched in the Go CLI fixture?",
		"task_type":         "registration_trace",
		"lanes":             []string{"code"},
		"source_profiles":   []string{"repo_code"},
		"required_evidence": []string{"RegisterDeployCommand", "RunDeployCommand", "Dispatch"},
		"limit":             10,
		"max_context_chars": 7000,
		"response_mode":     "full",
	}))
	if err != nil {
		t.Fatalf("gather_context: %v", err)
	}
	body, _ := json.Marshal(out)
	var bundle contextengine.ContextBundle
	if err := json.Unmarshal(body, &bundle); err != nil {
		t.Fatalf("decode bundle: %v", err)
	}
	var parts []string
	for _, fact := range bundle.Facts {
		parts = append(parts, fact.Fact)
		if len(fact.Metadata) > 0 {
			metadata, _ := json.Marshal(fact.Metadata)
			parts = append(parts, string(metadata))
		}
	}
	for _, evidence := range bundle.Evidence {
		parts = append(parts, evidence.Statement)
		if len(evidence.Metadata) > 0 {
			metadata, _ := json.Marshal(evidence.Metadata)
			parts = append(parts, string(metadata))
		}
	}
	text := strings.Join(parts, "\n")
	for _, want := range []string{"RegisterDeployCommand", "RunDeployCommand", "Dispatch"} {
		if !strings.Contains(text, want) {
			t.Fatalf("facts/evidence missing %s:\n%s", want, text)
		}
	}
}

func TestGatherContextSkipsExpensiveProvidersWhenCheapCoverageSatisfies(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "env", "tools.go"), "package env\n\nfunc DefaultTools() { _ = \"gather_context\" }\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "env", "tool_exec.go"), "package env\n\nfunc executeInternal() { _ = \"gather_context\" }\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "run_spec.go"), "package rlm\n\nfunc ResolveToolPolicy() { _ = \"gather_context\" }\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{
		Tools: []rlm.Tool{{Name: "gather_context", ReadOnly: true}},
	})
	out, err := adapter.Execute(context.Background(), "gather_context", mustJSON(map[string]any{
		"query":             "Where is gather_context declared, dispatched, and exposed through RLM tool policy?",
		"task_type":         "registration_trace",
		"lanes":             []string{"code"},
		"source_profiles":   []string{"repo_code"},
		"required_evidence": []string{"tools.go", "tool_exec.go", "run_spec.go"},
		"limit":             6,
		"response_mode":     "full",
	}))
	if err != nil {
		t.Fatalf("gather_context: %v", err)
	}
	body, _ := json.Marshal(out)
	var bundle contextengine.ContextBundle
	if err := json.Unmarshal(body, &bundle); err != nil {
		t.Fatalf("decode bundle: %v", err)
	}
	got := selectedPathStringsForEnvTest(bundle.SelectedPaths)
	for _, want := range []string{"internal/rlm/env/tools.go", "internal/rlm/env/tool_exec.go", "internal/rlm/run_spec.go"} {
		if !containsString(got, want) {
			t.Fatalf("selected paths=%v missing %s", got, want)
		}
	}
	telemetry := gatherContextProviderTelemetryForEnvTest(bundle)
	entry := telemetry["code_search_ensemble"]
	if entry == nil || entry["skipped"] != true {
		t.Fatalf("code_search_ensemble telemetry=%v all=%v", entry, telemetry)
	}
}

func TestGatherContextStructuredCoverageRequirementsFeedProviders(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "internal", "context", "contextengine", "context_gather.go"), "package contextengine\n\nfunc GatherContext() {}\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "context", "contextengine", "context_certify.go"), "package contextengine\n\nfunc CertifyContextBundle() {}\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{
		Tools: []rlm.Tool{{Name: "gather_context", ReadOnly: true}},
	})
	out, err := adapter.Execute(context.Background(), "gather_context", mustJSON(map[string]any{
		"query":           "Map the context subsystem.",
		"task_type":       "subsystem_map",
		"lanes":           []string{"code"},
		"source_profiles": []string{"repo_code"},
		"coverage_requirements": []map[string]any{
			{"id": "gatherer", "label": "contextengine GatherContext", "required": true},
			{"id": "certifier", "label": "contextengine CertifyContextBundle", "required": true},
		},
		"limit":             4,
		"max_context_chars": 5000,
		"response_mode":     "full",
	}))
	if err != nil {
		t.Fatalf("gather_context: %v", err)
	}
	body, _ := json.Marshal(out)
	var bundle contextengine.ContextBundle
	if err := json.Unmarshal(body, &bundle); err != nil {
		t.Fatalf("decode bundle: %v", err)
	}
	got := selectedPathStringsForEnvTest(bundle.SelectedPaths)
	for _, want := range []string{
		"internal/context/contextengine/context_gather.go",
		"internal/context/contextengine/context_certify.go",
	} {
		if !containsString(got, want) {
			t.Fatalf("selected paths=%v missing %s", got, want)
		}
	}
	if bundle.CoverageReport == nil || len(bundle.CoverageReport.Missing) > 0 {
		t.Fatalf("coverage report=%#v", bundle.CoverageReport)
	}
}

func codeSearchHitPaths(hits []rankedCodeSearchHit) []string {
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		out = append(out, hit.Hit.Path)
	}
	return out
}

func selectedPathStringsForEnvTest(paths []contextengine.ContextSelectedPath) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		out = append(out, path.Path)
	}
	return out
}

func gatherContextProviderTelemetryForEnvTest(bundle contextengine.ContextBundle) map[string]map[string]any {
	out := map[string]map[string]any{}
	groups, _ := bundle.Metadata["code_search_provider_telemetry"].([]any)
	for _, group := range groups {
		groupMap, _ := group.(map[string]any)
		providers, _ := groupMap["providers"].([]any)
		for _, provider := range providers {
			providerMap, _ := provider.(map[string]any)
			name, _ := providerMap["name"].(string)
			if name != "" {
				out[name] = providerMap
			}
		}
	}
	return out
}

func extractCodeSearchHitPaths(hits []contextengine.CodeSearchHit) []string {
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		out = append(out, hit.Path)
	}
	return out
}

func graphOutputHasPath(out map[string]any, path string) bool {
	nodes, _ := out["nodes"].([]any)
	for _, item := range nodes {
		node, _ := item.(map[string]any)
		if node["path"] == path {
			return true
		}
	}
	return false
}

func graphOutputHasEdgeType(out map[string]any, edgeType string) bool {
	edges, _ := out["edges"].([]any)
	for _, item := range edges {
		edge, _ := item.(map[string]any)
		if edge["type"] == edgeType {
			return true
		}
	}
	return false
}

func TestRankSemanticEmbeddingCacheEntriesAggregatesBestScorePerPath(t *testing.T) {
	entries := []semanticEmbeddingCacheEntry{
		{Path: "internal/a.go", Language: "go", Summary: "weak chunk", ChunkID: "a:0", Embedding: []float32{0, 1}},
		{Path: "internal/a.go", Language: "go", Summary: "strong chunk", ChunkID: "a:1", Embedding: []float32{1, 0}},
		{Path: "internal/b.go", Language: "go", Summary: "other file", ChunkID: "b:0", Embedding: []float32{0.5, 0.5}},
	}
	got := rankSemanticEmbeddingCacheEntries(entries, []float32{1, 0}, 8)
	if len(got) != 2 {
		t.Fatalf("ranked entry count = %d, want 2", len(got))
	}
	if got[0].Path != "internal/a.go" || got[0].ChunkID != "a:1" {
		t.Fatalf("top hit = %+v, want best chunk for internal/a.go", got[0])
	}
	if got[1].Path != "internal/b.go" {
		t.Fatalf("second hit = %+v, want internal/b.go", got[1])
	}
}

func TestGatherContextOmitsTestsByDefault(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "src", "billing.ts"), "export function chargeAccount() { return 'charged' }\n")
	writeTestFile(t, filepath.Join(workspace, "src", "billing.test.ts"), "import { chargeAccount } from './billing'\ntest('chargeAccount', () => chargeAccount())\n")
	writeTestFile(t, filepath.Join(workspace, "src", "ledger.ts"), "export function recordCharge() { return 'recorded' }\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{
		Tools: []rlm.Tool{{Name: "gather_context", ReadOnly: true}},
	})
	out, err := adapter.Execute(context.Background(), "gather_context", mustJSON(map[string]any{
		"query":             "Map the billing subsystem implementation and ledger flow.",
		"task_type":         "subsystem_map",
		"lanes":             []string{"code"},
		"source_profiles":   []string{"repo_code"},
		"required_evidence": []string{"billing implementation", "ledger flow"},
		"limit":             4,
		"response_mode":     "full",
	}))
	if err != nil {
		t.Fatalf("gather_context: %v", err)
	}
	body, _ := json.Marshal(out)
	var bundle contextengine.ContextBundle
	if err := json.Unmarshal(body, &bundle); err != nil {
		t.Fatalf("decode bundle: %v", err)
	}
	got := selectedPathStringsForEnvTest(bundle.SelectedPaths)
	if containsString(got, "src/billing.test.ts") {
		t.Fatalf("selected paths=%v should omit tests by default", got)
	}
	for _, want := range []string{"src/billing.ts", "src/ledger.ts"} {
		if !containsString(got, want) {
			t.Fatalf("selected paths=%v missing %s", got, want)
		}
	}
}

func TestGatherContextIncludesTestsWhenRequested(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "src", "billing.ts"), "export function chargeAccount() { return 'charged' }\n")
	writeTestFile(t, filepath.Join(workspace, "src", "billing.test.ts"), "import { chargeAccount } from './billing'\ntest('chargeAccount', () => chargeAccount())\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{
		Tools: []rlm.Tool{{Name: "gather_context", ReadOnly: true}},
	})
	out, err := adapter.Execute(context.Background(), "gather_context", mustJSON(map[string]any{
		"query":             "Map the billing implementation and tests.",
		"task_type":         "subsystem_map",
		"lanes":             []string{"code"},
		"source_profiles":   []string{"repo_code"},
		"required_evidence": []string{"billing implementation", "billing tests"},
		"limit":             4,
		"response_mode":     "full",
	}))
	if err != nil {
		t.Fatalf("gather_context: %v", err)
	}
	body, _ := json.Marshal(out)
	var bundle contextengine.ContextBundle
	if err := json.Unmarshal(body, &bundle); err != nil {
		t.Fatalf("decode bundle: %v", err)
	}
	got := selectedPathStringsForEnvTest(bundle.SelectedPaths)
	if !containsString(got, "src/billing.test.ts") {
		t.Fatalf("selected paths=%v want requested test file", got)
	}
}

func TestGatherTestContextIncludesTestsByDefault(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "src", "billing.ts"), "export function chargeAccount() { return 'charged' }\n")
	writeTestFile(t, filepath.Join(workspace, "src", "billing.test.ts"), "import { chargeAccount } from './billing'\ntest('chargeAccount', () => chargeAccount())\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{
		Tools: []rlm.Tool{{Name: "gather_test_context", ReadOnly: true}},
	})
	out, err := adapter.Execute(context.Background(), "gather_test_context", mustJSON(map[string]any{
		"query":         "Map the billing behavior.",
		"limit":         4,
		"response_mode": "full",
	}))
	if err != nil {
		t.Fatalf("gather_test_context: %v", err)
	}
	body, _ := json.Marshal(out)
	var bundle contextengine.ContextBundle
	if err := json.Unmarshal(body, &bundle); err != nil {
		t.Fatalf("decode bundle: %v", err)
	}
	got := selectedPathStringsForEnvTest(bundle.SelectedPaths)
	if !containsString(got, "src/billing.test.ts") {
		t.Fatalf("selected paths=%v want test surface to include tests", got)
	}
}

func TestGatherDocsContextUsesDocsProfileAndNoiseFilters(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "docs", "architecture.md"), "# Architecture\n\nBilling architecture notes.\n")
	writeTestFile(t, filepath.Join(workspace, "docs", "embedded", "package-lock.json"), `{"name":"embedded"}`)
	writeTestFile(t, filepath.Join(workspace, "src", "billing.ts"), "export function billingArchitecture() {}\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{
		Tools: []rlm.Tool{{Name: "gather_docs_context", ReadOnly: true}},
	})
	out, err := adapter.Execute(context.Background(), "gather_docs_context", mustJSON(map[string]any{
		"query":         "billing architecture",
		"limit":         4,
		"response_mode": "full",
	}))
	if err != nil {
		t.Fatalf("gather_docs_context: %v", err)
	}
	body, _ := json.Marshal(out)
	var bundle contextengine.ContextBundle
	if err := json.Unmarshal(body, &bundle); err != nil {
		t.Fatalf("decode bundle: %v", err)
	}
	got := selectedPathStringsForEnvTest(bundle.SelectedPaths)
	if !containsString(got, "docs/architecture.md") {
		t.Fatalf("selected paths=%v missing docs architecture", got)
	}
	if containsString(got, "docs/embedded/package-lock.json") || containsString(got, "src/billing.ts") {
		t.Fatalf("selected paths=%v should keep docs surface separate from package/code noise", got)
	}
}

func runGitForTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(output))
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
