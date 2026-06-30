package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Manifest mirrors skill.yaml structure.
type Manifest struct {
	APIVersion   string         `yaml:"apiVersion" json:"apiVersion"`
	Kind         string         `yaml:"kind" json:"kind"`
	Metadata     Metadata       `yaml:"metadata" json:"metadata"`
	Distribution Distribution   `yaml:"distribution" json:"distribution"`
	IO           IOConfig       `yaml:"io" json:"io"`
	Signature    Signature      `yaml:"signature" json:"signature"`
	Returns      []Parameter    `yaml:"returns,omitempty" json:"returns,omitempty"`
	Capabilities Capabilities   `yaml:"capabilities" json:"capabilities"`
	Memory       MemoryConfig   `yaml:"memory" json:"memory"`
	OpenAPI      *OpenAPIConfig `yaml:"openapi,omitempty" json:"openapi,omitempty"`
}

// Metadata describes the human-facing info for a skill.
type Metadata struct {
	Name        string   `yaml:"name" json:"name"`
	Version     string   `yaml:"version" json:"version"`
	Description string   `yaml:"description" json:"description"`
	Tags        []string `yaml:"tags" json:"tags"`
}

// Distribution declares how the skill binary/module is shipped.
type Distribution struct {
	Type string            `yaml:"type" json:"type"`
	Exec *ExecDistribution `yaml:"exec" json:"exec"`
	WASI *WASIDistribution `yaml:"wasi" json:"wasi"`
}

// ExecDistribution points to a native binary entry.
type ExecDistribution struct {
	Entry string `yaml:"entry" json:"entry"`
}

// WASIDistribution references a wasm module.
type WASIDistribution struct {
	Module string `yaml:"module" json:"module"`
}

// IOConfig controls envelope I/O limits.
type IOConfig struct {
	Format         string `yaml:"format" json:"format"`
	InlineOutputKB int    `yaml:"inline_output_kb" json:"inline_output_kb"`
}

// Signature declares command name and parameters.
type Signature struct {
	Command    string      `yaml:"command" json:"command"`
	Parameters []Parameter `yaml:"parameters" json:"parameters"`
	Returns    []Parameter `yaml:"returns" json:"returns"`
	Help       *Help       `yaml:"help,omitempty" json:"help,omitempty"`
}

// Parameter defines a single input or output field.
type Parameter struct {
	Name        string               `yaml:"name" json:"name"`
	Type        string               `yaml:"type" json:"type"`
	Required    bool                 `yaml:"required" json:"required"`
	Description string               `yaml:"description" json:"description"`
	Default     any                  `yaml:"default" json:"default"`
	Enum        []string             `yaml:"enum,omitempty" json:"enum,omitempty"`
	Items       *Parameter           `yaml:"items,omitempty" json:"items,omitempty"`
	Properties  map[string]Parameter `yaml:"properties,omitempty" json:"properties,omitempty"`
}

// Help describes skill-level help and example workflows.
type Help struct {
	Short     string         `yaml:"short,omitempty" json:"short,omitempty"`
	Long      string         `yaml:"long,omitempty" json:"long,omitempty"`
	Workflows []HelpWorkflow `yaml:"workflows,omitempty" json:"workflows,omitempty"`
}

// HelpWorkflow defines a named workflow and example input payload.
type HelpWorkflow struct {
	ID           string         `yaml:"id,omitempty" json:"id,omitempty"`
	Description  string         `yaml:"description,omitempty" json:"description,omitempty"`
	ExampleInput map[string]any `yaml:"example_input,omitempty" json:"example_input,omitempty"`
}

// Capabilities describe network/fs policies.
type Capabilities struct {
	Network     string       `yaml:"network" json:"network"`
	EgressAllow []string     `yaml:"egressAllow,omitempty" json:"egressAllow,omitempty"`
	Filesystem  []FileAccess `yaml:"filesystem" json:"filesystem"`
	Pure        bool         `yaml:"pure" json:"pure"`
	Cacheable   *bool        `yaml:"cacheable,omitempty" json:"cacheable,omitempty"` // nil=cacheable, false=skip cache
}

// FileAccess grants access to specific filesystem roots.
type FileAccess struct {
	Type string `yaml:"type" json:"type"`
	Path string `yaml:"path,omitempty" json:"path,omitempty"`
}

// MemoryConfig hints how results integrate with memory/cache.
type MemoryConfig struct {
	Recommend  bool   `yaml:"recommend" json:"recommend"`
	DefaultTTL string `yaml:"default_ttl" json:"default_ttl"`
}

// OpenAPIConfig declares REST API exposure for a skill.
// Skills must opt-in by setting enabled: true.
type OpenAPIConfig struct {
	// Enabled opts the skill into REST API exposure.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// Methods maps HTTP methods to operation values.
	// Example: {"GET": "list", "POST": "add", "PUT": "update", "DELETE": "complete"}
	// For skills without an operation param, use "true" to enable the method.
	Methods map[string]string `yaml:"methods,omitempty" json:"methods,omitempty"`

	// BasePath overrides the default /api/skills/{command} path.
	// Example: "/api/todos" instead of "/api/skills/todo/manage"
	BasePath string `yaml:"base_path,omitempty" json:"base_path,omitempty"`

	// IDParam specifies the name of the ID parameter for resource routes.
	// Defaults to "id". Used for GET/PUT/DELETE /{id} routes.
	IDParam string `yaml:"id_param,omitempty" json:"id_param,omitempty"`
}

