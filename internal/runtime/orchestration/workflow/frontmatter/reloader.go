package frontmatter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const defaultWorkflowFilename = "WORKFLOW.md"

// Snapshot is an immutable point-in-time effective orchestration config.
type Snapshot struct {
	Path     string
	Document Document
	Config   Config
	LoadedAt time.Time
	Version  int64
}

// ValidateFunc validates a decoded config.
type ValidateFunc func(cfg Config) error

// Reloader loads and reloads WORKFLOW.md with last-known-good retention semantics.
type Reloader struct {
	path     string
	opts     DecodeOptions
	validate ValidateFunc
	now      func() time.Time

	mu         sync.RWMutex
	current    Snapshot
	hasCurrent bool
}

// RetainedError indicates reload failed and previous config was retained.
type RetainedError struct {
	Path  string
	Cause error
}

func (e *RetainedError) Error() string {
	if e == nil || e.Cause == nil {
		return "workflow frontmatter reload failed; retained previous config"
	}
	return fmt.Sprintf("workflow frontmatter reload failed for %s; retained previous config: %v", e.Path, e.Cause)
}

func (e *RetainedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// ResolveWorkflowPath resolves explicit path or defaults to ./WORKFLOW.md under cwd.
func ResolveWorkflowPath(explicitPath, cwd string) (string, error) {
	if strings.TrimSpace(explicitPath) != "" {
		path := strings.TrimSpace(explicitPath)
		if !filepath.IsAbs(path) {
			if strings.TrimSpace(cwd) == "" {
				var err error
				cwd, err = os.Getwd()
				if err != nil {
					return "", fmt.Errorf("resolve workflow path: getwd: %w", err)
				}
			}
			path = filepath.Join(cwd, path)
		}
		return filepath.Abs(path)
	}
	if strings.TrimSpace(cwd) == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve workflow path: getwd: %w", err)
		}
	}
	return filepath.Abs(filepath.Join(cwd, defaultWorkflowFilename))
}

// NewReloader creates a reloader for a workflow markdown path.
//
// If validate is nil, ValidateDispatch is used by default.
func NewReloader(path string, opts DecodeOptions, validate ValidateFunc) *Reloader {
	if validate == nil {
		validate = ValidateDispatch
	}
	return &Reloader{
		path:     strings.TrimSpace(path),
		opts:     opts,
		validate: validate,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// Reload attempts to load latest config from disk.
//
// Behavior:
//  1. On success, replaces current snapshot and increments version.
//  2. On failure with existing snapshot, retains current and returns RetainedError.
//  3. On failure with no current snapshot, returns the original error.
func (r *Reloader) Reload() (Snapshot, error) {
	if r == nil {
		return Snapshot{}, fmt.Errorf("workflow frontmatter reloader: nil reloader")
	}
	path := strings.TrimSpace(r.path)
	if path == "" {
		return Snapshot{}, fmt.Errorf("workflow frontmatter reloader: empty path")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("workflow frontmatter reloader: abs path: %w", err)
	}

	doc, err := ParseFile(absPath)
	if err != nil {
		return r.onLoadError(absPath, err)
	}

	decodeOpts := r.opts
	if strings.TrimSpace(decodeOpts.BaseDir) == "" {
		decodeOpts.BaseDir = filepath.Dir(absPath)
	}

	cfg, err := DecodeConfig(doc.Config, decodeOpts)
	if err != nil {
		return r.onLoadError(absPath, err)
	}
	if r.validate != nil {
		if err := r.validate(cfg); err != nil {
			return r.onLoadError(absPath, err)
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	version := int64(1)
	if r.hasCurrent {
		version = r.current.Version + 1
	}
	next := Snapshot{
		Path:     absPath,
		Document: doc,
		Config:   cfg,
		LoadedAt: r.now().UTC(),
		Version:  version,
	}
	r.current = cloneSnapshot(next)
	r.hasCurrent = true
	return cloneSnapshot(next), nil
}

// Current returns a snapshot copy if available.
func (r *Reloader) Current() (Snapshot, bool) {
	if r == nil {
		return Snapshot{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.hasCurrent {
		return Snapshot{}, false
	}
	return cloneSnapshot(r.current), true
}

func (r *Reloader) onLoadError(path string, cause error) (Snapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.hasCurrent {
		return cloneSnapshot(r.current), &RetainedError{
			Path:  path,
			Cause: cause,
		}
	}
	return Snapshot{}, cause
}

func cloneSnapshot(in Snapshot) Snapshot {
	out := in
	out.Document = Document{
		Config:         cloneMapStringAnyDeep(in.Document.Config),
		PromptTemplate: in.Document.PromptTemplate,
		HasFrontMatter: in.Document.HasFrontMatter,
	}
	// Config is value-semantic; copy map fields explicitly.
	out.Config.Agent.MaxConcurrentAgentsByState = cloneMapStringInt(in.Config.Agent.MaxConcurrentAgentsByState)
	out.Config.Tracker.ActiveStates = cloneStringSlice(in.Config.Tracker.ActiveStates)
	out.Config.Tracker.TerminalStates = cloneStringSlice(in.Config.Tracker.TerminalStates)
	out.Config.Tracker.Extra = cloneMapStringString(in.Config.Tracker.Extra)
	if in.Config.Server.Port != nil {
		port := *in.Config.Server.Port
		out.Config.Server.Port = &port
	}
	return out
}

func cloneMapStringAnyDeep(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = cloneAnyDeep(v)
	}
	return out
}

func cloneMapStringInt(in map[string]int) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneMapStringString(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneAnyDeep(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return cloneMapStringAnyDeep(x)
	case []any:
		if len(x) == 0 {
			return []any{}
		}
		out := make([]any, len(x))
		for i := range x {
			out[i] = cloneAnyDeep(x[i])
		}
		return out
	default:
		return x
	}
}
