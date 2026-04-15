package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Loader discovers and loads workflow definitions.
type Loader struct {
	searchPaths []string
}

// LoaderOption configures a Loader.
type LoaderOption func(*Loader)

// WithWorkflowPaths sets custom search paths.
func WithWorkflowPaths(paths ...string) LoaderOption {
	return func(l *Loader) {
		l.searchPaths = paths
	}
}

// NewLoader creates a workflow loader.
func NewLoader(opts ...LoaderOption) *Loader {
	l := &Loader{
		searchPaths: defaultWorkflowPaths(),
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// defaultWorkflowPaths returns the default search paths for workflows.
func defaultWorkflowPaths() []string {
	var paths []string

	// 1. FOXCTL_WORKFLOW_PATH environment variable
	if wfPath := os.Getenv("FOXCTL_WORKFLOW_PATH"); wfPath != "" {
		paths = append(paths, filepath.SplitList(wfPath)...)
	}

	// 2. User workflows directory (~/.foxctl/workflows)
	if homeDir, err := os.UserHomeDir(); err == nil {
		userWorkflows := filepath.Join(homeDir, ".foxctl", "workflows")
		paths = append(paths, userWorkflows)
	}

	// 3. Built-in workflows (relative to executable)
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		builtinPath := filepath.Join(exeDir, "workflows")
		paths = append(paths, builtinPath)
	}

	// 4. Development paths
	if pwd, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(pwd, "workflows"))
	}

	return paths
}

// Handle represents a resolved workflow location.
type Handle struct {
	Name     string // Workflow name
	Path     string // Absolute path to workflow file
	Workflow *Workflow
}

// Load loads a workflow by name or path.
func (l *Loader) Load(nameOrPath string) (*Handle, error) {
	// Check if it's a path
	if l.isPath(nameOrPath) {
		return l.loadFromPath(nameOrPath)
	}

	// Search in configured paths
	return l.loadFromSearchPaths(nameOrPath)
}

// LoadFile loads a workflow from a specific file path.
func (l *Loader) LoadFile(path string) (*Workflow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read workflow file: %w", err)
	}

	return Parse(data)
}

// Parse parses workflow YAML data.
func Parse(data []byte) (*Workflow, error) {
	var wf Workflow
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return nil, fmt.Errorf("parse workflow YAML: %w", err)
	}

	// Validate
	if err := Validate(&wf); err != nil {
		return nil, err
	}

	return &wf, nil
}

// Validate checks a workflow for errors.
func Validate(wf *Workflow) error {
	if wf.APIVersion == "" {
		return fmt.Errorf("missing apiVersion")
	}
	if wf.APIVersion != APIVersion {
		return fmt.Errorf("unsupported apiVersion: %s (expected %s)", wf.APIVersion, APIVersion)
	}
	if wf.Kind == "" {
		return fmt.Errorf("missing kind")
	}
	if wf.Kind != Kind {
		return fmt.Errorf("invalid kind: %s (expected %s)", wf.Kind, Kind)
	}
	if wf.Metadata.Name == "" {
		return fmt.Errorf("missing metadata.name")
	}
	if len(wf.Steps) == 0 {
		return fmt.Errorf("workflow has no steps")
	}

	// Validate each step
	stepIDs := make(map[string]bool)
	for i, step := range wf.Steps {
		if step.ID == "" {
			return fmt.Errorf("step %d has no ID", i)
		}
		if stepIDs[step.ID] {
			return fmt.Errorf("duplicate step ID: %s", step.ID)
		}
		stepIDs[step.ID] = true

		if step.Skill == "" {
			return fmt.Errorf("step %s has no skill", step.ID)
		}

		// Note: Dependencies are validated later during DAG building,
		// as steps might be defined after they're referenced.

		// Validate loop config
		if step.Loop != nil {
			if step.Loop.Over == "" {
				return fmt.Errorf("step %s: loop.over is required", step.ID)
			}
			if step.Loop.As == "" {
				return fmt.Errorf("step %s: loop.as is required", step.ID)
			}
		}
	}

	return nil
}

// List returns all discoverable workflows.
func (l *Loader) List() ([]Handle, error) {
	handles := make([]Handle, 0) // Initialize as empty slice for JSON serialization
	seen := make(map[string]bool)

	for _, basePath := range l.searchPaths {
		entries, err := os.ReadDir(basePath)
		if err != nil {
			continue // Path might not exist
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			name := entry.Name()
			if !isWorkflowFile(name) {
				continue
			}

			// Extract workflow name (without extension)
			wfName := strings.TrimSuffix(name, filepath.Ext(name))
			if seen[wfName] {
				continue
			}

			path := filepath.Join(basePath, name)
			wf, err := l.LoadFile(path)
			if err != nil {
				continue // Skip invalid workflows
			}

			handles = append(handles, Handle{
				Name:     wfName,
				Path:     path,
				Workflow: wf,
			})
			seen[wfName] = true
		}
	}

	return handles, nil
}

// SearchPaths returns the configured search paths.
func (l *Loader) SearchPaths() []string {
	return append([]string{}, l.searchPaths...)
}

// isPath checks if the name looks like a filesystem path.
func (l *Loader) isPath(name string) bool {
	if filepath.IsAbs(name) {
		return true
	}
	if strings.HasPrefix(name, "./") || strings.HasPrefix(name, "../") {
		return true
	}
	if strings.Contains(name, "/") && (strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")) {
		return true
	}
	return false
}

// loadFromPath loads a workflow from an explicit path.
func (l *Loader) loadFromPath(path string) (*Handle, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("absolute path: %w", err)
	}

	wf, err := l.LoadFile(absPath)
	if err != nil {
		return nil, err
	}

	return &Handle{
		Name:     wf.Metadata.Name,
		Path:     absPath,
		Workflow: wf,
	}, nil
}

// loadFromSearchPaths searches configured paths for a workflow.
func (l *Loader) loadFromSearchPaths(name string) (*Handle, error) {
	extensions := []string{".yaml", ".yml"}

	for _, basePath := range l.searchPaths {
		for _, ext := range extensions {
			path := filepath.Join(basePath, name+ext)
			if _, err := os.Stat(path); err == nil {
				wf, err := l.LoadFile(path)
				if err != nil {
					return nil, err
				}
				return &Handle{
					Name:     name,
					Path:     path,
					Workflow: wf,
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("workflow not found: %s (searched: %v)", name, l.searchPaths)
}

// isWorkflowFile checks if a filename is a workflow file.
func isWorkflowFile(name string) bool {
	ext := filepath.Ext(name)
	return ext == ".yaml" || ext == ".yml"
}
