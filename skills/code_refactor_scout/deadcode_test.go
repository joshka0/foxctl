package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	refscope "github.com/jkatigb/agentctl/internal/refactor/scope"
	refstatus "github.com/jkatigb/agentctl/internal/refactor/status"
)

func TestApplyFocusDeadFiltersToDeadFindings(t *testing.T) {
	items := []finding{
		{RuleID: "unreachable_private_symbol", Symbol: "deadHelper", Score: 84},
		{RuleID: "test_only_helper", Symbol: "testHelper", Score: 74},
		{RuleID: "stale_export_candidate", Symbol: "ExportedHelper", Score: 66},
		{RuleID: "orphan_file", File: "sample/dead.go", Score: 76},
		{RuleID: "test_only_file", File: "sample/test_helper.go", Score: 70},
		{RuleID: "stale_package_candidate", Symbol: "deadpkg", Score: 68},
		{RuleID: "test_only_package", Symbol: "testpkg", Score: 64},
		{RuleID: "high_cyclomatic_complexity", Symbol: "Complex", Score: 88},
	}

	got := applyFocus(items, "dead")
	if len(got) != 7 {
		t.Fatalf("len(got)=%d want 7 (%#v)", len(got), got)
	}
	for _, item := range got {
		if !isDeadFinding(item) {
			t.Fatalf("non-dead finding survived focus filter: %#v", item)
		}
	}
}

