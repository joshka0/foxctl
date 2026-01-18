# CLI Entrypoint, Config/Env Loading, and Persistent Pre-Run Wiring

**Codemap ID:**
CLI_Entrypoint__Config_Env_Loading__and_Persistent_Pre-Run_Wiring_20260115_003112\
**Description:** Maps the initialization sequence from binary startup through
configuration loading to lazy storage initialization. The flow begins with .env
loading [1b], proceeds through Cobra's initialization hooks [2a, 2b],
materializes configuration with layered defaults [3c, 3d], wires context
dependencies [4c, 4d], and finally opens storage backends on-demand [5c, 6b].

---

## Trace 1: Binary Startup → Command Execution

**Description:** Main entrypoint flow showing how the CLI bootstraps from main()
through Cobra command execution

```
Binary Startup to Command Execution
├── main() entrypoint <-- main.go:12
│   ├── config.LoadDotEnv() <-- 1a
│   │   └── Load .env files (reverse order) <-- 1b
│   └── cmd.Execute(ctx) <-- 1c
│       └── Cobra command routing
│           ├── init() registration <-- root.go:35
│           │   ├── cobra.OnInitialize(initConfig) <-- 1d
│           │   └── rootCmd.PersistentPreRunE <-- 1e
│           └── Command execution
│               ├── initConfig() runs first <-- root.go:62
│               │   └── viper + .env setup <-- root.go:63
│               └── PersistentPreRunE runs second <-- root.go:38
│                   └── config.Load() + context wiring <-- root.go:47
```

### Location Details

- **1a:** First action: Load .env files\
  **Path:** `/Users/jkatigbak/repos/personal/agentctl/cmd/agentctl/main.go:15`\
  **Description:** Called before anything else to populate environment variables
  from .env files

- **1b:** Load .env files in reverse priority order\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/internal/platform/config/dotenv.go:48`\
  **Description:** Loads ~/.agentctl/.env, $AGENTCTL_HOME/.env, then $PWD/.env
  (later files override)

- **1c:** Execute Cobra root command\
  **Path:** `/Users/jkatigbak/repos/personal/agentctl/cmd/agentctl/main.go:17`\
  **Description:** Hands control to Cobra which triggers initialization hooks
  and command routing

- **1d:** Register Cobra initialization callback\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/cmd/agentctl/cmd/root.go:36`\
  **Description:** Sets up initConfig to run before any command executes

- **1e:** Register pre-run hook for all commands\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/cmd/agentctl/cmd/root.go:38`\
  **Description:** Ensures config and logger are loaded into context before any
  command runs

---

## Trace 2: Cobra Initialization Hooks: Viper + .env Loading

**Description:** Shows how Cobra's OnInitialize callback sets up viper and loads
additional .env files

```
Cobra Initialization Hooks: Viper + .env Loading
├── cobra.OnInitialize(initConfig) registration <-- root.go:36
│   └── initConfig() callback execution <-- root.go:62
│       ├── viper.SetEnvPrefix("agentctl") <-- 2a
│       ├── viper.AutomaticEnv() <-- 2b
│       ├── Load ~/.agentctl/.env (global) <-- 2c
│       ├── Walk up to find .git directory <-- root.go:77
│       │   └── Load {git_root}/.env <-- 2d
│       └── Load ./.env (highest priority) <-- 2e
└── Result: Environment variables populated
    └── Available to config.Load() via viper
```

### Location Details

- **2a:** Configure viper for AGENTCTL_* env vars\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/cmd/agentctl/cmd/root.go:63`\
  **Description:** Sets up automatic environment variable binding with AGENTCTL
  prefix

- **2b:** Enable automatic env var reading\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/cmd/agentctl/cmd/root.go:64`\
  **Description:** Allows viper to read AGENTCTL_* environment variables
  automatically

- **2c:** Load global .env file\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/cmd/agentctl/cmd/root.go:71`\
  **Description:** Loads ~/.agentctl/.env for global defaults

