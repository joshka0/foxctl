package ardoq

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListComponentsUsesBearerAuthOrgAndQuery(t *testing.T) {
	t.Helper()
	var gotAuth string
	var gotOrg string
	var gotQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotOrg = r.Header.Get("X-org")
		gotQuery = r.URL.RawQuery
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/v2/components" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"values":[{"_id":"c1"}]}`))
	}))
	defer srv.Close()

	client, err := NewClient(Config{
		BaseURL:    srv.URL,
		OrgLabel:   "acme",
		APIToken:   "secret",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	result, err := client.ListComponents(context.Background(), map[string]any{
		"rootWorkspace":            "ws1",
		"customFields.external_id": "svc-api",
	})
	if err != nil {
		t.Fatalf("ListComponents() error = %v", err)
	}

	if gotAuth != "Bearer secret" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotOrg != "acme" {
		t.Fatalf("X-org = %q", gotOrg)
	}
	if !strings.Contains(gotQuery, "rootWorkspace=ws1") {
		t.Fatalf("query = %q", gotQuery)
	}
	if !strings.Contains(gotQuery, "customFields.external_id=svc-api") {
		t.Fatalf("query = %q", gotQuery)
	}
	values, ok := result["values"].([]any)
	if !ok || len(values) != 1 {
		t.Fatalf("values = %#v", result["values"])
	}
}

func TestBatchPostsExpectedPayload(t *testing.T) {
	t.Helper()
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v2/batch" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	client, err := NewClient(Config{
		BaseURL:    srv.URL,
		OrgLabel:   "acme",
		APIToken:   "secret",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.Batch(context.Background(), map[string]any{
		"components": map[string]any{
			"upsert": []map[string]any{
				{
					"uniqueBy": []string{"customFields.external_id", "rootWorkspace"},
					"body": map[string]any{
						"rootWorkspace": "ws1",
						"typeId":        "p1",
						"name":          "API",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Batch() error = %v", err)
	}

	components, ok := gotBody["components"].(map[string]any)
	if !ok {
		t.Fatalf("components = %#v", gotBody["components"])
	}
	upserts, ok := components["upsert"].([]any)
	if !ok || len(upserts) != 1 {
		t.Fatalf("upsert = %#v", components["upsert"])
	}
}

func TestGetWorkspaceContextUsesContextEndpoint(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/v2/workspaces/ws1/context" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"referenceTypes":[{"type":13,"name":"Business owner of"}]}`))
	}))
	defer srv.Close()

	client, err := NewClient(Config{
		BaseURL:    srv.URL,
		OrgLabel:   "acme",
		APIToken:   "secret",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	result, err := client.GetWorkspaceContext(context.Background(), "ws1")
	if err != nil {
		t.Fatalf("GetWorkspaceContext() error = %v", err)
	}
	values, ok := result["referenceTypes"].([]any)
	if !ok || len(values) != 1 {
		t.Fatalf("referenceTypes = %#v", result["referenceTypes"])
	}
}

func TestListWorkspacesReturnsStructuredError(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"invalid token"}`))
	}))
	defer srv.Close()

	client, err := NewClient(Config{
		BaseURL:    srv.URL,
		OrgLabel:   "acme",
		APIToken:   "bad-token",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.ListWorkspaces(context.Background(), nil)
	if err == nil {
		t.Fatal("ListWorkspaces() error = nil, want error")
	}
	ardoqErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("error type = %T", err)
	}
	if ardoqErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d", ardoqErr.StatusCode)
	}
	if !strings.Contains(err.Error(), "invalid token") {
		t.Fatalf("error = %q", err.Error())
	}
}
