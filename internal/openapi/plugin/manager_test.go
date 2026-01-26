package plugin

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/protocol"
)

func TestManagerInvokeAuthSuccess(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	buildPluginBinary(t, tmp, "auth-hmac")

	cfg := config.Config{Home: tmp}
	mgr := NewManager(cfg,
		WithSearchPaths([]string{tmp}),
		WithHandshakeTimeout(5*time.Second),
		WithRuntimeLimits(RuntimeLimits{WallTimeout: 5 * time.Second}),
	)

	payload := AuthRequestPayload{
		Request: HTTPRequest{
			Method: "GET",
			URL:    "https://api.example.com/data",
			Headers: map[string]string{
				"User-Agent": "agentctl-test",
			},
		},
		Context: AuthContext{
			Credentials: map[string]any{
				"key":    "access",
				"secret": "secret123",
			},
		},
	}

	result, err := mgr.InvokeAuth(context.Background(), "auth-hmac", payload)
	if err != nil {
		t.Fatalf("invoke auth plugin: %v", err)
	}
	authHeader := result.Headers["Authorization"]
	expected := expectedHMACHeader(payload.Request, payload.Context)
	if authHeader != expected {
		t.Fatalf("unexpected authorization header: got %q want %q", authHeader, expected)
	}
}

func TestManagerInvokeAuthPluginError(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	buildPluginBinary(t, tmp, "auth-hmac")

	cfg := config.Config{Home: tmp}
	mgr := NewManager(cfg,
		WithSearchPaths([]string{tmp}),
		WithHandshakeTimeout(5*time.Second),
		WithRuntimeLimits(RuntimeLimits{WallTimeout: 5 * time.Second}),
	)

	payload := AuthRequestPayload{
		Request: HTTPRequest{Method: "GET", URL: "https://example.com"},
		Context: AuthContext{
			Credentials: map[string]any{
				"key": "only-key",
			},
		},
	}

	_, err := mgr.InvokeAuth(context.Background(), "auth-hmac", payload)
	if err == nil {
		t.Fatalf("expected error from plugin")
	}
	var invErr *InvocationError
	if !errors.As(err, &invErr) {
		t.Fatalf("expected InvocationError, got %T", err)
	}
	if invErr.Code != protocol.ErrorCodeEAuth {
		t.Fatalf("expected EAUTH, got %s", invErr.Code)
	}
}

func TestManagerInvokeAuthTimeout(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	buildPluginBinary(t, tmp, "auth-hmac")

	cfg := config.Config{Home: tmp}
	mgr := NewManager(cfg, WithSearchPaths([]string{tmp}), WithHandshakeTimeout(5*time.Second))

	payload := AuthRequestPayload{
		Request: HTTPRequest{Method: "GET", URL: "https://example.com"},
		Context: AuthContext{
			Credentials: map[string]any{
				"key":    "access",
				"secret": "secret",
			},
			SpecHints: map[string]any{
				"delay_ms": 600.0,
			},
		},
	}

	_, err := mgr.InvokeAuth(context.Background(), "auth-hmac", payload)
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	var invErr *InvocationError
	if !errors.As(err, &invErr) {
		t.Fatalf("expected InvocationError, got %T", err)
	}
	if invErr.Code != protocol.ErrorCodeETimeout {
		t.Fatalf("expected ETIMEOUT, got %s", invErr.Code)
	}
}

func TestManagerInvokePagination(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	buildPluginBinary(t, tmp, "paging-custom")

	cfg := config.Config{Home: tmp}
	mgr := NewManager(cfg,
		WithSearchPaths([]string{tmp}),
		WithHandshakeTimeout(5*time.Second),
		WithRuntimeLimits(RuntimeLimits{WallTimeout: 5 * time.Second}),
	)

	body := map[string]any{
		"items": []any{1, 2, 3},
		"meta":  map[string]any{"next_cursor": "cursor-2"},
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	payload := PaginationRequestPayload{
		LastResponse: HTTPResponse{
			Status: 200,
			Body:   bodyBytes,
		},
		ItemsFetchedSoFar: 3,
		Context: PaginationContext{
			SpecHints: map[string]any{"max_items": 10.0},
		},
	}

	result, err := mgr.InvokePagination(context.Background(), "paging-custom", payload)
	if err != nil {
		t.Fatalf("invoke pagination: %v", err)
	}
	if !result.Continue {
		t.Fatalf("expected continue true")
	}
	if result.NextCursor != "cursor-2" {
		t.Fatalf("expected next cursor cursor-2 got %s", result.NextCursor)
	}
	if len(result.NextQuery) != 1 || result.NextQuery["cursor"] != "cursor-2" {
		t.Fatalf("unexpected next query: %#v", result.NextQuery)
	}
	if result.ItemsInPage != 3 {
		t.Fatalf("expected items in page 3 got %d", result.ItemsInPage)
	}
}

func expectedHMACHeader(req HTTPRequest, ctx AuthContext) string {
	key, ok := ctx.Credentials["key"].(string)
	if !ok {
		panic("test setup error: key credential must be a string")
	}
	secret, ok := ctx.Credentials["secret"].(string)
	if !ok {
		panic("test setup error: secret credential must be a string")
	}
	base := req.Method + " " + req.URL
	if len(req.Body) > 0 {
		base += string(req.Body)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := mac.Write([]byte(base)); err != nil {
		panic(fmt.Sprintf("write hmac: %v", err))
	}
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("HMAC %s:%s", key, sig)
}

func buildPluginBinary(t *testing.T, outputDir, name string) string {
	t.Helper()
	root := projectRoot(t)
	output := filepath.Join(outputDir, pluginBinaryName(name))
	pkg := fmt.Sprintf("./plugins/%s", name)
	cmd := exec.Command("go", "build", "-o", output, pkg)
	cmd.Dir = root
	cmd.Env = os.Environ()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build plugin %s: %v\n%s", name, err, stderr.String())
	}
	return output
}

func projectRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("unable to resolve caller")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	return root
}

func pluginBinaryName(name string) string {
	base := "agentctl-plugin-" + name
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}
