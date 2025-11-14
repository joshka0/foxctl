# SPEC-009: Extract Skill Discovery Logic

## Status
**Draft** | Priority: Medium | Complexity: Low

## Problem Statement

Skill discovery and resolution logic is currently located in the **cmd layer** (`cmd/agentctl/cmd/skill_helpers.go`), making it:
- Impossible to reuse in other contexts (API, daemon, tests)
- Mixed with CLI concerns
- Hard to test independently
- Not aligned with clean architecture

### Current Location
```
cmd/agentctl/cmd/
└── skill_helpers.go:49-106  # findSkill() function (57 lines)
```

### Problems
1. **Business Logic in Presentation Layer**: Skill finding is domain logic, not CLI logic
2. **Not Reusable**: Cannot use skill discovery in non-CLI contexts
3. **Hard to Test**: Must set up full command context to test
4. **Unclear Ownership**: Is this a command concern or a skill concern?

## Current State Analysis

### findSkill Function
```go
// cmd/agentctl/cmd/skill_helpers.go:49-106
func findSkill(name string) (SkillHandle, error) {
    // 1. Check if it's a filesystem path
    if filepath.IsAbs(name) || strings.Contains(name, "/") {
        manifestPath := name
        if filepath.Base(name) != "skill.yaml" {
            manifestPath = filepath.Join(name, "skill.yaml")
        }

        if _, err := os.Stat(manifestPath); err != nil {
            return SkillHandle{}, fmt.Errorf("skill manifest not found: %s", manifestPath)
        }

        return SkillHandle{
            Name:         filepath.Base(filepath.Dir(manifestPath)),
            ManifestPath: manifestPath,
            ArtifactPath: filepath.Dir(manifestPath),
        }, nil
    }

    // 2. Search in AGENTCTL_SKILLS_PATH
    skillsPath := os.Getenv("AGENTCTL_SKILLS_PATH")
    if skillsPath == "" {
        homeDir, _ := os.UserHomeDir()
        skillsPath = filepath.Join(homeDir, ".agentctl", "skills")
    }

    searchPaths := filepath.SplitList(skillsPath)

    // 3. Add built-in skills directory
    exePath, err := os.Executable()
    if err == nil {
        exeDir := filepath.Dir(exePath)
        builtinPath := filepath.Join(exeDir, "skills")
        searchPaths = append(searchPaths, builtinPath)
    }

    // 4. Search all paths
    for _, basePath := range searchPaths {
        manifestPath := filepath.Join(basePath, name, "skill.yaml")

        if _, err := os.Stat(manifestPath); err == nil {
            return SkillHandle{
                Name:         name,
                ManifestPath: manifestPath,
                ArtifactPath: filepath.Dir(manifestPath),
            }, nil
        }
    }

    return SkillHandle{}, fmt.Errorf("skill not found: %s", name)
}
```

### Usage Locations
- `cmd/agentctl/cmd/run.go:73` - Finding skill to run
- `cmd/agentctl/cmd/skills.go:45` - Listing installed skills
- `cmd/agentctl/cmd/doctor.go:23` - Checking skill availability

## Proposed Solution

### Create Skill Resolver Package

