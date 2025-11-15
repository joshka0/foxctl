# SPEC-002: Refactor Run Command

## Status
**Draft** | Priority: Critical | Complexity: High

## Problem Statement

The `RunE` function in `cmd/agentctl/cmd/run.go` (lines 33-213) is **180 lines long** and violates the Single Responsibility Principle by handling 9 different concerns:

1. Configuration loading
2. Input validation
3. Workspace detection
4. Cache management
5. Job deduplication
6. Async/sync execution branching
7. Artifact handling
8. Memory saving
9. Result formatting

This creates several problems:
- **Hard to Test**: Cannot test individual concerns in isolation
- **Hard to Understand**: Requires reading 180 lines to understand flow
- **Hard to Modify**: Changes to one concern affect others
- **Code Duplication**: Async and sync paths have duplicated logic

### Affected Files
- `cmd/agentctl/cmd/run.go` (282 LOC total, RunE is 180 LOC)

## Current State Analysis

### Function Breakdown
```go
// cmd/agentctl/cmd/run.go:33-213
func newRunCommand() *cobra.Command {
    return &cobra.Command{
        RunE: func(cmd *cobra.Command, args []string) error {
            // Lines 33-53: Config and input loading (20 lines)
            cfg, _ := commandConfig(cmd)
            var input []byte
            if len(args) == 2 {
                input = []byte(args[1])
            } else {
                input, _ = io.ReadAll(cmd.InOrStdin())
            }

            // Lines 54-64: Workspace detection (10 lines)
            workspacePath, _ := workspace.Detect(ctx)

            // Lines 65-122: Cache handling (57 lines)
            cacheStore, _ := cache.Open(ctx, cfg.Paths.Cache)
            var cacheKey string
            if cacheMode != "off" {
                cacheKey = cache.Key(skillName, input)
                entry, hit, _ := cacheStore.Get(ctx, cacheKey)
                if hit && cacheMode != "off" {
                    // Handle cache hit (25 lines of logic)
                }
            }

            // Lines 123-153: Job preparation and deduplication (30 lines)
            jobStore, _ := jobs.Open(ctx, cfg.Paths.Jobs)
            job, isDup, _ := jobStore.FindOrPrepareSkillJob(...)
            if isDup {
                // Handle duplication based on policy (20 lines)
            }

            // Lines 154-178: Async vs Sync branching (24 lines)
            if async {
                fmt.Fprintf(cmd.OutOrStdout(), "%s\n", job.ID)
                return nil
            }

            // Lines 179-193: Sync execution (14 lines)
            result, _ := jobStore.RunSkill(...)

            // Lines 194-213: Memory saving and output (19 lines)
            if memoryName != "" {
                rememberResult(ctx, cfg, memoryName, ...)
            }
            fmt.Fprintf(cmd.OutOrStdout(), "%s\n", result)
            return nil
        },
    }
}
```

### Complexity Metrics
- **Lines of Code**: 180
- **Cyclomatic Complexity**: ~15 (should be < 10)
- **Number of Responsibilities**: 9 (should be 1)
- **Number of Variables**: 20+
- **Number of Error Paths**: 12+

## Proposed Solution

### Architecture: Orchestrator Pattern

Create a `RunOrchestrator` that coordinates smaller, focused components:

