package loader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage"
)

const (
	defaultHTTPTimeout = 30 * time.Second
)

// Loader loads and caches OpenAPI specifications from multiple sources.
type Loader struct {
	casStore         storage.CASStore
	memoryStore      storage.MemoryStore
	httpClient       *http.Client
	strictValidation bool
	workspace        string

	mu    sync.RWMutex
	cache map[string]*Spec
}

// Option configures the loader.
type Option func(*Loader)

// WithStrictValidation toggles strict validation mode.
func WithStrictValidation(strict bool) Option {
	return func(l *Loader) {
		l.strictValidation = strict
	}
}

// WithWorkspace sets a default workspace for memory lookups when the context does not provide one.
func WithWorkspace(path string) Option {
	return func(l *Loader) {
		l.workspace = workspace.Normalize(path)
	}
}

// WithHTTPClient overrides the HTTP client used for remote spec references.
func WithHTTPClient(client *http.Client) Option {
	return func(l *Loader) {
		l.httpClient = client
	}
}

// New returns a configured Loader.
func New(cas storage.CASStore, memory storage.MemoryStore, opts ...Option) *Loader {
	l := &Loader{
		casStore:    cas,
		memoryStore: memory,
		cache:       make(map[string]*Spec),
		httpClient: &http.Client{ //nolint:exhaustruct
			Timeout: defaultHTTPTimeout,
		},
	}
	for _, opt := range opts {
		opt(l)
	}
	if l.httpClient == nil {
		l.httpClient = &http.Client{ //nolint:exhaustruct
			Timeout: defaultHTTPTimeout,
		}
	}
	return l
}

// Spec represents a parsed OpenAPI specification.
type Spec struct {
	Doc        *openapi3.T
	Source     string
	Version    string
	Digest     string
	Raw        []byte
	Operations map[string]*Operation
}

// Operation is a normalized view of an OpenAPI operation.
type Operation struct {
	ID          string
	Method      string
	Path        string
	Summary     string
	Description string
	Tags        []string
	Parameters  openapi3.Parameters
	RequestBody *openapi3.RequestBodyRef
	Responses   *openapi3.Responses
	Security    *openapi3.SecurityRequirements
	Deprecated  bool
}