```go
// internal/domain/skill/resolver.go
package skill

import (
    "fmt"
    "os"
    "path/filepath"
    "strings"
)

// Resolver finds skills by name or path
type Resolver struct {
    searchPaths []string
}

// ResolverOption configures a Resolver
type ResolverOption func(*Resolver)

// NewResolver creates a new skill resolver
func NewResolver(opts ...ResolverOption) *Resolver {
    r := &Resolver{
        searchPaths: defaultSearchPaths(),
    }

    for _, opt := range opts {
        opt(r)
    }

    return r
}

// WithSearchPaths sets custom search paths
func WithSearchPaths(paths ...string) ResolverOption {
    return func(r *Resolver) {
        r.searchPaths = paths
    }
}

// WithAdditionalPaths adds paths to the default search paths
func WithAdditionalPaths(paths ...string) ResolverOption {
    return func(r *Resolver) {
        r.searchPaths = append(r.searchPaths, paths...)
    }
}

// Handle represents a resolved skill
type Handle struct {
    Name         string // Skill name
    ManifestPath string // Absolute path to skill.yaml
    ArtifactPath string // Directory containing skill artifact
    Source       string // Where it was found (path, builtin, etc.)
}

// Resolve finds a skill by name or path
func (r *Resolver) Resolve(nameOrPath string) (Handle, error) {
    // 1. Check if it's a filesystem path
    if r.isPath(nameOrPath) {
        return r.resolveFromPath(nameOrPath)
    }

    // 2. Search in configured paths
    return r.resolveFromSearchPaths(nameOrPath)
}

// List returns all discoverable skills
func (r *Resolver) List() ([]Handle, error) {
    var handles []Handle
    seen := make(map[string]bool)

    for _, basePath := range r.searchPaths {
        entries, err := os.ReadDir(basePath)
        if err != nil {
            continue // Path might not exist, skip
        }

        for _, entry := range entries {
            if !entry.IsDir() {
                continue
            }

            name := entry.Name()
            if seen[name] {
                continue // Already found in earlier path
            }

            manifestPath := filepath.Join(basePath, name, "skill.yaml")
            if _, err := os.Stat(manifestPath); err == nil {
                handles = append(handles, Handle{
                    Name:         name,
                    ManifestPath: manifestPath,
                    ArtifactPath: filepath.Dir(manifestPath),
                    Source:       basePath,
                })
                seen[name] = true
            }
        }
    }

    return handles, nil
}

// isPath checks if the name looks like a filesystem path
func (r *Resolver) isPath(name string) bool {
    return filepath.IsAbs(name) || strings.Contains(name, "/") || strings.Contains(name, "\\")
}

// resolveFromPath resolves a skill from an explicit path
func (r *Resolver) resolveFromPath(path string) (Handle, error) {
    manifestPath := path
    if filepath.Base(path) != "skill.yaml" {
        manifestPath = filepath.Join(path, "skill.yaml")
    }

    absPath, err := filepath.Abs(manifestPath)
    if err != nil {
        return Handle{}, fmt.Errorf("absolute path: %w", err)
    }

    if _, err := os.Stat(absPath); err != nil {
        return Handle{}, fmt.Errorf("skill manifest not found: %s", absPath)
    }

    return Handle{
        Name:         filepath.Base(filepath.Dir(absPath)),
        ManifestPath: absPath,
        ArtifactPath: filepath.Dir(absPath),
        Source:       "path",
    }, nil
}

// resolveFromSearchPaths searches configured paths for a skill
func (r *Resolver) resolveFromSearchPaths(name string) (Handle, error) {
    for _, basePath := range r.searchPaths {
        manifestPath := filepath.Join(basePath, name, "skill.yaml")

        if _, err := os.Stat(manifestPath); err == nil {
            return Handle{
                Name:         name,
                ManifestPath: manifestPath,
                ArtifactPath: filepath.Dir(manifestPath),
                Source:       basePath,
            }, nil
        }
    }

    return Handle{}, fmt.Errorf("skill not found: %s (searched: %v)", name, r.searchPaths)
}

// defaultSearchPaths returns the default skill search paths
func defaultSearchPaths() []string {
    var paths []string

    // 1. AGENTCTL_SKILLS_PATH environment variable
    if skillsPath := os.Getenv("AGENTCTL_SKILLS_PATH"); skillsPath != "" {
        paths = append(paths, filepath.SplitList(skillsPath)...)
    }

    // 2. User skills directory (~/.agentctl/skills)
    if homeDir, err := os.UserHomeDir(); err == nil {
        userSkills := filepath.Join(homeDir, ".agentctl", "skills")
        paths = append(paths, userSkills)
    }

    // 3. Built-in skills (relative to executable)
    if exePath, err := os.Executable(); err == nil {
        exeDir := filepath.Dir(exePath)
        builtinPath := filepath.Join(exeDir, "skills")
        paths = append(paths, builtinPath)
    }

    return paths
}
```

### Create Skill Installer

```go
// internal/domain/skill/installer.go
package skill

import (
    "context"
    "fmt"
    "io"
    "os"
    "path/filepath"
)

// Installer manages skill installation
type Installer struct {
    installPath string
}

// NewInstaller creates a new skill installer
func NewInstaller(installPath string) *Installer {
    return &Installer{installPath: installPath}
}

// Install installs a skill from a source
func (i *Installer) Install(ctx context.Context, source string) (Handle, error) {
    // Future: Support installing from:
    // - Local path (copy to install dir)
    // - Git repository
    // - URL (download archive)
    // - Registry (future)

    return Handle{}, fmt.Errorf("not implemented")
}

// Uninstall removes an installed skill
func (i *Installer) Uninstall(name string) error {
    skillPath := filepath.Join(i.installPath, name)

    if _, err := os.Stat(skillPath); os.IsNotExist(err) {
        return fmt.Errorf("skill not installed: %s", name)
    }

    return os.RemoveAll(skillPath)
}
```

### Update Command Layer

