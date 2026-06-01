package main

import (
	"path/filepath"
	"reflect"
	"testing"
	"testing/quick"
)

func TestDirectSkillForPath(t *testing.T) {
	if got := directSkillForPath("skills/todo/main.go"); got != "todo" {
		t.Fatalf("directSkillForPath() = %q, want todo", got)
	}
	if got := directSkillForPath("internal/foo/bar.go"); got != "" {
		t.Fatalf("directSkillForPath() = %q, want empty", got)
	}
}

func TestNearestPackageImport(t *testing.T) {
	repoRoot := filepath.ToSlash(filepath.Join(string(filepath.Separator), "repo"))
	dirToImport := map[string]string{
		filepath.ToSlash(filepath.Join(repoRoot, "internal", "foo")): "github.com/example/repo/internal/foo",
		filepath.ToSlash(filepath.Join(repoRoot, "skills", "todo")):  "github.com/example/repo/skills/todo",
	}

	got := nearestPackageImport("internal/foo/testdata/case.json", repoRoot, dirToImport)
	if got != "github.com/example/repo/internal/foo" {
		t.Fatalf("nearestPackageImport() = %q, want internal/foo import", got)
	}
}

func TestBuildImpactReportDirectAndDependency(t *testing.T) {
	allPkgs := []goListPackage{
		{ImportPath: "github.com/example/repo/internal/foo", Dir: filepath.ToSlash("/repo/internal/foo")},
		{ImportPath: "github.com/example/repo/internal/bar", Dir: filepath.ToSlash("/repo/internal/bar")},
		{ImportPath: "github.com/example/repo/skills/todo", Dir: filepath.ToSlash("/repo/skills/todo")},
		{ImportPath: "github.com/example/repo/skills/mailbox", Dir: filepath.ToSlash("/repo/skills/mailbox")},
	}
	skillPkgs := []goListPackage{
		{
			ImportPath: "github.com/example/repo/skills/todo",
			Dir:        filepath.ToSlash("/repo/skills/todo"),
			Deps:       []string{"github.com/example/repo/internal/foo"},
		},
		{
			ImportPath: "github.com/example/repo/skills/mailbox",
			Dir:        filepath.ToSlash("/repo/skills/mailbox"),
			Deps:       []string{"github.com/example/repo/internal/bar"},
		},
	}

	report := buildImpactReportWithRepoRoot("/repo", "origin/main", "HEAD", []string{
		"internal/foo/service.go",
		"skills/mailbox/skill.yaml",
	}, allPkgs, skillPkgs)
	got := make([]string, 0, len(report.Skills))
	for _, skill := range report.Skills {
		got = append(got, skill.Name)
	}
	want := []string{"mailbox", "todo"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("impacted skills = %v, want %v", got, want)
	}
}

func TestBuildImpactReportGlobalTriggerImpactsAllSkills(t *testing.T) {
	skillPkgs := []goListPackage{
		{ImportPath: "github.com/example/repo/skills/todo", Dir: filepath.ToSlash("/repo/skills/todo")},
		{ImportPath: "github.com/example/repo/skills/mailbox", Dir: filepath.ToSlash("/repo/skills/mailbox")},
	}
	report := buildImpactReportWithRepoRoot("/repo", "origin/main", "HEAD", []string{"go.mod"}, nil, skillPkgs)
	if len(report.Skills) != 2 {
		t.Fatalf("impacted skills = %d, want 2", len(report.Skills))
	}
}

func TestBuildImpactReportPackagesIncludesReverseDeps(t *testing.T) {
	allPkgs := []goListPackage{
		{ImportPath: "github.com/example/repo/internal/foo", Dir: filepath.ToSlash("/repo/internal/foo")},
		{ImportPath: "github.com/example/repo/internal/bar", Dir: filepath.ToSlash("/repo/internal/bar"), Deps: []string{"github.com/example/repo/internal/foo"}},
		{ImportPath: "github.com/example/repo/cmd/app", Dir: filepath.ToSlash("/repo/cmd/app"), Deps: []string{"github.com/example/repo/internal/bar", "github.com/example/repo/internal/foo"}},
	}
	report := buildImpactReportWithRepoRoot("/repo", "origin/main", "HEAD", []string{"internal/foo/service.go"}, allPkgs, nil)
	got := make([]string, 0, len(report.Packages))
	for _, pkg := range report.Packages {
		got = append(got, pkg.ImportPath)
	}
	want := []string{
		"github.com/example/repo/cmd/app",
		"github.com/example/repo/internal/bar",
		"github.com/example/repo/internal/foo",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("impacted packages = %v, want %v", got, want)
	}

	if !reflect.DeepEqual(report.ChangedPkgs, []string{"github.com/example/repo/internal/foo"}) {
		t.Fatalf("changed packages = %v, want direct foo package only", report.ChangedPkgs)
	}
}

