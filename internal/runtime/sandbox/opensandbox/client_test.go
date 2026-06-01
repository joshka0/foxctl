package opensandbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"testing/quick"
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

func TestShellQuotePropertyRoundTripsThroughPOSIXShell(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skipf("sh unavailable: %v", err)
	}

	prop := func(raw string) bool {
		value := shellQuotePropertyValue(raw)
		return shellQuoteRoundTrips(t, value)
	}

	if err := quick.Check(prop, &quick.Config{MaxCount: 200}); err != nil {
		t.Fatalf("shell quote property failed: %v", err)
	}
}

func TestShellQuoteRoundTripsHostileExamples(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skipf("sh unavailable: %v", err)
	}

	examples := []string{
		"",
		"plain",
		"has space",
		"quote'break",
		"semi; echo hacked",
		"$(echo hacked)",
		"`echo hacked`",
		"line\nnext",
	}
	for _, example := range examples {
		t.Run(example, func(t *testing.T) {
			if !shellQuoteRoundTrips(t, example) {
				t.FailNow()
			}
		})
	}
}

func TestStringLiteralJSONPropertyRoundTrips(t *testing.T) {
	prop := func(raw string) bool {
		literal := stringLiteralJSON(raw)
		var decoded string
		if err := json.Unmarshal([]byte(literal), &decoded); err != nil {
			t.Logf("stringLiteralJSON(%q) produced invalid JSON %q: %v", raw, literal, err)
			return false
		}
		if decoded != raw {
			t.Logf("stringLiteralJSON(%q) decoded as %q", raw, decoded)
			return false
		}
		return true
	}

	if err := quick.Check(prop, &quick.Config{MaxCount: 1000}); err != nil {
		t.Fatalf("JSON literal property failed: %v", err)
	}
}

func TestBuildNetworkPolicyTrimsDedupesAndAddsGitHubCompanions(t *testing.T) {
	policy := buildNetworkPolicy(" https://github.com/example/repo.git ", []string{
		" api.github.com ",
		"",
		"github.com",
		"api.github.com",
	})
	if policy == nil {
		t.Fatal("policy = nil, want deny-by-default policy")
	}
	if policy.DefaultAction != "deny" {
		t.Fatalf("default action = %q, want deny", policy.DefaultAction)
	}

	targets := make([]string, 0, len(policy.Egress))
	for _, rule := range policy.Egress {
		if rule.Action != "allow" {
			t.Fatalf("egress rule action = %q, want allow", rule.Action)
		}
		targets = append(targets, rule.Target)
	}
	want := []string{"api.github.com", "github.com", "codeload.github.com", "objects.githubusercontent.com"}
	if strings.Join(targets, ",") != strings.Join(want, ",") {
		t.Fatalf("targets = %v, want %v", targets, want)
	}
}

func TestBuildNetworkPolicyAllowsScpLikeRepoHost(t *testing.T) {
	policy := buildNetworkPolicy(" git@github.com:example/repo.git ", nil)
	if policy == nil {
		t.Fatal("policy = nil, want deny-by-default policy for scp-style repo URL")
	}

	targets := make([]string, 0, len(policy.Egress))
	for _, rule := range policy.Egress {
		targets = append(targets, rule.Target)
	}
	want := []string{"github.com", "codeload.github.com", "objects.githubusercontent.com"}
	if strings.Join(targets, ",") != strings.Join(want, ",") {
		t.Fatalf("targets = %v, want %v", targets, want)
	}
}

func shellQuotePropertyValue(raw string) string {
	value := strings.ReplaceAll(raw, "\x00", "_")
	if len(value) > 256 {
		value = value[:256]
	}
	return value
}

func shellQuoteRoundTrips(t *testing.T, value string) bool {
	t.Helper()

	script := "printf %s " + shellQuote(value)
	//nolint:gosec // This test intentionally executes generated shell quoting to prove it is inert data.
	cmd := exec.Command("sh", "-c", script)
	out, err := cmd.Output()
	if err != nil {
		t.Logf("shell quote round-trip command failed for %q: %v", value, err)
		return false
	}
	if string(out) != value {
		t.Logf("shellQuote(%q) round-tripped as %q", value, string(out))
		return false
	}
	return true
}

func serverURLHostPath(t *testing.T, r *http.Request) string {
	t.Helper()
	scheme := "http://"
	if r.TLS != nil {
		scheme = "https://"
	}
	return scheme + r.Host
}