```go
// cmd/agentctl/cmd/run_orchestrator.go
package cmd

import (
    "context"
    "github.com/jkatigb/agentctl/internal/platform/config"
    "github.com/jkatigb/agentctl/internal/storage"
)

// RunOrchestrator coordinates skill execution with caching, jobs, and memory
type RunOrchestrator struct {
    cfg     config.Config
    cache   storage.CacheStore
    jobs    storage.JobStore
    memory  storage.MemoryStore
}

// RunOptions contains all options for running a skill
type RunOptions struct {
    SkillName     string
    Input         []byte
    WorkspacePath string
    CacheMode     string
    DedupPolicy   string
    Async         bool
    MemoryName    string
    MemoryType    string
    MemorySummary string
}

// NewRunOrchestrator creates a new run orchestrator
func NewRunOrchestrator(cfg config.Config) (*RunOrchestrator, error) {
    cache, err := cache.Open(context.Background(), cfg.Paths.Cache)
    if err != nil {
        return nil, err
    }

    jobs, err := jobs.Open(context.Background(), cfg.Paths.Jobs)
    if err != nil {
        cache.Close()
        return nil, err
    }

    memory, err := memory.Open(context.Background(), cfg.Paths.Memory)
    if err != nil {
        cache.Close()
        jobs.Close()
        return nil, err
    }

    return &RunOrchestrator{
        cfg:    cfg,
        cache:  cache,
        jobs:   jobs,
        memory: memory,
    }, nil
}

// Close closes all resources
func (o *RunOrchestrator) Close() error {
    var errs []error
    if err := o.cache.Close(); err != nil {
        errs = append(errs, err)
    }
    if err := o.jobs.Close(); err != nil {
        errs = append(errs, err)
    }
    if err := o.memory.Close(); err != nil {
        errs = append(errs, err)
    }
    return errors.Join(errs...)
}

// Execute runs a skill with the given options
func (o *RunOrchestrator) Execute(ctx context.Context, opts RunOptions) ([]byte, error) {
    // Step 1: Check cache
    if opts.CacheMode != "off" {
        if result, ok := o.checkCache(ctx, opts); ok {
            return result, nil
        }
    }

    // Step 2: Prepare job (handles deduplication)
    job, isDuplicate, err := o.prepareJob(ctx, opts)
    if err != nil {
        return nil, err
    }

    // Step 3: Handle duplicate based on policy
    if isDuplicate {
        return o.handleDuplicate(ctx, job, opts)
    }

    // Step 4: Execute (async or sync)
    if opts.Async {
        return o.executeAsync(ctx, job, opts)
    }

    return o.executeSync(ctx, job, opts)
}
```

### Extracted Functions

#### 1. Cache Management
```go
// cmd/agentctl/cmd/run_cache.go
package cmd

// checkCache checks if a cached result exists and returns it
func (o *RunOrchestrator) checkCache(ctx context.Context, opts RunOptions) ([]byte, bool) {
    cacheKey := cache.Key(opts.SkillName, opts.Input)
    entry, hit, err := o.cache.Get(ctx, cacheKey)
    if err != nil || !hit {
        return nil, false
    }

    if opts.CacheMode == "only" {
        return entry.Result, true
    }

    // Pin artifacts from cache
    if err := o.pinArtifacts(ctx, entry.Digests); err != nil {
        // Log warning but continue
        return nil, false
    }

    return o.enrichCacheResult(entry.Result, cacheKey, opts.WorkspacePath)
}

// enrichCacheResult adds metadata to cached result
func (o *RunOrchestrator) enrichCacheResult(result []byte, cacheKey, workspace string) ([]byte, bool) {
    var env envelope.Envelope
    if err := json.Unmarshal(result, &env); err != nil {
        return nil, false
    }

    if env.Metadata == nil {
        env.Metadata = make(map[string]interface{})
    }
    env.Metadata["cache_key"] = cacheKey
    env.Metadata["source"] = "cache"
    env.Metadata["workspace"] = workspace

    enriched, _ := json.Marshal(env)
    return enriched, true
}

// pinArtifacts pins all artifacts from a cache entry
func (o *RunOrchestrator) pinArtifacts(ctx context.Context, digests []string) error {
    casStore, err := cas.Open(ctx, o.cfg.Paths.CAS)
    if err != nil {
        return err
    }
    defer casStore.Close()

    for _, digest := range digests {
        if err := casStore.Pin(ctx, digest); err != nil {
            return fmt.Errorf("pin %s: %w", digest, err)
        }
    }
    return nil
}
```