func TestBuildDeadCodeFindingsClassifiesCandidates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	storageRoot := t.TempDir()
	workspace := t.TempDir()

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatalf("open repoindex store: %v", err)
	}
	defer store.Close()

	pkg := "go:sample"
	now := time.Now().UTC()
	nodes := []repoindex.Node{
		{ID: repoindex.PackageID(store.RepoKey(), pkg), Kind: repoindex.NodePackage, Pkg: pkg, Name: "sample", UpdatedAt: now},
		{ID: repoindex.FileID(store.RepoKey(), pkg, "sample/dead.go"), Kind: repoindex.NodeFile, Pkg: pkg, File: "sample/dead.go", Name: "dead.go", SpanStart: 1, SpanEnd: 20, UpdatedAt: now},
		{ID: repoindex.FileID(store.RepoKey(), pkg, "sample/live.go"), Kind: repoindex.NodeFile, Pkg: pkg, File: "sample/live.go", Name: "live.go", SpanStart: 1, SpanEnd: 40, UpdatedAt: now},
		{ID: repoindex.FileID(store.RepoKey(), pkg, "sample/export.go"), Kind: repoindex.NodeFile, Pkg: pkg, File: "sample/export.go", Name: "export.go", SpanStart: 1, SpanEnd: 20, UpdatedAt: now},
		{ID: repoindex.FileID(store.RepoKey(), pkg, "sample/test_helper.go"), Kind: repoindex.NodeFile, Pkg: pkg, File: "sample/test_helper.go", Name: "test_helper.go", SpanStart: 1, SpanEnd: 20, UpdatedAt: now},
		{ID: repoindex.FileID(store.RepoKey(), pkg, "sample/test_helper_test.go"), Kind: repoindex.NodeFile, Pkg: pkg, File: "sample/test_helper_test.go", Name: "test_helper_test.go", SpanStart: 1, SpanEnd: 20, UpdatedAt: now},
		{ID: repoindex.SymbolID(store.RepoKey(), pkg, "deadHelper"), Kind: repoindex.NodeSymbol, Pkg: pkg, File: "sample/dead.go", Name: "deadHelper", Signature: "func deadHelper()", SpanStart: 10, SpanEnd: 12, UpdatedAt: now},
		{ID: repoindex.SymbolID(store.RepoKey(), pkg, "deadLeaf"), Kind: repoindex.NodeSymbol, Pkg: pkg, File: "sample/dead.go", Name: "deadLeaf", Signature: "func deadLeaf()", SpanStart: 14, SpanEnd: 16, UpdatedAt: now},
		{ID: repoindex.SymbolID(store.RepoKey(), pkg, "liveHelper"), Kind: repoindex.NodeSymbol, Pkg: pkg, File: "sample/live.go", Name: "liveHelper", Signature: "func liveHelper()", SpanStart: 10, SpanEnd: 12, UpdatedAt: now},
		{ID: repoindex.SymbolID(store.RepoKey(), pkg, "ExportedCaller"), Kind: repoindex.NodeSymbol, Pkg: pkg, File: "sample/live.go", Name: "ExportedCaller", Signature: "func ExportedCaller()", SpanStart: 26, SpanEnd: 30, Exported: true, UpdatedAt: now},
		{ID: repoindex.SymbolID(store.RepoKey(), pkg, "privateLiveLeaf"), Kind: repoindex.NodeSymbol, Pkg: pkg, File: "sample/live.go", Name: "privateLiveLeaf", Signature: "func privateLiveLeaf()", SpanStart: 32, SpanEnd: 34, UpdatedAt: now},
		{ID: repoindex.SymbolID(store.RepoKey(), pkg, "ExportedHelper"), Kind: repoindex.NodeSymbol, Pkg: pkg, File: "sample/export.go", Name: "ExportedHelper", Signature: "func ExportedHelper()", SpanStart: 10, SpanEnd: 14, Exported: true, UpdatedAt: now},
		{ID: repoindex.SymbolID(store.RepoKey(), pkg, "testHelper"), Kind: repoindex.NodeSymbol, Pkg: pkg, File: "sample/test_helper.go", Name: "testHelper", Signature: "func testHelper()", SpanStart: 10, SpanEnd: 12, UpdatedAt: now},
		{ID: repoindex.SymbolID(store.RepoKey(), pkg, "TestUsesHelper"), Kind: repoindex.NodeSymbol, Pkg: pkg, File: "sample/test_helper_test.go", Name: "TestUsesHelper", Signature: "func TestUsesHelper(t *testing.T)", SpanStart: 10, SpanEnd: 15, UpdatedAt: now},
		{ID: repoindex.PackageID(store.RepoKey(), "go:deadpkg"), Kind: repoindex.NodePackage, Pkg: "go:deadpkg", Name: "deadpkg", UpdatedAt: now},
		{ID: repoindex.FileID(store.RepoKey(), "go:deadpkg", "sample/deadpkg/orphan.go"), Kind: repoindex.NodeFile, Pkg: "go:deadpkg", File: "sample/deadpkg/orphan.go", Name: "orphan.go", SpanStart: 1, SpanEnd: 20, UpdatedAt: now},
		{ID: repoindex.SymbolID(store.RepoKey(), "go:deadpkg", "deadPkgHelper"), Kind: repoindex.NodeSymbol, Pkg: "go:deadpkg", File: "sample/deadpkg/orphan.go", Name: "deadPkgHelper", Signature: "func deadPkgHelper()", SpanStart: 10, SpanEnd: 12, UpdatedAt: now},
		{ID: repoindex.PackageID(store.RepoKey(), "go:testpkg"), Kind: repoindex.NodePackage, Pkg: "go:testpkg", Name: "testpkg", UpdatedAt: now},
		{ID: repoindex.FileID(store.RepoKey(), "go:testpkg", "sample/testpkg/helper.go"), Kind: repoindex.NodeFile, Pkg: "go:testpkg", File: "sample/testpkg/helper.go", Name: "helper.go", SpanStart: 1, SpanEnd: 20, UpdatedAt: now},
		{ID: repoindex.FileID(store.RepoKey(), "go:testpkg", "sample/testpkg/helper_test.go"), Kind: repoindex.NodeFile, Pkg: "go:testpkg", File: "sample/testpkg/helper_test.go", Name: "helper_test.go", SpanStart: 1, SpanEnd: 20, UpdatedAt: now},
		{ID: repoindex.SymbolID(store.RepoKey(), "go:testpkg", "testPkgHelper"), Kind: repoindex.NodeSymbol, Pkg: "go:testpkg", File: "sample/testpkg/helper.go", Name: "testPkgHelper", Signature: "func testPkgHelper()", SpanStart: 10, SpanEnd: 12, UpdatedAt: now},
		{ID: repoindex.SymbolID(store.RepoKey(), "go:testpkg", "TestPkgUsesHelper"), Kind: repoindex.NodeSymbol, Pkg: "go:testpkg", File: "sample/testpkg/helper_test.go", Name: "TestPkgUsesHelper", Signature: "func TestPkgUsesHelper(t *testing.T)", SpanStart: 10, SpanEnd: 15, UpdatedAt: now},
	}
	edges := []repoindex.Edge{
		{Src: repoindex.PackageID(store.RepoKey(), pkg), Dst: repoindex.FileID(store.RepoKey(), pkg, "sample/dead.go"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.PackageID(store.RepoKey(), pkg), Dst: repoindex.FileID(store.RepoKey(), pkg, "sample/live.go"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.PackageID(store.RepoKey(), pkg), Dst: repoindex.FileID(store.RepoKey(), pkg, "sample/export.go"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.PackageID(store.RepoKey(), pkg), Dst: repoindex.FileID(store.RepoKey(), pkg, "sample/test_helper.go"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.PackageID(store.RepoKey(), pkg), Dst: repoindex.FileID(store.RepoKey(), pkg, "sample/test_helper_test.go"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.FileID(store.RepoKey(), pkg, "sample/dead.go"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "deadHelper"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.FileID(store.RepoKey(), pkg, "sample/dead.go"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "deadLeaf"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.FileID(store.RepoKey(), pkg, "sample/live.go"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "liveHelper"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.FileID(store.RepoKey(), pkg, "sample/live.go"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "ExportedCaller"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.FileID(store.RepoKey(), pkg, "sample/live.go"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "privateLiveLeaf"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.FileID(store.RepoKey(), pkg, "sample/export.go"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "ExportedHelper"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.FileID(store.RepoKey(), pkg, "sample/test_helper.go"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "testHelper"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.FileID(store.RepoKey(), pkg, "sample/test_helper_test.go"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "TestUsesHelper"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.PackageID(store.RepoKey(), "go:deadpkg"), Dst: repoindex.FileID(store.RepoKey(), "go:deadpkg", "sample/deadpkg/orphan.go"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.FileID(store.RepoKey(), "go:deadpkg", "sample/deadpkg/orphan.go"), Dst: repoindex.SymbolID(store.RepoKey(), "go:deadpkg", "deadPkgHelper"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.PackageID(store.RepoKey(), "go:testpkg"), Dst: repoindex.FileID(store.RepoKey(), "go:testpkg", "sample/testpkg/helper.go"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.PackageID(store.RepoKey(), "go:testpkg"), Dst: repoindex.FileID(store.RepoKey(), "go:testpkg", "sample/testpkg/helper_test.go"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.FileID(store.RepoKey(), "go:testpkg", "sample/testpkg/helper.go"), Dst: repoindex.SymbolID(store.RepoKey(), "go:testpkg", "testPkgHelper"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.FileID(store.RepoKey(), "go:testpkg", "sample/testpkg/helper_test.go"), Dst: repoindex.SymbolID(store.RepoKey(), "go:testpkg", "TestPkgUsesHelper"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.SymbolID(store.RepoKey(), pkg, "deadHelper"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "deadLeaf"), Type: repoindex.EdgeCalls, Weight: 1.0},
		{Src: repoindex.SymbolID(store.RepoKey(), pkg, "ExportedCaller"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "liveHelper"), Type: repoindex.EdgeCalls, Weight: 1.0},
		{Src: repoindex.SymbolID(store.RepoKey(), pkg, "ExportedCaller"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "privateLiveLeaf"), Type: repoindex.EdgeCalls, Weight: 1.0},
		{Src: repoindex.SymbolID(store.RepoKey(), pkg, "TestUsesHelper"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "testHelper"), Type: repoindex.EdgeCalls, Weight: 1.0},
		{Src: repoindex.SymbolID(store.RepoKey(), "go:testpkg", "TestPkgUsesHelper"), Dst: repoindex.SymbolID(store.RepoKey(), "go:testpkg", "testPkgHelper"), Type: repoindex.EdgeCalls, Weight: 1.0},
		{Src: repoindex.FileID(store.RepoKey(), "go:testpkg", "sample/testpkg/helper_test.go"), Dst: repoindex.PackageID(store.RepoKey(), "go:testpkg"), Type: repoindex.EdgeTests, Weight: 1.0},
	}
	if err := store.ReplaceAll(ctx, nodes, edges); err != nil {
		t.Fatalf("replace repoindex graph: %v", err)
	}
	locators := []repoindex.LocatorEntry{
		{SymbolKey: "deadHelper", Pkg: pkg, FilePath: "sample/dead.go", Name: "deadHelper", Kind: "function", SpanStart: 10, SpanEnd: 12, UpdatedAt: now.Format(time.RFC3339Nano)},
		{SymbolKey: "deadLeaf", Pkg: pkg, FilePath: "sample/dead.go", Name: "deadLeaf", Kind: "function", SpanStart: 14, SpanEnd: 16, UpdatedAt: now.Format(time.RFC3339Nano)},
		{SymbolKey: "liveHelper", Pkg: pkg, FilePath: "sample/live.go", Name: "liveHelper", Kind: "function", SpanStart: 10, SpanEnd: 12, UpdatedAt: now.Format(time.RFC3339Nano)},
		{SymbolKey: "ExportedCaller", Pkg: pkg, FilePath: "sample/live.go", Name: "ExportedCaller", Kind: "function", Exported: true, SpanStart: 26, SpanEnd: 30, UpdatedAt: now.Format(time.RFC3339Nano)},
		{SymbolKey: "privateLiveLeaf", Pkg: pkg, FilePath: "sample/live.go", Name: "privateLiveLeaf", Kind: "function", SpanStart: 32, SpanEnd: 34, UpdatedAt: now.Format(time.RFC3339Nano)},
		{SymbolKey: "ExportedHelper", Pkg: pkg, FilePath: "sample/export.go", Name: "ExportedHelper", Kind: "function", Exported: true, SpanStart: 10, SpanEnd: 14, UpdatedAt: now.Format(time.RFC3339Nano)},
		{SymbolKey: "testHelper", Pkg: pkg, FilePath: "sample/test_helper.go", Name: "testHelper", Kind: "function", SpanStart: 10, SpanEnd: 12, UpdatedAt: now.Format(time.RFC3339Nano)},
		{SymbolKey: "TestUsesHelper", Pkg: pkg, FilePath: "sample/test_helper_test.go", Name: "TestUsesHelper", Kind: "function", SpanStart: 10, SpanEnd: 15, UpdatedAt: now.Format(time.RFC3339Nano)},
		{SymbolKey: "deadPkgHelper", Pkg: "go:deadpkg", FilePath: "sample/deadpkg/orphan.go", Name: "deadPkgHelper", Kind: "function", SpanStart: 10, SpanEnd: 12, UpdatedAt: now.Format(time.RFC3339Nano)},
		{SymbolKey: "testPkgHelper", Pkg: "go:testpkg", FilePath: "sample/testpkg/helper.go", Name: "testPkgHelper", Kind: "function", SpanStart: 10, SpanEnd: 12, UpdatedAt: now.Format(time.RFC3339Nano)},
		{SymbolKey: "TestPkgUsesHelper", Pkg: "go:testpkg", FilePath: "sample/testpkg/helper_test.go", Name: "TestPkgUsesHelper", Kind: "function", SpanStart: 10, SpanEnd: 15, UpdatedAt: now.Format(time.RFC3339Nano)},
	}
	for _, loc := range locators {
		if err := store.UpsertLocator(ctx, loc); err != nil {
			t.Fatalf("upsert locator: %v", err)
		}
	}

	scope := refscope.Scope{
		Workspace:    workspace,
		RepoRoot:     workspace,
		Path:         "sample",
		Absolute:     filepath.Join(workspace, "sample"),
		Mode:         "explicit",
		Language:     "go",
		Detected:     []string{"go"},
		IsDir:        true,
		IncludeTests: false,
	}
	status := refstatus.Status{Mode: refstatus.ModeIndexBacked}

	got, err := buildDeadCodeFindings(ctx, storageRoot, scope, status, "dead")
	if err != nil {
		t.Fatalf("buildDeadCodeFindings: %v", err)
	}

	bySymbol := make(map[string]string, len(got))
	for _, item := range got {
		bySymbol[item.Symbol] = item.RuleID
	}
	if bySymbol["deadHelper"] != "unreachable_private_symbol" {
		t.Fatalf("deadHelper rule=%q want unreachable_private_symbol (all=%#v)", bySymbol["deadHelper"], got)
	}
	if bySymbol["deadLeaf"] != "unreachable_private_symbol" {
		t.Fatalf("deadLeaf rule=%q want unreachable_private_symbol (all=%#v)", bySymbol["deadLeaf"], got)
	}
	if bySymbol["testHelper"] != "test_only_helper" {
		t.Fatalf("testHelper rule=%q want test_only_helper (all=%#v)", bySymbol["testHelper"], got)
	}
	if bySymbol["ExportedHelper"] != "stale_export_candidate" {
		t.Fatalf("ExportedHelper rule=%q want stale_export_candidate (all=%#v)", bySymbol["ExportedHelper"], got)
	}
	if bySymbol["dead.go"] != "orphan_file" {
		t.Fatalf("dead.go rule=%q want orphan_file (all=%#v)", bySymbol["dead.go"], got)
	}
	if bySymbol["test_helper.go"] != "test_only_file" {
		t.Fatalf("test_helper.go rule=%q want test_only_file (all=%#v)", bySymbol["test_helper.go"], got)
	}
	if bySymbol["deadpkg"] != "stale_package_candidate" {
		t.Fatalf("deadpkg rule=%q want stale_package_candidate (all=%#v)", bySymbol["deadpkg"], got)
	}
	if bySymbol["testpkg"] != "test_only_package" {
		t.Fatalf("testpkg rule=%q want test_only_package (all=%#v)", bySymbol["testpkg"], got)
	}
	if _, ok := bySymbol["liveHelper"]; ok {
		t.Fatalf("liveHelper should not be flagged: %#v", got)
	}
	if _, ok := bySymbol["privateLiveLeaf"]; ok {
		t.Fatalf("privateLiveLeaf should not be flagged: %#v", got)
	}
}

func TestBuildDeadCodeFindingsMarksDeadMethodChains(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	storageRoot := t.TempDir()
	workspace := t.TempDir()

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatalf("open repoindex store: %v", err)
	}
	defer store.Close()

	pkg := "go:sample"
	now := time.Now().UTC()
	nodes := []repoindex.Node{
		{ID: repoindex.PackageID(store.RepoKey(), pkg), Kind: repoindex.NodePackage, Pkg: pkg, Name: "sample", UpdatedAt: now},
		{ID: repoindex.FileID(store.RepoKey(), pkg, "sample/methods.go"), Kind: repoindex.NodeFile, Pkg: pkg, File: "sample/methods.go", Name: "methods.go", SpanStart: 1, SpanEnd: 30, UpdatedAt: now},
		{ID: repoindex.SymbolID(store.RepoKey(), pkg, "Agent.handle"), Kind: repoindex.NodeSymbol, Pkg: pkg, File: "sample/methods.go", Name: "Agent.handle", Signature: "func (a *Agent) handle()", SpanStart: 10, SpanEnd: 14, UpdatedAt: now},
		{ID: repoindex.SymbolID(store.RepoKey(), pkg, "methodLeaf"), Kind: repoindex.NodeSymbol, Pkg: pkg, File: "sample/methods.go", Name: "methodLeaf", Signature: "func methodLeaf()", SpanStart: 18, SpanEnd: 22, UpdatedAt: now},
	}
	edges := []repoindex.Edge{
		{Src: repoindex.PackageID(store.RepoKey(), pkg), Dst: repoindex.FileID(store.RepoKey(), pkg, "sample/methods.go"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.FileID(store.RepoKey(), pkg, "sample/methods.go"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "Agent.handle"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.FileID(store.RepoKey(), pkg, "sample/methods.go"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "methodLeaf"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.SymbolID(store.RepoKey(), pkg, "Agent.handle"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "methodLeaf"), Type: repoindex.EdgeCalls, Weight: 1.0},
	}
	if err := store.ReplaceAll(ctx, nodes, edges); err != nil {
		t.Fatalf("replace repoindex graph: %v", err)
	}
	locators := []repoindex.LocatorEntry{
		{SymbolKey: "Agent.handle", Pkg: pkg, FilePath: "sample/methods.go", Name: "Agent.handle", Kind: "method", SpanStart: 10, SpanEnd: 14, UpdatedAt: now.Format(time.RFC3339Nano)},
		{SymbolKey: "methodLeaf", Pkg: pkg, FilePath: "sample/methods.go", Name: "methodLeaf", Kind: "function", SpanStart: 18, SpanEnd: 22, UpdatedAt: now.Format(time.RFC3339Nano)},
	}
	for _, loc := range locators {
		if err := store.UpsertLocator(ctx, loc); err != nil {
			t.Fatalf("upsert locator: %v", err)
		}
	}

	scope := refscope.Scope{
		Workspace:    workspace,
		RepoRoot:     workspace,
		Path:         "sample",
		Absolute:     filepath.Join(workspace, "sample"),
		Mode:         "explicit",
		Language:     "go",
		Detected:     []string{"go"},
		IsDir:        true,
		IncludeTests: false,
	}
	status := refstatus.Status{Mode: refstatus.ModeIndexBacked}

	got, err := buildDeadCodeFindings(ctx, storageRoot, scope, status, "dead")
	if err != nil {
		t.Fatalf("buildDeadCodeFindings: %v", err)
	}

	bySymbol := make(map[string]string, len(got))
	for _, item := range got {
		bySymbol[item.Symbol] = item.RuleID
	}
	if bySymbol["Agent.handle"] != "unreachable_private_symbol" {
		t.Fatalf("Agent.handle rule=%q want unreachable_private_symbol (all=%#v)", bySymbol["Agent.handle"], got)
	}
	if bySymbol["methodLeaf"] != "unreachable_private_symbol" {
		t.Fatalf("methodLeaf rule=%q want unreachable_private_symbol (all=%#v)", bySymbol["methodLeaf"], got)
	}
}

func TestBuildDeadCodeFindingsKeepsInterfaceBackedMethodsLive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	storageRoot := t.TempDir()
	workspace := t.TempDir()

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatalf("open repoindex store: %v", err)
	}
	defer store.Close()

	pkg := "go:sample"
	now := time.Now().UTC()
	interfaceID := repoindex.NamespacedID(store.RepoKey(), repoindex.ConceptKeyword+"runner")
	nodes := []repoindex.Node{
		{ID: repoindex.PackageID(store.RepoKey(), pkg), Kind: repoindex.NodePackage, Pkg: pkg, Name: "sample", UpdatedAt: now},
		{ID: repoindex.FileID(store.RepoKey(), pkg, "sample/impl.go"), Kind: repoindex.NodeFile, Pkg: pkg, File: "sample/impl.go", Name: "impl.go", SpanStart: 1, SpanEnd: 30, UpdatedAt: now},
		{ID: repoindex.SymbolID(store.RepoKey(), pkg, "Directive.exec"), Kind: repoindex.NodeSymbol, Pkg: pkg, File: "sample/impl.go", Name: "Directive.exec", Signature: "func (d *Directive) exec()", SpanStart: 10, SpanEnd: 14, UpdatedAt: now},
		{ID: repoindex.SymbolID(store.RepoKey(), pkg, "execHelper"), Kind: repoindex.NodeSymbol, Pkg: pkg, File: "sample/impl.go", Name: "execHelper", Signature: "func execHelper()", SpanStart: 18, SpanEnd: 22, UpdatedAt: now},
		{ID: interfaceID, Kind: repoindex.NodeConcept, Name: "runner", UpdatedAt: now},
	}
	edges := []repoindex.Edge{
		{Src: repoindex.PackageID(store.RepoKey(), pkg), Dst: repoindex.FileID(store.RepoKey(), pkg, "sample/impl.go"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.FileID(store.RepoKey(), pkg, "sample/impl.go"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "Directive.exec"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.FileID(store.RepoKey(), pkg, "sample/impl.go"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "execHelper"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.SymbolID(store.RepoKey(), pkg, "Directive.exec"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "execHelper"), Type: repoindex.EdgeCalls, Weight: 1.0},
		{Src: repoindex.FileID(store.RepoKey(), pkg, "sample/impl.go"), Dst: interfaceID, Type: repoindex.EdgeImplements, Weight: 1.0},
	}
	if err := store.ReplaceAll(ctx, nodes, edges); err != nil {
		t.Fatalf("replace repoindex graph: %v", err)
	}
	locators := []repoindex.LocatorEntry{
		{SymbolKey: "Directive.exec", Pkg: pkg, FilePath: "sample/impl.go", Name: "Directive.exec", Kind: "method", SpanStart: 10, SpanEnd: 14, UpdatedAt: now.Format(time.RFC3339Nano)},
		{SymbolKey: "execHelper", Pkg: pkg, FilePath: "sample/impl.go", Name: "execHelper", Kind: "function", SpanStart: 18, SpanEnd: 22, UpdatedAt: now.Format(time.RFC3339Nano)},
	}
	for _, loc := range locators {
		if err := store.UpsertLocator(ctx, loc); err != nil {
			t.Fatalf("upsert locator: %v", err)
		}
	}

	scope := refscope.Scope{
		Workspace:    workspace,
		RepoRoot:     workspace,
		Path:         "sample",
		Absolute:     filepath.Join(workspace, "sample"),
		Mode:         "explicit",
		Language:     "go",
		Detected:     []string{"go"},
		IsDir:        true,
		IncludeTests: false,
	}
	status := refstatus.Status{Mode: refstatus.ModeIndexBacked}

	got, err := buildDeadCodeFindings(ctx, storageRoot, scope, status, "dead")
	if err != nil {
		t.Fatalf("buildDeadCodeFindings: %v", err)
	}

	for _, item := range got {
		if item.Symbol == "Directive.exec" || item.Symbol == "execHelper" {
			t.Fatalf("interface-backed method chain should not be flagged: %#v", got)
		}
	}
}

func TestBuildDeadCodeFindingsKeepsFileRootRegisteredHelpersLive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	storageRoot := t.TempDir()
	workspace := t.TempDir()

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatalf("open repoindex store: %v", err)
	}
	defer store.Close()

	pkg := "go:sample"
	now := time.Now().UTC()
	nodes := []repoindex.Node{
		{ID: repoindex.PackageID(store.RepoKey(), pkg), Kind: repoindex.NodePackage, Pkg: pkg, Name: "sample", UpdatedAt: now},
		{ID: repoindex.FileID(store.RepoKey(), pkg, "sample/routes.go"), Kind: repoindex.NodeFile, Pkg: pkg, File: "sample/routes.go", Name: "routes.go", SpanStart: 1, SpanEnd: 30, UpdatedAt: now},
		{ID: repoindex.SymbolID(store.RepoKey(), pkg, "routes.go/registeredHandler"), Kind: repoindex.NodeSymbol, Pkg: pkg, File: "sample/routes.go", Name: "registeredHandler", Signature: "func registeredHandler()", SpanStart: 10, SpanEnd: 14, UpdatedAt: now},
		{ID: repoindex.SymbolID(store.RepoKey(), pkg, "routes.go/registeredLeaf"), Kind: repoindex.NodeSymbol, Pkg: pkg, File: "sample/routes.go", Name: "registeredLeaf", Signature: "func registeredLeaf()", SpanStart: 18, SpanEnd: 22, UpdatedAt: now},
	}
	edges := []repoindex.Edge{
		{Src: repoindex.PackageID(store.RepoKey(), pkg), Dst: repoindex.FileID(store.RepoKey(), pkg, "sample/routes.go"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.FileID(store.RepoKey(), pkg, "sample/routes.go"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "routes.go/registeredHandler"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.FileID(store.RepoKey(), pkg, "sample/routes.go"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "routes.go/registeredLeaf"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.FileID(store.RepoKey(), pkg, "sample/routes.go"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "routes.go/registeredHandler"), Type: repoindex.EdgeRefersTo, Weight: 1.0},
		{Src: repoindex.SymbolID(store.RepoKey(), pkg, "routes.go/registeredHandler"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "routes.go/registeredLeaf"), Type: repoindex.EdgeCalls, Weight: 1.0},
	}
	if err := store.ReplaceAll(ctx, nodes, edges); err != nil {
		t.Fatalf("replace repoindex graph: %v", err)
	}
	locators := []repoindex.LocatorEntry{
		{SymbolKey: "routes.go/registeredHandler", Pkg: pkg, FilePath: "sample/routes.go", Name: "registeredHandler", Kind: "function", SpanStart: 10, SpanEnd: 14, UpdatedAt: now.Format(time.RFC3339Nano)},
		{SymbolKey: "routes.go/registeredLeaf", Pkg: pkg, FilePath: "sample/routes.go", Name: "registeredLeaf", Kind: "function", SpanStart: 18, SpanEnd: 22, UpdatedAt: now.Format(time.RFC3339Nano)},
	}
	for _, loc := range locators {
		if err := store.UpsertLocator(ctx, loc); err != nil {
			t.Fatalf("upsert locator: %v", err)
		}
	}

	scope := refscope.Scope{
		Workspace:    workspace,
		RepoRoot:     workspace,
		Path:         "sample",
		Absolute:     filepath.Join(workspace, "sample"),
		Mode:         "explicit",
		Language:     "go",
		Detected:     []string{"go"},
		IsDir:        true,
		IncludeTests: false,
	}
	status := refstatus.Status{Mode: refstatus.ModeIndexBacked}

	got, err := buildDeadCodeFindings(ctx, storageRoot, scope, status, "dead")
	if err != nil {
		t.Fatalf("buildDeadCodeFindings: %v", err)
	}

	for _, item := range got {
		if item.Symbol == "registeredHandler" || item.Symbol == "registeredLeaf" {
			t.Fatalf("file-root registered helper chain should not be flagged: %#v", got)
		}
	}
}

func TestBuildDeadCodeFindingsKeepsElixirApplicationCallbacksLive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	storageRoot := t.TempDir()
	workspace := t.TempDir()

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatalf("open repoindex store: %v", err)
	}
	defer store.Close()

	pkg := "ex:lib/praze_presence"
	now := time.Now().UTC()
	applicationPkgID := repoindex.PackageID(store.RepoKey(), "ex:Application")
	nodes := []repoindex.Node{
		{ID: repoindex.PackageID(store.RepoKey(), pkg), Kind: repoindex.NodePackage, Pkg: pkg, Name: "lib/praze_presence", UpdatedAt: now},
		{ID: repoindex.FileID(store.RepoKey(), pkg, "lib/praze_presence/application.ex"), Kind: repoindex.NodeFile, Pkg: pkg, File: "lib/praze_presence/application.ex", Name: "application.ex", SpanStart: 1, SpanEnd: 40, UpdatedAt: now},
		{ID: repoindex.SymbolID(store.RepoKey(), pkg, "start"), Kind: repoindex.NodeSymbol, Pkg: pkg, File: "lib/praze_presence/application.ex", Name: "start", Signature: "def start(_type, _args) do", SpanStart: 10, SpanEnd: 20, Exported: true, UpdatedAt: now},
		{ID: repoindex.SymbolID(store.RepoKey(), pkg, "maybe_add_metrics"), Kind: repoindex.NodeSymbol, Pkg: pkg, File: "lib/praze_presence/application.ex", Name: "maybe_add_metrics", Signature: "defp maybe_add_metrics(children) do", SpanStart: 22, SpanEnd: 30, UpdatedAt: now},
		{ID: applicationPkgID, Kind: repoindex.NodePackage, Pkg: "ex:Application", Name: "Application", UpdatedAt: now},
	}
	edges := []repoindex.Edge{
		{Src: repoindex.PackageID(store.RepoKey(), pkg), Dst: repoindex.FileID(store.RepoKey(), pkg, "lib/praze_presence/application.ex"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.FileID(store.RepoKey(), pkg, "lib/praze_presence/application.ex"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "start"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.FileID(store.RepoKey(), pkg, "lib/praze_presence/application.ex"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "maybe_add_metrics"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.SymbolID(store.RepoKey(), pkg, "start"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "maybe_add_metrics"), Type: repoindex.EdgeCalls, Weight: 1.0},
		{Src: repoindex.FileID(store.RepoKey(), pkg, "lib/praze_presence/application.ex"), Dst: applicationPkgID, Type: repoindex.EdgeRefersTo, Weight: 1.0},
	}
	if err := store.ReplaceAll(ctx, nodes, edges); err != nil {
		t.Fatalf("replace repoindex graph: %v", err)
	}
	locators := []repoindex.LocatorEntry{
		{SymbolKey: "start", Pkg: pkg, FilePath: "lib/praze_presence/application.ex", Name: "start", Kind: "function", Exported: true, SpanStart: 10, SpanEnd: 20, UpdatedAt: now.Format(time.RFC3339Nano)},
		{SymbolKey: "maybe_add_metrics", Pkg: pkg, FilePath: "lib/praze_presence/application.ex", Name: "maybe_add_metrics", Kind: "function", SpanStart: 22, SpanEnd: 30, UpdatedAt: now.Format(time.RFC3339Nano)},
	}
	for _, loc := range locators {
		if err := store.UpsertLocator(ctx, loc); err != nil {
			t.Fatalf("upsert locator: %v", err)
		}
	}

	scope := refscope.Scope{
		Workspace:    workspace,
		RepoRoot:     workspace,
		Path:         "lib",
		Absolute:     filepath.Join(workspace, "lib"),
		Mode:         "explicit",
		Language:     "elixir",
		Detected:     []string{"elixir"},
		IsDir:        true,
		IncludeTests: false,
	}
	status := refstatus.Status{Mode: refstatus.ModeIndexBacked}

	got, err := buildDeadCodeFindings(ctx, storageRoot, scope, status, "dead")
	if err != nil {
		t.Fatalf("buildDeadCodeFindings: %v", err)
	}

	for _, item := range got {
		if item.Symbol == "start" || item.Symbol == "maybe_add_metrics" {
			t.Fatalf("application callback chain should stay live: %#v", got)
		}
	}
}

func TestBuildDeadCodeFindingsDoesNotMisclassifyElixirTypeAsFunction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	storageRoot := t.TempDir()
	workspace := t.TempDir()

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatalf("open repoindex store: %v", err)
	}
	defer store.Close()

	pkg := "ex:lib/praze_presence/auth"
	now := time.Now().UTC()
	nodes := []repoindex.Node{
		{ID: repoindex.PackageID(store.RepoKey(), pkg), Kind: repoindex.NodePackage, Pkg: pkg, Name: "lib/praze_presence/auth", UpdatedAt: now},
		{ID: repoindex.FileID(store.RepoKey(), pkg, "lib/praze_presence/auth/jwt_verifier.ex"), Kind: repoindex.NodeFile, Pkg: pkg, File: "lib/praze_presence/auth/jwt_verifier.ex", Name: "jwt_verifier.ex", SpanStart: 1, SpanEnd: 60, UpdatedAt: now},
		{ID: repoindex.SymbolID(store.RepoKey(), pkg, "config:type"), Kind: repoindex.NodeSymbol, Pkg: pkg, File: "lib/praze_presence/auth/jwt_verifier.ex", Name: "config", Signature: "@type config :: %{jwks_url: String.t()}", SpanStart: 8, SpanEnd: 8, Exported: true, UpdatedAt: now},
		{ID: repoindex.SymbolID(store.RepoKey(), pkg, "verify_token"), Kind: repoindex.NodeSymbol, Pkg: pkg, File: "lib/praze_presence/auth/jwt_verifier.ex", Name: "verify_token", Signature: "def verify_token(token) do", SpanStart: 12, SpanEnd: 20, Exported: true, UpdatedAt: now},
		{ID: repoindex.SymbolID(store.RepoKey(), pkg, "config:function"), Kind: repoindex.NodeSymbol, Pkg: pkg, File: "lib/praze_presence/auth/jwt_verifier.ex", Name: "config", Signature: "defp config do", SpanStart: 24, SpanEnd: 28, UpdatedAt: now},
	}
	edges := []repoindex.Edge{
		{Src: repoindex.PackageID(store.RepoKey(), pkg), Dst: repoindex.FileID(store.RepoKey(), pkg, "lib/praze_presence/auth/jwt_verifier.ex"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.FileID(store.RepoKey(), pkg, "lib/praze_presence/auth/jwt_verifier.ex"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "config:type"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.FileID(store.RepoKey(), pkg, "lib/praze_presence/auth/jwt_verifier.ex"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "verify_token"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.FileID(store.RepoKey(), pkg, "lib/praze_presence/auth/jwt_verifier.ex"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "config:function"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.SymbolID(store.RepoKey(), pkg, "verify_token"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "config:function"), Type: repoindex.EdgeCalls, Weight: 1.0},
	}
	if err := store.ReplaceAll(ctx, nodes, edges); err != nil {
		t.Fatalf("replace repoindex graph: %v", err)
	}
	locators := []repoindex.LocatorEntry{
		{SymbolKey: "verify_token", Pkg: pkg, FilePath: "lib/praze_presence/auth/jwt_verifier.ex", Name: "verify_token", Kind: "function", Exported: true, SpanStart: 12, SpanEnd: 20, UpdatedAt: now.Format(time.RFC3339Nano)},
		{SymbolKey: "config:function", Pkg: pkg, FilePath: "lib/praze_presence/auth/jwt_verifier.ex", Name: "config", Kind: "function", SpanStart: 24, SpanEnd: 28, UpdatedAt: now.Format(time.RFC3339Nano)},
	}
	for _, loc := range locators {
		if err := store.UpsertLocator(ctx, loc); err != nil {
			t.Fatalf("upsert locator: %v", err)
		}
	}

	scope := refscope.Scope{
		Workspace:    workspace,
		RepoRoot:     workspace,
		Path:         "lib",
		Absolute:     filepath.Join(workspace, "lib"),
		Mode:         "explicit",
		Language:     "elixir",
		Detected:     []string{"elixir"},
		IsDir:        true,
		IncludeTests: false,
	}
	status := refstatus.Status{Mode: refstatus.ModeIndexBacked}

	got, err := buildDeadCodeFindings(ctx, storageRoot, scope, status, "dead")
	if err != nil {
		t.Fatalf("buildDeadCodeFindings: %v", err)
	}

	for _, item := range got {
		if item.Symbol == "config" {
			t.Fatalf("type/function name collision should not produce config dead-code finding: %#v", got)
		}
	}
}

func TestBuildDeadCodeFindingsKeepsTypeScriptReducerHelpersLive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	storageRoot := t.TempDir()
	workspace := t.TempDir()

	if err := os.WriteFile(filepath.Join(workspace, "package.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "src"), 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}

	source := `import { useReducer } from "react"

type State = { count: number }
type Action = { type: "reset" }

const INITIAL_STATE: State = { count: 0 }

function authPromptReducer(state: State, action: Action): State {
  switch (action.type) {
    case "reset":
      return INITIAL_STATE
  }
}

export function useAuthPromptController() {
  const [state, dispatch] = useReducer(authPromptReducer, INITIAL_STATE)
  return { state, dispatch }
}
`
	if err := os.WriteFile(filepath.Join(workspace, "src", "controller.ts"), []byte(source), 0o644); err != nil {
		t.Fatalf("write controller.ts: %v", err)
	}

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatalf("open repoindex store: %v", err)
	}
	defer store.Close()

	builder := repoindex.NewBuilder(store, workspace)
	if _, err := builder.Build(ctx, repoindex.BuildOptions{
		RepoRoot:          workspace,
		IncludeGo:         false,
		IncludeTypescript: true,
		IncludeElixir:     false,
	}); err != nil {
		t.Fatalf("build repoindex graph: %v", err)
	}

	scope := refscope.Scope{
		Workspace:    workspace,
		RepoRoot:     workspace,
		Path:         "src",
		Absolute:     filepath.Join(workspace, "src"),
		Mode:         "explicit",
		Language:     "typescript",
		Detected:     []string{"typescript"},
		IsDir:        true,
		IncludeTests: false,
	}
	status := refstatus.Status{Mode: refstatus.ModeIndexBacked}

	got, err := buildDeadCodeFindings(ctx, storageRoot, scope, status, "dead")
	if err != nil {
		t.Fatalf("buildDeadCodeFindings: %v", err)
	}

	for _, item := range got {
		if item.Symbol == "authPromptReducer" {
			t.Fatalf("reducer helper should stay live when referenced by exported hook: %#v", got)
		}
	}
}

func TestBuildDeadCodeFindingsSuppressesChildDeadFamiliesInBroadRuns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	storageRoot := t.TempDir()
	workspace := t.TempDir()

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatalf("open repoindex store: %v", err)
	}
	defer store.Close()

	pkg := "go:sample"
	now := time.Now().UTC()
	nodes := []repoindex.Node{
		{ID: repoindex.PackageID(store.RepoKey(), pkg), Kind: repoindex.NodePackage, Pkg: pkg, Name: "sample", UpdatedAt: now},
		{ID: repoindex.FileID(store.RepoKey(), pkg, "sample/dead.go"), Kind: repoindex.NodeFile, Pkg: pkg, File: "sample/dead.go", Name: "dead.go", SpanStart: 1, SpanEnd: 20, UpdatedAt: now},
		{ID: repoindex.SymbolID(store.RepoKey(), pkg, "deadHelper"), Kind: repoindex.NodeSymbol, Pkg: pkg, File: "sample/dead.go", Name: "deadHelper", Signature: "func deadHelper()", SpanStart: 10, SpanEnd: 12, UpdatedAt: now},
		{ID: repoindex.SymbolID(store.RepoKey(), pkg, "deadLeaf"), Kind: repoindex.NodeSymbol, Pkg: pkg, File: "sample/dead.go", Name: "deadLeaf", Signature: "func deadLeaf()", SpanStart: 14, SpanEnd: 16, UpdatedAt: now},
	}
	edges := []repoindex.Edge{
		{Src: repoindex.PackageID(store.RepoKey(), pkg), Dst: repoindex.FileID(store.RepoKey(), pkg, "sample/dead.go"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.FileID(store.RepoKey(), pkg, "sample/dead.go"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "deadHelper"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.FileID(store.RepoKey(), pkg, "sample/dead.go"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "deadLeaf"), Type: repoindex.EdgeContains, Weight: 1.0},
		{Src: repoindex.SymbolID(store.RepoKey(), pkg, "deadHelper"), Dst: repoindex.SymbolID(store.RepoKey(), pkg, "deadLeaf"), Type: repoindex.EdgeCalls, Weight: 1.0},
	}
	if err := store.ReplaceAll(ctx, nodes, edges); err != nil {
		t.Fatalf("replace repoindex graph: %v", err)
	}
	locators := []repoindex.LocatorEntry{
		{SymbolKey: "deadHelper", Pkg: pkg, FilePath: "sample/dead.go", Name: "deadHelper", Kind: "function", SpanStart: 10, SpanEnd: 12, UpdatedAt: now.Format(time.RFC3339Nano)},
		{SymbolKey: "deadLeaf", Pkg: pkg, FilePath: "sample/dead.go", Name: "deadLeaf", Kind: "function", SpanStart: 14, SpanEnd: 16, UpdatedAt: now.Format(time.RFC3339Nano)},
	}
	for _, loc := range locators {
		if err := store.UpsertLocator(ctx, loc); err != nil {
			t.Fatalf("upsert locator: %v", err)
		}
	}

	scope := refscope.Scope{
		Workspace:    workspace,
		RepoRoot:     workspace,
		Path:         "sample",
		Absolute:     filepath.Join(workspace, "sample"),
		Mode:         "explicit",
		Language:     "go",
		Detected:     []string{"go"},
		IsDir:        true,
		IncludeTests: false,
	}
	status := refstatus.Status{Mode: refstatus.ModeIndexBacked}

	got, err := buildDeadCodeFindings(ctx, storageRoot, scope, status, "all")
	if err != nil {
		t.Fatalf("buildDeadCodeFindings: %v", err)
	}

	seen := make(map[string]string, len(got))
	for _, item := range got {
		seen[item.Symbol] = item.RuleID
	}
	if seen["sample"] != "stale_package_candidate" {
		t.Fatalf("expected package-level summary, got %#v", got)
	}
	if _, ok := seen["dead.go"]; ok {
		t.Fatalf("dead.go should be suppressed once the package summary exists: %#v", got)
	}
	if _, ok := seen["deadHelper"]; ok {
		t.Fatalf("deadHelper should be suppressed in broad runs: %#v", got)
	}
	if _, ok := seen["deadLeaf"]; ok {
		t.Fatalf("deadLeaf should be suppressed in broad runs: %#v", got)
	}
}