- **2d:** Load git root .env file\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/cmd/agentctl/cmd/root.go:79`\
  **Description:** Walks up directory tree to find .git and loads .env from git
  root

- **2e:** Load current directory .env (highest priority)\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/cmd/agentctl/cmd/root.go:86`\
  **Description:** Loads ./.env which overrides all previous .env files

---

## Trace 3: Configuration Materialization: Defaults → YAML → Env Overrides

**Description:** Traces config.Load() through the layered configuration system
that applies defaults, reads YAML, and applies environment overrides

```
config.Load() - Configuration Materialization <-- config.go:249
├── parseOptions() - handle WithConfigFile <-- config.go:250
├── userHomeDir() - get user home directory <-- config.go:252
├── newConfiguredViper() <-- 3a
│   ├── viper.New() <-- config.go:282
│   ├── SetEnvPrefix("AGENTCTL") <-- config.go:284
│   └── AutomaticEnv() <-- config.go:286
├── applyDefaults(v, defaultHome) <-- 3b
│   ├── SetDefault("home", defaultHome) <-- 3c
│   ├── SetDefault("paths.*", ...) <-- config.go:294
│   ├── SetDefault("database.driver", "libsql") <-- 3d
│   ├── SetDefault("logging.*", ...) <-- config.go:304
│   └── SetDefault("cas.*", ...) <-- config.go:320
├── configureConfigFile() - set config path <-- config.go:260
├── readConfig() - v.ReadInConfig() <-- config.go:261
│   └── if err := v.ReadInConfig() <-- 3e
├── decodeConfig(v) <-- config.go:265
│   ├── if err := v.Unmarshal(&cfg) <-- 3f
│   └── parsePluginPathList() <-- config.go:348
└── finalizeConfig(cfg, home) <-- config.go:270
    ├── resolvePath() for all paths <-- config.go:354
    ├── normalize durations/defaults <-- config.go:372
    └── Apply env var overrides
        └── TURSO_DATABASE_URL override <-- 3g
```

### Location Details

- **3a:** Create viper instance with env bindings\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/internal/platform/config/config.go:257`\
  **Description:** Sets up viper with AGENTCTL prefix and dot-to-underscore
  replacement

- **3b:** Apply hardcoded defaults\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/internal/platform/config/config.go:259`\
  **Description:** Sets defaults for all config fields (paths, timeouts,
  database driver, etc.)

- **3c:** Set default home directory\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/internal/platform/config/config.go:291`\
  **Description:** Defaults to ~/.agentctl for all persistent data

- **3d:** Default to libsql driver\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/internal/platform/config/config.go:314`\
  **Description:** Uses libsql for local-first database with optional sync
  capability

- **3e:** Read YAML config file\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/internal/platform/config/config.go:334`\
  **Description:** Loads ~/.agentctl/config.yaml if present, merging with
  defaults

- **3f:** Unmarshal into Config struct\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/internal/platform/config/config.go:345`\
  **Description:** Converts viper's merged config into typed Config struct with
  env var overrides

- **3g:** Apply Turso env var overrides\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/internal/platform/config/config.go:392`\
  **Description:** Allows standard TURSO_* env vars to override config file
  settings

---

## Trace 4: Context Wiring: Config + Logger Injection

**Description:** Shows how PersistentPreRunE loads config and logger into
context for downstream commands

```
Cobra Command Execution Flow
└── rootCmd.Execute() <-- root.go:31
    └── Cobra framework invokes hooks
        ├── cobra.OnInitialize callbacks
        │   └── initConfig() sets up viper <-- root.go:62
        │       └── (loads .env files - trace 2)
        └── rootCmd.PersistentPreRunE hook <-- root.go:38
            ├── Check if config exists <-- 4a
            ├── config.Load() <-- 4b
            │   └── (materializes config - trace 3)
            ├── logging.New() <-- 4c
            │   └── Creates zerolog.Logger <-- logging.go:82
            ├── config.WithContext() <-- 4d
            │   └── Stores cfg in context.Value <-- context.go:9
            ├── logging.WithContext() <-- 4e
            │   └── Stores logger in context.Value <-- logging.go:104
            └── cmd.SetContext(ctx) <-- 4f
                └── Updates command's context
