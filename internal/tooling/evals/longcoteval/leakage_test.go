package longcoteval

import "testing"

func TestAssessLeakageEmptyToolListIsClean(t *testing.T) {
	t.Parallel()

	flags := AssessLeakage(nil, LeakageOptions{})
	if flags.Leaked() {
		t.Fatalf("expected clean flags: %+v", flags)
	}
}

func TestAssessLeakageMarksForbiddenTools(t *testing.T) {
	t.Parallel()

	flags := AssessLeakage([]string{"load_file", "search_vault", "subcall", "rlm_query", "load_file"}, LeakageOptions{})
	if !flags.Leaked() {
		t.Fatalf("expected leaked flags: %+v", flags)
	}
	if !flags.FilesystemEnabled || !flags.VaultEnabled || !flags.SubcallEnabled {
		t.Fatalf("missing category flags: %+v", flags)
	}
	want := []string{"load_file", "rlm_query", "search_vault", "subcall"}
	if len(flags.ForbiddenToolNames) != len(want) {
		t.Fatalf("forbidden=%v want %v", flags.ForbiddenToolNames, want)
	}
	for i := range want {
		if flags.ForbiddenToolNames[i] != want[i] {
			t.Fatalf("forbidden=%v want %v", flags.ForbiddenToolNames, want)
		}
	}
}

func TestAssessLeakageAllowsExplicitRecursiveSubcalls(t *testing.T) {
	t.Parallel()

	flags := AssessLeakage([]string{"python_repl", "rlm_query", "rlm_wait", "rlm_result"}, LeakageOptions{
		SubcallsAllowed: true,
	})
	if flags.Leaked() {
		t.Fatalf("expected explicit recursive condition to be clean: %+v", flags)
	}
	if !flags.SubcallEnabled || !flags.SubcallAllowed {
		t.Fatalf("expected allowed subcall fields: %+v", flags)
	}
	if len(flags.ForbiddenToolNames) != 0 {
		t.Fatalf("forbidden=%v want none", flags.ForbiddenToolNames)
	}
}

func TestAssessLeakageMarksExplicitBridgeAccess(t *testing.T) {
	t.Parallel()

	flags := AssessLeakage(nil, LeakageOptions{DatasetAccessibleDuringSolve: true})
	if !flags.DatasetAccessibleDuringSolve || !flags.Leaked() {
		t.Fatalf("expected dataset leakage: %+v", flags)
	}
}
