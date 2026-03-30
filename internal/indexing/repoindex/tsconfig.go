package repoindex

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type tsResolver struct {
	repoRoot        string
	mu              sync.RWMutex
	moduleRootCache map[string]string
	configCache     map[string]*tsConfigResolved
}

type tsConfig struct {
	CompilerOptions tsCompilerOptions `json:"compilerOptions"`
	References      []tsConfigRef     `json:"references"`
	Extends         string            `json:"extends"`
}

type tsCompilerOptions struct {
	BaseURL string              `json:"baseUrl"`
	Paths   map[string][]string `json:"paths"`
}

type tsConfigRef struct {
	Path string `json:"path"`
}

type tsConfigResolved struct {
	baseDir string
	baseURL string
	paths   map[string][]string
}

func newTSResolver(repoRoot string) *tsResolver {
	absRoot := repoRoot
	if absRoot == "" {
		absRoot = "."
	}
	if resolved, err := filepath.Abs(absRoot); err == nil {
		absRoot = resolved
	}

	return &tsResolver{
		repoRoot:        filepath.Clean(absRoot),
		moduleRootCache: make(map[string]string),
		configCache:     make(map[string]*tsConfigResolved),
	}
}

// ModuleRoot resolves the TypeScript module root for a file path.
func (r *tsResolver) ModuleRoot(filePath string) (string, error) {
	absPath, err := r.absPath(filePath)
	if err != nil {
		return "", err
	}

	dir := filepath.Dir(absPath)
	r.mu.Lock()
	if cached, ok := r.moduleRootCache[dir]; ok {
		r.mu.Unlock()
		return cached, nil
	}
	r.mu.Unlock()

	root := r.findModuleRoot(dir)

	r.mu.Lock()
	r.moduleRootCache[dir] = root
	r.mu.Unlock()

	return root, nil
}

// ResolveImportPackage resolves a TypeScript import to a repoindex package ID.
func (r *tsResolver) ResolveImportPackage(filePath, importPath string) string {
	if importPath == "" {
		return ""
	}

	absPath, err := r.absPath(filePath)
	if err != nil {
		return ""
	}

	if target := r.ResolveImportFile(absPath, importPath); target != "" {
		return r.packageForFile(target)
	}

	return npmPackageID(importPath)
}

// ResolveImportFile resolves a TypeScript import to a concrete local file path.
// It returns an absolute path when the import maps into the workspace.
func (r *tsResolver) ResolveImportFile(filePath, importPath string) string {
	if importPath == "" {
		return ""
	}

	absPath, err := r.absPath(filePath)
	if err != nil {
		return ""
	}

	if isRelativeImport(importPath) || strings.HasPrefix(importPath, "/") {
		return r.resolveLocalImport(absPath, importPath)
	}

	if cfg := r.configForFile(absPath); cfg != nil {
		if target := cfg.ResolveImport(importPath); target != "" {
			return target
		}
	}

	return ""
}

func (r *tsResolver) configForFile(absFile string) *tsConfigResolved {
	moduleRoot, err := r.ModuleRoot(absFile)
	if err != nil {
		return nil
	}

	startDir := filepath.Dir(absFile)
	configPath := r.findTSConfig(startDir, moduleRoot)
	if configPath == "" {
		return nil
	}

	r.mu.RLock()
	cached := r.configCache[configPath]
	r.mu.RUnlock()
	if cached != nil {
		return cached
	}

	resolved, err := r.loadConfig(configPath)
	if err != nil {
		return nil
	}

	r.mu.Lock()
	r.configCache[configPath] = resolved
	r.mu.Unlock()

	return resolved
}

func (r *tsResolver) loadConfig(configPath string) (*tsConfigResolved, error) {
	config, err := readTSConfig(configPath)
	if err != nil {
		return nil, err
	}

	resolved := resolveTSConfig(configPath, config)
	if resolved == nil {
		return nil, fmt.Errorf("repoindex: empty tsconfig %s", configPath)
	}

	if resolved.baseURL == "" && len(resolved.paths) == 0 && len(config.References) > 0 {
		for _, ref := range config.References {
			refPath := resolveTSConfigPath(filepath.Dir(configPath), ref.Path)
			refConfig, err := readTSConfig(refPath)
			if err != nil {
				continue
			}
			refResolved := resolveTSConfig(refPath, refConfig)
			if refResolved == nil {
				continue
			}
			resolved.merge(refResolved)
		}
	}

	if resolved.baseURL == "" && len(resolved.paths) == 0 && config.Extends != "" {
		extPath := resolveTSConfigPath(filepath.Dir(configPath), config.Extends)
		if extConfig, err := readTSConfig(extPath); err == nil {
			extResolved := resolveTSConfig(extPath, extConfig)
			if extResolved != nil {
				resolved.merge(extResolved)
			}
		}
	}

	return resolved, nil
}

func (r *tsResolver) resolveLocalImport(absFile, importPath string) string {
	var base string
	if strings.HasPrefix(importPath, "/") {
		base = filepath.Join(r.repoRoot, filepath.FromSlash(strings.TrimPrefix(importPath, "/")))
	} else {
		base = filepath.Join(filepath.Dir(absFile), filepath.FromSlash(importPath))
	}
	return resolveTSFile(base)
}

func (r *tsResolver) packageForFile(absPath string) string {
	moduleRoot, err := r.ModuleRoot(absPath)
	if err != nil {
		return ""
	}
	moduleRelPath, ok := relPath(r.repoRoot, moduleRoot)
	if !ok {
		moduleRelPath = "."
	}
	return tsLocalPrefix + moduleRelPath
}

