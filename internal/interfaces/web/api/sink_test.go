package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
)

func TestSinkHandlerWritesUnderConfiguredVault(t *testing.T) {
	vault := t.TempDir()
	t.Setenv("FOXCTL_ACA_VAULT_PATH", vault)
	t.Setenv("FOXCTL_OBSIDIAN_VAULT_PATH", "")

	req := httptest.NewRequest(http.MethodPost, "/api/sink", strings.NewReader(`{
		"filePath": "notes/repo/foxctl/semantic-and-memory.md",
		"content": "semantic anchors note"
	}`))
	req.Header.Set("X-Operation-Id", "op-test")
	rr := httptest.NewRecorder()

	SinkHandler(config.Config{}, zerolog.Nop())(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	target := filepath.Join(vault, "notes", "repo", "foxctl", "semantic-and-memory.md")
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read sink target: %v", err)
	}
	if string(body) != "semantic anchors note" {
		t.Fatalf("body=%q", string(body))
	}

	var resp envelope.Envelope
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != envelope.StatusOK || resp.Command != "sink.write" {
		t.Fatalf("response status=%q command=%q", resp.Status, resp.Command)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("data=%T", resp.Data)
	}
	if data["success"] != true || data["operationId"] != "op-test" {
		t.Fatalf("data=%v", data)
	}
	if data["filePath"] != "notes/repo/foxctl/semantic-and-memory.md" {
		t.Fatalf("filePath=%v", data["filePath"])
	}
}

func TestSinkHandlerRejectsRequestControlledVaultPath(t *testing.T) {
	vault := t.TempDir()
	outside := t.TempDir()
	t.Setenv("FOXCTL_ACA_VAULT_PATH", vault)
	t.Setenv("FOXCTL_OBSIDIAN_VAULT_PATH", "")

	req := httptest.NewRequest(http.MethodPost, "/api/sink", strings.NewReader(`{
		"vaultPath": "`+filepath.ToSlash(outside)+`",
		"filePath": "outside.md",
		"content": "do not write"
	}`))
	rr := httptest.NewRecorder()

	SinkHandler(config.Config{}, zerolog.Nop())(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(filepath.Join(outside, "outside.md")); !os.IsNotExist(err) {
		t.Fatalf("outside write err=%v, want not exist", err)
	}
}

func TestSinkHandlerRejectsTraversalPath(t *testing.T) {
	vault := t.TempDir()
	parent := filepath.Dir(vault)
	t.Setenv("FOXCTL_ACA_VAULT_PATH", vault)
	t.Setenv("FOXCTL_OBSIDIAN_VAULT_PATH", "")

	req := httptest.NewRequest(http.MethodPost, "/api/sink", strings.NewReader(`{
		"filePath": "../escape.md",
		"content": "do not write"
	}`))
	rr := httptest.NewRecorder()

	SinkHandler(config.Config{}, zerolog.Nop())(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(filepath.Join(parent, "escape.md")); !os.IsNotExist(err) {
		t.Fatalf("escape write err=%v, want not exist", err)
	}
}

func TestSinkHandlerRejectsSymlinkDirectoryEscape(t *testing.T) {
	vault := t.TempDir()
	outside := t.TempDir()
	t.Setenv("FOXCTL_ACA_VAULT_PATH", vault)
	t.Setenv("FOXCTL_OBSIDIAN_VAULT_PATH", "")

	if err := os.Symlink(outside, filepath.Join(vault, "linked")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/sink", strings.NewReader(`{
		"filePath": "linked/escape.md",
		"content": "do not write"
	}`))
	rr := httptest.NewRecorder()

	SinkHandler(config.Config{}, zerolog.Nop())(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(filepath.Join(outside, "escape.md")); !os.IsNotExist(err) {
		t.Fatalf("outside write err=%v, want not exist", err)
	}
}

func TestSinkHandlerRejectsSymlinkTarget(t *testing.T) {
	vault := t.TempDir()
	outside := t.TempDir()
	t.Setenv("FOXCTL_ACA_VAULT_PATH", vault)
	t.Setenv("FOXCTL_OBSIDIAN_VAULT_PATH", "")

	outsideTarget := filepath.Join(outside, "target.md")
	if err := os.WriteFile(outsideTarget, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write outside target: %v", err)
	}
	if err := os.Symlink(outsideTarget, filepath.Join(vault, "note.md")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/sink", strings.NewReader(`{
		"filePath": "note.md",
		"content": "do not write"
	}`))
	rr := httptest.NewRecorder()

	SinkHandler(config.Config{}, zerolog.Nop())(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body, err := os.ReadFile(outsideTarget)
	if err != nil {
		t.Fatalf("read outside target: %v", err)
	}
	if string(body) != "keep" {
		t.Fatalf("outside target body=%q", string(body))
	}
}

func TestSinkHandlerRequiresConfiguredVaultPath(t *testing.T) {
	t.Setenv("FOXCTL_ACA_VAULT_PATH", "")
	t.Setenv("FOXCTL_OBSIDIAN_VAULT_PATH", "")

	req := httptest.NewRequest(http.MethodPost, "/api/sink", strings.NewReader(`{
		"filePath": "note.md",
		"content": "content"
	}`))
	rr := httptest.NewRecorder()

	SinkHandler(config.Config{}, zerolog.Nop())(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}
