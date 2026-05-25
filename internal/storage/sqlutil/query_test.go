package sqlutil

import (
	"strings"
	"testing"
	"testing/quick"
)

func TestQueryBuilderBuild(t *testing.T) {
	qb := NewQueryBuilder("jobs").
		Select("id", "state").
		WhereEq("workspace", "default").
		Where("state IN (?, ?)", "running", "queued").
		OrderBy("created_at", "DESC").
		Limit(10).
		Offset(5)

	sql, args := qb.Build()
	expectedSQL := "SELECT id, state FROM jobs WHERE workspace = ? AND state IN (?, ?) ORDER BY created_at DESC LIMIT 10 OFFSET 5"
	if sql != expectedSQL {
		t.Fatalf("unexpected SQL:\n got: %s\nwant: %s", sql, expectedSQL)
	}
	expectedArgs := []any{"default", "running", "queued"}
	if len(args) != len(expectedArgs) {
		t.Fatalf("unexpected args length: %d", len(args))
	}
	for i, arg := range args {
		if arg != expectedArgs[i] {
			t.Fatalf("arg[%d] = %v, want %v", i, arg, expectedArgs[i])
		}
	}
}

func TestQueryBuilderWhereInEmpty(t *testing.T) {
	qb := NewQueryBuilder("jobs").WhereIn("state", nil)
	sql, args := qb.Build()
	if sql != "SELECT * FROM jobs" {
		t.Fatalf("unexpected SQL for empty WhereIn: %s", sql)
	}
	if len(args) != 0 {
		t.Fatalf("expected no args, got %d", len(args))
	}
}

func TestQueryBuilderDefaultSelect(t *testing.T) {
	qb := NewQueryBuilder("jobs")
	sql, _ := qb.Build()
	if sql != "SELECT * FROM jobs" {
		t.Fatalf("unexpected default SQL: %s", sql)
	}
}

func TestQueryBuilderWhereInPropertyPlaceholdersAndArgsStayAligned(t *testing.T) {
	t.Parallel()

	property := func(raw []uint8) bool {
		if len(raw) > 12 {
			raw = raw[:12]
		}
		values := make([]any, len(raw))
		for i, value := range raw {
			values[i] = int(value)
		}

		sql, args := NewQueryBuilder("jobs").
			WhereEq("tenant", "default").
			WhereIn("id", values).
			Build()

		wantArgs := append([]any{"default"}, values...)
		if len(args) != len(wantArgs) {
			t.Logf("args len=%d want %d sql=%q args=%v", len(args), len(wantArgs), sql, args)
			return false
		}
		for i := range wantArgs {
			if args[i] != wantArgs[i] {
				t.Logf("args[%d]=%v want %v sql=%q args=%v", i, args[i], wantArgs[i], sql, args)
				return false
			}
		}

		if strings.Count(sql, "?") != len(wantArgs) {
			t.Logf("placeholder count=%d want %d sql=%q", strings.Count(sql, "?"), len(wantArgs), sql)
			return false
		}
		hasWhereIn := strings.Contains(sql, "id IN (")
		if hasWhereIn != (len(values) > 0) {
			t.Logf("WhereIn presence=%v want %v sql=%q", hasWhereIn, len(values) > 0, sql)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 300}); err != nil {
		t.Fatal(err)
	}
}

func TestQueryBuilderBuildReturnsArgsCopy(t *testing.T) {
	t.Parallel()

	qb := NewQueryBuilder("jobs").WhereEq("tenant", "default")
	_, args := qb.Build()
	args[0] = "mutated"

	_, nextArgs := qb.Build()
	if nextArgs[0] != "default" {
		t.Fatalf("Build() returned args alias; next args=%v", nextArgs)
	}
}

func TestQueryBuilderLimitAndOffsetCanBeCleared(t *testing.T) {
	t.Parallel()

	sql, args := NewQueryBuilder("jobs").
		Limit(10).
		Limit(0).
		Offset(5).
		Offset(-1).
		Build()
	if sql != "SELECT * FROM jobs" {
		t.Fatalf("unexpected SQL after clearing limit/offset: %s", sql)
	}
	if len(args) != 0 {
		t.Fatalf("args len=%d want 0", len(args))
	}
}
