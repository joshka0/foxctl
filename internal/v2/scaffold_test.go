package v2

import (
	"testing"

	_ "github.com/joshka0/foxctl/internal/v2/adapters"
	_ "github.com/joshka0/foxctl/internal/v2/adapters/jido"
	_ "github.com/joshka0/foxctl/internal/v2/adapters/llm"
	_ "github.com/joshka0/foxctl/internal/v2/adapters/mailbox"
	_ "github.com/joshka0/foxctl/internal/v2/adapters/sqlite"
	_ "github.com/joshka0/foxctl/internal/v2/adapters/telemetry"
	_ "github.com/joshka0/foxctl/internal/v2/adapters/turso"
	_ "github.com/joshka0/foxctl/internal/v2/core/ask"
	_ "github.com/joshka0/foxctl/internal/v2/core/errors"
	_ "github.com/joshka0/foxctl/internal/v2/core/events"
	_ "github.com/joshka0/foxctl/internal/v2/core/kill"
	_ "github.com/joshka0/foxctl/internal/v2/core/list"
	_ "github.com/joshka0/foxctl/internal/v2/core/policy"
	_ "github.com/joshka0/foxctl/internal/v2/core/run"
	_ "github.com/joshka0/foxctl/internal/v2/core/services"
	_ "github.com/joshka0/foxctl/internal/v2/core/spawn"
	_ "github.com/joshka0/foxctl/internal/v2/core/tool"
	_ "github.com/joshka0/foxctl/internal/v2/runtime/contextbuilder"
	_ "github.com/joshka0/foxctl/internal/v2/runtime/enrichers"
	_ "github.com/joshka0/foxctl/internal/v2/runtime/maintenance"
	_ "github.com/joshka0/foxctl/internal/v2/runtime/runner"
	_ "github.com/joshka0/foxctl/internal/v2/runtime/snapshots"
	_ "github.com/joshka0/foxctl/internal/v2/runtime/supervisor"
	_ "github.com/joshka0/foxctl/internal/v2/services"
	_ "github.com/joshka0/foxctl/internal/v2/testkit/fakes"
	_ "github.com/joshka0/foxctl/internal/v2/testkit/golden"
)

func TestScaffoldPackagesCompile(t *testing.T) {
	t.Parallel()
}