#### 2. Job Management
```go
// cmd/agentctl/cmd/run_jobs.go
package cmd

// prepareJob creates or finds an existing job
func (o *RunOrchestrator) prepareJob(ctx context.Context, opts RunOptions) (storage.Job, bool, error) {
    dedupPolicy := storage.DedupNone
    switch opts.DedupPolicy {
    case "wait":
        dedupPolicy = storage.DedupWait
    case "reject":
        dedupPolicy = storage.DedupReject
    }

    return o.jobs.FindOrPrepareSkillJob(ctx, opts.SkillName, opts.Input, dedupPolicy)
}

// handleDuplicate handles a duplicate job based on dedup policy
func (o *RunOrchestrator) handleDuplicate(ctx context.Context, job storage.Job, opts RunOptions) ([]byte, error) {
    switch opts.DedupPolicy {
    case "reject":
        return nil, fmt.Errorf("duplicate job %s already %s", job.ID, job.State)

    case "wait":
        // Wait for job to complete
        waiter := jobs.NewWaiter(o.jobs, 1*time.Second)
        completed, err := waiter.WaitForCompletion(ctx, job.ID, 5*time.Minute)
        if err != nil {
            return nil, err
        }
        return completed.Result, nil

    default:
        return nil, fmt.Errorf("unexpected duplicate job %s", job.ID)
    }
}
```

#### 3. Execution Paths
```go
// cmd/agentctl/cmd/run_execute.go
package cmd

// executeAsync starts async execution and returns job ID
func (o *RunOrchestrator) executeAsync(ctx context.Context, job storage.Job, opts RunOptions) ([]byte, error) {
    // Job already created and queued by prepareJob
    // Just return the job ID
    return []byte(job.ID), nil
}

// executeSync executes synchronously and returns result
func (o *RunOrchestrator) executeSync(ctx context.Context, job storage.Job, opts RunOptions) ([]byte, error) {
    // Run the skill
    handle, err := findSkill(opts.SkillName)
    if err != nil {
        return nil, err
    }

    manifest, err := skill.Load(handle.ManifestPath)
    if err != nil {
        return nil, err
    }

    result, err := o.jobs.RunSkill(ctx, manifest, handle.ArtifactPath, opts.Input)
    if err != nil {
        return nil, err
    }

    // Save to cache if enabled
    if opts.CacheMode != "off" {
        if err := o.saveToCache(ctx, opts, result); err != nil {
            // Log warning but don't fail
        }
    }

    // Save to memory if requested
    if opts.MemoryName != "" {
        if err := o.saveToMemory(ctx, opts, result); err != nil {
            return nil, err
        }
    }

    return result, nil
}

// saveToCache saves execution result to cache
func (o *RunOrchestrator) saveToCache(ctx context.Context, opts RunOptions, result []byte) error {
    cacheKey := cache.Key(opts.SkillName, opts.Input)

    // Extract digests from result
    digests := artifacts.ExtractDigests(result)

    entry := storage.CacheEntry{
        Key:       cacheKey,
        Workspace: opts.WorkspacePath,
        Result:    result,
        Digests:   digests,
        CreatedAt: time.Now(),
        ExpiresAt: time.Now().Add(o.config.Memory.AutoCacheTTL), // Configurable via config.Memory.AutoCacheTTL
    }

    return o.cache.Put(ctx, entry)
}

// saveToMemory saves execution result to named memory
func (o *RunOrchestrator) saveToMemory(ctx context.Context, opts RunOptions, result []byte) error {
    return o.memory.Save(ctx, opts.MemoryName, opts.MemoryType, opts.WorkspacePath, opts.MemorySummary, result)
}
```