```

### Location Details

- **4a:** Check if config already in context\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/cmd/agentctl/cmd/root.go:39`\
  **Description:** Skip loading if config was already injected (e.g., by tests
  or parent command)

- **4b:** Load configuration\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/cmd/agentctl/cmd/root.go:47`\
  **Description:** Materializes full config from defaults, YAML, and env vars

- **4c:** Create logger from config\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/cmd/agentctl/cmd/root.go:51`\
  **Description:** Builds zerolog logger with level and format from
  config.Logging

- **4d:** Store config in context\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/cmd/agentctl/cmd/root.go:55`\
  **Description:** Makes config available to all downstream code via context

- **4e:** Store logger in context\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/cmd/agentctl/cmd/root.go:56`\
  **Description:** Makes logger available to all downstream code via context

- **4f:** Update command context\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/cmd/agentctl/cmd/root.go:57`\
  **Description:** Replaces command's context with enriched version containing
  config and logger

---

## Trace 5: Lazy Storage Initialization: Run Command → Job Store

**Description:** Demonstrates on-demand storage initialization when agentctl run
executes a skill

```
agentctl run command execution
├── CLI command handler
│   └── executeRunCommand() <-- 5a
│       └── config.MustFromContext() <-- run.go:61
├── Executor creation
│   └── runservice.NewExecutor() <-- 5b
│       └── stores executor with cfg reference <-- executor.go:36
├── Job preparation (lazy initialization)
│   └── PrepareJob() <-- jobs.go:35
│       └── ensureJobStore() <-- jobs.go:14
│           └── jobs.Open() <-- 5c
├── Storage layer initialization
│   └── jobs/store.Open()
│       └── persist.Open() <-- 5d
├── Database connection
│   └── persist/store.Open()
│       └── sqliteutil.OpenDB() <-- 5e
│           ├── os.MkdirAll(dir) <-- sqliteutil.go:26
│           ├── sql.Open("sqlite", path) <-- sqliteutil.go:29
│           ├── PRAGMA journal_mode=WAL <-- sqliteutil.go:46
│           └── migrate(ctx, db) <-- sqliteutil.go:56
└── Ready for skill execution
    └── jobStore.ExecutePreparedSkill() <-- jobs.go:151
```

### Location Details

- **5a:** Retrieve config from context\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/cmd/agentctl/cmd/run.go:61`\
  **Description:** Extracts config that was injected by PersistentPreRunE

- **5b:** Create executor with config\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/cmd/agentctl/cmd/run.go:105`\
  **Description:** Constructs executor that will lazily open storage when needed

- **5c:** Lazy-open job store on first use\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/internal/runservice/jobs.go:18`\
  **Description:** Opens SQLite database at ~/.agentctl/jobs/jobs.db only when
  needed

- **5d:** Open persistent job storage\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/internal/storage/jobs/store.go:38`\
  **Description:** Delegates to persist layer which handles database connection

- **5e:** Open SQLite database with migrations\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/internal/storage/jobs/persist/store.go:45`\
  **Description:** Creates parent dirs, opens DB, enables WAL, runs migrations

---

## Trace 6: Database Driver Selection: SQLite vs LibSQL vs Turso

**Description:** Shows how the storage layer selects and configures database
drivers based on environment variables

```
Database Storage Layer
├── SQLite Direct Path
│   ├── sqliteutil.OpenDB() entry <-- 6a
│   │   ├── Create parent directories <-- sqliteutil.go:26
│   │   ├── sql.Open("sqlite", path) <-- sqliteutil.go:29
│   │   ├── Set busy_timeout pragma <-- sqliteutil.go:34
│   │   ├── Enable WAL if not already on <-- 6b
│   │   ├── Enable foreign_keys pragma <-- sqliteutil.go:51
│   │   └── Run migrations <-- 6c
│   └── Returns *sql.DB <-- sqliteutil.go:61
│
└── Driver Abstraction Path
    ├── ConfigLoader.loadConfig() <-- config_loader.go:42
    │   ├── Read AGENTCTL_*_DB_DRIVER env <-- 6d
    │   └── Returns Config with driver type
    │
    └── dbdriver.OpenDB() <-- driver.go:91
        └── Switch on cfg.Driver <-- 6e
            ├── case DriverSQLite → openSQLite() <-- driver.go:98
            ├── case DriverLibSQL → openLibSQL() <-- driver.go:100
            └── case DriverTurso → openTurso() <-- driver.go:102
```

