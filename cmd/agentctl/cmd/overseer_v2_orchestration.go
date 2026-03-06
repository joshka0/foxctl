package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/dbdriver"
	v2jido "github.com/jkatigb/agentctl/internal/v2/adapters/jido"
	libsqlevents "github.com/jkatigb/agentctl/internal/v2/adapters/libsql/events"
	libsqlorchestration "github.com/jkatigb/agentctl/internal/v2/adapters/libsql/orchestration"
	coreorchestration "github.com/jkatigb/agentctl/internal/v2/core/orchestration"
	v2runtimeorchestration "github.com/jkatigb/agentctl/internal/v2/runtime/orchestration"
	v2services "github.com/jkatigb/agentctl/internal/v2/services"
)

const envJidoOrchestrationPollIntervalMS = "AGENTCTL_JIDO_ORCHESTRATION_POLL_INTERVAL_MS"

func orchestrationComponentEnabled() bool {
	return v2jido.OrchestrationRuntimeEnabled(v2jido.OrchestrationRuntimeConfig{})
}

func newOverseerJidoOrchestrationComponent(
	ctx context.Context,
	cfg config.Config,
) (*v2runtimeorchestration.Component, func(), error) {
	eventStore, err := openOverseerOrchestrationEventStore(ctx, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("open v2 orchestration events: %w", err)
	}
	orchestrationStore, closeStore, err := openOverseerOrchestrationStore(ctx, cfg)
	if err != nil {
		_ = eventStore.Close()
		return nil, nil, fmt.Errorf("open v2 orchestration store: %w", err)
	}

	runtime, err := v2jido.NewOrchestrationRuntime(v2jido.OrchestrationRuntimeConfig{
		Events:      eventStore,
		Projections: orchestrationStore,
		Reader:      orchestrationStore,
	})
	if err != nil {
		_ = closeStore()
		_ = eventStore.Close()
		return nil, nil, fmt.Errorf("configure jido orchestration runtime: %w", err)
	}

	dispatchParentID := resolveOverseerDispatchParentAgentID()
	if dispatchParentID == "" {
		_ = closeStore()
		_ = eventStore.Close()
		return nil, nil, fmt.Errorf("jido orchestration dispatch parent_agent_id is required")
	}

	source, err := v2runtimeorchestration.NewBoardCandidateSource(v2runtimeorchestration.BoardCandidateSourceConfig{
		Reader:        orchestrationStore,
		ParentAgentID: dispatchParentID,
	})
	if err != nil {
		_ = closeStore()
		_ = eventStore.Close()
		return nil, nil, fmt.Errorf("configure orchestration candidate source: %w", err)
	}

	spawnService := v2services.NewSpawnService(v2services.SpawnDependencies{
		RuntimeSpawner: runtime.ChildSpawner,
	})
	dispatchService := v2services.NewOrchestrationService(v2services.OrchestrationDependencies{
		Spawn:       spawnService,
		Reader:      orchestrationStore,
		LaneOptions: coreorchestration.DefaultLaneOptions(),
	})

	scheduler := v2runtimeorchestration.NewScheduler(v2runtimeorchestration.SchedulerConfig{
		Source:  source,
		Service: dispatchService,
	})

	component := v2runtimeorchestration.NewComponent(v2runtimeorchestration.ComponentConfig{
		PollInterval: parseDurationMillisEnv(envJidoOrchestrationPollIntervalMS, 5*time.Second),
		Scheduler:    scheduler,
		Reconciler:   runtime.Reconciler,
		OnError: func(err error) {
			if err == nil {
				return
			}
			log.Error().Err(err).Msg("v2 orchestration component cycle failed")
		},
	})

	cleanup := func() {
		if closeStore != nil {
			_ = closeStore()
		}
		_ = eventStore.Close()
	}
	return component, cleanup, nil
}

func resolveOverseerDispatchParentAgentID() string {
	if value := strings.TrimSpace(os.Getenv(v2jido.EnvJidoOrchestrationDispatchParentAgentID)); value != "" {
		return value
	}
	raw := strings.TrimSpace(os.Getenv(v2jido.EnvJidoOrchestrationParentAgentIDs))
	if raw == "" {
		return ""
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n'
	})
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func openOverseerOrchestrationStore(ctx context.Context, cfg config.Config) (*libsqlorchestration.Store, func() error, error) {
	storageRoot := strings.TrimSpace(cfg.Storage.Root)
	if storageRoot == "" {
		return nil, nil, fmt.Errorf("orchestration store open: storage root is required")
	}

	dbCfg, err := overseerOrchestrationDBConfig(cfg)
	if err != nil {
		return nil, nil, err
	}

	db, closeFn, err := dbdriver.OpenDBCompatWithCloser(ctx, dbCfg, libsqlorchestration.MigrateSchema)
	if err != nil && shouldFallbackOrchestrationLibSQLToSQLite(dbCfg, err) {
		sqliteCfg := dbdriver.DefaultSQLiteConfig(overseerLibSQLPathToSQLitePath(dbCfg.LibSQL.Path, filepath.Join(storageRoot, "v2_events.db")))
		db, closeFn, err = dbdriver.OpenDBCompatWithCloser(ctx, sqliteCfg, libsqlorchestration.MigrateSchema)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("orchestration store open: %w", err)
	}

	store := libsqlorchestration.NewStore(db, libsqlorchestration.StoreOptions{
		LaneOptions: coreorchestration.DefaultLaneOptions(),
	})
	return store, closeFn, nil
}