```go
// cmd/agentctl/cmd/skill_helpers.go (REFACTORED)
package cmd

import (
    "context"
    "fmt"

    "github.com/jkatigb/agentctl/internal/domain/skill"
    "github.com/jkatigb/agentctl/internal/platform/config"
)

// getSkillResolver creates a resolver from config
func getSkillResolver(cfg config.Config) *skill.Resolver {
    var opts []skill.ResolverOption

    // Add custom search paths from config if configured
    if len(cfg.SkillSearchPaths) > 0 {
        opts = append(opts, skill.WithSearchPaths(cfg.SkillSearchPaths...))
    }

    return skill.NewResolver(opts...)
}

// findSkill finds a skill using the resolver
func findSkill(ctx context.Context, name string) (skill.Handle, error) {
    cfg, err := config.Load(ctx)
    if err != nil {
        return skill.Handle{}, fmt.Errorf("load config: %w", err)
    }
    resolver := getSkillResolver(cfg)
    return resolver.Resolve(name)
}

// Backward compatibility alias
type SkillHandle = skill.Handle

> **Context propagation:** Every CLI entry point must pass `cmd.Context()` (or
> the relevant request context) to `findSkill` so cancellation and deadlines are
> preserved. Avoid `context.Background()` entirely in the presentation layer.
```

### Update Run Command

```go
// cmd/agentctl/cmd/run.go (BEFORE)
handle, err := findSkill(cmd.Context(), skillName)
if err != nil {
    return err
}

manifest, err := skill.Load(handle.ManifestPath)
if err != nil {
    return err
}

// cmd/agentctl/cmd/run.go (AFTER - using resolver directly)
cfg, err := config.Load(cmd.Context())
if err != nil {
    return fmt.Errorf("load config: %w", err)
}
resolver := skill.NewResolver()

handle, err := resolver.Resolve(skillName)
if err != nil {
    return fmt.Errorf("resolve skill: %w", err)
}

manifest, err := skill.Load(handle.ManifestPath)
if err != nil {
    return fmt.Errorf("load manifest: %w", err)
}
```

### Update Skills List Command

```go
// cmd/agentctl/cmd/skills.go (BEFORE)
func listSkills(cmd *cobra.Command, args []string) error {
    // Manual directory traversal logic here...
}

// cmd/agentctl/cmd/skills.go (AFTER)
func listSkills(cmd *cobra.Command, args []string) error {
    resolver := skill.NewResolver()

    handles, err := resolver.List()
    if err != nil {
        return err
    }

    for _, handle := range handles {
        fmt.Fprintf(cmd.OutOrStdout(), "%-20s %s\n", handle.Name, handle.Source)
    }

    return nil
}
```

## Implementation Plan

### Step 1: Create Resolver Package (3 hours)
- [ ] Create `internal/domain/skill/resolver.go`
- [ ] Implement Resolver struct
- [ ] Implement Resolve() method
- [ ] Implement List() method
- [ ] Add comprehensive tests

### Step 2: Create Installer Package (2 hours)
- [ ] Create `internal/domain/skill/installer.go`
- [ ] Implement basic Install() skeleton
- [ ] Implement Uninstall() method
- [ ] Add tests

### Step 3: Update Command Layer (2 hours)
- [ ] Refactor skill_helpers.go to use Resolver
- [ ] Update run.go to use Resolver
- [ ] Update skills.go to use Resolver
- [ ] Update doctor.go to use Resolver

### Step 4: Add Configuration Support (1 hour)
- [ ] Add SkillSearchPaths to config
- [ ] Update config loading
- [ ] Add tests

### Step 5: Testing (2 hours)
- [ ] Test resolver with various path formats
- [ ] Test search path ordering
- [ ] Test List() with duplicates
- [ ] Integration tests

### Step 6: Documentation (0.5 hours)
- [ ] Add godoc to all public APIs
- [ ] Add usage examples
- [ ] Update user documentation

**Total Estimated Time**: 10.5 hours

## Testing Strategy