func (r *tsResolver) findModuleRoot(startDir string) string {
	current := filepath.Clean(startDir)
	stop := filepath.Clean(r.repoRoot)

	for {
		if hasFile(current, "package.json") {
			return current
		}
		if hasFile(current, "tsconfig.json") {
			return current
		}
		if current == stop {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	return stop
}

func (r *tsResolver) findTSConfig(startDir, stopDir string) string {
	current := filepath.Clean(startDir)
	stop := filepath.Clean(stopDir)

	for {
		path := filepath.Join(current, "tsconfig.json")
		if fileExists(path) {
			return path
		}
		if current == stop {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	return ""
}

func (r *tsResolver) absPath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("repoindex: empty path")
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	return filepath.Abs(filepath.Join(r.repoRoot, path))
}

func resolveTSConfig(path string, config *tsConfig) *tsConfigResolved {
	if config == nil {
		return nil
	}
	baseDir := filepath.Dir(path)
	baseURL := strings.TrimSpace(config.CompilerOptions.BaseURL)
	resolved := &tsConfigResolved{
		baseDir: baseDir,
		baseURL: baseURL,
		paths:   make(map[string][]string),
	}
	for key, values := range config.CompilerOptions.Paths {
		resolved.paths[key] = append([]string(nil), values...)
	}
	return resolved
}

func (c *tsConfigResolved) merge(other *tsConfigResolved) {
	if other == nil {
		return
	}
	if c.baseURL == "" {
		c.baseURL = other.baseURL
	}
	for key, values := range other.paths {
		if _, exists := c.paths[key]; !exists {
			c.paths[key] = append([]string(nil), values...)
		}
	}
}

// ResolveImport resolves a TypeScript import path to a concrete file path.
func (c *tsConfigResolved) ResolveImport(importPath string) string {
	if importPath == "" {
		return ""
	}
	baseRoot := c.baseDir
	if c.baseURL != "" {
		baseRoot = filepath.Join(c.baseDir, filepath.FromSlash(c.baseURL))
	}

	for pattern, targets := range c.paths {
		match, ok := matchPathPattern(pattern, importPath)
		if !ok {
			continue
		}
		for _, target := range targets {
			replaced := strings.Replace(target, "*", match, 1)
			candidate := filepath.Join(baseRoot, filepath.FromSlash(replaced))
			if resolved := resolveTSFile(candidate); resolved != "" {
				return resolved
			}
		}
	}

	if c.baseURL != "" {
		candidate := filepath.Join(baseRoot, filepath.FromSlash(importPath))
		if resolved := resolveTSFile(candidate); resolved != "" {
			return resolved
		}
	}

	return ""
}

func matchPathPattern(pattern, value string) (string, bool) {
	if !strings.Contains(pattern, "*") {
		if pattern == value {
			return "", true
		}
		return "", false
	}

	parts := strings.Split(pattern, "*")
	if len(parts) != 2 {
		return "", false
	}
	prefix := parts[0]
	suffix := parts[1]
	if !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, suffix) {
		return "", false
	}
	start := len(prefix)
	end := len(value) - len(suffix)
	if start > end {
		return "", false
	}
	return value[start:end], true
}

func resolveTSConfigPath(baseDir, ref string) string {
	if ref == "" {
		return ""
	}
	if filepath.IsAbs(ref) {
		return ref
	}
	if strings.HasSuffix(ref, ".json") {
		return filepath.Join(baseDir, ref)
	}
	return filepath.Join(baseDir, ref, "tsconfig.json")
}

func readTSConfig(path string) (*tsConfig, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cleaned := stripJSONComments(content)
	var config tsConfig
	if err := json.Unmarshal(cleaned, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

func stripJSONComments(input []byte) []byte {
	out := make([]byte, 0, len(input))
	inString := false
	inLineComment := false
	inBlockComment := false
	escape := false

	for i := 0; i < len(input); i++ {
		c := input[i]
		if inLineComment {
			if c == '\n' {
				inLineComment = false
				out = append(out, c)
			}
			continue
		}
		if inBlockComment {
			if c == '*' && i+1 < len(input) && input[i+1] == '/' {
				inBlockComment = false
				i++
			}
			continue
		}

		if inString {
			out = append(out, c)
			if escape {
				escape = false
				continue
			}
			if c == '\\' {
				escape = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}

		if c == '"' {
			inString = true
			out = append(out, c)
			continue
		}
		if c == '/' && i+1 < len(input) && input[i+1] == '/' {
			inLineComment = true
			i++
			continue
		}
		if c == '/' && i+1 < len(input) && input[i+1] == '*' {
			inBlockComment = true
			i++
			continue
		}

		out = append(out, c)
	}

	return out
}

func resolveTSFile(path string) string {
	path = filepath.Clean(path)
	if fileExists(path) {
		return path
	}

	extensions := []string{".ts", ".tsx", ".js", ".jsx", ".d.ts"}
	for _, ext := range extensions {
		candidate := path + ext
		if fileExists(candidate) {
			return candidate
		}
	}

	for _, ext := range extensions {
		candidate := filepath.Join(path, "index"+ext)
		if fileExists(candidate) {
			return candidate
		}
	}

	return ""
}

func npmPackageID(importPath string) string {
	if importPath == "" {
		return ""
	}
	trimmed := strings.TrimSpace(importPath)
	if strings.HasPrefix(trimmed, "@") {
		parts := strings.Split(trimmed, "/")
		if len(parts) >= 2 {
			return tsNpmPrefix + parts[0] + "/" + parts[1]
		}
		return tsNpmPrefix + trimmed
	}
	if idx := strings.Index(trimmed, "/"); idx > 0 {
		trimmed = trimmed[:idx]
	}
	return tsNpmPrefix + trimmed
}

func isRelativeImport(path string) bool {
	return strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../")
}

func hasFile(dir, name string) bool {
	return fileExists(filepath.Join(dir, name))
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