### Location Details

- **6a:** Open SQLite database connection\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/internal/storage/sqliteutil/sqliteutil.go:29`\
  **Description:** Uses modernc.org/sqlite driver for pure-Go SQLite

- **6b:** Enable WAL journaling\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/internal/storage/sqliteutil/sqliteutil.go:46`\
  **Description:** Configures Write-Ahead Logging for better concurrency

- **6c:** Run database migrations\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/internal/storage/sqliteutil/sqliteutil.go:56`\
  **Description:** Applies schema migrations to ensure database is up-to-date

- **6d:** Check for driver override env var\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/internal/storage/dbdriver/config_loader.go:46`\
  **Description:** Reads AGENTCTL_<DB>_DB_DRIVER to select sqlite/libsql/turso

- **6e:** Route to driver-specific opener\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/internal/storage/dbdriver/driver.go:96`\
  **Description:** Dispatches to openSQLite, openLibSQL, or openTurso based on
  config

---

## Trace 7: Daemon Pre-Warming: Persistent Service Initialization

**Description:** Traces daemon startup showing how it pre-loads config and opens
shared resources for fast skill execution

```
Daemon Startup & Pre-Warming Flow
├── CLI: daemon start command
│   └── runDaemonStart() <-- daemon.go:126
│       └── config.Load(ctx) <-- 7a
│           └── [returns materialized Config]
├── Daemon Service Construction
│   └── daemon.NewService(cfg, opts) <-- service.go:65
│       ├── sqliteutil.NewPool() <-- 7b
│       ├── sqliteutil.SetGlobalPool(pool) <-- 7c
│       └── NewSkillResolver(cfg) <-- service.go:74
├── Service.Run() starts listener <-- service.go:89
│   ├── net.Listen("unix", socketPath) <-- 7d
│   ├── os.Chmod(socketPath, 0o600) <-- service.go:107
│   ├── writePIDFile() <-- service.go:113
│   └── go acceptLoop(ctx) <-- 7e
│       └── [loops accepting connections]
└── First Skill Execution Request
    └── handleRun() processes request <-- service.go:326
        └── getCacheStore() lazy init <-- service.go:487
            └── cache.Open(ctx, cfg.Paths.Cache) <-- 7f
```

### Location Details

- **7a:** Load config in daemon start\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/cmd/agentctl/cmd/daemon.go:133`\
  **Description:** Daemon loads config once at startup instead of per-request

- **7b:** Create shared connection pool\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/internal/daemon/service.go:67`\
  **Description:** Pre-allocates database connection pool for reuse across
  requests

- **7c:** Set global pool for reuse\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/internal/daemon/service.go:68`\
  **Description:** Makes pool available to all storage backends opened by daemon

- **7d:** Create Unix socket listener\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/internal/daemon/service.go:100`\
  **Description:** Opens /tmp/agentctl-{uid}.sock for IPC with CLI clients

- **7e:** Start connection accept loop\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/internal/daemon/service.go:120`\
  **Description:** Begins accepting skill execution requests from CLI clients

- **7f:** Lazy-open cache store on first use\
  **Path:**
  `/Users/jkatigbak/repos/personal/agentctl/internal/daemon/service.go:495`\
  **Description:** Opens cache database only when first skill execution needs it

---

## Code Snippets

### File: /Users/jkatigbak/repos/personal/agentctl/internal/daemon/service.go

```go
// Lines: 65-70
func NewService(cfg config.Config, opts ServiceOptions) (*Service, error) {
	// Create connection pool and set as global
	pool := sqliteutil.NewPool()
	sqliteutil.SetGlobalPool(pool)

	svc := &Service{
```