### Unit Tests
```go
// internal/domain/skill/resolver_test.go
func TestResolver_Resolve_AbsolutePath(t *testing.T) {
    tmpDir := t.TempDir()
    skillDir := filepath.Join(tmpDir, "myskill")
    os.MkdirAll(skillDir, 0755)

    manifestPath := filepath.Join(skillDir, "skill.yaml")
    os.WriteFile(manifestPath, []byte("name: myskill\n"), 0644)

    resolver := NewResolver()

    handle, err := resolver.Resolve(skillDir)

    require.NoError(t, err)
    assert.Equal(t, "myskill", handle.Name)
    assert.Equal(t, manifestPath, handle.ManifestPath)
    assert.Equal(t, "path", handle.Source)
}

func TestResolver_Resolve_FromSearchPaths(t *testing.T) {
    tmpDir := t.TempDir()
    skillDir := filepath.Join(tmpDir, "echo")
    os.MkdirAll(skillDir, 0755)
    os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: echo\n"), 0644)

    resolver := NewResolver(WithSearchPaths(tmpDir))

    handle, err := resolver.Resolve("echo")

    require.NoError(t, err)
    assert.Equal(t, "echo", handle.Name)
    assert.Equal(t, tmpDir, handle.Source)
}

func TestResolver_List(t *testing.T) {
    tmpDir := t.TempDir()

    // Create multiple skills
    createSkill(t, tmpDir, "skill1")
    createSkill(t, tmpDir, "skill2")
    createSkill(t, tmpDir, "skill3")

    resolver := NewResolver(WithSearchPaths(tmpDir))

    handles, err := resolver.List()

    require.NoError(t, err)
    assert.Len(t, handles, 3)

    names := []string{handles[0].Name, handles[1].Name, handles[2].Name}
    assert.Contains(t, names, "skill1")
    assert.Contains(t, names, "skill2")
    assert.Contains(t, names, "skill3")
}

func TestResolver_List_HandlesDuplicates(t *testing.T) {
    dir1 := t.TempDir()
    dir2 := t.TempDir()

    // Same skill in both directories - should return only first
    createSkill(t, dir1, "echo")
    createSkill(t, dir2, "echo")

    resolver := NewResolver(WithSearchPaths(dir1, dir2))

    handles, err := resolver.List()

    require.NoError(t, err)
    assert.Len(t, handles, 1)
    assert.Equal(t, "echo", handles[0].Name)
    assert.Equal(t, dir1, handles[0].Source) // First path wins
}

func createSkill(t *testing.T, base, name string) {
    skillDir := filepath.Join(base, name)
    os.MkdirAll(skillDir, 0755)
    os.WriteFile(filepath.Join(skillDir, "skill.yaml"),
        []byte(fmt.Sprintf("name: %s\n", name)), 0644)
}
```

### Integration Tests
```go
// cmd/agentctl/cmd/skills_integration_test.go
func TestSkillsCommand_ListsInstalledSkills(t *testing.T) {
    // Setup test environment with skills
    tmpDir := t.TempDir()
    os.Setenv("AGENTCTL_SKILLS_PATH", tmpDir)

    createSkill(t, tmpDir, "fs_ls")
    createSkill(t, tmpDir, "text_grep")

    // Run skills list command
    cmd := newSkillsCommand()
    var output bytes.Buffer
    cmd.SetOut(&output)

    err := cmd.Execute()

    require.NoError(t, err)
    assert.Contains(t, output.String(), "fs_ls")
    assert.Contains(t, output.String(), "text_grep")
}
```

## Benefits

### Before: Logic in CMD
```go
// cmd/agentctl/cmd/skill_helpers.go
func findSkill(name string) (SkillHandle, error) {
    // 57 lines of discovery logic in presentation layer
}
```

**Problems**:
- Cannot reuse outside CLI
- Hard to test
- Mixed concerns

### After: Clean Domain Layer
```go
// internal/domain/skill/resolver.go
type Resolver struct {
    searchPaths []string
}

func (r *Resolver) Resolve(name string) (Handle, error) {
    // Reusable, testable business logic
}
```

**Benefits**:
- ✅ Reusable in API, daemon, tests
- ✅ Easy to test independently
- ✅ Clear separation of concerns
- ✅ Configurable and extensible

### Improvements
- ✅ **Business logic in domain layer**
- ✅ **Testable without CLI context**
- ✅ **Reusable across application**
- ✅ **Configurable search paths**
- ✅ **Clean architecture**
- ✅ **Easy to extend (installer, registry)**

## Future Enhancements

### Skill Registry Support
```go
type RegistryResolver struct {
    registry SkillRegistry
}

func (r *RegistryResolver) Resolve(name string) (Handle, error) {
    // Fetch from remote registry
}
```

### Skill Caching
```go
type CachedResolver struct {
    resolver Resolver
    cache    map[string]Handle
}

func (r *CachedResolver) Resolve(name string) (Handle, error) {
    if handle, ok := r.cache[name]; ok {
        return handle, nil
    }
    // ...
}
```

## Success Criteria

- [ ] Resolver package created in domain/skill
- [ ] All discovery logic moved from cmd layer
- [ ] All commands use Resolver
- [ ] Backward compatibility maintained
- [ ] 90%+ test coverage for resolver
- [ ] Integration tests pass
- [ ] Documentation complete

## Related Specs
- SPEC-008: Reorganize Packages (Resolver goes in domain/skill/)
- SPEC-002: Refactor Run Command (uses Resolver)

## References
- Clean Architecture: Domain logic in domain layer
- Dependency Inversion: Commands depend on domain, not vice versa
- Service Locator Pattern (for skill discovery)