#### 4. Simplified Command Handler
```go
// cmd/agentctl/cmd/run.go (REFACTORED)
package cmd

func newRunCommand() *cobra.Command {
    var (
        async         bool
        cacheMode     string
        dedupPolicy   string
        memoryName    string
        memoryType    string
        memorySummary string
    )

    cmd := &cobra.Command{
        Use:   "run SKILL_NAME [INPUT]",
        Short: "Run a skill",
        Args:  cobra.RangeArgs(1, 2),
        RunE: func(cmd *cobra.Command, args []string) error {
            ctx := cmd.Context()

            // Load configuration
            cfg, err := commandConfig(cmd)
            if err != nil {
                return err
            }

            // Parse input
            input, err := parseInput(cmd, args)
            if err != nil {
                return err
            }

            // Detect workspace
            workspacePath, _ := workspace.Detect(ctx)

            // Create orchestrator
            orchestrator, err := NewRunOrchestrator(cfg)
            if err != nil {
                return err
            }
            defer orchestrator.Close()

            // Build options
            opts := RunOptions{
                SkillName:     args[0],
                Input:         input,
                WorkspacePath: workspacePath,
                CacheMode:     cacheMode,
                DedupPolicy:   dedupPolicy,
                Async:         async,
                MemoryName:    memoryName,
                MemoryType:    memoryType,
                MemorySummary: memorySummary,
            }

            // Execute
            result, err := orchestrator.Execute(ctx, opts)
            if err != nil {
                return err
            }

            // Output result
            fmt.Fprintf(cmd.OutOrStdout(), "%s\n", result)
            return nil
        },
    }

    // Flag definitions...
    cmd.Flags().BoolVar(&async, "async", false, "Run asynchronously")
    cmd.Flags().StringVar(&cacheMode, "cache", "auto", "Cache mode: auto, off, only")
    cmd.Flags().StringVar(&dedupPolicy, "dedup", "none", "Deduplication policy: none, wait, reject")
    cmd.Flags().StringVar(&memoryName, "remember-as", "", "Save result to named memory")
    cmd.Flags().StringVar(&memoryType, "remember-type", "artifact", "Memory entry type")
    cmd.Flags().StringVar(&memorySummary, "remember-summary", "", "Memory entry summary")

    return cmd
}

// parseInput reads input from args or stdin
func parseInput(cmd *cobra.Command, args []string) ([]byte, error) {
    if len(args) == 2 {
        return []byte(args[1]), nil
    }
    return io.ReadAll(cmd.InOrStdin())
}
```

## Implementation Plan

### Step 1: Create RunOptions Struct (1 hour)
- [ ] Create `run_options.go` with RunOptions definition
- [ ] Add validation method to RunOptions
- [ ] Add tests for option validation

### Step 2: Create RunOrchestrator Skeleton (2 hours)
- [ ] Create `run_orchestrator.go` with basic structure
- [ ] Implement constructor and Close method
- [ ] Add tests for resource management

### Step 3: Extract Cache Logic (3 hours)
- [ ] Create `run_cache.go`
- [ ] Implement `checkCache()` method
- [ ] Implement `enrichCacheResult()` method
- [ ] Implement `pinArtifacts()` method
- [ ] Implement `saveToCache()` method
- [ ] Add tests

### Step 4: Extract Job Logic (3 hours)
- [ ] Create `run_jobs.go`
- [ ] Implement `prepareJob()` method
- [ ] Implement `handleDuplicate()` method
- [ ] Add tests

### Step 5: Extract Execution Logic (4 hours)
- [ ] Create `run_execute.go`
- [ ] Implement `executeAsync()` method
- [ ] Implement `executeSync()` method
- [ ] Implement `saveToMemory()` method
- [ ] Add tests

### Step 6: Implement Main Execute Method (2 hours)
- [ ] Implement `Execute()` method in orchestrator
- [ ] Wire up all extracted methods
- [ ] Add integration tests

### Step 7: Refactor run.go Command Handler (2 hours)
- [ ] Simplify RunE function to ~30 lines
- [ ] Use RunOrchestrator
- [ ] Update tests