```go
// Lines: 98-102
	// Create listener
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", socketPath, err)
```

```go
// Lines: 118-122
	// Accept connections
	go s.acceptLoop(ctx)

	// Wait for shutdown signal
```

```go
// Lines: 493-497
	}

	store, err := cache.Open(ctx, s.cfg.Paths.Cache, cache.Options{
		AutoTTL: s.cfg.Memory.AutoCacheTTL,
		CASPath: s.cfg.Paths.CAS,
```

### File: /Users/jkatigbak/repos/personal/agentctl/internal/platform/config/dotenv.go

```go
// Lines: 46-50
		// godotenv.Load does not override existing env vars
		// Use Overload if you want later files to take precedence
		_ = godotenv.Load(envFiles[i])
	}
```

### File: /Users/jkatigbak/repos/personal/agentctl/cmd/agentctl/cmd/root.go

```go
// Lines: 34-41
func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "Path to a co...
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		if _, ok := config.FromContext(cmd.Context()); ok {
			return nil
		}
```

```go
// Lines: 45-59
		opts = append(opts, config.WithConfigFile(configPath))
		}
		cfg, err := config.Load(cmd.Context(), opts...)
		if err != nil {
			return err
		}
		logger := logging.New(logging.Config{
			Level:  logging.ParseLevel(cfg.Logging.Level),
			Format: logging.ParseFormat(cfg.Logging.Format),
		})
		ctx := config.WithContext(cmd.Context(), cfg)
		ctx = logging.WithContext(ctx, logger)
		cmd.SetContext(ctx)
		return nil
	}
```

```go
// Lines: 61-66
func initConfig() {
	viper.SetEnvPrefix("agentctl")
	viper.AutomaticEnv()

	// Load .env files from multiple locations (later files override earlier ones)
```

```go
// Lines: 69-73
	// 3. ./.env (current directory)
	if home, err := os.UserHomeDir(); err == nil {
		_ = godotenv.Load(filepath.Join(home, ".agentctl", ".env"))
	}
```

```go
// Lines: 77-81
		for dir != "/" && dir != "." {
			if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
				_ = godotenv.Load(filepath.Join(dir, ".env"))
				break
			}
```

```go
// Lines: 84-88
	}

	_ = godotenv.Load() // loads .env from current directory (highest priority)
}
```

### File: /Users/jkatigbak/repos/personal/agentctl/internal/platform/config/config.go

```go
// Lines: 255-261
	}

	v := newConfiguredViper()
	defaultHome := filepath.Join(home, ".agentctl")
	applyDefaults(v, defaultHome)
	configureConfigFile(v, l, defaultHome)
	if err := readConfig(v, l.configFile); err != nil {
```

```go
// Lines: 289-293
func applyDefaults(v *viper.Viper, defaultHome string) {
	v.SetDefault("home", defaultHome)
	v.SetDefault("inline_output_kb", DefaultInlineOutputKB)
	v.SetDefault("max_capture_kb", DefaultMaxCaptureKB)
```

```go
// Lines: 312-316
	v.SetDefault("indexing.post_review.indexers", []map[string]any{})
	// Database defaults - libsql for local-first with optional sync
	v.SetDefault("database.driver", "libsql")
	v.SetDefault("database.turso.url", "")
	v.SetDefault("database.turso.auth_token", "")
```

```go
// Lines: 332-336
func readConfig(v *viper.Viper, explicit string) error {
	if err := v.ReadInConfig(); err != nil {
		var configErr viper.ConfigFileNotFoundError
		if explicit != "" || !errors.As(err, &configErr) {
```

```go
// Lines: 343-347
func decodeConfig(v *viper.Viper) (Config, error) {
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("config decode: %w", err)
	}
```

```go
// Lines: 390-394
	// Database/Turso: Allow standard Turso env vars as overrides
	if url := os.Getenv("TURSO_DATABASE_URL"); url != "" && cfg.Database.Turso.U...
		cfg.Database.Turso.URL = url
	}
```