func openOverseerOrchestrationEventStore(ctx context.Context, cfg config.Config) (*libsqlevents.Store, error) {
	dbCfg, err := overseerOrchestrationDBConfig(cfg)
	if err != nil {
		return nil, err
	}
	db, closeFn, err := dbdriver.OpenDBCompatWithCloser(ctx, dbCfg, libsqlevents.MigrateSchema)
	if err != nil && shouldFallbackOrchestrationLibSQLToSQLite(dbCfg, err) {
		storageRoot := strings.TrimSpace(cfg.Storage.Root)
		sqliteCfg := dbdriver.DefaultSQLiteConfig(overseerLibSQLPathToSQLitePath(dbCfg.LibSQL.Path, filepath.Join(storageRoot, "v2_events.db")))
		db, closeFn, err = dbdriver.OpenDBCompatWithCloser(ctx, sqliteCfg, libsqlevents.MigrateSchema)
	}
	if err != nil {
		return nil, fmt.Errorf("orchestration event store open: %w", err)
	}
	return libsqlevents.NewStore(db, closeFn), nil
}

func overseerOrchestrationDBConfig(cfg config.Config) (dbdriver.Config, error) {
	storageRoot := strings.TrimSpace(cfg.Storage.Root)
	if storageRoot == "" {
		return dbdriver.Config{}, fmt.Errorf("orchestration db config: storage root is required")
	}

	if strings.TrimSpace(os.Getenv("AGENTCTL_V2_EVENTS_DB_DRIVER")) != "" {
		loader := dbdriver.NewConfigLoader(storageRoot)
		dbCfg := loader.LoadConfig("V2_EVENTS", "v2_events.db")
		switch dbCfg.Driver {
		case dbdriver.DriverSQLite, dbdriver.DriverLibSQL, dbdriver.DriverTurso:
			return dbCfg, nil
		case dbdriver.DriverPostgres:
			return dbdriver.Config{}, fmt.Errorf("orchestration db config: postgres is not supported by v2 libsql orchestration projections")
		default:
			return dbdriver.Config{}, fmt.Errorf("orchestration db config: unsupported database driver override %q", dbCfg.Driver)
		}
	}

	switch strings.ToLower(strings.TrimSpace(cfg.Database.Driver)) {
	case "", "libsql", "sqlite", "postgres":
		dbPath := filepath.Join(storageRoot, "v2_events.db")
		if override := strings.TrimSpace(os.Getenv("AGENTCTL_V2_EVENTS_DB_PATH")); override != "" {
			dbPath = override
		}
		return dbdriver.DefaultSQLiteConfig(dbPath), nil
	case "turso":
		url := strings.TrimSpace(cfg.Database.Turso.URL)
		token := strings.TrimSpace(cfg.Database.Turso.AuthToken)
		if url == "" || token == "" {
			return dbdriver.Config{}, fmt.Errorf("orchestration db config: turso url and auth_token are required")
		}
		dims := cfg.Database.Vector.Dimensions
		if dims <= 0 {
			dims = dbdriver.GetDefaultVectorDimensions()
		}
		replicaPath := filepath.Join(storageRoot, "v2_events.turso.replica")
		if override := strings.TrimSpace(os.Getenv("AGENTCTL_V2_EVENTS_DB_PATH")); override != "" {
			replicaPath = override
		}
		return dbdriver.Config{
			Driver: dbdriver.DriverTurso,
			Turso: dbdriver.TursoConfig{
				URL:                url,
				AuthToken:          token,
				DatabaseName:       "v2_events",
				ReplicaPath:        replicaPath,
				EnableVectorSearch: false,
				VectorDimensions:   dims,
			},
		}, nil
	default:
		return dbdriver.Config{}, fmt.Errorf("orchestration db config: unsupported database driver %q", cfg.Database.Driver)
	}
}

func shouldFallbackOrchestrationLibSQLToSQLite(dbCfg dbdriver.Config, err error) bool {
	if dbCfg.Driver != dbdriver.DriverLibSQL || err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "libsql driver requires cgo")
}

func overseerLibSQLPathToSQLitePath(libsqlPath string, fallback string) string {
	path := strings.TrimSpace(libsqlPath)
	if path == "" {
		return fallback
	}
	if strings.HasSuffix(path, ".libsql") {
		return strings.TrimSuffix(path, ".libsql") + ".db"
	}
	return path + ".db"
}