// GetOperation retrieves an operation by its identifier.
func (s *Spec) GetOperation(operationID string) (*Operation, error) {
	if s == nil {
		return nil, fmt.Errorf("nil spec")
	}
	op, ok := s.Operations[operationID]
	if !ok {
		ids := make([]string, 0, len(s.Operations))
		for id := range s.Operations {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		return nil, fmt.Errorf("operation %q not found (available: %s)", operationID, strings.Join(ids, ", "))
	}
	return op, nil
}

// Load resolves, parses, validates, and caches an OpenAPI specification.
//
// Index:
//
//	Purpose: Load and cache OpenAPI specs from CAS, memory, HTTP, or file paths
//	Flow: normalize workspace → compute cache key → fetch bytes → parse/validate → cache → return
//	Related: Loader.fetch, Loader.parse, Loader.cacheKey
//	Keywords: openapi_load, cas_digest, memory_ref, http_url, file_path, cache
//
// [[protocol:openapi-spec-loader]]
// [[domain:spec-resolution-caching]]
func (l *Loader) Load(ctx context.Context, ref string) (*Spec, error) {
	if strings.TrimSpace(ref) == "" {
		return nil, fmt.Errorf("spec reference cannot be empty")
	}

	opts := loadOptions{workspace: l.workspace, strict: l.strictValidation}
	if ws, ok := workspace.FromContext(ctx); ok {
		opts.workspace = ws
	}

	key, err := l.cacheKey(ref, opts)
	if err != nil {
		return nil, err
	}

	l.mu.RLock()
	cached, ok := l.cache[key]
	l.mu.RUnlock()
	if ok {
		return cached, nil
	}

	data, source, digest, err := l.fetch(ctx, ref, opts)
	if err != nil {
		return nil, err
	}

	spec, err := l.parse(ctx, data, source, digest, opts)
	if err != nil {
		return nil, err
	}

	l.mu.Lock()
	l.cache[key] = spec
	l.mu.Unlock()

	return spec, nil
}

type loadOptions struct {
	workspace string
	strict    bool
}

func (l *Loader) cacheKey(ref string, opts loadOptions) (string, error) {
	switch {
	case isCASDigest(ref):
		return strings.ToLower(ref), nil
	case isMemoryRef(ref):
		ws := workspace.Normalize(opts.workspace)
		if ws == "" {
			return "", fmt.Errorf("memory workspace not provided")
		}
		name := strings.TrimPrefix(ref, "memory:")
		return fmt.Sprintf("memory:%s:%s", ws, name), nil
	case isHTTPURL(ref):
		u, err := url.Parse(ref)
		if err != nil {
			return "", fmt.Errorf("invalid url %q: %w", ref, err)
		}
		u.Fragment = ""
		return u.String(), nil
	default:
		if strings.HasPrefix(ref, "file://") {
			u, err := url.Parse(ref)
			if err != nil {
				return "", fmt.Errorf("invalid file url %q: %w", ref, err)
			}
			ref = u.Path
		}
		path := ref
		if !filepath.IsAbs(path) {
			abs, err := filepath.Abs(path)
			if err == nil {
				path = abs
			}
		}
		return filepath.Clean(path), nil
	}
}

func (l *Loader) fetch(ctx context.Context, ref string, opts loadOptions) ([]byte, string, string, error) {
	switch {
	case isCASDigest(ref):
		if l.casStore == nil {
			return nil, "", "", fmt.Errorf("cas store not configured")
		}
		reader, meta, err := l.casStore.Get(ctx, ref)
		if err != nil {
			return nil, "", "", fmt.Errorf("load cas %s: %w", ref, err)
		}
		defer reader.Close()
		data, err := io.ReadAll(reader)
		if err != nil {
			return nil, "", "", fmt.Errorf("read cas %s: %w", ref, err)
		}
		return data, ref, meta.Digest, nil
	case isMemoryRef(ref):
		if l.memoryStore == nil {
			return nil, "", "", fmt.Errorf("memory store not configured")
		}
		ws := workspace.Normalize(opts.workspace)
		if ws == "" {
			return nil, "", "", fmt.Errorf("memory workspace not provided")
		}
		name := strings.TrimPrefix(ref, "memory:")
		entry, err := l.memoryStore.Get(ctx, name, ws)
		if err != nil {
			return nil, "", "", fmt.Errorf("memory %s (%s): %w", name, ws, err)
		}
		if len(entry.Result) > 0 && !looksLikeEnvelope(entry.Result) {
			return append([]byte(nil), entry.Result...), ref, firstDigest(entry.Digests), nil
		}
		digest := firstDigest(entry.Digests)
		if digest == "" {
			return nil, "", "", fmt.Errorf("memory %s (%s) missing spec data", name, ws)
		}
		if l.casStore == nil {
			return nil, "", "", fmt.Errorf("cas store required for memory digest %s", digest)
		}
		reader, meta, err := l.casStore.Get(ctx, digest)
		if err != nil {
			return nil, "", "", fmt.Errorf("memory %s (%s): load cas %s: %w", name, ws, digest, err)
		}
		defer reader.Close()
		data, err := io.ReadAll(reader)
		if err != nil {
			return nil, "", "", fmt.Errorf("memory %s (%s): read cas %s: %w", name, ws, digest, err)
		}
		return data, ref, meta.Digest, nil
	case isHTTPURL(ref):
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ref, nil)
		if err != nil {
			return nil, "", "", fmt.Errorf("build request for %s: %w", ref, err)
		}
		resp, err := l.httpClient.Do(req)
		if err != nil {
			return nil, "", "", fmt.Errorf("download %s: %w", ref, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, "", "", fmt.Errorf("download %s: unexpected status %s", ref, resp.Status)
		}
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, "", "", fmt.Errorf("read %s: %w", ref, err)
		}
		return data, ref, "", nil
	default:
		path := ref
		if strings.HasPrefix(path, "file://") {
			u, err := url.Parse(path)
			if err != nil {
				return nil, "", "", fmt.Errorf("invalid file url %q: %w", path, err)
			}
			path = u.Path
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, "", "", fmt.Errorf("read spec file %s: %w", path, err)
		}
		return data, path, "", nil
	}
}