// SupportsMethod checks if the skill supports a given HTTP method.
func (o *OpenAPIConfig) SupportsMethod(method string) bool {
	if o == nil || !o.Enabled {
		return false
	}
	if o.Methods == nil {
		return false
	}
	_, ok := o.Methods[method]
	return ok
}

// OperationForMethod returns the operation value for a given HTTP method.
// Returns empty string if not configured.
func (o *OpenAPIConfig) OperationForMethod(method string) string {
	if o == nil || o.Methods == nil {
		return ""
	}
	return o.Methods[method]
}

// GetIDParam returns the ID parameter name, defaulting to "id".
func (o *OpenAPIConfig) GetIDParam() string {
	if o == nil || o.IDParam == "" {
		return "id"
	}
	return o.IDParam
}

// LoadManifest reads and validates a manifest.
func LoadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return Manifest{}, err
	}
	if err := normalizeManifest(&m); err != nil {
		return Manifest{}, err
	}
	if err := validateManifest(m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func normalizeManifest(m *Manifest) error {
	if len(m.Returns) == 0 {
		return nil
	}
	if len(m.Signature.Returns) > 0 {
		return fmt.Errorf("returns must be declared in only one location")
	}
	m.Signature.Returns = m.Returns
	m.Returns = nil
	return nil
}

func validateManifest(m Manifest) error {
	if m.APIVersion == "" || m.Kind != "Skill" {
		return fmt.Errorf("invalid apiVersion/kind")
	}
	if !strings.Contains(m.Metadata.Name, "/") {
		return fmt.Errorf("metadata.name must include namespace (e.g. text/grep)")
	}
	if m.Metadata.Version == "" {
		return fmt.Errorf("version required")
	}
	switch m.Distribution.Type {
	case "exec":
		if m.Distribution.Exec == nil || m.Distribution.Exec.Entry == "" {
			return fmt.Errorf("exec entry required")
		}
	case "wasi":
		if m.Distribution.WASI == nil || m.Distribution.WASI.Module == "" {
			return fmt.Errorf("wasi module required")
		}
	case "k8s-sandbox":
		// k8s-sandbox uses the exec.entry as the command to run inside
		// the sandbox pod. No local binary is needed.
		if m.Distribution.Exec == nil || m.Distribution.Exec.Entry == "" {
			return fmt.Errorf("exec entry required (command for sandbox)")
		}
	default:
		return fmt.Errorf("unknown distribution type %q", m.Distribution.Type)
	}
	if m.Signature.Command == "" {
		return fmt.Errorf("signature.command required")
	}
	if err := validateIOConfig(m.IO); err != nil {
		return err
	}
	if err := validateParameters("signature.parameters", m.Signature.Parameters, true); err != nil {
		return err
	}
	if err := validateParameters("signature.returns", m.Signature.Returns, true); err != nil {
		return err
	}
	if err := ValidateWASIPolicy(m); err != nil {
		return fmt.Errorf("policy validation failed: %w", err)
	}
	if err := ValidateFilesystemCapabilities(m); err != nil {
		return fmt.Errorf("filesystem capabilities validation failed: %w", err)
	}
	return nil
}

func validateIOConfig(io IOConfig) error {
	if io.Format != "" && strings.ToUpper(strings.TrimSpace(io.Format)) != "JSON" {
		return fmt.Errorf("io.format must be JSON")
	}
	if io.InlineOutputKB < 0 {
		return fmt.Errorf("io.inline_output_kb must be non-negative")
	}
	return nil
}

func validateParameters(scope string, params []Parameter, requireName bool) error {
	seen := make(map[string]struct{}, len(params))
	for i := range params {
		param := &params[i]
		name := strings.TrimSpace(param.Name)
		if requireName && name == "" {
			return fmt.Errorf("%s[%d].name required", scope, i)
		}
		if name != "" {
			if _, ok := seen[name]; ok {
				return fmt.Errorf("duplicate %s name %q", scope, name)
			}
			seen[name] = struct{}{}
		}

		param.Name = name
		if err := validateParameterType(parameterScope(scope, i, name), param); err != nil {
			return err
		}
	}
	return nil
}

func parameterScope(scope string, index int, name string) string {
	if name == "" {
		return fmt.Sprintf("%s[%d]", scope, index)
	}
	return fmt.Sprintf("%s[%q]", scope, name)
}

func validateParameterType(scope string, param *Parameter) error {
	typ := strings.ToLower(strings.TrimSpace(param.Type))
	if typ == "" {
		return fmt.Errorf("%s.type required", scope)
	}
	switch typ {
	case "any", "array", "boolean", "integer", "number", "object", "string":
		param.Type = typ
	default:
		return fmt.Errorf("%s.type %q unsupported", scope, param.Type)
	}

	if param.Items != nil {
		if err := validateParameterType(scope+".items", param.Items); err != nil {
			return err
		}
	}
	if len(param.Properties) > 0 {
		for name, child := range param.Properties {
			child.Name = strings.TrimSpace(child.Name)
			if child.Name == "" {
				child.Name = strings.TrimSpace(name)
			}
			if err := validateParameterType(scope+".properties."+name, &child); err != nil {
				return err
			}
			param.Properties[name] = child
		}
	}
	return nil
}

// Discover finds manifests under root (skill.yaml files).
func Discover(root string) ([]Manifest, error) {
	var manifests []Manifest
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Base(path) != "skill.yaml" {
			return nil
		}
		m, err := LoadManifest(path)
		if err != nil {
			return err
		}
		manifests = append(manifests, m)
		return nil
	})
	return manifests, err
}
