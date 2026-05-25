package loader

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/cas"
	memstore "github.com/joshka0/foxctl/internal/storage/memory"
)

func TestLoadFromFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join("testdata", "valid-3.0.json")
	l := New(nil, nil)
	spec, err := l.Load(context.Background(), path)
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	if spec.Version != "3.0.3" {
		t.Fatalf("expected version 3.0.3 got %s", spec.Version)
	}
	op, err := spec.GetOperation("listUsers")
	if err != nil {
		t.Fatalf("get operation: %v", err)
	}
	if op.Method != "GET" || op.Path != "/users" {
		t.Fatalf("unexpected operation: %#v", op)
	}
	spec2, err := l.Load(context.Background(), path)
	if err != nil {
		t.Fatalf("reload spec: %v", err)
	}
	if spec2 != spec {
		t.Fatalf("expected cached spec pointer reuse")
	}
}

func TestLoadFromCAS(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tmp := t.TempDir()
	store, err := cas.NewStore(tmp)
	if err != nil {
		t.Fatalf("cas store: %v", err)
	}
	data, err := os.ReadFile(filepath.Join("testdata", "valid-3.1.yaml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	obj, err := store.Put(ctx, bytesReader(data), "application/openapi+yaml", nil)
	if err != nil {
		t.Fatalf("cas put: %v", err)
	}
	l := New(store, nil)
	spec, err := l.Load(ctx, obj.Digest)
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	if spec.Version != "3.1.0" {
		t.Fatalf("unexpected version %s", spec.Version)
	}
	if _, err := spec.GetOperation("createWidget"); err != nil {
		t.Fatalf("operation lookup: %v", err)
	}
}

func TestLoadFromMemoryWithDigest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	casPath := filepath.Join(root, "cas")
	memoryPath := filepath.Join(root, "memory")
	if err := os.MkdirAll(casPath, 0o755); err != nil {
		t.Fatalf("mkdir cas: %v", err)
	}
	if err := os.MkdirAll(memoryPath, 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	casStore, err := cas.NewStore(casPath)
	if err != nil {
		t.Fatalf("cas store: %v", err)
	}
	specBytes, err := os.ReadFile(filepath.Join("testdata", "valid-3.0.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	obj, err := casStore.Put(ctx, bytesReader(specBytes), "application/openapi+json", nil)
	if err != nil {
		t.Fatalf("cas put: %v", err)
	}
	memStore, err := memstore.Open(ctx, memoryPath, casPath)
	if err != nil {
		t.Fatalf("memory open: %v", err)
	}
	t.Cleanup(func() {
		if err := memStore.Close(); err != nil {
			t.Fatalf("memory close: %v", err)
		}
	})
	entry := storage.NamedEntry{
		Name:      "sample",
		Type:      "openapi_spec",
		Workspace: memoryPath,
		Summary:   "sample",
		Result:    []byte(`{"status":"ok","command":"test"}`),
		Digests:   []string{obj.Digest},
	}
	if _, err := memStore.Save(ctx, entry); err != nil {
		t.Fatalf("memory save: %v", err)
	}
	l := New(casStore, memStore, WithWorkspace(memoryPath))
	spec, err := l.Load(ctx, "memory:sample")
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	if spec.Source != "memory:sample" {
		t.Fatalf("expected memory source got %s", spec.Source)
	}
	if len(spec.Operations) == 0 {
		t.Fatalf("expected operations indexed")
	}
}

func TestLoadFromMemoryInlineSpecWithStatusCommandFields(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	casPath := filepath.Join(root, "cas")
	memoryPath := filepath.Join(root, "memory")
	if err := os.MkdirAll(casPath, 0o755); err != nil {
		t.Fatalf("mkdir cas: %v", err)
	}
	if err := os.MkdirAll(memoryPath, 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	memStore, err := memstore.Open(ctx, memoryPath, casPath)
	if err != nil {
		t.Fatalf("memory open: %v", err)
	}
	t.Cleanup(func() {
		if err := memStore.Close(); err != nil {
			t.Fatalf("memory close: %v", err)
		}
	})

	specBytes := []byte(`{
  "openapi": "3.0.3",
  "info": {
    "title": "Command API",
    "version": "1.0.0"
  },
  "paths": {
    "/commands": {
      "get": {
        "operationId": "getCommandStatus",
        "responses": {
          "200": {
            "description": "OK",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "status": { "type": "string" },
                    "command": { "type": "string" }
                  }
                }
              }
            }
          }
        }
      }
    }
  }
}`)
	entry := storage.NamedEntry{
		Name:      "inline-openapi",
		Type:      "openapi_spec",
		Workspace: memoryPath,
		Summary:   "inline spec",
		Result:    specBytes,
	}
	if _, err := memStore.Save(ctx, entry); err != nil {
		t.Fatalf("memory save: %v", err)
	}

	l := New(nil, memStore, WithWorkspace(memoryPath))
	spec, err := l.Load(ctx, "memory:inline-openapi")
	if err != nil {
		t.Fatalf("load inline spec: %v", err)
	}
	if spec.Digest != "" {
		t.Fatalf("expected inline memory spec without digest, got %s", spec.Digest)
	}
	op, err := spec.GetOperation("getCommandStatus")
	if err != nil {
		t.Fatalf("operation lookup: %v", err)
	}
	if op.Path != "/commands" || op.Method != "GET" {
		t.Fatalf("unexpected operation: %#v", op)
	}
}

func TestInvalidSpecs(t *testing.T) {
	t.Parallel()
	l := New(nil, nil)
	if _, err := l.Load(context.Background(), filepath.Join("testdata", "invalid-version.json")); err == nil {
		t.Fatalf("expected version error")
	}
	if _, err := l.Load(context.Background(), filepath.Join("testdata", "malformed.yaml")); err == nil {
		t.Fatalf("expected parse error")
	}
}

func TestIndexOperationsRejectsDuplicateOperationIDs(t *testing.T) {
	t.Parallel()
	responses := openapi3.NewResponses(openapi3.WithStatus(200, &openapi3.ResponseRef{
		Value: openapi3.NewResponse().WithDescription("OK"),
	}))
	doc := &openapi3.T{
		OpenAPI: "3.0.3",
		Info:    &openapi3.Info{Title: "Duplicate API", Version: "1.0.0"},
		Paths: openapi3.NewPaths(
			openapi3.WithPath("/alpha", &openapi3.PathItem{
				Get: &openapi3.Operation{
					OperationID: "sameOperation",
					Responses:   responses,
				},
			}),
			openapi3.WithPath("/beta", &openapi3.PathItem{
				Post: &openapi3.Operation{
					OperationID: "sameOperation",
					Responses:   responses,
				},
			}),
		),
	}

	if _, err := indexOperations(doc); err == nil || !strings.Contains(err.Error(), `duplicate operationId "sameOperation"`) {
		t.Fatalf("indexOperations duplicate error = %v", err)
	}
}

func FuzzParseGeneratedSpecIndexesOperations(f *testing.F) {
	seeds := []struct {
		operationID string
		pathPart    string
		methodSeed  uint8
		duplicate   bool
		strict      bool
	}{
		{operationID: "listUsers", pathPart: "users", methodSeed: 0},
		{operationID: "createWidget", pathPart: "widgets-id", methodSeed: 1, strict: true},
		{operationID: "duplicateOp", pathPart: "dupes", methodSeed: 2, duplicate: true},
		{operationID: "   ", pathPart: "blank-operation", methodSeed: 3},
	}
	for _, seed := range seeds {
		f.Add(seed.operationID, seed.pathPart, seed.methodSeed, seed.duplicate, seed.strict)
	}

	f.Fuzz(func(t *testing.T, operationID, pathPart string, methodSeed uint8, duplicate, strict bool) {
		const maxInputLen = 256
		if len(operationID) > maxInputLen || len(pathPart) > maxInputLen {
			t.Skip("input too large for focused OpenAPI loader fuzzing")
		}
		if !utf8.ValidString(operationID) || !utf8.ValidString(pathPart) {
			t.Skip("OpenAPI operation IDs and paths are UTF-8 strings")
		}

		method := openAPIFuzzMethod(methodSeed)
		path := "/" + openAPIFuzzPathPart(pathPart)
		raw := mustGeneratedOpenAPISpec(t, operationID, path, method, duplicate)

		l := New(nil, nil)
		spec, err := l.parse(context.Background(), raw, "memory:fuzz", "sha256:fuzz", loadOptions{strict: strict})
		if duplicate && strings.TrimSpace(operationID) != "" {
			if err == nil {
				t.Fatalf("duplicate operationId %q parsed successfully", operationID)
			}
			if !strings.Contains(err.Error(), "duplicate operationId") && !strings.Contains(err.Error(), "same operation id") {
				t.Fatalf("duplicate operationId error = %v", err)
			}
			return
		}
		if err != nil {
			t.Fatalf("parse generated spec: %v\n%s", err, raw)
		}

		if spec.Doc == nil {
			t.Fatal("parsed spec has nil document")
		}
		if spec.Version != "3.0.3" {
			t.Fatalf("version = %q, want 3.0.3", spec.Version)
		}
		if spec.Source != "memory:fuzz" || spec.Digest != "sha256:fuzz" {
			t.Fatalf("source/digest = %q/%q", spec.Source, spec.Digest)
		}
		if !bytes.Equal(spec.Raw, raw) {
			t.Fatal("parsed spec did not preserve raw spec bytes")
		}

		if strings.TrimSpace(operationID) == "" {
			if len(spec.Operations) != 0 {
				t.Fatalf("blank operationId indexed operations: %+v", spec.Operations)
			}
			return
		}

		if len(spec.Operations) != 1 {
			t.Fatalf("operation count = %d, want 1: %+v", len(spec.Operations), spec.Operations)
		}
		op, err := spec.GetOperation(operationID)
		if err != nil {
			t.Fatalf("get operation: %v", err)
		}
		if op.ID != operationID || op.Method != strings.ToUpper(method) || op.Path != path {
			t.Fatalf("operation = %+v, want id=%q method=%q path=%q", op, operationID, strings.ToUpper(method), path)
		}
		if op.Responses == nil {
			t.Fatal("operation responses were not preserved")
		}
	})
}

func mustGeneratedOpenAPISpec(t *testing.T, operationID, path, method string, duplicate bool) []byte {
	t.Helper()

	operation := map[string]any{
		"operationId": operationID,
		"responses": map[string]any{
			"200": map[string]any{"description": "OK"},
		},
	}
	pathItem := map[string]any{method: operation}
	if duplicate && strings.TrimSpace(operationID) != "" {
		otherMethod := "post"
		if method == otherMethod {
			otherMethod = "put"
		}
		pathItem[otherMethod] = map[string]any{
			"operationId": operationID,
			"responses": map[string]any{
				"200": map[string]any{"description": "OK"},
			},
		}
	}

	raw, err := json.Marshal(map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":   "Fuzz API",
			"version": "1.0.0",
		},
		"paths": map[string]any{path: pathItem},
	})
	if err != nil {
		t.Fatalf("marshal generated spec: %v", err)
	}
	return raw
}

func openAPIFuzzMethod(seed uint8) string {
	methods := []string{"get", "post", "put", "patch", "delete", "head", "options"}
	return methods[int(seed)%len(methods)]
}

func openAPIFuzzPathPart(raw string) string {
	var b strings.Builder
	lastSlash := false
	for _, r := range raw {
		if b.Len() >= 96 {
			break
		}
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
			lastSlash = false
		case r == '/' && b.Len() > 0 && !lastSlash:
			b.WriteByte('/')
			lastSlash = true
		}
	}
	path := strings.Trim(b.String(), "/")
	if path == "" {
		return "resource"
	}
	return path
}

func bytesReader(data []byte) *bytes.Reader {
	return bytes.NewReader(data)
}