### File: /Users/jkatigbak/repos/personal/agentctl/cmd/agentctl/cmd/run.go

```go
// Lines: 59-63
		return writeRunExamples(cmd, args)
	}
	cfg := config.MustFromContext(cmd.Context())
	data, err := loadSkillInput(cmd, cfg, flags.Input, flags.InputFile)
	if err != nil {
```

```go
// Lines: 103-107
	}

	executor := runservice.NewExecutor(ctx, cfg, handle, stdout, cmd.ErrOrStderr...
	executor.SetAsyncRunner(defaultAsyncRunner)
	defer executor.Close()
```

### File: /Users/jkatigbak/repos/personal/agentctl/internal/runservice/jobs.go

```go
// Lines: 16-20
		return nil
	}
	store, err := jobs.Open(e.ctx, e.cfg.Paths.Jobs)
	if err != nil {
		return err
```

### File: /Users/jkatigbak/repos/personal/agentctl/internal/storage/jobs/store.go

```go
// Lines: 36-40
func Open(ctx context.Context, root string) (store *Store, err error) {
	logger := logging.FromContext(ctx)
	p, err := persist.Open(ctx, root)
	if err != nil {
		return nil, err
```

### File: /Users/jkatigbak/repos/personal/agentctl/cmd/agentctl/main.go

```go
// Lines: 13-19
	// Load .env files before anything else
	// Priority: ~/.agentctl/.env → $PWD/.env (project overrides global)
	config.LoadDotEnv()

	if err := cmd.Execute(context.Background()); err != nil {
		log.Fatal(err)
	}
```

### File: /Users/jkatigbak/repos/personal/agentctl/internal/storage/jobs/persist/store.go

```go
// Lines: 43-47
func Open(ctx context.Context, root string) (Store, error) {
	dbPath := filepath.Join(root, "jobs.db")
	db, err := sqliteutil.OpenDB(ctx, dbPath, migrate)
	if err != nil {
		// Check if error is due to readonly filesystem
```

### File: /Users/jkatigbak/repos/personal/agentctl/internal/storage/sqliteutil/sqliteutil.go

```go
// Lines: 27-31
		return nil, fmt.Errorf("sqliteutil: ensure dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("sqliteutil: open: %w", err)
```

```go
// Lines: 44-48
	}
	if !strings.EqualFold(mode, "wal") {
		if _, err := db.ExecContext(ctx, `PRAGMA journal_mode=WAL;`); err != nil {
			errs.Ignore(db.Close(), "close sqlite db after WAL failure")
			return nil, fmt.Errorf("sqliteutil: enable wal: %w", err)
```

```go
// Lines: 54-58
	}
	if migrate != nil {
		if err := migrate(ctx, db); err != nil {
			errs.Ignore(db.Close(), "close sqlite db after migrate failure")
			return nil, fmt.Errorf("sqliteutil: migrate: %w", err)
```

### File: /Users/jkatigbak/repos/personal/agentctl/internal/storage/dbdriver/config_loader.go

```go
// Lines: 44-48
	// Format: AGENTCTL_<PREFIX>_DB_DRIVER (e.g., AGENTCTL_CACHE_DB_DRIVER)
	driverEnv := fmt.Sprintf("AGENTCTL_%s_DB_DRIVER", strings.ToUpper(prefix))
	driver := os.Getenv(driverEnv)

	// Default to SQLite if not specified
```

### File: /Users/jkatigbak/repos/personal/agentctl/internal/storage/dbdriver/driver.go

```go
// Lines: 94-98
	}

	switch cfg.Driver {
	case DriverSQLite:
		return openSQLite(ctx, cfg.SQLite, migrate)
```

### File: /Users/jkatigbak/repos/personal/agentctl/cmd/agentctl/cmd/daemon.go

```go
// Lines: 131-135
	if !ok {
		var err error
		cfg, err = config.Load(ctx)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
```

---

_Generated from codemap
CLI_Entrypoint__Config_Env_Loading__and_Persistent_Pre-Run_Wiring_20260115_003112_
