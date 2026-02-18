package v1bridge_test

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"testing"

	"github.com/jkatigb/agentctl/internal/v2/adapters/v1bridge"
	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
)

func TestToolBridge_Success(t *testing.T) {
	t.Parallel()

	bridge := v1bridge.NewToolBridge(fakeLegacyExecutor{
		out: `{"ok":true}`,
	})
	got, err := bridge.Execute(context.Background(), "fs_read_file", json.RawMessage(`{"path":"README.md"}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if got.Status != "ok" {
		t.Fatalf("Status=%q want ok", got.Status)
	}
	if got.Output != `{"ok":true}` {
		t.Fatalf("Output=%q want JSON payload", got.Output)
	}
}

func TestToolBridge_ErrorMappedToToolFailed(t *testing.T) {
	t.Parallel()

	bridge := v1bridge.NewToolBridge(fakeLegacyExecutor{
		err: stderrors.New("legacy boom"),
	})
	_, err := bridge.Execute(context.Background(), "fs_read_file", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error")
	}
	assertErrKind(t, err, v2errors.ErrToolFailed)
}

func TestToolBridge_NilLegacyIsDependencyError(t *testing.T) {
	t.Parallel()

	bridge := v1bridge.NewToolBridge(nil)
	_, err := bridge.Execute(context.Background(), "fs_read_file", nil)
	if err == nil {
		t.Fatal("expected dependency error")
	}
	assertErrKind(t, err, v2errors.ErrDependency)
}

func assertErrKind(t *testing.T, err error, kind v2errors.ErrorKind) {
	t.Helper()
	var verr *v2errors.V2Error
	if !stderrors.As(err, &verr) {
		t.Fatalf("expected V2Error, got %T", err)
	}
	if verr.Kind != kind {
		t.Fatalf("error kind=%q want %q", verr.Kind, kind)
	}
}

type fakeLegacyExecutor struct {
	out string
	err error
}

func (f fakeLegacyExecutor) Execute(_ context.Context, _ string, _ json.RawMessage) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.out, nil
}