func TestBuildImpactReportTestOnlyChangesDoNotFanOutReverseDeps(t *testing.T) {
	allPkgs := []goListPackage{
		{ImportPath: "github.com/example/repo/internal/foo", Dir: filepath.ToSlash("/repo/internal/foo")},
		{ImportPath: "github.com/example/repo/internal/bar", Dir: filepath.ToSlash("/repo/internal/bar"), Deps: []string{"github.com/example/repo/internal/foo"}},
		{ImportPath: "github.com/example/repo/cmd/app", Dir: filepath.ToSlash("/repo/cmd/app"), Deps: []string{"github.com/example/repo/internal/bar", "github.com/example/repo/internal/foo"}},
	}
	report := buildImpactReportWithRepoRoot("/repo", "origin/main", "HEAD", []string{"internal/foo/service_test.go"}, allPkgs, nil)

	got := packageImportPaths(report.Packages)
	want := []string{"github.com/example/repo/internal/foo"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("impacted packages = %v, want test-only package without reverse deps %v", got, want)
	}

	if !reflect.DeepEqual(report.ChangedPkgs, []string{"github.com/example/repo/internal/foo"}) {
		t.Fatalf("changed packages = %v, want direct foo package only", report.ChangedPkgs)
	}
}

func TestBuildImpactReportPackagesIncludesTestOnlyImports(t *testing.T) {
	allPkgs := []goListPackage{
		{ImportPath: "github.com/example/repo/internal/envelope", Dir: filepath.ToSlash("/repo/internal/envelope")},
		{
			ImportPath:  "github.com/example/repo/tests/integration",
			Dir:         filepath.ToSlash("/repo/tests/integration"),
			TestImports: []string{"github.com/example/repo/internal/envelope"},
		},
	}

	report := buildImpactReportWithRepoRoot("/repo", "origin/main", "HEAD", []string{"internal/envelope/envelope.go"}, allPkgs, nil)

	got := packageImportPaths(report.Packages)
	want := []string{
		"github.com/example/repo/internal/envelope",
		"github.com/example/repo/tests/integration",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("impacted packages = %v, want %v", got, want)
	}
}

func TestBuildImpactReportGlobalTriggerImpactsAllPackages(t *testing.T) {
	allPkgs := []goListPackage{
		{ImportPath: "github.com/example/repo/cmd/app", Dir: filepath.ToSlash("/repo/cmd/app")},
		{ImportPath: "github.com/example/repo/internal/foo", Dir: filepath.ToSlash("/repo/internal/foo")},
		{ImportPath: "github.com/example/repo/tests/integration", Dir: filepath.ToSlash("/repo/tests/integration")},
	}

	report := buildImpactReportWithRepoRoot("/repo", "origin/main", "HEAD", []string{"go.mod"}, allPkgs, nil)

	got := packageImportPaths(report.Packages)
	want := []string{
		"github.com/example/repo/cmd/app",
		"github.com/example/repo/internal/foo",
		"github.com/example/repo/tests/integration",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("impacted packages = %v, want %v", got, want)
	}
}

func TestPackageImpactIsOrderIndependentForTestImports(t *testing.T) {
	base := []goListPackage{
		{ImportPath: "github.com/example/repo/internal/envelope", Dir: filepath.ToSlash("/repo/internal/envelope")},
		{
			ImportPath:   "github.com/example/repo/internal/runner",
			Dir:          filepath.ToSlash("/repo/internal/runner"),
			Deps:         []string{"github.com/example/repo/internal/envelope"},
			TestImports:  []string{"github.com/example/repo/internal/envelope"},
			XTestImports: []string{"github.com/example/repo/internal/envelope"},
		},
		{
			ImportPath:  "github.com/example/repo/tests/integration",
			Dir:         filepath.ToSlash("/repo/tests/integration"),
			TestImports: []string{"github.com/example/repo/internal/runner", "github.com/example/repo/internal/envelope"},
		},
	}
	want := []string{
		"github.com/example/repo/internal/envelope",
		"github.com/example/repo/internal/runner",
		"github.com/example/repo/tests/integration",
	}

	check := func(rotation uint8, duplicate bool) bool {
		pkgs := rotatePackages(base, int(rotation))
		if duplicate {
			for i := range pkgs {
				if pkgs[i].ImportPath == "github.com/example/repo/tests/integration" {
					pkgs[i].TestImports = append(pkgs[i].TestImports, "github.com/example/repo/internal/envelope")
				}
			}
		}
		report := buildImpactReportWithRepoRoot("/repo", "origin/main", "HEAD", []string{"internal/envelope/envelope.go"}, pkgs, nil)
		return reflect.DeepEqual(packageImportPaths(report.Packages), want)
	}

	if err := quick.Check(check, &quick.Config{MaxCount: 64}); err != nil {
		t.Fatal(err)
	}
}

func packageImportPaths(pkgs []packageImpact) []string {
	out := make([]string, 0, len(pkgs))
	for _, pkg := range pkgs {
		out = append(out, pkg.ImportPath)
	}
	return out
}

func rotatePackages(pkgs []goListPackage, n int) []goListPackage {
	if len(pkgs) == 0 {
		return nil
	}
	n %= len(pkgs)
	out := append([]goListPackage(nil), pkgs[n:]...)
	out = append(out, pkgs[:n]...)
	return out
}