func (l *Loader) parse(ctx context.Context, data []byte, source, digest string, opts loadOptions) (*Spec, error) {
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	loader.Context = ctx
	var (
		doc *openapi3.T
		err error
	)
	if base := baseURLForSource(source); base != nil {
		doc, err = loader.LoadFromDataWithPath(data, base)
	} else {
		doc, err = loader.LoadFromData(data)
	}
	if err != nil {
		return nil, fmt.Errorf("parse OpenAPI: %w", err)
	}
	if opts.strict {
		if err := doc.Validate(ctx); err != nil {
			return nil, fmt.Errorf("invalid OpenAPI spec: %w", err)
		}
	} else {
		if err := doc.Validate(ctx,
			openapi3.DisableSchemaDefaultsValidation(),
			openapi3.DisableExamplesValidation(),
		); err != nil {
			return nil, fmt.Errorf("invalid OpenAPI spec: %w", err)
		}
	}
	if !strings.HasPrefix(doc.OpenAPI, "3.") {
		return nil, fmt.Errorf("unsupported OpenAPI version: %s", doc.OpenAPI)
	}
	operations, err := indexOperations(doc)
	if err != nil {
		return nil, err
	}
	spec := &Spec{
		Doc:        doc,
		Source:     source,
		Version:    doc.OpenAPI,
		Digest:     digest,
		Raw:        append([]byte(nil), data...),
		Operations: operations,
	}
	return spec, nil
}

func indexOperations(doc *openapi3.T) (map[string]*Operation, error) {
	operations := make(map[string]*Operation)
	if doc.Paths == nil {
		return operations, nil
	}
	for path, pathItem := range doc.Paths.Map() {
		if pathItem == nil {
			continue
		}
		for method, op := range pathItem.Operations() {
			if op == nil || strings.TrimSpace(op.OperationID) == "" {
				continue
			}
			method = strings.ToUpper(method)
			if _, exists := operations[op.OperationID]; exists {
				return nil, fmt.Errorf("duplicate operationId %q", op.OperationID)
			}
			params := op.Parameters
			if params == nil {
				params = openapi3.Parameters{}
			}
			security := op.Security
			if security != nil && len(*security) == 0 {
				security = nil
			}
			tags := append([]string(nil), op.Tags...)
			operations[op.OperationID] = &Operation{
				ID:          op.OperationID,
				Method:      method,
				Path:        path,
				Summary:     op.Summary,
				Description: op.Description,
				Tags:        tags,
				Parameters:  params,
				RequestBody: op.RequestBody,
				Responses:   op.Responses,
				Security:    security,
				Deprecated:  op.Deprecated,
			}
		}
	}
	return operations, nil
}

func isCASDigest(ref string) bool {
	return strings.HasPrefix(ref, "sha256:") && len(ref) == len("sha256:")+64
}

func isMemoryRef(ref string) bool {
	return strings.HasPrefix(ref, "memory:")
}

func isHTTPURL(ref string) bool {
	u, err := url.Parse(ref)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func looksLikeEnvelope(data []byte) bool {
	trimmed := strings.TrimSpace(string(data))
	return strings.HasPrefix(trimmed, "{") && strings.Contains(trimmed, "\"status\"") && strings.Contains(trimmed, "\"command\"")
}

func firstDigest(digests []string) string {
	for _, d := range digests {
		if strings.HasPrefix(d, "sha256:") {
			return d
		}
	}
	return ""
}

func baseURLForSource(source string) *url.URL {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil
	}
	if isHTTPURL(source) || strings.HasPrefix(source, "file://") {
		u, err := url.Parse(source)
		if err == nil {
			return u
		}
		return nil
	}
	if isCASDigest(source) || isMemoryRef(source) {
		return nil
	}
	if filepath.IsAbs(source) {
		return &url.URL{Scheme: "file", Path: source}
	}
	if strings.Contains(source, "://") {
		return nil
	}
	if abs, err := filepath.Abs(source); err == nil {
		return &url.URL{Scheme: "file", Path: abs}
	}
	return nil
}