### Step 8: Cleanup and Documentation (1 hour)
- [ ] Add godoc comments to all public types and methods
- [ ] Update README if needed
- [ ] Remove old code

**Total Estimated Time**: 18 hours

## Testing Strategy

### Unit Tests (per component)
```go
// cmd/agentctl/cmd/run_cache_test.go
func TestCheckCache_Hit(t *testing.T) {
    mockCache := &mockCacheStore{
        entries: map[string]storage.CacheEntry{
            "key1": {Result: []byte(`{"ok": true}`)},
        },
    }

    orchestrator := &RunOrchestrator{cache: mockCache}

    opts := RunOptions{
        SkillName: "test",
        Input:     []byte("input"),
        CacheMode: "auto",
    }

    result, hit := orchestrator.checkCache(context.Background(), opts)
    assert.True(t, hit)
    assert.Contains(t, string(result), "cache_key")
}

func TestCheckCache_Miss(t *testing.T) {
    // Test cache miss scenario
}

func TestCheckCache_OnlyMode(t *testing.T) {
    // Test cache-only mode
}
```

### Integration Tests
```go
// cmd/agentctl/cmd/run_integration_test.go
func TestRunOrchestrator_EndToEnd(t *testing.T) {
    // Create temp directories for cache, jobs, memory
    // Run full execution flow
    // Verify result
}
```

### Table-Driven Tests
```go
func TestExecute_Scenarios(t *testing.T) {
    tests := []struct {
        name     string
        opts     RunOptions
        cacheHit bool
        wantErr  bool
    }{
        {"cache hit", RunOptions{CacheMode: "auto"}, true, false},
        {"cache miss", RunOptions{CacheMode: "auto"}, false, false},
        {"async execution", RunOptions{Async: true}, false, false},
        {"with memory", RunOptions{MemoryName: "test"}, false, false},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test scenario
        })
    }
}
```

## Migration Strategy

### Phase 1: Create New Components (No Breaking Changes)
- Create all new files alongside existing code
- Existing run.go remains functional
- All tests pass

### Phase 2: Wire Up New Components
- Modify run.go RunE to use RunOrchestrator
- Keep old logic commented out for rollback
- Run both old and new side-by-side if possible

### Phase 3: Remove Old Code
- Delete old implementation
- Clean up commented code
- Final test pass

## Benefits

### Before: 180-line monolithic function
- Hard to test individual concerns
- High cyclomatic complexity
- Code duplication between async/sync paths

### After: 7 focused files
- `run.go` (30 lines) - command setup
- `run_options.go` (50 lines) - option validation
- `run_orchestrator.go` (80 lines) - coordination
- `run_cache.go` (100 lines) - cache logic
- `run_jobs.go` (80 lines) - job logic
- `run_execute.go` (120 lines) - execution logic
- Tests (500+ lines) - comprehensive coverage

### Improvements
- ✅ Each file has single responsibility
- ✅ Functions average 10-20 lines
- ✅ Easy to test each concern independently
- ✅ Easy to understand each component
- ✅ Easy to modify without affecting others
- ✅ Reusable components (orchestrator can be used in API)

## Success Criteria

- [ ] run.go RunE function reduced to < 50 lines
- [ ] All functions < 30 lines
- [ ] Cyclomatic complexity < 10 for all functions
- [ ] Test coverage > 80%
- [ ] All existing tests pass
- [ ] At least 20 new unit tests added
- [ ] No functional changes from user perspective

## Related Specs
- SPEC-001: Storage Interfaces (dependency)
- SPEC-007: Replace Long Parameter Lists (RunOptions addresses this)

## References
- Clean Code by Robert C. Martin (Single Responsibility Principle)
- Refactoring by Martin Fowler (Extract Method, Extract Class)
- Go best practices: Small functions, clear responsibilities
