package dbdriver

import (
	"strings"
	"testing"
)

func TestBuildSQLiteDSNUsesOpaqueFileURIForInMemoryDatabases(t *testing.T) {
	dsn, err := buildSQLiteDSN(":memory:", 5000)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(dsn, "file:foxctl_mem_") {
		t.Fatalf("dsn=%q want opaque file URI with generated name", dsn)
	}
	if strings.HasPrefix(dsn, "file://") {
		t.Fatalf("dsn=%q must not use URI authority for named in-memory DB", dsn)
	}
	for _, part := range []string{"mode=memory", "cache=shared", "_busy_timeout=5000"} {
		if !strings.Contains(dsn, part) {
			t.Fatalf("dsn=%q missing %q", dsn, part)
		}
	}
}
