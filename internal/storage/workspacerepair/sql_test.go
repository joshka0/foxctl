package workspacerepair

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/storage/dbutil"
)

func TestWorkspaceColumnsCollectPathWorkspaces(t *testing.T) {
	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "repair.db"), nil)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE one (workspace TEXT);
		CREATE TABLE two (workspace_id TEXT);
		INSERT INTO one (workspace) VALUES ('/tmp/repo'), ('/tmp/repo'), ('ws-golden'), ('');
		INSERT INTO two (workspace_id) VALUES ('~/repo'), ('plain-id');
	`); err != nil {
		t.Fatalf("seed db: %v", err)
	}

	columns := []WorkspaceColumn{
		{Table: "one", Column: "workspace"},
		{Table: "two", Column: "workspace_id"},
	}
	if !AnyPathWorkspace(ctx, db, columns...) {
		t.Fatal("AnyPathWorkspace() = false, want true")
	}

	got := CollectPathWorkspaces(ctx, db, columns...)
	if _, ok := got["/tmp/repo"]; !ok {
		t.Fatalf("missing /tmp/repo in %v", got)
	}
	if _, ok := got["~/repo"]; !ok {
		t.Fatalf("missing ~/repo in %v", got)
	}
	if _, ok := got["ws-golden"]; ok {
		t.Fatalf("unexpected ws-golden in %v", got)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d want 2: %v", len(got), got)
	}
}

func TestWorkspaceColumnRejectsInvalidIdentifiers(t *testing.T) {
	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "repair.db"), nil)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })

	columns := []WorkspaceColumn{
		{Table: "one; DROP TABLE one", Column: "workspace"},
		{Table: "main.one", Column: "workspace"},
		{Table: "one", Column: "workspace.id"},
		{Table: "one--comment", Column: "workspace"},
		{Table: "1one", Column: "workspace"},
	}
	for _, column := range columns {
		t.Run(column.Table+"/"+column.Column, func(t *testing.T) {
			if table, name, ok := column.normalized(); ok {
				t.Fatalf("normalized() = (%q, %q, true), want false", table, name)
			}
			if AnyPathWorkspace(ctx, db, column) {
				t.Fatal("AnyPathWorkspace() = true for invalid identifier")
			}
			if got := CollectPathWorkspaces(ctx, db, column); len(got) != 0 {
				t.Fatalf("CollectPathWorkspaces()=%v want empty", got)
			}
		})
	}
}
