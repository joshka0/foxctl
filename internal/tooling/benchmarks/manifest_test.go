package benchmarks

import (
	"slices"
	"strings"
	"testing"
)

func TestDefaultManifestIsValidAndCoversAllCategories(t *testing.T) {
	t.Parallel()

	manifest, path, err := LoadManifest("")
	if err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}
	if !strings.HasSuffix(path, DefaultManifestPath) {
		t.Fatalf("path=%q want suffix %q", path, DefaultManifestPath)
	}
	if err := ValidateManifest(manifest); err != nil {
		t.Fatalf("ValidateManifest() error = %v", err)
	}
	want := []string{"dag", "integrations", "repoindex", "retrieval", "rlm", "room", "runtime"}
	got := SortedCategoryIDs(manifest)
	if !slices.Equal(got, want) {
		t.Fatalf("categories=%v want %v", got, want)
	}
}

func TestValidateManifestRejectsUnsafeDefaultGate(t *testing.T) {
	t.Parallel()

	manifest := minimalManifest()
	manifest.Benchmarks[0].RequiresNetwork = true

	err := ValidateManifest(manifest)
	if err == nil {
		t.Fatal("ValidateManifest() error = nil, want unsafe default gate error")
	}
	if !strings.Contains(err.Error(), "cannot require network in the default gate") {
		t.Fatalf("ValidateManifest() error = %v", err)
	}
}

func TestValidateManifestRejectsMissingMetricsAndArtifacts(t *testing.T) {
	t.Parallel()

	manifest := minimalManifest()
	manifest.Benchmarks[0].Metrics = nil

	err := ValidateManifest(manifest)
	if err == nil {
		t.Fatal("ValidateManifest() error = nil, want missing metrics error")
	}
	if !strings.Contains(err.Error(), "must define metrics") {
		t.Fatalf("ValidateManifest() error = %v", err)
	}

	manifest = minimalManifest()
	manifest.Benchmarks[0].Artifacts = nil
	err = ValidateManifest(manifest)
	if err == nil {
		t.Fatal("ValidateManifest() error = nil, want missing artifacts error")
	}
	if !strings.Contains(err.Error(), "must define artifacts") {
		t.Fatalf("ValidateManifest() error = %v", err)
	}
}

func TestValidateManifestRejectsDuplicateIDsAndUnknownCategories(t *testing.T) {
	t.Parallel()

	manifest := minimalManifest()
	manifest.Benchmarks = append(manifest.Benchmarks, manifest.Benchmarks[0])
	if err := ValidateManifest(manifest); err == nil || !strings.Contains(err.Error(), "duplicate benchmark id") {
		t.Fatalf("ValidateManifest() error = %v, want duplicate benchmark id", err)
	}

	manifest = minimalManifest()
	manifest.Benchmarks[0].Category = "missing"
	if err := ValidateManifest(manifest); err == nil || !strings.Contains(err.Error(), "unknown category") {
		t.Fatalf("ValidateManifest() error = %v, want unknown category", err)
	}
}

func TestGoBenchmarkPackagesFiltersByGateAndDedupesInManifestOrder(t *testing.T) {
	t.Parallel()

	manifest := minimalManifest()
	manifest.Benchmarks = append(manifest.Benchmarks,
		BenchmarkSpec{
			ID:          "runtime.second",
			Category:    "runtime",
			Status:      "implemented",
			Runner:      "go-test-bench",
			DefaultGate: true,
			Commands:    []string{"go test -bench BenchmarkSecond ./internal/runtime/engine"},
			Packages:    []string{"./internal/runtime/engine", "./internal/runtime/execution/exec"},
			Fixtures:    []string{"fake tool executor"},
			Metrics:     []string{"ns/op"},
			Artifacts:   []string{"bench output"},
		},
		BenchmarkSpec{
			ID:           "runtime.extended",
			Category:     "runtime",
			Status:       "experimental",
			Runner:       "go-test-bench",
			ExtendedGate: true,
			Commands:     []string{"go test -bench BenchmarkExtended ./internal/runtime/actor"},
			Packages:     []string{"./internal/runtime/actor"},
			Fixtures:     []string{"actor lifecycle"},
			Metrics:      []string{"ns/op"},
			Artifacts:    []string{"bench output"},
		},
	)

	got := GoBenchmarkPackages(manifest, GateDefault)
	want := []string{"./internal/runtime/execution/exec", "./internal/runtime/engine"}
	if !slices.Equal(got, want) {
		t.Fatalf("GoBenchmarkPackages(default)=%v want %v", got, want)
	}

	got = GoBenchmarkPackages(manifest, GateExtended)
	want = []string{"./internal/runtime/actor"}
	if !slices.Equal(got, want) {
		t.Fatalf("GoBenchmarkPackages(extended)=%v want %v", got, want)
	}
}

func minimalManifest() Manifest {
	return Manifest{
		Version: 1,
		Suite:   "test",
		Categories: []Category{
			{
				ID:     "runtime",
				Name:   "Runtime",
				Status: "implemented",
			},
		},
		Benchmarks: []BenchmarkSpec{
			{
				ID:          "runtime.exec",
				Category:    "runtime",
				Status:      "implemented",
				Runner:      "go-test-bench",
				DefaultGate: true,
				Commands:    []string{"go test -bench BenchmarkBufferPooling ./internal/runtime/execution/exec"},
				Packages:    []string{"./internal/runtime/execution/exec"},
				Fixtures:    []string{"synthetic execution payload"},
				Metrics:     []string{"ns/op", "B/op", "allocs/op"},
				Artifacts:   []string{"bench output"},
			},
		},
	}
}
