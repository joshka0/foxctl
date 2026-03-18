package opensandbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProvisionShallowCloneWorkspace(t *testing.T) {
	t.Helper()

	var (
		gotCreateAuth   string
		gotEndpointAuth string
		gotCommandAuth  string
		gotCreateBody   CreateSandboxRequest
		gotCommandBody  RunCommandRequest
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/sandboxes", func(w http.ResponseWriter, r *http.Request) {
		gotCreateAuth = r.Header.Get("OPEN-SANDBOX-API-KEY")
		if r.Method != http.MethodPost {
			t.Fatalf("create method=%s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotCreateBody); err != nil {
			t.Fatalf("decode create body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "sbx-123",
			"status": map[string]any{
				"state": "Pending",
			},
		})
	})
	mux.HandleFunc("/v1/sandboxes/sbx-123/endpoints/44772", func(w http.ResponseWriter, r *http.Request) {
		gotEndpointAuth = r.Header.Get("OPEN-SANDBOX-API-KEY")
		if got := r.URL.Query().Get("use_server_proxy"); got != "true" {
			t.Fatalf("use_server_proxy=%q want true", got)
		}
		hostPath := strings.TrimPrefix(serverURLHostPath(t, r), "http://")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"endpoint": hostPath + "/sandboxes/sbx-123/proxy/44772",
			"headers": map[string]string{
				"X-EXECD-ACCESS-TOKEN": "token-123",
			},
		})
	})
	mux.HandleFunc("/sandboxes/sbx-123/proxy/44772/command", func(w http.ResponseWriter, r *http.Request) {
		gotCommandAuth = r.Header.Get("X-EXECD-ACCESS-TOKEN")
		if err := json.NewDecoder(r.Body).Decode(&gotCommandBody); err != nil {
			t.Fatalf("decode command body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"stdout\",\"text\":\"clone ok\"}\n\n"))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := New(Config{
		BaseURL:        server.URL,
		APIKey:         "api-key-1",
		UseServerProxy: true,
		HTTPClient:     server.Client(),
	})

	result, err := client.ProvisionShallowCloneWorkspace(context.Background(), ProvisionWorkspaceRequest{
		RepoURL:        "https://github.com/example/repo.git",
		RepoRef:        "main",
		Name:           "sandbox-one",
		WorkspaceRoot:  "/workspace/repo",
		Timeout:        time.Hour,
		AllowEgress:    []string{"api.github.com"},
		UseServerProxy: true,
	})
	if err != nil {
		t.Fatalf("ProvisionShallowCloneWorkspace() error = %v", err)
	}

	if gotCreateAuth != "api-key-1" || gotEndpointAuth != "api-key-1" {
		t.Fatalf("unexpected lifecycle auth headers: create=%q endpoint=%q", gotCreateAuth, gotEndpointAuth)
	}
	if gotCommandAuth != "token-123" {
		t.Fatalf("command auth header=%q want token-123", gotCommandAuth)
	}
	if result.SandboxID != "sbx-123" {
		t.Fatalf("sandbox id=%q", result.SandboxID)
	}
	if result.WorkspaceRoot != "/workspace/repo" {
		t.Fatalf("workspace root=%q", result.WorkspaceRoot)
	}
	if !strings.Contains(gotCommandBody.Command, "git -C '/workspace/repo' fetch --depth 1 origin 'main'") {
		t.Fatalf("clone command missing fetch: %s", gotCommandBody.Command)
	}
	if !strings.Contains(gotCommandBody.Command, "remote add origin 'https://github.com/example/repo.git'") {
		t.Fatalf("clone command missing repo url: %s", gotCommandBody.Command)
	}
	if gotCreateBody.Image.URI != DefaultSandboxImage {
		t.Fatalf("image=%q want %q", gotCreateBody.Image.URI, DefaultSandboxImage)
	}
	if gotCreateBody.NetworkPolicy == nil || gotCreateBody.NetworkPolicy.DefaultAction != "deny" {
		t.Fatalf("network policy missing deny default: %+v", gotCreateBody.NetworkPolicy)
	}
	if len(gotCreateBody.NetworkPolicy.Egress) < 2 {
		t.Fatalf("expected repo host plus extra egress, got %+v", gotCreateBody.NetworkPolicy.Egress)
	}
}

func TestGetEndpointPreservesPathInEndpoint(t *testing.T) {
	client := New(Config{BaseURL: "http://localhost:8080"})
	got, err := client.execdURL("127.0.0.1:8080/sandboxes/sbx/proxy/44772", "/command")
	if err != nil {
		t.Fatalf("execdURL() error = %v", err)
	}
	want := "http://127.0.0.1:8080/sandboxes/sbx/proxy/44772/command"
	if got != want {
		t.Fatalf("execdURL()=%q want %q", got, want)
	}
}

func TestRunSandboxCommandUsesResolvedExecdEndpoint(t *testing.T) {
	t.Helper()

	var gotCommandBody RunCommandRequest

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/sandboxes/sbx-456/endpoints/44772", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"endpoint": serverURLHostPath(t, r) + "/sandboxes/sbx-456/proxy/44772",
			"headers": map[string]string{
				"X-EXECD-ACCESS-TOKEN": "execd-token-456",
			},
		})
	})
	mux.HandleFunc("/sandboxes/sbx-456/proxy/44772/command", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-EXECD-ACCESS-TOKEN"); got != "execd-token-456" {
			t.Fatalf("execd auth header=%q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotCommandBody); err != nil {
			t.Fatalf("decode command body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"stdout\",\"text\":\"ok\"}\n\n"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := New(Config{
		BaseURL:        server.URL,
		UseServerProxy: true,
		HTTPClient:     server.Client(),
	})
	result, err := client.RunSandboxCommand(context.Background(), RunSandboxCommandRequest{
		SandboxID: "sbx-456",
		Command:   "pwd",
		Cwd:       "/workspace/repo",
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("RunSandboxCommand() error = %v", err)
	}
	if result.Stdout != "ok" {
		t.Fatalf("stdout=%q want ok", result.Stdout)
	}
	if gotCommandBody.Command != "pwd" {
		t.Fatalf("command=%q want pwd", gotCommandBody.Command)
	}
	if gotCommandBody.Cwd != "/workspace/repo" {
		t.Fatalf("cwd=%q want /workspace/repo", gotCommandBody.Cwd)
	}
	if gotCommandBody.Timeout != 5000 {
		t.Fatalf("timeout=%d want 5000", gotCommandBody.Timeout)
	}
}

func serverURLHostPath(t *testing.T, r *http.Request) string {
	t.Helper()
	scheme := "http://"
	if r.TLS != nil {
		scheme = "https://"
	}
	return scheme + r.Host
}
