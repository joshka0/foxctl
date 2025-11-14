package sqlutil

import "testing"

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
